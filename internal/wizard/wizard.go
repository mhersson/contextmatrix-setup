// Package wizard collects install answers with terminal forms. It owns no
// logic beyond input validation; engine.Answers.Validate is the authority.
package wizard

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/huh"

	"github.com/mhersson/contextmatrix-setup/internal/engine"
	"github.com/mhersson/contextmatrix-setup/internal/host"
	"github.com/mhersson/contextmatrix-setup/internal/migrate"
)

const welcome = `This installer is opinionated. It uses high ports (18080, 19092, 19093),
keeps every file under ~/.contextmatrix and one config directory, and exposes
only the settings a local setup needs. Everything else keeps the upstream
default; edit the config files by hand for the rest.`

func AskMigrate(found migrate.Found) (bool, error) {
	yes := true

	form := huh.NewForm(huh.NewGroup(
		huh.NewNote().Title("Existing install found").
			Description("These files use the apps' default locations:\n  "+strings.Join(found.Sources(), "\n  ")+
				"\n\nMigrating moves them under ~/.contextmatrix and ~/.config/contextmatrix and carries every value over."),
		huh.NewConfirm().Title("Migrate this install?").Value(&yes),
	))

	return yes, form.Run()
}

func AskMoveRepos(dirs []migrate.RepoDir) (map[string]bool, error) {
	out := map[string]bool{}

	for _, d := range dirs {
		move := false

		form := huh.NewForm(huh.NewGroup(
			huh.NewConfirm().
				Title(fmt.Sprintf("Move %s (%s) under ~/.contextmatrix?", d.Key, d.Path)).
				Description("Keep it in place if you also use this checkout yourself.").
				Affirmative("Move").Negative("Keep").
				Value(&move),
		))
		if err := form.Run(); err != nil {
			return nil, err
		}

		out[d.Key] = move
	}

	return out, nil
}

func Run(in engine.Answers, info host.Info) (engine.Answers, error) {
	a := in
	serverPort, agentPort, chatPort := strconv.Itoa(a.ServerPort), strconv.Itoa(a.AgentPort), strconv.Itoa(a.ChatPort)
	appID, installID := strconv.FormatInt(a.GitHubAppID, 10), strconv.FormatInt(a.GitHubInstallID, 10)
	prereqs := prerequisites(info)

	port := func(s string) error {
		n, err := strconv.Atoi(s)
		if err != nil || n < 1024 || n > 65535 {
			return errors.New("use a number between 1024 and 65535")
		}

		if !host.PortFree(n) {
			return fmt.Errorf("port %d is already in use", n)
		}

		return nil
	}

	form := huh.NewForm(
		huh.NewGroup(
			huh.NewNote().Title("ContextMatrix setup").Description(welcome+"\n\n"+prereqs),
		),
		huh.NewGroup(
			huh.NewSelect[string]().Title("Login mode").
				Description("multi: login required; the first admin is created through a one-time link opened after start.\n"+
					"none: single user, no login. For a laptop.\n"+
					"Either way the server listens on all interfaces, so it is reachable from your network.").
				Options(huh.NewOption("multi (login)", "multi"), huh.NewOption("none (single user)", "none")).
				Value(&a.AuthMode),
		),
		huh.NewGroup(
			huh.NewNote().Title("Ports").Description("High ports so nothing collides with the usual 8080 and 9090 crowd."),
			huh.NewInput().Title("Server port").Value(&serverPort).Validate(port),
			huh.NewInput().Title("Agent port").Value(&agentPort).Validate(port),
			huh.NewInput().Title("Chat port").Value(&chatPort).Validate(port),
		),
		huh.NewGroup(
			huh.NewNote().Title("Inference").Description("Runs and chats need an OpenRouter API key; it is forwarded to every worker. Leave empty to add it to server.yaml later."),
			huh.NewInput().Title("OpenRouter API key").EchoMode(huh.EchoModePassword).Value(&a.OpenRouterKey),
			huh.NewInput().Title("Default model").Description("Used by both backends; chat cannot start without one.").Value(&a.DefaultModel),
		),
		huh.NewGroup(
			huh.NewNote().Title("GitHub").Description(
				"pat: a personal access token; quickest, acts as you.\n"+
					"app: a GitHub App; installs per org, scoped permissions, better for teams.\n"+
					"skip: the server will not start until the github block in server.yaml is filled in."),
			huh.NewSelect[string]().Title("GitHub auth").
				Options(huh.NewOption("pat", "pat"), huh.NewOption("app", "app"), huh.NewOption("skip", "skip")).
				Value(&a.GitHubMode),
		),
		huh.NewGroup(
			huh.NewNote().Title("Personal access token").Description("Create one at https://github.com/settings/tokens/new?scopes=repo&description=ContextMatrix"),
			huh.NewInput().Title("Token").EchoMode(huh.EchoModePassword).Value(&a.GitHubPAT),
		).WithHideFunc(func() bool { return a.GitHubMode != "pat" }),
		huh.NewGroup(
			huh.NewInput().Title("App id").Value(&appID),
			huh.NewInput().Title("Installation id").Value(&installID),
			huh.NewInput().Title("Private key file").Description("Copied to ~/.contextmatrix/server/github-app.pem").Value(&a.GitHubKeyFile),
		).WithHideFunc(func() bool { return a.GitHubMode != "app" }),
		huh.NewGroup(
			huh.NewNote().Title("Artificial Analysis").Description("Optional. Enables live model quality scores for the selector. Free tier at https://artificialanalysis.ai"),
			huh.NewInput().Title("Artificial Analysis API key").EchoMode(huh.EchoModePassword).Value(&a.AAKey),
		),
		huh.NewGroup(
			huh.NewNote().Title("Task skills").Description("A git repo of task skills the agents read. Leave empty to use a local, empty directory."),
			huh.NewInput().Title("Task-skills repo URL").Value(&a.TaskSkillsURL),
		),
		huh.NewGroup(
			huh.NewNote().Title("Boards").Description("The git repo holding your cards. With a URL it is cloned and pushed to; without, a local repo is created."),
			huh.NewInput().Title("Boards repo URL").Value(&a.BoardsURL),
			huh.NewInput().Title("Boards name").Description("Directory name under ~/.contextmatrix/boards. Derived from the URL when empty.").Value(&a.BoardsName),
		),
		huh.NewGroup(
			huh.NewConfirm().Title("Install services?").Description("systemd user units on Linux, LaunchAgents on macOS. Started now and at login.").Value(&a.Services),
			huh.NewConfirm().Title("Enable linger?").Description("Linux only: lets services run without an open login session. May prompt for a password.").
				Value(&a.Linger),
		),
	)

	if err := form.Run(); err != nil {
		return a, err
	}

	a.ServerPort, _ = strconv.Atoi(serverPort)
	a.AgentPort, _ = strconv.Atoi(agentPort)
	a.ChatPort, _ = strconv.Atoi(chatPort)
	a.GitHubAppID, _ = strconv.ParseInt(appID, 10, 64)
	a.GitHubInstallID, _ = strconv.ParseInt(installID, 10, 64)
	a.Normalize()

	if err := a.Validate(); err != nil {
		return a, err
	}

	return Confirm(a)
}

// Confirm shows every answer with secrets masked and asks to proceed.
func Confirm(a engine.Answers) (engine.Answers, error) {
	mask := func(s string) string {
		if s == "" {
			return "(none)"
		}

		return "****"
	}

	summary := fmt.Sprintf(
		"login mode      %s\nports           server %d, agent %d, chat %d\nOpenRouter key  %s\ndefault model   %s\n"+
			"GitHub          %s\nAA key          %s\ntask skills     %s\nboards          %s (%s)\nservices        %v (linger %v)",
		a.AuthMode, a.ServerPort, a.AgentPort, a.ChatPort, mask(a.OpenRouterKey), a.DefaultModel,
		a.GitHubMode, mask(a.AAKey), or(a.TaskSkillsURL, "local only"), or(a.BoardsURL, "local only"), a.BoardsName, a.Services, a.Linger)

	ok := true

	form := huh.NewForm(huh.NewGroup(
		huh.NewNote().Title("Summary").Description(summary),
		huh.NewConfirm().Title("Install with these settings?").Value(&ok),
	))
	if err := form.Run(); err != nil {
		return a, err
	}

	if !ok {
		return a, errors.New("install cancelled")
	}

	return a, nil
}

func or(v, fallback string) string {
	if v == "" {
		return fallback
	}

	return v
}

func prerequisites(info host.Info) string {
	var b strings.Builder

	b.WriteString("Prerequisites:\n")

	for _, name := range host.Required {
		mark := "ok     "
		if _, found := info.Tools[name]; !found {
			mark = "MISSING"
		}

		fmt.Fprintf(&b, "  %s %s\n", mark, name)
	}

	if info.Docker {
		b.WriteString("  ok      docker\n")
	} else {
		b.WriteString("  warning docker not available: worker images are skipped and the backends stay disabled until a later update finds it\n")
	}

	fmt.Fprintf(&b, "  service manager: %s", info.ServiceManager)

	return b.String()
}
