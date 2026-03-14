---
name: human-review
description: Fetch a ticket and review the current branch's changes against its acceptance criteria
argument-hint: <ticket-key>
---

Delegate to the **human-reviewer** agent using the Task tool:

```
Task(subagent_type="human-reviewer", prompt="Review changes for ticket $ARGUMENTS")
```

After the agent finishes, tell the user: `Review written to .human/reviews/<key>.md` (with the actual lowercased key).
