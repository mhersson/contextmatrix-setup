package host

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mhersson/contextmatrix-setup/internal/run"
)

func execIn(paths ...string) func(string) bool {
	return func(p string) bool { return slices.Contains(paths, p) }
}

func noShell(string) (string, bool) { return "", false }

func TestServicePathPerOS(t *testing.T) {
	assert.Equal(t, "/g:/usr/local/bin:/usr/bin:/bin", Info{OS: "linux", GoBin: "/g"}.ServicePath())
	assert.Equal(t, "/g:/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin", Info{OS: "darwin", GoBin: "/g"}.ServicePath())
}

func TestProbeSearchToolsUsesTheServicePath(t *testing.T) {
	s := ProbeSearchTools("/g:/usr/local/bin:/usr/bin:/bin", execIn("/usr/bin/rg", "/usr/local/bin/fd"), noShell)

	assert.Equal(t, SearchTools{RG: "/usr/bin/rg", FD: "/usr/local/bin/fd"}, s)
}

func TestProbeSearchToolsFollowsPathOrderAndFindsFdfind(t *testing.T) {
	s := ProbeSearchTools("/g:/usr/bin", execIn("/g/rg", "/usr/bin/rg", "/usr/bin/fdfind"), noShell)

	assert.Equal(t, "/g/rg", s.RG, "the first PATH entry wins, as it will for the service")
	assert.Equal(t, "/usr/bin/fdfind", s.FD, "Debian ships fd as fdfind")
}

func TestProbeSearchToolsReportsAnRgOnlyTheShellSees(t *testing.T) {
	shell := func(name string) (string, bool) { return "/home/u/.cargo/bin/" + name, name == "rg" }

	s := ProbeSearchTools("/g:/usr/bin", execIn(), shell)
	assert.Empty(t, s.RG)
	assert.Equal(t, "/home/u/.cargo/bin/rg", s.ShellRG)

	s = ProbeSearchTools("/g:/usr/bin", execIn("/usr/bin/rg"), shell)
	assert.Empty(t, s.ShellRG, "an rg the service finds needs no hint")
}

func TestIsExecutable(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "rg")
	plain := filepath.Join(dir, "fd")

	require.NoError(t, os.WriteFile(exe, []byte("#!/bin/sh\n"), 0o755))
	require.NoError(t, os.WriteFile(plain, []byte("text\n"), 0o644))

	assert.True(t, isExecutable(exe))
	assert.False(t, isExecutable(plain), "not executable")
	assert.False(t, isExecutable(dir), "a directory")
	assert.False(t, isExecutable(filepath.Join(dir, "absent")))
}

func TestDetectProbesTheServicePath(t *testing.T) {
	gobin := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(gobin, "rg"), []byte("#!/bin/sh\n"), 0o755))

	f := run.NewFake()
	f.Has("go")
	f.On("go", "env", "GOBIN").Return(gobin+"\n", "", 0)

	info, err := Detect(context.Background(), f, "linux", getenv(map[string]string{"HOME": "/home/u"}))
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(gobin, "rg"), info.Search.RG, "GOBIN leads the service PATH")
}
