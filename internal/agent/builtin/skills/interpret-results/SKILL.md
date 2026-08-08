---
name: interpret-results
description: Turn a finished run's counters, percentiles and step latencies into an answer someone can act on, without overstating what the numbers support.
invocation: [explain-results, analyse-run, read-metrics]
tools: [get_load_test_metrics, get_step_latencies, get_run_summary, get_console_log, list_load_tests]
instructions: [reading-latency]
---

# Interpreting a run

## When to use

A run has finished, or is running, and the user wants to know what it means.

## Gather

1. `get_load_test_metrics` — counters and duration percentiles.
2. `get_step_latencies` — per-step timings, if the run was started with
   `-promListen`. This is the only place per-step timing exists; without it
   you can say a workflow took 4 seconds but not which step took them.
3. `get_run_summary`, and `get_console_log` if something failed.

## Separate the outcomes

`success`, `failure` and `connect_failed` are three different things and
collapsing them into an error rate hides the most useful distinction:

- **connect_failed** — the host would not accept the session. A capacity or
  configuration limit, not a slow transaction. If these rise with
  concurrency, the ceiling is on sessions rather than on throughput.
- **failure** — the session opened and a step did not do what it should. A
  CheckValue that did not match is often the workflow describing a screen the
  host no longer shows, rather than the host being wrong.
- **success** — completed every step.

Timings from a run with failures need saying so. A workflow that failed at
step two is fast, and it pulls every percentile down with it.

## Say what the numbers support

p50 is the typical experience. p95 and p99 are what people complain about.
Quote the sample size with them — see `reading-latency.instructions.md` —
because the durations are a rolling window, not the whole run.

Per-step latencies usually point straight at the answer: one step much slower
than the rest is the transaction to look at, and if that step is `Connect`
the cost is in session setup rather than in the application at all.

## What not to conclude

- Do not derive a capacity figure from one concurrency level. That needs the
  `find-concurrency-knee` skill.
- Do not attribute a slow step to a cause you cannot see from here. This tool
  measures from outside the host: it can say a transaction took four seconds,
  never why. Name the step and hand it to someone who can look inside.
- Do not compare two runs that used different workflows, concurrency or
  durations, and present the difference as a change in the host.

## Anti-patterns

- Reporting a mean as though it described the experience.
- Quoting p99 from a few dozen samples as a property of the host.
- Turning "p95 is 3s" into "the host is overloaded" with nothing else
  supporting it.
