package wizard

import (
	"errors"
	"image/color"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
	"charm.land/lipgloss/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func noteForm(title string) *huh.Form {
	return huh.NewForm(huh.NewGroup(huh.NewNote().Title(title).Description("body")))
}

func TestScreenCentresTheFormInAFrame(t *testing.T) {
	s := newScreen(func() (*huh.Form, error) { return noteForm("Hello"), nil })
	s.Init()
	s.Update(tea.WindowSizeMsg{Width: 100, Height: 30})

	view := s.View().Content
	lines := strings.Split(view, "\n")
	require.Len(t, lines, 30, "the view fills the terminal so the panel can sit in the middle")

	top := -1

	for i, line := range lines {
		if strings.Contains(line, "╭") {
			top = i

			break
		}
	}

	require.Positive(t, top, "blank rows above the panel")
	assert.True(t, strings.HasPrefix(lines[top], "   "), "blank columns left of the panel: %q", lines[top])
	assert.Contains(t, view, "Hello")
	assert.Contains(t, view, "ContextMatrix")

	// Every framed row is the same width, and narrower than the terminal.
	width := 0

	for _, line := range lines[top:] {
		if !strings.Contains(line, "│") && !strings.Contains(line, "╭") && !strings.Contains(line, "╰") {
			continue
		}

		w := lipgloss.Width(strings.TrimRight(line, " "))
		if width == 0 {
			width = w
		}

		assert.Equal(t, width, w, "ragged frame: %q", line)
	}

	assert.Less(t, width, 100)
}

func TestScreenWithoutASizeStillRendersThePanel(t *testing.T) {
	s := newScreen(func() (*huh.Form, error) { return noteForm("Hello"), nil })
	s.Init()

	assert.Contains(t, s.View().Content, "Hello")
}

// drive runs a screen as a real program fed by the given key bytes.
func drive(t *testing.T, keys string, steps ...step) error {
	t.Helper()

	r, w, err := os.Pipe()
	require.NoError(t, err)

	s := newScreen(steps...)
	p := tea.NewProgram(s, tea.WithInput(r), tea.WithOutput(io.Discard), tea.WithoutRenderer(), tea.WithoutSignalHandler())

	done := make(chan error, 1)

	go func() {
		_, err := p.Run()
		done <- err
	}()

	go func() {
		for _, k := range keys {
			time.Sleep(20 * time.Millisecond)

			_, _ = w.WriteString(string(k))
		}
	}()

	select {
	case err := <-done:
		_ = w.Close()

		if err != nil {
			return err
		}

		return s.err
	case <-time.After(5 * time.Second):
		p.Kill()

		_ = w.Close()

		t.Fatal("screen did not finish")

		return nil
	}
}

func TestScreenRunsStepsInOrderAndSkipsNilForms(t *testing.T) {
	var built []string

	form := func(name string) step {
		return func() (*huh.Form, error) {
			built = append(built, name)

			return noteForm(name), nil
		}
	}

	skip := func() (*huh.Form, error) {
		built = append(built, "skip")

		return nil, nil
	}

	require.NoError(t, drive(t, "\r\r", form("a"), skip, form("b")))
	assert.Equal(t, []string{"a", "skip", "b"}, built)
}

func TestScreenBuildsAStepOnlyAfterTheOneBeforeIt(t *testing.T) {
	var built []string

	first := func() (*huh.Form, error) {
		built = append(built, "a")

		return noteForm("a"), nil
	}

	second := func() (*huh.Form, error) {
		require.Equal(t, []string{"a"}, built, "the second step sees the first one's answers")
		built = append(built, "b")

		return noteForm("b"), nil
	}

	require.NoError(t, drive(t, "\r\r", first, second))
	assert.Equal(t, []string{"a", "b"}, built)
}

func TestScreenReportsAnAbort(t *testing.T) {
	err := drive(t, "\x03", func() (*huh.Form, error) { return noteForm("a"), nil })
	assert.ErrorIs(t, err, huh.ErrUserAborted)
}

func TestScreenStopsAtAStepError(t *testing.T) {
	boom := errors.New("boom")
	err := drive(t, "\r", func() (*huh.Form, error) { return noteForm("a"), nil }, func() (*huh.Form, error) { return nil, boom })
	assert.ErrorIs(t, err, boom)
}

func TestScreenFollowsTheTerminalBackground(t *testing.T) {
	const darkAccent, lightAccent = "38;2;117;113;249", "38;2;90;86;224"

	s := newScreen(func() (*huh.Form, error) { return noteForm("Hello"), nil })
	s.Init()

	assert.Contains(t, s.View().Content, darkAccent, "dark is assumed until the terminal answers")

	s.Update(tea.BackgroundColorMsg{Color: color.White})
	assert.Contains(t, s.View().Content, lightAccent)
	assert.NotContains(t, s.View().Content, darkAccent)
}

func TestCharmThemeKeepsOptionTextReadable(t *testing.T) {
	for _, tc := range []struct {
		dark       bool
		text, back string
	}{
		{dark: true, text: "252", back: "237"},
		{dark: false, text: "235", back: "252"},
	} {
		th := charmTheme(tc.dark)

		for _, f := range []huh.FieldStyles{th.Focused, th.Blurred} {
			assert.Equal(t, lipgloss.Color(tc.text), f.UnselectedOption.GetForeground(), "dark=%v", tc.dark)
			assert.Equal(t, lipgloss.Color(tc.text), f.BlurredButton.GetForeground(), "dark=%v", tc.dark)
			assert.Equal(t, lipgloss.Color(tc.back), f.BlurredButton.GetBackground(), "dark=%v", tc.dark)
		}
	}
}
