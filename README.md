<p align="center">
  <img src="docs/logo.png" alt="3270.io" width="25%">
</p>

# 3270Connect

3270Connect is a Go package and command-line utility for interacting with x3270 or s3270 terminal emulators, commonly used for mainframe and 3270 terminal applications. It provides a convenient way to automate terminal interactions, capture screens, and perform various tasks programmatically.

## Features

Here are the key features of 3270Connect:

- Running workflows defined in a configuration file.
- Capturing the 3270 screens as the workflow executes.
- Running workflows concurrently with options for controlling the number of concurrent workflows and runtime duration.
- Headless mode for running workflows without a graphical user interface.
- Verbose mode for detailed output.
- API mode for advanced automation.
- Running a 3270 sample application to assist with testing workflow features.

## Documentation

- [ Documentation](https://3270.io)

## Prior to Release 1: Known issues and short term planned changes

1. <s>When running under concurrent mode with runtime and the volumes are high, the tactical logic to `sleep and retry` on issue no longer works. This is planed to be replaced with wait_for_field logic.</s> Done

2. <s>When running under concurrent mode with no runtime, the ramp logic is not in place.</s> Fixed

3. <s>When running in API mode, make headless the default option.</s> Done

4. <s>Remake the videos in higher resolution.</s> Done

5. <s>When running in API mode, provide a new option to return the HTML screen grab contents.</s> Done

6. <s>App additional dynamic sample 3270 applications.</s> Done

7. Give the option to return pure ASCII or HTML for the screen captures. API mode to default as pure.

## License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.
