---
seo_title: "Load test IBM 3270 applications with concurrent workflows"
description: >-
  Plan a 3270 load test with replayable JSON, controlled concurrency and
  per-workflow timeouts. Interpret failures, latency and Prometheus metrics.
---

# Load test IBM 3270 applications with concurrent workflows

3270Connect replays the same terminal workflow across concurrent workers. Use
it to exercise an approved test system and measure successful transactions,
failures and response times. A concurrency setting is a workload choice, not
a promise of unlimited throughput: the host, runner and test data all impose
limits.

## Establish a working baseline

Record a representative flow in
[3270Web](https://3270web.3270.io/workflow/) or write JSON using
[Workflow Actions](workflow.md). Add checks after navigation and at the final
business result. Run one copy first:

```bash
3270Connect -config workflow.json -headless \
  -showConnectionErrors -workflowTimeout 60 -verboseFailures
```

Read the summary in `logs/summary_<pid>.txt`. Confirm a completed workflow and
zero failures, and inspect the final screen. The shell exit status alone is
not a sufficient success assertion in the current CLI; the
[GitHub Actions guide](github-actions.md) shows a saved-summary gate.

## Design the workload

| Choice | What to decide before increasing load |
|---|---|
| Transaction | A real business operation, including assertions and cleanup |
| Test records | Separate accounts/records for active workers to avoid contention |
| Think time | Delays that represent users, rather than only the fastest possible loop |
| Concurrency | Start small, then increase only within the agreed host capacity |
| Runtime | Long enough to distinguish startup effects from sustained behaviour |
| Stop conditions | Failure rate, host impact, response-time budget and runner saturation |

Use [Dynamic Field Injection](injection-config.md) to provide distinct data.
An injection entry is locked while its workflow runs. Fewer entries than
workers can skip starts and generate contention warnings, changing the load
you actually deliver.

## Run a controlled test

For an initial two-worker, sixty-second exercise:

```bash
3270Connect -config workflow.json -headless \
  -concurrent 2 -runtime 60 -workflowTimeout 60 \
  -gracePeriod 30 -autoShutdown 10 \
  -showConnectionErrors -verboseFailures \
  -promListen 127.0.0.1:9091
```

The runtime controls when to stop scheduling; grace and workflow timeouts
control in-flight work. Inspect the saved report after the run deadline. The
operations dashboard may keep the process alive after concurrent work ends;
close it with Ctrl+C once the report has been saved. Do not use an unconditional
process timeout as evidence that all transactions finished.

For larger workloads, configure `RampUpBatchSize` and `RampUpDelay` in the
workflow JSON. For example, these fields add workers in batches of two with a
two-second interval:

```json
{
  "RampUpBatchSize": 2,
  "RampUpDelay": 2
}
```

Merge these fields into your existing workflow, retaining its `Host`, `Port`
and `Steps`. Increase concurrency in separate measured runs, with a reset or
review of test data between them. Use [Basic Usage](basic-usage.md) for the
complete configuration reference.

## Interpret the result

Read the [operations console](dashboard.md) and
[Prometheus metrics](metrics.md) together. Record requested concurrency,
achieved starts/completions, failed workflows, step latency percentiles and
runner CPU/memory. Keep the workflow revision and host configuration alongside
the report so another test is comparable.

A fast failure is not a fast successful transaction. Check correctness and
completion totals before comparing latency. A saturated generator can limit
throughput even while the host has capacity; a data lock can prevent the
intended load reaching the host. Metrics are evidence from this workload, not
a capacity certification for every application on the mainframe.

Bind the example metrics listener to loopback. If a remote collector needs
access, use a deliberately configured private path rather than broadly
publishing a metrics or dashboard port. Self-hosting infrastructure is your
cost; these runs do not require an AI provider.
