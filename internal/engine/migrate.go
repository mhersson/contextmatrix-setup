package engine

import (
	"context"
	"os"
	"path/filepath"

	"github.com/mhersson/contextmatrix-setup/internal/configsync"
	"github.com/mhersson/contextmatrix-setup/internal/migrate"
	"github.com/mhersson/contextmatrix-setup/internal/repos"
	"github.com/mhersson/contextmatrix-setup/internal/state"
)

// Migrate applies a migration plan: stops the old services, moves files,
// and writes the rewritten trees where Install will read them as the
// user's values. Install must follow to build, merge and start.
func (e *Engine) Migrate(ctx context.Context, p migrate.Plan) error {
	for _, name := range repos.Apps {
		_ = e.Services.Stop(ctx, name)
	}

	// The moves run before makeDirs: Apply refuses an existing destination,
	// and it creates each destination's parent itself.
	if err := migrate.Apply(p.Moves, e.Out); err != nil {
		return err
	}

	for repo, tree := range map[string]configsync.Tree{repos.Server: p.Server, repos.Agent: p.Agent, repos.Chat: p.Chat} {
		data, err := configsync.Encode(tree, referenceFor(repo))
		if err != nil {
			return err
		}

		if _, err := configsync.WriteIfChanged(e.L.ConfigFor(repo), data); err != nil {
			return err
		}
	}

	for _, src := range p.Sources {
		removeIfEmpty(filepath.Dir(src))
	}

	if err := e.makeDirs(); err != nil {
		return err
	}

	st, _, err := state.Load(e.L.StateFile())
	if err != nil {
		return err
	}

	st.Migration = &state.Migration{DoneAt: e.now(), From: p.Sources}
	e.logf("%-22s %d sources moved under %s", "migrate", len(p.Sources), e.L.StateDir)

	return st.Save(e.L.StateFile())
}

func removeIfEmpty(dir string) {
	entries, err := os.ReadDir(dir)
	if err == nil && len(entries) == 0 {
		_ = os.Remove(dir)
	}
}
