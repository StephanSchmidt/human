# Project

'human' enables AI to act as human developers.

Phase 1: Interact with issue trackers with product management issues and create implementation tickets with an implementation plan.

# Done Done

The status of 'done done' means not only things are done,
but nothing more needs to be done, e.g. change documentation,
website, configuration and the project is in the state for 
a new release.

Whatever you do, the new state needs to be 'done done'.

# Tickets

There are two kinds of tickets:

- **PM tickets** (Shortcut, project "Stephan Schmidt's Team"): Product requirements, user stories, feature requests. Read these to understand what to build.
- **Engineering tickets** (Linear, project "HUM"): Implementation plans, technical tasks, bugs. Create these when planning and executing work.

Use 'human' to read and write tickets on both trackers.

Maintain traceability from PM ticket → engineering ticket → git commits. Reference the Shortcut ticket in the Linear ticket, and reference the PM ticket (Shortcut) in commit messages.

# Daemon

When the human daemon is running, all CLI commands (except `daemon`, `install`, `init`, `tui`) are automatically forwarded to it. The daemon holds all tracker credentials on the host — **do NOT set tokens manually when the daemon is running**. Just run `human` commands directly.

The daemon is auto-discovered via `~/.human/daemon.json`. Check with `human daemon status`.

# Tracker Tokens (Daemon Host Setup)

These tokens only need to be set **once on the host where the daemon runs**. They are NOT needed for individual CLI invocations when the daemon is running.

## Preferred: Native vault provider (1Password)

Add a `vault` section and use `1pw://` references directly in `.humanconfig.yaml`:

```yaml
vault:
  provider: 1password
  account: my-account    # 1Password account name (top-left in app sidebar)

githubs:
  - name: personal
    token: 1pw://Development/GitHub PAT/token

linears:
  - name: work
    token: 1pw://Development/Linear Token/token

jiras:
  - name: amazingcto
    url: https://amazingcto.atlassian.net
    user: alice@example.com
    key: 1pw://Development/Jira API Key/token
```

Secrets are resolved via the 1Password desktop app integration. The 1Password app must be installed and running — it will prompt for biometric or master password authentication. Enable "Integrate with other apps" in 1Password Settings > Developer.

## Alternative: Environment variables

Tracker API tokens can also be injected via env vars (legacy approach):

```sh
export SHORTCUT_HUMAN_TOKEN="$(op.exe item get 'Shortcut Token' --fields label=notesPlain)"
export LINEAR_WORK_TOKEN="$(op.exe item get 'Linear Token' --fields label=notesPlain)"
export JIRA_AMAZINGCTO_KEY="$(op.exe item get 'Jira API Key' --fields label=notesPlain)"
export GITLAB_HUMAN_TOKEN="$(op.exe item get 'Gitlab Token' --fields label=notesPlain)"
export AZUREDEVOPS_GETHUMAN_TOKEN="$(op.exe item get 'Azure Token' --fields label=notesPlain)"
export TELEGRAM_BOT_TOKEN="$(op.exe item get 'Telegram Token' --fields label=notesPlain)"
```

The env var naming convention is `<TRACKER>_<CONFIG_NAME>_TOKEN` (or `_KEY` for Jira), matching the uppercase `name:` field in `.humanconfig`.

# Project Structure

- `main.go` — CLI entry point
- `internal/tracker/` — Provider-agnostic issue tracker interfaces (Lister, Getter, Creator, etc.)
- `internal/vault/` — Pluggable vault secret resolution (1Password, extensible to Vault/AWS/etc.)
- `internal/jira/` — Jira API client and types
- `errors/` — Custom error handling (WithDetails)

internal/tracker/ is an abstraction layer for issue trackers. **ALWAYS** define new tracker operations as interfaces in `internal/tracker/`. **NEVER** add provider-specific types or logic to `internal/tracker/`. Concrete implementations (JIRA, Linear, Github, etc.) go under `internal/<provider>/` and **MUST** implement the `internal/tracker/` interfaces.

# Tools

Is it about finding FILES? use 'fd' instead of 'find'
Is it about finding TEXT/strings? use 'rg' instead of 'grep'
Is it about interacting with Markdown? use 'mdq'
Is it about interacting with JSON? use 'jq'
Use 'sd' instead of 'sed'
Is it about interacting with YAML or XML? use 'yq'
For accessing Github **ALWAYS** use 'gh'

# Commit

When asked to commit, go through changes and create atomar commits that have one connected change each.

Every commit message **must** contain an issue reference, **unless** the commit touches only documentation (`README.md`, `CLAUDE.md`, `LICENSE`, `CHANGELOG.md`, `CONTRIBUTING.md`, `CODE_OF_CONDUCT.md`, or anything under `docs/`). Any commit that touches code or config — including a mixed docs+code commit — still needs a ref. Accepted formats: `Issue #123`, `Issue HUM-30`, `[SC-57]`, `octocat/repo#42`, `MyProject/42`. A `commit-msg` hook enforces this — activate with `make hooks`.

# Code

**ALWAYS** use WithDetails for error creation.

# Process

Use todo list as much as possible.

# Release

By default increase versions for a release by 0.1.0

# Verification
 
Run 'make test' before and after changes. Run 'make lint' after changes. **ALWAYS** run 'make check' before pushing.

Treat tests as a second source of truth. **ALWAYS** check for failing tests if the code is wrong or the test is wrong. Fix accordingly. Testcoverage is not allowed to fall below 80%.

Apply these refactorings after changes to keep code testable:
- 'Extract Interface': Accept interfaces instead of concrete types if possible.
- 'Inject Dependencies': Pass dependencies as function/constructor parameters instead of creating them internally.
- 'Extract Function': Pull out logic that is hard to reach via the outer function's inputs into its own function.
- 'Decompose Conditional': Replace IF conditionals and nested IFs with clear, named conditions or early returns.
