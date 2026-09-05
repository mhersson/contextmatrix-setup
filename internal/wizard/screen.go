package wizard

import (
	"errors"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
)

// Panel geometry: the frame never grows past panelWidth columns, and the
// form wraps inside the padding.
const (
	panelWidth = 80
	padX       = 2
	padY       = 1
)

var (
	accent = lipgloss.AdaptiveColor{Light: "#5A56E0", Dark: "#7571F9"}
	muted  = lipgloss.AdaptiveColor{Light: "245", Dark: "243"}

	frameStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(accent).
			Padding(padY, padX)
	brandStyle = lipgloss.NewStyle().Foreground(accent).Bold(true)
	mutedStyle = lipgloss.NewStyle().Foreground(muted)
)

// step builds the next form from what the earlier ones answered. A nil form
// skips the step; an error ends the run with it.
type step func() (*huh.Form, error)

// screen runs steps one after another inside a single alternate-screen
// program, drawing the current form centred in a framed panel. Forms are
// built lazily so a later step can depend on an earlier answer.
type screen struct {
	steps  []step
	next   int
	form   *huh.Form
	width  int
	height int
	err    error
}

func newScreen(steps ...step) *screen {
	return &screen{steps: steps}
}

func (s *screen) Init() tea.Cmd {
	return s.advance()
}

// advance installs the next form that is not skipped, or quits after the
// last one.
func (s *screen) advance() tea.Cmd {
	for s.next < len(s.steps) {
		form, err := s.steps[s.next]()
		s.next++

		if err != nil {
			s.err = err

			return tea.Quit
		}

		if form == nil {
			continue
		}

		s.form = form

		// The form's own Init asks the terminal for its size; the size this
		// screen already knows is handed over as well in case that fails.
		return tea.Batch(form.Init(), s.resize())
	}

	s.form = nil

	return tea.Quit
}

func (s *screen) resize() tea.Cmd {
	if s.width == 0 {
		return nil
	}

	size := tea.WindowSizeMsg{Width: s.width, Height: s.height}

	return func() tea.Msg { return size }
}

func (s *screen) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if size, ok := msg.(tea.WindowSizeMsg); ok {
		s.width, s.height = size.Width, size.Height
		msg = tea.WindowSizeMsg{Width: s.innerWidth(), Height: s.innerHeight()}
	}

	if s.form == nil {
		return s, nil
	}

	model, cmd := s.form.Update(msg)
	s.form = model.(*huh.Form)

	switch s.form.State {
	case huh.StateAborted:
		s.err = huh.ErrUserAborted

		return s, tea.Quit
	case huh.StateCompleted:
		return s, s.advance()
	default:
		return s, cmd
	}
}

func (s *screen) View() string {
	if s.form == nil || s.form.State != huh.StateNormal {
		return ""
	}

	header := brandStyle.Render("ContextMatrix") + " " + mutedStyle.Render("setup")
	panel := frameStyle.Width(s.frameWidth() - 2).Render(header + "\n\n" + s.form.View())

	if s.width == 0 {
		return panel
	}

	return lipgloss.Place(s.width, s.height, lipgloss.Center, lipgloss.Center, panel)
}

// frameWidth is the panel's full width including its border.
func (s *screen) frameWidth() int {
	if s.width == 0 || s.width > panelWidth {
		return panelWidth
	}

	return s.width
}

func (s *screen) innerWidth() int {
	return s.frameWidth() - 2 - 2*padX
}

// innerHeight leaves room for the border, the padding and the header.
func (s *screen) innerHeight() int {
	if s.height == 0 {
		return 0
	}

	return max(s.height-2-2*padY-2, 1)
}

// runSteps shows the steps in the framed screen, or as plain prompts when
// the output is not a terminal or ACCESSIBLE is set.
func runSteps(steps ...step) error {
	if accessible() {
		for _, st := range steps {
			form, err := st()
			if err != nil {
				return err
			}

			if form == nil {
				continue
			}

			if err := form.WithAccessible(true).Run(); err != nil {
				return err
			}
		}

		return nil
	}

	s := newScreen(steps...)

	if _, err := tea.NewProgram(s, tea.WithAltScreen()).Run(); err != nil {
		if errors.Is(err, tea.ErrInterrupted) {
			return huh.ErrUserAborted
		}

		return err
	}

	return s.err
}

func accessible() bool {
	if os.Getenv("ACCESSIBLE") != "" {
		return true
	}

	info, err := os.Stdout.Stat()

	return err != nil || info.Mode()&os.ModeCharDevice == 0
}
