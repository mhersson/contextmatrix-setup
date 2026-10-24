package ui

import (
	"bytes"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type lockedBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	return l.b.Write(p)
}

func (l *lockedBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()

	return l.b.String()
}

// screen replays terminal output as a cooked tty shows it (\n is CRLF) and
// returns the visible rows, trailing blanks trimmed.
func screen(raw string) []string {
	rows := [][]rune{nil}
	row, col := 0, 0
	rs := []rune(raw)

	for i := 0; i < len(rs); i++ {
		for len(rows) <= row {
			rows = append(rows, nil)
		}

		switch r := rs[i]; {
		case r == '\r':
			col = 0
		case r == '\n':
			row, col = row+1, 0
		case r == 0x1b && i+1 < len(rs) && rs[i+1] == '[':
			j := i + 2
			for j < len(rs) && (rs[j] < 0x40 || rs[j] > 0x7e) {
				j++
			}

			if j == len(rs) {
				i = j

				continue
			}

			switch rs[j] {
			case 'A':
				row = max(row-1, 0)
			case 'B':
				row++
			case 'K':
				if string(rs[i+2:j]) == "2" {
					rows[row] = nil
				} else if col < len(rows[row]) {
					rows[row] = rows[row][:col]
				}
			}

			i = j
		default:
			for len(rows[row]) < col {
				rows[row] = append(rows[row], ' ')
			}

			if col < len(rows[row]) {
				rows[row][col] = r
			} else {
				rows[row] = append(rows[row], r)
			}

			col++
		}
	}

	out := make([]string, 0, len(rows))
	for _, r := range rows {
		out = append(out, strings.TrimRight(string(r), " "))
	}

	for len(out) > 0 && out[len(out)-1] == "" {
		out = out[:len(out)-1]
	}

	return out
}

func plainLines(s string) []string {
	return strings.Split(strings.TrimSuffix(s, "\n"), "\n")
}

func liveConsole(t *testing.T, width int, log io.Writer) (*Console, *lockedBuffer) {
	t.Helper()

	out := &lockedBuffer{}
	c := NewConsole(out, testTheme(true, false, width), log, "/logs/update.log")
	c.interval = time.Hour
	t.Cleanup(c.Close)

	return c, out
}

func TestPlainStepPrintsStartAndDoneAndStreams(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer

	c := NewConsole(&out, testTheme(true, true, 100), nil, "")
	s := c.Start("build", "contextmatrix", "make install")
	_, err := io.WriteString(s.Writer(), "compiling\n")
	require.NoError(t, err)
	s.Done("make install")
	c.Close()

	raw := out.String()
	assert.NotContains(t, raw, "\x1b")

	lines := plainLines(raw)
	require.Len(t, lines, 3, "%q", raw)
	assert.Contains(t, lines[0], "build")
	assert.Contains(t, lines[0], "contextmatrix")
	assert.Equal(t, "compiling", lines[1])
	assert.True(t, strings.HasPrefix(lines[2], " ✓"), lines[2])
	assert.Contains(t, lines[2], "build")
}

func TestPlainQuietStepPrintsOneLine(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer

	c := NewConsole(&out, testTheme(true, true, 100), nil, "")
	c.Start("config", "4 files", "").Done("in sync")
	c.Close()

	lines := plainLines(out.String())
	require.Len(t, lines, 1, "%q", out.String())
	assert.True(t, strings.HasPrefix(lines[0], " ✓"), lines[0])
	assert.Contains(t, lines[0], "config")
}

func TestPlainStepEndsPartialLineBeforeDoneLine(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer

	c := NewConsole(&out, testTheme(true, true, 100), nil, "")
	s := c.Start("build", "contextmatrix", "make install")
	_, err := io.WriteString(s.Writer(), "compiling")
	require.NoError(t, err)
	s.Done("make install")

	lines := plainLines(out.String())
	require.Len(t, lines, 3, "%q", out.String())
	assert.Equal(t, "compiling", lines[1])
	assert.True(t, strings.HasPrefix(lines[2], " ✓"), lines[2])
}

func TestLiveStepSendsOutputToLogNotOut(t *testing.T) {
	t.Parallel()

	var log bytes.Buffer

	c, out := liveConsole(t, 100, &log)
	s := c.Start("build", "contextmatrix", "make install")
	_, err := io.WriteString(s.Writer(), "secret line\n")
	require.NoError(t, err)
	s.Done("make install")

	assert.NotContains(t, out.String(), "secret")

	rows := screen(out.String())
	require.Len(t, rows, 1, "%q", rows)
	assert.True(t, strings.HasPrefix(rows[0], " ✓"), rows[0])
	assert.True(t, strings.HasSuffix(out.String(), "\n"), "the cursor ends on a fresh line")

	// The log is the plain transcript: step lines stripped, output raw.
	logLines := plainLines(log.String())
	require.Len(t, logLines, 3, "%q", log.String())
	assert.Contains(t, logLines[0], "build")
	assert.Equal(t, "secret line", logLines[1])
	assert.True(t, strings.HasPrefix(logLines[2], " ✓"), logLines[2])
	assert.NotContains(t, log.String(), "\x1b")
}

func TestStepTailHandlesCarriageReturnsAndPartialLines(t *testing.T) {
	t.Parallel()

	const in = "a\n50%\r100%\r\n\x1b[31mred\x1b[0m\npartial"

	for _, chunk := range []int{len(in), 1} {
		t.Run(fmt.Sprintf("chunk %d", chunk), func(t *testing.T) {
			t.Parallel()

			var out, log bytes.Buffer

			c := NewConsole(&out, testTheme(true, true, 100), &log, "/logs/update.log")
			s := c.Start("build", "contextmatrix", "")
			w := s.Writer()

			for b := []byte(in); len(b) > 0; {
				n := min(chunk, len(b))
				_, err := w.Write(b[:n])
				require.NoError(t, err)

				b = b[n:]
			}

			assert.Equal(t, []string{"a", "100%", "red", "partial"}, s.Tail())
			assert.Contains(t, log.String(), in, "the log keeps the raw bytes")
			assert.NotContains(t, out.String(), "\x1b")
		})
	}
}

func TestStepTailKeepsLast40(t *testing.T) {
	t.Parallel()

	c := NewConsole(io.Discard, testTheme(true, true, 100), nil, "")
	s := c.Start("build", "contextmatrix", "")
	w := s.Writer()

	for i := range 50 {
		fmt.Fprintf(w, "line %d\n  \n", i)
	}

	tail := s.Tail()
	require.Len(t, tail, 40)
	assert.Equal(t, "line 10", tail[0])
	assert.Equal(t, "line 49", tail[39])

	fmt.Fprint(w, "line 50")

	tail = s.Tail()
	require.Len(t, tail, 40)
	assert.Equal(t, "line 11", tail[0])
	assert.Equal(t, "line 50", tail[39])
}

func TestConsoleCancelledStepEndsFailed(t *testing.T) {
	t.Parallel()

	out := &lockedBuffer{}
	c := NewConsole(out, testTheme(true, false, 100), nil, "")
	c.interval = time.Millisecond

	s := c.Start("build", "contextmatrix", "make install")
	_, err := io.WriteString(s.Writer(), "compiling module\n")
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		rows := screen(out.String())

		return len(rows) == 2 && strings.Contains(rows[1], "╰ compiling module")
	}, 5*time.Second, time.Millisecond, "the spinner draws the tail line")

	s.Fail("cancelled")
	c.Close()
	c.Close()

	raw := out.String()
	assert.NotContains(t, raw, "\x1b[?25l")
	assert.True(t, strings.HasSuffix(raw, "\n"), "the cursor ends on a fresh line")

	rows := screen(raw)
	require.Len(t, rows, 1, "the live region is gone: %q", rows)
	assert.True(t, strings.HasPrefix(rows[0], " ✗"), rows[0])
	assert.Contains(t, rows[0], "cancelled")
}

func TestCloseStopsSpinner(t *testing.T) {
	t.Parallel()

	out := &lockedBuffer{}
	c := NewConsole(out, testTheme(true, false, 100), nil, "")
	c.interval = time.Millisecond

	c.Start("build", "contextmatrix", "make install")

	first := out.String()

	require.Eventually(t, func() bool { return out.String() != first }, 5*time.Second, time.Millisecond)

	c.Close()

	stopped := out.String()

	time.Sleep(20 * time.Millisecond)
	assert.Equal(t, stopped, out.String())
}

func TestLivePrintlnRedrawsStepBelow(t *testing.T) {
	t.Parallel()

	c, out := liveConsole(t, 100, nil)
	s := c.Start("build", "contextmatrix", "make install")
	c.Println("note: something")

	rows := screen(out.String())
	require.Len(t, rows, 2, "%q", rows)
	assert.Equal(t, "note: something", rows[0])
	assert.Contains(t, rows[1], "contextmatrix")

	s.Done("make install")

	rows = screen(out.String())
	require.Len(t, rows, 2, "%q", rows)
	assert.Equal(t, "note: something", rows[0])
	assert.True(t, strings.HasPrefix(rows[1], " ✓"), rows[1])
}

func TestStepFinishesOnce(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer

	c := NewConsole(&out, testTheme(true, true, 100), nil, "")
	fetch := c.Start("fetch", "4 app repos", "origin/main")
	build := c.Start("build", "contextmatrix", "make install")

	fetch.Done("late")
	build.Fail("boom")
	build.Done("again")

	lines := plainLines(out.String())
	require.Len(t, lines, 2, "%q", out.String())
	assert.True(t, strings.HasPrefix(lines[0], " ✓"), lines[0])
	assert.Contains(t, lines[0], "fetch")
	assert.NotContains(t, lines[0], "late")
	assert.True(t, strings.HasPrefix(lines[1], " ✗"), lines[1])
	assert.Contains(t, lines[1], "boom")
}

func TestStepLineFitsWidth(t *testing.T) {
	t.Parallel()

	long := strings.Repeat("a long detail ", 20)

	for _, w := range []int{60, 30} {
		for _, plain := range []bool{false, true} {
			th := testTheme(true, plain, w)
			for _, st := range []StepState{Pending, Running, Done, Failed} {
				line := th.StepLine(st, "build", "contextmatrix-architect-with-a-long-name", long, 75*time.Second)
				assert.LessOrEqual(t, lipgloss.Width(line), w-1, "width %d state %d", w, st)
				assert.NotContains(t, line, "\n")
			}
		}
	}
}

func TestLiveRegionFitsWidth(t *testing.T) {
	t.Parallel()

	c, out := liveConsole(t, 40, nil)
	s := c.Start("build", "contextmatrix-architect", strings.Repeat("detail ", 20))
	_, err := io.WriteString(s.Writer(), "\tindented\twith tabs and a line far longer than the terminal\n")
	require.NoError(t, err)
	c.tick()

	rows := screen(out.String())
	require.Len(t, rows, 2, "%q", rows)
	assert.Contains(t, rows[1], "indented with", "tabs become spaces")

	for _, r := range rows {
		assert.LessOrEqual(t, lipgloss.Width(r), 39, r)
		assert.NotContains(t, r, "\t")
	}
}

func boxContents(box string) []string {
	var rows []string

	for l := range strings.SplitSeq(box, "\n") {
		if l = strings.TrimSpace(l); strings.HasPrefix(l, "│") {
			rows = append(rows, strings.TrimSpace(strings.Trim(l, "│")))
		}
	}

	return rows
}

func TestFailureBoxShowsTailAndLogPath(t *testing.T) {
	t.Parallel()

	th := testTheme(true, true, 60)
	tail := []string{"first", "", "   ", "second"}

	box := th.Output(th.FailureBox("contextmatrix build failed", tail, "/home/u/.cache/update.log"))
	assert.Contains(t, box, "contextmatrix build failed")

	for l := range strings.SplitSeq(box, "\n") {
		assert.LessOrEqual(t, lipgloss.Width(l), 59, l)
	}

	rows := boxContents(box)
	require.Len(t, rows, 4, "%q", rows)
	assert.Equal(t, []string{"first", "second", ""}, rows[:3])
	assert.Contains(t, rows[3], "/home/u/.cache/update.log")

	noLog := th.Output(th.FailureBox("contextmatrix build failed", tail, ""))
	assert.Equal(t, []string{"first", "second"}, boxContents(noLog))
}

func TestDuration(t *testing.T) {
	t.Parallel()

	tests := []struct {
		in   time.Duration
		want string
	}{
		{1500 * time.Millisecond, "1.5s"},
		{59960 * time.Millisecond, "1m00s"},
		{75 * time.Second, "1m15s"},
	}

	for _, tt := range tests {
		assert.Equal(t, tt.want, Duration(tt.in), tt.in)
	}
}

func TestCloseClearsLiveRegion(t *testing.T) {
	t.Parallel()

	c, out := liveConsole(t, 100, nil)
	s := c.Start("build", "contextmatrix", "make install")
	_, err := io.WriteString(s.Writer(), "compiling module\n")
	require.NoError(t, err)
	c.tick()
	require.Len(t, screen(out.String()), 2)

	c.Close()
	c.tick() // a tick that lost the race with Close must not redraw

	_, err = io.WriteString(out, "next\n")
	require.NoError(t, err)
	assert.Equal(t, []string{"next"}, screen(out.String()), "the next print starts on a clean row")
}

func TestLiveRedrawReplacesTwoRowRegion(t *testing.T) {
	t.Parallel()

	c, out := liveConsole(t, 100, nil)
	s := c.Start("build", "contextmatrix", "make install")
	w := s.Writer()

	_, err := io.WriteString(w, "first line\n")
	require.NoError(t, err)
	c.tick()

	_, err = io.WriteString(w, "second line\n")
	require.NoError(t, err)
	c.tick()

	rows := screen(out.String())
	require.Len(t, rows, 2, "%q", rows)
	assert.Contains(t, rows[0], "contextmatrix")
	assert.Contains(t, rows[1], "╰ second line")
}

func TestMultiLineDetailStaysOneRow(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer

	c := NewConsole(&out, testTheme(true, true, 100), nil, "")
	c.Start("fetch", "contextmatrix\n", "origin/main").Fail("exit status 128\nfatal: bad object\n\thint: x")

	lines := plainLines(out.String())
	require.Len(t, lines, 1, "%q", out.String())
	assert.Contains(t, lines[0], "exit status 128 fatal: bad object hint: x")

	lc, lo := liveConsole(t, 100, nil)
	s := lc.Start("build", "contextmatrix", "line one\nline two")
	_, err := io.WriteString(s.Writer(), "output\n")
	require.NoError(t, err)
	lc.tick()
	lc.tick()
	require.Len(t, screen(lo.String()), 2, "%q", screen(lo.String()))

	s.Fail("a\nb")
	assert.Len(t, screen(lo.String()), 1, "%q", screen(lo.String()))
}
