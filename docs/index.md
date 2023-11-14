
 <div style="text-align: left;">
  <img src="logo.png" alt="3270.io" style="max-width: 200px; height: auto;">
</div>

## Introduction

Welcome to the official documentation for the 3270Connect command-line utility. This documentation provides comprehensive information on how to use 3270Connect for automating interactions with terminal emulators like x3270 or s3270.

## Features

Here are the key features of 3270Connect:

- Running workflows defined in a configuration file.
- Capturing the 3270 screens as the workflow executes.
- Running workflows concurrently with options for controlling the number of concurrent workflows and runtime duration.
- Headless mode for running workflows without a graphical user interface.
- Verbose mode for detailed output.
- API mode for advanced automation.
- Running a 3270 sample application to assist with testing workflow features.

## Getting Started

If you're new to 3270Connect, you can start by exploring the following sections:

- [Installation](installation.md): Learn how to install 3270Connect on your system.
- [Basic Usage](basic-usage.md): Get started with basic usage, running workflows and sample 3270 application(s) to aid testing.

## Advanced Features

Once you've mastered the basics, you can dive into more advanced features:

- [API Mode](advanced-features.md): Discover how to run 3270Connect as an API server for advanced automation and load performance testing.

## Known issues and short term planned changes

1. <s>When running under concurrent mode with runtime and the volumes are high, the tactical logic to `sleep and retry` on issue no longer works. This is planed to be replaced with wait_for_field logic.</s> Done

2. <s>When running under concurrent mode with no runtime, the ramp logic is not in place.</s> Fixed

3. <s>When running in API mode, make headless the default option.</s> Done

4. <s>Remake the videos in higher resolution.</s> Done

5. When running in API mode, provide a new option to return the HTML screen grab contents.

6. App additional dynamic sample 3270 applications

## Conclusion

The 3270Connect command-line utility is a powerful tool for automating terminal emulator interactions. This documentation is here to help you make the most of it. If you have any questions or need assistance, feel free to reach out to the community or refer to the [GitHub repository](https://github.com/3270io/3270Connect) for more details.

Let's get started with 3270Connect!

## Video example

### 3270Connect Basic Usage

![type:video](3270Connect_1_0_3_9.mp4){: style=''}

### 3270Connect API Usage

![type:video](3270Connect_API_1_0_3_9.mp4){: style=''}
