# Reading the timings

## Durations is a window, not the run

`Durations` holds the most recent few hundred completed workflows, not every
workflow the run performed. Percentiles computed from it describe that window.

For a short run the two are the same thing. For a long one they are not, and
"p99 across the run" quoted from it is really "p99 across the last few
minutes" — which is a different and usually better-looking number, because a
run's worst latency tends to arrive during ramp-up or during whatever went
wrong in the middle.

Say the sample size with the percentile. `get_load_test_metrics` returns a
`count` for this reason. "p95 was 2.4s over the last 500 workflows" is a claim
that survives someone checking it; "p95 was 2.4s" is not.

## Percentiles, not means

A mean hides exactly what people notice. A workflow that usually takes 400ms
and occasionally takes 12 seconds has a perfectly reasonable mean and a user
population that complains.

- **p50** — the typical experience.
- **p95** — what a regular user hits often enough to remember.
- **p99** — what shows up in complaints.

These are nearest-rank: every figure reported is a duration some workflow
actually took, rather than a point interpolated between two of them.

p99 needs samples to mean anything. From fifty workflows it is the worst one,
which is a single event and not a property of the host. Below a few hundred
samples, report p95 and say the sample was small.

## Failures distort the timings

A workflow that failed at step two finished quickly. Its duration is in the
distribution and it is pulling every percentile down.

Report the failure count next to the timings, always. If failures are more
than a small fraction, say the timings describe the runs that succeeded — and
consider that a run with a high failure rate has measured how fast the host
says no.

## Per-step timing comes from Prometheus only

Workflow duration is in the metrics file. Per-step timing is not: it exists
only as Prometheus histograms, exposed when the run was started with
`-promListen`.

Without it you can say a workflow took four seconds and not which step took
them, and that is usually the whole question. If per-step timing is wanted,
say the run needs restarting with `-promListen`; do not estimate it by
subtraction.

## What these numbers cannot say

Everything here is measured from outside the host. It can say a transaction
took four seconds. It cannot say whether that was CPU, a file wait, a lock, or
a queue — and inferring one of those from a timing alone is how a performance
investigation goes looking in the wrong place. Name the step, give the timing,
and hand it to someone who can see inside the region.
