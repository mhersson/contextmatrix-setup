// Package run wraps process execution behind an interface so every other
// package can be tested with scripted commands.
package run

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
)

type Cmd struct {
	Name string
	Args []string
	Dir  string
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

// ExitError reports a streamed command that exited non-zero; its output
// already went to the stream.
type ExitError struct {
	Cmd  Cmd
	Code int
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

	if exitErr, ok := errors.AsType[*exec.ExitError](err); ok {
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
	cmd.Stdout = out
	cmd.Stderr = out

	err := cmd.Run()

	if exitErr, ok := errors.AsType[*exec.ExitError](err); ok {
		return &ExitError{Cmd: c, Code: exitErr.ExitCode()}
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

	return cmd
}
