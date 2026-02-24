# Project

'human' enables AI to act as human developers.

Phase 1: Interact with issue trackers with product management issues and create implementation tickets with an implementation plan.

# Project Structure

- `cmd/human/` — CLI entry point
- `internal/tracker/` — Provider-agnostic issue tracker interfaces (Lister, Getter, Creator, etc.)
- `internal/jira/` — Jira API client and types
- `errors/` — Custom error handling (WithDetails)

internal/tracker/ is an abstraction layer for issue trackers. **ALWAYS** define new tracker operations as interfaces in `internal/tracker/`. **NEVER** add provider-specific types or logic to `internal/tracker/`. Concrete implementations (JIRA, Linear, Github, etc.) go under `internal/<provider>/` and **MUST** implement the `internal/tracker/` interfaces.

# Tools

Is it about finding FILES? use 'fd' instead of 'find'
Is it about finding TEXT/strings? use 'rg' instead of 'grep'
Is it about interacting with Markdown? use 'mdq'
Is it about interacting with JSON? use 'jq'
Use 'sd' instead of 'sed'
Is it about interacting with YAML or XML? use 'yq'
For accessing Github **ALWAYS** use 'gh'

# Commit

When asked to commit, go through changes and create atomar commits that have one connected change each.

# Code

**ALWAYS** use WithDetails for error creation.

# Verification
 
Run 'make test' before and after changes. Run 'make lint' after changes.

Treat tests as a second source of truth. **ALWAYS** check for failing tests if the code is wrong or the test is wrong. Fix accordingly.

Apply these refactorings after changes to keep code testable:
- 'Extract Interface': Accept interfaces instead of concrete types for external dependencies (HTTP clients, APIs, databases).
- 'Inject Dependencies': Pass dependencies as function/constructor parameters instead of creating them internally.
- 'Extract Function': Pull out logic that is hard to reach via the outer function's inputs into its own function.
- 'Decompose Conditional': Replace IF conditionals and nested IFs with clear, named conditions or early returns.
