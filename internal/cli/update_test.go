package cli

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mhersson/contextmatrix-setup/internal/engine"
	"github.com/mhersson/contextmatrix-setup/internal/layout"
	"github.com/mhersson/contextmatrix-setup/internal/selfupdate"
	"github.com/mhersson/contextmatrix-setup/internal/ui"
)

func TestHealthBudgetFromEnv(t *testing.T) {
	env := func(v string) func(string) string {
		return func(k string) string {
			if k == "CONTEXTMATRIX_SETUP_HEALTH_BUDGET" {
				return v
			}

			return ""
		}
	}

	var e engine.Engine

	require.NoError(t, healthFromEnv(&e, env("")))
	assert.False(t, e.SkipHealth)
	assert.Zero(t, e.HealthBudget, "unset keeps the default")

	require.NoError(t, healthFromEnv(&e, env("0")))
	assert.True(t, e.SkipHealth)

	e = engine.Engine{}
	require.NoError(t, healthFromEnv(&e, env("5s")))
	assert.False(t, e.SkipHealth)
	assert.Equal(t, 5*time.Second, e.HealthBudget)

	for _, bad := range []string{"soon", "-1s"} {
		require.Error(t, healthFromEnv(&engine.Engine{}, env(bad)), bad)
	}
}

func logLayout(t *testing.T) layout.Layout {
	t.Helper()

	return layout.Layout{CacheDir: t.TempDir()}
}

func noEnv(string) string { return "" }

func updateLogs(t *testing.T, l layout.Layout) []string {
	t.Helper()

	logs, err := filepath.Glob(filepath.Join(l.LogsDir(), "update-*.log"))
	require.NoError(t, err)

	return logs
}

func TestUpdateConsolePlainHasNoLog(t *testing.T) {
	l := logLayout(t)

	c, done, err := newUpdateConsole(&bytes.Buffer{}, ui.Mode{Plain: true}, l, noEnv, time.Now())
	require.NoError(t, err)

	defer done()

	assert.Empty(t, c.LogPath())
	assert.Empty(t, updateLogs(t, l))
}

func TestUpdateConsoleTTYOpensLog(t *testing.T) {
	t.Setenv("CONTEXTMATRIX_SETUP_LOG", "")

	l := logLayout(t)

	c, done, err := newUpdateConsole(&bytes.Buffer{}, ui.Mode{TTY: true, Width: 100}, l, noEnv, time.Now())
	require.NoError(t, err)

	defer done()

	logs := updateLogs(t, l)
	require.Len(t, logs, 1)
	assert.Equal(t, layout.Tilde(l, logs[0]), c.LogPath())
	assert.Equal(t, logs[0], os.Getenv("CONTEXTMATRIX_SETUP_LOG"))
}

func TestUpdateConsoleReusesParentLog(t *testing.T) {
	l := logLayout(t)
	parent := filepath.Join(t.TempDir(), "parent.log")
	require.NoError(t, os.WriteFile(parent, []byte("before\n"), 0o600))

	env := func(k string) string {
		switch k {
		case "CONTEXTMATRIX_SETUP_LOG":
			return parent
		case selfupdate.EnvMarker:
			return "1"
		}

		return ""
	}

	c, done, err := newUpdateConsole(&bytes.Buffer{}, ui.Mode{TTY: true}, l, env, time.Now())
	require.NoError(t, err)

	c.Println("child line")
	done()

	b, err := os.ReadFile(parent)
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(string(b), "before\n"))
	assert.Contains(t, string(b), "child line")
	assert.Empty(t, updateLogs(t, l))
}

func TestUpdateConsoleIgnoresExportedLogOutsideReexec(t *testing.T) {
	t.Setenv("CONTEXTMATRIX_SETUP_LOG", "")

	l := logLayout(t)
	exported := filepath.Join(t.TempDir(), "exported.log")

	env := func(k string) string {
		if k == "CONTEXTMATRIX_SETUP_LOG" {
			return exported
		}

		return ""
	}

	c, done, err := newUpdateConsole(&bytes.Buffer{}, ui.Mode{TTY: true}, l, env, time.Now())
	require.NoError(t, err)

	c.Println("fresh run")
	done()

	assert.NoFileExists(t, exported)
	assert.Len(t, updateLogs(t, l), 1)
}

func TestUpdateConfirmParsesAnswer(t *testing.T) {
	for in, want := range map[string]bool{
		"": true, "\n": true, "y\n": true, "Y\n": true, "yes\n": true, " YES \n": true,
		"n\n": false, "no thanks\n": false, "yes please\n": false, "y": true,
	} {
		var out bytes.Buffer

		c := ui.NewConsole(&out, ui.NewTheme(ui.Mode{Plain: true}), nil, "")

		assert.Equal(t, want, updateConfirm(c, strings.NewReader(in))("s"), "input %q", in)
		assert.NotEmpty(t, out.String(), "a prompt is written before the answer is read")
	}
}

// Without an Enter the terminal echoed no line break, so the prompt row is
// still open until the console ends it.
func TestUpdateConfirmEndsThePromptLineWithoutEnter(t *testing.T) {
	for in, breaks := range map[string]int{"": 1, "n": 1, "y\n": 0} {
		var out bytes.Buffer

		c := ui.NewConsole(&out, ui.NewTheme(ui.Mode{Plain: true}), nil, "")
		updateConfirm(c, strings.NewReader(in))("s")

		s := out.String()
		assert.Equal(t, breaks, strings.Count(s, "\n"), "input %q: %q", in, s)
		assert.Equal(t, breaks == 1, strings.HasSuffix(s, "\n"), "input %q: %q", in, s)
	}
}

func TestStyledOutDownsamplesOnlyColour(t *testing.T) {
	colour := ui.NewTheme(ui.Mode{TTY: true, Width: 100}).Accent("x")
	require.Contains(t, colour, "38;2;", "lipgloss renders 24-bit colour")

	const kept = "\r\x1b[1A\x1b[2K\x1b]8;;https://example.com\x07x\x1b]8;;\x07"

	var b bytes.Buffer

	_, err := io.WriteString(styledOut(&b, []string{"TTY_FORCE=1", "TERM=vt100"}), colour+kept)
	require.NoError(t, err)
	assert.NotContains(t, b.String(), "38;2;", "a 16-colour terminal gets 16 colours")
	assert.Contains(t, b.String(), "x\x1b[m"+kept, "cursor moves and links pass through")

	assert.Same(t, io.Writer(&b), styledOut(&b, []string{"TTY_FORCE=1"}), "a profile that would strip cursor moves is not used")
}
