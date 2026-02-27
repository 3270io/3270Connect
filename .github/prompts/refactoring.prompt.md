# Safe refactoring within 3270Connect boundaries

## When to use
- Simplifying duplicated workflow logic without changing behavior.
- Extracting helper functions inside existing packages (`main`, `connect3270`).
- Improving readability/maintainability while preserving CLI/API contracts.

## Inputs
- <goal>
- <scope>
- <files>
- <__filter_complete__></__filter_complete__>

## Prompt
Refactor for <goal> in <scope> with behavior preserved and minimal code movement. Keep architecture boundaries intact (`go3270Connect.go` orchestration vs `connect3270/` emulator/session operations). Do not add new dependencies or alter workflow JSON schema/step names. Update tests only where needed for confidence and keep edits constrained to <files>. Respect filter state: <__filter_complete__></__filter_complete__>.
