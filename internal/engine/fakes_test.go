package engine

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/mhersson/contextmatrix-setup/internal/host"
	"github.com/mhersson/contextmatrix-setup/internal/layout"
	"github.com/mhersson/contextmatrix-setup/internal/run"
	"github.com/mhersson/contextmatrix-setup/internal/services"
)

// fakeGit pretends every repo is at heads[name]; Sync creates the checkout
// dir and a workflow-skills fixture for the server repo.
type fakeGit struct {
	heads   map[string]string
	changed map[string]bool // path -> PathChanged answer
	logs    map[string]string
}

func (g *fakeGit) Sync(_ context.Context, dir, _ string, _ io.Writer) (string, error) {
	name := filepath.Base(dir)
	if err := os.MkdirAll(filepath.Join(dir, "workflow-skills"), 0o755); err != nil {
		return "", err
	}

	if name == "contextmatrix" {
		_ = os.WriteFile(filepath.Join(dir, "workflow-skills", "create-plan.md"), []byte("plan "+g.heads[name]+"\n"), 0o644)
	}

	return g.heads[name], nil
}

func (g *fakeGit) Head(_ context.Context, dir string) (string, error) {
	return g.heads[filepath.Base(dir)], nil
}

func (g *fakeGit) Log(_ context.Context, dir, _, _ string) (string, error) {
	return g.logs[filepath.Base(dir)], nil
}

func (g *fakeGit) PathChanged(_ context.Context, _, _, _, path string) (bool, error) {
	return g.changed[path], nil
}

type fakeImages struct {
	built   []string
	removed []string
	fail    bool
}

func (fakeImages) Host(context.Context) string { return "" }

func (fakeImages) BridgeGateway(context.Context) string { return "172.17.0.1" }

func (f *fakeImages) Build(_ context.Context, _, repo, commit string, out io.Writer) (string, string, error) {
	if f.fail {
		return "", "", io.ErrUnexpectedEOF
	}

	tag := repo + "-worker:" + commit[:7]
	f.built = append(f.built, tag)
	_, _ = io.WriteString(out, "built "+tag+"\n")

	return tag, "sha256:" + commit, nil
}

func (f *fakeImages) RemoveTag(_ context.Context, tag string) error {
	f.removed = append(f.removed, tag)

	return nil
}

type harness struct {
	e      *Engine
	runner *run.Fake
	git    *fakeGit
	images *fakeImages
	out    *bytesBuffer
	home   string
}

type bytesBuffer struct{ b []byte }

func (b *bytesBuffer) Write(p []byte) (int, error) {
	b.b = append(b.b, p...)

	return len(p), nil
}

func (b *bytesBuffer) String() string { return string(b.b) }

func fixture(t *testing.T, name string) string {
	t.Helper()

	data, err := os.ReadFile(filepath.Join("testdata", name))
	require.NoError(t, err)

	return string(data)
}

func newHarness(t *testing.T, docker bool) *harness {
	t.Helper()

	home := t.TempDir()
	l := layout.New(home, nil)
	gobin := filepath.Join(home, "go", "bin")
	require.NoError(t, os.MkdirAll(gobin, 0o755))

	f := run.NewFake()
	f.On("make", "install").Return("", "", 0)
	f.On("systemctl").Return("", "", 0)
	f.On("journalctl").Return("", "", 0)
	f.On("loginctl").Return("", "", 0)
	f.On(filepath.Join(gobin, "contextmatrix"), "config", "defaults").Return(fixture(t, "server-defaults.yaml"), "", 0)
	f.On(filepath.Join(gobin, "contextmatrix-agent"), "config", "defaults").Return(fixture(t, "agent-defaults.yaml"), "", 0)
	f.On(filepath.Join(gobin, "contextmatrix-chat"), "config", "defaults").Return(fixture(t, "chat-defaults.yaml"), "", 0)
	f.On(filepath.Join(gobin, "contextmatrix"), "config", "validate").Return("ok\n", "", 0)
	f.On(filepath.Join(gobin, "contextmatrix-agent"), "config", "validate").Return("ok\n", "", 0)
	f.On(filepath.Join(gobin, "contextmatrix-chat"), "config", "validate").Return("ok\n", "", 0)

	git := &fakeGit{
		heads:   map[string]string{"contextmatrix": "aaaaaaa1111", "contextmatrix-agent": "bbbbbbb2222", "contextmatrix-chat": "ccccccc3333"},
		changed: map[string]bool{},
		logs:    map[string]string{},
	}
	img := &fakeImages{}
	out := &bytesBuffer{}

	e := &Engine{
		L:        l,
		Host:     host.Info{OS: "linux", Hostname: "box", GoBin: gobin, Docker: docker, ServiceManager: "systemd", Tools: map[string]string{}},
		R:        f,
		Git:      git,
		Images:   img,
		Services: services.New("systemd", f, l, 1000),
		Out:      out,
		Browser:  func(context.Context, string) error { return nil },
		RepoURL:  func(n string) string { return "file:///origin/" + n },
		Path:     "/usr/bin:/bin",
	}

	return &harness{e: e, runner: f, git: git, images: img, out: out, home: home}
}
