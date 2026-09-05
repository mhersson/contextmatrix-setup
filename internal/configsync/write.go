package configsync

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"

	"gopkg.in/yaml.v3"
)

const header = `# Managed by contextmatrix-setup: it keeps the KEY SET in sync with upstream.
# You own the VALUES. Edit freely; they are never overwritten. Comments are not preserved.
# Upstream reference with comments: %s
`

func Encode(t Tree, reference string) ([]byte, error) {
	var buf bytes.Buffer

	fmt.Fprintf(&buf, header, reference)

	var node yaml.Node
	if err := node.Encode(t); err != nil {
		return nil, err
	}

	decimalFloats(&node)
	styleTokenCosts(&node)

	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)

	if err := enc.Encode(&node); err != nil {
		return nil, err
	}

	if err := enc.Close(); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

// decimalFloats rewrites every float scalar in plain decimal form: the
// encoder's 8e-07 reads back fine but is not what a person edits.
func decimalFloats(n *yaml.Node) {
	if n.Kind == yaml.ScalarNode && n.Tag == "!!float" {
		if f, err := strconv.ParseFloat(n.Value, 64); err == nil {
			n.Value = strconv.FormatFloat(f, 'f', -1, 64)
		}

		return
	}

	for _, c := range n.Content {
		decimalFloats(c)
	}
}

// styleTokenCosts writes each token_costs entry the way the upstream
// example does: one flow mapping per model, prompt before completion.
func styleTokenCosts(root *yaml.Node) {
	if root.Kind != yaml.MappingNode {
		return
	}

	for i := 0; i+1 < len(root.Content); i += 2 {
		if root.Content[i].Value != "token_costs" {
			continue
		}

		for _, entry := range mappingValues(root.Content[i+1]) {
			entry.Style = yaml.FlowStyle
			orderKeys(entry, "prompt", "completion")
		}
	}
}

func mappingValues(n *yaml.Node) []*yaml.Node {
	if n.Kind != yaml.MappingNode {
		return nil
	}

	var out []*yaml.Node

	for i := 1; i < len(n.Content); i += 2 {
		out = append(out, n.Content[i])
	}

	return out
}

// orderKeys moves the named keys to the front of a mapping, in the order
// given; other keys keep their place after them.
func orderKeys(n *yaml.Node, keys ...string) {
	if n.Kind != yaml.MappingNode {
		return
	}

	var front, rest []*yaml.Node

	for _, key := range keys {
		for i := 0; i+1 < len(n.Content); i += 2 {
			if n.Content[i].Value == key {
				front = append(front, n.Content[i], n.Content[i+1])
			}
		}
	}

	for i := 0; i+1 < len(n.Content); i += 2 {
		if !slices.Contains(keys, n.Content[i].Value) {
			rest = append(rest, n.Content[i], n.Content[i+1])
		}
	}

	front = append(front, rest...)
	n.Content = front
}

func Hash(data []byte) string {
	sum := sha256.Sum256(data)

	return hex.EncodeToString(sum[:])
}

func WriteIfChanged(path string, data []byte) (bool, error) {
	if existing, err := os.ReadFile(path); err == nil && bytes.Equal(existing, data) {
		return false, nil
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return false, err
	}

	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return false, err
	}

	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)

		return false, err
	}

	return true, nil
}
