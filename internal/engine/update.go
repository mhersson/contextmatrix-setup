package engine

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mhersson/contextmatrix-setup/internal/configsync"
	"github.com/mhersson/contextmatrix-setup/internal/images"
	"github.com/mhersson/contextmatrix-setup/internal/repos"
	"github.com/mhersson/contextmatrix-setup/internal/state"
)

type UpdateOptions struct {
	Yes     bool
	Confirm func(summary string) bool
}

func (e *Engine) Update(ctx context.Context, opts UpdateOptions) error {
	st, found, err := state.Load(e.L.StateFile())
	if err != nil {
		return err
	}

	if !found || !st.Installed() {
		return errors.New("nothing installed yet: run contextmatrix-setup install first")
	}

	e.useRecordedManager(st.ServiceManager)
	unmanaged := e.Services.Kind() == "none"

	heads, err := e.syncRepos(ctx, repos.Apps)
	if err != nil {
		return err
	}

	moved := map[string]bool{}

	var summary strings.Builder

	for _, repo := range repos.Apps {
		old := st.Repos[repo].Commit
		if heads[repo] == old {
			continue
		}

		moved[repo] = true

		log, err := e.Git.Log(ctx, e.L.SrcDir(repo), old, heads[repo])
		if err != nil {
			return err
		}

		fmt.Fprintf(&summary, "%s %s..%s\n%s", repo, repos.Short(old), repos.Short(heads[repo]), log)
	}

	if len(moved) > 0 {
		e.logf("%s", summary.String())

		if !opts.Yes && opts.Confirm != nil && !opts.Confirm(summary.String()) {
			e.logf("update cancelled; nothing changed")

			return nil
		}
	}

	for _, repo := range repos.Apps {
		if moved[repo] {
			if err := e.buildBinary(ctx, repo); err != nil {
				return err
			}
		}
	}

	dockerAppeared := !st.Docker && e.Host.Docker
	if dockerAppeared {
		e.logf("docker found: building worker images and enabling the backends")
	}

	// Existing values feed the opinionated tree so nothing is regenerated.
	existing := e.loadTrees()
	answers, _ := AnswersFrom(existing)

	keys, instanceID, err := e.resolveIdentity(existing)
	if err != nil {
		return err
	}

	facts := Facts{Layout: e.L, Gateway: e.gateway(ctx), Docker: e.Host.Docker, Keys: keys, InstanceID: instanceID}

	force := map[string]map[string]any{repos.Server: {}, repos.Agent: {}, repos.Chat: {}}
	oldTags := map[string]string{}
	imageChanged := map[string]bool{}
	buildFailed := map[string]bool{}

	if e.Host.Docker {
		for _, repo := range []string{repos.Agent, repos.Chat} {
			if !moved[repo] && !dockerAppeared {
				continue
			}

			family := images.Family(repo)
			oldTags[repo] = st.Images[family].Tag

			tag, id, err := e.Images.Build(ctx, e.L.SrcDir(repo), repo, heads[repo], e.Out)
			if err != nil {
				e.logf("%-22s image build failed, keeping %s: %v", repo, oldTags[repo], err)

				buildFailed[repo] = true

				continue
			}

			st.Images[family] = state.Image{Tag: tag, ID: id}
			force[repo]["base_image"] = tag
			imageChanged[repo] = tag != oldTags[repo]
		}
	}

	if dockerAppeared {
		force[repos.Server]["backends.agent.enabled"] = true
		force[repos.Server]["backends.chat.enabled"] = true
	}

	trees := Opinionated(answers, facts)
	results := map[string]configResult{}
	configChanged := map[string]bool{}
	handEdited := map[string]bool{}

	for repo, op := range map[string]configsync.Tree{repos.Server: trees.Server, repos.Agent: trees.Agent, repos.Chat: trees.Chat} {
		name := filepath.Base(e.L.ConfigFor(repo))

		if prev, ok := st.Configs[name]; ok {
			// A hand edit that survives the merge re-encodes to the bytes
			// already on disk, so only this hash reveals that the running
			// service is out of date with its file.
			if data, err := os.ReadFile(e.L.ConfigFor(repo)); err == nil && configsync.Hash(data) != prev.SHA256 {
				e.logf("%-22s edited by hand since the last run; values kept", name)

				handEdited[repo] = true
			}
		}

		res, err := e.writeConfig(ctx, repo, op, force[repo])
		if err != nil {
			return err
		}

		results[repo] = res
		configChanged[repo] = res.Changed
		st.Configs[name] = state.ConfigHash{SHA256: res.Hash}

		if res.Changed {
			e.logf("%-22s updated", name)
		}
	}

	if moved[repos.Server] {
		changed, err := e.Git.PathChanged(ctx, e.L.SrcDir(repos.Server), st.Repos[repos.Server].Commit, heads[repos.Server], "workflow-skills")
		if err != nil {
			return err
		}

		if changed {
			files, skipped, err := e.copyWorkflowSkills(st.WorkflowSkills.Files)
			if err != nil {
				return err
			}

			for _, s := range skipped {
				e.logf("%-22s %s locally modified, not overwritten", "workflow-skills", s)
			}

			st.WorkflowSkills = state.WorkflowSkills{Commit: heads[repos.Server], Files: files}

			e.logf("%-22s updated", "workflow-skills")
		}
	}

	valid := e.validateAll(ctx, results)

	for _, w := range configsync.CrossCheck(results[repos.Server].Tree, results[repos.Agent].Tree, results[repos.Chat].Tree) {
		e.logf("warning: %s", w)
	}

	unitChanged := map[string]bool{}

	if !unmanaged {
		for _, repo := range repos.Apps {
			changed, err := e.Services.Install(ctx, e.serviceFor(repo, results[repos.Server].Tree))
			if err != nil {
				return err
			}

			unitChanged[repo] = changed
		}
	}

	github := githubConfigured(results[repos.Server].Tree)
	anything := false

	for _, repo := range repos.Apps {
		needs := moved[repo] || configChanged[repo] || handEdited[repo] || imageChanged[repo] || unitChanged[repo]
		if !needs {
			continue
		}

		anything = true

		switch {
		case buildFailed[repo]:
			e.logf("%-22s not restarted: image build failed", repo)
		case !valid[repo]:
			e.logf("%-22s not restarted: config invalid", repo)
		case repo == repos.Server && !github:
			e.logf("%-22s not started: github block missing in server.yaml", repo)
		case repo != repos.Server && !e.Host.Docker:
			e.logf("%-22s not started: docker not available", repo)
		case unmanaged:
			// Nothing to restart, and the old image stays until the user
			// restarts the process by hand.
			continue
		default:
			if err := e.Services.Restart(ctx, repo); err != nil {
				e.logf("%-22s restart failed: %v", repo, err)
				e.printRecentLogs(ctx, repo)

				continue
			}

			e.logf("%-22s restarted", repo)

			if old := oldTags[repo]; old != "" && imageChanged[repo] {
				if err := e.Images.RemoveTag(ctx, old); err != nil {
					e.logf("%-22s could not remove old image %s: %v", repo, old, err)
				}
			}
		}
	}

	if anything && unmanaged {
		e.printStartCommands()
	}

	if !anything && len(moved) == 0 {
		e.logf("up to date")
	}

	st.Docker = e.Host.Docker
	st.ServiceManager = e.Services.Kind()

	for _, repo := range repos.Apps {
		// A repo whose image did not build stays on its recorded commit, so
		// the next run sees it as moved again and retries the build.
		if buildFailed[repo] {
			continue
		}

		st.Repos[repo] = state.Repo{Commit: heads[repo], InstalledAt: e.now()}
	}

	return st.Save(e.L.StateFile())
}

func (e *Engine) loadTrees() Trees {
	load := func(p string) configsync.Tree {
		t, _, err := configsync.LoadFile(p)
		if err != nil || t == nil {
			return configsync.Tree{}
		}

		return t
	}

	return Trees{Server: load(e.L.ServerConfig()), Agent: load(e.L.AgentConfig()), Chat: load(e.L.ChatConfig())}
}

// AnswersFrom rebuilds answers from installed or migrated config trees and
// reports which of them the trees held. The update flow uses the answers for
// opinionated values (existing user values win over them anyway); the wizard
// prefills from them and skips what is known after a migration.
func AnswersFrom(t Trees) (Answers, Known) {
	a := DefaultAnswers()

	var k Known

	str := func(tr configsync.Tree, path string) string {
		v, _ := configsync.Get(tr, path)
		s, _ := v.(string)

		return s
	}

	port := func(tr configsync.Tree, path string, fallback int) (int, bool) {
		v, ok := configsync.Get(tr, path)
		if !ok {
			return fallback, false
		}

		if n, ok := v.(int); ok && n > 0 {
			return n, true
		}

		return fallback, false
	}

	var server, agent, chat bool

	a.ServerPort, server = port(t.Server, "port", a.ServerPort)
	a.AgentPort, agent = port(t.Agent, "port", a.AgentPort)
	a.ChatPort, chat = port(t.Chat, "port", a.ChatPort)
	k.Ports = server && agent && chat

	if s := str(t.Server, "auth.mode"); s != "" {
		a.AuthMode, k.AuthMode = s, true
	}

	if s := str(t.Server, "backends.agent.default_model"); s != "" {
		a.DefaultModel, k.DefaultModel = s, true
	}

	if s := str(t.Server, "llm_endpoint.api_key"); s != "" {
		a.OpenRouterKey, k.OpenRouterKey = s, true
	}

	if s := str(t.Server, "backends.agent.aa_api_key"); s != "" {
		a.AAKey, k.AAKey = s, true
	}

	switch mode := str(t.Server, "github.auth_mode"); mode {
	case "pat":
		a.GitHubMode = mode
		a.GitHubPAT = str(t.Server, "github.pat.token")
	case "app":
		a.GitHubMode = mode
		a.GitHubAppID = integer(t.Server, "github.app.app_id")
		a.GitHubInstallID = integer(t.Server, "github.app.installation_id")
		a.GitHubKeyFile = str(t.Server, "github.app.private_key_path")
	}

	k.GitHub = a.githubComplete()

	a.TaskSkillsURL = str(t.Server, "task_skills.git_remote_url")
	k.TaskSkills = a.TaskSkillsURL != "" || str(t.Server, "task_skills.dir") != ""

	// An old config may list several boards; the answers describe the first.
	boards := t.Server["boards"]
	if list, ok := boards.([]any); ok && len(list) > 0 {
		boards = list[0]
	}

	if entry, ok := boards.(map[string]any); ok {
		a.BoardsURL = str(entry, "git_remote_url")

		if dir := str(entry, "dir"); dir != "" {
			a.BoardsName = filepath.Base(dir)
			k.Boards = true
		}

		k.Boards = k.Boards || a.BoardsURL != ""
	}

	return a, k
}

// integer reads a whole number the YAML decoder or a Set call may have typed
// differently.
func integer(tr configsync.Tree, path string) int64 {
	v, _ := configsync.Get(tr, path)

	switch n := v.(type) {
	case int:
		return int64(n)
	case int64:
		return n
	case float64:
		return int64(n)
	default:
		return 0
	}
}

func (e *Engine) printRecentLogs(ctx context.Context, repo string) {
	logs, err := e.Services.Logs(ctx, repo, 20)
	if err != nil || strings.TrimSpace(logs) == "" {
		return
	}

	e.logf("%s", logs)
}
