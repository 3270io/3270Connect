# Basic Usage

## Introduction

The basic usage of `3270Connect` involves running workflows defined in a configuration file. The configuration file specifies a sequence of actions to perform, such as connecting to a host, filling fields, and capturing screens. 

To run a workflow, use the following command:

```bash
3270Connect -config workflow.json
```

- `-config`: Specifies the path to the configuration file (default is "workflow.json").

## Running Workflows

### Single Workflow

To run a single workflow, create a JSON configuration file that describes the workflow steps. Here's an example configuration file:

```json
{
  "Host": "10.27.27.62",
  "Port": 3270,
  "HTMLFilePath": "output.html",
  "Steps": [
    {
      "Type": "InitializeHTMLFile"
    },
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

In this example, the workflow connects to a host, captures the screen, fills a field, presses Enter, captures the screen again, and then disconnects.

### Concurrent Workflows

You can run multiple workflows concurrently by specifying the `-concurrent` and `-runtime` flags:

- `-concurrent`: Sets the number of concurrent workflows to run (default is 1).
- `-runtime`: Specifies the duration to run workflows in seconds (only used in concurrent mode).

For example, to run two workflows concurrently for 60 seconds, use:

```bash
3270Connect -config workflow.json -concurrent 2 -runtime 60
```

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
docker run -it -v $(pwd)/workflow.json:/app/workflow.json 3270io/3270connect-linux:latest -config /app/workflow.json
```

Run in headless mode:

```bash
docker run -it -v $(pwd)/workflow.json:/app/workflow.json 3270io/3270connect-linux:latest -config /app/workflow.json -headless
```

Run in verbose mode:

```bash
docker run -it -v $(pwd)/workflow.json:/app/workflow.json 3270io/3270connect-linux:latest -config /app/workflow.json -verbose
```

Run multiple workflows concurrently:

```bash
docker run -it -v $(pwd)/workflow.json:/app/workflow.json 3270io/3270connect-linux:latest -config /app/workflow.json -concurrent 2 -runtime 60
```

Run a test 3270 sample application:

```bash
docker run -it 3270io/3270connect-linux:latest -runApp
```

Run a specific test 3270 sample application:

```bash
docker run -it 3270io/3270connect-linux:latest -runApp [number]
```

### Windows

Pull the latest image:

```bash
docker pull 3270io/3270connect-windows:latest
```

Run the container with a configuration file:

```bash
docker run -it -v ${PWD}/workflow.json:/app/workflow.json 3270io/3270connect-windows:latest -config /app/workflow.json
```

Run in headless mode:

```bash
docker run -it -v ${PWD}/workflow.json:/app/workflow.json 3270io/3270connect-windows:latest -config /app/workflow.json -headless
```

Run in verbose mode:

```bash
docker run -it -v ${PWD}/workflow.json:/app/workflow.json 3270io/3270connect-windows:latest -config /app/workflow.json -verbose
```

Run multiple workflows concurrently:

```bash
docker run -it -v ${PWD}/workflow.json:/app/workflow.json 3270io/3270connect-windows:latest -config /app/workflow.json -concurrent 2 -runtime 60
```

Run a test 3270 sample application:

```bash
docker run -it 3270io/3270connect-windows:latest -runApp
```

Run a specific test 3270 sample application:

```bash
docker run -it 3270io/3270connect-windows:latest -runApp [number]
```

### 3270Connect Basic Usage

![type:video](3270Connect_1_0_3_9.mp4){: style=''}

## Conclusion

The `3270Connect` command-line utility offers a flexible way to automate interactions with terminal emulators. Whether you need to connect to hosts, manipulate screens, or run multiple workflows concurrently, `3270Connect` has you covered. Explore its features, experiment with different workflows, and streamline your terminal automation tasks.

That's it! You're now ready to use `3270Connect` for your terminal automation needs, including the API mode for more advanced automation scenarios.