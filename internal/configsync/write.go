package configsync

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

const header = `# Managed by contextmatrix-setup: it keeps the KEY SET in sync with upstream.
# You own the VALUES. Edit freely; they are never overwritten. Comments are not preserved.
# Upstream reference with comments: %s
`

func Encode(t Tree, reference string) ([]byte, error) {
	var buf bytes.Buffer

	fmt.Fprintf(&buf, header, reference)

	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)

	if err := enc.Encode(t); err != nil {
		return nil, err
	}

	if err := enc.Close(); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
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
