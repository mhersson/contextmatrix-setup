package engine

import (
	"context"
	"errors"
	"os"
	"path/filepath"

	"github.com/mhersson/contextmatrix-setup/internal/configsync"
	"github.com/mhersson/contextmatrix-setup/internal/migrate"
	"github.com/mhersson/contextmatrix-setup/internal/repos"
	"github.com/mhersson/contextmatrix-setup/internal/state"
)

// Migrate applies a migration plan: removes the old units, writes the
// rewritten trees where Install will read them as the user's values, then
// moves the remaining files and drops the old configs. Install must follow
// to build, merge and start.
//
// Nothing in the old layout is touched before its replacement exists, so an
// interrupted run leaves a rerun everything it needs to finish.
func (e *Engine) Migrate(ctx context.Context, p migrate.Plan) error {
	// The old units point at the config this run deletes; one left enabled
	// would crash-loop at the next login if the install that follows
	// declines services. Install writes fresh units when they are wanted.
	for _, name := range repos.Apps {
		_ = e.Services.Remove(ctx, name)
	}

	if err := e.writeCarried(p); err != nil {
		return err
	}

	if err := migrate.Apply(p.Moves, e.Out); err != nil {
		return err
	}

	for _, path := range p.Remove {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}

		e.logf("%-22s removed %s", "migrate", path)
	}

	for _, src := range p.Sources {
		removeIfEmpty(filepath.Dir(src))
	}

	// The moves run before this: Apply refuses an existing destination, and
	// makeDirs would create the workflow-skills directory a move targets.
	if err := e.makeDirs(); err != nil {
		return err
	}

	st, _, err := state.Load(e.L.StateFile())
	if err != nil {
		return err
	}

	// A rerun that finds nothing left to migrate must not erase the record
	// the run that did the work wrote.
	if len(p.Sources) > 0 || st.Migration == nil {
		st.Migration = &state.Migration{DoneAt: e.now(), From: p.Sources}
	}

	e.logf("%-22s %d sources moved under %s", "migrate", len(p.Sources), e.L.StateDir)

	return st.Save(e.L.StateFile())
}

// writeCarried writes the rewritten tree for each config whose old file was
// found. A tree with no source holds only the keys Build rewrites, so
// writing it would strip a config an earlier run already migrated.
func (e *Engine) writeCarried(p migrate.Plan) error {
	carried := map[string]configsync.Tree{}

	if p.HasServer {
		carried[repos.Server] = p.Server
	}

	if p.HasAgent {
		carried[repos.Agent] = p.Agent
	}

	if p.HasChat {
		carried[repos.Chat] = p.Chat
	}

	for repo, tree := range carried {
		data, err := configsync.Encode(tree, referenceFor(repo))
		if err != nil {
			return err
		}

		if _, err := configsync.WriteIfChanged(e.L.ConfigFor(repo), data); err != nil {
			return err
		}
	}

	return nil
}

func removeIfEmpty(dir string) {
	entries, err := os.ReadDir(dir)
	if err == nil && len(entries) == 0 {
		_ = os.Remove(dir)
	}
}
