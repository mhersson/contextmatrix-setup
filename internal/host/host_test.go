package host

import (
	"context"
	"net"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mhersson/contextmatrix-setup/internal/run"
)

func getenv(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestDetectLinuxWithEverything(t *testing.T) {
	f := run.NewFake()
	for _, tool := range []string{"git", "go", "make", "node", "npm", "docker", "systemctl"} {
		f.Has(tool)
	}

	f.On("go", "env", "GOBIN").Return("\n", "", 0)
	f.On("go", "env", "GOPATH").Return("/home/u/go\n", "", 0)
	f.On("docker", "info").Return("27.1\n", "", 0)
	f.On("systemctl", "--user", "is-system-running").Return("running\n", "", 0)
	f.On("hostname").Return("box\n", "", 0)

	info, err := Detect(context.Background(), f, "linux", getenv(map[string]string{"HOME": "/home/u"}))
	require.NoError(t, err)

	assert.Equal(t, "linux", info.OS)
	assert.Equal(t, "/home/u/go/bin", info.GoBin)
	assert.Equal(t, "/home/u/go/bin/contextmatrix", info.Binary("contextmatrix"))
	assert.True(t, info.Docker)
	assert.Equal(t, "systemd", info.ServiceManager)
	assert.Empty(t, info.Missing())
	assert.NotEmpty(t, info.Hostname)
}

func TestDetectMissingToolsAndNoDocker(t *testing.T) {
	f := run.NewFake()
	f.Has("git")
	f.Has("go")
	f.Has("docker")
	f.On("go", "env", "GOBIN").Return("/gobin\n", "", 0)
	f.On("docker", "info").Return("", "Cannot connect to the Docker daemon", 1)

	info, err := Detect(context.Background(), f, "darwin", getenv(map[string]string{"HOME": "/Users/u"}))
	require.NoError(t, err)

	assert.Equal(t, "/gobin", info.GoBin)
	assert.False(t, info.Docker, "docker CLI without a daemon counts as absent")
	assert.Equal(t, []string{"make", "node", "npm"}, info.Missing())
	assert.Equal(t, "none", info.ServiceManager, "no launchctl on PATH")
}

func TestDetectDarwinLaunchd(t *testing.T) {
	f := run.NewFake()
	f.Has("launchctl")
	f.Has("go")
	f.On("go", "env", "GOBIN").Return("/gobin\n", "", 0)

	info, err := Detect(context.Background(), f, "darwin", getenv(map[string]string{"HOME": "/Users/u"}))
	require.NoError(t, err)
	assert.Equal(t, "launchd", info.ServiceManager)
}

func TestDetectNeedsGo(t *testing.T) {
	f := run.NewFake()

	_, err := Detect(context.Background(), f, "linux", getenv(map[string]string{"HOME": "/home/u"}))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "go")
}

func TestPortFree(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	defer ln.Close()

	port := ln.Addr().(*net.TCPAddr).Port
	assert.False(t, PortFree(port))

	ln.Close()
	assert.True(t, PortFree(port))
}

func TestOpenBrowserPicksPlatformCommand(t *testing.T) {
	f := run.NewFake()
	f.On("xdg-open").Return("", "", 0)
	f.On("open").Return("", "", 0)

	require.NoError(t, OpenBrowser(context.Background(), f, "linux", "http://localhost:18080"))
	require.NoError(t, OpenBrowser(context.Background(), f, "darwin", "http://localhost:18080"))

	calls := f.Calls()
	require.Len(t, calls, 2)
	assert.Equal(t, "xdg-open", calls[0].Name)
	assert.Equal(t, "open", calls[1].Name)
}
