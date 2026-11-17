---
seo_title: "Run your first 3270Connect workflow from the CLI"
description: >-
  Run a workflow end to end: the JSON configuration file, the steps it
  sequences — connect, fill fields, check values, grab screens — and the
  command to run it.
---

# Basic Usage

## Your first successful replay

Generate a valid example (the command refuses to overwrite an existing file):

```bash
3270connect -sampleWorkflow sample-workflow.json
```

Start the bundled sample host in another terminal:

```bash
3270connect -runApp 1 -runApp-port 3270
```

Replay once, without opening an emulator window:

```bash
3270connect -config sample-workflow.json -headless
```

The workflow enters a name, checks the confirmation and saves terminal captures to `sample-output-3270.html`. A completed replay exits with status 0. The separate Docker lab ships its own `workflow-sampleapp.json`; use that file with its `sampleapps` host rather than this local example.

For your own host, export a recorded workflow from [3270Web](https://3270web.3270.io/chaos-mode/) and upload it in the console or pass it with `-config`. Recording runs in 3270Web, a separate application; 3270Connect replays the exported JSON.

## CLI outcomes and load tests

```bash
3270connect -config sample-workflow.json -headless -concurrent 5 -runtime 60
```

Finite load tests exit after cleanup and the summary. Add `-keepDashboard` only when you want an interactive console to remain open. `-plain` selects plain logs and skips interactive shutdown prompts; redirected output does this automatically. `-headless` selects s3270 and does not mute logging.

| Exit status | Meaning |
| --- | --- |
| 0 | Successful replay |
| 1 | Startup failure |
| 2 | Configuration or usage error |
| 3 | Host connection failure |
| 4 | Workflow assertion/action failure |
| 130 | Interrupted or forced shutdown |

Use `3270connect -help` for tasks and `3270connect -help-all` for the full flag reference.

### Injecting a runtime RSA token

Workflows can reference a transient RSA token by placing `{{token}}` in any `Text` field. Supply the token when launching 3270Connect:

```bash
3270connect -config workflow.json -token 123456
```

The placeholder will be substituted immediately before each step runs, ensuring the token is never stored in the workflow file.

## Running Workflows

### Single Workflow

To run a single workflow, create a JSON configuration file that describes the workflow steps. Here's an example configuration file:

```json
{
  "Host": "127.0.0.1",
  "Port": 3270,
  "CodePage": "cp037",
  "EveryStepDelay": { "Min": 0.1, "Max": 0.3 },
  "WaitForField": true,
  "OutputFilePath": "output.html",
  "RampUpBatchSize": 10,
  "RampUpDelay": 1,
  "EndOfTaskDelay": { "Min": 30, "Max": 90 },
  "Steps": [
    {
      "Type": "Connect"
    },
    {
      "Type": "AsciiScreenGrab"
    },
    {
      "Type": "CheckValue",
      "Coordinates": {"Row": 1, "Column": 29, "Length": 24},
      "Text": "3270 Example Application"
    },
    {
      "Type": "FillString",
      "Coordinates": {"Row": 5, "Column": 21},
      "Text": "user1-firstname"
    },
    {
      "Type": "FillString",
      "Coordinates": {"Row": 6, "Column": 21},
      "Text": "user1-lastname"
    },
    {
      "Type": "AsciiScreenGrab"
    },
    {
      "Type": "StepDelay",
      "StepDelay": { "Min": 1.0, "Max": 2.0 }
    },
    {
      "Type": "PressEnter"
    },
    {
      "Type": "CheckValue",
      "Coordinates": {"Row": 1, "Column": 29, "Length": 24},
      "Text": "3270 Example Application"
    },
    {
      "Type": "AsciiScreenGrab"
    },
    {
      "Type": "Disconnect"
    }
  ]
}
```

In this example, an `EveryStepDelay` range keeps the steps paced, the `StepDelay` step adds a longer pause before pressing Enter, and an `EndOfTaskDelay` holds the virtual user after completion to mirror real think-time. The workflow connects to a host, captures the screen, fills both fields, presses Enter, captures the screen again, and then disconnects. By default, `WaitForField` will wait before all steps once connected. 

You can customize the WaitForField behavior using the object format:

```json
  "WaitForField": { "Delay": 2, "Retries": 5 }
```

This example uses a 2-second delay per retry and allows up to 5 retries. If you disable the global setting (`"WaitForField": false`), you can still add an explicit step where you need it:

```json
    { "Type": "WaitForField", "Delay": 2 }
```

Place it after `Connect` or after navigation steps (e.g., `PressEnter`) when the host is slow to render the next screen.

### Concurrent Workflows

You can run multiple workflows concurrently by specifying the `-concurrent` and `-runtime` flags:

- `-concurrent`: Sets the number of concurrent workflows to run (default is 1).
- `-runtime`: Specifies the duration to run workflows in seconds (only used in concurrent mode).
- `-gracePeriod`: Seconds to wait for in-flight workflows to finish after the runtime deadline (default: 30).
- `-autoShutdown`: Seconds for the auto-shutdown countdown prompt when the grace period elapses (default: 10).

For example, to run two workflows concurrently for 60 seconds, use:

```bash
3270connect -config workflow.json -concurrent 2 -runtime 60
```

When `-injectionConfig` is also used, injection entries are locked per active workflow so the same entry is not reused by another active workflow at the same time. If all entries are in use, that workflow start attempt is skipped for the current scheduling cycle and processing continues. A `WARNING` terminal message is emitted for this condition.

`3270Connect` also emits a startup `WARNING` when the number of loaded injection entries is lower than requested concurrency, indicating potential scheduling contention.

## Configuration

### Headless Mode

`-headless` drives the session with `s3270` instead of opening an `x3270`
window. Without it a run needs an X display, so this is the flag for a CI
runner, a container, or any server you reach over SSH.

```bash
3270connect -config workflow.json -headless
```

It does not quiet the terminal: the header, the live stats and the run report
all still print, which is what ends up in a CI log. There is no flag that turns
that output off — `-verbose` and `-verboseFailures` below only add to it.

### Verbose Mode

To enable verbose mode for detailed output, use the `-verbose` flag.

```bash
3270connect -config workflow.json -verbose
```

### Failure-only verbose logging

To log only failing steps (without the volume of full verbose output), use the `-verboseFailures` flag. This is helpful when running many concurrent workflows and you just want to capture which steps failed.

```bash
3270connect -config workflow.json -verboseFailures
```

### Screen capture on failures

When troubleshooting intermittent automation failures in high-concurrency environments, you can enable automatic screen captures using the `-verboseScreenCaptureFailures` flag. This flag works in conjunction with `-verboseFailures` to capture the terminal screen whenever a workflow step fails or a WaitForField timeout occurs.

```bash
3270connect -config workflow.json -verboseFailures -verboseScreenCaptureFailures
```

Key features:
- Captures are saved as plain text files in the current directory
- Files are named using the format `failure_[scriptPort]_step[stepIndex]_[timestamp].txt`
- Limited to 5 total captures across all concurrent workflows to prevent disk exhaustion
- The capture file path is automatically included in the failure log message

Example failure log with screen capture:
```
Workflow failure on scriptPort 5001 at step 4 (CheckValue): CheckValue failed. Expected: LOGIN, Found: ERROR | Screen captured to: failure_5001_step4_1234567890.txt
```

### Screen readiness (WaitForField)

The `WaitForField` configuration controls whether the workflow waits for the terminal to unlock an input field before each step once connected. It applies to all steps in the workflow (not just after the Connect step).

**Configuration formats:**

- **Boolean format** (backward compatible):
  - `"WaitForField": true` - Enabled with defaults (1s delay, 10 retries)
  - `"WaitForField": false` - Disabled globally

- **Object format** (customizable):
  - `"WaitForField": { "Delay": 2, "Retries": 5 }` - Enabled with custom settings
  - `Delay`: Timeout in seconds per retry attempt (default: 1)
  - `Retries`: Maximum number of retry attempts (default: 10)

**Usage examples:**

```json
// Use defaults
"WaitForField": true
"WaitForField": { "Delay": 2, "Retries": 15 }
"WaitForField": false
```

**Per-step override:** You can also add an explicit `WaitForField` step wherever you need an extra wait (e.g., after `PressEnter`). Use the `Delay` parameter in the step to override the timeout for that specific wait.

### Workflow timeout

- `-workflowTimeout`: Hard timeout (seconds) applied to each workflow run. Default 120; set to `0` to disable. When the timeout is hit, the workflow stops without counting as a connect failure.

### Grace period and auto-shutdown

When a concurrent run reaches its `-runtime` deadline, any workflows that are still in progress are given additional time to finish cleanly. Two settings control this behaviour:

- `-gracePeriod <seconds>`: How long to wait for in-flight workflows after the runtime deadline. Default is **30 seconds**. Set a higher value if your workflows are long-running and you want to give them more time to complete naturally.
- `-autoShutdown <seconds>`: If workflows are still running when the grace period expires, 3270Connect prompts with *"Continue waiting? (y/N)"* and begins a countdown. This setting controls the length of that countdown before shutdown is automatically selected. Default is **10 seconds**.

Both values can also be set in the workflow JSON file (see [Workflow Configuration](workflow.md#grace-period-settings)) so they travel with the workflow definition rather than being passed on the command line every time. The CLI flags take precedence over the JSON values.

```bash
# Wait up to 60s for in-flight workflows, with a 20s prompt countdown
3270connect -config workflow.json -concurrent 10 -runtime 120 -gracePeriod 60 -autoShutdown 20
```

### startPort Flag

The -startPort flag allows you to specify the starting port for the sample application. This help to prevent port usage conflicts when running 3270Connect multiple times on the same machine.

Use it as follows:

```bash
3270connect -config workflow.json -startPort 5000
```

### Host Code Page and Character Set

Mainframe sessions exchange data in EBCDIC, and the correct national code page (also called the host character set) must be selected so that accented and language-specific characters render correctly. For example, Finnish/Swedish hosts commonly use code page **cp278**.

You can set the code page in two ways:

- **In the workflow JSON** with the top-level `CodePage` property:

  ```json
  {
    "Host": "mvs.example.com",
    "Port": 992,
    "CodePage": "cp278",
    "Steps": [ { "Type": "Connect" }, { "Type": "Disconnect" } ]
  }
  ```

- **On the command line** with the `-codePage` flag, which overrides the value in the workflow JSON:

  ```bash
  3270connect -config workflow.json -codePage cp278
  ```

The value is passed directly to the embedded x3270/s3270 emulator's `-codepage` option, so any code page name, alias, or number that the emulator recognizes is accepted. Leave `CodePage` unset (and omit `-codePage`) to use the emulator's built-in default code page.

## Screen size, TLS and LU names

The session is a 24x80 colour model 2 unless you ask for something else. Three
settings describe the terminal rather than the run, and each can be set in the
workflow or on the command line:

```bash
# a 43x80 model 4 session, over TLS, bound to a named LU
3270connect -config workflow.json -model 4 -tls -luName LU01
```

```json
{
  "Host": "mvs.example.com",
  "Port": 992,
  "Model": "4",
  "TLS": true,
  "LUName": "LU01",
  "Steps": [ { "Type": "Connect" }, { "Type": "Disconnect" } ]
}
```

- **Model** decides how large the screen can be. A model 4 session still starts
  on the 24-row primary screen and moves to 43 rows when the host writes to the
  alternate size. Steps are checked against the size in use at the time, so a
  step aimed at row 30 is rejected — naming the screen size — rather than
  applied to the bottom row of a 24-row screen.
- **TLS** wraps the connection (the emulator's `L:` host prefix). Certificate
  validation is on; `-tlsSkipVerify` turns it off for an internal host with a
  private CA.
- **LUName** asks the host to bind the session to a named logical unit, which
  hosts that route by LU require.

IPv6 hosts are written as the address alone — `"Host": "2001:db8::5"` — with
no brackets; they are added where the emulator needs them.

Common SBCS code pages (with aliases) supported by the bundled emulator:

| Code page | Aliases | Region / language |
|-----------|---------|-------------------|
| `cp037` | `us`, `us-intl` | US / Canada (English) |
| `cp273` | `german` | Germany / Austria |
| `cp277` | `norwegian` | Denmark / Norway |
| `cp278` | `finnish`, `swedish` | Finland / Sweden |
| `cp280` | `italian` | Italy |
| `cp284` | `spanish` | Spain / Latin America |
| `cp285` | `uk` | United Kingdom |
| `cp297` | `french` | France |
| `cp500` | `belgian` | International / Belgium |
| `cp1140`–`cp1149` | `*-euro` | Euro-symbol variants of the above |

You can supply the canonical name (`cp278`), an alias (`finnish`), or the bare number (`278`). Run `s3270 -v` to print the full list of code pages compiled into the emulator. If an unrecognized code page is supplied, the emulator logs a warning and falls back to its default, so connectivity is not interrupted.

## Examples

Let's explore some common use cases with examples:

### 1. Running a Basic Workflow

Run a basic workflow defined in "workflow.json":

```bash
3270connect -config workflow.json
```

### 2. Running Multiple Workflows Concurrently

Run two workflows concurrently for 60 seconds:

```bash
3270connect -config workflow.json -concurrent 2 -runtime 60
```

### 3. Running in Headless Mode

Run a workflow in headless mode:

```bash
3270connect -config workflow.json -headless
```

### 4. Using the API Mode

Run `3270Connect` in API mode and interact with it using HTTP requests.

- [API Mode](advanced-features.md): Discover how to run 3270Connect as an API server for advanced automation.

### 5. Running a 3270 sample application to help with testing the workflow features

As well as performing workflows on a 3270 running instance, 3270Connect can emulate a 3270 sample application using the [github.com/racingmars/go3270](https://github.com/racingmars/go3270) framework. Full credit go to `racingmars` for this great open source repo.

!!! note

    `github.com/racingmars/go3270` is Copyright (c) 2020 Matthew R. Wilson, under MIT License.

Run a test 3270 sample application to assist with testing 3270Connect workflow features:

??? note "Available Apps"

    - [1] Example 1 application from https://github.com/racingmars/go3270

    - [2] Dynamic RSS Reader

```bash
3270connect -runApp
```
or
```bash
3270connect -runApp [number]
```

Once running and listening on port 3270, run a separate 3270 Connect to run a workflow against the sample 3270 application. The "workflow.json" provided with the root folder of the repo works with the sample application.


## Docker Usage

The published image is `ghcr.io/3270io/3270connect`, rebuilt from `main` by CI.
It is `linux/amd64` only: the `s3270` the emulator drives is embedded in the
binary as an x86-64 ELF, so an arm64 image would serve the console and then
fail the first workflow it was asked to run.

```bash
docker pull ghcr.io/3270io/3270connect:latest
```

!!! warning "The Docker Hub images are not maintained"
    `3270io/3270connect-linux` and `3270io/3270connect-windows` still exist on
    Docker Hub, but nothing has pushed to them since June 2024 — they predate
    the operations console, sign-in, the MCP server and the host profiler.
    Earlier versions of this page named them. Use the GHCR image above.

### Where things live inside the container

The image's working directory is `/data`, it runs as a non-root user, and
`XDG_CONFIG_HOME` points at `/data` so the console keeps its metrics there.
Mount your workflow into `/data` and give paths relative to it:

```bash
docker run --rm -v "$PWD/workflow.json":/data/workflow.json \
  ghcr.io/3270io/3270connect:latest -config workflow.json -headless
```

Mount a **directory** rather than a file when you want the output back —
bind-mounting a file that does not exist yet makes Docker create a directory
with that name:

```bash
mkdir -p out
docker run --rm -v "$PWD/workflow.json":/data/workflow.json -v "$PWD/out":/data/out \
  ghcr.io/3270io/3270connect:latest -config workflow.json -headless
```

with `"OutputFilePath": "out/screens.html"` in the workflow.

### The console

The console is what the image runs when you give it no other flags. It listens
on 9200:

```bash
docker run --rm -p 9200:9200 ghcr.io/3270io/3270connect:latest
```

Keep runs across a container replacement by naming a volume for `/data`:

```bash
docker run --rm -p 9200:9200 -v 3270connect-data:/data ghcr.io/3270io/3270connect:latest
```

### Other modes

Verbose, concurrent, API and sample-app runs are the same flags as everywhere
else — anything after the image name replaces the default command:

```bash
# verbose
docker run --rm -v "$PWD/workflow.json":/data/workflow.json \
  ghcr.io/3270io/3270connect:latest -config workflow.json -headless -verbose

# two workers for sixty seconds
docker run --rm -v "$PWD/workflow.json":/data/workflow.json \
  ghcr.io/3270io/3270connect:latest -config workflow.json -headless -concurrent 2 -runtime 60

# the API listener
docker run --rm -p 8080:8080 ghcr.io/3270io/3270connect:latest -api -api-port 8080

# a bundled sample 3270 application
docker run --rm -p 3270:3270 ghcr.io/3270io/3270connect:latest -runApp 1 -runApp-port 3270
```

Add `-e DASHBOARD_BIND=0.0.0.0` only if you override it; the image already sets
it, because a published port forwards to the container's external interface and
the `localhost` default would refuse every connection from the host.

### Windows

There is no published Windows image. A Windows container image cannot be built
on the Linux runners CI uses, so the build job ships the Windows **binary** as a
release asset instead — see [the releases
page](https://github.com/3270io/3270Connect/releases).

To build one yourself on a Windows host, the repository carries
`Dockerfile.windows`:

```powershell
docker build -f Dockerfile.windows -t 3270connect .
```

## Watch it end to end

### Your first workflow

<figure class="demo-video">
  <video controls preload="metadata" playsinline
         poster="/assets/video/terminal-first-workflow.jpg">
    <source src="/assets/video/terminal-first-workflow.mp4" type="video/mp4">
    <a href="/assets/video/terminal-first-workflow.mp4">Download the video</a>.
  </video>
  <figcaption>
    The workflow file, the run, and the two things it leaves behind — a summary
    under <code>logs/</code> and the screens captured by
    <code>AsciiScreenGrab</code>.
  </figcaption>
</figure>

### Running it at scale

<figure class="demo-video">
  <video controls preload="metadata" playsinline
         poster="/assets/video/terminal-load-test.jpg">
    <source src="/assets/video/terminal-load-test.mp4" type="video/mp4">
    <a href="/assets/video/terminal-load-test.mp4">Download the video</a>.
  </video>
  <figcaption>
    The same workflow with <code>-concurrent 8 -runtime 30</code>: workers ramping
    up in batches, the live stats row, and what happens at the deadline.
  </figcaption>
</figure>

## Conclusion

The `3270Connect` command-line utility offers a flexible way to automate interactions with terminal emulators. Whether you need to connect to hosts, manipulate screens, or run multiple workflows concurrently, `3270Connect` has you covered. Explore its features, experiment with different workflows, and streamline your terminal automation tasks.

That's it! You're now ready to use `3270Connect` for your terminal automation needs, including the API mode for more advanced automation scenarios.

## Put the workflow to work

- [Run regression tests in GitHub Actions](github-actions.md).
- [Plan a concurrent load test](load-testing.md).
