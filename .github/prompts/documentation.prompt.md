# Documentation update for 3270Connect behavior

## When to use
- CLI flags, workflow JSON fields, or API route behavior changed.
- Timeout/retry/security behavior updates need user-facing documentation.
- Build/test/docs commands in scripts or CI were adjusted.

## Inputs
- <goal>
- <scope>
- <files>
- <__filter_complete__></__filter_complete__>

## Prompt
Update documentation for <goal> within <scope> using repo documentation style (`README.md` plus MkDocs under `docs/`). Keep statements precise and mapped to actual code behavior in <files>. Include exact runnable commands from current repo scripts/workflows (`./build.sh`, `pwsh ./build.ps1`, `go test -v ./...`, `mkdocs build`) when relevant. Avoid speculative features. Respect filter state: <__filter_complete__></__filter_complete__>.
