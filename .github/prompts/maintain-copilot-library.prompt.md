# Maintain Copilot instructions and prompt library

## When to use
- Repo structure, tooling commands, or CI steps changed.
- New conventions were added and Copilot guidance is drifting.
- Prompt files under `.github/prompts/` need consistency updates.

## Inputs
- <goal>
- <scope>
- <files>
- <__filter_complete__></__filter_complete__>

## Prompt
Synchronize Copilot guidance for <goal> in <scope> by updating existing files first (`.github/copilot-instructions.md`, then `.github/prompts/*.prompt.md`). Keep content specific to this repository’s Go/TN3270 architecture, scripts, and tests. Avoid generic advice and avoid adding path-scoped instruction files unless conventions genuinely differ by directory. Keep edits concise, verify command accuracy against repository files, and constrain updates to <files>. Respect filter state: <__filter_complete__></__filter_complete__>.
