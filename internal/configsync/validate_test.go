package configsync

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mhersson/contextmatrix-setup/internal/run"
)

func TestValidateRunsBinary(t *testing.T) {
	f := run.NewFake()
	f.On("/gobin/contextmatrix", "config", "validate", "/cfg/server.yaml").Return("ok\n", "", 0)
	f.On("/gobin/contextmatrix-agent", "config", "validate", "/cfg/agent.yaml").Return("", "error: invalid service config: api_key must be at least 32 characters\n", 1)

	require.NoError(t, Validate(context.Background(), f, "/gobin/contextmatrix", "/cfg/server.yaml"))

	err := Validate(context.Background(), f, "/gobin/contextmatrix-agent", "/cfg/agent.yaml")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "api_key must be at least 32 characters")
}

func TestCrossCheck(t *testing.T) {
	server := mustParse(t, `
port: 18080
mcp_api_key: MCP
backends:
  agent: {url: "http://localhost:19092", api_key: AGENT}
  chat: {url: "http://localhost:19093", api_key: CHAT}
`)
	agent := mustParse(t, "port: 19092\napi_key: AGENT\nmcp_api_key: MCP\ncontextmatrix_url: http://localhost:18080\n")
	chat := mustParse(t, "port: 19093\napi_key: CHAT\ncontextmatrix_url: http://localhost:18080\n")

	assert.Empty(t, CrossCheck(server, agent, chat))

	Set(agent, "api_key", "OTHER")
	Set(chat, "port", 19099)
	Set(server, "mcp_api_key", "MCP2")
	Set(agent, "contextmatrix_url", "http://localhost:1")

	warnings := CrossCheck(server, agent, chat)
	require.Len(t, warnings, 4)
	assert.Contains(t, warnings[0], "agent.yaml api_key")
	assert.Contains(t, warnings[0], "server.yaml backends.agent.api_key")
}

func TestPortOf(t *testing.T) {
	assert.Equal(t, "19092", portOf("http://localhost:19092"))
	assert.Equal(t, "443", portOf("https://x"))
	assert.Equal(t, "80", portOf("http://x"))
	assert.Equal(t, "19092", portOf("localhost:19092"), "scheme-less host:port")
	assert.Empty(t, portOf("garbage"))
}
