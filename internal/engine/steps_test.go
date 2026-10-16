package engine

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mhersson/contextmatrix-setup/internal/configsync"
	"github.com/mhersson/contextmatrix-setup/internal/layout"
	"github.com/mhersson/contextmatrix-setup/internal/repos"
)

func pathsEngine() *Engine {
	return &Engine{L: layout.New("/home/u", nil)}
}

// An empty directory value must never reach ReadWritePaths: the root would
// cancel ProtectSystem=strict for the unit.
func TestServerPathsSkipsEmptyDirectories(t *testing.T) {
	cases := map[string]any{
		"mapping": configsync.Tree{"name": "boards", "dir": ""},
		"list":    []any{map[string]any{"name": "boards", "dir": ""}},
	}

	for name, boards := range cases {
		t.Run(name, func(t *testing.T) {
			server := configsync.Tree{
				"boards":      boards,
				"task_skills": configsync.Tree{"dir": ""},
				"auth":        configsync.Tree{"db_path": "", "master_key_file": ""},
				"images":      configsync.Tree{"db_path": ""},
				"op_store":    configsync.Tree{"db_path": ""},
			}

			paths := pathsEngine().serverPaths(server)

			assert.NotContains(t, paths, "/")
			assert.NotContains(t, paths, "")
			assert.Empty(t, paths)
		})
	}
}

func TestServerPathsListsLocationsOutsideStateDir(t *testing.T) {
	server := configsync.Tree{
		"boards":      configsync.Tree{"dir": "~/outside/boards"},
		"task_skills": configsync.Tree{"dir": "~/.contextmatrix/task-skills"},
		"auth":        configsync.Tree{"db_path": "/var/lib/cm/auth.db"},
	}

	paths := pathsEngine().serverPaths(server)

	assert.Equal(t, []string{"/home/u/outside/boards", "/var/lib/cm"}, paths)
}

func TestServiceForRendersFixedPath(t *testing.T) {
	t.Setenv("PATH", "/ambient/bin:/usr/bin")

	h := newHarness(t, true)

	s := h.e.serviceFor("contextmatrix-agent", Trees{})
	assert.Equal(t, h.e.Host.GoBin+":/usr/local/bin:/usr/bin:/bin", s.Env["PATH"])

	h.e.Host.OS = "darwin"
	s = h.e.serviceFor("contextmatrix-agent", Trees{})
	assert.Equal(t, h.e.Host.GoBin+":/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin", s.Env["PATH"])
}

func TestResolveIdentityKeepsAndFillsTheArchitectKey(t *testing.T) {
	e := pathsEngine()
	paired := parseTree(t, "mcp_api_key: MCP\nbackends:\n  agent: {api_key: A}\n  chat: {api_key: C}\n  architect: {api_key: ARCH}\n")

	keys, _, err := e.resolveIdentity(Trees{Server: paired})
	require.NoError(t, err)
	assert.Equal(t, Keys{MCP: "MCP", Agent: "A", Chat: "C", Architect: "ARCH"}, keys, "a key server.yaml already pairs is reused")

	placeholder := parseTree(t, "mcp_api_key: MCP\nbackends:\n  agent: {api_key: A}\n  chat: {api_key: C}\n  architect: {api_key: \"\"}\n")

	keys, _, err = e.resolveIdentity(Trees{Server: placeholder})
	require.NoError(t, err)
	assert.Equal(t, "A", keys.Agent, "existing keys survive")
	assert.Len(t, keys.Architect, 64, "the empty placeholder gets a fresh key")

	keys, _, err = e.resolveIdentity(Trees{Server: placeholder, Architect: parseTree(t, "api_key: FROMFILE\n")})
	require.NoError(t, err)
	assert.Equal(t, "FROMFILE", keys.Architect, "architect.yaml is read first")
}

func TestServiceForArchitect(t *testing.T) {
	h := newHarness(t, true)
	h.images.dockerHost = "unix:///run/user/1000/docker.sock"

	s := h.e.serviceFor(repos.Architect, Trees{Architect: configsync.Tree{"state_dir": "/srv/architect"}})
	assert.Equal(t, h.e.Host.Binary("contextmatrix-architect"), s.Binary)
	assert.Equal(t, []string{"serve", "--config", h.e.L.ArchitectConfig()}, s.Args)
	assert.Equal(t, []string{h.e.L.StateDir, "/srv/architect"}, s.ReadWritePaths)

	_, has := s.Env["DOCKER_HOST"]
	assert.False(t, has, "the architect runs no containers")
	assert.Equal(t, "unix:///run/user/1000/docker.sock", h.e.serviceFor(repos.Agent, Trees{}).Env["DOCKER_HOST"])
}

func TestArchitectPathsListsOnlyAnAbsoluteDirOutsideStateDir(t *testing.T) {
	cases := map[string]struct {
		dir  string
		want []string
	}{
		"outside":    {"/srv/architect", []string{"/srv/architect"}},
		"inside":     {"/home/u/.contextmatrix/architect", nil},
		"state root": {"/home/u/.contextmatrix", nil},
		"tilde":      {"~/architect", nil},
		"relative":   {"architect", nil},
		"empty":      {"", nil},
	}

	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, c.want, pathsEngine().architectPaths(configsync.Tree{"state_dir": c.dir}))
		})
	}
}
