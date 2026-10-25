package engine

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mhersson/contextmatrix-setup/internal/configsync"
	"github.com/mhersson/contextmatrix-setup/internal/repos"
	"github.com/mhersson/contextmatrix-setup/internal/services"
	"github.com/mhersson/contextmatrix-setup/internal/state"
	"github.com/mhersson/contextmatrix-setup/internal/ui"
)

func installed(t *testing.T, docker bool) *harness {
	t.Helper()

	h := newHarness(t, docker)
	a := DefaultAnswers()
	a.AuthMode = "none"
	a.GitHubMode = "pat"
	a.GitHubPAT = "x"
	require.NoError(t, h.e.Install(context.Background(), a))
	h.out.b = nil
	h.images.built = nil

	return h
}

// declined installs with service management turned down, the way a user who
// answers no to the services question ends up.
func declined(t *testing.T) *harness {
	t.Helper()

	h := newHarness(t, true)
	a := DefaultAnswers()
	a.AuthMode = "none"
	a.GitHubMode = "pat"
	a.GitHubPAT = "x"
	a.Services = false
	require.NoError(t, h.e.Install(context.Background(), a))
	h.out.b = nil
	h.images.built = nil

	return h
}

func systemctlCalls(h *harness) []string {
	var out []string

	for _, c := range h.runner.Calls() {
		if c.Name == "systemctl" {
			out = append(out, strings.Join(c.Args, " "))
		}
	}

	return out
}

func starts(h *harness) []string {
	var out []string

	for _, c := range h.runner.Calls() {
		if c.Name == "systemctl" && len(c.Args) == 3 && c.Args[1] == "start" {
			out = append(out, c.Args[2])
		}
	}

	return out
}

func restarts(h *harness) []string {
	var out []string

	for _, c := range h.runner.Calls() {
		if c.Name == "systemctl" && len(c.Args) == 3 && c.Args[1] == "restart" {
			out = append(out, c.Args[2])
		}
	}

	return out
}

// enables lists the units enabled, in call order.
func enables(h *harness) []string {
	var out []string

	for _, c := range h.runner.Calls() {
		if c.Name == "systemctl" && len(c.Args) == 3 && c.Args[1] == "enable" {
			out = append(out, c.Args[2])
		}
	}

	return out
}

// validations counts the runs of repo's own config validation.
func validations(h *harness, repo string) int {
	n := 0

	for _, c := range h.runner.Calls() {
		if c.Name == h.e.Host.Binary(repo) && len(c.Args) > 1 && c.Args[0] == "config" && c.Args[1] == "validate" {
			n++
		}
	}

	return n
}

// rejectIncomplete makes the validators reject what a host without GitHub
// and docker leaves behind, as upstream does: the server an empty github
// block, the worker backends an empty base_image.
func rejectIncomplete(h *harness) {
	h.runner.On(h.e.Host.Binary(repos.Server), "config", "validate").Return("", `error: github.auth_mode is required: must be "app" or "pat" (got "")`, 1)
	h.runner.On(h.e.Host.Binary(repos.Agent), "config", "validate").Return("", "error: base_image is required", 1)
	h.runner.On(h.e.Host.Binary(repos.Chat), "config", "validate").Return("", "error: base_image is required", 1)
}

func TestUpdateNothingMovedIsQuiet(t *testing.T) {
	h := installed(t, true)

	require.NoError(t, h.e.Update(context.Background(), UpdateOptions{Yes: true}))

	assert.Contains(t, h.out.String(), "up to date")
	assert.Empty(t, h.images.built)
	assert.Empty(t, restarts(h))
}

// An install made before the variants existed records only the default
// image; the next update builds the variants without a repo moving.
func TestUpdateBuildsMissingVariants(t *testing.T) {
	h := installed(t, true)

	st, _, err := state.Load(h.e.L.StateFile())
	require.NoError(t, err)

	for family, img := range st.Images {
		img.Variants = nil
		st.Images[family] = img
	}

	require.NoError(t, st.Save(h.e.L.StateFile()))
	require.NoError(t, h.e.Update(context.Background(), UpdateOptions{Yes: true}))

	assert.Equal(t, []string{"contextmatrix-agent-worker:bbbbbbb", "contextmatrix-chat-worker:ccccccc"}, h.images.built)
	assert.Empty(t, restarts(h), "same commit, same tag: nothing to restart")
	assert.Empty(t, h.images.removed)
	assert.Contains(t, h.out.String(), "variant images missing")

	st, _, _ = state.Load(h.e.L.StateFile())
	assert.Len(t, st.Images["contextmatrix-agent-worker"].Variants, 4)

	h.images.built = nil
	require.NoError(t, h.e.Update(context.Background(), UpdateOptions{Yes: true}))
	assert.Empty(t, h.images.built, "recorded variants are not rebuilt")
}

// A record made before jvm-node was tracked holds the other variants; the
// image already exists, so it is recorded at the next build, not by an
// extra one.
func TestUpdateRecordsUntrackedVariantAtNextBuild(t *testing.T) {
	h := installed(t, true)

	st, _, err := state.Load(h.e.L.StateFile())
	require.NoError(t, err)

	for family, img := range st.Images {
		delete(img.Variants, "jvm-node")
		st.Images[family] = img
	}

	require.NoError(t, st.Save(h.e.L.StateFile()))
	require.NoError(t, h.e.Update(context.Background(), UpdateOptions{Yes: true}))
	assert.Empty(t, h.images.built, "a partial record builds nothing")

	h.git.heads["contextmatrix-agent"] = "b9999999999"
	h.git.logs["contextmatrix-agent"] = []repos.Change{{SHA: "b9999999999", Title: "fix(agent): something", Commits: 1}}
	require.NoError(t, h.e.Update(context.Background(), UpdateOptions{Yes: true}))

	st, _, _ = state.Load(h.e.L.StateFile())
	assert.Equal(t, "sha256:b9999999999-jvm-node", st.Images["contextmatrix-agent-worker"].Variants["jvm-node"])
	assert.NotContains(t, st.Images["contextmatrix-chat-worker"].Variants, "jvm-node", "chat did not move, so it waits for its own build")
	assert.Equal(t, []string{
		"sha256:bbbbbbb2222-go-node",
		"sha256:bbbbbbb2222-python",
		"contextmatrix-agent-worker:bbbbbbb",
	}, h.images.removed, "no ID was recorded for the replaced jvm-node image, so nothing is removed for it")
}

func TestUpdateRebuildsMovedRepoAndRestartsIt(t *testing.T) {
	h := installed(t, true)
	h.git.heads["contextmatrix-agent"] = "b9999999999"
	h.git.logs["contextmatrix-agent"] = []repos.Change{{SHA: "b9999999999", Title: "fix(agent): something", Commits: 1}}

	var summary string

	opts := UpdateOptions{Confirm: func(s string) bool {
		summary = s

		return true
	}}

	require.NoError(t, h.e.Update(context.Background(), opts))

	assert.Contains(t, summary, "something")
	assert.Contains(t, summary, "fix")
	assert.NotContains(t, summary, "\x1b")
	assert.Equal(t, []string{"contextmatrix-agent-worker:b999999"}, h.images.built)
	assert.Equal(t, []string{"contextmatrix-agent"}, restarts(h))
	assert.Equal(t, []string{
		"sha256:bbbbbbb2222-go-node",
		"sha256:bbbbbbb2222-python",
		"sha256:bbbbbbb2222-jvm-node",
		"contextmatrix-agent-worker:bbbbbbb",
	}, h.images.removed, "replaced variants removed after the build, the old tag after restart, the unchanged rust variant kept")

	agent, _, _ := configsync.LoadFile(h.e.L.AgentConfig())
	assert.Equal(t, "contextmatrix-agent-worker:b999999", get(t, agent, "base_image"))

	st, _, _ := state.Load(h.e.L.StateFile())
	assert.Equal(t, "b9999999999", st.Repos["contextmatrix-agent"].Commit)
	assert.Equal(t, "aaaaaaa1111", st.Repos["contextmatrix"].Commit)
}

func TestUpdateDeclinedDoesNothing(t *testing.T) {
	h := installed(t, true)
	h.git.heads["contextmatrix"] = "aaaaaaa9999"

	err := h.e.Update(context.Background(), UpdateOptions{Confirm: func(string) bool { return false }})
	require.NoError(t, err)
	assert.Empty(t, restarts(h))

	st, _, _ := state.Load(h.e.L.StateFile())
	assert.Equal(t, "aaaaaaa1111", st.Repos["contextmatrix"].Commit)
}

func TestUpdateKeepsManualEditsAndDropsStaleKeys(t *testing.T) {
	h := installed(t, true)

	server, _, _ := configsync.LoadFile(h.e.L.ServerConfig())
	configsync.Set(server, "log_level", "debug")
	configsync.Set(server, "obsolete", 1)
	data, _ := configsync.Encode(server, "x")
	require.NoError(t, os.WriteFile(h.e.L.ServerConfig(), data, 0o600))

	require.NoError(t, h.e.Update(context.Background(), UpdateOptions{Yes: true}))

	server, _, _ = configsync.LoadFile(h.e.L.ServerConfig())
	assert.Equal(t, "debug", get(t, server, "log_level"))
	_, has := configsync.Get(server, "obsolete")
	assert.False(t, has)
	assert.Contains(t, h.out.String(), "dropped obsolete")
	assert.Contains(t, h.out.String(), "edited by hand")
	assert.Equal(t, []string{"contextmatrix"}, restarts(h), "config changed, server restarted")
}

func TestUpdateFailedImageBuildIsAProblem(t *testing.T) {
	h := installed(t, true)
	h.git.heads["contextmatrix-agent"] = "b9999999999"
	h.images.fail = true

	err := h.e.Update(context.Background(), UpdateOptions{Yes: true})

	var pe *ProblemsError
	require.ErrorAs(t, err, &pe)
	assert.Equal(t, 1, pe.N)

	assert.Empty(t, restarts(h))
	assert.Contains(t, h.out.String(), "image build failed")

	st, _, _ := state.Load(h.e.L.StateFile())
	assert.Equal(t, "bbbbbbb2222", st.Repos["contextmatrix-agent"].Commit, "the repo stays unrecorded so the next run retries")
	assert.Equal(t, "contextmatrix-agent-worker:bbbbbbb", st.Images["contextmatrix-agent-worker"].Tag)

	agent, _, _ := configsync.LoadFile(h.e.L.AgentConfig())
	assert.Equal(t, "contextmatrix-agent-worker:bbbbbbb", get(t, agent, "base_image"))
}

func TestUpdateHandEditAloneRestartsService(t *testing.T) {
	h := installed(t, true)

	// Re-encoded with the engine's own reference line, so the merge produces
	// byte-identical output and only the state hash reveals the edit.
	server, _, _ := configsync.LoadFile(h.e.L.ServerConfig())
	configsync.Set(server, "log_level", "debug")
	data, err := configsync.Encode(server, referenceFor(repos.Server))
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(h.e.L.ServerConfig(), data, 0o600))

	require.NoError(t, h.e.Update(context.Background(), UpdateOptions{Yes: true}))

	assert.Equal(t, []string{"contextmatrix"}, restarts(h))
	assert.Contains(t, h.out.String(), "edited by hand")
	assert.NotContains(t, h.out.String(), "up to date")
}

func TestUpdateWorkflowSkillsOnlyWhenChanged(t *testing.T) {
	h := installed(t, true)
	h.git.heads["contextmatrix"] = "aaaaaaa2222"
	h.git.changed["workflow-skills"] = false

	require.NoError(t, h.e.Update(context.Background(), UpdateOptions{Yes: true}))
	data, _ := os.ReadFile(filepath.Join(h.e.L.WorkflowSkillsDir(), "create-plan.md"))
	assert.Equal(t, "plan aaaaaaa1111\n", string(data), "unchanged upstream skills are not recopied")

	h.git.heads["contextmatrix"] = "aaaaaaa3333"
	h.git.changed["workflow-skills"] = true
	require.NoError(t, os.WriteFile(filepath.Join(h.e.L.WorkflowSkillsDir(), "create-plan.md"), []byte("mine\n"), 0o600))

	require.NoError(t, h.e.Update(context.Background(), UpdateOptions{Yes: true}))
	data, _ = os.ReadFile(filepath.Join(h.e.L.WorkflowSkillsDir(), "create-plan.md"))
	assert.Equal(t, "mine\n", string(data), "locally modified skill is left alone")
	assert.Contains(t, h.out.String(), "locally modified")
}

func TestUpdateDockerAppeared(t *testing.T) {
	h := installed(t, false)
	h.e.Host.Docker = true

	require.NoError(t, h.e.Update(context.Background(), UpdateOptions{Yes: true}))

	assert.Len(t, h.images.built, 2)
	server, _, _ := configsync.LoadFile(h.e.L.ServerConfig())
	assert.Equal(t, true, get(t, server, "backends.agent.enabled"))
	assert.Equal(t, true, get(t, server, "backends.chat.enabled"))
	assert.Contains(t, h.out.String(), "docker found")

	st, _, _ := state.Load(h.e.L.StateFile())
	assert.True(t, st.Docker)
}

// A server waiting for its github block and backends waiting for docker are
// held back for those reasons, not reported invalid for the values their
// validators reject until then.
func TestUpdateSkipReasonsWinOverValidation(t *testing.T) {
	for _, tc := range []struct {
		name             string
		architectInvalid bool
	}{
		{name: "only skip rows"},
		{name: "an eligible invalid config is still a problem", architectInvalid: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t, false)
			rejectIncomplete(h)
			require.NoError(t, h.e.Install(context.Background(), DefaultAnswers()))
			h.out.b = nil

			if tc.architectInvalid {
				h.runner.On(h.e.Host.Binary(repos.Architect), "config", "validate").Return("", "error: contextmatrix_url is required", 1)
			}

			for repo, head := range map[string]string{
				repos.Server: "a9999999999", repos.Agent: "b9999999999",
				repos.Chat: "c9999999999", repos.Architect: "d9999999999",
			} {
				h.git.heads[repo] = head
			}

			err := h.e.Update(context.Background(), UpdateOptions{Yes: true})
			out := h.out.String()

			for _, repo := range []string{repos.Server, repos.Agent, repos.Chat} {
				row := resultRow(t, out, repo)
				assert.Contains(t, row, "not started", repo)
				assert.NotContains(t, row, "✗", repo)
				assert.Zero(t, validations(h, repo), "%s is held back, so it is not validated", repo)
			}

			assert.NotContains(t, out, "auth_mode is required")
			assert.NotContains(t, out, "base_image is required")

			if !tc.architectInvalid {
				require.NoError(t, err, "skip rows are not problems")
				assert.Equal(t, []string{repos.Architect}, restarts(h))

				return
			}

			var pe *ProblemsError
			require.ErrorAs(t, err, &pe)
			assert.Equal(t, 1, pe.N, "only the architect")
			assert.Contains(t, resultRow(t, out, repos.Architect), "✗")
			assert.Contains(t, out, "contextmatrix_url is required")
			assert.Empty(t, restarts(h))
		})
	}
}

// Once docker is there, the backends held back for it are validated again.
func TestUpdateValidatesABackendOnceDockerIsThere(t *testing.T) {
	h := newHarness(t, false)
	rejectIncomplete(h)
	require.NoError(t, h.e.Install(context.Background(), DefaultAnswers()))
	require.NoError(t, h.e.Update(context.Background(), UpdateOptions{Yes: true}))
	require.Zero(t, validations(h, repos.Agent))

	h.e.Host.Docker = true
	h.runner.On(h.e.Host.Binary(repos.Agent), "config", "validate").Return("ok\n", "", 0)
	h.runner.On(h.e.Host.Binary(repos.Chat), "config", "validate").Return("ok\n", "", 0)

	require.NoError(t, h.e.Update(context.Background(), UpdateOptions{Yes: true}))

	assert.Equal(t, 1, validations(h, repos.Agent))
	assert.Equal(t, 1, validations(h, repos.Chat))
	assert.Contains(t, restarts(h), repos.Agent)
	assert.Contains(t, restarts(h), repos.Chat)
}

// A unit is enabled only once setup starts it: the server held back for its
// github block is enabled by the update that starts it.
func TestUpdateEnablesAServiceItStartsForTheFirstTime(t *testing.T) {
	h := newHarness(t, true)
	a := DefaultAnswers()
	a.AuthMode = "none"
	require.NoError(t, h.e.Install(context.Background(), a))
	require.NotContains(t, starts(h), repos.Server)
	assert.NotContains(t, enables(h), repos.Server, "a unit setup did not start is not enabled")

	server, _, err := configsync.LoadFile(h.e.L.ServerConfig())
	require.NoError(t, err)
	configsync.Set(server, "github.auth_mode", "pat")
	configsync.Set(server, "github.pat.token", "x")
	data, err := configsync.Encode(server, referenceFor(repos.Server))
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(h.e.L.ServerConfig(), data, 0o600))

	n := len(h.runner.Calls())

	require.NoError(t, h.e.Update(context.Background(), UpdateOptions{Yes: true}))

	assert.Equal(t, []string{repos.Server}, restarts(h))

	var update []string

	for _, c := range h.runner.Calls()[n:] {
		if c.Name == "systemctl" {
			update = append(update, strings.Join(c.Args, " "))
		}
	}

	assert.Equal(t, []string{"--user enable " + repos.Server, "--user restart " + repos.Server}, update,
		"only the started unit is enabled, before its restart")
}

// A run that cannot reach the service manager the install recorded, such
// as an SSH session or cron job without the user manager, stops before it
// changes anything: going on would build, restart nothing and record the
// install as unmanaged.
func TestUpdateStopsWhenTheRecordedManagerIsUnreachable(t *testing.T) {
	h := installed(t, true)
	h.git.heads[repos.Agent] = "b9999999999"

	const reason = "systemctl --user is-system-running answered offline"

	h.e.Host.ServiceManager = "none"
	h.e.Host.ServiceManagerReason = reason
	h.e.Services = services.New("none", h.runner, h.e.L, 1000)

	before, err := os.ReadFile(h.e.L.StateFile())
	require.NoError(t, err)

	agent, err := os.ReadFile(h.e.L.AgentConfig())
	require.NoError(t, err)

	h.git.syncs.Store(0)
	n := len(h.runner.Calls())

	err = h.e.Update(context.Background(), UpdateOptions{Yes: true})
	require.Error(t, err)
	assert.Contains(t, err.Error(), reason, "the error says why the manager is unreachable")

	var pe *ProblemsError
	assert.NotErrorAs(t, err, &pe, "no run happened to report on")

	assert.Zero(t, h.git.syncs.Load(), "nothing is fetched")
	assert.Empty(t, h.runner.Calls()[n:], "nothing is built, validated or started")
	assert.Empty(t, h.images.built)

	after, err := os.ReadFile(h.e.L.StateFile())
	require.NoError(t, err)
	assert.Equal(t, string(before), string(after), "the state is untouched")

	agentAfter, err := os.ReadFile(h.e.L.AgentConfig())
	require.NoError(t, err)
	assert.Equal(t, string(agent), string(agentAfter), "no config is written")
}

func TestUpdateKeepsTheRecordedManager(t *testing.T) {
	for _, tc := range []struct {
		name     string
		recorded string
		detected string
		managed  bool
	}{
		{name: "systemd recorded and reachable", recorded: "systemd", detected: "systemd", managed: true},
		{name: "none recorded, none detected", recorded: "none", detected: "none"},
		{name: "none recorded, systemd detected", recorded: "none", detected: "systemd"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var h *harness
			if tc.recorded == "none" {
				h = declined(t)
			} else {
				h = installed(t, true)
			}

			h.e.Host.ServiceManager = tc.detected
			h.e.Services = services.New(tc.detected, h.runner, h.e.L, 1000)
			h.git.heads[repos.Agent] = "b9999999999"

			require.NoError(t, h.e.Update(context.Background(), UpdateOptions{Yes: true}))

			if tc.managed {
				assert.Equal(t, []string{repos.Agent}, restarts(h))
			} else {
				assert.Empty(t, systemctlCalls(h))
				assert.Contains(t, resultRow(t, h.out.String(), repos.Agent), "start by hand")
			}

			st, _, err := state.Load(h.e.L.StateFile())
			require.NoError(t, err)
			assert.Equal(t, tc.recorded, st.ServiceManager)
			assert.Equal(t, "b9999999999", st.Repos[repos.Agent].Commit)
		})
	}
}

func TestUpdateRequiresInstall(t *testing.T) {
	h := newHarness(t, true)
	err := h.e.Update(context.Background(), UpdateOptions{Yes: true})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "install")
}

func TestStatusAndUninstall(t *testing.T) {
	h := installed(t, true)
	h.runner.On("systemctl", "--user", "is-active").Return("active\n", "", 0)

	s, err := h.e.Status(context.Background())
	require.NoError(t, err)
	assert.Len(t, s.Repos, 4)
	assert.Equal(t, "aaaaaaa", s.Repos[0].Installed[:7])
	assert.Equal(t, 18080, s.Ports["contextmatrix"])
	assert.Equal(t, 19094, s.Ports["contextmatrix-architect"])
	assert.True(t, s.Docker)
	assert.Equal(t, "contextmatrix-agent-worker:bbbbbbb", s.Images["contextmatrix-agent-worker"])
	assert.Equal(t, []string{"go-node", "jvm-node", "python", "rust"}, s.Variants["contextmatrix-chat-worker"])

	h.e.PrintStatus(s)
	assert.Contains(t, h.out.String(), "18080")
	assert.Contains(t, h.out.String(), "contextmatrix-chat-worker:ccccccc")
	assert.Contains(t, h.out.String(), "variants: go-node jvm-node python rust")

	require.NoError(t, h.e.Uninstall(context.Background()))

	for _, name := range []string{"contextmatrix", "contextmatrix-agent", "contextmatrix-chat", "contextmatrix-architect"} {
		_, err := os.Stat(h.e.Services.UnitPath(name))
		assert.True(t, os.IsNotExist(err), name)
	}

	_, err = os.Stat(h.e.L.ServerConfig())
	assert.NoError(t, err, "configs survive uninstall")
}

func TestUpdateAfterDeclinedServicesSkipsUnits(t *testing.T) {
	h := declined(t)
	before := len(h.runner.Calls())
	h.git.heads["contextmatrix-agent"] = "b9999999999"

	require.NoError(t, h.e.Update(context.Background(), UpdateOptions{Yes: true}))

	assert.Empty(t, systemctlCalls(h), "no unit is written and nothing is restarted")

	rebuilt := false

	for _, c := range h.runner.Calls()[before:] {
		if c.Name == "make" && c.Dir == h.e.L.SrcDir("contextmatrix-agent") && c.Args[0] == "install" {
			rebuilt = true
		}
	}

	assert.True(t, rebuilt, "the moved repo is still rebuilt")
	assert.Contains(t, h.out.String(), "start by hand")

	st, _, _ := state.Load(h.e.L.StateFile())
	assert.Equal(t, "none", st.ServiceManager)
	assert.Equal(t, "b9999999999", st.Repos["contextmatrix-agent"].Commit)
}

func TestStatusAndUninstallWithoutServices(t *testing.T) {
	h := declined(t)

	s, err := h.e.Status(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "none", s.Manager)

	h.e.PrintStatus(s)
	assert.Contains(t, h.out.String(), "unmanaged")

	require.NoError(t, h.e.Uninstall(context.Background()))
	assert.NotContains(t, h.out.String(), "service removed")
	assert.Contains(t, h.out.String(), "kept:")
	assert.Empty(t, systemctlCalls(h))
}

// preArchitect turns an installed harness into an install made before setup
// knew the architect, whose server.yaml carries block as the architect
// entry: CM's schema writes the empty placeholder on a pre-architect update.
func preArchitect(t *testing.T, h *harness, block configsync.Tree) {
	t.Helper()

	require.NoError(t, h.e.Services.Remove(context.Background(), repos.Architect))
	require.NoError(t, os.Remove(h.e.L.ArchitectConfig()))

	server, _, err := configsync.LoadFile(h.e.L.ServerConfig())
	require.NoError(t, err)
	configsync.Set(server, "backends.architect", block)

	data, err := configsync.Encode(server, referenceFor(repos.Server))
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(h.e.L.ServerConfig(), data, 0o600))

	st, _, err := state.Load(h.e.L.StateFile())
	require.NoError(t, err)
	delete(st.Repos, repos.Architect)
	delete(st.Configs, "architect.yaml")
	st.Configs["server.yaml"] = state.ConfigHash{SHA256: configsync.Hash(data)}
	require.NoError(t, st.Save(h.e.L.StateFile()))
}

func placeholderBlock() configsync.Tree {
	return configsync.Tree{"url": "", "api_key": "", "enabled": false, "default_model": ""}
}

func TestUpdateAddsTheArchitectToAnOlderInstall(t *testing.T) {
	h := installed(t, true)
	preArchitect(t, h, placeholderBlock())

	require.NoError(t, h.e.Update(context.Background(), UpdateOptions{Yes: true}))

	server, _, err := configsync.LoadFile(h.e.L.ServerConfig())
	require.NoError(t, err)

	architect, found, err := configsync.LoadFile(h.e.L.ArchitectConfig())
	require.NoError(t, err)
	require.True(t, found)

	assert.Equal(t, "http://localhost:19094", get(t, server, "backends.architect.url"))
	assert.Equal(t, true, get(t, server, "backends.architect.enabled"))
	assert.Equal(t, DefaultArchitectModel, get(t, server, "backends.architect.default_model"))
	assert.Len(t, get(t, architect, "api_key"), 64)
	assert.Equal(t, get(t, architect, "api_key"), get(t, server, "backends.architect.api_key"), "both ends hold the same fresh key")

	assert.ElementsMatch(t, []string{"contextmatrix", "contextmatrix-architect"}, restarts(h),
		"the server rereads its backends; the architect starts")
	assert.FileExists(t, h.e.Services.UnitPath(repos.Architect))
	assert.Empty(t, h.images.built, "the architect has no image")

	st, _, err := state.Load(h.e.L.StateFile())
	require.NoError(t, err)
	assert.Equal(t, "ddddddd4444", st.Repos[repos.Architect].Commit)
}

func TestUpdateKeepsArchitectValuesTheUserSet(t *testing.T) {
	h := installed(t, true)
	block := placeholderBlock()
	block["default_model"] = "my/model"
	preArchitect(t, h, block)

	require.NoError(t, h.e.Update(context.Background(), UpdateOptions{Yes: true}))

	server, _, err := configsync.LoadFile(h.e.L.ServerConfig())
	require.NoError(t, err)
	assert.Equal(t, "my/model", get(t, server, "backends.architect.default_model"))
	assert.Equal(t, true, get(t, server, "backends.architect.enabled"))
}

func TestUpdateHoldsANewArchitectWhosePortIsTaken(t *testing.T) {
	h := installed(t, true)
	preArchitect(t, h, placeholderBlock())
	h.runner.On("systemctl", "--user", "is-active", repos.Architect).Return("inactive\n", "", 3)
	h.e.PortFree = func(int) bool { return false }

	require.NoError(t, h.e.Update(context.Background(), UpdateOptions{Yes: true}))

	assert.NotContains(t, restarts(h), repos.Architect)

	st, _, err := state.Load(h.e.L.StateFile())
	require.NoError(t, err)
	assert.Empty(t, st.Repos[repos.Architect].Commit, "left unrecorded so the next update checks again")

	h.e.PortFree = func(int) bool { return true }

	require.NoError(t, h.e.Update(context.Background(), UpdateOptions{Yes: true}))

	assert.Contains(t, restarts(h), repos.Architect)

	st, _, err = state.Load(h.e.L.StateFile())
	require.NoError(t, err)
	assert.Equal(t, "ddddddd4444", st.Repos[repos.Architect].Commit)
}

// An architect already running, such as one started by hand after it was
// held, owns the listener on its port, which must not hold it again.
func TestUpdateDoesNotHoldAnArchitectThatIsAlreadyRunning(t *testing.T) {
	h := installed(t, true)
	preArchitect(t, h, placeholderBlock())
	h.runner.On("systemctl", "--user", "is-active", repos.Architect).Return("active\n", "", 0)
	h.e.PortFree = func(int) bool { return false }

	require.NoError(t, h.e.Update(context.Background(), UpdateOptions{Yes: true}))

	assert.Contains(t, restarts(h), repos.Architect)

	st, _, err := state.Load(h.e.L.StateFile())
	require.NoError(t, err)
	assert.Equal(t, "ddddddd4444", st.Repos[repos.Architect].Commit)
}

// Without docker the agent and chat wait, but the architect runs no
// containers and still restarts when its repo moves.
func TestUpdateRestartsAMovedArchitectWithoutDocker(t *testing.T) {
	h := installed(t, false)
	h.git.heads[repos.Architect] = "d9999999999"

	require.NoError(t, h.e.Update(context.Background(), UpdateOptions{Yes: true}))

	assert.Equal(t, []string{repos.Architect}, restarts(h))
}

func TestUpdateIgnoresThePortOfAnInstalledArchitect(t *testing.T) {
	h := installed(t, true)
	h.e.PortFree = func(int) bool { return false }
	h.git.heads[repos.Architect] = "d9999999999"

	require.NoError(t, h.e.Update(context.Background(), UpdateOptions{Yes: true}))

	assert.Equal(t, []string{repos.Architect}, restarts(h), "a running architect holds its own port")
}

func TestUpdateContinuesWhenChangeListUnavailable(t *testing.T) {
	h := installed(t, true)
	h.git.heads["contextmatrix-agent"] = "b9999999999"
	h.git.errs["contextmatrix-agent"] = errors.New("git log: bad object")

	require.NoError(t, h.e.Update(context.Background(), UpdateOptions{Yes: true}))

	assert.Contains(t, h.out.String(), "change list unavailable")
	assert.Equal(t, []string{"contextmatrix-agent-worker:b999999"}, h.images.built)
	assert.Equal(t, []string{"contextmatrix-agent"}, restarts(h))
}

// stepLines returns, in order, the plain step lines that start with glyph.
func stepLines(out, glyph string) []string {
	var lines []string

	for _, l := range strings.Split(out, "\n") {
		if strings.HasPrefix(l, " "+glyph+" ") {
			lines = append(lines, l)
		}
	}

	return lines
}

func verbs(lines []string) []string {
	out := make([]string, 0, len(lines))

	for _, l := range lines {
		if f := strings.Fields(l); len(f) > 1 {
			out = append(out, f[1])
		}
	}

	return out
}

func TestUpdateStepsInPlainOutput(t *testing.T) {
	h := installed(t, true)
	h.git.heads["contextmatrix-agent"] = "b9999999999"

	require.NoError(t, h.e.Update(context.Background(), UpdateOptions{Yes: true}))

	out := h.out.String()
	done := stepLines(out, "✓")

	assert.NotContains(t, out, "\x1b")
	assert.Equal(t, []string{"fetch", "build", "images", "config", "restart", "update"}, verbs(done), "the steps, then the final line")
	assert.Empty(t, stepLines(out, "✗"))
	require.Len(t, done, 6)
	assert.Contains(t, done[1], "contextmatrix-agent", "only the moved repo is built")
	assert.Contains(t, done[2], "contextmatrix-agent-worker")
	assert.Contains(t, done[3], "agent.yaml", "the config step names the file the new image changed")
	assert.Contains(t, done[4], "contextmatrix-agent", "only the moved service restarts")
	assert.Contains(t, done[4], "healthy")
}

func TestUpdateBuildFailureShowsStepTail(t *testing.T) {
	h := installed(t, true)
	h.git.heads["contextmatrix-agent"] = "b9999999999"
	h.runner.On("make", "install").Return("boom\n", "", 2)

	require.Error(t, h.e.Update(context.Background(), UpdateOptions{Yes: true}))

	out := h.out.String()
	failed := stepLines(out, "✗")

	require.Len(t, failed, 1)
	assert.Equal(t, []string{"build"}, verbs(failed))
	assert.Contains(t, failed[0], "contextmatrix-agent")
	assert.GreaterOrEqual(t, strings.Index(out, "boom"), 0)
	assert.Less(t, strings.Index(out, "boom"), strings.Index(out, failed[0]), "plain output streams the build output before the failed line")
}

// A live console shows no build output while the step runs, so a failure
// boxes the tail and names the log.
func TestUpdateBuildFailureBoxesTheTailInATerminal(t *testing.T) {
	h := installed(t, true)
	h.git.heads["contextmatrix-agent"] = "b9999999999"
	h.runner.On("make", "install").Return("boom\n", "", 2)

	var screen, log bytes.Buffer

	c := ui.NewConsole(&screen, ui.NewTheme(ui.Mode{TTY: true, Width: 100}), &log, "/logs/update.log")

	err := h.e.Update(context.Background(), UpdateOptions{Yes: true, UI: c})
	c.Close()

	require.Error(t, err)
	assert.Contains(t, ansi.Strip(screen.String()), "/logs/update.log")
	assert.Contains(t, log.String(), "boom")
	assert.Empty(t, h.out.String(), "everything goes through the console")
}

func TestUpdateUnhealthyServiceReturnsProblems(t *testing.T) {
	h := installed(t, true)
	h.git.heads["contextmatrix-agent"] = "b9999999999"
	h.e.Probe = refused
	h.e.HealthBudget = 10 * time.Millisecond

	err := h.e.Update(context.Background(), UpdateOptions{Yes: true})

	var pe *ProblemsError
	require.ErrorAs(t, err, &pe)
	assert.Equal(t, 1, pe.N)
	assert.Contains(t, resultRow(t, h.out.String(), repos.Agent), "connection refused")

	st, _, err := state.Load(h.e.L.StateFile())
	require.NoError(t, err)
	assert.Equal(t, "b9999999999", st.Repos["contextmatrix-agent"].Commit, "the new binary is installed whatever its health")
}

func TestUpdateHealthyRunSucceeds(t *testing.T) {
	h := installed(t, true)
	h.git.heads["contextmatrix-agent"] = "b9999999999"

	require.NoError(t, h.e.Update(context.Background(), UpdateOptions{Yes: true}))

	row := resultRow(t, h.out.String(), repos.Agent)
	assert.Contains(t, row, "restarted")
	assert.Contains(t, row, "healthy")
	assert.Contains(t, row, "b999999", "the service now runs the new commit")
	assert.Contains(t, row, "agent-worker:b999999", "and the new worker image")
}

func TestUpdateFailedRestartShowsTheOldWorkerImage(t *testing.T) {
	h := installed(t, true)
	h.git.heads["contextmatrix-agent"] = "b9999999999"
	h.runner.On("systemctl", "--user", "restart", repos.Agent).Return("", "unit failed", 1)

	err := h.e.Update(context.Background(), UpdateOptions{Yes: true})

	var pe *ProblemsError
	require.ErrorAs(t, err, &pe)

	row := resultRow(t, h.out.String(), repos.Agent)
	assert.Contains(t, row, "restart failed")
	assert.Contains(t, row, "agent-worker:bbbbbbb", "the image built but the service never restarted onto it")
	assert.NotContains(t, row, "b999999")
	assert.Equal(t, []string{"contextmatrix-agent-worker:b999999"}, h.images.built)
}

// A service that is down after its restart is reported at once, with the
// lines it logged, instead of waiting out the budget.
func TestUpdateStoppedServiceShowsItsLogs(t *testing.T) {
	h := installed(t, true)
	h.git.heads["contextmatrix-agent"] = "b9999999999"
	h.e.Services = inactive{h.e.Services}
	h.e.Probe = refused
	h.e.NotRunningGrace = time.Millisecond
	h.runner.On("journalctl", "--user", "-u", repos.Agent).Return("panic: bad config\n", "", 0)

	err := h.e.Update(context.Background(), UpdateOptions{Yes: true})

	var pe *ProblemsError
	require.ErrorAs(t, err, &pe)
	assert.Contains(t, resultRow(t, h.out.String(), repos.Agent), "not running")

	out := h.out.String()
	assert.Greater(t, strings.Index(out, "panic: bad config"), strings.Index(out, "Result"), "the log lines follow the table")
}

func TestUpdateUnmanagedDoesNotProbe(t *testing.T) {
	h := declined(t)
	h.git.heads["contextmatrix-agent"] = "b9999999999"
	h.e.Probe = func(context.Context, string) error {
		t.Error("an unmanaged service was probed")

		return nil
	}

	require.NoError(t, h.e.Update(context.Background(), UpdateOptions{Yes: true}))

	out := h.out.String()
	assert.Contains(t, resultRow(t, out, repos.Agent), "start by hand")
	assert.Contains(t, out, h.e.L.AgentConfig(), "the start commands still print")
}

func TestUpdateSkipHealthShowsNotChecked(t *testing.T) {
	h := installed(t, true)
	h.git.heads["contextmatrix-agent"] = "b9999999999"
	h.e.SkipHealth = true

	var calls atomic.Int32

	h.e.Probe = func(context.Context, string) error {
		calls.Add(1)

		return nil
	}

	require.NoError(t, h.e.Update(context.Background(), UpdateOptions{Yes: true}))

	assert.Zero(t, calls.Load())
	assert.Contains(t, resultRow(t, h.out.String(), repos.Agent), "not checked")
}

// A docker that appears with a broken build leaves no image recorded; the
// next update builds it although no repo moved.
func TestUpdateRetriesAFailedImageBuildAfterDockerAppeared(t *testing.T) {
	h := installed(t, false)
	h.e.Host.Docker = true
	h.images.fail = true

	err := h.e.Update(context.Background(), UpdateOptions{Yes: true})

	var pe *ProblemsError
	require.ErrorAs(t, err, &pe)
	assert.Equal(t, 2, pe.N, "agent and chat")
	assert.NotContains(t, restarts(h), repos.Agent)
	assert.Contains(t, resultRow(t, h.out.String(), repos.Agent), "not started")
	assert.NotContains(t, h.out.String(), "keeps running", "a backend that never ran without docker keeps nothing")

	h.images.fail = false
	h.out.b = nil

	require.NoError(t, h.e.Update(context.Background(), UpdateOptions{Yes: true}))

	assert.Equal(t, []string{"contextmatrix-agent-worker:bbbbbbb", "contextmatrix-chat-worker:ccccccc"}, h.images.built)
	assert.Contains(t, restarts(h), repos.Agent)
	assert.Contains(t, restarts(h), repos.Chat)
}

// clock is a console clock that only a test moves.
type clock struct{ ns atomic.Int64 }

func (c *clock) now() time.Time { return time.Unix(0, c.ns.Load()) }

// slowRestart takes five seconds of the console clock to restart a service.
type slowRestart struct {
	services.Manager

	clock *clock
}

func (m slowRestart) Restart(ctx context.Context, name string) error {
	m.clock.ns.Add(int64(5 * time.Second))

	return m.Manager.Restart(ctx, name)
}

func TestUpdateRestartStepCoversTheRestart(t *testing.T) {
	h := installed(t, true)
	h.git.heads["contextmatrix-agent"] = "b9999999999"

	clk := &clock{}
	h.e.Services = slowRestart{Manager: h.e.Services, clock: clk}

	var screen bytes.Buffer

	c := ui.NewConsole(&screen, ui.NewTheme(ui.Mode{Plain: true, Width: 100}), nil, "")
	c.Now = clk.now

	require.NoError(t, h.e.Update(context.Background(), UpdateOptions{Yes: true, UI: c}))
	c.Close()

	var restart []string

	for _, l := range stepLines(screen.String(), "✓") {
		if f := strings.Fields(l); f[1] == "restart" {
			restart = append(restart, l)
		}
	}

	require.Len(t, restart, 1)
	assert.True(t, strings.HasSuffix(restart[0], "5.0s"), "the step was running while the service restarted: %q", restart[0])
}

func TestUpdateRemovalNoteFollowsTheRestartSteps(t *testing.T) {
	h := installed(t, true)
	h.git.heads["contextmatrix-agent"] = "b9999999999"
	h.images.removeFail = true

	require.NoError(t, h.e.Update(context.Background(), UpdateOptions{Yes: true}))

	out := h.out.String()
	restart := stepLines(out, "✓")
	note := strings.Index(out, "could not remove old image contextmatrix-agent-worker:bbbbbbb")

	require.NotEmpty(t, restart)
	require.GreaterOrEqual(t, note, 0)
	assert.Greater(t, note, strings.Index(out, " restart  "+repos.Agent), "the note comes after the restart step it belongs to")
}

// slowAgentRestart takes longer than the health budget to restart the agent
// and records when that restart returned.
type slowAgentRestart struct {
	services.Manager

	delay    time.Duration
	returned atomic.Int64
}

func (m *slowAgentRestart) Restart(ctx context.Context, name string) error {
	if name == repos.Agent {
		time.Sleep(m.delay)
		defer m.returned.Store(time.Now().UnixNano())
	}

	return m.Manager.Restart(ctx, name)
}

// The budget starts once the last restart has returned, so a service that
// is slow to restart is still probed, and the set ends a budget later.
func TestUpdateHealthBudgetStartsAfterTheLastRestart(t *testing.T) {
	const budget = 200 * time.Millisecond

	h := installed(t, true)
	h.git.heads["contextmatrix"] = "a9999999999"
	h.git.heads["contextmatrix-agent"] = "b9999999999"
	h.e.HealthBudget = budget

	m := &slowAgentRestart{Manager: h.e.Services, delay: 300 * time.Millisecond}
	h.e.Services = m

	var agentCalls, agentAfterRestart atomic.Int32

	h.e.Probe = func(_ context.Context, url string) error {
		if !strings.Contains(url, ":19092/") {
			return errors.New("connection refused")
		}

		if r := m.returned.Load(); r != 0 && time.Now().UnixNano() >= r {
			agentAfterRestart.Add(1)
		}

		if agentCalls.Add(1) <= 2 {
			return errors.New("connection refused")
		}

		return nil
	}

	err := h.e.Update(context.Background(), UpdateOptions{Yes: true})
	sinceLast := time.Since(time.Unix(0, m.returned.Load()))

	var pe *ProblemsError
	require.ErrorAs(t, err, &pe)
	assert.Equal(t, 1, pe.N, "only the server, whose probe never passes")
	assert.Positive(t, agentAfterRestart.Load(), "the agent is probed after its slow restart")
	assert.Contains(t, resultRow(t, h.out.String(), repos.Agent), "healthy")
	assert.Contains(t, resultRow(t, h.out.String(), repos.Server+" "), "connection refused")
	assert.GreaterOrEqual(t, sinceLast, budget, "the server kept trying for a whole budget after the last restart")
	assert.Less(t, sinceLast, budget+time.Second, "and not much longer")
}
