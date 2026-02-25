<img src="h-l1.svg" width="80" alt="human logo">

# human

AI-powered issue tracker CLI. Reads and manages issues across Jira and GitHub with output as JSON and markdown.

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

Commands output JSON by default for easy piping to scripts and LLMs. Use `--table` for human-readable output.

### Jira examples

```bash
# JSON output (default)
human issues list --project=KAN

# Human-readable table
human issues list --project=KAN --table

# Get a single issue as markdown
human issue get KAN-1

# With a named Jira instance
human --tracker=work issues list --project=KAN

# With explicit flags
human --jira-url=https://yourorg.atlassian.net --jira-user=you@example.com --jira-key=YOUR_TOKEN issues list --project=KAN
```

### GitHub examples

```bash
# List open issues
human issues list --project=octocat/hello-world

# Get a single issue as markdown
human issue get octocat/hello-world#42

# Create a new issue
human issue create --project=octocat/hello-world "Fix the bug"

# With a named GitHub instance
human --tracker=personal issues list --project=octocat/hello-world

# With explicit flags
human --github-token=ghp_xxx issues list --project=octocat/hello-world
```

## Setup

```bash
cp .humanconfig.example .humanconfig.yaml
# edit .humanconfig.yaml with your tracker instances
```

## Build

```bash
make build
```

## Configuration

`.humanconfig.yaml` holds named tracker instances:

### Jira

```yaml
jiras:
  - name: work
    url: https://work.atlassian.net
    user: me@work.com
    key: work-api-token
  - name: personal
    url: https://personal.atlassian.net
    user: me@personal.com
    key: personal-api-token
```

### GitHub

```yaml
githubs:
  - name: personal
    # url: https://api.github.com  # optional, this is the default
    token: ghp_xxx
  - name: work
    url: https://github.example.com/api/v3
    token: ghp_yyy
```

By default the first entry is used. Select a specific instance with `--tracker`:

```bash
human --tracker=personal issues list --project=KAN
human --tracker=work issues list --project=octocat/hello-world
```

When only one tracker type is configured, it is auto-detected. When both Jira and GitHub are configured, specify which one with `--tracker=<name>`.

List all configured trackers (JSON output, also the default when run without arguments):

```bash
human tracker list
```

### Jira settings resolution

Settings are resolved in priority order (highest wins):

1. **CLI flags** (`--jira-url`, `--jira-user`, `--jira-key`)
2. **Global environment variables** (`JIRA_URL`, `JIRA_USER`, `JIRA_KEY`)
3. **Per-instance environment variables** (`JIRA_<NAME>_URL`, `JIRA_<NAME>_USER`, `JIRA_<NAME>_KEY` — name is uppercased, e.g. `JIRA_WORK_KEY`)
4. **`.humanconfig.yaml` file** — selected entry fills remaining gaps

### GitHub settings resolution

1. **CLI flags** (`--github-url`, `--github-token`)
2. **Global environment variables** (`GITHUB_URL`, `GITHUB_TOKEN`)
3. **Per-instance environment variables** (`GITHUB_<NAME>_URL`, `GITHUB_<NAME>_TOKEN` — name is uppercased, e.g. `GITHUB_PERSONAL_TOKEN`)
4. **`.humanconfig.yaml` file** — selected entry fills remaining gaps

The GitHub API URL defaults to `https://api.github.com` when not set.
