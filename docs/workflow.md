# Workflow Steps Documentation

This page provides an overview of the various workflow steps available in the 3270Connect application. Each step represents an individual action taken on the terminal during a workflow execution.

This page and the validator are the authority on the format. An AI client can
read the same thing programmatically through the [MCP Server](mcp.md)'s
`describe_workflow_schema` tool, and check a document with `validate_workflow`
before running it.

## Connection Settings

These top-level properties configure the terminal connection for the whole workflow:

- **Host** (string): Hostname or IP address of the TN3270 host.
- **Port** (int): TCP port of the TN3270 host.
- **CodePage** (string, optional): Host EBCDIC code page / character set for the session, for example `cp037`, `cp285`, or `cp278`/`finnish`. When set, it is passed to the embedded x3270/s3270 emulator via its `-codepage` option so that national and language-specific characters render correctly. The `-codePage` CLI flag overrides this value. Leave it unset to use the emulator default. See [Host Code Page and Character Set](basic-usage.md#host-code-page-and-character-set) for the list of supported code pages.

```json
{
  "Host": "mvs.example.com",
  "Port": 992,
  "CodePage": "cp278",
  "Steps": [ { "Type": "Connect" }, { "Type": "Disconnect" } ]
}
```

## Grace Period Settings

These top-level properties control what happens when a concurrent run reaches its runtime deadline and workflows are still in progress:

- **GracePeriod** (number, optional): How long in seconds to wait for in-flight workflows to finish after the `-runtime` deadline expires. Defaults to **30** when not set. Overridden by the `-gracePeriod` CLI flag.
- **AutoShutdownTimeout** (number, optional): Length in seconds of the auto-shutdown countdown prompt shown when the grace period elapses. If no input is received before the countdown reaches zero, shutdown is selected automatically. Defaults to **10** when not set. Overridden by the `-autoShutdown` CLI flag.

```json
{
  "Host": "mvs.example.com",
  "Port": 3270,
  "GracePeriod": 60,
  "AutoShutdownTimeout": 20,
  "Steps": [ { "Type": "Connect" }, { "Type": "Disconnect" } ]
}
```

Priority order: **CLI flag > workflow JSON field > built-in default**.

## Delay Behavior

You can control pacing with flexible delay ranges:

- **EveryStepDelay** (workflow-level): Adds a randomized pause between every step using `Min`/`Max` values (sub-second friendly) to mimic keystrokes and host reaction time.
- **StepDelay** (step-level): Insert this step when you need a targeted hesitation using a `StepDelay` object with `Min`/`Max` values (typically seconds).
- **EndOfTaskDelay** (workflow-level): Adds a randomized pause after the final step to model user think-time between repeats (minutes-scale ranges are common).

Legacy `Delay` and `HumanDelay` settings are no longer used.

## Available Workflow Steps

### InitializeOutput
- **Description**: (Optional) Re-initializes the workflow output file.
- **Parameters**: None.
- **Usage**: In most cases you do not need this step: the output file is initialized automatically at the start of each workflow run. The output destination comes from the top-level `OutputFilePath` setting (CLI mode) or an internal temporary file (API mode).

### Connect
- **Description**: Establishes a connection to the terminal.
- **Usage**: This step is essential to start the interaction with the terminal.

### CheckValue
- **Description**: Checks a value at specified coordinates on the terminal screen.
- **Parameters**: 
  - `Coordinates` (connect3270.Coordinates) - The row and column to check the value.
  - `Text` (string) - The expected text value at the coordinates.
- **Usage**: Utilized to verify if the terminal displays expected data at specified locations.

### FillString
- **Description**: Fills a string at specified coordinates on the terminal screen.
- **Parameters**: 
  - `Coordinates` (connect3270.Coordinates) - The row and column to fill the string.
  - `Text` (string) - The text to fill at the coordinates.
- **Usage**: This step is used to input text at a specific position on the terminal.
  
  If `Coordinates` is omitted (or `Row`/`Column` are both `0`), the text is typed at the current cursor position.

### AsciiScreenGrab
- **Description**: Captures and appends the ASCII representation of the current screen to the output file.
- **Parameters**: None.
- **Usage**: To capture the current state of the terminal screen as ASCII text.

### WaitForField
- **Description**: Waits for the terminal to unlock an input field (keyboard ready) before proceeding.
- **Parameters**: Optional `Delay` (float, seconds) to override the default 1 second timeout used per retry.
- **Usage**: Insert after `Connect` or after navigation steps (e.g., `PressEnter`) when the host is slow to render screens. This is also applied automatically before each step once connected when the top-level `WaitForField` setting is enabled (default). The global `WaitForField` setting now supports both simple boolean and detailed configuration formats:
  - Simple boolean: `"WaitForField": true` (uses default 1s delay, 10 retries)
  - Detailed configuration: `"WaitForField": { "Delay": 2, "Retries": 5 }` (custom delay and retry count)
  - Defaults: If `Delay` is not specified, it defaults to 1 second. If `Retries` is not specified, it defaults to 10.

### StepDelay
- **Description**: Inserts a randomized pause to mimic human timing between automated interactions.
- **Parameters**: `StepDelay.Min` and `StepDelay.Max` (float, seconds) - Bounds for the pause duration.
- **Usage**: Add just before actions that benefit from a brief hesitation (for example, immediately before `PressEnter`).

### PressEnter
- **Description**: Simulates pressing the Enter key.
- **Usage**: Commonly used to submit data or commands entered on the terminal.

### PressTab
- **Description**: Simulates pressing the Tab key.
- **Usage**: Useful for moving focus/cursor between fields on some host screens.

### PressPF1 ... PressPF24
- **Description**: Simulates pressing a Program Function key (PF1 through PF24).
- **Usage**: Use the PF key that matches your host application navigation.

### Disconnect
- **Description**: Disconnects from the terminal.
- **Usage**: This step is used to end the terminal session cleanly.

## Example Workflow

Here is an example of how these steps might be sequenced in a typical workflow:

1. **Connect**: Connect to the terminal.
2. **AsciiScreenGrab**: Capture the initial screen.
3. **FillString**: Populate fields.
4. **StepDelay**: Add a targeted pause (optional).
5. **PressEnter**: Submit.
6. **AsciiScreenGrab**: Capture the post-submit screen.
7. **Disconnect**: Disconnect from the terminal.

Each step plays a crucial role in the automated interaction with the terminal. By combining these steps, complex workflows can be executed seamlessly.
