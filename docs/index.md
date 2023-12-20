
 <div style="text-align: left;">
  <img src="logo.png" alt="3270.io" style="max-width: 200px; height: auto;">
</div>

## Introduction

3270Connect is a robust command-line utility designed specifically for enhancing productivity and efficiency in managing and automating interactions with mainframe 3270 applications. It acts as a bridge between modern computing environments and the traditional mainframe terminals, providing a suite of tools that facilitate automated tasks and workflows in a terminal session.

The utility is used by system administrators, developers, and testers who frequently interact with mainframe systems, which are still pivotal in various industries such as banking, insurance, and government services. With 3270Connect, users can script complex sequences of tasks, automate data entry, perform complex online operations, and capture terminal screens for logging or debugging purposes.

One of the main reasons for using 3270Connect is its ability to save time on repetitive tasks by automating them. This can be especially beneficial in testing scenarios where the same set of operations needs to be performed repeatedly. Moreover, the utility provides a way to integrate mainframe operations with modern CI/CD pipelines, thereby modernizing the development and deployment workflows that involve mainframe systems.

With 3270Connect, users can:

- Define and execute automated workflows through a configuration file, enhancing repeatability and reliability in interactions with terminal screens.
- Capture the state of the 3270 terminal screens at any point during a workflow, which is invaluable for documentation and troubleshooting.
- Execute multiple workflows in parallel, optimizing time and resources, especially in complex test environments.
- Operate in a headless mode, allowing the automation to run in the background or in environments without a graphical interface, such as servers or continuous integration systems.
- Utilize a verbose output mode for an in-depth understanding of workflow execution, which assists in monitoring and debugging.
- Run 3270Connect as an API server, enabling advanced automation scenarios and facilitating load and performance testing of mainframe applications.

Through these features, 3270Connect empowers organizations to integrate their legacy systems into modern automated processes, reducing errors, and increasing efficiency.

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
- [Workflow Steps](workflow.md): Overview of the various workflow steps available in the 3270Connect application

## Advanced Features

Once you've mastered the basics, you can dive into more advanced features:

- [API Mode](advanced-features.md): Discover how to run 3270Connect as an API server for advanced automation and load performance testing.

### UPDATE: Version 1.0.4 has now been tagged as the first stable release

1. <s>When running under concurrent mode with runtime and the volumes are high, the tactical logic to `sleep and retry` on issue no longer works. This is planed to be replaced with wait_for_field logic.</s> Done

2. <s>When running under concurrent mode with no runtime, the ramp logic is not in place.</s> Fixed

3. <s>When running in API mode, make headless the default option.</s> Done

4. <s>Remake the videos in higher resolution.</s> Done

5. <s>When running in API mode, provide a new option to return the HTML screen grab contents.</s> Done

6. <s>App additional dynamic sample 3270 applications.</s> Done

7. <s>Give the option to return pure ASCII or HTML for the screen captures. API mode to default as pure.</s>

## Conclusion

The 3270Connect command-line utility is a powerful tool for automating terminal emulator interactions. This documentation is here to help you make the most of it. If you have any questions or need assistance, feel free to reach out to the community or refer to the [GitHub repository](https://github.com/3270io/3270Connect) for more details.

Let's get started with 3270Connect!

## Video example

### 3270Connect Basic Usage

![type:video](3270Connect_1_0_3_9.mp4){: style=''}

### 3270Connect API Usage

![type:video](3270Connect_API_1_0_4_0.mp4){: style=''}
