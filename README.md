# human

AI-powered issue tracker CLI. Reads and manages Jira issues with output as plain text tables and markdown.

## Setup

```bash
cp .env.example .env
# edit .env with your Jira URL, user email, and 1Password CLI path
```

Required environment variables:

| Variable | Description |
|----------|-------------|
| `JIRA_URL` | Jira base URL (e.g. `https://yourorg.atlassian.net`) |
| `JIRA_USER` | Jira user email |
| `JIRA_KEY` | Jira API token |

## Build

```bash
make build
```

## Usage

Each required value (`JIRA_URL`, `JIRA_USER`, `JIRA_KEY`) can be provided as a CLI flag, an environment variable, or via `.env` — and you can mix all three. Flags override env vars.

With everything in `.env` (simplest):

```bash
human issues list --project=KAN
human issue get KAN-1
```

With explicit flags:

```bash
human --jira-url=https://yourorg.atlassian.net --jira-user=you@example.com --jira-key=YOUR_TOKEN issues list --project=KAN
```

Mixed (e.g. URL and user from `.env`, token from a secret manager):

```bash
human --jira-key=$(op item get "Jira API Key" --fields notesPlain) issue get KAN-1 | llm 'summarize this'
```
