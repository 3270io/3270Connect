# 3270Connect Copilot Instructions

## Architecture & layout
- Go automation tool for 3270 workflows: CLI + API server + optional dashboard UI.
- Main entrypoint is `go3270Connect.go` (CLI flags, API routes, dashboard handlers).
- TN3270 emulator control/retries live in `connect3270/` (see `emulator.go` for session logic).
- Dashboard HTML templates are in `templates/`, static assets in `app/static/` and `site/`.
- Embedded x3270/s3270 binaries live under `binaries/` and are bundled via generated `binaries/bindata.go`.
- Sample workflows/apps are under `sampleapps/` with example configs like `workflow*.json`.

## Build, test, docs
- Toolchain: Go 1.23.x (see `go.mod` toolchain). No repo-specific linter beyond `gofmt`/`go vet`.
- CI build order: `./build.sh` (or `pwsh ./build.ps1` on Windows) → `go test -v ./...`.
- `build.sh` writes `resource.syso` and `dist/` binaries; avoid committing `dist/` updates unless intentionally refreshing shipped artifacts.
- Docs site: `pip install --upgrade mkdocs-material pymdown-extensions mkdocs-video mkdocs-simple-plugin` then `mkdocs build` → `site/`.

## Project-specific workflows
- Run the CLI with a config: `./dist/3270Connect_linux -headless -config workflow.json`.
- Update embedded emulator binaries by replacing `binaries/linux` or `binaries/windows`, then run `./update-binaries.ps1` to regenerate `binaries/bindata.go`.
