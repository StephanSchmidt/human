---
name: human-brainstorm
description: Brainstorm approaches for a ticket or topic before planning
argument-hint: <ticket-key or topic>
---

Follow these steps in order:

1. **Parse** `$ARGUMENTS`:
   - If it looks like a ticket key (e.g. `KAN-1`, `ENG-123`, `123`), treat it as a ticket key and set `<identifier>` to the lowercased key.
   - Otherwise treat it as a freeform topic and set `<identifier>` to a slugified version (lowercase, spaces to hyphens, strip special chars).

2. **Create** the output directory: `mkdir -p .human/brainstorms`

3. **Phase 1 — Context gathering**: Delegate to the **human-brainstormer** agent:

```
Task(subagent_type="human-brainstormer", prompt="Phase 1: Gather context for $ARGUMENTS. If this is a ticket key, fetch it with the human CLI. Explore the codebase for relevant code, existing patterns, and any .human/ artifacts. Return a context summary and 3-5 suggested clarifying questions.")
```

4. **Present** the agent's context summary to the user.

5. **Ask clarifying questions** one at a time using `AskUserQuestion`. Ask each of the agent's suggested questions individually (max 5). Collect all answers.

6. **Phase 2 — Generate approaches**: Delegate to the **human-brainstormer** agent with the collected answers:

```
Task(subagent_type="human-brainstormer", prompt="Phase 2: Generate approaches for $ARGUMENTS. Clarification answers: <paste all Q&A pairs>. Generate 2-3 approaches with trade-offs, pros/cons, complexity estimates, and a recommendation.")
```

7. **Present** the 2-3 approaches to the user with their trade-offs.

8. **Ask** the user which approach to proceed with using `AskUserQuestion`.

9. **Write** the complete brainstorm document to `.human/brainstorms/<identifier>.md` using the agent's structured output, adding the chosen approach.

10. **Tell** the user: `Brainstorm written to .human/brainstorms/<identifier>.md — run /human-plan $ARGUMENTS to create an implementation plan.`
