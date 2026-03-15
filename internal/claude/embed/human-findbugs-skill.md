---
name: human-findbugs
description: Scan the codebase for bugs using a multi-agent pipeline
---

# AI-Powered Bug Scanner

Scan this codebase for bugs using a 3-phase agent pipeline: reconnaissance, deep analysis, and triage.

## Phase 1: Reconnaissance

Create the output directory, then run the recon agent:

```bash
mkdir -p .human/bugs
```

```
Task(subagent_type="findbugs-recon", prompt="Perform reconnaissance on this codebase. Write your recon report to .human/bugs/.findbugs-recon.md")
```

Wait for the recon agent to finish before proceeding.

## Phase 2: Deep Analysis (parallel)

Launch all 4 analysis agents **in a single message** so they run in parallel:

```
Task(subagent_type="findbugs-logic", prompt="Read the recon report at .human/bugs/.findbugs-recon.md, then analyze the codebase for logic bugs. Write findings to .human/bugs/.findbugs-logic.md")

Task(subagent_type="findbugs-errors", prompt="Read the recon report at .human/bugs/.findbugs-recon.md, then analyze the codebase for error handling bugs. Write findings to .human/bugs/.findbugs-errors.md")

Task(subagent_type="findbugs-concurrency", prompt="Read the recon report at .human/bugs/.findbugs-recon.md, then analyze the codebase for concurrency bugs. Write findings to .human/bugs/.findbugs-concurrency.md")

Task(subagent_type="findbugs-api", prompt="Read the recon report at .human/bugs/.findbugs-recon.md, then analyze the codebase for API and security bugs. Write findings to .human/bugs/.findbugs-api.md")
```

Wait for all 4 agents to finish before proceeding.

## Phase 3: Triage

Run the triage agent to validate, deduplicate, and produce the final report:

```
Task(subagent_type="findbugs-triage", prompt="Read all analysis reports from .human/bugs/.findbugs-*.md, validate findings against the actual code, deduplicate, and write the final report to .human/bugs/")
```

## After completion

Tell the user:
- How many bugs were found (by severity)
- The path to the final report
- Any critical findings that need immediate attention
