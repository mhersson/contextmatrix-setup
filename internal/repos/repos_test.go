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

	changes, err := g.Changes(context.Background(), dir, head1, head2)
	require.NoError(t, err)
	require.Len(t, changes, 1)
	assert.Equal(t, head2, changes[0].SHA)
	assert.Equal(t, "add workflow-skills/a.md", changes[0].Title)
	assert.Equal(t, 1, changes[0].Commits)
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

func TestParseFirstParent(t *testing.T) {
	rec := func(sha, parents, author, subject, body string) string {
		return sha + "\x1f" + parents + "\x1f" + author + "\x1f" + subject + "\x1f" + body + "\x1e\n"
	}
	raw := rec("a1", "p1 p2", "alice", "Merge pull request #52 from o/docs/x", "\ndocs(design): name it\n") +
		rec("b2", "q1 q2", "bob", "Merge pull request #9 from o/dependabot/go_modules/x", "\nbuild(deps): bump x\n") +
		rec("c3", "r1", "dependabot[bot]", "fix(web): y (#17)", "") +
		rec("d4", "s1", "carol", "chore: z", "") +
		rec("e5", "t1 t2", "dan", "Merge pull request #3 from o/feat/y", "")

	got := ParseFirstParent(raw)
	require.Len(t, got, 5)
	assert.Equal(t, "Merge pull request #3 from o/feat/y", got[4].Title)
	assert.Equal(t, Change{SHA: "a1", Parents: []string{"p1", "p2"}, PR: 52, Title: "docs(design): name it", Commits: 1}, got[0])
	assert.Equal(t, 9, got[1].PR)
	assert.True(t, got[1].Dependabot)
	assert.Equal(t, "build(deps): bump x", got[1].Title)
	assert.Equal(t, 17, got[2].PR)
	assert.Equal(t, "fix(web): y", got[2].Title)
	assert.True(t, got[2].Dependabot)
	assert.Equal(t, 0, got[3].PR)
	assert.Equal(t, "chore: z", got[3].Title)
	assert.False(t, got[3].Dependabot)
}

func TestWebBase(t *testing.T) {
	for in, want := range map[string]string{
		"https://github.com/o/r":       "https://github.com/o/r",
		"https://github.com/o/r.git":   "https://github.com/o/r",
		"ssh://git@github.com/o/r.git": "https://github.com/o/r",
		"ssh://github.com/o/r.git":     "https://github.com/o/r",
		"git@github.com:o/r.git":       "https://github.com/o/r",
		"file:///origin/x":             "",
		"https://gitlab.com/o/r.git":   "",
		"/tmp/origin.git":              "",
	} {
		assert.Equal(t, want, WebBase(in), in)
	}
}

func TestChangesCountsMergeCommits(t *testing.T) {
	f := run.NewFake()
	f.On("git", "log").Return("m1\x1fp1 p2\x1fal\x1fMerge pull request #5 from o/feat/x\x1f\nfeat: x\n\x1e\n", "", 0)
	f.On("git", "rev-list", "--count", "p1..p2").Return("3\n", "", 0)

	got, err := Git{R: f}.Changes(context.Background(), "/d", "a", "b")
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, 3, got[0].Commits)
}

func TestChangesFailsOnUnreachableRange(t *testing.T) {
	f := run.NewFake()
	f.On("git", "log").Return("", "bad revision", 128)

	_, err := Git{R: f}.Changes(context.Background(), "/d", "a", "b")
	assert.ErrorContains(t, err, "bad revision")
}

func TestChangesKeepsListWhenCountFails(t *testing.T) {
	f := run.NewFake()
	f.On("git", "log").Return("m1\x1fp1 p2\x1fal\x1fMerge pull request #5 from o/feat/x\x1f\nfeat: x\n\x1e\n", "", 0)
	f.On("git", "rev-list").Return("", "bad object", 128)

	got, err := Git{R: f}.Changes(context.Background(), "/d", "a", "b")
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, 1, got[0].Commits)
}
