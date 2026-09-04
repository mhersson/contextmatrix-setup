package selfupdate

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mhersson/contextmatrix-setup/internal/layout"
	"github.com/mhersson/contextmatrix-setup/internal/run"
	"github.com/mhersson/contextmatrix-setup/internal/state"
)

type fakeGit struct{ head string }

func (g fakeGit) Sync(_ context.Context, dir, _ string, _ io.Writer) (string, error) {
	_ = os.MkdirAll(dir, 0o755)

	return g.head, nil
}

func (g fakeGit) Head(context.Context, string) (string, error) { return g.head, nil }

func (fakeGit) Log(context.Context, string, string, string) (string, error) { return "", nil }

func (fakeGit) PathChanged(context.Context, string, string, string, string) (bool, error) {
	return false, nil
}

func setup(t *testing.T, recorded string) (Options, *run.Fake, string) {
	t.Helper()

	home := t.TempDir()
	l := layout.New(home, nil)
	exe := filepath.Join(home, "contextmatrix-setup")
	require.NoError(t, os.WriteFile(exe, []byte("v1"), 0o755))

	st := state.New()
	if recorded != "" {
		st.Repos["contextmatrix-setup"] = state.Repo{Commit: recorded}
	}

	require.NoError(t, st.Save(l.StateFile()))

	f := run.NewFake()
	f.On("go", "install").Return("", "", 0)

	o := Options{
		L: l, R: f, Git: fakeGit{head: "new0000000"}, Out: &bytes.Buffer{},
		Getenv: func(string) string { return "" },
		Self:   func() (string, error) { return exe, nil },
		Args:   []string{exe, "update"},
		URL:    "file:///origin/contextmatrix-setup",
	}

	return o, f, exe
}

func TestRunSkipsWhenMarkerSet(t *testing.T) {
	o, f, _ := setup(t, "old")
	o.Getenv = func(k string) string {
		if k == EnvMarker {
			return "1"
		}

		return ""
	}

	require.NoError(t, Run(context.Background(), o))
	assert.Empty(t, f.Calls())
}

func TestRunSkipsWhenUpToDate(t *testing.T) {
	o, f, _ := setup(t, "new0000000")
	require.NoError(t, Run(context.Background(), o))
	assert.Empty(t, f.Calls())
}

func TestRunRebuildsAndReexecsWhenBinaryChanged(t *testing.T) {
	o, f, exe := setup(t, "old0000000")

	var execArgv []string

	o.Exec = func(_ string, argv, env []string) error {
		execArgv = argv

		assert.Contains(t, env, EnvMarker+"=1")

		return nil
	}

	// go install is faked, so simulate the binary changing on disk.
	f.On("go", "install").Do(func() { _ = os.WriteFile(exe, []byte("v2"), 0o755) }).Return("", "", 0)
	o.R = f

	require.NoError(t, Run(context.Background(), o))
	assert.Equal(t, []string{exe, "update"}, execArgv)

	st, _, _ := state.Load(o.L.StateFile())
	assert.Equal(t, "new0000000", st.Repos["contextmatrix-setup"].Commit)
	assert.Equal(t, "install", f.Calls()[0].Args[0])
}

func TestRunFailedBuildIsLoggedNotFatal(t *testing.T) {
	o, f, _ := setup(t, "old0000000")
	f.On("go", "install").Return("", "boom", 1)

	var out bytes.Buffer

	o.Out = &out

	require.NoError(t, Run(context.Background(), o))
	assert.Contains(t, out.String(), "self-update failed")
}
