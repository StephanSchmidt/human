---
name: human-bug-plan
description: Fetch a bug ticket and create a root-cause analysis with fix plan
argument-hint: <ticket-key>
---

Delegate to the **human-bug-analyzer** agent using the Task tool:

```
Task(subagent_type="human-bug-analyzer", prompt="Analyze bug ticket $ARGUMENTS")
```

After the agent finishes, tell the user: `Bug analysis written to .human/bugs/<key>.md` (with the actual lowercased key).
