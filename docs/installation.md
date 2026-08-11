---
seo_title: "Install 3270Connect on Linux, Windows or Docker"
description: >-
  Install 3270Connect with one command, as a container running the operations
  console, as a Compose stack, or as a lab with a 3270 host to practise
  against — plus direct downloads for Linux and Windows.
---


# Installation

## One command

```shell
curl -fsSL https://3270connect.3270.io/install.sh | bash
```

The installer asks how you want it and gets out of the way. Four doors:

| | | |
|---|---|---|
| **Binary** | a single file on your PATH | s3270 is bundled — nothing else to install |
| **Docker** | one container, the operations console | `ghcr.io/3270io/3270connect` |
| **Compose** | a stack you can edit and re-`up` | writes `./3270connect/docker-compose.yml` |
| **Lab** | the console, a browser terminal and a 3270 host to aim at | three containers, no mainframe required |

Skip the questions by naming the method:

```shell
curl -fsSL https://3270connect.3270.io/install.sh | bash -s -- --method lab --yes
```

| Option | Default | Purpose |
|---|---|---|
| `--method` | ask | `binary`, `docker`, `compose` or `lab` |
| `--version` | `latest` | A release tag, e.g. `v2.0` |
| `--port` | `9200` | Host port for the console |
| `--bind` | `127.0.0.1` | Host interface to publish on |
| `--dir` | `./3270connect` | Compose project directory (`./3270-lab` for the lab) |
| `--system` / `--user` | user | Binary install to `/usr/local/bin` or `~/.local/bin` |
| `--dry-run` | off | Print what would happen, change nothing |
| `--yes` | off | Accept every prompt |

Re-running the installer updates an install that is already here rather than
replacing it: the port it is published on, the version it is pinned to and the
data folder holding its run history are all carried forward unless you pass
something different. It finds that install through Docker, not through the
directory you happen to be standing in, so running the one-line command a
second time from somewhere else does not strand the first one.

!!! warning "The console has no sign-in"
    Its start-process dialog launches a load run — against any host it is
    given — for whoever can open the page. That is why every method publishes
    on `127.0.0.1` by default. To reach it from another machine, forward the
    port over SSH rather than publishing it:

    ```shell
    ssh -L 9200:localhost:9200 user@runner-01
    ```

    If you do publish it, put an authenticating reverse proxy in front.

---

## The lab

The question after installing an automation tool is always "against what?".
The lab answers it: a TN3270 host, a browser terminal to explore it by hand,
and the console to replay what you did there.

```shell
curl -fsSL https://3270connect.3270.io/install.sh | bash -s -- --method lab
```

```
http://localhost:3270    the terminal. Connect to host "sampleapps", port 3271
http://localhost:9200    the console
```

The applications are [3270Web](https://3270Web.3270.io)'s bundled samples —
a pet store with a back office on 3271, and the name-entry example on 3272 —
served as an ordinary TN3270 host, so they negotiate, hold session state and
behave like the real thing. Nothing there is a mainframe and nothing has data
behind it, which is what makes it safe to point a load test at.

The stack ships with a workflow already aimed at it:

```shell
cd 3270-lab
docker compose run --rm 3270connect -config workflow-sampleapp.json -headless
```

The same three services are checked into both repositories as
`docker-compose.lab.yml`, if you would rather start from a file you can read
first.

---

## Docker

The image runs the operations console by default and takes any other set of
flags as its command, so one image covers both.

=== "The console"

    ```shell
    docker run -d --name 3270connect \
      -p 127.0.0.1:9200:9200 \
      -e DASHBOARD_BIND=0.0.0.0 \
      -v "$PWD/data":/data \
      ghcr.io/3270io/3270connect:latest
    ```

    Then open <http://localhost:9200/dashboard>.

=== "A workflow"

    ```shell
    docker run --rm \
      -v "$PWD/workflow.json":/data/workflow.json \
      -v "$PWD/data":/data \
      ghcr.io/3270io/3270connect:latest -config workflow.json -headless
    ```

=== "A load test"

    ```shell
    docker run --rm \
      -v "$PWD/data":/data \
      ghcr.io/3270io/3270connect:latest \
      -config workflow.json -headless -concurrent 25 -runtime 600
    ```

=== "A sample host"

    ```shell
    docker run --rm -p 3270:3270 \
      ghcr.io/3270io/3270connect:latest -runApp 1 -runApp-port 3270
    ```

!!! info "`DASHBOARD_BIND` and why the image sets it"
    The console binds `localhost`, which is right on a laptop and useless in a
    container: a published port forwards to the container's external
    interface, so a loopback listener refuses every connection from the host
    while the container still reports healthy. The image sets
    `DASHBOARD_BIND=0.0.0.0` for that reason. What the console is exposed to is
    decided by the port mapping, not by that variable.

!!! note "`/data` and who owns it"
    The container writes the metrics each run publishes, the console's logs and
    any workflow output to `/data`, and runs unprivileged as uid `10001`. A
    bind-mounted host folder keeps the host's ownership, so hand it over before
    the first start:

    ```shell
    mkdir -p ./data && sudo chown -R 10001:10001 ./data
    ```

    A named volume needs none of this — Docker sets its ownership from the
    image.

### Compose

`docker-compose.yml` in the repository runs the console on its own:

```shell
docker compose up -d
docker compose run --rm 3270connect -config workflow.json -headless
```

The installer writes an equivalent stack, pinned to a version and configured
with the port and data folder you chose.

!!! warning "linux/amd64 only"
    The s3270 the emulator drives is embedded in the binary as an x86-64
    executable and written out when a workflow first connects, so there is no
    arm64 image: it would start, serve the console, and fail the first run.
    On Apple silicon the image runs under emulation.

---

## Linux

### Direct download

```shell
# The latest release, straight from GitHub
curl -fsSLO https://github.com/3270io/3270Connect/releases/latest/download/3270Connect_linux

chmod +x 3270Connect_linux
sudo mv 3270Connect_linux /usr/local/bin/3270connect
```

The binary is self-contained: s3270 travels inside it and is written to the
temporary directory the first time a workflow connects. Run history goes to
`~/.config/3270Connect/`, which is where the console reads it from.

```shell
3270connect -dashboard                       # the console on :9200
3270connect -config workflow.json            # one run
3270connect -runApp 1 -runApp-port 3270      # a 3270 host to aim at
```

## Windows

### Direct download

```powershell
$latest = "https://github.com/3270io/3270Connect/releases/latest/download/3270Connect.exe"
Invoke-WebRequest -Uri $latest -OutFile 3270Connect.exe

# Optionally, move it somewhere on PATH (requires admin)
# Move-Item -Path 3270Connect.exe -Destination "C:\Program Files\3270Connect.exe"
```

Double-clicking `3270Connect.exe` with no arguments starts the console and
opens it in a window.

### Docker

A Windows container image is built from `Dockerfile.windows` and is not
published: a Windows image cannot be built on a Linux runner. Build it
yourself where you need it:

```powershell
docker build -f Dockerfile.windows -t 3270connect-windows .
```

---

## Building from source

```shell
git clone https://github.com/3270io/3270Connect.git
cd 3270Connect
./build.sh          # binaries into dist/
go test ./...
```

Go 1.25 or newer. The x3270 binaries embedded in the build are produced
separately; see [BUILD.md](https://github.com/3270io/3270Connect/blob/main/BUILD.md).
