---
name: human-triage
description: Fetch an issue tracker ticket and evaluate it against a Definition of Ready checklist
argument-hint: <ticket-key>
---

Follow these steps in order:

1. **Delegate** to the **human-triage** agent using the Task tool:

```
Task(subagent_type="human-triage", prompt="Triage ticket $ARGUMENTS")
```

2. **Present** the agent's assessment to the user.

3. **Ask** the user to fill in each missing or partially present item using `AskUserQuestion`. Ask about all gaps — do not skip any.

4. **Write** the completed triage (the original assessment plus the user's answers) to `.human/triage/<key>.md` where `<key>` is the ticket key lowercased (e.g. `KAN-1` → `kan-1.md`). Create the directory first with `mkdir -p .human/triage`.

5. **Tell** the user: `Triage written to .human/triage/<key>.md` (with the actual lowercased key).
