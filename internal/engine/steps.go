package engine

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/mhersson/contextmatrix-setup/internal/configsync"
	"github.com/mhersson/contextmatrix-setup/internal/repos"
	"github.com/mhersson/contextmatrix-setup/internal/run"
	"github.com/mhersson/contextmatrix-setup/internal/services"
)

func (e *Engine) syncRepos(ctx context.Context, names []string) (map[string]string, error) {
	heads := map[string]string{}

	for _, name := range names {
		head, err := e.Git.Sync(ctx, e.L.SrcDir(name), e.repoURL(name), e.Out)
		if err != nil {
			return nil, fmt.Errorf("sync %s: %w", name, err)
		}

		heads[name] = head

		e.logf("%-22s %s", name, repos.Short(head))
	}

	return heads, nil
}

func (e *Engine) buildBinary(ctx context.Context, repo string) error {
	e.logf("%-22s make install", repo)

	if err := e.R.Stream(ctx, run.Cmd{Name: "make", Args: []string{"install"}, Dir: e.L.SrcDir(repo)}, e.Out); err != nil {
		return fmt.Errorf("build %s: %w", repo, err)
	}

	return nil
}

func (e *Engine) schema(ctx context.Context, repo string) (configsync.Tree, error) {
	bin := e.Host.Binary(binaryFor(repo))

	res, err := e.R.Run(ctx, run.Cmd{Name: bin, Args: []string{"config", "defaults"}})
	if err != nil {
		return nil, err
	}

	if res.ExitCode != 0 {
		return nil, fmt.Errorf("%s config defaults: %s", bin, strings.TrimSpace(res.Stderr))
	}

	tree, err := configsync.Parse([]byte(res.Stdout))
	if err != nil {
		return nil, fmt.Errorf("%s config defaults: %w", bin, err)
	}

	return tree, nil
}

type configResult struct {
	Path    string
	Changed bool
	Hash    string
	Dropped []configsync.Dropped
	Tree    configsync.Tree
}

func (e *Engine) writeConfig(ctx context.Context, repo string, opinionated configsync.Tree, force map[string]any) (configResult, error) {
	path := e.L.ConfigFor(repo)

	schema, err := e.schema(ctx, repo)
	if err != nil {
		return configResult{}, err
	}

	user, _, err := configsync.LoadFile(path)
	if err != nil {
		return configResult{}, err
	}

	merged, dropped := configsync.Merge(schema, user, opinionated)

	for k, v := range force {
		configsync.Set(merged, k, v)
	}

	data, err := configsync.Encode(merged, referenceFor(repo))
	if err != nil {
		return configResult{}, err
	}

	changed, err := configsync.WriteIfChanged(path, data)
	if err != nil {
		return configResult{}, fmt.Errorf("write %s: %w", path, err)
	}

	// Merge appends in map iteration order, so sort before reporting.
	report := slices.Clone(dropped)
	slices.SortFunc(report, func(a, b configsync.Dropped) int { return strings.Compare(a.Path, b.Path) })

	for _, d := range report {
		e.logf("%-22s dropped %s (was %v): key no longer exists upstream", filepath.Base(path), d.Path, d.Value)
	}

	return configResult{Path: path, Changed: changed, Hash: configsync.Hash(data), Dropped: dropped, Tree: merged}, nil
}

func referenceFor(repo string) string {
	if repo == repos.Server {
		return "config.yaml.example in " + repos.URL(repo)
	}

	return "serve.yaml.example in " + repos.URL(repo)
}

// copyWorkflowSkills copies the server checkout's workflow-skills tree. A
// destination file whose hash differs from what the installer last wrote is
// a local edit and is left alone.
func (e *Engine) copyWorkflowSkills(previous map[string]string) (map[string]string, []string, error) {
	src := filepath.Join(e.L.SrcDir(repos.Server), "workflow-skills")
	dst := e.L.WorkflowSkillsDir()
	files := map[string]string{}

	var skipped []string

	err := filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}

		rel, _ := filepath.Rel(src, p)
		target := filepath.Join(dst, rel)

		// Both ends are installer-owned: the source is the git checkout under
		// CacheDir, the target the state dir, neither reachable by a caller.
		data, err := os.ReadFile(p) //nolint:gosec // walked path is inside the installer's own checkout.
		if err != nil {
			return err
		}

		if existing, err := os.ReadFile(target); err == nil {
			if prev, ok := previous[rel]; ok && prev != fileHash(existing) {
				skipped = append(skipped, rel)
				files[rel] = prev

				return nil
			}
		}

		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			return err
		}

		if err := os.WriteFile(target, data, 0o600); err != nil { //nolint:gosec // target is joined onto the installer's own state dir.
			return err
		}

		files[rel] = fileHash(data)

		return nil
	})
	if err != nil {
		return nil, nil, fmt.Errorf("copy workflow skills: %w", err)
	}

	return files, skipped, nil
}

func fileHash(data []byte) string {
	sum := sha256.Sum256(data)

	return hex.EncodeToString(sum[:])
}

func (e *Engine) serviceFor(repo string, server configsync.Tree) services.Service {
	bin := e.Host.Binary(binaryFor(repo))
	env := map[string]string{"HOME": e.L.Home, "PATH": e.path()}

	// The docker host is a static machine fact, not a per-call decision, so
	// this lookup does not take the caller's context.
	if h := e.Images.Host(context.Background()); h != "" && repo != repos.Server {
		env["DOCKER_HOST"] = h
	}

	s := services.Service{
		Name:           repo,
		Binary:         bin,
		Env:            env,
		WorkingDir:     e.L.Home,
		ReadWritePaths: []string{e.L.StateDir},
		LogFile:        filepath.Join(e.L.MacLogsDir(), repo+".log"),
	}

	switch repo {
	case repos.Server:
		s.Description = "ContextMatrix server"
		s.Args = []string{"-config", e.L.ServerConfig()}
		s.ReadWritePaths = append(s.ReadWritePaths, e.serverPaths(server)...)
	case repos.Agent:
		s.Description = "ContextMatrix Agent (task backend)"
		s.Args = []string{"serve", "--config", e.L.AgentConfig()}
	case repos.Chat:
		s.Description = "ContextMatrix Chat (chat backend)"
		s.Args = []string{"serve", "--config", e.L.ChatConfig()}
	}

	return s
}

// serverPaths lists every directory the server writes, expanded from the
// config so a hand-edited boards.dir outside ~/.contextmatrix still works.
func (e *Engine) serverPaths(server configsync.Tree) []string {
	var out []string

	// An empty value must be rejected before anything is appended to it: a
	// suffix would turn "" into a rooted path and grant the service "/".
	usable := func(v any) (string, bool) {
		s, ok := v.(string)
		if !ok || s == "" {
			return "", false
		}

		if strings.HasPrefix(s, "~/") {
			s = filepath.Join(e.L.Home, s[2:])
		}

		if strings.HasPrefix(s, e.L.StateDir+string(filepath.Separator)) {
			return "", false
		}

		return s, true
	}

	addDir := func(v any) {
		if s, ok := usable(v); ok {
			out = append(out, s)
		}
	}

	addFile := func(v any) {
		if s, ok := usable(v); ok {
			out = append(out, filepath.Dir(s))
		}
	}

	if list, ok := server["boards"].([]any); ok {
		for _, item := range list {
			entry, ok := item.(map[string]any)
			if !ok {
				continue
			}

			addDir(entry["dir"])
		}
	} else if v, ok := configsync.Get(server, "boards.dir"); ok {
		addDir(v)
	}

	if v, ok := configsync.Get(server, "task_skills.dir"); ok {
		addDir(v)
	}

	for _, key := range []string{"auth.db_path", "auth.master_key_file", "images.db_path", "op_store.db_path"} {
		if v, ok := configsync.Get(server, key); ok {
			addFile(v)
		}
	}

	return out
}

func (e *Engine) gateway(ctx context.Context) string {
	if e.Host.OS == "darwin" {
		return "host.docker.internal"
	}

	return e.Images.BridgeGateway(ctx)
}

func githubConfigured(server configsync.Tree) bool {
	v, ok := configsync.Get(server, "github.auth_mode")

	return ok && (v == "pat" || v == "app")
}

// currentKeys reads the shared secrets from existing config trees so an
// update never regenerates a key that is already in use.
func (e *Engine) currentKeys(server, agent, chat configsync.Tree) Keys {
	str := func(t configsync.Tree, path string) string {
		v, _ := configsync.Get(t, path)
		s, _ := v.(string)

		return s
	}

	k := Keys{MCP: str(server, "mcp_api_key"), Agent: str(agent, "api_key"), Chat: str(chat, "api_key")}

	if k.Agent == "" {
		k.Agent = str(server, "backends.agent.api_key")
	}

	if k.Chat == "" {
		k.Chat = str(server, "backends.chat.api_key")
	}

	return k
}
