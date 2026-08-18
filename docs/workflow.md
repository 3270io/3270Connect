---
seo_title: "3270Connect workflow steps and JSON schema reference"
description: >-
  Every workflow step 3270Connect executes — Connect, FillString,
  PressEnter, CheckValue, AsciiScreenGrab and the rest — with the schema
  that validates them.
---

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
- **Model** (string, optional): The device type to negotiate. `2` (24x80), `3` (32x80), `4` (43x80) or `5` (27x132), or written in full as `3278-4` for a monochrome device and `3279-4` for colour. Defaults to `3279-2`. **A workflow that addresses rows past 24 needs a model that has them** — on a model 2 session the host will never send more than 24 rows, and a step aimed at row 30 is rejected rather than quietly applied to the bottom of the screen. Overridden by `-model`.
- **Oversize** (string, optional): A screen larger than the model defines, written `<cols>x<rows>`, e.g. `132x50`. Only hosts that support the larger geometry will use it. Overridden by `-oversize`.
- **LUName** (string, optional): The logical unit to request at connect time, for hosts that route sessions by LU. Overridden by `-luName`.
- **TLS** (bool, optional): Connect over TLS. Overridden by `-tls`.
- **TLSSkipVerify** (bool, optional): Skip host certificate validation. Requires `TLS`. For an internal host with a private CA or a self-signed certificate; leave it off otherwise. Overridden by `-tlsSkipVerify`.

```json
{
  "Host": "mvs.example.com",
  "Port": 992,
  "CodePage": "cp278",
  "Model": "4",
  "TLS": true,
  "LUName": "LU01",
  "Steps": [ { "Type": "Connect" }, { "Type": "Disconnect" } ]
}
```

### Screen size

The negotiated screen is what the model allows and what the host chooses to
use. A model 4 session can show 43 rows, but starts on the 24-row primary
screen and moves to the alternate size only when the host writes to it. Steps
are checked against the size in use at the time, so a workflow that addresses
row 30 works once the host has switched and is rejected — with the screen size
in the message — before it has.

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
  - `Coordinates` (connect3270.Coordinates) - The row, column and `Length` to read. `Length` is required: a check that reads no characters can only ever fail.
  - `Text` (string) - The expected text value at the coordinates.
- **Usage**: Utilized to verify if the terminal displays expected data at specified locations.

  Leading and trailing spaces are ignored on both sides of the comparison. A
  read that runs past the end of a row continues on the next one, so `Length`
  can span the row boundary and still returns the characters that follow.

### FillString
- **Description**: Fills a string at specified coordinates on the terminal screen.
- **Parameters**: 
  - `Coordinates` (connect3270.Coordinates) - The row and column to fill the string.
  - `Text` (string) - The text to fill at the coordinates.
- **Usage**: This step is used to input text at a specific position on the terminal.
  
  If `Coordinates` is omitted (or `Row`/`Column` are both `0`), the text is typed at the current cursor position.

  `Text` is typed literally. Commas, brackets, quotes and backslashes go to the
  host as written — a value like `SMITH,JOHN` arrives whole. A tab or newline
  in the text still moves to the next field, which is how one step can fill
  several fields in order.

  Two things are refused rather than done, because the emulator does them
  silently and the damage shows up somewhere else:

  - **A value longer than the field.** Typing does not stop at the end of a
    field; the tail runs on into the next one, and on a logon screen the field
    below the user name is usually the password. The step fails naming both
    lengths instead.
  - **A protected position.** The host locks the keyboard with an operator
    error and leaves it locked, so every later step fails too. The step fails
    naming the position instead, and the session stays usable.

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

### PressPA1, PressPA2, PressPA3
- **Description**: Sends a Program Attention key.
- **Usage**: PA1 is the interrupt an operator uses to stop a running transaction and PA2 usually cancels. Unlike a PF key, a PA key carries no screen data back to the host, which is why applications use them for control rather than for input.

### PressClear
- **Description**: Clears the screen and sends the Clear AID.
- **Usage**: The usual way to get from one CICS transaction to a blank screen ready for the next.

### PressReset
- **Description**: Resets an error-locked keyboard (the `X` in a real terminal's status line).
- **Usage**: After a step the host rejected, to make the session usable again rather than failing every step that follows.

### PressHome, PressBackTab, PressNewline
- **Description**: Cursor movement between fields: to the first unprotected field, to the previous field, and to the first field on the next line.
- **Usage**: For screens whose field order is easier to walk than to address by coordinates.

### PressEraseEOF, PressEraseInput
- **Description**: Erases from the cursor to the end of the field, or every unprotected field on the screen.
- **Usage**: `PressEraseEOF` before a `FillString` is how a shorter value is written over a longer one; without it the tail of the old value is left behind.

### PressSysReq, PressAttn
- **Description**: Sends SysReq, which reaches the SSCP rather than the application, or Attn, the TN3270E interrupt.
- **Usage**: For dropping a hung LU session or interrupting an application that has stopped responding.

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
