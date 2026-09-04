package run

import (
	"bytes"
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExecRunCapturesExit(t *testing.T) {
	var r Exec

	res, err := r.Run(context.Background(), Cmd{Name: "sh", Args: []string{"-c", "echo out; echo err >&2; exit 3"}})
	require.NoError(t, err)
	assert.Equal(t, "out\n", res.Stdout)
	assert.Equal(t, "err\n", res.Stderr)
	assert.Equal(t, 3, res.ExitCode)
}

func TestExecStreamReturnsExitError(t *testing.T) {
	var r Exec

	var out bytes.Buffer

	err := r.Stream(context.Background(), Cmd{Name: "sh", Args: []string{"-c", "echo building; exit 2"}}, &out)

	var exitErr *ExitError
	require.ErrorAs(t, err, &exitErr)
	assert.Equal(t, 2, exitErr.Code)
	assert.Contains(t, out.String(), "building")
	assert.Contains(t, exitErr.Tail, "building")
}

func TestExecLookPath(t *testing.T) {
	var r Exec

	_, ok := r.LookPath("sh")
	assert.True(t, ok)

	_, ok = r.LookPath("definitely-not-a-binary-xyz")
	assert.False(t, ok)
}

func TestFakeScriptsAndRecords(t *testing.T) {
	f := NewFake()
	f.On("git", "rev-parse", "HEAD").Return("abc1234\n", "", 0)
	f.On("git").Return("", "generic", 1)
	f.Has("git")

	res, err := f.Run(context.Background(), Cmd{Name: "git", Args: []string{"rev-parse", "HEAD"}, Dir: "/x"})
	require.NoError(t, err)
	assert.Equal(t, "abc1234\n", res.Stdout)

	res, _ = f.Run(context.Background(), Cmd{Name: "git", Args: []string{"status"}})
	assert.Equal(t, 1, res.ExitCode, "shorter prefix matches")

	res, _ = f.Run(context.Background(), Cmd{Name: "docker"})
	assert.Equal(t, 127, res.ExitCode, "unscripted command")

	_, ok := f.LookPath("git")
	assert.True(t, ok)
	_, ok = f.LookPath("docker")
	assert.False(t, ok)

	calls := f.Calls()
	require.Len(t, calls, 3)
	assert.Equal(t, "/x", calls[0].Dir)
}

func TestFakeLaterStubWinsOnEqualPrefix(t *testing.T) {
	f := NewFake()
	f.On("make", "install").Return("first", "", 0)
	f.On("make", "install").Return("", "boom", 2)

	res, err := f.Run(context.Background(), Cmd{Name: "make", Args: []string{"install"}})
	require.NoError(t, err)
	assert.Equal(t, 2, res.ExitCode)
	assert.Equal(t, "boom", res.Stderr)
}
