# Workflow Steps Documentation

This page provides an overview of the various workflow steps available in the 3270Connect application. Each step represents an individual action taken on the terminal during a workflow execution.

## Available Workflow Steps

### InitializeOutput
- **Description**: Initializes the output file with run details.
- **Parameters**: `outputFilePath` (string) - Path to the output file.
- **Usage**: This step is used to set up the output file before executing other steps.

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

### AsciiScreenGrab
- **Description**: Captures and appends the ASCII representation of the current screen to the output file.
- **Parameters**: `outputFilePath` (string) - Path to the output file.
- **Usage**: To capture the current state of the terminal screen as ASCII text.

### PressEnter
- **Description**: Simulates pressing the Enter key.
- **Usage**: Commonly used to submit data or commands entered on the terminal.

### Disconnect
- **Description**: Disconnects from the terminal.
- **Usage**: This step is used to end the terminal session cleanly.

## Example Workflow

Here is an example of how these steps might be sequenced in a typical workflow:

1. **InitializeOutput**: Set up the output file.
2. **Connect**: Connect to the terminal.
3. **FillString**: Input a username at the specified coordinates.
4. **PressEnter**: Submit the username.
5. **FillString**: Input a password at the specified coordinates.
6. **PressEnter**: Submit the password.
7. **CheckValue**: Verify successful login by checking for a welcome message.
8. **AsciiScreenGrab**: Capture the screen after login.
9. **Disconnect**: Disconnect from the terminal.

Each step plays a crucial role in the automated interaction with the terminal. By combining these steps, complex workflows can be executed seamlessly.
