---
name: human-done
description: Fetch a ticket and evaluate whether the implementation is complete and shippable
argument-hint: <ticket-key>
---

Delegate to the **human-done** agent using the Task tool:

```
Task(subagent_type="human-done", prompt="Evaluate whether ticket $ARGUMENTS is done")
```

After the agent finishes, tell the user: `Done report written to .human/done/<key>.md` (with the actual lowercased key).
