package configsync

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEncodeHeaderAndIndent(t *testing.T) {
	data, err := Encode(Tree{"port": 18080, "auth": Tree{"mode": "none"}}, "config.yaml.example")
	require.NoError(t, err)

	text := string(data)
	assert.True(t, len(text) > 0 && text[0] == '#')
	assert.Contains(t, text, "contextmatrix-setup")
	assert.Contains(t, text, "config.yaml.example")
	assert.Contains(t, text, "auth:\n  mode: none\n")
	assert.Contains(t, text, "port: 18080\n")

	back, err := Parse(data)
	require.NoError(t, err)
	assert.Equal(t, 18080, back["port"])
}

func TestWriteIfChanged(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "server.yaml")

	changed, err := WriteIfChanged(path, []byte("a: 1\n"))
	require.NoError(t, err)
	assert.True(t, changed)

	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())

	changed, err = WriteIfChanged(path, []byte("a: 1\n"))
	require.NoError(t, err)
	assert.False(t, changed)

	changed, err = WriteIfChanged(path, []byte("a: 2\n"))
	require.NoError(t, err)
	assert.True(t, changed)

	entries, err := os.ReadDir(filepath.Dir(path))
	require.NoError(t, err)
	assert.Len(t, entries, 1, "no temp file left behind")
}

func TestHash(t *testing.T) {
	first := Hash([]byte("x"))
	second := Hash([]byte("x"))
	assert.Equal(t, first, second, "same input hashes the same")
	assert.NotEqual(t, Hash([]byte("x")), Hash([]byte("y")))
	assert.Len(t, Hash(nil), 64)
}
