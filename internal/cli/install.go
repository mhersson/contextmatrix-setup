package cli

import (
	"errors"
	"fmt"
	"os"
	"runtime"
	"strconv"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/mhersson/contextmatrix-setup/internal/engine"
	"github.com/mhersson/contextmatrix-setup/internal/host"
	"github.com/mhersson/contextmatrix-setup/internal/migrate"
	"github.com/mhersson/contextmatrix-setup/internal/state"
	"github.com/mhersson/contextmatrix-setup/internal/wizard"
)

func installFlags(cmd *cobra.Command, a *engine.Answers, yes, doMigrate, moveRepos *bool) {
	f := cmd.Flags()
	f.StringVar(&a.AuthMode, "auth-mode", a.AuthMode, "multi or none")
	f.IntVar(&a.ServerPort, "server-port", a.ServerPort, "server HTTP port")
	f.IntVar(&a.AgentPort, "agent-port", a.AgentPort, "agent webhook port")
	f.IntVar(&a.ChatPort, "chat-port", a.ChatPort, "chat webhook port")
	f.StringVar(&a.OpenRouterKey, "openrouter-key", "", "OpenRouter API key (llm_endpoint.api_key)")
	f.StringVar(&a.DefaultModel, "default-model", a.DefaultModel, "default model slug for both backends")
	f.StringVar(&a.GitHubMode, "github-mode", a.GitHubMode, "pat, app or skip")
	f.StringVar(&a.GitHubPAT, "github-pat", "", "GitHub personal access token")
	f.Int64Var(&a.GitHubAppID, "github-app-id", 0, "GitHub App id")
	f.Int64Var(&a.GitHubInstallID, "github-installation-id", 0, "GitHub App installation id")
	f.StringVar(&a.GitHubKeyFile, "github-key-file", "", "GitHub App private key file")
	f.StringVar(&a.AAKey, "aa-key", "", "Artificial Analysis API key")
	f.StringVar(&a.TaskSkillsURL, "task-skills-url", "", "task-skills repo URL")
	f.StringVar(&a.BoardsURL, "boards-url", "", "boards repo URL")
	f.StringVar(&a.BoardsName, "boards-name", "", "boards directory name (derived from the URL when empty)")
	f.Var(invertBool{&a.Services}, "no-services", "do not write or start service units")
	// pflag only treats a Value as a boolean flag when it has a no-option
	// default, so without this --no-services would swallow the next argument.
	f.Lookup("no-services").NoOptDefVal = "true"
	f.BoolVar(&a.Linger, "linger", false, "Linux: run loginctl enable-linger")
	f.BoolVar(yes, "yes", false, "no prompts; take flags and defaults")
	f.BoolVar(doMigrate, "migrate", false, "with --yes: migrate an existing default-layout install")
	f.BoolVar(moveRepos, "move-repos", false, "with --migrate: move boards and task-skills checkouts under ~/.contextmatrix")
}

// invertBool binds a --no-x flag to a positive field.
type invertBool struct{ target *bool }

func (v invertBool) String() string { return strconv.FormatBool(!*v.target) }

func (v invertBool) Set(s string) error {
	b, err := strconv.ParseBool(s)
	if err != nil {
		return err
	}

	*v.target = !b

	return nil
}

func (invertBool) Type() string { return "bool" }

func (invertBool) IsBoolFlag() bool { return true }

// overlayChangedFlags copies the answers whose flags were actually given on
// the command line over values carried in from a migrated install. Without it
// a non-interactive install accepts every flag and then throws it away.
func overlayChangedFlags(fs *pflag.FlagSet, dst *engine.Answers, src engine.Answers) {
	copyIf := func(name string, apply func()) {
		if fs.Changed(name) {
			apply()
		}
	}

	copyIf("auth-mode", func() { dst.AuthMode = src.AuthMode })
	copyIf("server-port", func() { dst.ServerPort = src.ServerPort })
	copyIf("agent-port", func() { dst.AgentPort = src.AgentPort })
	copyIf("chat-port", func() { dst.ChatPort = src.ChatPort })
	copyIf("openrouter-key", func() { dst.OpenRouterKey = src.OpenRouterKey })
	copyIf("default-model", func() { dst.DefaultModel = src.DefaultModel })
	copyIf("github-mode", func() { dst.GitHubMode = src.GitHubMode })
	copyIf("github-pat", func() { dst.GitHubPAT = src.GitHubPAT })
	copyIf("github-app-id", func() { dst.GitHubAppID = src.GitHubAppID })
	copyIf("github-installation-id", func() { dst.GitHubInstallID = src.GitHubInstallID })
	copyIf("github-key-file", func() { dst.GitHubKeyFile = src.GitHubKeyFile })
	copyIf("aa-key", func() { dst.AAKey = src.AAKey })
	copyIf("task-skills-url", func() { dst.TaskSkillsURL = src.TaskSkillsURL })
	copyIf("boards-url", func() { dst.BoardsURL = src.BoardsURL })
	copyIf("boards-name", func() { dst.BoardsName = src.BoardsName })
	copyIf("no-services", func() { dst.Services = src.Services })
	copyIf("linger", func() { dst.Linger = src.Linger })
}

func newInstallCmd() *cobra.Command {
	a := engine.DefaultAnswers()

	var yes, doMigrate, moveRepos bool

	cmd := &cobra.Command{
		Use:   "install",
		Short: "Interactive first-time install",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()

			e, err := newEngine(ctx, cmd.OutOrStdout())
			if err != nil {
				return err
			}

			if missing := e.Host.Missing(); len(missing) > 0 {
				return fmt.Errorf("missing required tools: %v", missing)
			}

			st, _, err := state.Load(e.L.StateFile())
			if err != nil {
				return err
			}

			if st.Installed() {
				fmt.Fprintln(cmd.OutOrStdout(), "already installed; running update instead")

				return e.Update(ctx, engine.UpdateOptions{Yes: yes, Confirm: confirmPrompt(cmd)})
			}

			found := migrate.Detect(e.L)

			if found.Any() {
				want := doMigrate

				if !yes {
					if want, err = wizard.AskMigrate(found); err != nil {
						return err
					}
				}

				if want {
					plan, err := migrate.Build(e.L, found, nil)
					if err != nil {
						return err
					}

					moves := map[string]bool{}

					if yes {
						for _, d := range plan.RepoDirs {
							moves[d.Key] = moveRepos
						}
					} else if moves, err = wizard.AskMoveRepos(plan.RepoDirs); err != nil {
						return err
					}

					if plan, err = migrate.Build(e.L, found, moves); err != nil {
						return err
					}

					if err := e.Migrate(ctx, plan); err != nil {
						return err
					}

					prefill := engine.AnswersFrom(engine.Trees{Server: plan.Server, Agent: plan.Agent, Chat: plan.Chat})
					overlayChangedFlags(cmd.Flags(), &prefill, a)
					prefill.Services, prefill.Linger = a.Services, a.Linger
					a = prefill
				}
			}

			if !yes {
				open := func(u string) error { return host.OpenBrowser(ctx, e.R, runtime.GOOS, u) }
				if a, err = wizard.Run(a, e.Host, open); err != nil {
					return err
				}
			}

			return e.Install(ctx, a)
		},
	}

	installFlags(cmd, &a, &yes, &doMigrate, &moveRepos)

	return cmd
}

// confirmPrompt asks a plain y/N question on the command's input.
func confirmPrompt(cmd *cobra.Command) func(string) bool {
	return func(string) bool {
		fmt.Fprint(cmd.OutOrStdout(), "Continue? [Y/n] ")

		var answer string

		if _, err := fmt.Fscanln(os.Stdin, &answer); err != nil && !errors.Is(err, os.ErrClosed) {
			return true
		}

		return answer == "" || answer == "y" || answer == "Y"
	}
}
