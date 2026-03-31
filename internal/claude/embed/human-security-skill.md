---
name: human-security
description: Scan the codebase for security vulnerabilities using an iterative multi-agent pipeline
---

# AI-Powered Security Scanner

Scan this codebase for security vulnerabilities using an iterative agent pipeline: attack surface mapping once, then repeated specialized scanning passes that accumulate candidate findings, followed by exploitation analysis and triage when no new findings emerge.

## Phase 1: Attack Surface Mapping

Create the output directory, then run the surface mapper:

```bash
mkdir -p .human/security
```

```
Task(subagent_type="security-surface", prompt="Map the attack surface of this codebase. Write your report to .human/security/.security-surface.md")
```

Wait for the surface mapper to finish before proceeding.

## Phase 2: Initialize candidates file

Create the empty candidates file and state file:

```bash
echo "# Security Candidates" > .human/security/.security-candidates.md
echo "" >> .human/security/.security-candidates.md
cat > .human/security/.security-state.md << 'EOF'
# Security State
- iterations: 0
- last_new_candidates: -1
- total_candidates: 0
- status: running
EOF
```

## Phase 3: Iterative Specialized Scanning

Repeat the following iteration block. Stop when an iteration finds zero new candidates.

### Iteration step

Generate a timestamp for this iteration:

```bash
ITER_TS=$(date +"%Y-%m-%d %H:%M:%S")
ITER_NUM=$(grep "^- iterations:" .human/security/.security-state.md | awk '{print $NF}')
ITER_NUM=$((ITER_NUM + 1))
echo "Starting iteration $ITER_NUM at $ITER_TS"
```

Launch all 5 scanning agents **in a single message** so they run in parallel:

```
Task(subagent_type="security-injection", prompt="Read the attack surface report at .human/security/.security-surface.md and existing candidates at .human/security/.security-candidates.md. This is iteration ITER_NUM. Analyze the codebase for injection and input validation vulnerabilities. Append NEW findings only (skip anything already in candidates) to .human/security/.security-candidates.md. Write the count of new findings to .human/security/.security-injection-count")

Task(subagent_type="security-auth", prompt="Read the attack surface report at .human/security/.security-surface.md and existing candidates at .human/security/.security-candidates.md. This is iteration ITER_NUM. Analyze the codebase for authentication, authorization, and session management vulnerabilities. Append NEW findings only (skip anything already in candidates) to .human/security/.security-candidates.md. Write the count of new findings to .human/security/.security-auth-count")

Task(subagent_type="security-secrets", prompt="Read the attack surface report at .human/security/.security-surface.md and existing candidates at .human/security/.security-candidates.md. This is iteration ITER_NUM. Scan the codebase and git history for leaked secrets, hardcoded credentials, and weak cryptography. Append NEW findings only (skip anything already in candidates) to .human/security/.security-candidates.md. Write the count of new findings to .human/security/.security-secrets-count")

Task(subagent_type="security-deps", prompt="Read the attack surface report at .human/security/.security-surface.md and existing candidates at .human/security/.security-candidates.md. This is iteration ITER_NUM. Audit dependencies for known vulnerabilities and supply chain risks. Append NEW findings only (skip anything already in candidates) to .human/security/.security-candidates.md. Write the count of new findings to .human/security/.security-deps-count")

Task(subagent_type="security-infra", prompt="Read the attack surface report at .human/security/.security-surface.md and existing candidates at .human/security/.security-candidates.md. This is iteration ITER_NUM. Analyze configuration files, Dockerfiles, CI pipelines, and infrastructure settings for security misconfigurations. Append NEW findings only (skip anything already in candidates) to .human/security/.security-candidates.md. Write the count of new findings to .human/security/.security-infra-count")
```

Wait for all 5 agents to finish.

### Check convergence

Read the count files and sum new candidates:

```bash
INJECTION=$(cat .human/security/.security-injection-count 2>/dev/null || echo 0)
AUTH=$(cat .human/security/.security-auth-count 2>/dev/null || echo 0)
SECRETS=$(cat .human/security/.security-secrets-count 2>/dev/null || echo 0)
DEPS=$(cat .human/security/.security-deps-count 2>/dev/null || echo 0)
INFRA=$(cat .human/security/.security-infra-count 2>/dev/null || echo 0)
NEW_TOTAL=$((INJECTION + AUTH + SECRETS + DEPS + INFRA))
TOTAL=$(grep -c "^### C-" .human/security/.security-candidates.md 2>/dev/null || echo 0)
echo "Iteration $ITER_NUM: $NEW_TOTAL new candidates ($INJECTION injection, $AUTH auth, $SECRETS secrets, $DEPS deps, $INFRA infra). Total: $TOTAL"
```

Update the state file:

```bash
cat > .human/security/.security-state.md << EOF
# Security State
- iterations: $ITER_NUM
- last_new_candidates: $NEW_TOTAL
- total_candidates: $TOTAL
- status: running
EOF
```

Clean up count files for next iteration:

```bash
rm -f .human/security/.security-injection-count .human/security/.security-auth-count .human/security/.security-secrets-count .human/security/.security-deps-count .human/security/.security-infra-count
```

**Decision point:**
- If `NEW_TOTAL` is **0** → proceed to Phase 4 (Exploitation Analysis)
- If `NEW_TOTAL` is **> 0** → go back to "Iteration step" and repeat

## Phase 4: Exploitation Analysis

Update state:

```bash
sed -i 's/status: running/status: chains/' .human/security/.security-state.md
```

Run the attack chain agent to connect individual findings into exploitable paths:

```
Task(subagent_type="security-chains", prompt="Read all candidate findings from .human/security/.security-candidates.md and the attack surface map from .human/security/.security-surface.md. Trace data flows to build attack chains that connect individual candidate findings into exploitable paths. Reference candidates by their C-NNN IDs. Write your analysis to .human/security/.security-chains.md")
```

Wait for the chain analysis to finish before proceeding.

## Phase 5: Triage

Update state:

```bash
sed -i 's/status: chains/status: triaging/' .human/security/.security-state.md
```

Run the triage agent to validate, deduplicate, and produce the final report:

```
Task(subagent_type="security-triage", prompt="Read all candidate findings from .human/security/.security-candidates.md, the attack chain analysis from .human/security/.security-chains.md, and the surface map from .human/security/.security-surface.md. Validate every finding against actual code, assign severity, and write the final security report to .human/security/. Clean up intermediate files when done.")
```

## After completion

Tell the user:
- How many iterations ran before convergence
- How many vulnerabilities were found (by severity)
- Any critical findings that need immediate attention
- The path to the final report
- If attack chains were found, highlight the most dangerous one
