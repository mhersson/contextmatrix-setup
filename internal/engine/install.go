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
	"github.com/mhersson/contextmatrix-setup/internal/services"
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

	st, _, err := state.Load(e.L.StateFile())
	if err != nil {
		return err
	}

	heads, err := e.syncRepos(ctx, repos.Apps)
	if err != nil {
		return err
	}

	for _, repo := range repos.Apps {
		if err := e.buildBinary(ctx, repo); err != nil {
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
			tag, id, err := e.Images.Build(ctx, e.L.SrcDir(repo), repo, heads[repo], e.Out)
			if err != nil {
				return err
			}

			st.Images[images.Family(repo)] = state.Image{Tag: tag, ID: id}

			if repo == repos.Agent {
				facts.AgentImage = tag
			} else {
				facts.ChatImage = tag
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

	// A migrated config names the image its old install ran; the one built
	// here is the only one this install maintains.
	if facts.AgentImage != "" {
		force[repos.Agent]["base_image"] = facts.AgentImage
	}

	if facts.ChatImage != "" {
		force[repos.Chat]["base_image"] = facts.ChatImage
	}

	for repo, op := range map[string]configsync.Tree{repos.Server: trees.Server, repos.Agent: trees.Agent, repos.Chat: trees.Chat} {
		res, err := e.writeConfig(ctx, repo, op, force[repo])
		if err != nil {
			return err
		}

		results[repo] = res
		st.Configs[filepath.Base(res.Path)] = state.ConfigHash{SHA256: res.Hash}

		e.logf("%-22s written", filepath.Base(res.Path))
	}

	files, _, err := e.copyWorkflowSkills(nil)
	if err != nil {
		return err
	}

	st.WorkflowSkills = state.WorkflowSkills{Commit: heads[repos.Server], Files: files}

	valid := e.validateAll(ctx, results)

	for _, w := range configsync.CrossCheck(results[repos.Server].Tree, results[repos.Agent].Tree, results[repos.Chat].Tree) {
		e.logf("warning: %s", w)
	}

	// Read GitHub readiness from the merged file, not the answers: after a
	// migration the block is carried over while the answers say "skip".
	github := githubConfigured(results[repos.Server].Tree)

	// The server logs the admin link once, at startup, so the log must be
	// followed before the server is started or the line is missed.
	var watch *linkWatch

	if a.AuthMode == "multi" && a.Services && valid[repos.Server] && github {
		watch = e.followLink(ctx)
		defer watch.stop()
	}

	started := map[string]bool{}

	if a.Services {
		if err := e.installUnits(ctx, results[repos.Server].Tree); err != nil {
			return err
		}

		if a.Linger && e.Host.OS == "linux" {
			e.enableLinger(ctx)
		}

		started = e.startEligible(ctx, github, valid)
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
		st.Repos[repo] = state.Repo{Commit: heads[repo], InstalledAt: e.now()}
	}

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
	force := map[string]map[string]any{repos.Server: {}, repos.Agent: {}, repos.Chat: {}}

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

	if a.AuthMode != prefill.AuthMode {
		server("auth.mode")
	}

	if a.ServerPort != prefill.ServerPort {
		server("port")
		agent("contextmatrix_url", "container_contextmatrix_url")
		chat("contextmatrix_url", "container_contextmatrix_url")
	}

	if a.AgentPort != prefill.AgentPort {
		server("backends.agent.url")
		agent("port")
	}

	if a.ChatPort != prefill.ChatPort {
		server("backends.chat.url")
		chat("port")
	}

	if a.DefaultModel != prefill.DefaultModel {
		server("backends.agent.default_model", "backends.chat.default_model")
		agent("default_model")
	}

	if a.OpenRouterKey != "" {
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

// validateAll runs each app's own validation and reports failures without
// touching the file; the user fixes the value and reruns.
func (e *Engine) validateAll(ctx context.Context, results map[string]configResult) map[string]bool {
	valid := map[string]bool{}

	for _, repo := range repos.Apps {
		err := configsync.Validate(ctx, e.R, e.Host.Binary(binaryFor(repo)), results[repo].Path)
		valid[repo] = err == nil

		if err != nil {
			e.logf("%-22s invalid: %v", filepath.Base(results[repo].Path), err)
		}
	}

	return valid
}

func (e *Engine) installUnits(ctx context.Context, server configsync.Tree) error {
	for _, repo := range repos.Apps {
		changed, err := e.Services.Install(ctx, e.serviceFor(repo, server))
		if err != nil {
			return fmt.Errorf("install %s unit: %w", repo, err)
		}

		if changed {
			e.logf("%-22s unit written", repo)
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

// startEligible starts what can run and reports what it started: the
// server only with GitHub configured and a valid file, backends only with
// docker and a valid file.
func (e *Engine) startEligible(ctx context.Context, github bool, valid map[string]bool) map[string]bool {
	started := map[string]bool{}

	for _, repo := range repos.Apps {
		switch {
		case !valid[repo]:
			e.logf("%-22s not started: config invalid", repo)
		case repo == repos.Server && !github:
			e.logf("%-22s not started: fill in the github block of server.yaml, then run update", repo)
		case repo != repos.Server && !e.Host.Docker:
			e.logf("%-22s not started: docker not available", repo)
		default:
			if err := e.Services.Start(ctx, repo); err != nil {
				e.logf("%-22s start failed: %v", repo, err)
			} else {
				e.logf("%-22s started", repo)

				started[repo] = true
			}
		}
	}

	return started
}

func (e *Engine) printStartCommands() {
	e.logf("services not installed; start by hand with:")
	e.logf("  %s -config %s", e.Host.Binary(repos.Server), e.L.ServerConfig())
	e.logf("  %s serve --config %s", e.Host.Binary(repos.Agent), e.L.AgentConfig())
	e.logf("  %s serve --config %s", e.Host.Binary(repos.Chat), e.L.ChatConfig())
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
			return e.Services.Follow(ctx, services.Server, out)
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
		log := layout.Tilde(e.L, filepath.Join(e.L.MacLogsDir(), services.Server+".log"))

		return "grep 'bootstrap link' " + log
	}

	return "journalctl --user -u " + services.Server + " -n 50 | grep 'bootstrap link'"
}
