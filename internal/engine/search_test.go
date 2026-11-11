package engine

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mhersson/contextmatrix-setup/internal/host"
)

func TestSearchNoticeOnlyWithoutRG(t *testing.T) {
	info := host.Info{OS: "linux", GoBin: "/g", Search: host.SearchTools{RG: "/usr/bin/rg"}}
	assert.Empty(t, searchNotice(info))

	info.Search = host.SearchTools{FD: "/usr/bin/fd"}
	withFD := searchNotice(info)
	assert.NotEmpty(t, withFD)

	info.Search = host.SearchTools{}
	assert.Len(t, searchNotice(info), len(withFD)+1, "a missing fd adds a warning")
}

// assertFramed checks the notice opens and closes the output, so the build
// output between cannot scroll it out of sight.
func assertFramed(t *testing.T, out string, notice []string) {
	t.Helper()

	block := strings.Join(notice, "\n") + "\n"
	assert.True(t, strings.HasPrefix(out, block), "the notice comes first")
	assert.True(t, strings.HasSuffix(out, block), "the notice comes last")
}

func TestInstallUpdateAndStatusShowTheSearchNotice(t *testing.T) {
	h := newHarness(t, true)
	h.e.Host.Search = host.SearchTools{}

	notice := searchNotice(h.e.Host)
	require.NotEmpty(t, notice)

	a := DefaultAnswers()
	a.AuthMode = "none"
	a.GitHubMode = "pat"
	a.GitHubPAT = "x"

	require.NoError(t, h.e.Install(context.Background(), a))
	assertFramed(t, h.out.String(), notice)

	h.out.b = nil

	require.NoError(t, h.e.Update(context.Background(), UpdateOptions{Yes: true}))
	assertFramed(t, h.out.String(), notice)

	h.out.b = nil

	s, err := h.e.Status(context.Background())
	require.NoError(t, err)
	h.e.PrintStatus(s)
	assert.Equal(t, 1, strings.Count(h.out.String(), notice[0]))
}

func TestNoSearchNoticeWhenTheServiceFindsRG(t *testing.T) {
	h := installed(t, true)

	missing := searchNotice(host.Info{OS: h.e.Host.OS, GoBin: h.e.Host.GoBin})

	require.NoError(t, h.e.Update(context.Background(), UpdateOptions{Yes: true}))
	assert.NotContains(t, h.out.String(), missing[0])
}

func TestClaudeNotice(t *testing.T) {
	missing := host.Info{OS: "linux", GoBin: "/g"}
	assert.NotEmpty(t, claudeNotice(missing, true), "a token with no claude to spend it")
	assert.Empty(t, claudeNotice(missing, false), "without a token the subscription is not in use")

	found := missing
	found.Claude = host.ClaudeBinary{OnServicePath: "/usr/local/bin/claude"}
	assert.Empty(t, claudeNotice(found, true))

	found.Claude = host.ClaudeBinary{Native: "/home/u/.local/bin/claude"}
	assert.Empty(t, claudeNotice(found, true), "the architect PATH gets the native installer's directory")

	shellOnly := missing
	shellOnly.Claude = host.ClaudeBinary{ShellPath: "/home/u/.nvm/bin/claude"}
	notice := strings.Join(claudeNotice(shellOnly, true), "\n")
	assert.Contains(t, notice, "/home/u/.nvm/bin/claude", "the hint names the binary the service cannot see")
	assert.Contains(t, notice, "/g/claude", "and where a link makes it visible")
}
