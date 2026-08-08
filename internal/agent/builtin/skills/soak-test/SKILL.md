---
name: soak-test
description: Hold a steady, modest load for a long period to surface the problems that only appear with time — leaks, pool exhaustion, log growth, gradual slowdown.
invocation: [soak, endurance-test, stability-test]
tools: [validate_workflow, start_load_test, get_load_test_metrics, get_live_workflow_status, stop_load_test]
instructions: [load-test-safety, reading-latency]
---

# Soak testing

A soak asks a different question from a load test. Not "how much can it
take?" but "does it stay well?". The failures it finds — a connection pool
that never returns entries, a log filling a volume, a region getting steadily
slower — are invisible in a five-minute run at any concurrency.

## When to use

The user asks for a soak, an endurance or stability test, or is chasing
something that only goes wrong after hours.

## Method

1. Pick a load that is comfortably below the knee — around half is usual. A
   soak that saturates the host is a load test that went on too long, and
   what it finds is contention rather than degradation.
2. Run for hours, not minutes. Whatever is being looked for takes time by
   definition; that is why it needs a soak.
3. Sample `get_load_test_metrics` at intervals and **keep the readings**.
   The result of a soak is the trend, and a single reading at the end cannot
   show one.

## What to watch

- **p95 climbing while concurrency is flat.** The clearest degradation
  signal. A host serving the same load more slowly than an hour ago is
  accumulating something.
- **Memory climbing without a plateau** in the sampled usage.
- **Failures appearing after a period of none.** Note when they started —
  the time is often the most useful fact, because it correlates with
  something on a schedule.
- **Workers stuck on one step** in `get_live_workflow_status`, which is
  usually pool or resource exhaustion rather than slowness.

## Reporting

Give the trend: readings over time, not one summary. Say explicitly whether
p95 at the end differs from p95 at the start, because that comparison is the
answer to the question that was asked.

A clean soak is a real result and worth stating plainly: steady throughput,
flat latency, no failures, for however long it ran.

## Anti-patterns

- Soaking at maximum concurrency.
- Reporting only the final numbers. Without the trend a soak has told you
  nothing a short run would not have.
- Stopping at the first failure. Note it, and keep going unless it is
  continuous — an intermittent failure's rate is part of the finding.
