# Security hardening for 3270Connect

## When to use
- Touching token handling (`-token`, API `Token`, `{{token}}` replacement).
- Modifying API endpoints, config parsing, or external host input handling.
- Reviewing changes in emulator process/session management for unsafe behavior.

## Inputs
- <goal>
- <scope>
- <files>
- <__filter_complete__></__filter_complete__>

## Prompt
Perform a security-focused implementation for <goal> in <scope> with minimal diffs in <files>. Preserve existing runtime secret-handling model (no secrets in workflow JSON), keep input validation strict, and maintain current retry/deadline protections in TN3270 communication. Identify threat impact, apply localized fixes, and ensure compatibility with current CLI/API contracts. Respect filter state: <__filter_complete__></__filter_complete__>.
