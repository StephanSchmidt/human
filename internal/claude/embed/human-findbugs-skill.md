---
name: human-findbugs
description: Scan the codebase for bugs using an iterative multi-agent pipeline
---

# AI-Powered Bug Scanner

Scan this codebase for bugs using an iterative agent pipeline: reconnaissance once, then repeated deep analysis passes that accumulate candidate bugs, followed by triage when no new bugs are found.

## Phase 1: Reconnaissance

Create the output directory, then run the recon agent:

```bash
mkdir -p .human/bugs
```

```
Task(subagent_type="findbugs-recon", prompt="Perform reconnaissance on this codebase. Write your recon report to .human/bugs/.findbugs-recon.md")
```

Wait for the recon agent to finish before proceeding.

## Phase 2: Initialize candidates file

Create the empty candidates file and state file:

```bash
echo "# Findbugs Candidates" > .human/bugs/.findbugs-candidates.md
echo "" >> .human/bugs/.findbugs-candidates.md
cat > .human/bugs/.findbugs-state.md << 'EOF'
# Findbugs State
- iterations: 0
- last_new_candidates: -1
- total_candidates: 0
- status: running
EOF
```

## Phase 3: Iterative Deep Analysis

Repeat the following iteration block. Stop when an iteration finds zero new candidates.

### Iteration step

Generate a timestamp for this iteration:

```bash
ITER_TS=$(date +"%Y-%m-%d %H:%M:%S")
ITER_NUM=$(grep "^- iterations:" .human/bugs/.findbugs-state.md | awk '{print $NF}')
ITER_NUM=$((ITER_NUM + 1))
echo "Starting iteration $ITER_NUM at $ITER_TS"
```

Launch all 4 analysis agents **in a single message** so they run in parallel:

```
Task(subagent_type="findbugs-logic", prompt="Read the recon report at .human/bugs/.findbugs-recon.md and existing candidates at .human/bugs/.findbugs-candidates.md. This is iteration ITER_NUM. Analyze the codebase for logic bugs. Append NEW findings only (skip anything already in candidates) to .human/bugs/.findbugs-candidates.md under a new '## Iteration ITER_NUM' heading. Write the count of new findings to .human/bugs/.findbugs-logic-count")

Task(subagent_type="findbugs-errors", prompt="Read the recon report at .human/bugs/.findbugs-recon.md and existing candidates at .human/bugs/.findbugs-candidates.md. This is iteration ITER_NUM. Analyze the codebase for error handling bugs. Append NEW findings only (skip anything already in candidates) to .human/bugs/.findbugs-candidates.md under a new '## Iteration ITER_NUM' heading. Write the count of new findings to .human/bugs/.findbugs-errors-count")

Task(subagent_type="findbugs-concurrency", prompt="Read the recon report at .human/bugs/.findbugs-recon.md and existing candidates at .human/bugs/.findbugs-candidates.md. This is iteration ITER_NUM. Analyze the codebase for concurrency bugs. Append NEW findings only (skip anything already in candidates) to .human/bugs/.findbugs-candidates.md under a new '## Iteration ITER_NUM' heading. Write the count of new findings to .human/bugs/.findbugs-concurrency-count")

Task(subagent_type="findbugs-api", prompt="Read the recon report at .human/bugs/.findbugs-recon.md and existing candidates at .human/bugs/.findbugs-candidates.md. This is iteration ITER_NUM. Analyze the codebase for API and security bugs. Append NEW findings only (skip anything already in candidates) to .human/bugs/.findbugs-candidates.md under a new '## Iteration ITER_NUM' heading. Write the count of new findings to .human/bugs/.findbugs-api-count")
```

Wait for all 4 agents to finish.

### Check convergence

Read the count files and sum new candidates:

```bash
LOGIC=$(cat .human/bugs/.findbugs-logic-count 2>/dev/null || echo 0)
ERRORS=$(cat .human/bugs/.findbugs-errors-count 2>/dev/null || echo 0)
CONCURRENCY=$(cat .human/bugs/.findbugs-concurrency-count 2>/dev/null || echo 0)
API=$(cat .human/bugs/.findbugs-api-count 2>/dev/null || echo 0)
NEW_TOTAL=$((LOGIC + ERRORS + CONCURRENCY + API))
TOTAL=$(grep -c "^### C-" .human/bugs/.findbugs-candidates.md 2>/dev/null || echo 0)
echo "Iteration $ITER_NUM: $NEW_TOTAL new candidates ($LOGIC logic, $ERRORS errors, $CONCURRENCY concurrency, $API api). Total: $TOTAL"
```

Update the state file:

```bash
cat > .human/bugs/.findbugs-state.md << EOF
# Findbugs State
- iterations: $ITER_NUM
- last_new_candidates: $NEW_TOTAL
- total_candidates: $TOTAL
- status: running
EOF
```

Clean up count files for next iteration:

```bash
rm -f .human/bugs/.findbugs-logic-count .human/bugs/.findbugs-errors-count .human/bugs/.findbugs-concurrency-count .human/bugs/.findbugs-api-count
```

**Decision point:**
- If `NEW_TOTAL` is **0** → proceed to Phase 4 (Triage)
- If `NEW_TOTAL` is **> 0** → go back to "Iteration step" and repeat

## Phase 4: Triage

Update state to complete:

```bash
sed -i 's/status: running/status: triaging/' .human/bugs/.findbugs-state.md
```

Run the triage agent to validate, deduplicate, and produce the final report:

```
Task(subagent_type="findbugs-triage", prompt="Read all candidate findings from .human/bugs/.findbugs-candidates.md and the recon report from .human/bugs/.findbugs-recon.md. Validate each finding against the actual code, deduplicate, assign final severity, and write the final report to .human/bugs/. Clean up intermediate files when done.")
```

## After completion

Tell the user:
- How many iterations ran before convergence
- How many bugs were found (by severity)
- The path to the final report
- Any critical findings that need immediate attention
