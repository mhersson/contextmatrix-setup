package images

import (
	"bytes"
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mhersson/contextmatrix-setup/internal/run"
)

func TestFamilyAndTag(t *testing.T) {
	assert.Equal(t, "contextmatrix-agent-worker", Family("contextmatrix-agent"))
	assert.Equal(t, "contextmatrix-chat-worker:abc1234", Tag("contextmatrix-chat", "abc1234567"))
	assert.Empty(t, Family("contextmatrix"))
}

func TestHostAndGateway(t *testing.T) {
	f := run.NewFake()
	f.On("docker", "context", "inspect").Return("unix:///Users/u/.docker/run/docker.sock\n", "", 0)
	f.On("docker", "network", "inspect", "bridge").Return("172.18.0.1\n", "", 0)

	d := Docker{R: f}
	assert.Equal(t, "unix:///Users/u/.docker/run/docker.sock", d.Host(context.Background()))
	assert.Equal(t, "172.18.0.1", d.BridgeGateway(context.Background()))

	f = run.NewFake()
	f.On("docker", "context", "inspect").Return("unix:///var/run/docker.sock\n", "", 0)
	f.On("docker", "network", "inspect", "bridge").Return("", "no such network", 1)

	d = Docker{R: f}
	assert.Empty(t, d.Host(context.Background()), "default socket needs no DOCKER_HOST")
	assert.Equal(t, "172.17.0.1", d.BridgeGateway(context.Background()))
}

func TestVariants(t *testing.T) {
	assert.Equal(t, []string{"go-node", "python", "rust"}, Variants)
	assert.Equal(t, "contextmatrix-chat-worker:python", VariantTag("contextmatrix-chat", "python"))
}

func TestBuildTagsAndInspects(t *testing.T) {
	f := run.NewFake()
	f.On("make", "docker-worker").Return("Successfully built\n", "", 0)
	f.On("make", "docker-worker-variants").Return("variants built\n", "", 0)
	f.On("docker", "tag").Return("", "", 0)
	f.On("docker", "image", "inspect").Return("sha256:feedface\n", "", 0)
	f.On("docker", "image", "inspect", "--format", "{{.Id}}", "contextmatrix-agent-worker:go-node").Return("sha256:g0\n", "", 0)
	f.On("docker", "image", "inspect", "--format", "{{.Id}}", "contextmatrix-agent-worker:python").Return("sha256:py\n", "", 0)
	f.On("docker", "image", "inspect", "--format", "{{.Id}}", "contextmatrix-agent-worker:rust").Return("sha256:rs\n", "", 0)

	var out bytes.Buffer

	b, err := Docker{R: f}.Build(context.Background(), "/cache/src/contextmatrix-agent", "contextmatrix-agent", "abc1234567", &out)
	require.NoError(t, err)
	assert.Equal(t, "contextmatrix-agent-worker:abc1234", b.Tag)
	assert.Equal(t, "sha256:feedface", b.ID)
	assert.Equal(t, map[string]string{"go-node": "sha256:g0", "python": "sha256:py", "rust": "sha256:rs"}, b.Variants)
	assert.Contains(t, out.String(), "Successfully built")
	assert.Contains(t, out.String(), "variants built")

	calls := f.Calls()
	require.Len(t, calls, 7)
	assert.Equal(t, "/cache/src/contextmatrix-agent", calls[0].Dir)
	assert.Equal(t, []string{"docker-worker"}, calls[0].Args)
	assert.Equal(t, "/cache/src/contextmatrix-agent", calls[1].Dir)
	assert.Equal(t, []string{"docker-worker-variants"}, calls[1].Args)
	assert.Equal(t, []string{"tag", "contextmatrix-agent-worker:dev", "contextmatrix-agent-worker:abc1234"}, calls[2].Args)
}

func TestBuildFailureKeepsOldImage(t *testing.T) {
	f := run.NewFake()
	f.On("make", "docker-worker").Return("", "step 7 failed", 2)

	_, err := Docker{R: f}.Build(context.Background(), "/x", "contextmatrix-chat", "abc", &bytes.Buffer{})
	require.Error(t, err)
	assert.Len(t, f.Calls(), 1, "no tag after a failed build")
}

func TestVariantFailureKeepsOldImage(t *testing.T) {
	f := run.NewFake()
	f.On("make", "docker-worker").Return("ok\n", "", 0)
	f.On("make", "docker-worker-variants").Return("", "rust stage failed", 2)

	_, err := Docker{R: f}.Build(context.Background(), "/x", "contextmatrix-chat", "abc", &bytes.Buffer{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "variants")
	assert.Len(t, f.Calls(), 2, "no tag after a failed variant build")
}

func TestRemoveImage(t *testing.T) {
	f := run.NewFake()
	f.On("docker", "rmi").Return("", "", 0)

	require.NoError(t, Docker{R: f}.RemoveImage(context.Background(), "sha256:old"))
	assert.Equal(t, []string{"rmi", "sha256:old"}, f.Calls()[0].Args)
}
