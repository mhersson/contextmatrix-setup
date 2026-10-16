package engine

import "github.com/mhersson/contextmatrix-setup/internal/configsync"

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
