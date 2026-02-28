


## More Trackers

* ~~Gitlab~~ ✅
* ~~Azure Devops~~ ✅
* ~~Shortcut~~ ✅

## AI Features

### Phase 1: Perfect PM → DEV Plan flow

⚠️ **#1 priority: No AI slop.** Plans, subtasks, and refinements must read like a senior engineer wrote them — not generic, not verbose, not templated. If a PM or dev looks at the output and thinks "AI wrote this", we failed.

* /human-split — Break large tickets into subtasks, create them in the tracker
* /human-spike — Research plan for uncertain/vague tickets
* /human-refine — Iterate on existing plan with feedback instead of regenerating

### Phase 2: Guardrails & Gates

* Architecture constraints — define rules before generation (layer boundaries, dependency policies, no-touch zones)
* Permission boundaries — restrict which directories/files/systems AI is allowed to modify
* Pattern consistency — enforce existing codebase conventions (error handling, naming, structure)
* Dependency vetting — block unreviewed/unlicensed/vulnerable dependencies

### Phase 3: Verification & Security

* /human-verify — Compare git diff against .human/plans/<key>.md, report done/missing/drifted
* /human-review — Review PR against PM ticket + implementation plan, check AC coverage
* /human-drift — Lighter mid-work scope drift check
* Test proof — require tests added/updated, passing, coverage held as a hard gate
* Security review — agent checks diff for common vulnerabilities (injection, leaked secrets, unsafe deserialization)

* Blast radius analysis — show impact surface (affected callers, dependents) before review
* Breaking change detection — flag public API changes, schema migrations, config format changes

### Phase 4: Audit & Close the loop

* Audit trail — full provenance: ticket → plan → model version → diff → review verdict (immutable, queryable)
* License compliance — detect license conflicts in added dependencies
* Human-in-the-loop proof — evidence that a human approved before merge
* Auto-comment plan summary on the original ticket
* Implement TransitionIssue — move tickets through workflow states from skills
* /human-done — Compare git changes against PM ticket, report what's done and what's still missing
* /human-finish — Write PR description from diff + ticket, comment summary on ticket, transition status — one command, zero writing
* /human-next — Look at my assigned tickets, rank by priority/dependencies/blockers, pull in context so I can start immediately
