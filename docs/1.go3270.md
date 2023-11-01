
# go3270 Command-Line Utility

The `go3270` command-line utility allows you to automate interactions with a 3270 terminal emulator session. It provides a set of commands and features for scripting and automating mainframe tasks. Below are various usage examples and modes for `go3270`.

## Installation

Before using `go3270`, make sure you have it installed. You can download and install it using the following command:

```bash
go get -u gitlab.jnnn.gs/jnnngs/go3270
```

## Usage Examples

### Basic Commands

### Workflows

Define complex workflows using JSON configuration files. Here's an example `workflow.json` file:

```json
{
  "Host": "10.27.27.62",
  "Port": 30050,
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
      "Type": "FillString",
      "Coordinates": {"Row": 10, "Column": 44},
      "Text": "b0001"
    },
    {
      "Type": "AsciiScreenGrab"
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

Execute the workflow defined in `workflow.json`:

```bash
go3270 -config workflow.json 
```

### API Mode

Run `go3270` in API mode to interact with terminal sessions via HTTP POST requests. Start the API on port 8080:

```bash
go3270 -api -api-port 8080
```

Use tools like Postman to send POST requests with JSON payloads to `http://localhost:8080` for terminal actions.

### Concurrent Mode

Execute multiple commands concurrently with `go3270`. Specify the number of concurrent workflows:

```bash
go3270 -concurrent <num> 
```

- Replace `<num>` with the desired number of concurrent workflows.

## Additional Options

- `-verbose`: Run `go3270` in verbose mode for detailed output.
- `-help`: Show usage information.

## Contributing

If you find any issues or have suggestions for improvements, please feel free to [open an issue](https://gitlab.jnnn.gs/jnnngs/go3270/-/issues) or submit a pull request.

## License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.

---

--- Appendix

-- directly running x3270 in script mode
x3270 -script < test.script

-- recreate bindata
cd ~/chatGPT/go3270/binaries/
cp /usr/bin/x3270 .
cp /usr/bin/x3270if .
cp /usr/bin/s3270 .
go-bindata -o bindata.go -pkg binaries -prefix "binaries/" .