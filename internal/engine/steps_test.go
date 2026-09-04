package engine

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/mhersson/contextmatrix-setup/internal/configsync"
	"github.com/mhersson/contextmatrix-setup/internal/layout"
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

	s := h.e.serviceFor("contextmatrix-agent", configsync.Tree{})
	assert.Equal(t, h.e.Host.GoBin+":/usr/local/bin:/usr/bin:/bin", s.Env["PATH"])

	h.e.Host.OS = "darwin"
	s = h.e.serviceFor("contextmatrix-agent", configsync.Tree{})
	assert.Equal(t, h.e.Host.GoBin+":/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin", s.Env["PATH"])
}
