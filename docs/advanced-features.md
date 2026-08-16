---
seo_title: "3270Connect advanced features: API mode and concurrency"
description: >-
  API mode, AI Chat, concurrency and worker fan-out, timeouts and grace
  periods — what 3270Connect does beyond running a single workflow a single
  time.
---


## Advanced Features

### 3270Web AI Chat Mode

3270Web now includes an **AI Chat** side panel for conversational control of a live 3270 session. The assistant reads the current screen, proposes one action at a time, and waits for approval before it writes fields, presses keys, or starts chaos exploration unless **Auto Mode** is enabled.

See [AI Chat Mode](ai-chat-mode.md) for sign-in, approvals, model selection, and chaos integration details.

### API Mode

`3270Connect` can also run as an API server using the `-api` and `-api-port` flags:

- `-api`: Run `3270Connect` as an API.
- `-api-port`: Specifies the port for the API (default is 8080).

To run `3270Connect` in API mode, use the following command:

```bash
3270Connect -api -api-port 8080
```

Once the API is running, you can send HTTP requests to it to trigger workflows and retrieve information.

POST:

```bash
http://localhost:8080/api/execute
```

Body:
```json
{
  "Host": "10.27.27.27",
  "Port": 3270,
  "CodePage": "cp037",
  "Token": "123456",
  "EveryStepDelay": { "Min": 0.1, "Max": 0.3 },
  "EndOfTaskDelay": { "Min": 30, "Max": 90 },
  "Steps": [
    {
      "Type": "Connect"
    },
    {
      "Type": "AsciiScreenGrab"
    },
    {
      "Type": "CheckValue",
      "Coordinates": {"Row": 1, "Column": 2, "Length": 11},
      "Text": "Some: VALUE"
    },
    {
      "Type": "FillString",
      "Coordinates": {"Row": 10, "Column": 44},
      "Text": "user1"
    },
    {
      "Type": "FillString",
      "Coordinates": {"Row": 11, "Column": 44},
      "Text": "mypass"
    },
    {
      "Type": "AsciiScreenGrab"
    },
    {
      "Type": "StepDelay",
      "StepDelay": { "Min": 1.0, "Max": 2.0 }
    },
    {
      "Type": "PressEnter"
    },
    {
      "Type": "AsciiScreenGrab"
    },
    {
      "Type": "Disconnect"
    }
  ]
}
```

The API responds with the rendered output in the JSON response body (it does not require an `OutputFilePath`).

- `Token` (optional): provide a one-time RSA token that will be injected wherever the workflow text contains `{{token}}`.
- `CodePage` (optional): host EBCDIC code page / character set for the session (for example `cp037`, `cp285`, or `cp278`/`finnish`). When omitted, the server falls back to the `-codePage` flag the API process was started with, and otherwise uses the emulator default. See [Host Code Page and Character Set](basic-usage.md#host-code-page-and-character-set).

!!! note

  The Start Process modal on the dashboard now includes a dedicated **RSA Token** field. Values supplied through the modal are forwarded to the API as the `Token` property, matching the `-token` flag used on the command line.

#### Requiring a credential

The API listener binds `localhost` and, by default, asks for nothing: one
operator on their own machine is already the person the request would be
attributed to. There are two ways to close it, and which one applies follows
from the deployment:

=== "One operator"

    Set `API_TOKEN`, and every request must present it:

    ```bash
    API_TOKEN=$(openssl rand -hex 32) 3270Connect -api -api-port 8080
    curl -H "Authorization: Bearer $API_TOKEN" -X POST http://localhost:8080/api/execute -d @workflow.json
    ```

=== "Several people"

    Set `AUTH_MODE=local` and issue a token per account. Each one reaches
    exactly what its owner reaches, is revocable on its own, and appears in the
    audit trail by name:

    ```bash
    3270Connect token add alice "ci pipeline"
    3270Connect token add watcher "grafana" --read-only
    ```

    `API_TOKEN` is refused alongside accounts — one credential held by
    everybody would be a hole straight through the separation the mode was
    turned on for — so unset it.

Either way, a request without a usable credential is answered `401` and
recorded. See [Accounts and Sign-In](authentication.md#api-tokens).

### API Mode with Docker

`3270Connect` can also run as an API server using the `-api` and `-api-port` flags:

- `-api`: Run `3270Connect` as an API.
- `-api-port`: Specifies the port for the API (default is 8080).

To run `3270Connect` in API mode, use the following command:

#### Linux
```bash
docker run --rm -p 8080:8080 3270io/3270connect-linux:latest -api -api-port 8080
```

#### Windows
```bash
docker run --rm -p 8080:8080 3270io/3270connect-windows:latest -api -api-port 8080
```

### API mode in practice

<figure class="demo-video">
  <video controls preload="metadata" playsinline
         poster="/assets/video/terminal-api-mode.jpg">
    <source src="/assets/video/terminal-api-mode.mp4" type="video/mp4">
    <a href="/assets/video/terminal-api-mode.mp4">Download the video</a>.
  </video>
  <figcaption>
    Starting the API server, posting a workflow to <code>/api/execute</code>, and
    reading the captured 3270 screen back out of the response.
  </figcaption>
</figure>

### Metrics & Monitoring

3270Connect can expose a Prometheus `/metrics` endpoint with histograms
for connect and step timing, a counter partitioned by workflow outcome,
and a live gauge of active workers. Enable it with `-promListen :9091`
and scrape with the sample config in
[Metrics & Monitoring](metrics.md).

### Host Compatibility Profiler

Run `3270Connect -profile -profileHost <host> -profilePort <port>` for a
one-shot probe that writes a `CompatibilityProfile` JSON document. The
document shares its schema with 3270Web's `POST /profile` endpoint, so
the same JSON drops into 3270Web's chaos mind-map compare workflow for
cross-environment diffing. See [Host Compatibility Profiler](host-profiler.md)
and [Compatibility Profile Schema](compatibility-profile-schema.md).
