// Package ui holds the shared terminal styling: a theme that adapts to the
// background, commit-type badges and boxed tables.
package ui

import (
	"os"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/term"
)

const (
	defaultWidth = 100
	maxWidth     = 120
)

// Mode describes the output terminal. Plain output carries no escape
// sequences and sends no terminal queries.
type Mode struct {
	TTY, Plain, Dark bool
	Width            int
}

// Detect inspects out and the environment.
func Detect(out *os.File, getenv func(string) string) Mode {
	fd := out.Fd()

	return detect(
		func() bool { return term.IsTerminal(fd) },
		func() (int, error) {
			w, _, err := term.GetSize(fd)

			return w, err
		},
		func() bool { return lipgloss.HasDarkBackground(os.Stdin, out) },
		getenv,
	)
}

func detect(isTerm func() bool, size func() (int, error), dark func() bool, getenv func(string) string) Mode {
	tty := isTerm()
	m := Mode{
		TTY:   tty,
		Plain: getenv("NO_COLOR") != "" || getenv("TERM") == "dumb" || !tty,
		Width: defaultWidth,
	}

	if w, err := size(); err == nil && w > 0 {
		m.Width = min(w, maxWidth)
	}

	m.Dark = m.Plain || dark()

	return m
}
