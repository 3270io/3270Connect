Certainly! Here's a README that includes instructions for using the `go3270` command-line utility with a workflow JSON file that defines a series of steps to automate interactions with a 3270 terminal emulator session.

---

# go3270 Command-Line Utility

The `go3270` command-line utility allows you to automate interactions with a 3270 terminal emulator session. It provides a set of commands that can be executed from the command line, making it easy to script and automate mainframe tasks. In addition, you can define complex workflows using JSON configuration files.

## Installation

Before using `go3270`, make sure you have it installed. You can download and install it using the following command:

```bash
go get -u gitlab.jnnn.gs/jnnngs/go3270
```

## Usage

### Basic Commands

- **Connect to a 3270 Terminal Session:**

  ```bash
  go3270 connect -host <host> -port <port>
  ```

  - Replace `<host>` with the IP address or hostname of the 3270 terminal emulator.
  - Replace `<port>` with the port number to use for the connection.

- **Disconnect from the Current Session:**

  ```bash
  go3270 disconnect
  ```

- **Capture the Current Screen as HTML:**

  ```bash
  go3270 capture -output <output.html>
  ```

  - Replace `<output.html>` with the path where the screen capture should be saved as an HTML file.

### Advanced Workflows

To automate more complex tasks, you can define workflows using JSON configuration files. Here's an example of a `workflow.json` file that defines a series of steps to automate interactions with a 3270 terminal emulator session:

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
      "Type": "CheckValue",
      "Coordinates": {"Row": 1, "Column": 2, "Length": 12},
      "Text": "Scrn: BANK10"
    },
    {
      "Type": "FillString",
      "Coordinates": {"Row": 10, "Column": 44},
      "Text": "b0001"
    },
    {
      "Type": "FillString",
      "Coordinates": {"Row": 11, "Column": 44},
      "Text": "mypass"
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

To execute the workflow defined in `workflow.json`, use the following command:

```bash
go3270 workflow -config workflow.json
```

This will perform the specified actions on the 3270 terminal emulator session based on the defined workflow steps.

## Contributing

If you find any issues or have suggestions for improvements, please feel free to [open an issue](https://gitlab.jnnn.gs/jnnngs/go3270/-/issues) or submit a pull request.

## License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.

---

With this README, you can easily get started with `go3270` and automate interactions with a 3270 terminal emulator session using both basic commands and more complex workflows defined in JSON configuration files.


--- Appendix

-- directly running x3270 in script mode
x3270 -script < test.script

-- recreate bindata
cd ~/chatGPT/go3270/binaries/
cp /usr/bin/x3270 .
cp /usr/bin/x3270if .
cp /usr/bin/s3270 .
go-bindata -o bindata.go -pkg binaries -prefix "binaries/" .