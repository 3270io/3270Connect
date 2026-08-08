---
name: ramp-up-load-test
description: Run a concurrent load test against a 3270 host and report what it did, from a first smoke run through to a result worth quoting.
invocation: [load-test, run-load, volume-test]
tools: [validate_workflow, run_workflow_once, start_load_test, get_load_test_metrics, get_live_workflow_status, stop_load_test]
instructions: [load-test-safety, reading-latency]
---

# Running a load test

## When to use

The user asks for a load test, a volume test, a soak, or wants to know how
many concurrent users a host will take.

## Before you generate any load

1. Confirm **which host**. A load test against the wrong LPAR is not something
   you can undo, and a test host and a production host often differ by one
   character. Say the hostname back and get agreement — see
   `load-test-safety.instructions.md`.
2. `validate_workflow` on the workflow you are about to run. A malformed step
   fails on every virtual user at once and tells you nothing about the host.
3. `run_workflow_once`. One user, once. If a single pass does not complete,
   a hundred concurrent ones will only produce a hundred copies of the same
   failure. Read the captured screens and confirm the workflow does what the
   user thinks it does.

## Sizing the run

Start smaller than the target and work up. A run that saturates the host on
its first ramp tells you it broke but not where.

- `concurrent` — virtual users. Begin at 5–10 even when the goal is hundreds.
- `runtime_sec` — how long to hold that load. Under about 60 seconds, ramp-up
  is most of the run and the numbers describe the ramp rather than the load.
- The workflow's own `RampUpBatchSize` and `RampUpDelay` control how quickly
  users arrive. Arriving all at once measures the host's ability to accept
  connections, which is rarely the question.

## While it runs

`get_load_test_metrics` for the counters and the duration percentiles.
`get_live_workflow_status` for where each virtual user currently is.

The second is what to reach for when throughput drops: if every worker is
sitting on the same step, the host is slow at one transaction rather than
slow in general, and that is a different problem with a different fix.

## Reporting

Read `reading-latency.instructions.md` before quoting a percentile. The
short version: `Durations` is a rolling window of recent workflows, not the
whole run, so say what the sample was.

Report, in this order:

1. What was run — host, workflow, concurrency, duration.
2. Completed, failed, and connect failures separately. A connect failure is
   the host refusing to talk, not a transaction failing, and mixing them
   hides a capacity limit behind an apparent error rate.
3. p50, p95 and p99 workflow duration, with the sample size.
4. What the numbers mean for the question asked.

## Anti-patterns

- Starting at the target concurrency because that is what was asked for.
- Quoting a mean. One slow tail is what everybody notices and a mean hides it.
- Reporting a run that had failures as though the timings are still valid —
  workflows that failed early are fast, and they drag every percentile down.
- Leaving a run going after answering. Call `stop_load_test`.
