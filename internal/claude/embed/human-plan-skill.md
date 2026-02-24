---
name: human-plan
description: Fetch an issue tracker ticket and create an implementation plan
argument-hint: <ticket-key>
---

Delegate to the **human-planner** agent using the Task tool:

```
Task(subagent_type="human-planner", prompt="Create an implementation plan for ticket $ARGUMENTS")
```

After the agent finishes, tell the user: `Plan written to .human/plans/<key>.md` (with the actual lowercased key).
