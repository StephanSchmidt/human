# Documentation

## Configuration

`.humanconfig.yaml` holds named tracker instances. Multiple instances per tracker are supported. By default the first entry is used; select a specific one with `--tracker`:

```yaml
jiras:
  - name: work
    url: https://work.atlassian.net
    user: me@work.com
    key: api-token

githubs:
  - name: personal
    token: ghp_xxx

gitlabs:
  - name: work
    token: glpat-xxx

linears:
  - name: work
    token: lin_xxx

azuredevops:
  - name: work
    org: myorg
    token: pat-xxx

shortcuts:
  - name: work
    token: xxx
```

Select a specific instance with `--tracker`:

```bash
human --tracker=personal issues list --project=KAN
human --tracker=work issues list --project=octocat/hello-world
```

When only one tracker type is configured, it is auto-detected. When multiple tracker types are configured, specify which one with `--tracker=<name>`.

List all configured trackers (JSON output, also the default when run without arguments):

```bash
human tracker list
```

### Settings resolution

Each setting is resolved in priority order (highest wins):

1. **CLI flags** (e.g. `--jira-url`)
2. **Global env vars** (e.g. `JIRA_URL`)
3. **Per-instance env vars** (e.g. `JIRA_WORK_URL` — name uppercased)
4. **`.humanconfig.yaml`** — selected entry fills remaining gaps

| Tracker | Env prefix | Settings | Default URL |
|---------|-----------|----------|-------------|
| Jira | `JIRA_` | `URL`, `USER`, `KEY` | — |
| GitHub | `GITHUB_` | `URL`, `TOKEN` | `https://api.github.com` |
| GitLab | `GITLAB_` | `URL`, `TOKEN` | `https://gitlab.com` |
| Linear | `LINEAR_` | `URL`, `TOKEN` | `https://api.linear.app` |
| Azure DevOps | `AZURE_` | `URL`, `ORG`, `TOKEN` | `https://dev.azure.com` |
| Shortcut | `SHORTCUT_` | `URL`, `TOKEN` | `https://api.app.shortcut.com` |
