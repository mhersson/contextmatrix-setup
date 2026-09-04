// Package run wraps process execution behind an interface so every other
// package can be tested with scripted commands.
package run

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
)

type Cmd struct {
	Name  string
	Args  []string
	Dir   string
	Env   []string
	Stdin io.Reader
}

type Result struct {
	Stdout   string
	Stderr   string
	ExitCode int
}

type Runner interface {
	Run(ctx context.Context, c Cmd) (Result, error)
	Stream(ctx context.Context, c Cmd, out io.Writer) error
	LookPath(name string) (string, bool)
}

// ExitError carries the last lines of combined output so a failed build can
// be reported without re-running it.
type ExitError struct {
	Cmd  Cmd
	Code int
	Tail string
}

func (e *ExitError) Error() string {
	return fmt.Sprintf("%s %s exited %d", e.Cmd.Name, strings.Join(e.Cmd.Args, " "), e.Code)
}

type Exec struct{}

func (Exec) Run(ctx context.Context, c Cmd) (Result, error) {
	cmd := build(ctx, c)

	var stdout, stderr bytes.Buffer

	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	res := Result{Stdout: stdout.String(), Stderr: stderr.String()}

	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		res.ExitCode = exitErr.ExitCode()

		return res, nil
	}

	if err != nil {
		return res, fmt.Errorf("run %s: %w", c.Name, err)
	}

	return res, nil
}

func (Exec) Stream(ctx context.Context, c Cmd, out io.Writer) error {
	cmd := build(ctx, c)
	tail := &tailBuffer{limit: 40}
	w := io.MultiWriter(out, tail)
	cmd.Stdout = w
	cmd.Stderr = w

	err := cmd.Run()

	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return &ExitError{Cmd: c, Code: exitErr.ExitCode(), Tail: tail.String()}
	}

	if err != nil {
		return fmt.Errorf("run %s: %w", c.Name, err)
	}

	return nil
}

func (Exec) LookPath(name string) (string, bool) {
	p, err := exec.LookPath(name)

	return p, err == nil
}

func build(ctx context.Context, c Cmd) *exec.Cmd {
	// Running caller-supplied commands is this package's purpose: it wraps
	// process execution behind an interface for every other package.
	cmd := exec.CommandContext(ctx, c.Name, c.Args...) //nolint:gosec
	cmd.Dir = c.Dir
	cmd.Stdin = c.Stdin

	if len(c.Env) > 0 {
		cmd.Env = append(os.Environ(), c.Env...)
	}

	return cmd
}

// tailBuffer keeps the last limit lines written to it.
type tailBuffer struct {
	limit int
	lines []string
	part  string
}

func (t *tailBuffer) Write(p []byte) (int, error) {
	t.part += string(p)

	for {
		i := strings.IndexByte(t.part, '\n')
		if i < 0 {
			break
		}

		t.lines = append(t.lines, t.part[:i])
		t.part = t.part[i+1:]

		if len(t.lines) > t.limit {
			t.lines = t.lines[1:]
		}
	}

	return len(p), nil
}

func (t *tailBuffer) String() string {
	lines := t.lines
	if t.part != "" {
		lines = append(append([]string{}, lines...), t.part)
	}

	return strings.Join(lines, "\n")
}
