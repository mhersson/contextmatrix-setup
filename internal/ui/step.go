package ui

import (
	"fmt"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
)

type StepState int

const (
	Pending StepState = iota
	Running
	Done
	Failed
)

const (
	verbWidth    = 9
	targetWidth  = 29
	elapsedWidth = 8
)

var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// StepLine renders one progress row: glyph, verb, target, detail and
// elapsed time, fitted to Width-1. A zero elapsed leaves the column empty.
func (t *Theme) StepLine(state StepState, verb, target, detail string, elapsed time.Duration) string {
	var glyph string

	switch state {
	case Running:
		glyph = t.spinner(2)
	case Done:
		glyph = t.Good("✓")
	case Failed:
		glyph = t.Bad("✗")
	default:
		glyph = t.Dim("·")
	}

	return t.stepRow(glyph, state, verb, target, detail, elapsed)
}

func (t *Theme) spinner(frame int) string {
	return t.fg(t.accent).Bold(true).Render(spinnerFrames[frame%len(spinnerFrames)])
}

func (t *Theme) stepRow(glyph string, state StepState, verb, target, detail string, elapsed time.Duration) string {
	w := t.mode.Width - 1
	verb, target = Fit(verb, verbWidth-1), Fit(target, targetWidth-1)

	if detail != "" {
		switch state {
		case Running:
			detail = t.Accent(detail)
		case Failed:
			detail = t.Bad(detail)
		case Pending:
			detail = t.Dim(detail)
		}
	}

	if state == Pending {
		target = t.Dim(target)
	}

	left := " " + glyph + "  " + Pad(t.Dim(verb), verbWidth) + Pad(target, targetWidth)
	room := w - lipgloss.Width(left) - elapsedWidth

	if elapsed <= 0 {
		return Fit(strings.TrimRight(left+Fit(detail, room), " "), w)
	}

	line := left + Pad(Fit(detail, room), room) + t.Dim(fmt.Sprintf("%*s", elapsedWidth, Duration(elapsed)))

	return Fit(line, w)
}

// Duration formats an elapsed time as 4.2s, or 1m05s from a minute on.
func Duration(d time.Duration) string {
	d = d.Round(100 * time.Millisecond)
	if d < time.Minute {
		return fmt.Sprintf("%.1fs", d.Seconds())
	}

	return fmt.Sprintf("%dm%02ds", int(d.Minutes()), int(d.Seconds())%60)
}

// FailureBox shows the tail of a failed step's output and where the full log
// is, boxed and indented one space within Width-1.
func (t *Theme) FailureBox(title string, tail []string, logPath string) string {
	w := t.mode.Width - 1
	boxWidth := w - 1
	inner := boxWidth - 4 // border and padding

	var rows []string

	for _, l := range tail {
		if strings.TrimSpace(l) != "" {
			rows = append(rows, t.Dim(Fit(l, inner)))
		}
	}

	head := " " + t.Bad("✗ "+title)
	if len(rows) > 0 {
		head += "  " + t.Dim(fmt.Sprintf("last %d lines", len(rows)))
	}

	head = Fit(head, w)

	body := strings.Join(rows, "\n")
	if logPath != "" {
		if body != "" {
			body += "\n\n"
		}

		body += t.Bold("full log") + "  " + logPath
	}

	if body == "" {
		return head
	}

	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(t.fail).
		Padding(0, 1).
		Width(boxWidth).
		Render(body)

	return head + "\n " + strings.ReplaceAll(box, "\n", "\n ")
}
