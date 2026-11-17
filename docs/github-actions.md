---
seo_title: "Automate 3270 regression tests in GitHub Actions"
description: >-
  Run a headless 3270 workflow in GitHub Actions, assert the saved result and
  retain diagnostics. Start with a sample host, then connect your test mainframe.
---

# Automate 3270 regression tests in GitHub Actions

3270Connect runs IBM 3270 workflows headless on a CI runner. Record a business
flow in [3270Web](https://3270web.3270.io/workflow/), add explicit screen checks,
then run the JSON on each change. This walkthrough first checks a bundled
sample host: no mainframe account or AI provider is needed.

## Before you start

Use a Linux runner with Docker. Set the repository variable
`TN3270_CONNECT_IMAGE` to a reviewed, pinned image reference such as
`ghcr.io/3270io/3270connect@sha256:<your-verified-digest>`.
Copy a real published digest into that variable; the placeholder is not runnable.
The current published image targets `linux/amd64`.

The sample host is separate from the runner container, so the workflow connects
to its Docker network name rather than `127.0.0.1`. The latter would address the
workflow container itself.

## 1. Commit a screen assertion

Save this as `tests/3270/workflow.json`:

```json
{
  "Host": "sample-host",
  "Port": 3270,
  "OutputFilePath": "screens.html",
  "WaitForField": { "Delay": 1, "Retries": 10 },
  "Steps": [
    { "Type": "Connect" },
    {
      "Type": "CheckValue",
      "Coordinates": { "Row": 1, "Column": 29, "Length": 24 },
      "Text": "3270 Example Application"
    },
    { "Type": "AsciiScreenGrab" },
    { "Type": "Disconnect" }
  ]
}
```

This uses the first bundled 3270Connect sample application and the same title
assertion as the repository's sample workflow. A real regression test should
also check the business result, not merely that a login page appeared. See
[Workflow Actions](workflow.md) for field coordinates and assertions.

## 2. Add the workflow

Save as `.github/workflows/3270-regression.yml`:

```yaml
name: 3270 regression
on: [push, pull_request, workflow_dispatch]
permissions:
  contents: read

jobs:
  sample-regression:
    runs-on: ubuntu-latest
    timeout-minutes: 5
    env:
      CONNECT_IMAGE: ${{ vars.TN3270_CONNECT_IMAGE }}
    steps:
      - uses: actions/checkout@v4
      - name: Run the sample workflow and assert the result
        shell: bash
        run: |
          set -euo pipefail
          : "${CONNECT_IMAGE:?Set TN3270_CONNECT_IMAGE to a pinned image digest}"
          work="$RUNNER_TEMP/3270-regression"
          network="tn3270-${GITHUB_RUN_ID}-${GITHUB_RUN_ATTEMPT}"
          host="${network}-host"
          mkdir -p "$work"
          cp tests/3270/workflow.json "$work/workflow.json"
          docker network create "$network"
          trap 'docker rm -f "$host" >/dev/null 2>&1 || true; docker network rm "$network" >/dev/null 2>&1 || true' EXIT
          docker run -d --name "$host" --network "$network" \
            --network-alias sample-host "$CONNECT_IMAGE" \
            -runApp 1 -runApp-port 3270
          # Wait for the TCP listener without publishing it on the runner host.
          ready=false
          for attempt in {1..30}; do
            if docker run --rm --network "$network" --entrypoint bash \
              "$CONNECT_IMAGE" -c 'echo >/dev/tcp/sample-host/3270' 2>/dev/null; then
              ready=true
              break
            fi
            sleep 1
          done
          if [ "$ready" != true ]; then
            docker logs "$host"
            exit 1
          fi
          docker run --rm --network "$network" \
            --user "$(id -u):$(id -g)" -v "$work:/data" \
            "$CONNECT_IMAGE" -config workflow.json -headless \
            -showConnectionErrors -workflowTimeout 60 -verboseFailures
          python3 - "$work" <<'PY'
          import pathlib, re, sys
          summaries = list(pathlib.Path(sys.argv[1]).glob("logs/summary_*.txt"))
          if len(summaries) != 1:
              raise SystemExit("Expected exactly one saved run summary")
          report = summaries[0].read_text()
          print(report)
          for label, expected in [("Started", 1), ("Completed", 1), ("Failed", 0)]:
              match = re.search(rf"^Total Workflows {label}: (\d+)$", report, re.M)
              if not match or int(match[1]) != expected:
                  raise SystemExit(f"Unexpected workflow total: {label}")
          PY
      - name: Retain sample diagnostics
        if: always()
        uses: actions/upload-artifact@v4
        with:
          name: 3270-sample-result
          path: ${{ runner.temp }}/3270-regression/
          retention-days: 7
```

The saved summary gate matters: the current CLI can finish a failed workflow
without returning a failing process exit status. A missing summary, no completed
workflow, or a nonzero failure count must fail the check. The parser deliberately
fails if the report format changes; review it when upgrading the pinned image.

## 3. Prove the check can fail

Run the sample successfully, then temporarily change the expected title to an
incorrect value. The job must fail and the saved report should identify the
failed check. Restore the expected value and confirm it passes again. This is a
stronger CI check than treating a completed process as a completed transaction.

## Use your test mainframe

Replace the sample host with a reachable test-system address and use a reviewed
recording. Choose a self-hosted runner with an approved route to private hosts;
a hosted runner does not automatically reach your internal TN3270 service.
Keep host TLS validation enabled and configure the terminal model/code page to
match the test system. See [Basic Usage](basic-usage.md).

Keep credentials outside committed recordings. Use reviewed runtime
[field injection](injection-config.md) or the documented one-time token
mechanism where appropriate. Restrict host-test jobs to trusted branches or
manual runs; do not give untrusted pull requests host credentials. Screen
captures and logs from a real host may contain business data: restrict or omit
artifact upload rather than copying this sample's retention policy blindly.

Use one workflow as a regression gate. For controlled performance exercises,
continue with [Load Testing](load-testing.md).
