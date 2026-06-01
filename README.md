# 3270Connect

![3270Connect dashboard](https://raw.githubusercontent.com/3270io/3270Connect/main/docs/dashboard.png)

3270Connect is a robust automation toolkit that provides both a command-line utility and 3270Web, a browser-based web console for enhancing productivity and efficiency when managing and automating interactions with mainframe 3270 applications. It acts as a bridge between modern computing environments and the traditional mainframe terminals, providing a suite of tools that facilitate automated tasks and workflows in a terminal session.

The utility is used by system administrators, developers, and testers who frequently interact with mainframe systems, which are still pivotal in various industries such as banking, insurance, and government services. With 3270Connect, users can script complex sequences of tasks, automate data entry, perform complex online operations, and capture terminal screens for logging or debugging purposes.

One of the main reasons for using 3270Connect is its ability to save time on repetitive tasks by automating them. This can be especially beneficial in testing scenarios where the same set of operations needs to be performed repeatedly. Moreover, the utility provides a way to integrate mainframe operations with modern CI/CD pipelines, thereby modernizing the development and deployment workflows that involve mainframe systems.

With 3270Connect, users can:

- Define and execute automated workflows through a configuration file, enhancing repeatability and reliability in interactions with terminal screens.
- Capture the state of the 3270 terminal screens at any point during a workflow, which is invaluable for documentation and troubleshooting.
- Execute multiple workflows in parallel, optimizing time and resources, especially in complex test environments.
- Operate in a headless mode, allowing the automation to run in the background or in environments without a graphical interface, such as servers or continuous integration systems.
- Utilize a verbose output mode for an in-depth understanding of workflow execution, which assists in monitoring and debugging.
- Surface failure-only logging with `-verboseFailures` to collect concise diagnostics at high concurrency without the noise of full verbose output.
- Run 3270Connect as an API server, enabling advanced automation scenarios and facilitating load and performance testing of mainframe applications.
- Use AI Chat mode in 3270Web to inspect screens, fill fields, press keys, and run chaos exploration through plain-language conversation.

Through these features, 3270Connect empowers organizations to integrate their legacy systems into modern automated processes, reducing errors, and increasing efficiency.

> **Windows SmartScreen notice**  
> This app is digitally signed.  
> If Windows shows **“protected your PC”**, click **More info → Run anyway**.  
> The warning disappears automatically as usage grows.

## Features

Here are the key features of 3270Connect:

- Running workflows defined in a configuration file.
- Command-line interface for scripting and running automation from the terminal.
- Capturing the 3270 screens as the workflow executes.
- Running workflows concurrently with options for controlling the number of concurrent workflows and runtime duration.
- Dashboard and 3270Web to visually provide metrics on concurrency usage, manage runs, and open AI Chat mode for conversational session control.
- Headless mode for running workflows without a graphical user interface.
- Verbose mode for detailed output, plus failure-only logging with `-verboseFailures` for noisy test loads.
- API mode for advanced automation.
- AI Chat mode for screen reading, field entry, key presses, and chaos exploration with explicit approval or Auto Mode.
- Runtime RSA token injection using the `-token` flag or API `Token` property, keeping one-time passwords out of workflow files.
- Configurable host EBCDIC code page / character set via the workflow `CodePage` property or the `-codePage` flag (for example `cp037`, `cp285`, or `cp278`/`finnish`) so national and language-specific characters render correctly.
- Prometheus `/metrics` endpoint (`-promListen`) exposing connect/step timing histograms, workflow outcomes, and live worker count for fleet-scale monitoring.
- One-shot host compatibility profiler (`-profile`) that writes a `CompatibilityProfile` JSON document — same schema as 3270Web — for cross-environment comparison and chaos mind-map diffs.
- Running a 3270 sample application to assist with testing workflow features.

## Connection timeout and retries

- The emulator script connection uses a 5-second TCP dial timeout (`scriptDialTimeout`) and a 30-second I/O deadline (`scriptIOTimeout`) when communicating with the embedded x3270/s3270 instance.  
  Source: `connect3270/emulator.go`.
- Establishing a TN3270 session runs through `Emulator.Connect`, which retries up to 10 times with a 1-second delay between attempts (`maxRetries`/`retryDelay`). Starting the emulator process itself is also retried up to 10 times before surfacing an error.  
  Source: `connect3270/emulator.go`.
- After a successful `Connect`, the workflow waits for the terminal to unlock an input field (`WaitForField`) using a 1-second timeout and up to 10 retries before each step. Disable this with top-level `WaitForField: false` in the config and/or add explicit `WaitForField` steps where needed.  
  Source: `go3270Connect.go`.
- Connection failures for the workflow Connect step are informational by default and do not increment the failed workflow counter; pass `-showConnectionErrors` if you want connection failures to be treated as errors and surfaced in the failure tally.  
  Use `-verboseFailures` to log failing steps without enabling full verbose when you need concise failure diagnostics at scale.  
  Source: `go3270Connect.go`.
- The `/testConnection` API endpoint that probes host reachability uses a 5-second TCP dial timeout when opening the socket to the TN3270 host.  
  Source: `go3270Connect.go`.

## Host code page / character set

3270 sessions exchange data in EBCDIC, so the correct national code page (host character set) must be selected for accented and language-specific characters to render correctly. Set it either in the workflow JSON with the `CodePage` property or on the command line with `-codePage` (the flag overrides the JSON value):

```bash
# Finnish/Swedish host (cp278)
3270Connect -config workflow.json -codePage cp278
```

```json
{ "Host": "mvs.example.com", "Port": 992, "CodePage": "cp278", "Steps": [ ... ] }
```

The value is passed straight to the embedded x3270/s3270 `-codepage` option, so any code page name (`cp037`), alias (`finnish`), or number (`278`) the emulator recognizes is accepted. When unset, the emulator default is used. In API mode the per-request `CodePage` property wins, falling back to the `-codePage` flag the server was started with. See the [Basic Usage guide](https://3270.io/basic-usage/#host-code-page-and-character-set) for the supported code page list.

## Metrics

Enable the Prometheus listener with `-promListen <addr>`:

```bash
3270Connect -config workflow.json -concurrent 10 -runtime 300 -promListen :9091
```

Collectors:

- `tn3270_connect_seconds` — histogram of TN3270 session establishment time.
- `tn3270_step_seconds{action}` — histogram of per-step wall time.
- `tn3270_workflow_total{result}` — `success` / `failure` / `connect_failed` counter.
- `tn3270_concurrent_workers` — live worker gauge.

See the [Metrics & Monitoring guide](https://3270.io/metrics/) for example queries and a Prometheus scrape config.

## Host Compatibility Profiler

Run a one-shot probe against a host and capture its negotiated terminal model, protocol options, capabilities, and timing:

```bash
3270Connect -profile -profileHost mvs01.example.com -profilePort 992 -profileTLS \
            -profileOut mvs01.profile.json
```

The resulting `CompatibilityProfile` JSON document uses the same schema as 3270Web's `POST /profile` endpoint, so profiles produced by either tool can be diffed against each other (e.g. IBM z/OS vs Rocket Enterprise Server). See the [profiler guide](https://3270.io/host-profiler/) and the [schema reference](https://3270.io/compatibility-profile-schema/).

## Security hardening

Recent releases tightened input handling across the dashboard and API surfaces. User-facing guarantees:

- Sample-app launch arguments are validated against an allow-list — no shell or argument injection via crafted process names.
- Uploaded workflow filenames in the dashboard's `start-process` handler are sanitised; path separators and traversal sequences are rejected.
- The dashboard rejects absolute or parent-escaping values for `overrideOutputFilePath`; outputs always land under the configured working directory.
- `getNextAvailablePort` is bounded and propagates exhaustion errors instead of looping indefinitely under heavy concurrency.

## Documentation

- [Documentation](https://3270.io)
- [AI Chat Mode](https://3270.io/ai-chat-mode/)
- [Metrics & Monitoring](https://3270.io/metrics/)
- [Host Compatibility Profiler](https://3270.io/host-profiler/)

## License

This project is licensed under the MIT License - see the [LICENSE](https://github.com/3270io/3270Connect/blob/main/LICENSE) file for details.

## Notes

go-bindata -o binaries/bindata.go -pkg binaries ./binaries/...

CGO_ENABLED=1 GOOS=linux GOARCH=amd64 go build -o 3270Connect .

CGO_ENABLED=1 GOOS=windows GOARCH=amd64 go build -o 3270Connect.exe .

.\3270Connect -runApp 1
./3270Connect -verbose -headless

mkdocs build

## Refreshing embedded binaries

Run `.\update-binaries.ps1` from the repo root after you update `binaries/linux` or `binaries/windows`. The script now simply runs `go-bindata -o binaries/bindata.go -pkg binaries ./binaries/...` against the assets that already live in those directories, so make sure the native executables you need are in place beforehand.
