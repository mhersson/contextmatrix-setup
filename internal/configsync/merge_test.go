package configsync

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func mustParse(t *testing.T, s string) Tree {
	t.Helper()

	tr, err := Parse([]byte(s))
	require.NoError(t, err)

	return tr
}

const schemaYAML = `
port: 8080
log_level: info
token_costs: {}
model_allowlist: []
boards:
  name: boards
  dir: ""
  git_auto_push: false
  git_remote_url: ""
auth:
  mode: multi
  db_path: ""
backends:
  agent:
    url: ""
    enabled: false
    aa_api_key: ""
`

func TestMergeFreshInstallUsesOpinionatedThenSchema(t *testing.T) {
	schema := mustParse(t, schemaYAML)
	opinionated := mustParse(t, "port: 18080\nauth:\n  db_path: ~/.contextmatrix/server/auth.db\n")

	out, dropped := Merge(schema, Tree{}, opinionated)

	assert.Empty(t, dropped)
	assert.Equal(t, 18080, out["port"])
	assert.Equal(t, "info", out["log_level"])
	assert.Equal(t, "~/.contextmatrix/server/auth.db", out["auth"].(Tree)["db_path"])
	assert.Equal(t, "multi", out["auth"].(Tree)["mode"])
	assert.Equal(t, false, out["backends"].(Tree)["agent"].(Tree)["enabled"])
}

func TestMergeUserValueWinsAndStaleKeysDrop(t *testing.T) {
	schema := mustParse(t, schemaYAML)
	user := mustParse(t, `
port: 4242
log_level: debug
old_key: 1
auth:
  mode: none
  legacy: true
backends:
  agent:
    url: http://localhost:19092
    enabled: true
`)
	opinionated := mustParse(t, "port: 18080\n")

	out, dropped := Merge(schema, user, opinionated)

	assert.Equal(t, 4242, out["port"], "user beats opinionated")
	assert.Equal(t, "none", out["auth"].(Tree)["mode"])
	assert.Equal(t, true, out["backends"].(Tree)["agent"].(Tree)["enabled"])
	assert.Empty(t, out["backends"].(Tree)["agent"].(Tree)["aa_api_key"], "new key added from schema")

	paths := map[string]any{}
	for _, d := range dropped {
		paths[d.Path] = d.Value
	}

	assert.Equal(t, 1, paths["old_key"])
	assert.Equal(t, true, paths["auth.legacy"])
	assert.Len(t, dropped, 2)

	_, hasOld := out["old_key"]
	assert.False(t, hasOld)
}

func TestMergeOpaqueNodesKeepUserContent(t *testing.T) {
	schema := mustParse(t, schemaYAML)
	user := mustParse(t, `
token_costs:
  my-model: {prompt: 0.1, completion: 0.2}
model_allowlist: [qwen, z-ai]
`)

	out, dropped := Merge(schema, user, Tree{})

	assert.Empty(t, dropped)
	assert.Equal(t, user["token_costs"], out["token_costs"])
	assert.Equal(t, user["model_allowlist"], out["model_allowlist"])

	out, _ = Merge(schema, Tree{}, Tree{})
	assert.Equal(t, Tree{}, out["token_costs"], "empty map stays a map")
	assert.Equal(t, []any{}, out["model_allowlist"], "empty list stays a list")
}

func TestMergeBoardsListForm(t *testing.T) {
	schema := mustParse(t, schemaYAML)
	user := mustParse(t, `
boards:
  - name: team
    dir: ~/b/team
    stale: 1
  - name: private
    dir: ~/b/private
    git_auto_push: true
`)
	opinionated := mustParse(t, "boards:\n  dir: ~/.contextmatrix/boards/default\n  git_auto_push: true\n")

	out, dropped := Merge(schema, user, opinionated)

	list, ok := out["boards"].([]any)
	require.True(t, ok, "list form preserved")
	require.Len(t, list, 2)

	first := list[0].(Tree)
	assert.Equal(t, "team", first["name"])
	assert.Equal(t, "~/b/team", first["dir"])
	assert.Equal(t, false, first["git_auto_push"], "opinionated values do not apply to list entries")
	assert.Empty(t, first["git_remote_url"], "missing key filled from schema")
	_, hasStale := first["stale"]
	assert.False(t, hasStale)

	require.Len(t, dropped, 1)
	assert.Equal(t, "boards[0].stale", dropped[0].Path)
}

func TestGetSetClone(t *testing.T) {
	tr := Tree{}
	Set(tr, "backends.agent.url", "http://x")

	v, ok := Get(tr, "backends.agent.url")
	require.True(t, ok)
	assert.Equal(t, "http://x", v)

	_, ok = Get(tr, "backends.chat.url")
	assert.False(t, ok)

	c := Clone(tr)
	Set(c, "backends.agent.url", "http://y")

	v, _ = Get(tr, "backends.agent.url")
	assert.Equal(t, "http://x", v, "clone is deep")
}

func TestParseEmptyAndLoadMissing(t *testing.T) {
	tr, err := Parse(nil)
	require.NoError(t, err)
	assert.Empty(t, tr)

	tr, found, err := LoadFile("/nonexistent/x.yaml")
	require.NoError(t, err)
	assert.False(t, found)
	assert.Empty(t, tr)
}
