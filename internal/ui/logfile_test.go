package ui

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOpenLogKeepsNewestTen(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	base := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)

	var old []string

	for i := range 12 {
		p := filepath.Join(dir, "update-"+base.Add(time.Duration(i)*time.Hour).Format("20060102-150405")+".log")
		require.NoError(t, os.WriteFile(p, []byte("old\n"), 0o600))

		old = append(old, p)
	}

	other := filepath.Join(dir, "notes.txt")
	require.NoError(t, os.WriteFile(other, nil, 0o600))

	f, err := OpenLog(dir, base.Add(24*time.Hour), 10)
	require.NoError(t, err)
	t.Cleanup(func() { _ = f.Close() })

	logs, err := filepath.Glob(filepath.Join(dir, "update-*.log"))
	require.NoError(t, err)
	assert.Len(t, logs, 10)
	assert.Contains(t, logs, f.Name())

	for _, gone := range old[:3] {
		assert.NoFileExists(t, gone)
	}

	assert.FileExists(t, old[3])
	assert.FileExists(t, other)
}

func TestOpenLogCreatesPrivateFile(t *testing.T) {
	t.Parallel()

	dir := filepath.Join(t.TempDir(), "cache", "logs")

	f, err := OpenLog(dir, time.Date(2026, 10, 24, 9, 33, 41, 0, time.UTC), 10)
	require.NoError(t, err)
	t.Cleanup(func() { _ = f.Close() })

	assert.Equal(t, "update-20261024-093341.log", filepath.Base(f.Name()), "pruning sorts these names")

	di, err := os.Stat(dir)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o700), di.Mode().Perm())

	fi, err := os.Stat(f.Name())
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), fi.Mode().Perm())
}

func TestAppendLogAppends(t *testing.T) {
	t.Parallel()

	p := filepath.Join(t.TempDir(), "update.log")
	require.NoError(t, os.WriteFile(p, []byte("parent\n"), 0o600))

	f, err := AppendLog(p)
	require.NoError(t, err)
	_, err = f.WriteString("child\n")
	require.NoError(t, err)
	require.NoError(t, f.Close())

	data, err := os.ReadFile(p)
	require.NoError(t, err)
	assert.Equal(t, "parent\nchild\n", string(data))
}

func TestOpenLogKeepsLogWhenPruneFails(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}

	t.Parallel()

	dir := t.TempDir()
	stale := filepath.Join(dir, "update-20261001-120000.log")
	require.NoError(t, os.WriteFile(stale, nil, 0o600))
	// The new log already exists, so it opens in a read-only dir where
	// nothing can be removed.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "update-20261024-093341.log"), nil, 0o600))
	require.NoError(t, os.Chmod(dir, 0o500))
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })

	f, err := OpenLog(dir, time.Date(2026, 10, 24, 9, 33, 41, 0, time.UTC), 1)
	require.NoError(t, err)
	t.Cleanup(func() { _ = f.Close() })

	_, err = f.WriteString("still logging\n")
	require.NoError(t, err)
	assert.FileExists(t, stale, "the failed prune left the old log")
}
