package engine

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
)

type Keys struct {
	MCP   string
	Agent string
	Chat  string
}

func randomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("random bytes: %w", err)
	}

	return hex.EncodeToString(b), nil
}

// NewKeys generates the three shared secrets: 32 bytes each, hex encoded,
// which satisfies the backends' 32-character minimum with room to spare.
func NewKeys() (Keys, error) {
	var k Keys

	var err error

	if k.MCP, err = randomHex(32); err != nil {
		return k, err
	}

	if k.Agent, err = randomHex(32); err != nil {
		return k, err
	}

	if k.Chat, err = randomHex(32); err != nil {
		return k, err
	}

	return k, nil
}

func NewInstanceID(hostname string) (string, error) {
	if hostname == "" {
		hostname = "local"
	}

	suffix, err := randomHex(3)
	if err != nil {
		return "", err
	}

	return hostname + "-" + suffix, nil
}
