---
name: before-after-comparison
description: Measure whether a change actually moved throughput or latency, by running the same workflow at the same concurrency on both sides and saying which differences the numbers support.
invocation: [compare-runs, before-and-after, did-it-help, regression-check]
tools: [list_workflows, validate_workflow, test_connection, start_load_test, get_load_test_metrics, get_step_latencies, get_run_summary, list_load_tests, stop_load_test]
instructions: [load-test-safety, reading-latency]
---

# Comparing a run before and after a change

## When to use

Someone changed something — a region setting, a JVM flag, more CICS
tasks, a code deployment, different hardware — and wants to know whether it
helped. Also for the reverse question: whether a change that was supposed to
be neutral cost anything.

Not for "how much load can this take". That is `find-concurrency-knee`.

## The comparison is only worth as much as what was held still

Everything below is one paragraph long because each one has, on its own,
produced a confident wrong answer:

- **The same workflow document.** A workflow edited between runs measures the
  edit. Take the path from `list_workflows` and use it for both sides.
- **The same concurrency and the same runtime.** Throughput is not linear in
  either, so a 10-user run and a 20-user run are not comparable by scaling.
- **The same host, and nothing else on it.** Another test running against the
  same region is not an experiment, it is two experiments sharing a host.
- **The same time of day, as far as possible.** A batch window is a change
  you did not make.

If any of these could not be held still, say which, and say it before the
numbers rather than after them.

## Steps

1. `list_workflows` — pick one document and use its path for both sides.
2. `validate_workflow` and `test_connection` once, before either run.
3. Run the **before** side: `start_load_test` with the chosen concurrency and
   runtime. Record the pid.
4. Poll `get_load_test_metrics` until it finishes. Collect
   `get_run_summary`, and `get_step_latencies` if the run had `-promListen`.
5. Ask the user to apply the change, and wait. Do not attempt it — this tool
   drives a terminal, it does not administer a host.
6. Run the **after** side with identical parameters.
7. Collect the same three artefacts.

Prefer at least two runs per side when the change is expected to be small.
One run per side can only distinguish differences larger than the run-to-run
variation, and nothing in a single pair says what that variation is.

## Reading the difference

Report each side's counters separately before any delta, so the reader can
see the sample sizes the comparison rests on.

- **Completed workflows** is the throughput figure. Compare the rate, not the
  total, unless both runtimes were identical to the second.
- **p50** moving is a change in the typical transaction. **p95 moving while
  p50 holds** is a change in the tail — often queueing or contention rather
  than the transaction getting faster, and worth saying in those terms.
- **Per-step latencies** are where a real explanation lives. "p95 improved by
  600ms and the whole 600ms is in step 4" is an answer; "p95 improved" is a
  measurement.
- **Failures and connect failures** must be compared before anything else. A
  faster run with more failures is usually faster *because* of them: a
  workflow that failed at step two did not do the work the other one did.

## Say how confident the numbers let you be

State the sample count on both sides — the durations are a rolling window,
not the whole run, so `get_load_test_metrics` reports how many. A difference
smaller than the spread within either side is not a result; say that plainly
rather than reporting a percentage that reads like one.

If the two sides disagree in direction between p50 and p95, or between
throughput and latency, report both and do not pick the flattering one.

## Anti-patterns

- Reporting a percentage improvement with no sample size behind it.
- Comparing runs at different concurrency and calling the difference a change
  in the host.
- Quietly dropping the failure counts because they complicate the story.
- Attributing the improvement to the change. This measures from outside the
  host: it can say the transaction got faster and when, never why. Name what
  moved and hand the cause to someone who can see inside.
- Running the after side while the before side is still finishing.
