package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mhersson/contextmatrix-setup/internal/engine"
	"github.com/mhersson/contextmatrix-setup/internal/layout"
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

func TestUpdateConsolePlainHasNoLog(t *testing.T) {
	l := logLayout(t)

	c, done, err := newUpdateConsole(&bytes.Buffer{}, ui.Mode{Plain: true}, l, noEnv, time.Now())
	require.NoError(t, err)

	defer done()

	assert.Empty(t, c.LogPath())
	assert.NoFileExists(t, l.LogsDir())
}

func TestUpdateConsoleTTYOpensLog(t *testing.T) {
	t.Setenv("CONTEXTMATRIX_SETUP_LOG", "")

	l := logLayout(t)

	c, done, err := newUpdateConsole(&bytes.Buffer{}, ui.Mode{TTY: true, Width: 100}, l, noEnv, time.Now())
	require.NoError(t, err)

	defer done()

	logs, err := filepath.Glob(filepath.Join(l.LogsDir(), "update-*.log"))
	require.NoError(t, err)
	require.Len(t, logs, 1)
	assert.Equal(t, layout.Tilde(l, logs[0]), c.LogPath())
	assert.Equal(t, logs[0], os.Getenv("CONTEXTMATRIX_SETUP_LOG"))
}

func TestUpdateConsoleReusesParentLog(t *testing.T) {
	l := logLayout(t)
	parent := filepath.Join(t.TempDir(), "parent.log")
	require.NoError(t, os.WriteFile(parent, []byte("before\n"), 0o600))

	env := func(k string) string {
		if k == "CONTEXTMATRIX_SETUP_LOG" {
			return parent
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
	assert.NoFileExists(t, l.LogsDir())
}

func TestUpdateConfirmDefaultsYes(t *testing.T) {
	for in, want := range map[string]bool{"\n": true, "": true, "n\n": false, "y\n": true} {
		var out bytes.Buffer

		c := ui.NewConsole(&out, ui.NewTheme(ui.Mode{Plain: true}), nil, "")

		assert.Equal(t, want, updateConfirm(c, strings.NewReader(in))("s"), "input %q", in)
		assert.Contains(t, out.String(), "[Y/n]")
		assert.False(t, strings.HasSuffix(out.String(), "\n"), "prompt stays on the answer row")
	}
}
