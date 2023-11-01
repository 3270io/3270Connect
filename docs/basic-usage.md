# Basic Usage

## Introduction

The basic usage of `go3270` involves running workflows defined in a configuration file. The configuration file specifies a sequence of actions to perform, such as connecting to a host, filling fields, and capturing screens. 

To run a workflow, use the following command:

```bash
go3270 -config workflow.json
```

- `-config`: Specifies the path to the configuration file (default is "workflow.json").

## Running Workflows

### Single Workflow

To run a single workflow, create a JSON configuration file that describes the workflow steps. Here's an example configuration file:

```json
{
  "Host": "10.27.27.62",
  "Port": 30050,
  "HTMLFilePath": "output.html",
  "Steps": [
    {
      "Type": "Connect"
    },
    {
      "Type": "AsciiScreenGrab"
    },
    {
      "Type": "FillString",
      "Coordinates": {"Row": 10, "Column": 44},
      "Text": "b0001"
    },
    {
      "Type": "PressEnter"
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

In this example, the workflow connects to a host, captures the screen, fills a field, presses Enter, captures the screen again, and then disconnects.

### Concurrent Workflows

You can run multiple workflows concurrently by specifying the `-concurrent` and `-runtime` flags:

- `-concurrent`: Sets the number of concurrent workflows to run (default is 1).
- `-runtime`: Specifies the duration to run workflows in seconds (only used in concurrent mode).

For example, to run two workflows concurrently for 60 seconds, use:

```bash
go3270 -config workflow.json -concurrent 2 -runtime 60
```

## Configuration

### Headless Mode

You can run `go3270` in headless mode using the `-headless` flag. Headless mode is useful for running workflows without a graphical user interface.

```bash
go3270 -config workflow.json -headless
```

### Verbose Mode

To enable verbose mode for detailed output, use the `-verbose` flag.

```bash
go3270 -config workflow.json -verbose
```

## Examples

Let's explore some common use cases with examples:

### 1. Running a Basic Workflow

Run a basic workflow defined in "workflow.json":

```bash
go3270 -config workflow.json
```

### 2. Running Multiple Workflows Concurrently

Run two workflows concurrently for 60 seconds:

```bash
go3270 -config workflow.json -concurrent 2 -runtime 60
```

### 3. Running in Headless Mode

Run a workflow in headless mode:

```bash
go3270 -config workflow.json -headless
```

### 4. Using the API Mode

Run `go3270` in API mode and interact with it using HTTP requests.

## Conclusion

The `go3270` command-line utility offers a flexible way to automate interactions with terminal emulators. Whether you need to connect to hosts, manipulate screens, or run multiple workflows concurrently, `go3270` has you covered. Explore its features, experiment with different workflows, and streamline your terminal automation tasks.

That's it! You're now ready to use `go3270` for your terminal automation needs, including the API mode for more advanced automation scenarios.