//go:build integration

package integration

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// env is one isolated machine: HOME, GOBIN, a stub bin dir first on PATH,
// local bare origins for the four repos, and a log the stubs append to.
type env struct {
	t       *testing.T
	home    string
	gobin   string
	stubs   string
	origins string
	log     string
	setup   string // path to the built contextmatrix-setup binary
}

func newEnv(t *testing.T) *env {
	t.Helper()

	root := t.TempDir()
	e := &env{
		t:       t,
		home:    filepath.Join(root, "home"),
		gobin:   filepath.Join(root, "gobin"),
		stubs:   filepath.Join(root, "stubs"),
		origins: filepath.Join(root, "origins"),
		log:     filepath.Join(root, "stub.log"),
	}

	for _, d := range []string{e.home, e.gobin, e.stubs, e.origins} {
		require.NoError(t, os.MkdirAll(d, 0o755))
	}

	e.setup = filepath.Join(root, "contextmatrix-setup")

	// The output path is this test's own temp dir; the package is fixed.
	build := exec.Command("go", "build", "-o", e.setup, "../..") //nolint:gosec

	out, err := build.CombinedOutput()
	require.NoError(t, err, string(out))

	fixtures, _ := filepath.Abs("testdata")

	e.writeStub("make", `#!/bin/sh
case "$1" in
  install)
    name=$(basename "$PWD")
    cat > "$GOBIN/$name" <<EOF
#!/bin/sh
if [ "\$1" = "config" ] && [ "\$2" = "defaults" ]; then cat "`+fixtures+`/$(echo $name | sed 's/^contextmatrix$/server/; s/^contextmatrix-//')-defaults.yaml"; exit 0; fi
if [ "\$1" = "config" ] && [ "\$2" = "validate" ]; then grep -q '^log_level: invalid' "\$3" && { echo "error: log_level must be debug|info|warn|error" >&2; exit 1; }; echo "\$3: ok"; exit 0; fi
sleep 3600
EOF
    chmod +x "$GOBIN/$name"
    echo "make install $name" >> "$STUB_LOG" ;;
  docker-worker) echo "make docker-worker $(basename "$PWD") $(git rev-parse --short HEAD)" >> "$STUB_LOG" ;;
  *) echo "make: unknown target $1" >&2; exit 2 ;;
esac
`)

	e.writeStub("docker", `#!/bin/sh
echo "docker $*" >> "$STUB_LOG"
case "$1" in
  info) echo 27.0 ;;
  context) echo unix:///var/run/docker.sock ;;
  network) echo 172.17.0.1 ;;
  image) echo "sha256:$5" ;;
  tag|rmi) ;;
esac
exit 0
`)

	e.writeStub("systemctl", `#!/bin/sh
echo "systemctl $*" >> "$STUB_LOG"
[ "$2" = "is-system-running" ] && echo running
[ "$2" = "is-active" ] && echo active
exit 0
`)

	e.writeStub("journalctl", `#!/bin/sh
echo "journalctl $*" >> "$STUB_LOG"
case "$*" in *" -f "*) echo 'msg="auth: bootstrap link" path=/auth/token/stub-token' ;; esac
exit 0
`)

	e.writeStub("loginctl", "#!/bin/sh\nexit 0\n")
	e.writeStub("xdg-open", "#!/bin/sh\necho \"xdg-open $1\" >> \"$STUB_LOG\"\nexit 0\n")

	for _, name := range []string{"contextmatrix-setup", "contextmatrix", "contextmatrix-agent", "contextmatrix-chat"} {
		e.newOrigin(name)
	}

	return e
}

func (e *env) writeStub(name, body string) {
	e.t.Helper()
	require.NoError(e.t, os.WriteFile(filepath.Join(e.stubs, name), []byte(body), 0o755))
}

// gitEnv rebuilds the environment for the fixture repos from scratch: every
// GIT_* variable of the host (GIT_DIR, a forced hooksPath, ...) is dropped
// and both config files are pinned away, so nothing outside this test can
// change what the origins look like.
func gitEnv() []string {
	env := []string{"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1"}

	for _, v := range os.Environ() {
		if !strings.HasPrefix(v, "GIT_") {
			env = append(env, v)
		}
	}

	return env
}

func (e *env) git(dir string, args ...string) string {
	e.t.Helper()

	// git is fixed and the test drives its own args in a temp dir.
	cmd := exec.Command("git", append([]string{ //nolint:gosec
		"-c", "user.name=t",
		"-c", "user.email=t@t",
		"-c", "init.defaultBranch=main",
		"-c", "commit.gpgsign=false",
	}, args...)...)
	cmd.Dir = dir
	cmd.Env = gitEnv()

	out, err := cmd.CombinedOutput()
	require.NoError(e.t, err, "git %v: %s", args, out)

	return strings.TrimSpace(string(out))
}

func (e *env) newOrigin(name string) {
	bare := filepath.Join(e.origins, name+".git")
	work := filepath.Join(e.origins, name+"-work")

	require.NoError(e.t, os.MkdirAll(bare, 0o755))
	e.git(bare, "init", "--bare", "--initial-branch=main")
	e.git(e.origins, "clone", "-q", bare, work)
	require.NoError(e.t, os.WriteFile(filepath.Join(work, "Makefile"), []byte("install:\n\tmake install\n"), 0o644))

	if name == "contextmatrix" {
		require.NoError(e.t, os.MkdirAll(filepath.Join(work, "workflow-skills"), 0o755))
		require.NoError(e.t, os.WriteFile(filepath.Join(work, "workflow-skills", "create-plan.md"), []byte("v1\n"), 0o644))
	}

	e.git(work, "add", ".")
	e.git(work, "commit", "-q", "-m", "init")
	e.git(work, "push", "-q", "origin", "HEAD:main")
}

// commit adds a file to a repo's origin and returns the new HEAD.
func (e *env) commit(name, file, content string) string {
	work := filepath.Join(e.origins, name+"-work")

	require.NoError(e.t, os.MkdirAll(filepath.Dir(filepath.Join(work, file)), 0o755))
	require.NoError(e.t, os.WriteFile(filepath.Join(work, file), []byte(content), 0o644))
	e.git(work, "add", file)
	e.git(work, "commit", "-q", "-m", "change "+file)
	e.git(work, "push", "-q", "origin", "HEAD:main")

	return e.git(work, "rev-parse", "HEAD")
}

func (e *env) run(args ...string) (string, error) {
	// The binary under test is built by this test into its own temp dir.
	cmd := exec.Command(e.setup, args...) //nolint:gosec
	cmd.Env = []string{
		"HOME=" + e.home,
		"GOBIN=" + e.gobin,
		"PATH=" + e.stubs + ":" + os.Getenv("PATH"),
		"STUB_LOG=" + e.log,
		"CONTEXTMATRIX_SETUP_REPO_BASE=" + e.origins,
		"CONTEXTMATRIX_SETUP_REEXEC=1",
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_CONFIG_NOSYSTEM=1",
	}

	out, err := cmd.CombinedOutput()

	return string(out), err
}

func (e *env) stubLog() string {
	data, _ := os.ReadFile(e.log)

	return string(data)
}

func (e *env) read(rel string) string {
	data, err := os.ReadFile(filepath.Join(e.home, rel))
	require.NoError(e.t, err, rel)

	return string(data)
}
