// Package migrate moves an install from the apps' default layout
// (~/.config/contextmatrix{,-agent,-chat}, ~/.local/state/contextmatrix)
// under the installer's single config dir and ~/.contextmatrix.
package migrate

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/mhersson/contextmatrix-setup/internal/configsync"
	"github.com/mhersson/contextmatrix-setup/internal/layout"
)

var stateFileNames = []string{"auth.db", "images.db", "ops.db", "master.key", "instance_id"}

type Found struct {
	ServerConfig   string
	AgentConfig    string
	ChatConfig     string
	WorkflowSkills string
	StateFiles     []string
}

func (f Found) Any() bool {
	return len(f.Sources()) > 0
}

func (f Found) Sources() []string {
	var out []string

	for _, p := range []string{f.ServerConfig, f.AgentConfig, f.ChatConfig, f.WorkflowSkills} {
		if p != "" {
			out = append(out, p)
		}
	}

	return append(out, f.StateFiles...)
}

func exists(p string) bool {
	_, err := os.Stat(p)

	return err == nil
}

func Detect(l layout.Layout) Found {
	var f Found

	if exists(l.OldServerConfig()) {
		f.ServerConfig = l.OldServerConfig()
	}

	if exists(l.OldAgentConfig()) {
		f.AgentConfig = l.OldAgentConfig()
	}

	if exists(l.OldChatConfig()) {
		f.ChatConfig = l.OldChatConfig()
	}

	if exists(l.OldWorkflowSkillsDir()) {
		f.WorkflowSkills = l.OldWorkflowSkillsDir()
	}

	for _, name := range stateFileNames {
		if p := filepath.Join(l.OldStateDir, name); exists(p) {
			f.StateFiles = append(f.StateFiles, p)
		}
	}

	return f
}

type RepoDir struct {
	Key  string
	Path string
}

type Move struct {
	From string
	To   string
}

type Plan struct {
	Server   configsync.Tree
	Agent    configsync.Tree
	Chat     configsync.Tree
	Moves    []Move
	RepoDirs []RepoDir
	Sources  []string
}

func Build(l layout.Layout, f Found, moveRepos map[string]bool) (Plan, error) {
	p := Plan{Sources: f.Sources()}

	var err error

	if p.Server, err = load(f.ServerConfig); err != nil {
		return p, err
	}

	if p.Agent, err = load(f.AgentConfig); err != nil {
		return p, err
	}

	if p.Chat, err = load(f.ChatConfig); err != nil {
		return p, err
	}

	tilde := func(s string) string { return layout.Tilde(l, s) }
	expand := func(s string) string {
		if strings.HasPrefix(s, "~/") {
			return filepath.Join(l.Home, s[2:])
		}

		return s
	}

	// Config files move to their new names; the merged content overwrites
	// them right after, so this is a rename, never a copy.
	if f.ServerConfig != "" {
		p.Moves = append(p.Moves, Move{From: f.ServerConfig, To: l.ServerConfig()})
	}

	if f.AgentConfig != "" {
		p.Moves = append(p.Moves, Move{From: f.AgentConfig, To: l.AgentConfig()})
	}

	if f.ChatConfig != "" {
		p.Moves = append(p.Moves, Move{From: f.ChatConfig, To: l.ChatConfig()})
	}

	// Server state files: the old config may point anywhere; default to the
	// old state dir.
	stateKeys := map[string]string{"auth.db": "auth.db_path", "master.key": "auth.master_key_file", "images.db": "images.db_path", "ops.db": "op_store.db_path"}

	for name, key := range stateKeys {
		from := filepath.Join(l.OldStateDir, name)

		if v, ok := configsync.Get(p.Server, key); ok {
			if s, _ := v.(string); s != "" {
				from = expand(s)
			}
		}

		to := filepath.Join(l.ServerStateDir(), name)
		configsync.Set(p.Server, key, tilde(to))

		if exists(from) && from != to {
			p.Moves = append(p.Moves, Move{From: from, To: to})
		}
	}

	if v, ok := configsync.Get(p.Server, "instance.id"); !ok || v == "" {
		idFile := filepath.Join(l.OldStateDir, "instance_id")
		if data, err := os.ReadFile(idFile); err == nil && strings.TrimSpace(string(data)) != "" {
			configsync.Set(p.Server, "instance.id", strings.TrimSpace(string(data)))
			p.Moves = append(p.Moves, Move{From: idFile, To: filepath.Join(l.ServerStateDir(), "instance_id")})
		}
	}

	configsync.Set(p.Server, "workflow_skills_dir", tilde(l.WorkflowSkillsDir()))

	if f.WorkflowSkills != "" {
		p.Moves = append(p.Moves, Move{From: f.WorkflowSkills, To: l.WorkflowSkillsDir()})
	}

	// Repos the user owns: listed for the wizard, moved only on request.
	if list, ok := p.Server["boards"].([]any); ok {
		for i, item := range list {
			entry, ok := item.(map[string]any)
			if !ok {
				continue
			}

			dir, _ := entry["dir"].(string)
			if dir == "" {
				continue
			}

			key := fmt.Sprintf("boards[%d]", i)
			p.RepoDirs = append(p.RepoDirs, RepoDir{Key: key, Path: dir})

			if moveRepos[key] {
				to := l.BoardsDir(filepath.Base(expand(dir)))
				entry["dir"] = tilde(to)
				p.Moves = append(p.Moves, Move{From: expand(dir), To: to})
			}
		}
	} else if v, ok := configsync.Get(p.Server, "boards.dir"); ok {
		if dir, _ := v.(string); dir != "" {
			p.RepoDirs = append(p.RepoDirs, RepoDir{Key: "boards", Path: dir})

			if moveRepos["boards"] {
				to := l.BoardsDir(filepath.Base(expand(dir)))
				configsync.Set(p.Server, "boards.dir", tilde(to))
				p.Moves = append(p.Moves, Move{From: expand(dir), To: to})
			}
		}
	}

	if v, ok := configsync.Get(p.Server, "task_skills.dir"); ok {
		if dir, _ := v.(string); dir != "" {
			p.RepoDirs = append(p.RepoDirs, RepoDir{Key: "task_skills", Path: dir})

			if moveRepos["task_skills"] {
				configsync.Set(p.Server, "task_skills.dir", tilde(l.TaskSkillsDir()))
				p.Moves = append(p.Moves, Move{From: expand(dir), To: l.TaskSkillsDir()})
			}
		}
	}

	// Backend runtime dirs are repointed only; their content is per-run.
	configsync.Set(p.Agent, "secrets_dir", tilde(l.AgentSecretsDir()))
	configsync.Set(p.Agent, "log_dir", tilde(l.AgentLogsDir()))
	configsync.Set(p.Chat, "secrets_dir", tilde(l.ChatSecretsDir()))
	configsync.Set(p.Chat, "chat_run_dir", tilde(l.ChatSessionsDir()))

	return p, nil
}

func load(path string) (configsync.Tree, error) {
	if path == "" {
		return configsync.Tree{}, nil
	}

	t, _, err := configsync.LoadFile(path)
	if err != nil {
		return nil, err
	}

	return t, nil
}

func Apply(moves []Move, out io.Writer) error {
	for _, m := range moves {
		if !exists(m.From) {
			fmt.Fprintf(out, "%-22s skip: %s does not exist\n", "migrate", m.From)

			continue
		}

		if exists(m.To) {
			return fmt.Errorf("migrate: %s already exists; move it aside and rerun", m.To)
		}

		if err := os.MkdirAll(filepath.Dir(m.To), 0o700); err != nil {
			return err
		}

		if err := os.Rename(m.From, m.To); err != nil {
			var linkErr *os.LinkError
			if !errors.As(err, &linkErr) {
				return err
			}

			if err := copyTree(m.From, m.To); err != nil {
				return fmt.Errorf("copy %s: %w", m.From, err)
			}

			if err := os.RemoveAll(m.From); err != nil {
				return err
			}
		}

		fmt.Fprintf(out, "%-22s %s -> %s\n", "migrate", m.From, m.To)
	}

	return nil
}

func copyTree(src, dst string) error {
	info, err := os.Stat(src)
	if err != nil {
		return err
	}

	if !info.IsDir() {
		data, err := os.ReadFile(src)
		if err != nil {
			return err
		}

		// Both ends come from a plan the installer built out of its own
		// layout, never from a value a caller supplies.
		return os.WriteFile(dst, data, info.Mode().Perm()) //nolint:gosec // src and dst are installer-owned paths.
	}

	if err := os.MkdirAll(dst, info.Mode().Perm()); err != nil {
		return err
	}

	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}

	for _, e := range entries {
		if err := copyTree(filepath.Join(src, e.Name()), filepath.Join(dst, e.Name())); err != nil {
			return err
		}
	}

	return nil
}
