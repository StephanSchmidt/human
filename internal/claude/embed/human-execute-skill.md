---
name: human-execute
description: Load a plan and execute it step by step, then review the result
argument-hint: <ticket-key>
---

Delegate to the **human-executor** agent using the Task tool:

```
Task(subagent_type="human-executor", prompt="Execute the plan for ticket $ARGUMENTS")
```

After the agent finishes, tell the user what was done and whether the review checkpoint passed.
