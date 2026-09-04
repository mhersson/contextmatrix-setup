package cli

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewRootCmd(t *testing.T) {
	cmd := NewRootCmd()
	require.NotNil(t, cmd)
	assert.Equal(t, "contextmatrix-setup", cmd.Use)

	names := map[string]bool{}
	for _, sub := range cmd.Commands() {
		names[sub.Name()] = true
	}

	for _, want := range []string{"install", "update", "status", "migrate", "uninstall"} {
		assert.True(t, names[want], "expected %s subcommand", want)
	}
}
