# Performance tuning for 3270 workflows

## When to use
- Reducing end-to-end runtime for high-concurrency runs (`-concurrency`, `-timeout`).
- Investigating slow step execution or retry-heavy TN3270 sessions.
- Optimizing API/dashboard telemetry paths without changing workflow semantics.

## Inputs
- <goal>
- <scope>
- <files>
- <__filter_complete__></__filter_complete__>

## Prompt
Improve performance in <scope> for goal <goal> using only existing repo patterns. Prioritize hotspots in `go3270Connect.go` workflow loops, `connect3270/emulator.go` retry/IO paths, and shared in-memory history buffers. Keep behavior backward compatible with existing JSON workflow schema and CLI flags. Use minimal code changes, preserve safety around mutex/atomic access, and propose concrete measurements (before/after command timing using existing binaries/tests) for files: <files>. Respect filter state: <__filter_complete__></__filter_complete__>.
