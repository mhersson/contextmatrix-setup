// Package configsync keeps an installed config file's key set equal to the
// upstream schema while leaving the user's values alone.
package configsync

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// Tree is a decoded YAML mapping. yaml.v3 decodes nested mappings with string
// keys as map[string]any, so nested nodes are Trees too.
type Tree = map[string]any

func Parse(data []byte) (Tree, error) {
	var raw any
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, err
	}

	if raw == nil {
		return Tree{}, nil
	}

	tr, ok := raw.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("top level is %T, want a mapping", raw)
	}

	return tr, nil
}

func LoadFile(path string) (Tree, bool, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return Tree{}, false, nil
	}

	if err != nil {
		return nil, false, err
	}

	tr, err := Parse(data)
	if err != nil {
		return nil, true, fmt.Errorf("parse %s: %w", path, err)
	}

	return tr, true, nil
}

func Get(t Tree, path string) (any, bool) {
	parts := strings.Split(path, ".")
	cur := any(t)

	for _, p := range parts {
		m, ok := cur.(map[string]any)
		if !ok {
			return nil, false
		}

		cur, ok = m[p]
		if !ok {
			return nil, false
		}
	}

	return cur, true
}

func Set(t Tree, path string, v any) {
	parts := strings.Split(path, ".")
	cur := t

	for _, p := range parts[:len(parts)-1] {
		next, ok := cur[p].(map[string]any)
		if !ok {
			next = Tree{}
			cur[p] = next
		}

		cur = next
	}

	cur[parts[len(parts)-1]] = v
}

func Clone(t Tree) Tree {
	return cloneValue(t).(map[string]any)
}

func cloneValue(v any) any {
	switch x := v.(type) {
	case map[string]any:
		out := make(Tree, len(x))
		for k, val := range x {
			out[k] = cloneValue(val)
		}

		return out
	case []any:
		out := make([]any, len(x))
		for i, val := range x {
			out[i] = cloneValue(val)
		}

		return out
	default:
		return v
	}
}
