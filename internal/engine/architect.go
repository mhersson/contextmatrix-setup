package engine

import (
	"fmt"

	"github.com/mhersson/contextmatrix-setup/internal/configsync"
)

// fillArchitectBlock forces the opinionated architect entry over the empty,
// disabled block CM's schema puts in server.yaml before the architect is
// installed: the merge would keep those empty values as the user's. A value
// the user did set is kept.
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
// port is taken, so it would only restart in a loop. An installed architect
// holds its own port, so only a new one is checked.
func (e *Engine) holdNewArchitect(isNew bool, architect configsync.Tree) string {
	if !isNew {
		return ""
	}

	v, _ := configsync.Get(architect, "port")

	port, ok := v.(int)
	if !ok || e.portFree(port) {
		return ""
	}

	return fmt.Sprintf("port %d is in use; set port in architect.yaml and backends.architect.url in server.yaml, then run update", port)
}
