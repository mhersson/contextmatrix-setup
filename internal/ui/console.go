package ui

import (
	"bytes"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/charmbracelet/x/ansi"
)

const (
	tailLines    = 40
	maxLineBytes = 4096 // bounds the tail when output never ends a line
	spinInterval = 100 * time.Millisecond

	cursorUp  = "\x1b[1A"
	eraseLine = "\x1b[2K"
)

// Console renders steps on out: a redrawn live region on a colour terminal,
// one line per event otherwise. The log, when set, gets the plain transcript
// plus every step's raw output.
type Console struct {
	// Now times the steps; tests replace it.
	Now func() time.Time

	out     io.Writer
	log     io.Writer
	logPath string
	t       *Theme
	live    bool

	// mu guards every write to out and log, and the fields below.
	mu       sync.Mutex
	interval time.Duration
	cur      *Step
	drawn    int  // rows of the live region on screen
	frame    int  // spinner frame
	midLine  bool // the last step output did not end its line
	stop     chan struct{}
	done     chan struct{}
	closed   bool
}

// NewConsole renders on out with t. log may be nil, and then logPath is "".
func NewConsole(out io.Writer, t *Theme, log io.Writer, logPath string) *Console {
	if log == nil {
		logPath = ""
	}

	m := t.Mode()

	return &Console{
		Now:      time.Now,
		out:      out,
		log:      log,
		logPath:  logPath,
		t:        t,
		live:     m.TTY && !m.Plain,
		interval: spinInterval,
	}
}

func (c *Console) LogPath() string { return c.logPath }

func (c *Console) Theme() *Theme { return c.t }

// Println writes s on its own line, above the live region when one is drawn.
func (c *Console) Println(s string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.lineLocked(s, true)

	if c.live && c.cur != nil {
		c.drawLocked()
	}
}

// Start begins a step. Only one step runs at a time: a step still running
// is first finished as done with no detail.
func (c *Console) Start(verb, target, detail string) *Step {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.cur != nil {
		c.finishLocked(c.cur, Done, "")
	}

	s := &Step{c: c, verb: verb, target: target, detail: detail, start: c.Now()}
	c.cur = s

	if c.live {
		c.drawLocked()

		if c.stop == nil && !c.closed {
			c.stop, c.done = make(chan struct{}), make(chan struct{})
			go c.spin(c.stop, c.done, c.interval)
		}
	}

	return s
}

// Close stops the spinner and is idempotent. A step still running stays
// drawn as it is, so fail it first to end it cleanly.
func (c *Console) Close() {
	c.mu.Lock()
	stop, done := c.stop, c.done
	c.stop, c.done, c.closed = nil, nil, true
	c.mu.Unlock()

	if stop != nil {
		close(stop)
		<-done
	}
}

func (c *Console) spin(stop <-chan struct{}, done chan<- struct{}, every time.Duration) {
	defer close(done)

	tk := time.NewTicker(every)
	defer tk.Stop()

	for {
		select {
		case <-stop:
			return
		case <-tk.C:
			c.tick()
		}
	}
}

func (c *Console) tick() {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.cur == nil {
		return
	}

	c.frame++
	c.drawLocked()
}

func (c *Console) finish(s *Step, state StepState, detail string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.finishLocked(s, state, detail)
}

func (c *Console) finishLocked(s *Step, state StepState, detail string) {
	if s.finished {
		return
	}

	s.finished = true

	if c.cur == s {
		c.cur = nil
	}

	c.lineLocked(c.t.StepLine(state, s.verb, s.target, detail, c.Now().Sub(s.start)), true)
}

func (c *Console) write(s *Step, p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if !s.announced && !s.finished {
		s.announced = true
		c.lineLocked(c.t.stepRow(c.t.Accent("→"), Running, s.verb, s.target, s.detail, 0), !c.live)
	}

	s.feed(p)

	if text := ansi.Strip(string(p)); text != "" {
		c.midLine = !strings.HasSuffix(text, "\n")
	}

	if c.log != nil {
		// The log is best-effort: a full disk must not fail the build.
		_, _ = c.log.Write(p)
	}

	if !c.live {
		if _, err := io.WriteString(c.out, c.t.Output(string(p))); err != nil {
			return 0, fmt.Errorf("write step output: %w", err)
		}
	}

	return len(p), nil
}

// lineLocked writes a complete line to the log and, when toOut, to out in
// place of any live region. Output that stopped mid-line is ended first.
func (c *Console) lineLocked(s string, toOut bool) {
	nl := ""
	if c.midLine {
		nl = "\n"
	}

	c.midLine = false

	if c.log != nil {
		_, _ = io.WriteString(c.log, nl+ansi.Strip(s)+"\n")
	}

	if !toOut {
		return
	}

	if c.live {
		c.clearLocked()

		nl = ""
	}

	c.writeOut(nl + c.t.Output(s) + "\n")
}

// drawLocked rewrites the live region in place. It never ends the region
// with a newline, so the cursor stays on its last row.
func (c *Console) drawLocked() {
	lines := c.liveLines()
	if len(lines) < c.drawn {
		c.clearLocked()
	}

	var b strings.Builder

	b.WriteString("\r")

	for range c.drawn - 1 {
		b.WriteString(cursorUp)
	}

	for i, l := range lines {
		if i > 0 {
			b.WriteString("\n")
		}

		b.WriteString(eraseLine + l)
	}

	c.writeOut(b.String())
	c.drawn = len(lines)
}

// clearLocked erases the live region and leaves the cursor at its start.
func (c *Console) clearLocked() {
	if c.drawn == 0 {
		return
	}

	c.writeOut("\r" + eraseLine + strings.Repeat(cursorUp+eraseLine, c.drawn-1))
	c.drawn = 0
}

func (c *Console) liveLines() []string {
	s := c.cur
	lines := []string{c.t.stepRow(c.t.spinner(c.frame), Running, s.verb, s.target, s.detail, c.Now().Sub(s.start))}

	if last := s.lastLine(); last != "" {
		lines = append(lines, c.t.Dim("    ╰ "+Fit(last, c.t.mode.Width-1-6)))
	}

	return lines
}

// writeOut has no caller to report to; a terminal that is gone ends the
// process on the next write anyway.
func (c *Console) writeOut(s string) {
	_, _ = io.WriteString(c.out, s)
}

// Step is one unit of work on a Console.
type Step struct {
	c                    *Console
	verb, target, detail string
	start                time.Time

	// Guarded by c.mu.
	announced, finished bool
	lines               []string
	cur                 string // raw text of the line being written
	afterCR             bool   // a \r ended the last segment, so new text replaces cur
}

// Writer takes the step's subprocess output: raw into the log, into the
// tail, and stripped onto out in plain mode. It is safe for concurrent use.
func (s *Step) Writer() io.Writer { return stepWriter{s} }

type stepWriter struct{ s *Step }

func (w stepWriter) Write(p []byte) (int, error) { return w.s.c.write(w.s, p) }

// Done ends the step as succeeded; detail replaces the start detail. Only
// the first Done or Fail counts.
func (s *Step) Done(detail string) { s.c.finish(s, Done, detail) }

// Fail ends the step as failed; detail replaces the start detail.
func (s *Step) Fail(detail string) { s.c.finish(s, Failed, detail) }

// Tail returns the last 40 non-blank output lines, escapes stripped. A line
// still being written counts as the last one.
func (s *Step) Tail() []string {
	s.c.mu.Lock()
	defer s.c.mu.Unlock()

	tail := append([]string(nil), s.lines...)
	if l := cleanLine(s.cur); l != "" {
		tail = append(tail, l)
	}

	return tail[max(len(tail)-tailLines, 0):]
}

// feed splits output on \n and \r. A \r keeps the line until new text
// replaces it, as a terminal shows progress updates.
func (s *Step) feed(p []byte) {
	for len(p) > 0 {
		seg := p

		i := bytes.IndexAny(p, "\r\n")
		if i >= 0 {
			seg = p[:i]
		}

		if len(seg) > 0 {
			if s.afterCR {
				s.cur, s.afterCR = "", false
			}

			s.cur += string(seg[:min(len(seg), max(maxLineBytes-len(s.cur), 0))])
		}

		if i < 0 {
			return
		}

		if p[i] == '\r' {
			s.afterCR = true
		} else {
			s.commit()
		}

		p = p[i+1:]
	}
}

func (s *Step) commit() {
	if l := cleanLine(s.cur); l != "" {
		if len(s.lines) == tailLines {
			s.lines = append(s.lines[:0], s.lines[1:]...)
		}

		s.lines = append(s.lines, l)
	}

	s.cur, s.afterCR = "", false
}

func (s *Step) lastLine() string {
	if l := cleanLine(s.cur); l != "" {
		return l
	}

	if len(s.lines) > 0 {
		return s.lines[len(s.lines)-1]
	}

	return ""
}

// cleanLine strips escapes and control characters, which would break width
// fitting, and returns "" for a blank line.
func cleanLine(raw string) string {
	l := strings.Map(func(r rune) rune {
		switch {
		case r == '\t':
			return ' '
		case unicode.IsControl(r):
			return -1
		}

		return r
	}, ansi.Strip(raw))

	if strings.TrimSpace(l) == "" {
		return ""
	}

	return strings.TrimRight(l, " ")
}
