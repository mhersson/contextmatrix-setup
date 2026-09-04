package bootstrap

import (
	"context"
	"io"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseLink(t *testing.T) {
	p, ok := ParseLink(`time=2026-09-04T07:00:00Z level=INFO msg="auth: bootstrap link" path=/auth/token/abc.DEF-123`)
	require.True(t, ok)
	assert.Equal(t, "/auth/token/abc.DEF-123", p)

	_, ok = ParseLink(`level=INFO msg="config loaded" path=/home/u/.config/contextmatrix/server.yaml`)
	assert.False(t, ok)
}

func TestURL(t *testing.T) {
	assert.Equal(t, "http://localhost:18080/auth/token/x", URL(18080, "/auth/token/x"))
}

func TestWaitFindsLink(t *testing.T) {
	follow := func(ctx context.Context, w io.Writer) error {
		_, _ = io.WriteString(w, "starting\n")
		_, _ = io.WriteString(w, `msg="auth: bootstrap link" path=/auth/token/tok`+"\n")

		<-ctx.Done()

		return nil
	}

	p, err := Wait(context.Background(), follow, 2*time.Second)
	require.NoError(t, err)
	assert.Equal(t, "/auth/token/tok", p)
}

func TestWaitTimesOut(t *testing.T) {
	follow := func(ctx context.Context, w io.Writer) error {
		_, _ = io.WriteString(w, "nothing here\n")

		<-ctx.Done()

		return nil
	}

	_, err := Wait(context.Background(), follow, 100*time.Millisecond)
	require.ErrorIs(t, err, ErrNoLink)
}

func TestWaitFollowEndsEarly(t *testing.T) {
	follow := func(context.Context, io.Writer) error { return nil }

	_, err := Wait(context.Background(), follow, time.Second)
	require.ErrorIs(t, err, ErrNoLink)
}

func TestWaitStopsAChattyFollower(t *testing.T) {
	// follow never checks ctx itself; it keeps writing until a write fails.
	// This is deliberate: it must attempt a write after the link is found,
	// with no chance to observe cancellation before that write blocks.
	follow := func(_ context.Context, w io.Writer) error {
		_, _ = io.WriteString(w, `msg="auth: bootstrap link" path=/auth/token/chatty`+"\n")

		for {
			if _, err := io.WriteString(w, "still logging\n"); err != nil {
				return nil
			}
		}
	}

	p, err := Wait(context.Background(), follow, 2*time.Second)
	require.NoError(t, err)
	assert.Equal(t, "/auth/token/chatty", p)
}
