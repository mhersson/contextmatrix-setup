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
