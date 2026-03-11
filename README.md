<img src="h-l2.svg" width="80" alt="human logo">

[![CI](https://github.com/StephanSchmidt/human/actions/workflows/ci.yml/badge.svg)](https://github.com/StephanSchmidt/human/actions/workflows/ci.yml)
[![codecov](https://codecov.io/gh/StephanSchmidt/human/branch/main/graph/badge.svg)](https://codecov.io/gh/StephanSchmidt/human)
[![Go Report Card](https://goreportcard.com/badge/github.com/stephanschmidt/human)](https://goreportcard.com/report/github.com/stephanschmidt/human)
[![Go Reference](https://pkg.go.dev/badge/github.com/stephanschmidt/human.svg)](https://pkg.go.dev/github.com/stephanschmidt/human)
[![Latest Release](https://img.shields.io/github/v/release/StephanSchmidt/human)](https://github.com/StephanSchmidt/human/releases/latest)
[![Dependabot](https://img.shields.io/badge/dependabot-enabled-blue?logo=dependabot)](https://github.com/StephanSchmidt/human/network/updates)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://github.com/StephanSchmidt/human/blob/main/LICENSE)

# human

[https://gethuman.sh](https://gethuman.sh)

**Human in the loop —** Issue tracker CLI for AIs. Reads and manages issues across Jira, GitHub, GitLab, Linear, Azure DevOps, and Shortcut with output as JSON and markdown.

- **One CLI for Jira, GitHub, GitLab, Linear, Azure DevOps, and Shortcut** — no tool-switching for the AI
- **JSON and markdown output** — pipe directly into LLMs - LLMs can work with it
- **Claude Code skills** turn PM tickets into implementation plans and bug analyses
- **Definition of Ready checks** AI catches incomplete tickets before coding starts

### Why

As AI agents write more code autonomously, the question becomes: who decides *what* gets built? With `human`, an AI reads a product ticket from the issue tracker, creates an implementation ticket with a plan, and a human reviews it before coding starts. That's the human in the loop — and that's the pun.

<img src="human-loop.svg" width="960" alt="human loop: PM Ticket → AI → Dev Ticket">

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

### Analyze a bug

The `/human-bug-plan` skill fetches a bug ticket (including comments for stack traces and logs), explores the codebase for root cause, and writes a structured bug analysis with a fix plan.

```
/human-bug-plan KAN-1
```

The analysis is written to `.human/bugs/kan-1.md`.

## Install

```bash
curl -sSfL gethuman.sh/install.sh | bash
```

Or with Homebrew:

```bash
brew install stephanschmidt/tap/human
```

Or with [mise](https://mise.jdx.dev):

```bash
mise use -g github:StephanSchmidt/human
```

Or with Go:

```bash
go install github.com/stephanschmidt/human@latest
```

## CLI usage

Commands output JSON by default for easy piping to scripts and LLMs. Use `--table` for human-readable output. The same commands work across Jira, GitHub, GitLab, Linear, Azure DevOps, and Shortcut — only the project identifier changes.

```bash
# List issues (JSON by default)
human issues list --project=KAN                    # Jira
human issues list --project=octocat/hello-world    # GitHub
human issues list --project=mygroup/myproject      # GitLab
human issues list --project=ENG                    # Linear
human issues list --project=Human                  # Azure DevOps
human issues list --project=MyProject              # Shortcut

# Human-readable table
human issues list --project=KAN --table

# Get a single issue as markdown
human issue get KAN-1
human issue get octocat/hello-world#42
human issue get mygroup/myproject#42
human issue get ENG-123
human issue get Human/42                           # Azure DevOps
human issue get 123                                # Shortcut

# Create an issue
human issue create --project=ENG "Implement feature"

# Add a comment to an issue
human issue comment add KAN-1 "This is done"

# List comments on an issue
human issue comment list KAN-1

# Use a named tracker instance from .humanconfig.yaml
human --tracker=work issues list --project=KAN
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

See [documentation.md](documentation.md) for configuration details including `.humanconfig.yaml` setup, environment variables, and settings resolution.
