---
name: human-security
description: Scan the codebase for security vulnerabilities using a multi-agent pipeline
---

# AI-Powered Security Scanner

Scan this codebase for security vulnerabilities using a 4-phase agent pipeline: attack surface mapping, specialized scanning, exploitation analysis, and triage.

## Phase 1: Attack Surface Mapping

Create the output directory, then run the surface mapper:

```bash
mkdir -p .human/security
```

```
Task(subagent_type="security-surface", prompt="Map the attack surface of this codebase. Write your report to .human/security/.security-surface.md")
```

Wait for the surface mapper to finish before proceeding.

## Phase 2: Specialized Scanning (parallel)

Launch all 5 scanning agents **in a single message** so they run in parallel:

```
Task(subagent_type="security-injection", prompt="Read the attack surface report at .human/security/.security-surface.md, then analyze the codebase for injection and input validation vulnerabilities. Write findings to .human/security/.security-injection.md")

Task(subagent_type="security-auth", prompt="Read the attack surface report at .human/security/.security-surface.md, then analyze the codebase for authentication, authorization, and session management vulnerabilities. Write findings to .human/security/.security-auth.md")

Task(subagent_type="security-secrets", prompt="Read the attack surface report at .human/security/.security-surface.md, then scan the codebase and git history for leaked secrets, hardcoded credentials, and weak cryptography. Write findings to .human/security/.security-secrets.md")

Task(subagent_type="security-deps", prompt="Read the attack surface report at .human/security/.security-surface.md, then audit dependencies for known vulnerabilities and supply chain risks. Write findings to .human/security/.security-deps.md")

Task(subagent_type="security-infra", prompt="Read the attack surface report at .human/security/.security-surface.md, then analyze configuration files, Dockerfiles, CI pipelines, and infrastructure settings for security misconfigurations. Write findings to .human/security/.security-infra.md")
```

Wait for all 5 agents to finish before proceeding.

## Phase 3: Exploitation Analysis

Run the attack chain agent to connect individual findings into exploitable paths:

```
Task(subagent_type="security-chains", prompt="Read all scanning reports from .human/security/.security-*.md and the surface map. Trace data flows to build attack chains that connect individual findings into exploitable paths. Write your analysis to .human/security/.security-chains.md")
```

Wait for the chain analysis to finish before proceeding.

## Phase 4: Triage

Run the triage agent to validate, deduplicate, and produce the final report:

```
Task(subagent_type="security-triage", prompt="Read all reports from .human/security/.security-*.md. Validate every finding against actual code, assign severity, and write the final security report to .human/security/")
```

## After completion

Tell the user:
- How many vulnerabilities were found (by severity)
- Any critical findings that need immediate attention
- The path to the final report
- If attack chains were found, highlight the most dangerous one
