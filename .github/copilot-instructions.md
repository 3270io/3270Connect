# 3270Connect Copilot Instructions

## Architecture & layout
- Go automation tool for 3270 workflows: CLI + API server + optional dashboard UI.
- Main entrypoint is `go3270Connect.go` (CLI flags, API routes, dashboard handlers).
- TN3270 emulator control/retries live in `connect3270/` (see `emulator.go` for session logic).
- Dashboard HTML templates are in `templates/`, static assets in `app/static/` and `site/`.
- Embedded x3270/s3270 binaries live under `binaries/` and are bundled via generated `binaries/bindata.go`.
- Sample workflows/apps are under `sampleapps/` with example configs like `workflow*.json`.
- Keep platform-specific behavior in existing split files (`open_dashboard_windows.go`, `open_dashboard_other.go`) instead of adding OS checks inline.

## Build, test, docs
- Toolchain: Go 1.23.x (see `go.mod` toolchain). No repo-specific linter beyond `gofmt`/`go vet`.
- CI build order: `./build.sh` (or `pwsh ./build.ps1` on Windows) → `go test -v ./...`.
- `build.sh` writes `resource.syso` and `dist/` binaries; avoid committing `dist/` updates unless intentionally refreshing shipped artifacts.
- Docs site: `pip install --upgrade mkdocs-material pymdown-extensions mkdocs-video mkdocs-simple-plugin` then `mkdocs build` → `site/`.
- x3270 asset build flow is separate: use `make linux-only` (or `make linux` / `make windows`) when updating binaries under `binaries/`.

## Commands to run before/after changes
- Format changed Go files: `gofmt -w <changed-files>.go`
- Basic static checks: `go vet ./...`
- Build: `./build.sh` (Linux/macOS shell) or `pwsh ./build.ps1` (Windows/CI path)
- Tests: `go test -v ./...`
- Optional docs build when docs changed: `mkdocs build`

## Coding conventions
- Stay in idiomatic Go patterns already used here: small functions, explicit `if err != nil` handling, and concrete error messages.
- Preserve backward-compatible config parsing patterns (for example `WaitForField` custom JSON unmarshaling supporting both bool and object forms).
- Keep workflow/state shared data concurrency-safe (`sync.Mutex`, `sync/atomic`) as done for status/history/error collections in `go3270Connect.go`.
- Preserve existing step names and JSON field names; they are part of user-facing workflow contracts.
- Avoid adding dependencies unless required by existing architecture.

## Error handling and observability
- Return errors to callers instead of panicking; only log where behavior already logs.
- Keep connection/retry behavior consistent with existing constants (`scriptDialTimeout`, `scriptIOTimeout`, retry loops).
- Respect verbose modes (`-verbose`, `-verboseFailures`) and avoid introducing noisy default logging.
- Keep failure-screen capture and failure counters behavior unchanged unless a task explicitly targets it.

## Security expectations
- Never hardcode credentials, tokens, or host secrets in code/tests/docs.
- Preserve runtime token injection patterns (`-token`, API `Token`, and `{{token}}` placeholder replacement) instead of writing secrets to workflow JSON.
- Validate external input from JSON/API routes before use; follow existing `validateConfiguration` style.
- Do not weaken auth/session controls in API paths.

## Testing expectations
- Add/adjust tests in existing `*_test.go` files near changed code (`go3270Connect_test.go`, `connect3270/emulator_test.go`).
- Prefer table-driven tests for step/config validation and deterministic tests by controlling RNG/time where needed.
- Run targeted package tests while iterating, then run full `go test -v ./...` before completion.
- If modifying workflow JSON compatibility behavior, include coverage for both legacy and current forms.

## Documentation expectations
- Update `README.md` and/or `docs/` when user-visible behavior, flags, config schema, or API behavior changes.
- Keep docs aligned with actual commands in CI (`.github/workflows/go.yml`) and scripts (`build.sh`, `build.ps1`).

## Project-specific workflows
- Run the CLI with a config: `./dist/3270Connect_linux -headless -config workflow.json`.
- Update embedded emulator binaries by replacing `binaries/linux` or `binaries/windows`, then run `./update-binaries.ps1` to regenerate `binaries/bindata.go`.
