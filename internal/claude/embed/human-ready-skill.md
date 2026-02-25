---
name: human-ready
description: Fetch an issue tracker ticket and evaluate it against a Definition of Ready checklist
argument-hint: <ticket-key>
---

Follow these steps in order:

1. **Delegate** to the **human-ready** agent using the Task tool:

```
Task(subagent_type="human-ready", prompt="Evaluate readiness of ticket $ARGUMENTS")
```

2. **Present** the agent's assessment to the user.

3. **Ask** the user to fill in each missing or partially present item using `AskUserQuestion`. Ask about all gaps — do not skip any.

4. **Write** the completed readiness assessment (the original assessment plus the user's answers) to `.human/ready/<key>.md` where `<key>` is the ticket key lowercased (e.g. `KAN-1` → `kan-1.md`). Create the directory first with `mkdir -p .human/ready`.

5. **Tell** the user: `Readiness written to .human/ready/<key>.md` (with the actual lowercased key).
