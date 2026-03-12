# Project

'human' enables AI to act as human developers.

Phase 1: Interact with issue trackers with product management issues and create implementation tickets with an implementation plan.

# Done Done

The status of 'done done' means not only things are done,
but nothing more needs to be done, e.g. change documentation,
website, configuration and the project is in the state for 
a new release.

Whatever you do, the new state needs to be 'done done'.

# Project Structure

- `main.go` — CLI entry point
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

# Process

Use todo list as much as possible.

# Release

By default increase versions for a release by 0.0.1

# Verification
 
Run 'make test' before and after changes. Run 'make lint' after changes. **ALWAYS** run 'make check' before pushing.

Treat tests as a second source of truth. **ALWAYS** check for failing tests if the code is wrong or the test is wrong. Fix accordingly. Testcoverage is not allowed to fall below 80%.

Apply these refactorings after changes to keep code testable:
- 'Extract Interface': Accept interfaces instead of concrete types if possible.
- 'Inject Dependencies': Pass dependencies as function/constructor parameters instead of creating them internally.
- 'Extract Function': Pull out logic that is hard to reach via the outer function's inputs into its own function.
- 'Decompose Conditional': Replace IF conditionals and nested IFs with clear, named conditions or early returns.
