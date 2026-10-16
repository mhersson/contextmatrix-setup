package engine

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestFillArchitectBlockRepairsTheSchemaPlaceholder(t *testing.T) {
	op := Opinionated(DefaultAnswers(), testFacts()).Server
	placeholder := parseTree(t, "backends:\n  architect:\n    url: \"\"\n    api_key: \"\"\n    enabled: false\n    default_model: \"\"\n")

	force := map[string]any{}
	fillArchitectBlock(force, placeholder, op)

	assert.Equal(t, map[string]any{
		"backends.architect.url":           "http://localhost:19094",
		"backends.architect.api_key":       "architectkey",
		"backends.architect.enabled":       true,
		"backends.architect.default_model": DefaultArchitectModel,
	}, force)

	force = map[string]any{}
	fillArchitectBlock(force, parseTree(t, "port: 18080\n"), op)
	assert.Len(t, force, 4, "a server.yaml without the block gets all four")
}

func TestFillArchitectBlockKeepsWhatTheUserSet(t *testing.T) {
	op := Opinionated(DefaultAnswers(), testFacts()).Server
	set := parseTree(t, "backends:\n  architect:\n    url: http://elsewhere:9095\n    api_key: \"\"\n    enabled: false\n    default_model: my/model\n")

	force := map[string]any{}
	fillArchitectBlock(force, set, op)

	assert.Equal(t, map[string]any{
		"backends.architect.api_key": "architectkey",
		"backends.architect.enabled": true,
	}, force)
}
