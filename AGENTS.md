# AGENTS.md - ContextMatrix Setup

## Coding conventions

### Go

- `internal/` for all packages - nothing exported outside the module.
- Wrap errors with `fmt.Errorf("operation: %w", err)`. Never swallow errors.
- `context.Context` is the first parameter of any function that does I/O.
- No mutable global state, no `init()` functions. Dependencies are injected
  via struct fields (`engine.Engine`) and wired in `internal/cli`.
- Engine output goes to the injected writer (`Engine.Out`) or `ui.Console`;
  only `internal/cli` and the wizard bind them to the terminal.
- Tests sit next to code (`update.go` -> `update_test.go`), table-driven
  where cases share a shape, with testify, `t.Helper()` in helpers and
  `t.TempDir()` for scratch dirs.
- Tests must exercise behavior. A test that only fails when someone rewords a
  string, renames a heading, or edits prose is not a test - do not write it,
  and delete it when found. Asserting exact text is legitimate only where the
  text is a wire contract a consumer parses; if no code reads it, no test
  should pin it.
- Format with `gofumpt -w .` (`make fmt`), not `gofmt`.

### Documentation

- Documentation must always be true: every statement matches the code at
  HEAD, and a change that makes a doc line false updates it in the same
  commit.
- Document the current state - what exists now and why, not how it got here.
- Comments explain only non-obvious decisions, constraints, safety invariants,
  or workarounds. Do not narrate what code does or record change history; git
  holds history. Keep comments to one or two tight lines unless a longer
  explanation is genuinely necessary. Rewrite or delete if you find comments
  that don't follow this rule.
- Never use em-dashes; use hyphens (-).
- Never reference plan phases, task numbers, or private card IDs in doc comments.

## Commit discipline

Run before every commit:

```bash
make test              # clean
make test-race         # clean
make test-integration  # clean
make lint              # clean
make build             # builds
```

- **Never commit to `main`.** Work on a feature branch; tasks of a plan or
  scope the user approved commit there without asking again. Never push
  without explicit user approval.
- Conventional commits: `type(scope): concise summary`. Always include a scope.
- Body uses bullet points for the what and why - no long paragraphs.
- Never reference plan phases, task numbers, or private card IDs in messages.
- No session links in commits or PRs.
