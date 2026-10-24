package engine

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/x/ansi"

	"github.com/mhersson/contextmatrix-setup/internal/configsync"
	"github.com/mhersson/contextmatrix-setup/internal/images"
	"github.com/mhersson/contextmatrix-setup/internal/repos"
	"github.com/mhersson/contextmatrix-setup/internal/state"
	"github.com/mhersson/contextmatrix-setup/internal/ui"
)

type UpdateOptions struct {
	Yes     bool
	Confirm func(summary string) bool
	// UI renders the run. Nil means a plain console on Out that Update
	// creates and closes itself.
	UI *ui.Console
}

func (e *Engine) Update(ctx context.Context, opts UpdateOptions) error {
	c := opts.UI
	if c == nil {
		c = ui.NewConsole(e.Out, ui.NewTheme(ui.Mode{Plain: true, Width: 100}), nil, "")
		defer c.Close()
	}

	began := c.Now()

	st, found, err := state.Load(e.L.StateFile())
	if err != nil {
		return err
	}

	if !found || !st.Installed() {
		return errors.New("nothing installed yet: run contextmatrix-setup install first")
	}

	e.useRecordedManager(st.ServiceManager)
	unmanaged := e.Services.Kind() == "none"

	e.showSearchNotice(c)

	fetch := c.Start("fetch", plural(len(repos.Apps), "app repo"), "origin/main")

	heads, err := e.syncRepos(ctx, repos.Apps, fetch.Writer())
	if err != nil {
		fetch.Fail(err.Error())
		showFailure(c, fetch, "fetch failed")

		return err
	}

	fetch.Done("origin/main")

	moved := map[string]bool{}

	var list []repoChanges

	for _, repo := range repos.Apps {
		old := st.Repos[repo].Commit
		if heads[repo] == old {
			continue
		}

		moved[repo] = true

		rc := repoChanges{Repo: repo, From: old, To: heads[repo], Web: repos.WebBase(e.repoURL(repo))}
		if e.Host.Docker && needsDocker(repo) {
			rc.Images = 1 + len(images.Variants)
		}

		if old != "" {
			rc.Changes, rc.Err = e.Git.Changes(ctx, e.L.SrcDir(repo), old, heads[repo])
			if repo == repos.Server {
				rc.Skills, _ = e.Git.PathChanged(ctx, e.L.SrcDir(repo), old, heads[repo], "workflow-skills")
			}
		}

		list = append(list, rc)
	}

	if len(moved) > 0 {
		view := renderChanges(c.Theme(), list)
		c.Println(view)

		if !opts.Yes && opts.Confirm != nil && !opts.Confirm(ansi.Strip(view)) {
			c.Println("update cancelled; nothing changed")

			return nil
		}
	}

	for _, repo := range repos.Apps {
		if !moved[repo] {
			continue
		}

		s := c.Start("build", repo, "make install")

		if err := e.buildBinary(ctx, repo, s.Writer(), nil); err != nil {
			// The target already names the repo the wrapped error repeats.
			cause := err
			if inner := errors.Unwrap(err); inner != nil {
				cause = inner
			}

			s.Fail(cause.Error())
			showFailure(c, s, repo+" build failed")

			return err
		}

		s.Done(repos.Short(heads[repo]))
	}

	dockerAppeared := !st.Docker && e.Host.Docker
	if dockerAppeared {
		printNotes(c, note{"docker found", "building worker images and enabling the backends"})
	}

	// Existing values feed the opinionated tree so nothing is regenerated.
	existing := e.loadTrees()
	answers, _ := AnswersFrom(existing)

	keys, instanceID, err := e.resolveIdentity(existing)
	if err != nil {
		return err
	}

	facts := Facts{Layout: e.L, Gateway: e.gateway(ctx), Docker: e.Host.Docker, Keys: keys, InstanceID: instanceID}

	force := map[string]map[string]any{repos.Server: {}, repos.Agent: {}, repos.Chat: {}, repos.Architect: {}}
	oldTags := map[string]string{}
	imageChanged := map[string]bool{}
	buildFailed := map[string]bool{}

	if e.Host.Docker {
		for _, repo := range []string{repos.Agent, repos.Chat} {
			family := images.Family(repo)
			old := st.Images[family]

			// An install made before the variants existed recorded only the
			// default image; build once so project overrides can resolve. No
			// image at all means a failed build or a lost record: build it.
			missingImage := old.Tag == ""
			missingVariants := !missingImage && len(old.Variants) == 0

			if !moved[repo] && !dockerAppeared && !missingImage && !missingVariants {
				continue
			}

			var notes []note

			switch {
			case moved[repo] || dockerAppeared:
			case missingImage:
				notes = append(notes, note{repo, "worker image missing: built at the recorded commit"})
			case missingVariants:
				notes = append(notes, note{repo, "variant images missing: built at the recorded commit"})
			}

			oldTags[repo] = old.Tag

			s := c.Start("images", family, fmt.Sprintf("docker build · default + %d variants", len(images.Variants)))

			b, err := e.Images.Build(ctx, e.L.SrcDir(repo), repo, heads[repo], s.Writer())
			if err != nil {
				s.Fail("docker build failed")
				showFailure(c, s, family+" build failed")

				failed := fmt.Sprintf("image build failed: %v", err)
				if old.Tag != "" {
					failed = fmt.Sprintf("image build failed, keeping %s: %v", old.Tag, err)
				}

				printNotes(c, append(notes, note{repo, failed})...)

				buildFailed[repo] = true

				continue
			}

			s.Done(builtImages(heads[repo], b))
			printNotes(c, notes...)

			st.Images[family] = state.Image{Tag: b.Tag, ID: b.ID, Variants: b.Variants}
			force[repo]["base_image"] = b.Tag
			imageChanged[repo] = b.Tag != oldTags[repo]
			e.removeReplacedVariants(ctx, c, repo, old.Variants, b.Variants)
		}
	}

	if dockerAppeared {
		force[repos.Server]["backends.agent.enabled"] = true
		force[repos.Server]["backends.chat.enabled"] = true
	}

	trees := Opinionated(answers, facts)
	opinionated := map[string]configsync.Tree{repos.Server: trees.Server, repos.Agent: trees.Agent, repos.Chat: trees.Chat, repos.Architect: trees.Architect}
	results := map[string]configResult{}
	configChanged := map[string]bool{}
	handEdited := map[string]bool{}

	// An install made before the architect may already carry CM's empty,
	// disabled architect block, which the merge would keep.
	newArchitect := st.Repos[repos.Architect].Commit == ""
	if newArchitect {
		fillArchitectBlock(force[repos.Server], existing.Server, trees.Server)
	}

	cfg := c.Start("config", plural(len(repos.Apps), "file"), "")

	var (
		notes   []note
		written []string
	)

	for _, repo := range repos.Apps {
		name := filepath.Base(e.L.ConfigFor(repo))

		if prev, ok := st.Configs[name]; ok {
			// A hand edit that survives the merge re-encodes to the bytes
			// already on disk, so only this hash reveals that the running
			// service is out of date with its file.
			if data, err := os.ReadFile(e.L.ConfigFor(repo)); err == nil && configsync.Hash(data) != prev.SHA256 {
				notes = append(notes, note{name, "edited by hand since the last run; values kept"})
				handEdited[repo] = true
			}
		}

		res, err := e.writeConfig(ctx, repo, opinionated[repo], force[repo])
		if err != nil {
			cfg.Fail(err.Error())
			printNotes(c, notes...)

			return err
		}

		for _, d := range res.Dropped {
			notes = append(notes, note{name, droppedText(d)})
		}

		results[repo] = res
		configChanged[repo] = res.Changed
		st.Configs[name] = state.ConfigHash{SHA256: res.Hash}

		if res.Changed {
			written = append(written, name)
		}
	}

	invalid := e.validateAll(ctx, results)

	for _, repo := range repos.Apps {
		if err := invalid[repo]; err != nil {
			notes = append(notes, note{filepath.Base(results[repo].Path), "invalid: " + err.Error()})
		}
	}

	for _, w := range configsync.CrossCheck(results[repos.Server].Tree, results[repos.Agent].Tree, results[repos.Chat].Tree, results[repos.Architect].Tree) {
		notes = append(notes, note{"warning", w})
	}

	if len(written) == 0 {
		cfg.Done("in sync")
	} else {
		cfg.Done(strings.Join(written, ", "))
	}

	printNotes(c, notes...)

	if moved[repos.Server] {
		changed, err := e.Git.PathChanged(ctx, e.L.SrcDir(repos.Server), st.Repos[repos.Server].Commit, heads[repos.Server], "workflow-skills")
		if err != nil {
			return err
		}

		if changed {
			s := c.Start("skills", "workflow-skills", "")

			files, skipped, err := e.copyWorkflowSkills(st.WorkflowSkills.Files)
			if err != nil {
				s.Fail(err.Error())

				return err
			}

			st.WorkflowSkills = state.WorkflowSkills{Commit: heads[repos.Server], Files: files}

			s.Done(plural(len(files)-len(skipped), "file") + " copied")

			kept := make([]note, 0, len(skipped))
			for _, f := range skipped {
				kept = append(kept, note{f, "locally modified, not overwritten"})
			}

			printNotes(c, kept...)
		}
	}

	unitChanged := map[string]bool{}
	hold := map[string]string{}

	if !unmanaged {
		if reason := e.holdNewArchitect(ctx, newArchitect, results[repos.Architect].Tree); reason != "" {
			hold[repos.Architect] = reason
		}

		for _, repo := range repos.Apps {
			changed, err := e.Services.Install(ctx, e.serviceFor(repo, mergedTrees(results)))
			if err != nil {
				return err
			}

			unitChanged[repo] = changed
		}
	}

	github := githubConfigured(results[repos.Server].Tree)

	var (
		rows    []serviceResult
		restart = map[string]bool{}
	)

	for _, repo := range repos.Apps {
		needs := moved[repo] || configChanged[repo] || handEdited[repo] || imageChanged[repo] || unitChanged[repo]
		if !needs && !buildFailed[repo] {
			continue
		}

		// A new image runs only once the service restarts onto it.
		tag := st.Images[images.Family(repo)].Tag
		if imageChanged[repo] {
			tag = oldTags[repo]
		}

		row := serviceResult{Repo: repo, Running: repos.Short(st.Repos[repo].Commit), Image: e.workerImage(repo, tag)}

		switch {
		case buildFailed[repo] && oldTags[repo] == "":
			// Without an image the backend never ran, so nothing is kept.
			row.Result, row.Problem, row.Running = "✗ not started", true, ""
			row.Note = "its worker image did not build, so it was not started. The next update builds it."
		case buildFailed[repo]:
			row.Result, row.Problem, row.Kept = "● kept", true, true
			row.Note = fmt.Sprintf("keeps running %s: its worker image did not build, so it was not restarted. The next update retries it.", row.Running)
		case invalid[repo] != nil:
			row.Result, row.Problem = "✗ not restarted", true
			row.Note = "config invalid: " + invalid[repo].Error()
		case repo == repos.Server && !github:
			row.Result, row.Note = "● not started", "github block missing in server.yaml"
		case needsDocker(repo) && !e.Host.Docker:
			row.Result, row.Note = "● not started", "docker not available"
		case hold[repo] != "":
			row.Result, row.Note = "● not started", hold[repo]
		case unmanaged:
			// The old process runs until the user restarts it by hand.
			row.Result = "start by hand"
		default:
			restart[repo] = true
		}

		rows = append(rows, row)
	}

	var removeNotes []note

	e.restartServices(ctx, c, rows, restart, results, func(r *serviceResult) {
		r.Running = repos.Short(heads[r.Repo])
		r.Image = e.workerImage(r.Repo, st.Images[images.Family(r.Repo)].Tag)

		if old := oldTags[r.Repo]; old != "" && imageChanged[r.Repo] {
			if err := e.Images.RemoveImage(ctx, old); err != nil {
				removeNotes = append(removeNotes, note{r.Repo, fmt.Sprintf("could not remove old image %s: %v", old, err)})
			}
		}
	})

	printNotes(c, removeNotes...)

	if len(rows) > 0 {
		c.Println(renderResult(c.Theme(), rows, e.Host.Docker))

		if unmanaged {
			for _, line := range e.startCommands() {
				c.Println(line)
			}
		}
	}

	st.Docker = e.Host.Docker
	st.ServiceManager = e.Services.Kind()

	for _, repo := range repos.Apps {
		// A repo whose image did not build stays on its recorded commit, so
		// the next run sees it as moved again and retries the build; a held
		// one stays unrecorded and is checked again.
		if buildFailed[repo] || hold[repo] != "" {
			continue
		}

		st.Repos[repo] = state.Repo{Commit: heads[repo], InstalledAt: e.now()}
	}

	if err := st.Save(e.L.StateFile()); err != nil {
		return err
	}

	problems := 0

	for _, r := range rows {
		if r.Problem {
			problems++
		}
	}

	t := c.Theme()
	took := t.Dim(" in " + ui.Duration(c.Now().Sub(began)))

	switch {
	case len(rows) == 0:
		c.Println(" " + t.Good("✓ everything is up to date"))
	case problems > 0:
		c.Println("\n " + t.Bad("✗ update finished with "+plural(problems, "problem")) + took)
	default:
		c.Println("\n " + t.Good("✓ update finished") + took)
	}

	if p := c.LogPath(); p != "" {
		c.Println(" " + t.Dim("log "+p))
	}

	// Repeated last, so the build output above cannot scroll it away.
	e.showSearchNotice(c)

	if problems > 0 {
		return &ProblemsError{N: problems}
	}

	return nil
}

// restartServices restarts every row in restart and gives each its step
// line, in table order. The restarts run one after another in the
// background, and each service is probed as soon as it is back, so the
// probes overlap within one budget while a step stays live until its own
// service is restarted and checked. restarted runs for each success once
// its step has ended.
func (e *Engine) restartServices(ctx context.Context, c *ui.Console, rows []serviceResult, restart map[string]bool, results map[string]configResult, restarted func(r *serviceResult)) {
	var order []string

	urls := map[string]string{}
	health := map[string]healthResult{}

	for _, r := range rows {
		if !restart[r.Repo] {
			continue
		}

		order = append(order, r.Repo)

		if e.SkipHealth {
			health[r.Repo] = healthResult{Reason: "not checked"}

			continue
		}

		url, err := healthURL(r.Repo, results[r.Repo].Tree)
		if err != nil {
			health[r.Repo] = healthResult{Checked: true, Reason: err.Error()}

			continue
		}

		urls[r.Repo] = url
	}

	var outcomes map[string]<-chan restartOutcome

	for i := range rows {
		r := &rows[i]
		if !restart[r.Repo] {
			continue
		}

		_, probing := urls[r.Repo]

		detail := ""
		if probing {
			detail = "with health check"
		}

		s := c.Start("restart", r.Repo, detail)

		// Started after the first step, so that step covers its restart.
		if outcomes == nil {
			outcomes = e.restartInOrder(ctx, order, urls)
		}

		o := <-outcomes[r.Repo]
		if o.err != nil {
			s.Fail(o.err.Error())

			r.Result, r.Problem = "✗ restart failed", true
			r.Note = strings.TrimSpace(o.err.Error() + "\n" + e.recentLogs(ctx, r.Repo))

			continue
		}

		r.Result = "✓ restarted"
		r.Health = health[r.Repo]

		if probing {
			r.Health = o.health
		}

		switch h := r.Health; {
		case h.OK:
			s.Done("healthy in " + ui.Duration(h.After))
		case h.Checked:
			s.Fail(h.Reason)

			r.Problem = true

			if h.Reason == reasonNotRunning {
				if logs := strings.TrimSpace(e.recentLogs(ctx, r.Repo)); logs != "" {
					r.Note = "recent log:\n" + logs
				}
			}
		default:
			s.Done("restarted")
		}

		restarted(r)
	}
}

// restartOutcome is a service's restart error, or its health once back.
type restartOutcome struct {
	err    error
	health healthResult
}

// restartInOrder restarts order one service at a time in the background
// and probes each one in urls as soon as it is back. The health budget
// starts once the last restart has returned, so a slow stop cannot use up
// a later service's wait.
func (e *Engine) restartInOrder(ctx context.Context, order []string, urls map[string]string) map[string]<-chan restartOutcome {
	chans := make(map[string]chan restartOutcome, len(order))
	out := make(map[string]<-chan restartOutcome, len(order))

	for _, repo := range order {
		ch := make(chan restartOutcome, 1)
		chans[repo], out[repo] = ch, ch
	}

	go func() {
		budget, arm := e.healthBudget(ctx)

		for _, repo := range order {
			if err := e.Services.Restart(ctx, repo); err != nil {
				chans[repo] <- restartOutcome{err: err}

				continue
			}

			url, ok := urls[repo]
			if !ok {
				chans[repo] <- restartOutcome{}

				continue
			}

			go func(ctx context.Context) {
				chans[repo] <- restartOutcome{health: e.waitHealthy(ctx, repo, url)}
			}(budget)
		}

		arm()
	}()

	return out
}

// workerImage shows a docker-backed service's image tag without the
// contextmatrix- prefix the service column already shows.
func (e *Engine) workerImage(repo, tag string) string {
	if !e.Host.Docker || !needsDocker(repo) {
		return ""
	}

	return strings.TrimPrefix(tag, "contextmatrix-")
}

// note is a warning printed indented under the step line it belongs to.
type note struct{ name, text string }

func printNotes(c *ui.Console, notes ...note) {
	for _, n := range notes {
		c.Println("   " + c.Theme().Caution("● "+n.name) + "  " + n.text)
	}
}

// showFailure boxes a failed step's output tail on a live console. Plain
// output already streamed it above the failed line.
func showFailure(c *ui.Console, s *ui.Step, title string) {
	if m := c.Theme().Mode(); m.TTY && !m.Plain {
		c.Println(c.Theme().FailureBox(title, s.Tail(), c.LogPath()))
	}
}

func builtImages(head string, b images.Built) string {
	var built []string

	for _, v := range images.Variants {
		if _, ok := b.Variants[v]; ok {
			built = append(built, v)
		}
	}

	if len(built) == 0 {
		return repos.Short(head)
	}

	return repos.Short(head) + " + " + strings.Join(built, ", ")
}

func (e *Engine) loadTrees() Trees {
	load := func(p string) configsync.Tree {
		t, _, err := configsync.LoadFile(p)
		if err != nil || t == nil {
			return configsync.Tree{}
		}

		return t
	}

	return Trees{
		Server:    load(e.L.ServerConfig()),
		Agent:     load(e.L.AgentConfig()),
		Chat:      load(e.L.ChatConfig()),
		Architect: load(e.L.ArchitectConfig()),
	}
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
	// An install made before the architect has no architect port; the other
	// three still describe it, so the default fills in without asking again.
	a.ArchitectPort, _ = port(t.Architect, "port", a.ArchitectPort)
	k.Ports = server && agent && chat

	if s := str(t.Server, "auth.mode"); s != "" {
		a.AuthMode, k.AuthMode = s, true
	}

	if s := str(t.Server, "backends.agent.default_model"); s != "" {
		a.DefaultModel, k.DefaultModel = s, true
	}

	if s := str(t.Server, "backends.architect.default_model"); s != "" {
		a.ArchitectModel = s
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

// recentLogs is the tail of a service's log, or "" when it cannot be read.
func (e *Engine) recentLogs(ctx context.Context, repo string) string {
	logs, err := e.Services.Logs(ctx, repo, 20)
	if err != nil {
		return ""
	}

	return logs
}

// removeReplacedVariants drops the images a rebuild untagged. Variant tags
// are stable and already point at the new images, so unlike the per-commit
// default tag there is no restart to wait for.
func (e *Engine) removeReplacedVariants(ctx context.Context, c *ui.Console, repo string, old, cur map[string]string) {
	for _, v := range images.Variants {
		id := old[v]
		if id == "" || id == cur[v] {
			continue
		}

		if err := e.Images.RemoveImage(ctx, id); err != nil {
			printNotes(c, note{repo, fmt.Sprintf("could not remove old %s variant image %s: %v", v, id, err)})
		}
	}
}
