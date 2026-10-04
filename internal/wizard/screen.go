package wizard

import (
	"errors"
	"os"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/colorprofile"
)

// Panel geometry: the frame never grows past panelWidth columns, and the
// form wraps inside the padding.
const (
	panelWidth = 80
	padX       = 2
	padY       = 1
)

type styles struct {
	frame, brand, muted lipgloss.Style
}

func newStyles(dark bool) styles {
	pick := lipgloss.LightDark(dark)
	accent := pick(lipgloss.Color("#5A56E0"), lipgloss.Color("#7571F9"))
	muted := pick(lipgloss.Color("245"), lipgloss.Color("243"))

	return styles{
		frame: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(accent).
			Padding(padY, padX),
		brand: lipgloss.NewStyle().Foreground(accent).Bold(true),
		muted: lipgloss.NewStyle().Foreground(muted),
	}
}

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

	// dark starts true, the same fallback lipgloss uses when the terminal
	// does not answer the background colour query.
	dark   bool
	styles styles
}

func newScreen(steps ...step) *screen {
	return &screen{steps: steps, dark: true, styles: newStyles(true)}
}

func (s *screen) Init() tea.Cmd {
	return tea.Batch(tea.RequestBackgroundColor, s.advance())
}

// theme follows the screen's background rather than huh's own detection,
// which only reaches the group in focus when the answer arrives.
func (s *screen) theme() huh.Theme {
	return huh.ThemeFunc(func(bool) *huh.Styles { return charmTheme(s.dark) })
}

// charmTheme is huh's Charm theme with the option text and the blurred
// button put back the right way round: huh v2 swaps their light and dark
// colours, which leaves near-black option text on a dark terminal.
func charmTheme(dark bool) *huh.Styles {
	t := huh.ThemeCharm(dark)
	pick := lipgloss.LightDark(dark)
	normal := pick(lipgloss.Color("235"), lipgloss.Color("252"))
	buttonBg := pick(lipgloss.Color("252"), lipgloss.Color("237"))

	for _, f := range []*huh.FieldStyles{&t.Focused, &t.Blurred} {
		f.Option = f.Option.Foreground(normal)
		f.UnselectedOption = f.UnselectedOption.Foreground(normal)
		f.BlurredButton = f.BlurredButton.Foreground(normal).Background(buttonBg)
	}

	return t
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

		s.form = form.WithTheme(s.theme())

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
	switch m := msg.(type) {
	case tea.WindowSizeMsg:
		s.width, s.height = m.Width, m.Height
		msg = tea.WindowSizeMsg{Width: s.innerWidth(), Height: s.innerHeight()}
	case tea.BackgroundColorMsg:
		s.dark = m.IsDark()
		s.styles = newStyles(s.dark)

		if s.form != nil {
			// Reapplying the theme refreshes the help styles huh caches.
			s.form.WithTheme(s.theme())
		}
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

// View keeps the alternate screen on even between forms, so the terminal
// does not flash back to the shell while the next step is built.
func (s *screen) View() tea.View {
	v := tea.NewView(s.content())
	v.AltScreen = true

	return v
}

func (s *screen) content() string {
	if s.form == nil || s.form.State != huh.StateNormal {
		return ""
	}

	header := s.styles.brand.Render("ContextMatrix") + " " + s.styles.muted.Render("setup")
	panel := s.styles.frame.Width(s.frameWidth()).Render(header + "\n\n" + s.form.View())

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
		// huh writes styled prompts straight to its output, so it goes through
		// a writer that strips the colours a pipe or a plain terminal cannot show.
		out := colorprofile.NewWriter(os.Stdout, os.Environ())
		dark := out.Profile <= colorprofile.ASCII || lipgloss.HasDarkBackground(os.Stdin, os.Stdout)
		theme := huh.ThemeFunc(func(bool) *huh.Styles { return charmTheme(dark) })

		for _, st := range steps {
			form, err := st()
			if err != nil {
				return err
			}

			if form == nil {
				continue
			}

			if err := form.WithAccessible(true).WithOutput(out).WithTheme(theme).Run(); err != nil {
				return err
			}
		}

		return nil
	}

	s := newScreen(steps...)

	if _, err := tea.NewProgram(s).Run(); err != nil {
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
