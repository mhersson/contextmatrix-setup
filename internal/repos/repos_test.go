package repos

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mhersson/contextmatrix-setup/internal/run"
)

// origin creates a bare repo with a main branch and returns its URL plus a
// commit helper that writes a file into a work clone and pushes.
func origin(t *testing.T) (string, func(name, content string) string) {
	t.Helper()

	root := t.TempDir()
	bare := filepath.Join(root, "origin.git")
	work := filepath.Join(root, "work")

	// The host's global/system config and any GIT_* variable (GIT_DIR,
	// forced commit.gpgsign, a global hooksPath, ...) must never reach this
	// fixture, so the environment is rebuilt from scratch for every call.
	env := []string{"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1"}

	for _, e := range os.Environ() {
		if !strings.HasPrefix(e, "GIT_") {
			env = append(env, e)
		}
	}

	git := func(dir string, args ...string) string {
		t.Helper()

		// git is fixed and the test drives its own args in a temp dir.
		cmd := exec.Command("git", append([]string{ //nolint:gosec
			"-c", "user.name=t",
			"-c", "user.email=t@t",
			"-c", "init.defaultBranch=main",
			"-c", "commit.gpgsign=false",
		}, args...)...)
		cmd.Dir = dir
		cmd.Env = env

		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "git %v: %s", args, out)

		return string(out)
	}

	require.NoError(t, os.MkdirAll(bare, 0o755))
	git(bare, "init", "--bare", "--initial-branch=main")
	git(root, "clone", bare, work)

	commit := func(name, content string) string {
		require.NoError(t, os.MkdirAll(filepath.Dir(filepath.Join(work, name)), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(work, name), []byte(content), 0o644))
		git(work, "add", name)
		git(work, "commit", "-q", "-m", "add "+name)
		git(work, "push", "-q", "origin", "HEAD:main")

		return git(work, "rev-parse", "HEAD")[:40]
	}

	commit("README.md", "hello\n")

	return bare, commit
}

func TestSyncClonesThenUpdates(t *testing.T) {
	url, commit := origin(t)
	g := Git{R: run.Exec{}}
	dir := filepath.Join(t.TempDir(), "src", "repo")

	var out bytes.Buffer

	head1, err := g.Sync(context.Background(), dir, url, &out)
	require.NoError(t, err)
	assert.Len(t, head1, 40)

	got, err := g.Head(context.Background(), dir)
	require.NoError(t, err)
	assert.Equal(t, head1, got)

	head2 := commit("workflow-skills/a.md", "x\n")

	got, err = g.Sync(context.Background(), dir, url, &out)
	require.NoError(t, err)
	assert.Equal(t, head2, got)

	changed, err := g.PathChanged(context.Background(), dir, head1, head2, "workflow-skills")
	require.NoError(t, err)
	assert.True(t, changed)

	changed, err = g.PathChanged(context.Background(), dir, head1, head2, "docs")
	require.NoError(t, err)
	assert.False(t, changed)

	log, err := g.Log(context.Background(), dir, head1, head2)
	require.NoError(t, err)
	assert.Contains(t, log, "add workflow-skills/a.md")
}

func TestSyncDiscardsLocalChanges(t *testing.T) {
	url, _ := origin(t)
	g := Git{R: run.Exec{}}
	dir := filepath.Join(t.TempDir(), "repo")

	_, err := g.Sync(context.Background(), dir, url, &bytes.Buffer{})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "README.md"), []byte("dirty\n"), 0o644))

	_, err = g.Sync(context.Background(), dir, url, &bytes.Buffer{})
	require.NoError(t, err)

	data, err := os.ReadFile(filepath.Join(dir, "README.md"))
	require.NoError(t, err)
	assert.Equal(t, "hello\n", string(data))
}

func TestURLAndShort(t *testing.T) {
	assert.Equal(t, "https://github.com/mhersson/contextmatrix-agent.git", URL(Agent))
	assert.Equal(t, "abc1234", Short("abc1234567890"))
	assert.Equal(t, "abc", Short("abc"))
}
