# 3270Connect

Automation toolkit for IBM mainframe 3270 terminal systems. Enables scripted workflows via JSON config, concurrent load testing, REST API mode, and a web dashboard.

**Version:** 2.0.0  
**Language:** Go 1.25

## Project Structure

```
go3270Connect.go          # Main entry point (~3,900 lines) — CLI, API, dashboard, workflow runner
connect3270/emulator.go   # Core TN3270 protocol implementation (x3270/s3270 wrapper)
charmui.go                # Terminal UI rendering (lipgloss/pterm)
auth*.go                  # Authentication and authorization — see below
templates/dashboard.gohtml # Web dashboard markup (+ inline icon sprite)
templates/auth/            # Sign-in, setup and administration pages (layout + pages/)
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

# Accounts (only meaningful with AUTH_MODE=local or oidc)
./dist/3270Connect user add root --admin
./dist/3270Connect token add root "ci pipeline"

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
| `-api-bind` | Interface for the API listener (default `localhost`; env `API_BIND`) |
| `-dashboard` / `-dashboardPort` | Web dashboard |
| `-dashboardBind` | Interface for the dashboard listener (default `localhost`, `all` for every interface; env `DASHBOARD_BIND`). The container image sets it to `0.0.0.0` |
| `-headless` | Drive the session with s3270 rather than opening an x3270 window. It does **not** silence the terminal UI — the header, live stats and run report still print, which is what CI logs capture. Without it a run needs an X display, so this is the flag for any headless box |
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

- `POST /api/execute` — Execute a workflow (the `-api` listener)
- `GET /dashboard` — Web dashboard
- `GET /dashboard/data` — Live metrics JSON
- `POST /start-process` / `POST /kill` — Process lifecycle
- `POST /test-connection` — Connectivity test
- `GET /healthz` — Liveness; reachable without a credential in every mode
- `/login`, `/logout`, `/setup`, `/account/password`, `/whoami`, `/auth/sso*` — Sign-in
- `/admin`, `/admin/{users,groups,tokens,runs,audit}` and `/admin/api/*` — Administration (admin role)

## Authentication and Authorization

Off by default (`AUTH_MODE=none`): one operator, no sign-in, everything open —
and that must stay true, because it is what every existing install relies on.
`AUTH_MODE=local` adds accounts; `AUTH_MODE=oidc` adds an identity provider.
The variable names, roles, group model and token format are deliberately
identical to 3270Web's, and `internal/{authz,authsession,users,apitoken,audit,oidc,reqsec}`
are the same packages — keep them in step rather than letting them drift.

**The copying is one-way now.** This repository is MIT; 3270Web is
AGPL-3.0-or-later. Code may go from here into 3270Web, never the reverse
unless the copyright holder wrote it. Write shared changes here first, then
copy across. See "Shared packages" in `README.md`.

| File | Holds |
|------|-------|
| `auth.go` | `authState`, the request gate, principals, run ownership, the sweep |
| `authlogin.go` | Sign in, sign out, change password, `whoami` |
| `authsetup.go` | First-run setup and its one-time code |
| `authsso.go` | OIDC sign-in |
| `authtokens.go` | Bearer tokens, scopes, the gin middleware for `-api` |
| `authadmin.go` | The `/admin` pages and their JSON endpoints |
| `authcli.go` | `3270Connect user …` and `3270Connect token …` |
| `authlimit.go` | Failed-sign-in throttling |
| `authpages.go` | Page templates; one parsed set per page, and their CSP |

Things that are load-bearing and easy to break:

- **The gate wraps the whole mux** (`http.Serve(listener, auth.Gate(...))`), so
  a route registered later cannot forget to opt in. `/admin` is gated by
  prefix for the same reason. Do not move either to per-route middleware.
- **State lives under `os.UserConfigDir()/3270Connect`** — `/data` in the image,
  via `XDG_CONFIG_HOME`. `users.json`, `api-tokens.json`, `audit.log`.
- **CSRF uses `Sec-Fetch-Site` first**, then `Origin`/`Referer`, and allows a
  request carrying none of the three. That last part is not an oversight: curl,
  CI and the installer's checks call these endpoints and send no browser
  headers.
- **`AUTH_MODE=none` + no `API_TOKEN` leaves the REST API open.** Changing that
  breaks every existing deployment.
- A run's owner lives in memory (`runOwners`), keyed by pid, and unowned means
  admin-only rather than anybody's.

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

`Dockerfile` is the Linux image: linux/amd64 only (the embedded s3270 is an
x86-64 binary), glibc runtime for it to link against, non-root uid 10001, state
in `/data` via `XDG_CONFIG_HOME`, `CMD ["-dashboard"]` so it serves the console
unless given other flags. Published to `ghcr.io/3270io/3270connect` by CI.

```bash
docker build -f Dockerfile -t 3270connect .         # Linux
docker build -f Dockerfile.windows -t 3270connect . # Windows (on a Windows host)

docker compose up -d                                # the console alone
docker compose -f docker-compose.lab.yml up -d      # + a terminal and a 3270 host
```

`docker-compose.lab.yml` runs 3270Web's sample applications as a TN3270 host
(`3270Web sampleapp`), the browser terminal and this console together;
`lab/workflow-sampleapp.json` is aimed at it. The mirror of that file lives in
the 3270Web repository.

## Installer

`docs/install.sh` publishes to <https://3270connect.3270.io/install.sh>. Four
methods — binary, docker, compose, lab. `install_test.go` runs it against
`testdata/fake-docker.sh`, a Docker stand-in, and checks the generated stacks
and the re-run carry-forward (a second run must update the install that is
already here, against the data folder it already uses).

## Dependencies

Key Go modules:
- `github.com/gin-gonic/gin` — REST API
- `github.com/racingmars/go3270` — TN3270 protocol
- `github.com/charmbracelet/lipgloss` — Terminal styling
- `github.com/pterm/pterm` — Terminal UI
- `github.com/jchv/go-webview2` — Windows WebView2 UI
- `github.com/shirou/gopsutil` — System metrics
