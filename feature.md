# Feature Outline

## CLI

- **Supported trackers**
  - Jira — issue keys `<PROJECT>-<number>` (e.g. `KAN-1`), project keys uppercase (e.g. `KAN`)
  - GitHub — issue keys `owner/repo#<number>` (e.g. `octocat/hello-world#42`), project keys `owner/repo`
  - GitLab — issue keys `namespace/project#<IID>` (e.g. `mygroup/myproject#42`), project keys `namespace/project`
  - Linear — issue keys `<TEAM>-<number>` (e.g. `ENG-123`), project keys uppercase (e.g. `ENG`)
- **Auto-detection** — tracker type is inferred from issue key format when possible (GitHub keys containing `/` and `#` are globally unique; other trackers require `--tracker` when multiple types are configured)
- **Commands**
  - `issues list` — list issues for a project (requires `--project`)
  - `issue get <key>` — fetch a single issue with metadata and description
  - `issue create <summary>` — create a new issue (requires `--project`)
  - `issue comment add <key> <body>` — add a comment to an issue
  - `issue comment list <key>` — list comments on an issue
  - `issue delete <key>` — delete (or close) an issue by key
  - `tracker list` — list configured tracker instances
  - `install --agent claude` — install Claude Code skills and agents
-
## Skills / Agents

- **`/human-ready` skill** (`.claude/skills/human-ready/SKILL.md`)
  - Accepts `<ticket-key>` argument
  - Delegates to the `human-ready` agent to fetch and evaluate the ticket
  - Presents the agent's assessment, then asks the user to fill in each missing or partial item via `AskUserQuestion`
  - Writes the completed readiness assessment to `.human/ready/<key>.md` (lowercased key)
- **`/human-plan` skill** (`.claude/skills/human-plan/SKILL.md`)
  - Accepts `<ticket-key>` argument
  - Delegates to the `human-planner` agent with prompt `Create an implementation plan for ticket <key>`
  - Writes the plan to `.human/plans/<key>.md` (lowercased key)
- **`human-ready` agent** (`.claude/agents/human-ready.md`)
  - Tools: Bash, Read
  - Runs `human tracker list` to discover configured trackers, then `human issue get <key>` to fetch the ticket
  - Evaluates against the Definition of Ready checklist (6 criteria):
    1. Clear description — is the problem or feature clearly stated?
    2. Acceptance criteria — are there concrete, testable conditions for "done"?
    3. Scope — is the ticket small enough for a single implementation effort?
    4. Dependencies — are external dependencies or blockers identified?
    5. Context — is the "why" explained (user need, business reason)?
    6. Edge cases — are failure modes or boundary conditions mentioned?
  - Returns a structured report (summary, status table with present/partial/missing per criterion, missing-information questions) without prompting the user directly
- **`human-planner` agent** (`.claude/agents/human-planner.md`)
  - Tools: Bash, Read, Grep, Glob, Write
  - Planning process:
    1. Fetch the ticket via `human issue get <key>`
    2. Explore the codebase with Glob, Grep, and Read to understand affected areas
    3. Identify existing patterns, conventions, and related code
    4. Produce a structured plan (context, ordered changes with rationale, verification steps)
    5. Write the plan to `.human/plans/<key>.md` (lowercased key)
- **Integration with Claude Code**
  - `human install --agent claude` writes skills and agents to `.claude/skills/` and `.claude/agents/`
  - `--personal` flag installs to `~/.claude/` for user-wide availability
  - Installs four files: `human-plan` skill, `human-ready` skill, `human-planner` agent, `human-ready` agent
