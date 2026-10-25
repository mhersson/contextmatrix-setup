# contextmatrix-setup

Installs and updates a local ContextMatrix stack (server, agent, chat,
architect) on macOS and Linux. No root, no sudo. One command in the morning
keeps it current.

The installer is opinionated: high ports (18080, 19092, 19093, 19094), every
file under `~/.contextmatrix`, one config directory, and only the settings a
local setup needs. Everything else keeps the upstream default; edit the config
files by hand for the rest, your values are never overwritten.

![The install wizard's welcome screen](assets/welcome.png)

## Prerequisites

### Tools

git, go (1.27 or newer), make, node and npm (server frontend build). Docker is
optional: without it the install completes, the worker images are skipped, and
the agent and chat stay disabled until a later `update` finds docker. The
architect runs no containers and needs no docker.

ripgrep (`rg`) is needed by the architect's grep tool and is never installed
by the installer: install it with your package manager
(`brew install ripgrep`, `apt install ripgrep`, ...). It must be on the
services' PATH, which is fixed to `$GOBIN:/usr/local/bin:/usr/bin:/bin` (plus
`/opt/homebrew/bin` on macOS), so an `rg` under `~/.cargo/bin` does not count.
Without it the install carries on and `install`, `update` and `status` print a
warning block. The welcome screen checks all of these before the first
question.

### Have these ready

The wizard asks for the items below, one screen each. With them at hand the
first install is a single sitting. Every item also has a flag, see
[Install](#install).

| Item                                       | Where to get it                                                                                                                                                                                                                                                                       |
| ------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| **OpenRouter API key**                     | <https://openrouter.ai/keys>. Every run and chat goes through OpenRouter. The server passes the key to the agent and chat services, which keep it and give each worker only a per-run proxy token. Leave it empty to add it to `server.yaml` later, but nothing runs until then.      |
| **Default model**                          | An OpenRouter model slug. The default is `deepseek/deepseek-v4-flash`. The agent and chat use it; chat cannot start without one. The architect has its own default, `anthropic/claude-sonnet-5.5`; `--architect-model` changes it.                                                    |
| **GitHub credential**                      | A GitHub App or a personal access token, see below. Without one the server does not start.                                                                                                                                                                                            |
| **Artificial Analysis API key** (optional) | Create an account at <https://artificialanalysis.ai> (free tier) and generate a key on your account page. Gives the model selector live quality scores.                                                                                                                               |
| **Boards repo URL** (optional)             | The `https://` URL of a GitHub repo the credential above can push to, for example `https://github.com/you/contextmatrix-boards.git`. Create it empty; the server clones it and pushes the first board. Without a URL the boards live in a local repo under `~/.contextmatrix/boards`. |
| **Task-skills repo URL** (optional)        | The `https://` URL of a repo of task skills the agents read. Without one the agents read an empty local directory.                                                                                                                                                                    |

**GitHub App or PAT?** Pick the App on a personal GitHub account: you create
and install it yourself, workers receive one-hour tokens scoped to the repos
you pick, and nothing long-lived leaves the server. Pick a PAT on a corporate
account, where creating an App usually needs an organisation admin and a token
needs only you.

- **App**: have the App ID, the installation ID and the downloaded private key
  (`.pem` file) ready; the installer copies the key under `~/.contextmatrix`.
  Install the App on the boards repo, the task-skills repo and every project
  repo. Creation steps and the permission list are in
  [github-auth-setup.md](https://github.com/mhersson/contextmatrix/blob/main/docs/github-auth-setup.md).
- **PAT**: a classic token with the `repo` scope, or a fine-grained one with
  the permissions listed in the same document. The wizard opens GitHub's
  new-token page with the `repo` scope prefilled, so this is the one item you
  can create mid-wizard.

The remaining questions (login mode, ports, services) have defaults that suit
a laptop. An architect whose port is already taken is installed but not
started, and the next `update` tries again.

## Install

```bash
go install github.com/mhersson/contextmatrix-setup@latest
contextmatrix-setup install
```

The wizard explains each step. Every answer also has a flag; `--yes` runs
without prompts:

```bash
contextmatrix-setup install --yes --auth-mode none --github-mode pat --github-pat "$TOKEN" --openrouter-key "$KEY"
```

In `multi` login mode the installer opens the one-time link that creates the
first admin account.

## Every morning

```bash
contextmatrix-setup update
```

Pulls the five repos, shows what changed, rebuilds what moved, syncs your config
files with upstream (new keys added, removed keys dropped with a note, your
values kept), rebuilds a backend's worker images only when its repo moved, and
restarts only what changed. `--yes` skips the confirmation. An install made
before the architect gets it at its next `update`: built, configured in
`architect.yaml` and in `server.yaml`'s `backends.architect` (empty values there
are filled, as is `enabled: false`; other values you set are kept), and started.

The confirmation shows a summary table with one row per repo (from and to
commit, counts, what happens). Below it, each repo that was already installed
gets a rule and one row per merged PR from the first-parent history, with its
type badge and title. Direct commits appear as their own rows. Dependabot PRs
are dimmed, listed last, and folded into one row when a repo has more than
three.

![The changes view before update asks to go ahead](assets/update-changes.png)

Progress is one line per step. In a terminal, the output of git, make and
docker goes to a run log, `~/.cache/contextmatrix-setup/logs/update-<timestamp>.log`
(under `$XDG_CACHE_HOME` when set), and the newest 10 logs are kept. When a step
fails, its last 40 lines are shown with the log path.

After restarting, `update` checks each restarted service: `/healthz` on the
server and `/health` on the agent, chat and architect, on the port in its
config. It waits up to 60 seconds after the last restart. A service whose unit
stays down for 15 seconds is reported as not running, with its recent log
lines. Without service management, nothing is restarted or checked, and the
start commands are printed.

![A successful update run: steps and the result table](assets/update-run.png)

The run ends with a result table, one row per service that needed a restart:
its result, its health, the commit it now runs and, with Docker, the worker
image it runs.

`update` exits 1 when a row of the result table has a problem: an image that
did not build, an invalid config for a service that needed a restart, a failed
restart, or an unhealthy service.

With `NO_COLOR` set, `TERM=dumb`, or output that is not a terminal (a pipe, a
log), `update` prints plain text with the same layout, no colours and no escape
codes. Subprocess output streams inline and no run log is written.

## Other commands

| Command     | Effect                                                                                  |
| ----------- | --------------------------------------------------------------------------------------- |
| `status`    | Installed and cached commits, ports, services, images                                   |
| `migrate`   | Move an install from the apps' default locations under `~/.contextmatrix`, then install |
| `uninstall` | Remove services only; configs, state and binaries stay                                  |

`migrate` moves the old config and state files to their new locations and keeps
every value: keys, GitHub credentials, ports, model and repositories found in
the old config are carried over, and the wizard asks only for what the old
install never had. Boards and task-skills checkouts move only when asked
(`--move-repos` with `--yes`). Shipped
workflow skills under `~/.contextmatrix/workflow-skills` are refreshed from
upstream at the install that follows migration, so save a customised copy of a
shipped skill under its own name before migrating. Files that do not exist
upstream are never touched.

## Where things live

| Purpose                     | Path                                                                    |
| --------------------------- | ----------------------------------------------------------------------- |
| Configs                     | `~/.config/contextmatrix/{server,agent,chat,architect}.yaml`            |
| State, boards, skills, logs | `~/.contextmatrix/`                                                     |
| Installer record            | `~/.contextmatrix/setup/state.yaml`                                     |
| Source cache                | `~/.cache/contextmatrix-setup/src/`                                     |
| Update run logs             | `~/.cache/contextmatrix-setup/logs/`                                    |
| Binaries                    | `$GOBIN` or `$(go env GOPATH)/bin`                                      |
| Units                       | `~/.config/systemd/user/` or `~/Library/LaunchAgents/`                  |
| Service logs                | `journalctl --user -u contextmatrix` or `~/Library/Logs/contextmatrix/` |

The architect's state (`architect.db`, code checkouts, exports) lives in
`~/.contextmatrix/architect`.

Units are installer-owned. On Linux, customise with
`systemctl --user edit <name>`; the installer never touches drop-ins. On macOS
edit the config file, not the plist. launchd loads every plist in
`~/Library/LaunchAgents` at login, so a service the installer did not start
(GitHub skipped, invalid config, no docker, architect port taken) still starts
at the next login until the cause is fixed and `contextmatrix-setup update` is
run.

## Worker images

The agent and chat each get their default worker image, tagged per commit and
written to `base_image`, plus the `go-node`, `python`, `rust` and `jvm-node`
variants under their stable tags (`contextmatrix-agent-worker:go-node`,
`contextmatrix-chat-worker:python`, and so on). A project's
`remote_execution.worker_image` or `chat_worker_image` can name any of them from
the project-settings picker, and the name keeps resolving after an update
because the variant tags move to the rebuilt images. The images a rebuild untags
are removed; `status` lists what is built. An install made before the variants
existed builds them at its next `update`; one that recorded only some of them
records the rest at the repo's next image build. Until then `status` omits them,
and that build leaves the unrecorded images it replaces in place.

## Config files

The installer manages the key set; you own the values. Each file starts with a
header saying so and naming the upstream example file that documents every key
(`config.yaml.example` for the server and the architect, `serve.yaml.example`
for the agent and chat).
Comments are not preserved, so keep notes elsewhere. Paths are
written absolute: the agent and chat do not expand `~`, and the server does so
only for some keys.

The schema comes from each app's own `config defaults` command, so a fresh
upstream key appears at the next update and a removed one is dropped and listed
in the output. A value that fails the app's validation is reported with the key
path and that service is not restarted; fix it and rerun.

## Development

```bash
make test               # unit tests
make test-integration   # end-to-end with stub tools, no network
make lint
```

`CONTEXTMATRIX_SETUP_REPO_BASE=<dir>` makes the binary clone from
`<dir>/<repo>.git` instead of GitHub; the integration tests use it. The
integration suite asserts systemd behaviour and skips itself on macOS; the smoke
checklist below covers launchd.

## macOS smoke checklist

Run after changes to services or images; CI covers Linux only.

1. `contextmatrix-setup install` on a machine with Docker Desktop or OrbStack;
   confirm the plists load
   (`launchctl print gui/$UID/com.github.mhersson.contextmatrix`).
2. Confirm `DOCKER_HOST` appears in the agent plist when the docker context is
   not the default socket.
3. Confirm the agent config has
   `container_contextmatrix_url: http://host.docker.internal:<port>`.
4. `contextmatrix-setup update` after a commit in a backend repo; confirm
   `kickstart -k` restarted only that agent and the log file under
   `~/Library/Logs/contextmatrix/` continues.
5. `contextmatrix-setup uninstall`; confirm `launchctl print` no longer lists
   the four labels and the configs remain.
6. Confirm `~/Library/Logs/contextmatrix/contextmatrix-architect.log` shows the
   architect listening, and that `contextmatrix-setup status` lists `rg`.
