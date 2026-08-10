---
seo_title: "Prometheus metrics for 3270Connect workflow runs"
description: >-
  Prometheus metrics for fleet-scale monitoring of workflow throughput,
  concurrency and outcomes, on an opt-in listener — with a scrape config and
  queries.
---

# Metrics & Monitoring

3270Connect exposes Prometheus metrics for fleet-scale monitoring of
workflow performance, concurrency, and outcome distribution. The metrics
endpoint is opt-in and runs on a dedicated HTTP listener so it does not
collide with the dashboard or API ports.

## Enabling the `/metrics` endpoint

Use the `-promListen` flag with any `host:port` address. Disabled when
empty (the default).

```bash
3270Connect -config workflow.json -concurrent 10 -runtime 300 -promListen :9091
```

Once running, the metrics endpoint is available at:

```
http://localhost:9091/metrics
```

The listener uses a 5-second read-header timeout and runs in its own
goroutine — failures are logged but never abort the workflow runner.

## Collectors

| Metric | Type | Labels | Meaning |
|---|---|---|---|
| `tn3270_connect_seconds` | Histogram | — | Wall-clock time to establish a TN3270 session (exponential buckets starting at 50 ms). |
| `tn3270_step_seconds` | Histogram | `action` | Wall-clock time per workflow step, partitioned by step action (`Connect`, `FillString`, `CheckValue`, `PressEnter`, etc.). |
| `tn3270_workflow_total` | Counter | `result` | Workflows that have terminated, partitioned by outcome: `success`, `failure`, `connect_failed`. |
| `tn3270_concurrent_workers` | Gauge | — | Active workflow worker count. Useful for confirming that `-concurrent` ramps cleanly and never overshoots. |

Source: [`internal/metrics/metrics.go`](https://github.com/3270io/3270Connect/blob/main/internal/metrics/metrics.go).

## Sample scrape config

Add a scrape job to `prometheus.yml`:

```yaml
scrape_configs:
  - job_name: '3270connect'
    scrape_interval: 15s
    static_configs:
      - targets: ['runner-01:9091', 'runner-02:9091']
```

## Useful queries

```promql
# p95 connect time over the last 5 minutes
histogram_quantile(0.95, sum(rate(tn3270_connect_seconds_bucket[5m])) by (le))

# Failure rate by outcome
sum(rate(tn3270_workflow_total[1m])) by (result)

# Slowest step actions (p95)
histogram_quantile(0.95,
  sum(rate(tn3270_step_seconds_bucket[5m])) by (le, action)
)

# Live worker count vs. configured concurrency
tn3270_concurrent_workers
```

## Pairing with the host compatibility profiler

For one-shot host fingerprinting (rather than continuous timing), use
the [Host Compatibility Profiler](host-profiler.md) — the resulting
`CompatibilityProfile` JSON is comparable to the one produced by
3270Web and can be diffed across environments.

## Reading these from an AI client

The [MCP Server](mcp.md) exposes these metrics as tools, so an assistant can
report on a run in conversation. Two things it adds beyond the raw
collectors:

- **Percentiles.** `get_load_test_metrics` computes p50, p95 and p99 from the
  workflow durations, alongside the counters. Those durations are a rolling
  window of the most recent few hundred completed workflows, so every reply
  carries the sample count.
- **Live worker positions.** `get_live_workflow_status` reports which step
  each virtual user is on right now. When throughput drops, workers clustered
  on one step mean the host is slow at a single transaction rather than slow
  in general.

Per-step timings still come only from the histograms above, so a run has to
be started with `-promListen` for `get_step_latencies` to have anything to
read.
