---
hide:
  - toc
---

<div class="hero" markdown>
<div class="split" markdown>
<div markdown>

<div class="hero-lockup">
<p class="hero-mark"></p>
<span class="chip accent"><span class="dot live"></span> Open source · v1.9.2</span>
</div>

# Replay the mainframe <span class="grad">at any scale</span>

<p class="lede" markdown>
3270Connect turns a recorded 3270 session into a repeatable workflow: one JSON file, run
headless in CI, fanned out across hundreds of concurrent workers, with every step landing
on a live operations console and a Prometheus endpoint.
</p>

<div class="hero-actions" markdown>
[Install it](installation.md){ .md-button .md-button--primary }
[Basic usage](basic-usage.md){ .md-button }
[Workflow reference](workflow.md){ .md-button }
</div>

</div>
<div markdown>

<div class="term">
  <div class="term-head">
    <span class="dot live"></span>
    <span>session · load run</span>
    <span class="right">25 workers</span>
  </div>
  <pre class="term-body"><span class="sig">$</span> 3270Connect -config workflow.json \
    <span class="cmt">-concurrent 25 -runtime 60 -headless</span>
<span class="sig">›</span> connect  mvs.example.com:992    <span class="tag">[ok]</span>
<span class="sig">›</span> workers  25 spawned               <span class="tag info">[live]</span>
<span class="sig">›</span> steps    FillString · PressEnter  <span class="tag">[ok]</span>
<span class="sig">›</span> metrics  :9090/metrics scraped    <span class="tag info">[up]</span>
<span class="sig">›</span> <span class="caret"></span></pre>
</div>

</div>
</div>

<div class="kpi-strip" markdown>
<div class="kpi"><span class="k">Success rate</span><span class="v">97.1%</span><span class="n">2,381 finished workflows</span></div>
<div class="kpi"><span class="k">p95 step</span><span class="v">0.34s</span><span class="n">measured host-side</span></div>
<div class="kpi"><span class="k">Completed</span><span class="v">2,313</span><span class="n">this session</span></div>
<div class="kpi"><span class="k">Dependencies</span><span class="v">0</span><span class="n">single static binary</span></div>
</div>

</div>

## What it does

<div class="grid cards" markdown>

-   :material-file-code: **Workflows as JSON**

    ---

    Describe a session once — connect, fill, press, assert, grab the screen, disconnect —
    and run it anywhere. No scripting language to learn, no emulator to install.

    [:octicons-arrow-right-24: Workflow actions](workflow.md)

-   :material-speedometer: **Concurrency & load testing**

    ---

    Run the same workflow across hundreds of parallel workers for a fixed duration, with
    per-workflow timeouts, grace periods and a controlled shutdown.

    [:octicons-arrow-right-24: Basic usage](basic-usage.md)

-   :material-view-dashboard: **Live operations console**

    ---

    Watch runs as they happen: latency percentiles, outcomes, per-process controls and
    streaming logs — served straight from the binary, no external services.

    [:octicons-arrow-right-24: Web dashboard](dashboard.md)

-   :material-chart-line: **Prometheus metrics**

    ---

    Scrape `tn3270_connect_seconds`, `tn3270_step_seconds`, workflow outcomes and live
    worker counts from `-promListen` and put mainframe runs on the same board as everything else.

    [:octicons-arrow-right-24: Metrics & monitoring](metrics.md)

-   :material-robot-excited: **AI Chat mode**

    ---

    Drive a live session from 3270Web by typing plain English. The model reads the screen,
    proposes field fills and key presses, and waits for your approval before acting.

    [:octicons-arrow-right-24: AI Chat mode](ai-chat-mode.md)

-   :material-fingerprint: **Host compatibility profiler**

    ---

    Probe a host once with `-profile` and write a `CompatibilityProfile` JSON document that
    diffs cleanly against 3270Web output across environments.

    [:octicons-arrow-right-24: Host profiler](host-profiler.md)

</div>

## The operations console

![The 3270Connect operations console](assets/dashboard/console-overview.webp){: .shot }

<p style="text-align:center; font-size:0.72rem; opacity:0.75;">
The <a href="dashboard/">web dashboard</a> — live workflow metrics, latency percentiles,
per-process controls and log streaming, served straight from the binary.
</p>

## Introduction

3270Connect is a robust automation toolkit that pairs a powerful command-line utility with 3270Web, a browser-based web console for enhancing productivity and efficiency in managing and automating interactions with mainframe 3270 applications. It acts as a bridge between modern computing environments and the traditional mainframe terminals, providing a suite of tools that facilitate automated tasks and workflows in a terminal session.

The utility is used by system administrators, developers, and testers who frequently interact with mainframe systems, which are still pivotal in various industries such as banking, insurance, and government services. With 3270Connect, users can script complex sequences of tasks, automate data entry, perform complex online operations, and capture terminal screens for logging or debugging purposes.

One of the main reasons for using 3270Connect is its ability to save time on repetitive tasks by automating them. This can be especially beneficial in testing scenarios where the same set of operations needs to be performed repeatedly. Moreover, the utility provides a way to integrate mainframe operations with modern CI/CD pipelines, thereby modernizing the development and deployment workflows that involve mainframe systems.

With 3270Connect, users can:

- Define and execute automated workflows through a configuration file, enhancing repeatability and reliability in interactions with terminal screens.
- Capture the state of the 3270 terminal screens at any point during a workflow, which is invaluable for documentation and troubleshooting.
- Execute multiple workflows in parallel, optimizing time and resources, especially in complex test environments.
- Operate in a headless mode, allowing the automation to run in the background or in environments without a graphical interface, such as servers or continuous integration systems.
- Utilize a verbose output mode for an in-depth understanding of workflow execution, which assists in monitoring and debugging.
- Surface failure-only logging with `-verboseFailures` to capture concise diagnostics during high-volume runs without enabling full verbose output.
- Run 3270Connect as an API server, enabling advanced automation scenarios and facilitating load and performance testing of mainframe applications.
- Drive a live 3270 session from 3270Web with AI Chat mode, which reads the screen, proposes actions, and can orchestrate chaos exploration with explicit approval.

Through these features, 3270Connect empowers organizations to integrate their legacy systems into modern automated processes, reducing errors, and increasing efficiency.

## Features

Here are the key features of 3270Connect:

- Running workflows defined in a configuration file.
- Command-line interface for scripting and running automation from the terminal.
- Capturing the 3270 screens as the workflow executes.
- Running workflows concurrently with options for controlling the number of concurrent workflows and runtime duration.
- Web dashboard for live metrics, latency percentiles, log streaming and per-process control — self-contained, with no external dependencies at runtime.
- 3270Web to open AI Chat mode for conversational session control.
- Headless mode for running workflows without a graphical user interface.
- Verbose mode for detailed output, plus failure-only logging with `-verboseFailures` for high-concurrency test runs.
- API mode for advanced automation.
- AI Chat mode in 3270Web for screen reading, field entry, key presses, and chaos exploration with per-action approval or Auto Mode.
- Prometheus metrics endpoint (`-promListen`) exposing connect/step timing, workflow outcomes, and live concurrency for fleet-scale monitoring.
- One-shot host compatibility profiler (`-profile`) that produces a `CompatibilityProfile` JSON document shareable with 3270Web for cross-environment comparison.
- Running a 3270 sample application to assist with testing workflow features.

## Getting Started

If you're new to 3270Connect, you can start by exploring the following sections:

- [Installation](installation.md): Learn how to install 3270Connect on your system.
- [Basic Usage](basic-usage.md): Get started with basic usage, running workflows and sample 3270 application(s) to aid testing.
- [Web Dashboard](dashboard.md): Watch runs live, launch them from the browser, and stream logs from the operations console.
- [Workflow Steps](workflow.md): Overview of the various workflow steps available in the 3270Connect application

## Advanced Features

Once you've mastered the basics, you can dive into more advanced features:

- [API Mode](advanced-features.md): Discover how to run 3270Connect as an API server for advanced automation and load performance testing.
- [AI Chat Mode](ai-chat-mode.md): Use 3270Web to drive a live 3270 session through conversation, approve tool calls, and run chaos exploration.
- [Chaos Mode](chaos-mode.md): Learn how toolbar controls and AI Chat share the same exploration state and export workflows.
- [Metrics & Monitoring](metrics.md): Scrape `tn3270_connect_seconds`, `tn3270_step_seconds`, workflow outcomes, and live worker counts from `-promListen`.
- [Host Compatibility Profiler](host-profiler.md): Probe a host once with `-profile` and write a `CompatibilityProfile` JSON document that compares cleanly against 3270Web output.
- [Compatibility Profile Schema](compatibility-profile-schema.md): Field-by-field reference for the shared `CompatibilityProfile` document (v1.0.0).

## Video example

### 3270Connect Basic Usage

![type:video](3270Connect_1_0_3_9.mp4){: style=''}

### 3270Connect API Usage

![type:video](3270Connect_API_1_0_4_0.mp4){: style=''}

## Conclusion

The 3270Connect command-line utility is a powerful tool for automating terminal emulator interactions. This documentation is here to help you make the most of it. If you have any questions or need assistance, feel free to reach out to the community or refer to the [GitHub repository](https://github.com/3270io/3270Connect) for more details.

Let's get started with 3270Connect!
