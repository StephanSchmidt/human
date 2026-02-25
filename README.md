# human

AI-powered issue tracker CLI. Reads and manages Jira issues with output as plain text tables and markdown.

## Setup

```bash
cp .humanconfig.example .humanconfig
# edit .humanconfig with your Jira URL, user email, and API token
```

Required configuration:

| Variable | Description |
|----------|-------------|
| `JIRA_URL` | Jira base URL (e.g. `https://yourorg.atlassian.net`) |
| `JIRA_USER` | Jira user email |
| `JIRA_KEY` | Jira API token |

## Configuration

Settings are resolved in priority order (highest wins):

1. **CLI flags** (`--jira-url`, `--jira-user`, `--jira-key`)
2. **Shell environment variables** (`export JIRA_URL=...`)
3. **`.humanconfig` file** — YAML config, fills remaining gaps

Example `.humanconfig`:

```yaml
jira:
  url: https://yourorg.atlassian.net
  user: you@example.com
  key: your-api-token
```

## Build

```bash
make build
```

## Claude Code usage

Install the Claude Code skills and agents into your project:

```bash
human install --agent claude
```

This writes skill and agent files to `.claude/` in the current directory. Re-run after upgrading `human` to pick up changes.

### Check ticket readiness

The `/human-ready` skill fetches a ticket, evaluates it against a Definition of Ready checklist, and asks you to fill in any gaps. The result is saved for reference.

In Claude Code:

```
/human-ready KAN-1
```

The skill checks six criteria: clear description, acceptance criteria, scope, dependencies, context, and edge cases. For anything missing or incomplete, it asks you to provide the information. The completed assessment is written to `.human/ready/kan-1.md`.

### Create an implementation plan

The `/human-plan` skill fetches a ticket, explores the codebase, and produces a structured implementation plan.

```
/human-plan KAN-1
```

The plan is written to `.human/plans/kan-1.md`.

## CLI usage

Each required value (`JIRA_URL`, `JIRA_USER`, `JIRA_KEY`) can be provided as a CLI flag, an environment variable, or via `.humanconfig` — and you can mix all three. Flags override env vars.

With everything in `.humanconfig` (simplest):

```bash
human issues list --project=KAN
human issue get KAN-1
```

With explicit flags:

```bash
human --jira-url=https://yourorg.atlassian.net --jira-user=you@example.com --jira-key=YOUR_TOKEN issues list --project=KAN
```

Mixed (e.g. URL and user from `.humanconfig`, token from a flag):

```bash
human --jira-key=YOUR_TOKEN issue get KAN-1 | llm 'summarize this'
```
