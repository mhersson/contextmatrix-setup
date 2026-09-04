package engine

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/mhersson/contextmatrix-setup/internal/bootstrap"
	"github.com/mhersson/contextmatrix-setup/internal/configsync"
	"github.com/mhersson/contextmatrix-setup/internal/images"
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

	facts, err := e.freshFacts(ctx, a)
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
	}

	trees := Opinionated(a, facts)
	results := map[string]configResult{}

	for repo, op := range map[string]configsync.Tree{repos.Server: trees.Server, repos.Agent: trees.Agent, repos.Chat: trees.Chat} {
		res, err := e.writeConfig(ctx, repo, op, nil)
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

	if a.Services {
		if err := e.installUnits(ctx, results[repos.Server].Tree); err != nil {
			return err
		}

		if a.Linger && e.Host.OS == "linux" {
			e.enableLinger(ctx)
		}

		e.startEligible(ctx, github, valid)
	} else {
		e.printStartCommands()
	}

	e.announce(ctx, a, valid[repos.Server] && a.Services && github)

	st.OS = e.Host.OS
	st.ServiceManager = e.Services.Kind()
	st.Docker = e.Host.Docker

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

func (e *Engine) freshFacts(ctx context.Context, a Answers) (Facts, error) {
	keys, err := NewKeys()
	if err != nil {
		return Facts{}, err
	}

	id, err := NewInstanceID(e.Host.Hostname)
	if err != nil {
		return Facts{}, err
	}

	f := Facts{Layout: e.L, Gateway: e.gateway(ctx), Docker: e.Host.Docker, Keys: keys, InstanceID: id}

	if a.GitHubMode == "app" {
		f.GitHubKey = filepath.Join(e.L.ServerStateDir(), "github-app.pem")
		if err := copyFile(a.GitHubKeyFile, f.GitHubKey); err != nil {
			return Facts{}, fmt.Errorf("copy github app key: %w", err)
		}
	}

	return f, nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}

	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()

		return err
	}

	return out.Close()
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

// startEligible starts what can run: the server only with GitHub configured
// and a valid file, backends only with docker and a valid file.
func (e *Engine) startEligible(ctx context.Context, github bool, valid map[string]bool) {
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
			}
		}
	}
}

func (e *Engine) printStartCommands() {
	e.logf("services not installed; start by hand with:")
	e.logf("  %s -config %s", e.Host.Binary(repos.Server), e.L.ServerConfig())
	e.logf("  %s serve --config %s", e.Host.Binary(repos.Agent), e.L.AgentConfig())
	e.logf("  %s serve --config %s", e.Host.Binary(repos.Chat), e.L.ChatConfig())
}

// announce prints the server URL and, on a first multi-mode start, follows
// the log for the one-time admin link and opens it.
func (e *Engine) announce(ctx context.Context, a Answers, serverStarted bool) {
	base := fmt.Sprintf("http://localhost:%d", a.ServerPort)

	if a.AuthMode != "multi" || !serverStarted {
		e.logf("server URL: %s", base)

		return
	}

	follow := func(ctx context.Context, w io.Writer) error {
		return e.Services.Follow(ctx, services.Server, w)
	}

	path, err := bootstrap.Wait(ctx, follow, bootstrapWait)
	if errors.Is(err, bootstrap.ErrNoLink) {
		e.logf("server URL: %s (no first-admin link seen; users may already exist)", base)

		return
	}

	url := bootstrap.URL(a.ServerPort, path)
	e.logf("create the first admin account here: %s", url)

	if err := e.Browser(ctx, url); err != nil {
		e.logf("could not open a browser (%v); open the link by hand", err)
	}
}
