# Basic Usage

## Introduction

The basic usage of `3270Connect` involves running workflows defined in a configuration file. The configuration file specifies a sequence of actions to perform, such as connecting to a host, filling fields, and capturing screens.

To run a workflow, use the following command:

```bash
3270Connect -config workflow.json
```

- `-config`: Specifies the path to the configuration file (default is "workflow.json").
- `-token`: Provides a one-time RSA token that replaces any `{{token}}` placeholder in workflow step text during execution.
- `-codePage`: Sets the host EBCDIC code page / character set for the 3270 session (for example `cp037`, `cp285`, or `cp278`/`finnish`). Overrides the workflow's `CodePage` value when supplied and is passed straight through to the embedded x3270/s3270 emulator's `-codepage` option. See [Host Code Page and Character Set](#host-code-page-and-character-set).
- `-showConnectionErrors`: By default, connection failures for the `Connect` step are informational and do not increment the failed workflow counter. Set this flag to surface connection failures as errors and include them in the failure tally.
- `WaitForField` (config, default `true`): When enabled, the workflow waits for the terminal to unlock an input field before each step after a successful `Connect`. Supports both simple boolean and detailed configuration:
  - Boolean format: `"WaitForField": true` or `"WaitForField": false` (uses defaults: 1s delay, 10 retries)
  - Object format: `"WaitForField": { "Delay": 2, "Retries": 5 }` (custom delay in seconds and retry count)
  - Defaults: `Delay` defaults to 1 second if not specified. `Retries` defaults to 10 if not specified.
  - The WaitForField setting applies to all steps in the workflow once connected (not just after the Connect step).
- `-workflowTimeout`: Hard timeout (seconds) per workflow. A zero value disables the per-workflow timeout.
- `-verboseFailures`: Emit concise failure-only logs (step, script port, error) without enabling full verbose mode-useful for high-concurrency runs where you only want failure diagnostics.
- `-verboseScreenCaptureFailures`: When enabled alongside `-verboseFailures`, automatically captures the terminal screen as plain text whenever a workflow step fails or a WaitForField timeout occurs. Captures are limited to 5 total across all concurrent workflows to prevent disk exhaustion. Files are named using the format `failure_[scriptPort]_step[stepIndex]_[timestamp].txt` and saved in the current directory. The capture file path is included in the failure log message.
- `-bar`: Enable compact progress bars and hide the live INFO rows. (Deprecated alias: `-enableProgressBar`.)
- `-promListen <addr>`: Expose Prometheus metrics on `/metrics` at the given address (e.g. `:9091`). Disabled when empty. See [Metrics & Monitoring](metrics.md) for the collector list and example queries.
- `-profile`: Run as a one-shot host compatibility profiler instead of executing the workflow. Connects, probes the host, writes a `CompatibilityProfile` JSON document, and exits. See [Host Compatibility Profiler](host-profiler.md).
- `-profileHost <host>` / `-profilePort <port>`: Override the profile target. If omitted, the profiler reads `Host`/`Port` from `-config`.
- `-profileTLS`: Mark the profiled host as TLS-protected in the output.
- `-profileOut <path>`: Write the profile JSON to this path instead of stdout.
- `-profileCollectRaw`: Include raw s3270 `Query` responses in the profile output.

### Injecting a runtime RSA token

Workflows can reference a transient RSA token by placing `{{token}}` in any `Text` field. Supply the token when launching 3270Connect:

```bash
3270Connect -config workflow.json -token 123456
```

The placeholder will be substituted immediately before each step runs, ensuring the token is never stored in the workflow file.

## Running Workflows

### Single Workflow

To run a single workflow, create a JSON configuration file that describes the workflow steps. Here's an example configuration file:

```json
{
  "Host": "10.27.27.62",
  "Port": 3270,
  "CodePage": "cp037", // optional host code page / charset; omit to use the emulator default
  "EveryStepDelay": { "Min": 0.1, "Max": 0.3 },
  "WaitForField": true, // optional (default true) to wait before all steps once connected
  "OutputFilePath": "output.html", // optional; if omitted a temp file is used
  "RampUpBatchSize": 10, //optional for concurrency runs
  "RampUpDelay": 1, //optional for concurrency runs
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

For example, to run two workflows concurrently for 60 seconds, use:

```bash
3270Connect -config workflow.json -concurrent 2 -runtime 60
```

When `-injectionConfig` is also used, injection entries are locked per active workflow so the same entry is not reused by another active workflow at the same time. If all entries are in use, that workflow start attempt is skipped for the current scheduling cycle and processing continues. A `WARNING` terminal message is emitted for this condition.

`3270Connect` also emits a startup `WARNING` when the number of loaded injection entries is lower than requested concurrency, indicating potential scheduling contention.

## Configuration

### Headless Mode

You can run `3270Connect` in headless mode using the `-headless` flag. Headless mode is useful for running workflows without a graphical user interface.

```bash
3270Connect -config workflow.json -headless
```

### Verbose Mode

To enable verbose mode for detailed output, use the `-verbose` flag.

```bash
3270Connect -config workflow.json -verbose
```

### Failure-only verbose logging

To log only failing steps (without the volume of full verbose output), use the `-verboseFailures` flag. This is helpful when running many concurrent workflows and you just want to capture which steps failed.

```bash
3270Connect -config workflow.json -verboseFailures
```

### Screen capture on failures

When troubleshooting intermittent automation failures in high-concurrency environments, you can enable automatic screen captures using the `-verboseScreenCaptureFailures` flag. This flag works in conjunction with `-verboseFailures` to capture the terminal screen whenever a workflow step fails or a WaitForField timeout occurs.

```bash
3270Connect -config workflow.json -verboseFailures -verboseScreenCaptureFailures
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

// Custom delay and retries
"WaitForField": { "Delay": 2, "Retries": 15 }

// Disable automatic waiting
"WaitForField": false
```

**Per-step override:** You can also add an explicit `WaitForField` step wherever you need an extra wait (e.g., after `PressEnter`). Use the `Delay` parameter in the step to override the timeout for that specific wait.

### Workflow timeout

- `-workflowTimeout`: Hard timeout (seconds) applied to each workflow run. Default 120; set to `0` to disable. When the timeout is hit, the workflow stops without counting as a connect failure.

### startPort Flag

The -startPort flag allows you to specify the starting port for the sample application. This help to prevent port usage conflicts when running 3270Connect multiple times on the same machine.

Use it as follows:

```bash
3270Connect -config workflow.json -startPort 5000
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
  3270Connect -config workflow.json -codePage cp278
  ```

The value is passed directly to the embedded x3270/s3270 emulator's `-codepage` option, so any code page name, alias, or number that the emulator recognizes is accepted. Leave `CodePage` unset (and omit `-codePage`) to use the emulator's built-in default code page.

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
3270Connect -config workflow.json
```

### 2. Running Multiple Workflows Concurrently

Run two workflows concurrently for 60 seconds:

```bash
3270Connect -config workflow.json -concurrent 2 -runtime 60
```

### 3. Running in Headless Mode

Run a workflow in headless mode:

```bash
3270Connect -config workflow.json -headless
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
3270Connect -runApp
```
or
```bash
3270Connect -runApp [number]
```

Once running and listening on port 3270, run a separate 3270 Connect to run a workflow against the sample 3270 application. The "workflow.json" provided with the root folder of the repo works with the sample application.


## Docker Usage

### Linux

Pull the latest image:

```bash
docker pull 3270io/3270connect-linux:latest
```

Run the container with a configuration file:

```bash
docker run -it -v $(pwd)/workflow.json:/app/workflow.json -v $(pwd)/output.html:/app/output.html -p 3270:3270 3270io/3270connect-linux:latest -config /app/workflow.json
```

Run in headless mode:

```bash
docker run -it -v $(pwd)/workflow.json:/app/workflow.json -v $(pwd)/output.html:/app/output.html -p 3270:3270 3270io/3270connect-linux:latest -config /app/workflow.json -headless
```

Run in verbose mode:

```bash
docker run -it -v $(pwd)/workflow.json:/app/workflow.json -v $(pwd)/output.html:/app/output.html -p 3270:3270 3270io/3270connect-linux:latest -config /app/workflow.json -verbose
```

Run multiple workflows concurrently:

```bash
docker run -it -v $(pwd)/workflow.json:/app/workflow.json -v $(pwd)/output.html:/app/output.html -p 3270:3270 3270io/3270connect-linux:latest -config /app/workflow.json -concurrent 2 -runtime 60
```

Run a test 3270 sample application:

```bash
docker run -it -p 3270:3270 3270io/3270connect-linux:latest -runApp
```

Run a specific test 3270 sample application:

```bash
docker run -it -p 3270:3270 3270io/3270connect-linux:latest -runApp [number]
```

### Windows

Pull the latest image:

```bash
docker pull 3270io/3270connect-windows:latest
```

Run the container with a configuration file:

```bash
docker run -it -v ${PWD}/workflow.json:/app/workflow.json -v ${PWD}/output.html:/app/output.html -p 3270:3270 3270io/3270connect-windows:latest -config /app/workflow.json
```

Run in headless mode:

```bash
docker run -it -v ${PWD}/workflow.json:/app/workflow.json -v ${PWD}/output.html:/app/output.html -p 3270:3270 3270io/3270connect-windows:latest -config /app/workflow.json -headless
```

Run in verbose mode:

```bash
docker run -it -v ${PWD}/workflow.json:/app/workflow.json -v ${PWD}/output.html:/app/output.html -p 3270:3270 3270io/3270connect-windows:latest -config /app/workflow.json -verbose
```

Run multiple workflows concurrently:

```bash
docker run -it -v ${PWD}/workflow.json:/app/workflow.json -v ${PWD}/output.html:/app/output.html -p 3270:3270 3270io/3270connect-windows:latest -config /app/workflow.json -concurrent 2 -runtime 60
```

Run a test 3270 sample application:

```bash
docker run -it -p 3270:3270 3270io/3270connect-windows:latest -runApp
```

Run a specific test 3270 sample application and listening port:

```bash
docker run -it -p 3270:3270 3270io/3270connect-windows:latest -runApp [number] -runApp-port [portNumber]
```

### 3270Connect Basic Usage

![type:video](3270Connect_1_0_3_9.mp4){: style=''}

## Conclusion

The `3270Connect` command-line utility offers a flexible way to automate interactions with terminal emulators. Whether you need to connect to hosts, manipulate screens, or run multiple workflows concurrently, `3270Connect` has you covered. Explore its features, experiment with different workflows, and streamline your terminal automation tasks.

That's it! You're now ready to use `3270Connect` for your terminal automation needs, including the API mode for more advanced automation scenarios.
