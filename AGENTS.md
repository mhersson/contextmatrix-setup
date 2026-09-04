# AGENTS.md - ContextMatrix Setup

## Tech stack

- **Go 1.26+**

## Coding conventions

### Go

- `internal/` for all packages - nothing exported outside the module.

### Documentation

- Document the current state - what exists now and why, not how it got here.
- Comments explain only non-obvious decisions, constraints, safety invariants,
  or workarounds. Do not narrate what code does or record change history; git
  holds history. Keep comments to one or two tight lines unless a longer
  explanation is genuinely necessary. Rewrite or delete if you find comments
  that don't follow this rule.
- Never use em-dashes; use hyphens (-).
- Never reference plan phases, task numbers, or private card IDs in doc
  comments.

## Commit discipline

Run before every commit:

```bash
make test      # clean
make lint      # clean
make build     # builds
```

- Conventional commits: `type(scope): concise summary`. Always include a scope.
- Body uses bullet points for the what and why - no long paragraphs.
- Never reference plan phases, task numbers, or private card IDs in messages.
