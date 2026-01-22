# Workflow Steps Documentation

This page provides an overview of the various workflow steps available in the 3270Connect application. Each step represents an individual action taken on the terminal during a workflow execution.

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
- **Usage**: Insert after `Connect` or after navigation steps (e.g., `PressEnter`) when the host is slow to render screens. This is also applied automatically before each step once connected when the top-level `WaitForField` setting is `true` (default).

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
