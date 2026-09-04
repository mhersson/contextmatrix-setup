package run

import (
	"context"
	"io"
	"strings"
	"sync"
)

type Stub struct {
	name   string
	prefix []string
	res    Result
	err    error
	do     func()
}

func (s *Stub) Return(stdout, stderr string, code int) *Stub {
	s.res = Result{Stdout: stdout, Stderr: stderr, ExitCode: code}

	return s
}

func (s *Stub) Fail(err error) *Stub {
	s.err = err

	return s
}

// Do registers a callback run when the stub matches, for tests that need a
// side effect the fake cannot produce, such as a rebuilt file on disk.
func (s *Stub) Do(fn func()) *Stub {
	s.do = fn

	return s
}

// Fake matches a call by name and the longest scripted args prefix. On equal
// prefix length the latest registration wins, so a test can override a
// default stub. Unscripted calls return exit 127 and are still recorded.
type Fake struct {
	mu    sync.Mutex
	stubs []*Stub
	calls []Cmd
	paths map[string]bool
}

func NewFake() *Fake {
	return &Fake{paths: map[string]bool{}}
}

func (f *Fake) On(name string, args ...string) *Stub {
	s := &Stub{name: name, prefix: args}
	f.stubs = append(f.stubs, s)

	return s
}

func (f *Fake) Has(name string) {
	f.paths[name] = true
}

func (f *Fake) Calls() []Cmd {
	f.mu.Lock()
	defer f.mu.Unlock()

	return append([]Cmd{}, f.calls...)
}

func (f *Fake) Run(_ context.Context, c Cmd) (Result, error) {
	f.mu.Lock()
	f.calls = append(f.calls, c)
	f.mu.Unlock()

	best := f.match(c)
	if best == nil {
		return Result{ExitCode: 127, Stderr: "fake: unscripted " + c.Name + " " + strings.Join(c.Args, " ")}, nil
	}

	if best.do != nil {
		best.do()
	}

	return best.res, best.err
}

func (f *Fake) Stream(ctx context.Context, c Cmd, out io.Writer) error {
	res, err := f.Run(ctx, c)
	if err != nil {
		return err
	}

	_, _ = io.WriteString(out, res.Stdout+res.Stderr)

	if res.ExitCode != 0 {
		return &ExitError{Cmd: c, Code: res.ExitCode, Tail: res.Stderr}
	}

	return nil
}

func (f *Fake) LookPath(name string) (string, bool) {
	if f.paths[name] {
		return "/usr/bin/" + name, true
	}

	return "", false
}

func (f *Fake) match(c Cmd) *Stub {
	var best *Stub

	for _, s := range f.stubs {
		if s.name != c.Name || len(s.prefix) > len(c.Args) {
			continue
		}

		ok := true

		for i, a := range s.prefix {
			if c.Args[i] != a {
				ok = false

				break
			}
		}

		if ok && (best == nil || len(s.prefix) >= len(best.prefix)) {
			best = s
		}
	}

	return best
}
