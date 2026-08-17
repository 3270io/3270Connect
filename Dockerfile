# syntax=docker/dockerfile:1

# 3270Connect — https://3270connect.3270.io
#
# The Linux image. It starts the operations console by default, and takes any
# other set of flags as the command, so the same image runs a load test in CI:
#
#   docker run --rm -p 9200:9200 ghcr.io/3270io/3270connect          # console
#   docker run --rm -v "$PWD/workflow.json":/data/workflow.json \
#     ghcr.io/3270io/3270connect -config workflow.json -headless     # a run
#
# The Windows image is built from Dockerfile.windows.

# Built on the builder's own architecture and cross-compiled, so an arm64
# builder does not pay for a QEMU-emulated compile. CGO is off, which makes the
# result a static binary with nothing to link against at runtime.
FROM --platform=$BUILDPLATFORM golang:1.25-bookworm AS build
WORKDIR /src

ENV CGO_ENABLED=0
ENV GOTOOLCHAIN=auto

COPY go.mod go.sum ./
RUN go mod download

COPY . ./
RUN GOOS=linux GOARCH=amd64 go build -trimpath -ldflags "-s -w" -o /out/3270Connect .

# linux/amd64 and no other architecture, deliberately.
#
# The s3270 this drives is embedded in the binary as an x86-64 ELF and written
# out to the temp directory the first time a workflow connects. An arm64 image
# would therefore build, start, serve the console — and fail the first run it
# was asked to make, which is a worse outcome than not publishing one. Pinning
# the platform here means a `docker build` on an arm64 laptop produces the
# image that works rather than the image that looks like it should.
FROM --platform=linux/amd64 public.ecr.aws/ubuntu/ubuntu:24.04 AS runtime

# What that extracted s3270 needs to run: glibc, and OpenSSL 3 for TLS to a
# host. The library package is pulled in through `openssl` rather than named
# directly because its name is not stable across releases — libssl3 on one,
# libssl3t64 on the next — while `openssl`'s dependency on the right one is.
# curl is here for the HEALTHCHECK below.
RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates curl openssl \
    && rm -rf /var/lib/apt/lists/* \
    && groupadd --gid 10001 app \
    && useradd --uid 10001 --gid 10001 --no-create-home --home-dir /data app

COPY --from=build /out/3270Connect /usr/local/bin/3270Connect

# The licences travel with the program. MIT asks that its notice go with every
# copy, and the embedded s3270's BSD 3-Clause asks the same of a binary
# redistribution — which an image containing that binary is. Someone who only
# ever pulls the image would otherwise never see either.
COPY LICENSE THIRD-PARTY-LICENSES.md /usr/share/doc/3270Connect/

# State goes somewhere the image does not, because the image is replaced on
# every deploy. The metrics files every run publishes, the console's logs and
# whatever a workflow writes all land here; the image holds only the program.
#
# XDG_CONFIG_HOME is what puts the metrics in it: they go to the user config
# directory, and a container has no home directory to derive one from — without
# this the console would fall back to a folder beside the program and lose
# sight of runs started by any other process.
ENV XDG_CONFIG_HOME=/data
WORKDIR /data
RUN chown -R app:app /data

# Declared so an instance started without an explicit volume still keeps its
# runs across a container replacement. Name it in compose (see
# docker-compose.yml) to keep track of which volume is which.
VOLUME ["/data"]

USER app

# Listen on all interfaces *inside* the container. A published port forwards to
# the container's external interface, so the localhost default — which is the
# right default everywhere else, because the console has no sign-in — would
# refuse every connection from the host while the container still reported
# healthy. What the console is exposed to is decided by the port mapping, not
# by this line.
ENV DASHBOARD_BIND=0.0.0.0

EXPOSE 9200

HEALTHCHECK --interval=30s --timeout=3s --start-period=10s --retries=3 \
    CMD curl -fsS http://127.0.0.1:9200/healthz || exit 1

ENTRYPOINT ["/usr/local/bin/3270Connect"]

# Overridden by anything passed after the image name, so `docker run <image>
# -config workflow.json` runs a workflow instead of the console.
CMD ["-dashboard"]
