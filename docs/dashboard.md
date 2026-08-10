---
seo_title: "The 3270Connect web dashboard and operations console"
description: >-
  The browser-based operations console: live workflow counts, durations,
  latency percentiles, host CPU and memory, log tailing and process control.
---

# Web Dashboard

The dashboard is 3270Connect's browser-based operations console. It shows what
your runs are doing right now — live workflow counts, durations, latency
percentiles, host CPU and memory — and lets you launch runs, inspect
configuration, tail logs and terminate processes without leaving the page.

![The 3270Connect operations console](assets/dashboard/console-overview.webp){: .shot }

!!! info "Runs entirely offline"
    Every asset the console needs — styles, scripts, charting library and icons —
    is embedded in the 3270Connect binary and served from `localhost`. Nothing is
    fetched from a CDN, so the dashboard behaves identically on an air-gapped
    build server and on a laptop. Web fonts are the single optional extra; when
    they cannot be reached the console falls back to your local monospace stack.

## Starting the dashboard

=== "Dashboard only"

    ```bash
    3270Connect -dashboard
    ```

    Then open <http://localhost:9200/dashboard>.

=== "Custom port"

    ```bash
    3270Connect -dashboard -dashboardPort 8500
    ```

=== "Alongside a load test"

    ```bash
    3270Connect -config workflow.json -concurrent 25 -runtime 600
    ```

    The dashboard starts automatically whenever `-concurrent` is greater than 1
    or `-runtime` is set, so a load test is observable without extra flags.

Launching 3270Connect with **no command-line arguments at all** — for example by
double-clicking the executable — also forces dashboard mode.

| Flag | Default | Purpose |
|---|---|---|
| `-dashboard` | off | Start the dashboard server. |
| `-dashboardPort` | `9200` | Port for the dashboard HTTP listener. |

!!! note "Bound to localhost"
    The listener binds to `localhost` only, so the console is never exposed on
    the network. To view it from another machine, forward the port over SSH:
    `ssh -L 9200:localhost:9200 user@runner-01`.

## Layout at a glance

The console is a single page in three bands: a **command bar** and **status
bar** pinned to the top, a **metrics area** (KPI tiles and charts), and the
**process table**.

### Command bar

Holds the primary actions — *Start Process*, *Start App*, *Console Logs*,
*Refresh* and the shortcut sheet — alongside a live readout of running
processes, in-flight workflows and console uptime. The search field opens the
[command palette](#command-palette).

### Status bar

Controls how the page stays current, and how it looks.

- **Live** switch — pauses and resumes polling. The ring to its left counts down
  to the next refresh, and pulses when a request is in flight.
- **Interval** — 1 s, 5 s, 10 s, 30 s or 60 s.
- **Theme**, **density** and **effects** toggles — see [Appearance](#appearance).

Polling pauses automatically while any dialog is open, so nothing shifts under
the cursor while you are reading a log or filling in a form, and resumes when
the dialog closes.

## Key metrics

![KPI tiles: active, started, completed, failed, success rate and throughput](assets/dashboard/kpi-strip.webp){: .shot }

Six tiles summarise the whole fleet. The counters carry a sparkline built from
the samples this browser session has collected, so you can see the shape of the
last few minutes rather than just the current number.

| Tile | Meaning |
|---|---|
| **Active Workflows** | Workflows in flight across every process. |
| **Total Started** | Workflows launched since the processes started. |
| **Completed** | Workflows that finished without error. |
| **Failed** | Workflows that terminated with an error. The tile turns red as soon as this is non-zero. |
| **Success Rate** | `completed / (completed + failed)`, drawn as a gauge — green at 99 % and above, amber from 90 %, red below. |
| **Throughput** | Workflows completed per minute, derived from the change between polls. |

The chip under each counter shows the change since the previous poll, so a
stalled run is obvious at a glance: started keeps climbing while completed does
not.

## Charts

![Duration, outcome, resource and latency charts](assets/dashboard/charts.webp){: .shot }

**Workflow Duration** plots one series per process, newest run on the right.
Use the legend chips below the chart to isolate a single PID.

**Outcomes** breaks finished work into completed, failed and in-flight, with the
success percentage in the centre.

**System Resources** shows CPU averaged across all 3270Connect processes and
host memory utilisation over time.

**Latency Profile** summarises every recorded duration: p50, p90, p99 and max as
bars, a distribution histogram beneath them, and sample count, mean, min and
standard deviation below that. This is the panel to check when average duration
looks acceptable but users are reporting slow sessions — a healthy mean with a
p99 several times larger points at a tail problem rather than a general one.

### Chart controls

- **Window presets** (`30` / `60` / `120` / `ALL`) limit how many data points the
  duration and resource charts show.
- **Scroll to zoom**, **drag to pan**, and the reset button returns to the full
  range.
- **Export** each chart as a **PNG** image or its underlying series as **CSV**.

## Reading the same data from an AI client

The process table and the KPI tiles read the per-process metrics files every
running 3270Connect writes. The [MCP Server](mcp.md) reads the same files, so
an assistant can list runs, report percentiles and stop a run in conversation
— `list_load_tests`, `get_load_test_metrics`, `stop_load_test`. Starting a run
that way is the same primitive the **Start process** button uses.

## Process table

![The process intelligence table](assets/dashboard/process-table.webp){: .shot }

One row per 3270Connect process, refreshed on every poll.

| Column | Notes |
|---|---|
| **Actions** | Per-process operations — see below. |
| **PID** | Operating-system process ID. |
| **Status** | `Running`, `Ended` or `Killed`, with a pulsing dot while the process is alive. |
| **Progress** | Elapsed against `-runtime`, with the time remaining. Runs without a runtime limit show `unbounded`. |
| **Active / Started / Completed / Failed** | Per-process workflow counters. |
| **Success** | Completed-versus-failed ratio as a split bar; hover for exact figures. |
| **Avg Duration** | Mean workflow duration with a sparkline of the last 24 runs. |
| **Parameters** | The command line the process was started with, with a copy button. |

Every column marked sortable can be clicked to sort ascending or descending.
The toolbar above adds free-text filtering across PID, status and parameters,
status filters (**All**, **Running**, **Ended**, **With failures**), a
table/card layout switch, and **Export** to CSV of whatever is currently
visible.

!!! tip
    Press ++slash++ anywhere on the page to jump straight to the filter field.

### Row actions

| Action | Available when | Opens |
|---|---|---|
| :material-file-code: **Workflow JSON** | The process was started with a config file | The workflow definition, syntax highlighted with line numbers, with copy and download |
| :material-monitor: **Output preview** | The workflow declares `OutputFilePath` | The generated HTML output in a live-refreshing frame |
| :material-file-document: **Performance summary** | The run has ended | The `summary_<pid>.txt` report |
| :material-console: **Logs** | Always | The console stream, pre-filtered to that PID |
| :material-skull: **Terminate** | Always | A confirmation dialog, then sends a kill signal |

The dashboard refuses to terminate its own process, so you cannot accidentally
shut down the console you are using.

## Launching a run

![The start-process dialog with a parsed workflow](assets/dashboard/start-process.webp){: .shot }

**Start Process** opens a two-column dialog that mirrors the command-line
options.

1. **Drop a workflow JSON** onto the upload zone, or click to browse. The file
   is parsed in the browser before it is uploaded, so host, port, code page,
   output file, step count and ramp-up settings are shown immediately — and a
   malformed file is reported before anything reaches the server. The full
   document is available under *Raw configuration*.
2. **Override** host, port, output file path, ramp-up batch size or ramp-up
   delay for this run only. The uploaded file is not modified. **Test
   connection** dials the host and port and reports the result inline, which is
   worth doing before committing to a long run.
3. **Set the load profile** — concurrent workflows, runtime in seconds, starting
   port, and whether to run headless.
4. Optionally attach an [injection config](injection-config.md) and an
   [RSA token](basic-usage.md#injecting-a-runtime-rsa-token) that replaces
   `{{token}}` placeholders immediately before execution.

**Start App** launches one of the bundled
[sample 3270 applications](basic-usage.md#5-running-a-3270-sample-application-to-help-with-testing-the-workflow-features)
on a port of your choosing, which is the quickest way to exercise a workflow
without a real host.

## Console logs

![The console log stream with filters](assets/dashboard/console-logs.webp){: .shot }

The log viewer aggregates `logs/logs_<pid>.json` across every process.

- Filter by **PID**, by **severity** (errors, warnings, success) and by **free
  text**, with matches highlighted in place.
- **Follow tail** keeps the newest line in view; turn it off to scroll back
  without being yanked forward.
- **Wrap** toggles soft wrapping for long lines.
- **Auto refresh** polls the log endpoint on its own interval, independent of
  the page refresh.
- **Copy** the whole stream to the clipboard or **download** it as a `.log`
  file, and **maximise** the dialog for dense output.

Severity is inferred from the message text, so lines mentioning failure,
timeouts or refusals are marked as errors even though the underlying log format
has no explicit level field.

## Inspecting a workflow

![The workflow JSON viewer](assets/dashboard/workflow-viewer.webp){: .shot }

The workflow viewer reads the configuration file recorded for a process, pretty
prints it, and highlights keys, strings, numbers and literals with line numbers
down the side. It is the fastest way to confirm which revision of a workflow a
given PID is actually running.

## Command palette

![The command palette](assets/dashboard/command-palette.webp){: .shot }

Press ++ctrl+k++ (++cmd+k++ on macOS) to open the palette. It searches every
action, view setting and per-process operation — including "PID *n* · logs" and
"PID *n* · terminate" for each running process — and matches on subsequences, so
`exp` finds *Export process table*. Navigate with the arrow keys, run with
++enter++, dismiss with ++esc++.

## Keyboard shortcuts

![The keyboard shortcut sheet](assets/dashboard/shortcuts.webp){: .shot }

| Key | Action |
|---|---|
| ++ctrl+k++ | Command palette |
| ++slash++ | Focus the process filter |
| ++r++ | Refresh now |
| ++c++ | Console logs |
| ++s++ | Start process |
| ++a++ | Start sample app |
| ++p++ | Pause / resume polling |
| ++d++ | Toggle compact density |
| ++t++ | Cycle theme |
| ++question++ | Show this sheet |
| ++esc++ | Close the active dialog |

Single-key shortcuts are ignored while you are typing in a field or while a
dialog is open.

## Appearance

Four themes ship with the console. Every colour is a CSS custom property, so
charts, gauges and sparklines all re-skin together when you switch.

<div class="shot-grid" markdown>

![Phosphor green](assets/dashboard/theme-phosphor.webp){: .shot }
**Phosphor** — the default terminal palette.

![Amber CRT](assets/dashboard/theme-amber.webp){: .shot }
**Amber** — warm vintage CRT.

![Ice blue](assets/dashboard/theme-ice.webp){: .shot }
**Ice** — cool, high contrast.

![Daylight](assets/dashboard/theme-daylight.webp){: .shot }
**Daylight** — light mode for bright rooms and projectors.

</div>

Two further controls sit beside the theme picker:

- **Density** switches between comfortable and compact rows — compact tightens
  padding and type scale to fit noticeably more processes on screen, which helps
  on large fleets.
- **Effects** turns off the CRT scanlines and the drifting grid backdrop for a
  flatter, quieter surface. The console also honours your operating system's
  *reduce motion* setting automatically.

![Compact density](assets/dashboard/density-compact.webp){: .shot }

Your theme, density, effects, layout, refresh interval and status filter are
remembered in the browser's local storage, so the console comes back the way you
left it.

## Small screens

Below roughly 780 px the process table becomes a card list and the panels stack
into a single column, so the console stays usable from a phone or a narrow
split-screen window. You can also force the card layout at any width from the
layout switch in the panel header.

<div class="shot-grid" markdown>

![Metrics on a narrow viewport](assets/dashboard/responsive-cards.webp){: .shot }

![Process cards on a narrow viewport](assets/dashboard/responsive-processes.webp){: .shot }

</div>

## Where the data comes from

The console is a thin layer over files that 3270Connect already writes.

| Data | Location |
|---|---|
| Per-process metrics | `<user config dir>/3270Connect/dashboard/metrics_<pid>.json` |
| Console logs | `logs/logs_<pid>.json`, relative to the working directory |
| Performance summaries | `logs/summary_<pid>.txt` |
| Workflow definitions and HTML output | Wherever the workflow's `configFilePath` and `OutputFilePath` point |

The user config directory is `~/.config` on Linux, `~/Library/Application
Support` on macOS and `%AppData%` on Windows. Each process rewrites its own
metrics file every two seconds; stale entries for processes that are long gone
are cleaned up automatically.

### HTTP endpoints

The same endpoints the page uses are available to any HTTP client, which makes
them convenient for scripting or for a health check.

| Endpoint | Method | Returns |
|---|---|---|
| `/dashboard` | `GET` | The console page |
| `/dashboard/data` | `GET` | Metrics as JSON for every running process — or the last snapshot when none are running |
| `/dashboard/workflow?pid=<pid>` | `GET` | The workflow JSON for a process |
| `/dashboard/output?pid=<pid>` | `GET` | The generated HTML output |
| `/dashboard/summary?pid=<pid>` | `GET` | The plain-text performance summary |
| `/console` / `/console?pid=<pid>` | `GET` | Log entries as JSON |
| `/terminal-console?pid=<pid>` | `GET` | The same log lines as plain text |
| `/start-process` | `POST` | Starts a run from a multipart form |
| `/kill?pid=<pid>` | `POST` | Terminates a process |
| `/test-connection` | `POST` | Dials `{"host": …, "port": …}` and reports reachability |

```bash
# Current totals across every live process
curl -s http://localhost:9200/dashboard/data | jq '.aggregated'

# Tail a specific process from the terminal
curl -s "http://localhost:9200/terminal-console?pid=12345"
```

## Troubleshooting

??? question "The table is empty"
    The console lists processes that have written a metrics file, and each
    process writes its file every two seconds — so a run shorter than that may
    never appear. Note also that starting the dashboard clears metrics from
    previous runs, so launch it before, or at the same time as, the work you
    want to watch.

??? question "A red banner says contact was lost"
    The page failed to reach `/dashboard/data` twice in a row. It keeps showing
    the last known snapshot and retries on every interval; the banner clears by
    itself once the server responds. If it persists, the 3270Connect process
    hosting the dashboard has exited.

??? question "Counters look frozen"
    Check the **Live** switch in the status bar — polling is paused while any
    dialog is open, and stays off if you switched it off previously, since the
    setting is remembered between visits.

??? question "Charts are missing and everything else works"
    This means the vendored charting script did not load. Reload with a cache
    bypass (++ctrl+shift+r++); the tiles, latency percentiles and table remain
    fully functional in the meantime.

## Related pages

- [Basic Usage](basic-usage.md) — the flags the dashboard's start dialog mirrors
- [Dynamic Field Injection](injection-config.md) — the optional injection config
- [Metrics & Monitoring](metrics.md) — Prometheus metrics for fleet-scale monitoring
- [API Mode](advanced-features.md) — driving 3270Connect over HTTP instead
