# UX/UI update for dashboard and terminal flows

## When to use
- Changing dashboard template behavior in `templates/` or static assets in `app/static/`.
- Improving clarity of CLI/TUI output (`pterm`, lipgloss, spinner/status messaging).
- Adjusting API-driven dashboard interactions while preserving existing routes.

## Inputs
- <goal>
- <scope>
- <files>
- <__filter_complete__></__filter_complete__>

## Prompt
Implement a focused UX/UI improvement for <goal> in <scope>. Keep changes aligned with current dashboard/template structure and terminal output style used by `go3270Connect.go` + `charmui.go`. Do not introduce new UI frameworks. Preserve existing workflow/API behavior and only touch files in <files>. Include a short manual verification checklist and attach screenshot evidence for visible UI changes. Respect filter state: <__filter_complete__></__filter_complete__>.
