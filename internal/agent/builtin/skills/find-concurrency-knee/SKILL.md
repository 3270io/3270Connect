---
name: find-concurrency-knee
description: Find the concurrency at which a host stops scaling — where response time starts climbing faster than load — by stepping up in stages and comparing.
invocation: [find-knee, capacity-test, scaling-test]
tools: [run_workflow_once, start_load_test, get_load_test_metrics, stop_load_test]
instructions: [load-test-safety, reading-latency]
---

# Finding the knee

The question behind "how many users can it take?" is usually not where the
host falls over. It is where response time stops being acceptable — the point
at which each additional user costs more than the last. That point is well
below the breaking point, and it is the number capacity planning needs.

## When to use

The user asks how many concurrent users a host supports, where it stops
scaling, or what its capacity is.

## Method

1. Establish a baseline: one virtual user, several minutes. This is the
   host's response time with no contention, and every later figure is read
   against it.
2. Step up in roughly doubling stages — 1, 5, 10, 20, 40 — holding each long
   enough to be past the ramp. Sixty seconds minimum, longer if the workflow
   is slow.
3. After each stage, record from `get_load_test_metrics`: completed per
   second, p50, p95, and the failure count.
4. Stop when p95 exceeds the acceptable figure the user gave, or when
   throughput stops rising while concurrency does. If they gave no figure,
   ask before starting: without one there is no knee, only a curve.

## Reading the result

Throughput rising and latency flat means headroom remains.

Throughput flat while latency climbs means the knee has been passed: the
host is now queueing rather than serving, and each new user waits for the
ones ahead. This is the number to report.

Throughput *falling* while latency climbs means it is past saturation and
into collapse. Do not push further to confirm it, and say what concurrency
it happened at.

Failures appearing at a stage that had none is the same signal by another
route — but check whether they are connect failures, which usually mean a
region limit rather than a performance one.

## Reporting

Give the table of stages, not just the final number. The shape of the curve
is what tells someone whether they are near the edge at their current load,
and a single figure without it invites planning to the breaking point rather
than to the knee.

State the workflow. A knee is a property of this transaction mix on this
host, not of the host alone.

## Anti-patterns

- One long run at high concurrency. It says whether that level works and
  nothing about where the limit is.
- Reading a stage before it has settled. The first seconds are ramp-up.
- Reporting a knee from a run with failures in it.
