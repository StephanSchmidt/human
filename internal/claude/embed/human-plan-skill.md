---
name: human-plan
description: Fetch an issue tracker ticket and create an implementation plan
argument-hint: <ticket-key>
---

# Implementation Plan Pipeline

Create an implementation plan using a 3-phase agent pipeline: draft, verify, finalize.

## Phase 1: Draft Plan

Create the output directory, then run the planner agent:

```bash
mkdir -p .human/plans
```

```
Task(subagent_type="human-planner", prompt="Create an implementation plan for ticket $ARGUMENTS")
```

Wait for the planner agent to finish before proceeding. Read the draft plan to extract the key:

```bash
ls .human/plans/
```

## Phase 2: Verify (parallel)

Launch both verification agents **in a single message** so they run in parallel:

```
Task(subagent_type="plan-verify-code", prompt="Read the draft plan at .human/plans/<key>.md and verify all code references against the actual codebase. Write your report to .human/plans/.verify-code-<key>.md")

Task(subagent_type="plan-verify-docs", prompt="Read the draft plan at .human/plans/<key>.md and verify all library, framework, and API assumptions against actual documentation and source. Write your report to .human/plans/.verify-docs-<key>.md")
```

Wait for both agents to finish before proceeding.

## Phase 3: Finalize

Read all three files:
- `.human/plans/<key>.md` (draft plan)
- `.human/plans/.verify-code-<key>.md` (code verification report)
- `.human/plans/.verify-docs-<key>.md` (docs verification report)

If the verification reports found **no issues** (all OK, zero mismatches, zero missing):
- Proceed directly to creating the engineering ticket.

If the verification reports found **issues** (mismatches, missing references, unaccounted callers, deprecations, or unverifiable claims):
- Update the plan in `.human/plans/<key>.md` to fix all verified issues:
  - Correct wrong signatures, types, or file paths
  - Add handling for unaccounted callers/dependents
  - Replace deprecated APIs with their replacements
  - Mark unverifiable claims with "UNVERIFIED — confirm before implementing"
- Write the corrected plan back to `.human/plans/<key>.md`

Then ask the user which tracker and project to create the engineering ticket on:

1. Ask via `AskUserQuestion`: "Which tracker should the engineering ticket be created on? (e.g. linear, jira, github, gitlab, azuredevops, shortcut)"
2. Ask via `AskUserQuestion`: "What project should the ticket be created in? (e.g. 'HUM' for Linear, 'myorg/myrepo' for GitHub)"

Create the engineering ticket:

```bash
human <tracker> issue create --project=<PROJECT> "Short title" --description "$(cat .human/plans/<key>.md)"
```

## After completion

Clean up the hidden verification reports:

```bash
rm -f .human/plans/.verify-code-<key>.md .human/plans/.verify-docs-<key>.md
```

Tell the user:
- A short summary of the plan (3-5 bullet points: what will change, key files, risks)
- Whether verification found issues and what was corrected
- `Plan written to .human/plans/<key>.md`
- The engineering ticket key if created
