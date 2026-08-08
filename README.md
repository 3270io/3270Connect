<picture>
  <source media="(prefers-color-scheme: dark)" srcset="brand/3270connect-lockup-600.png">
  <img alt="3270Connect" src="brand/3270connect-lockup-light-600.png" width="300">
</picture>

Scripted 3270 workflows that replay human online integration at unlimited scale —
a command-line utility, an API server, and a live operations console served
straight from the binary, with no external dependencies at runtime.

![3270Connect operations console](https://raw.githubusercontent.com/3270io/3270Connect/main/docs/assets/dashboard/console-overview.webp)

3270Connect bridges modern computing environments and traditional mainframe
terminals, providing a suite of tools that automate tasks and workflows in a
terminal session. It is used by system administrators, developers, and testers
who work with mainframe systems — still pivotal in banking, insurance, and
government services — to script complex sequences of tasks, automate data entry,
perform online operations, and capture terminal screens for logging or debugging.

Its main value is removing repetitive work: the same set of operations, run
reliably and repeatedly, at whatever concurrency a test calls for. It also gives
mainframe operations a way into modern CI/CD pipelines.

With 3270Connect, users can:

- Define and execute automated workflows through a configuration file, enhancing repeatability and reliability in interactions with terminal screens.
- Capture the state of the 3270 terminal screens at any point during a workflow, which is invaluable for documentation and troubleshooting.
- Execute multiple workflows in parallel, optimizing time and resources, especially in complex test environments.
- Watch a run live in the operations console — KPIs, latency percentiles, log streaming and per-process control, served from the binary itself.
- Operate in a headless mode, allowing the automation to run in the background or in environments without a graphical interface, such as servers or continuous integration systems.
- Utilize a verbose output mode for an in-depth understanding of workflow execution, which assists in monitoring and debugging.
- Surface failure-only logging with `-verboseFailures` to collect concise diagnostics at high concurrency without the noise of full verbose output.
- Run 3270Connect as an API server, enabling advanced automation scenarios and facilitating load and performance testing of mainframe applications.

Through these features, 3270Connect empowers organizations to integrate their legacy systems into modern automated processes, reducing errors, and increasing efficiency.

## Where the workflows come from

3270Connect runs workflow JSON; its companion product
[**3270Web**](https://github.com/3270io/3270Web) is where that JSON is usually
produced. 3270Web is an enterprise-grade 3270 terminal in the browser whose AI
auto-navigation explores a host, maps every screen, and exports the full screen
coverage it achieved as a 3270Connect-compatible `workflow.json` — so a load
profile comes from the real application rather than being written by hand. The
two also emit the same `CompatibilityProfile` schema, so host profiles taken by
either tool diff cleanly against each other.

```
3270Web                                            3270Connect
──────────────────────────────────────────         ─────────────────────
browse  →  AI discovers  →  screen graph   ─┐
                            + business fns  ├──→  workflow.json  →  concurrent
run by prompt  ←────────────────────────────┘                       load / volume
                                                                    / CI runs
```

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
- [Operations console](https://3270connect.3270.io/dashboard/) for live workflow metrics, latency percentiles, log streaming and per-process control — served entirely from the binary, with no external dependencies at runtime.
- Headless mode for running workflows without a graphical user interface.
- Verbose mode for detailed output, plus failure-only logging with `-verboseFailures` for noisy test loads.
- API mode for advanced automation.
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

The value is passed straight to the embedded x3270/s3270 `-codepage` option, so any code page name (`cp037`), alias (`finnish`), or number (`278`) the emulator recognizes is accepted. When unset, the emulator default is used. In API mode the per-request `CodePage` property wins, falling back to the `-codePage` flag the server was started with. See the [Basic Usage guide](https://3270connect.3270.io/basic-usage/#host-code-page-and-character-set) for the supported code page list.

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

See the [Metrics & Monitoring guide](https://3270connect.3270.io/metrics/) for example queries and a Prometheus scrape config.

## Host Compatibility Profiler

Run a one-shot probe against a host and capture its negotiated terminal model, protocol options, capabilities, and timing:

```bash
3270Connect -profile -profileHost mvs01.example.com -profilePort 992 -profileTLS \
            -profileOut mvs01.profile.json
```

The resulting `CompatibilityProfile` JSON document uses the same schema as 3270Web's `POST /profile` endpoint, so profiles produced by either tool can be diffed against each other (e.g. IBM z/OS vs Rocket Enterprise Server). See the [profiler guide](https://3270connect.3270.io/host-profiler/) and the [schema reference](https://3270connect.3270.io/compatibility-profile-schema/).

## Security hardening

Recent releases tightened input handling across the dashboard and API surfaces. User-facing guarantees:

- Sample-app launch arguments are validated against an allow-list — no shell or argument injection via crafted process names.
- Uploaded workflow filenames in the dashboard's `start-process` handler are sanitised; path separators and traversal sequences are rejected.
- The dashboard rejects absolute or parent-escaping values for `overrideOutputFilePath`; outputs always land under the configured working directory.
- `getNextAvailablePort` is bounded and propagates exhaustion errors instead of looping indefinitely under heavy concurrency.

## Documentation

- [Documentation](https://3270connect.3270.io)
- [AI Chat Mode](https://3270connect.3270.io/ai-chat-mode/)
- [Metrics & Monitoring](https://3270connect.3270.io/metrics/)
- [Host Compatibility Profiler](https://3270connect.3270.io/host-profiler/)

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

## Brand

The 3270Connect mark and its lockups live in [`brand/`](brand/) — SVG, PNG and
`.ico`, in a dark-ground and a light-ground pair. They are generated from the shared kit in the
[3270io](https://github.com/3270io/3270io) repo (`brand/build.mjs`); regenerate
there rather than editing these by hand.
