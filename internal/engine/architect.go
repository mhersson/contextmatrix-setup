package engine

import (
	"context"
	"fmt"

	"github.com/mhersson/contextmatrix-setup/internal/configsync"
	"github.com/mhersson/contextmatrix-setup/internal/repos"
)

// fillArchitectBlock forces the opinionated architect entry over the empty,
// disabled block CM's schema puts in server.yaml before the architect is
// installed: the merge would keep those empty values as the user's. Empty
// values and enabled: false are filled; other values the user set are kept.
func fillArchitectBlock(force map[string]any, user, opinionated configsync.Tree) {
	for _, k := range []string{
		"backends.architect.url",
		"backends.architect.api_key",
		"backends.architect.enabled",
		"backends.architect.default_model",
	} {
		if v, ok := configsync.Get(user, k); ok && v != nil && v != "" && v != false {
			continue
		}

		if v, ok := configsync.Get(opinionated, k); ok {
			force[k] = v
		}
	}
}

// holdNewArchitect names why an architect this run adds must not start: its
// port is taken, so it would only restart in a loop. Only a new architect is
// checked, and not one already running: the listener on the port is then its
// own.
func (e *Engine) holdNewArchitect(ctx context.Context, isNew bool, architect configsync.Tree) string {
	if !isNew {
		return ""
	}

	if active, _ := e.Services.Active(ctx, repos.Architect); active {
		return ""
	}

	v, _ := configsync.Get(architect, "port")

	port, ok := v.(int)
	if !ok || e.portFree(port) {
		return ""
	}

	return fmt.Sprintf("port %d is in use; set port in architect.yaml and backends.architect.url in server.yaml, then run update", port)
}
