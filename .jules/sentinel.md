# Sentinel learnings

Only CRITICAL patterns that recur go here.

- **`os.FindProcess` on Unix does no lookup**, so a pid handed to
  `Process.Kill` reaches `syscall.Kill(pid, SIGKILL)` verbatim. Pids ≤ 0
  have special POSIX meaning (0 = process group, -1 = every process the
  caller may signal). Any handler that takes a pid must gate `pid > 0`
  before `FindProcess`, and `pid == os.Getpid()` is not a substitute --
  it only covers the self-kill case.
- **An allow-list on one outbound tool is not an allow-list on the
  package.** MCP's `hostAllowed` fences TN3270 targets, but every outbound
  URL that lands at an MCP tool is a separate SSRF surface: `stepLatencies`
  dialled any `prometheus_url` the caller named. Grep for callers of
  `net/http.Client`, `exec.Command`, `net.Dial*` inside `mcp_*.go` /
  `*_handlers.go` and check each one applies the fence AND disables
  redirect following -- a 302 to an internal address slips past a check
  that only looked at the initial URL.
