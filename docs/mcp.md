---
seo_title: "3270Connect MCP server for AI-composed workflows"
description: >-
  Run 3270Connect as a Model Context Protocol server so an AI client can
  compose a workflow, validate it, run it at concurrency and read back
  latency percentiles.
---

# MCP Server

3270Connect can act as a **Model Context Protocol** server, so an AI client
drives performance and volume testing directly: compose a workflow, validate
it, smoke-test it against a host, run it at concurrency, and read back
throughput and latency percentiles.

It is the same binary. There is nothing extra to install.

```bash
3270Connect mcp
```

## Check it works first

Before configuring any client, confirm the binary answers:

```bash
3270Connect mcp --list-tools
```

That prints the tool catalogue as JSON and exits. It needs no host, no
workflow and no run in progress. If you see JSON, the wiring is right and
anything that fails afterwards is configuration.

## Setting up Claude Desktop

1. Open **Claude Desktop** → **Settings** → **Developer** → **Edit Config**.
   That opens `claude_desktop_config.json`.

2. Add 3270Connect to the `mcpServers` section.

    **Windows**

    ```json
    {
      "mcpServers": {
        "3270connect": {
          "command": "C:\\3270Connect\\3270Connect.exe",
          "args": ["mcp"],
          "env": { "MCP_TOOLS": "smoke" }
        }
      }
    }
    ```

    **macOS and Linux**

    ```json
    {
      "mcpServers": {
        "3270connect": {
          "command": "/opt/3270connect/3270Connect",
          "args": ["mcp"],
          "env": { "MCP_TOOLS": "smoke" }
        }
      }
    }
    ```

3. **Restart Claude Desktop.** 3270Connect appears in the tools list.

4. Try it with no mainframe at all:

    > Start sample app 1 on port 3271, write a workflow that connects to it,
    > fills in the name fields and checks the confirmation screen, then run it
    > once and show me the captured screens.

Other MCP clients — VS Code, Claude Code, Cursor, Windsurf — take the same
command and arguments in their own configuration format.

## Tool tiers

The tools are grouped by what they can do, and the tier is set with the
`MCP_TOOLS` environment variable. **The default is `readonly`.**

| `MCP_TOOLS` | Adds | For |
|---|---|---|
| `readonly` *(default)* | `describe_workflow_schema`, `validate_workflow`, `list_workflows`, `list_load_tests`, `get_load_test_metrics`, `get_live_workflow_status`, `get_step_latencies`, `get_run_summary`, `get_console_log`, `test_connection`, `list_skills`, `load_skill`, `list_instructions`, `load_instruction`, `list_extensions` | Writing and checking workflows, and reporting on runs someone else started |
| `smoke` | + `run_workflow_once`, `save_workflow`, `start_sample_app`, `profile_host` | Single-session runs against a real host |
| `load` | + `start_load_test`, `stop_load_test` | Concurrent load generation |

Each tier includes the ones below it.

Two are worth knowing about before you start writing anything:
`list_workflows` reports the workflow documents already on disk — including
any an installed extension contributes — with the step count, the host each
targets and **whether it would pass validation**, so a file that is one field
short of running is visible rather than something to find out at the start of
a load test. `profile_host` connects once and reports what the host actually
supports: screen model, colour, extended attributes and character sets. It
presses no keys and submits nothing, but it does open a session, which is why
it sits in `smoke` rather than `readonly`. It answers the question that
invalidates a workflow silently — whether the screen size the coordinates
assume is the screen size you will get.

The default is deliberately the most restrictive. A load test is the one thing
3270Connect does that a host cannot ignore — every other operation asks a
question, this one takes resources from whoever else is using the system. Opt
in to that rather than out of it.

## Limits

These apply whatever the tier:

| Variable | Default | What it does |
|---|---|---|
| `MCP_MAX_CONCURRENT` | `50` | Largest concurrency a single call may request |
| `MCP_MAX_RUNTIME_SEC` | `300` | Longest runtime a single call may request |
| `MCP_ALLOWED_HOSTS` | unset | Comma-separated glob list of hosts that may be targeted at all. Unset means any host |

A request over a cap is **refused, naming the limit**. It is not quietly
reduced: a run at a tenth of the requested concurrency, reported as the
requested one, is worse than a refusal.

`MCP_ALLOWED_HOSTS` is how you fence off production:

```
MCP_ALLOWED_HOSTS=*.test.example.com,10.20.*,127.0.0.1
```

!!! note "No sign-in, and none needed"
    The MCP server speaks over stdin and stdout to the client that launched the
    process. There is no listener, so there is nothing to authenticate to:
    whoever can start the process is already the operator, and
    [`AUTH_MODE`](authentication.md) does not apply to it. The caps above are
    the control surface here.

    A run started this way therefore has no account behind it, which is what
    the [Load runs page](administration.md#load-runs) means by "not started
    here" — an administrator's to stop.

## Settings file

An AI client launches the server with a command line and no terminal, so
there is nowhere interactive to put a setting. `3270Connect.env`, beside the
binary, is read at startup:

```ini
# 3270Connect.env
MCP_TOOLS=load
MCP_MAX_CONCURRENT=100
MCP_ALLOWED_HOSTS=*.test.example.com
RSA_TOKEN=...
```

Real environment variables take precedence, so a deployment can override the
file without editing it.

Prefer this over the `env` block in `claude_desktop_config.json` for anything
secret. `RSA_TOKEN` in particular ends up in every screenshot and support
paste of that config file.

## What a session looks like

A typical exchange runs schema → validate → smoke → load → interpret:

> **Write me a workflow for the account enquiry on `mvs.test.example.com:992`.**
> The assistant calls `describe_workflow_schema`, composes the JSON, and calls
> `validate_workflow` until it passes.
>
> **Try it once.** `run_workflow_once` opens one session, runs the steps and
> returns the captured screens, so you can see it did what you meant.
>
> **Now run 20 users for two minutes.** `save_workflow` writes the file,
> `start_load_test` spawns the run and returns its pid.
>
> **How is it doing?** `get_load_test_metrics` returns completed, failed and
> the duration percentiles; `get_live_workflow_status` shows where each worker
> currently is.
>
> **What does that mean?** The assistant loads the `interpret-results` skill
> and reports p50, p95 and p99 with the sample size, keeping connect failures
> separate from transaction failures.

## Reading the numbers

Two things the tools state and that are worth knowing yourself:

**Percentiles come from a rolling window.** A run publishes the durations of
its most recent few hundred completed workflows, not all of them. Every
metrics reply includes the sample count for that reason. "p95 was 2.4s over
the last 500 workflows" is a claim someone can check; "p95 was 2.4s" is not.

**Per-step timings come only from Prometheus.** Workflow duration is in the
metrics file; which *step* took the time is not. Start the run with
`prometheus_listen` (the `-promListen` flag) and `get_step_latencies` can read
them. Without it, a workflow that took four seconds cannot be broken down.

See [Metrics & Monitoring](metrics.md) for the collectors themselves.

## Skills

The procedures the assistant follows are files, not prose baked into a prompt.
`list_skills` shows what is available; `load_skill` fetches one. The built-in
set covers ramping a load test, finding a host's concurrency knee, soak
testing, interpreting results, and writing a workflow.

You can add your own, or replace a built-in with one that knows your hosts.
See [Skills and Extensions](skills.md).

## Running a load test safely

The assistant is told to do these, and they are worth knowing so you can tell
when it has not:

- **Confirm the host** before the first concurrent run. A test LPAR and a
  production one differ by a character often enough to be worth a sentence.
- **Validate, then run once, then run many.** A malformed workflow fails on
  every virtual user simultaneously and tells you nothing about the host.
- **Ramp up.** Starting at the target concurrency tells you it broke, at a
  level you have no comparison for.
- **Stop runs when you are done.** A run continues to its runtime deadline
  whether or not anyone is reading it.

Also consider what the workflow *does*. A read-only enquiry is safe to run ten
thousand times; one that creates a record will create ten thousand of them.
