package engine

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mhersson/contextmatrix-setup/internal/bootstrap"
	"github.com/mhersson/contextmatrix-setup/internal/configsync"
	"github.com/mhersson/contextmatrix-setup/internal/images"
	"github.com/mhersson/contextmatrix-setup/internal/layout"
	"github.com/mhersson/contextmatrix-setup/internal/repos"
	"github.com/mhersson/contextmatrix-setup/internal/run"
	"github.com/mhersson/contextmatrix-setup/internal/state"
)

const bootstrapWait = 60 * time.Second

func (e *Engine) Install(ctx context.Context, a Answers) error {
	a.Normalize()

	if err := a.Validate(); err != nil {
		return err
	}

	if a.Services && e.Services.Kind() == "none" {
		a.Services = false

		reason := e.Host.ServiceManagerReason
		if reason == "" {
			reason = "none detected"
		}

		e.logf("services: no usable service manager (%s); start commands are printed instead", reason)
	}

	e.printSearchNotice()

	st, _, err := state.Load(e.L.StateFile())
	if err != nil {
		return err
	}

	newArchitect := st.Repos[repos.Architect].Commit == ""

	heads := map[string]string{}

	// One repo at a time, so each head line follows that repo's git output.
	for _, name := range repos.Apps {
		head, err := e.syncRepos(ctx, []string{name}, e.Out)
		if err != nil {
			return err
		}

		heads[name] = head[name]
		e.logf("%-*s %s", repos.NameWidth(), name, repos.Short(head[name]))
	}

	for _, repo := range repos.Apps {
		say := func(cmd string) { e.logf("%-*s %s", repos.NameWidth(), repo, cmd) }
		if err := e.buildBinary(ctx, repo, e.Out, say); err != nil {
			return err
		}
	}

	if err := e.makeDirs(); err != nil {
		return err
	}

	// A migrated or partially installed config already carries values the
	// merge prefers; what the user changed since is forced over them.
	existing := e.loadTrees()
	prefill, _ := AnswersFrom(existing)
	// The wizard normalizes what it returns, so the carried answers are
	// compared in the same form or a boards dir named "boards" looks changed.
	prefill.Normalize()

	facts, err := e.freshFacts(ctx, a, existing)
	if err != nil {
		return err
	}

	if e.Host.Docker {
		for _, repo := range []string{repos.Agent, repos.Chat} {
			b, err := e.Images.Build(ctx, e.L.SrcDir(repo), repo, heads[repo], e.Out)
			if err != nil {
				return err
			}

			st.Images[images.Family(repo)] = state.Image{Tag: b.Tag, ID: b.ID, Variants: b.Variants}

			if repo == repos.Agent {
				facts.AgentImage = b.Tag
			} else {
				facts.ChatImage = b.Tag
			}
		}
	} else {
		e.logf("docker not available: worker images skipped, backends disabled until the next update finds docker")

		if e.Host.DockerHint != "" {
			e.logf("%s", e.Host.DockerHint)
		}
	}

	trees := Opinionated(a, facts)
	force := forcedAnswers(a, prefill, trees)
	results := map[string]configResult{}

	if newArchitect {
		fillArchitectBlock(force[repos.Server], existing.Server, trees.Server)
	}

	// A migrated config names the image its old install ran; the one built
	// here is the only one this install maintains.
	if facts.AgentImage != "" {
		force[repos.Agent]["base_image"] = facts.AgentImage
	}

	if facts.ChatImage != "" {
		force[repos.Chat]["base_image"] = facts.ChatImage
	}

	for repo, op := range map[string]configsync.Tree{repos.Server: trees.Server, repos.Agent: trees.Agent, repos.Chat: trees.Chat, repos.Architect: trees.Architect} {
		res, err := e.writeConfig(ctx, repo, op, force[repo])
		if err != nil {
			return err
		}

		for _, d := range res.Dropped {
			e.logf("%-*s %s", repos.NameWidth(), filepath.Base(res.Path), droppedText(d))
		}

		results[repo] = res
		st.Configs[filepath.Base(res.Path)] = state.ConfigHash{SHA256: res.Hash}

		e.logf("%-*s written", repos.NameWidth(), filepath.Base(res.Path))
	}

	files, _, err := e.copyWorkflowSkills(nil)
	if err != nil {
		return err
	}

	st.WorkflowSkills = state.WorkflowSkills{Commit: heads[repos.Server], Files: files}

	// Read GitHub readiness from the merged file, not the answers: after a
	// migration the block is carried over while the answers say "skip".
	github := githubConfigured(results[repos.Server].Tree)

	invalid := e.validateAll(ctx, results, github)
	valid := map[string]bool{}

	for _, repo := range repos.Apps {
		valid[repo] = invalid[repo] == nil

		if err := invalid[repo]; err != nil {
			e.logf("%-*s invalid: %v", repos.NameWidth(), filepath.Base(results[repo].Path), err)
		}
	}

	for _, w := range configsync.CrossCheck(results[repos.Server].Tree, results[repos.Agent].Tree, results[repos.Chat].Tree, results[repos.Architect].Tree) {
		e.logf("warning: %s", w)
	}

	// The server logs the admin link once, at startup, so the log must be
	// followed before the server is started or the line is missed.
	var watch *linkWatch

	if a.AuthMode == "multi" && a.Services && valid[repos.Server] && github {
		watch = e.followLink(ctx)
		defer watch.stop()
	}

	started := map[string]bool{}
	hold := map[string]string{}

	if a.Services {
		if err := e.installUnits(ctx, mergedTrees(results)); err != nil {
			return err
		}

		for _, n := range e.disableHeldBack(ctx, github, st.Images) {
			e.logf("%-*s %s", repos.NameWidth(), n.name, n.text)
		}

		if a.Linger && e.Host.OS == "linux" {
			e.enableLinger(ctx)
		}

		if reason := e.holdNewArchitect(ctx, newArchitect, results[repos.Architect].Tree); reason != "" {
			hold[repos.Architect] = reason
		}

		started = e.startEligible(ctx, github, valid, hold)
	} else {
		e.printStartCommands()
	}

	e.announce(ctx, a, started[repos.Server], watch)

	st.OS = e.Host.OS
	st.Docker = e.Host.Docker

	// The host may well have a service manager; what is recorded is whether
	// this installation uses one, so later runs stay off it.
	st.ServiceManager = "none"
	if a.Services {
		st.ServiceManager = e.Services.Kind()
	}

	for _, repo := range repos.Apps {
		// A held repo stays unrecorded, so the next update treats it as new
		// and checks again.
		if hold[repo] != "" {
			continue
		}

		st.Repos[repo] = state.Repo{Commit: heads[repo], InstalledAt: e.now()}
	}

	// Repeated last, so the build output above cannot scroll it away.
	e.printSearchNotice()

	return st.Save(e.L.StateFile())
}

func (e *Engine) makeDirs() error {
	for _, d := range e.L.RuntimeDirs() {
		if err := os.MkdirAll(d, 0o700); err != nil {
			return fmt.Errorf("create %s: %w", d, err)
		}
	}

	return nil
}

func (e *Engine) freshFacts(ctx context.Context, a Answers, existing Trees) (Facts, error) {
	// A migrated or partially installed config already carries the shared
	// secrets and the instance id, and the three files are paired against
	// them; only what is missing is generated.
	keys, id, err := e.resolveIdentity(existing)
	if err != nil {
		return Facts{}, err
	}

	f := Facts{Layout: e.L, Gateway: e.gateway(ctx), Docker: e.Host.Docker, Keys: keys, InstanceID: id}

	if a.GitHubMode == "app" {
		// A carried path may use the ~ form the server accepts for this key.
		src := a.GitHubKeyFile
		if strings.HasPrefix(src, "~/") {
			src = filepath.Join(e.L.Home, src[2:])
		}

		f.GitHubKey = filepath.Join(e.L.ServerStateDir(), "github-app.pem")
		if err := copyFile(src, f.GitHubKey); err != nil {
			return Facts{}, fmt.Errorf("copy github app key: %w", err)
		}
	}

	return f, nil
}

// forcedAnswers lists the keys whose answer differs from what the existing
// config prefilled, with the value the opinionated tree gives them. The
// merge prefers a present user value, so a value changed in the wizard or
// given as a flag would otherwise lose to the carried one. On a fresh
// install nothing is carried and a difference is always real input.
func forcedAnswers(a, prefill Answers, trees Trees) map[string]map[string]any {
	force := map[string]map[string]any{repos.Server: {}, repos.Agent: {}, repos.Chat: {}, repos.Architect: {}}

	take := func(repo string, tree configsync.Tree, keys ...string) {
		for _, k := range keys {
			if v, ok := configsync.Get(tree, k); ok {
				force[repo][k] = v
			}
		}
	}

	server := func(keys ...string) { take(repos.Server, trees.Server, keys...) }
	agent := func(keys ...string) { take(repos.Agent, trees.Agent, keys...) }
	chat := func(keys ...string) { take(repos.Chat, trees.Chat, keys...) }
	architect := func(keys ...string) { take(repos.Architect, trees.Architect, keys...) }

	if a.AuthMode != prefill.AuthMode {
		server("auth.mode")
	}

	if a.ServerPort != prefill.ServerPort {
		server("port")
		agent("contextmatrix_url", "container_contextmatrix_url")
		chat("contextmatrix_url", "container_contextmatrix_url")
		architect("contextmatrix_url")
	}

	if a.AgentPort != prefill.AgentPort {
		server("backends.agent.url")
		agent("port")
	}

	if a.ChatPort != prefill.ChatPort {
		server("backends.chat.url")
		chat("port")
	}

	if a.ArchitectPort != prefill.ArchitectPort {
		server("backends.architect.url")
		architect("port")
	}

	if a.DefaultModel != prefill.DefaultModel {
		server("backends.agent.default_model", "backends.chat.default_model")
		agent("default_model")
	}

	if a.ArchitectModel != prefill.ArchitectModel {
		server("backends.architect.default_model")
	}

	// An unchanged key means the answers still describe the carried
	// endpoint, whatever its type; forcing would overwrite an OpenAI
	// endpoint with the OpenRouter default.
	if a.OpenRouterKey != "" && a.OpenRouterKey != prefill.OpenRouterKey {
		server("llm_endpoint.type", "llm_endpoint.api_key")
	}

	if a.AAKey != "" {
		server("backends.agent.aa_api_key")
	}

	// The server rejects a populated block for the mode not in use, so a
	// mode switch blanks the other block.
	switch a.GitHubMode {
	case "pat":
		server("github.auth_mode", "github.pat.token")

		force[repos.Server]["github.app.app_id"] = int64(0)
		force[repos.Server]["github.app.installation_id"] = int64(0)
		force[repos.Server]["github.app.private_key_path"] = ""
	case "app":
		server("github.auth_mode", "github.app.app_id", "github.app.installation_id", "github.app.private_key_path")

		force[repos.Server]["github.pat.token"] = ""
	}

	if a.BoardsURL != prefill.BoardsURL || a.BoardsName != prefill.BoardsName {
		server("boards.dir", "boards.git_remote_url", "boards.git_clone_on_empty", "boards.git_auto_push")
	}

	if a.TaskSkillsURL != prefill.TaskSkillsURL {
		server("task_skills.dir", "task_skills.git_remote_url", "task_skills.git_clone_on_empty")
	}

	return force
}

// copyFile copies src to dst through a temp file next to dst. A source that
// already is the destination, directly or through a symlink, is left alone:
// opening it for truncation would empty it before a byte is read.
func copyFile(src, dst string) error {
	same, err := samePath(src, dst)
	if err != nil {
		return err
	}

	if same {
		return nil
	}

	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	tmp := dst + ".tmp"

	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}

	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		_ = os.Remove(tmp)

		return err
	}

	if err := out.Close(); err != nil {
		_ = os.Remove(tmp)

		return err
	}

	return os.Rename(tmp, dst)
}

// samePath reports whether two paths name one file once absolute and with
// symlinks resolved; a path that does not exist yet stands for itself.
func samePath(a, b string) (bool, error) {
	resolve := func(p string) (string, error) {
		abs, err := filepath.Abs(p)
		if err != nil {
			return "", err
		}

		if resolved, err := filepath.EvalSymlinks(abs); err == nil {
			return resolved, nil
		}

		return abs, nil
	}

	ra, err := resolve(a)
	if err != nil {
		return false, err
	}

	rb, err := resolve(b)
	if err != nil {
		return false, err
	}

	return ra == rb, nil
}

// validateAll runs each app's own validation, nil meaning valid, except for
// a repo with a skip reason, which the first run after the reason is gone
// validates. A failure never touches the file; the user fixes the value and
// reruns.
func (e *Engine) validateAll(ctx context.Context, results map[string]configResult, github bool) map[string]error {
	errs := map[string]error{}

	for _, repo := range repos.Apps {
		if e.skipReason(repo, github) != "" {
			continue
		}

		errs[repo] = configsync.Validate(ctx, e.R, e.Host.Binary(repo), results[repo].Path)
	}

	return errs
}

func (e *Engine) installUnits(ctx context.Context, trees Trees) error {
	for _, repo := range repos.Apps {
		changed, err := e.Services.Install(ctx, e.serviceFor(repo, trees))
		if err != nil {
			return fmt.Errorf("install %s unit: %w", repo, err)
		}

		if changed {
			e.logf("%-*s unit written", repos.NameWidth(), repo)
		}
	}

	return nil
}

func (e *Engine) enableLinger(ctx context.Context) {
	res, err := e.R.Run(ctx, run.Cmd{Name: "loginctl", Args: []string{"enable-linger"}})
	if err != nil || res.ExitCode != 0 {
		e.logf("loginctl enable-linger failed; services will run while you are logged in")
	}
}

// startEligible starts what can run and reports what it started: a repo
// with no skip reason and a valid file. A repo in hold is not started, for
// the reason given.
func (e *Engine) startEligible(ctx context.Context, github bool, valid map[string]bool, hold map[string]string) map[string]bool {
	started := map[string]bool{}

	for _, repo := range repos.Apps {
		switch skip := e.skipReason(repo, github); {
		case skip != "":
			e.logf("%-*s not started: %s", repos.NameWidth(), repo, skip)
		case !valid[repo]:
			e.logf("%-*s not started: config invalid", repos.NameWidth(), repo)
		case hold[repo] != "":
			e.logf("%-*s not started: %s", repos.NameWidth(), repo, hold[repo])
		default:
			if err := e.Services.Start(ctx, repo); err != nil {
				e.logf("%-*s start failed: %v", repos.NameWidth(), repo, err)
			} else {
				e.logf("%-*s started", repos.NameWidth(), repo)

				started[repo] = true
			}
		}
	}

	return started
}

func (e *Engine) printStartCommands() {
	for _, line := range e.startCommands() {
		e.logf("%s", line)
	}
}

func (e *Engine) startCommands() []string {
	return []string{
		"services not installed; start by hand with:",
		fmt.Sprintf("  %s -config %s", e.Host.Binary(repos.Server), e.L.ServerConfig()),
		fmt.Sprintf("  %s serve --config %s", e.Host.Binary(repos.Agent), e.L.AgentConfig()),
		fmt.Sprintf("  %s serve --config %s", e.Host.Binary(repos.Chat), e.L.ChatConfig()),
		fmt.Sprintf("  %s serve --config %s", e.Host.Binary(repos.Architect), e.L.ArchitectConfig()),
	}
}

// linkWatch follows the server log for the one-time admin link in the
// background, from before the server is started until the link is seen,
// the timeout passes or stop is called.
type linkWatch struct {
	cancel context.CancelFunc
	done   chan struct{}
	path   string
	err    error
}

func (e *Engine) followLink(ctx context.Context) *linkWatch {
	ctx, cancel := context.WithCancel(ctx)
	w := &linkWatch{cancel: cancel, done: make(chan struct{})}

	go func() {
		defer close(w.done)

		w.path, w.err = bootstrap.Wait(ctx, func(ctx context.Context, out io.Writer) error {
			return e.Services.Follow(ctx, repos.Server, out)
		}, bootstrapWait)
	}()

	return w
}

func (w *linkWatch) wait() (string, error) {
	<-w.done
	w.cancel()

	return w.path, w.err
}

func (w *linkWatch) stop() {
	w.cancel()
	<-w.done
}

// announce prints the server URL and, on a first multi-mode start, waits
// for the admin link the watch is following and opens it. A server that
// was not started cannot log the link, so nothing is waited for then.
func (e *Engine) announce(ctx context.Context, a Answers, serverStarted bool, watch *linkWatch) {
	base := fmt.Sprintf("http://localhost:%d", a.ServerPort)

	if watch == nil || !serverStarted {
		if watch != nil {
			watch.stop()
		}

		e.logf("server URL: %s", base)

		return
	}

	path, err := watch.wait()
	if errors.Is(err, bootstrap.ErrNoLink) {
		e.logf("server URL: %s", base)
		e.logf("no first-admin link seen within %s; the server logs it at its first start, read it with:", bootstrapWait)
		e.logf("  %s", e.linkCommand())

		return
	}

	url := bootstrap.URL(a.ServerPort, path)
	e.logf("create the first admin account here: %s", url)

	if err := e.Browser(ctx, url); err != nil {
		e.logf("could not open a browser (%v); open the link by hand", err)
	}
}

func (e *Engine) linkCommand() string {
	if e.Host.OS == "darwin" {
		log := layout.Tilde(e.L, filepath.Join(e.L.MacLogsDir(), repos.Server+".log"))

		return "grep 'bootstrap link' " + log
	}

	return "journalctl --user -u " + repos.Server + " -n 50 | grep 'bootstrap link'"
}
