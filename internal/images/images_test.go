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

func TestBuildTagsAndInspects(t *testing.T) {
	f := run.NewFake()
	f.On("make", "docker-worker").Return("Successfully built\n", "", 0)
	f.On("docker", "tag").Return("", "", 0)
	f.On("docker", "image", "inspect").Return("sha256:feedface\n", "", 0)

	var out bytes.Buffer

	tag, id, err := Docker{R: f}.Build(context.Background(), "/cache/src/contextmatrix-agent", "contextmatrix-agent", "abc1234567", &out)
	require.NoError(t, err)
	assert.Equal(t, "contextmatrix-agent-worker:abc1234", tag)
	assert.Equal(t, "sha256:feedface", id)
	assert.Contains(t, out.String(), "Successfully built")

	calls := f.Calls()
	require.Len(t, calls, 3)
	assert.Equal(t, "/cache/src/contextmatrix-agent", calls[0].Dir)
	assert.Equal(t, []string{"tag", "contextmatrix-agent-worker:dev", "contextmatrix-agent-worker:abc1234"}, calls[1].Args)
}

func TestBuildFailureKeepsOldImage(t *testing.T) {
	f := run.NewFake()
	f.On("make", "docker-worker").Return("", "step 7 failed", 2)

	_, _, err := Docker{R: f}.Build(context.Background(), "/x", "contextmatrix-chat", "abc", &bytes.Buffer{})
	require.Error(t, err)
	assert.Len(t, f.Calls(), 1, "no tag after a failed build")
}

func TestRemoveTag(t *testing.T) {
	f := run.NewFake()
	f.On("docker", "rmi").Return("", "", 0)

	require.NoError(t, Docker{R: f}.RemoveTag(context.Background(), "contextmatrix-agent-worker:old1234"))
	assert.Equal(t, []string{"rmi", "contextmatrix-agent-worker:old1234"}, f.Calls()[0].Args)
}
