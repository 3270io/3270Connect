# 3270Connect

Automation toolkit for IBM mainframe 3270 terminal systems. Enables scripted workflows via JSON config, concurrent load testing, REST API mode, and a web dashboard.

**Version:** 1.9.2  
**Language:** Go 1.25

## Project Structure

```
go3270Connect.go          # Main entry point (~3,800 lines) — CLI, API, dashboard, workflow runner
connect3270/emulator.go   # Core TN3270 protocol implementation (x3270/s3270 wrapper)
charmui.go                # Terminal UI rendering (lipgloss/pterm)
templates/dashboard.gohtml # Web dashboard markup (+ inline icon sprite)
templates/static/          # Embedded dashboard assets (css/, js/, vendor/, images/)
sampleapps/               # Embedded sample 3270 apps for testing
binaries/                 # Pre-compiled x3270 binaries (linux/ and windows/)
docs/                     # MkDocs source (published to 3270connect.3270.io)
dist/                     # Build output — committed to repo by CI
```

## Build & Run

```bash
# Build binaries (outputs to dist/)
./build.sh

# Run with a workflow
./dist/3270Connect -config workflow.json

# Concurrent load test (10 workers, 60s)
./dist/3270Connect -config workflow.json -concurrent 10 -runtime 60

# API server mode
./dist/3270Connect -api -api-port 8080

# Run embedded sample app for testing
./dist/3270Connect -runApp 1 -runApp-port 3270

# Tests
go test -v ./...
```

## Key Flags

| Flag | Description |
|------|-------------|
| `-config` | Workflow JSON file (default: workflow.json) |
| `-injectionConfig` | Dynamic field injection JSON |
| `-token` | RSA token substitution |
| `-codePage` | Host EBCDIC code page / charset (e.g. `cp037`, `cp278`/`finnish`); overrides workflow `CodePage`, passed to s3270 `-codepage` |
| `-concurrent` | Number of parallel workflows (default: 1) |
| `-runtime` | Max run duration in seconds |
| `-api` / `-api-port` | REST API mode |
| `-dashboard` / `-dashboardPort` | Web dashboard |
| `-headless` | No terminal UI (for CI/CD) |
| `-verbose` | Verbose logging |
| `-workflowTimeout` | Per-workflow hard timeout (seconds) |
| `-gracePeriod` | Seconds to wait for in-flight workflows after runtime ends (default: 30; overrides workflow `GracePeriod`) |
| `-autoShutdown` | Seconds for the auto-shutdown countdown prompt when grace period elapses (default: 10; overrides workflow `AutoShutdownTimeout`) |

## Workflow Config Format

A step's keys are `Type`, `Coordinates` and `Text`. They are **not** `Action`,
`Row`/`Column` at the top level, `Value`, or `FilePath` — an earlier version of
this file documented that shape and no such workflow has ever run. Coordinates
are **1-based**. `docs/workflow.md` and `Validate` in `internal/workflow` are
authoritative; regenerate this block from `describe_workflow_schema` rather
than editing it by hand.

```json
{
  "Host": "mainframe.host",
  "Port": 3270,
  "CodePage": "cp037",
  "OutputFilePath": "output.html",
  "EveryStepDelay": { "Min": 0.1, "Max": 0.3 },
  "WaitForField": true,
  "Steps": [
    { "Type": "Connect" },
    { "Type": "FillString", "Coordinates": { "Row": 10, "Column": 20, "Length": 8 }, "Text": "{{username}}" },
    { "Type": "PressEnter" },
    { "Type": "CheckValue", "Coordinates": { "Row": 24, "Column": 1, "Length": 5 }, "Text": "READY" },
    { "Type": "AsciiScreenGrab" },
    { "Type": "Disconnect" }
  ]
}
```

`AsciiScreenGrab` writes to the workflow's top-level `OutputFilePath`, which is
required whenever any step uses it.

## REST API Endpoints

- `POST /api/execute` — Execute a workflow
- `GET /dashboard` — Web dashboard
- `GET /dashboard/data` — Live metrics JSON
- `POST /start-process` / `POST /kill` — Process lifecycle
- `POST /test-connection` — Connectivity test

## CI/CD

GitHub Actions (`.github/workflows/go.yml`):
- Builds Windows + Linux binaries on every push to `main`
- Runs `go test`
- Auto-commits built binaries to `dist/`
- Deploys MkDocs docs to GitHub Pages

## x3270 Binaries (Makefile)

The embedded x3270 binaries are built separately:
```bash
make linux    # Build s3270/x3270if for Linux
make windows  # Cross-compile for Windows
make all      # Both platforms
```
Pre-built binaries are checked into `binaries/`.

## Docker

```bash
docker build -f Dockerfile -t 3270connect .        # Linux
docker build -f Dockerfile.windows -t 3270connect . # Windows
```

## Dependencies

Key Go modules:
- `github.com/gin-gonic/gin` — REST API
- `github.com/racingmars/go3270` — TN3270 protocol
- `github.com/charmbracelet/lipgloss` — Terminal styling
- `github.com/pterm/pterm` — Terminal UI
- `github.com/jchv/go-webview2` — Windows WebView2 UI
- `github.com/shirou/gopsutil` — System metrics
