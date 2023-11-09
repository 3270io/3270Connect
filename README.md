<img
  src="docs/logo.png"
  alt="3270Connect logo"
  title="3270Connect logo"
  style="display: inline-block; margin: 0 auto; max-width: 300px">

# 3270Connect

3270Connect is a Go package and command-line utility for interacting with x3270 or s3270 terminal emulators, commonly used for mainframe and 3270 terminal applications. It provides a convenient way to automate terminal interactions, capture screens, and perform various tasks programmatically.

## Features

- **Terminal Automation**: Interact with terminal screens, send keys, and fill fields programmatically using a external workflow json file.
- **Screen Captures**: Capture terminal screens in ASCII format and save them to HTML files.
- **Cross-Platform**: Works on Linux, macOS, and Windows (soon).
- **Performance Load Testing**: Run a number of concurrent connections with an optional period of time 

## Documentation

- [ Documentation](https://3270.io)

## Known issues and short term planned changes

1. When running under concurrent mode with runtime and the volumes are high, the tactical logic to 'sleep and retry' on issue no longer works. This is planed to be replaced with wait_for_field logic.

2. When running under concurrent mode with no runtime, the ramp logic is not in place.

3. When running in API mode, make headless the default option.

4. When running in API mode, provide a new option to return the HTML screen grab contents.

5. Remake the videos in higher resolution.

## License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.
