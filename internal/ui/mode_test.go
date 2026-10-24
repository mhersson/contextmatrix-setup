package ui

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
)

func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestDetectPlainCases(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		isTerm    bool
		env       map[string]string
		wantPlain bool
		wantTTY   bool
		wantDark  bool
	}{
		{"no color", true, map[string]string{"NO_COLOR": "1"}, true, true, true},
		{"dumb", true, map[string]string{"TERM": "dumb"}, true, true, true},
		{"not a terminal", false, nil, true, false, true},
		{"terminal", true, nil, false, true, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dark := func() bool {
				if tt.wantPlain {
					t.Fatal("dark queried in plain mode")
				}

				return false
			}
			m := detect(func() bool { return tt.isTerm },
				func() (int, error) { return 80, nil }, dark, env(tt.env))
			assert.Equal(t, tt.wantPlain, m.Plain)
			assert.Equal(t, tt.wantTTY, m.TTY)
			assert.Equal(t, tt.wantDark, m.Dark)
		})
	}
}

func TestDetectWidth(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		w    int
		err  error
		want int
	}{
		{"zero", 0, nil, 100},
		{"error", 80, errors.New("x"), 100},
		{"clamped", 200, nil, 120},
		{"as is", 90, nil, 90},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			m := detect(func() bool { return true },
				func() (int, error) { return tt.w, tt.err },
				func() bool { return true }, env(nil))
			assert.Equal(t, tt.want, m.Width)
		})
	}
}
