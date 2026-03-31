---
name: human-plan
description: Fetch an issue tracker ticket and create an implementation plan
argument-hint: <ticket-key>
---

# Implementation Plan Pipeline

Create an implementation plan using a 3-phase agent pipeline: draft, verify, finalize. The plan is embedded directly in the engineering ticket description — no plan files are created.

## Phase 1: Draft Plan

Run the planner agent. It returns the plan as its output (no files written):

```
Task(subagent_type="human-planner", prompt="Create an implementation plan for ticket $ARGUMENTS. Return the complete plan as your output. Do not write any files.")
```

Wait for the planner agent to finish. Capture its output as `<PLAN_CONTENT>`.

## Phase 2: Verify (parallel)

Launch both verification agents **in a single message** so they run in parallel. Pass the plan content inline using markers. Each agent returns its report as output (no files written):

```
Task(subagent_type="plan-verify-code", prompt="Verify all code references in the following implementation plan against the actual codebase. Return your verification report as output. Do not write any files.\n\n---BEGIN PLAN---\n<PLAN_CONTENT>\n---END PLAN---")

Task(subagent_type="plan-verify-docs", prompt="Verify all library, framework, and API assumptions in the following implementation plan against actual documentation and source. Return your verification report as output. Do not write any files.\n\n---BEGIN PLAN---\n<PLAN_CONTENT>\n---END PLAN---")
```

Wait for both agents to finish before proceeding.

## Phase 3: Finalize

Read both verification reports from the agent outputs.

If the verification reports found **no issues** (all OK, zero mismatches, zero missing):
- Proceed directly to creating the engineering ticket.

If the verification reports found **issues** (mismatches, missing references, unaccounted callers, deprecations, or unverifiable claims):
- Update `<PLAN_CONTENT>` to fix all verified issues:
  - Correct wrong signatures, types, or file paths
  - Add handling for unaccounted callers/dependents
  - Replace deprecated APIs with their replacements
  - Mark unverifiable claims with "UNVERIFIED — confirm before implementing"

Then ask the user which tracker and project to create the engineering ticket on:

1. Ask via `AskUserQuestion`: "Which tracker should the engineering ticket be created on? (e.g. linear, jira, github, gitlab, azuredevops, shortcut)"
2. Ask via `AskUserQuestion`: "What project should the ticket be created in? (e.g. 'HUM' for Linear, 'myorg/myrepo' for GitHub)"

Create the engineering ticket with the plan as the description. Use a heredoc to handle special characters:

```bash
human <tracker> issue create --project=<PROJECT> "Short title from plan" --description "$(cat <<'PLAN_EOF'
<FINAL_PLAN_CONTENT>
PLAN_EOF
)"
```

## After completion

Tell the user:
- A short summary of the plan (3-5 bullet points: what will change, key files, risks)
- Whether verification found issues and what was corrected
- The engineering ticket key
