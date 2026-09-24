# Sentinel learnings

Only CRITICAL patterns that recur go here.

- **`os.FindProcess` on Unix does no lookup**, so a pid handed to
  `Process.Kill` reaches `syscall.Kill(pid, SIGKILL)` verbatim. Pids ≤ 0
  have special POSIX meaning (0 = process group, -1 = every process the
  caller may signal). Any handler that takes a pid must gate `pid > 0`
  before `FindProcess`, and `pid == os.Getpid()` is not a substitute --
  it only covers the self-kill case.
