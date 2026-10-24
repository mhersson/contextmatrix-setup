package engine

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mhersson/contextmatrix-setup/internal/configsync"
	"github.com/mhersson/contextmatrix-setup/internal/repos"
	"github.com/mhersson/contextmatrix-setup/internal/state"
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

func TestUpdateFailedImageBuildSkipsRestart(t *testing.T) {
	h := installed(t, true)
	h.git.heads["contextmatrix-agent"] = "b9999999999"
	h.images.fail = true

	require.NoError(t, h.e.Update(context.Background(), UpdateOptions{Yes: true}))

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

// A held architect's unit stays enabled, so the init system can start it; its
// own listener must not hold it again.
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
