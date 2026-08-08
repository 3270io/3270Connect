# syntax=docker/dockerfile:1.2

#############################
# Builder for Linux
#############################
FROM golang:1.25 AS builder-linux

# Set environment variables for Linux
ENV GOARCH=amd64
ENV GOOS=linux

WORKDIR /app

# Copy Go module files first (cache optimization)
COPY go.mod go.sum ./
RUN go mod download

# Copy the Go source code
COPY . .

# Build the Linux binary
RUN go build -o 3270Connect-linux .

#############################
# Builder for Windows
#############################
FROM golang:1.25 AS builder-windows

# Set environment variables for Windows
ENV GOARCH=amd64
ENV GOOS=windows

WORKDIR /app

# Copy Go module files first (cache optimization)
COPY go.mod go.sum ./
RUN go mod download

# Copy the Go source code
COPY . .

# Build the Windows binary using CMD
RUN cmd /C "go build -o 3270Connect.exe ."

#############################
# Final stage for Windows
#############################
FROM mcr.microsoft.com/windows/servercore:ltsc2019 AS final-windows

# Copy the Windows binary from the builder
COPY --from=builder-windows /app/3270Connect.exe C:\3270Connect.exe

# Define the entrypoint for the Windows container
ENTRYPOINT ["C:\\3270Connect.exe"]

#############################
# Final stage for Linux
#############################
FROM alpine:latest AS final-linux

# Copy the Linux binary from the builder
COPY --from=builder-linux /app/3270Connect-linux /usr/local/bin/3270Connect

# Make the binary executable
RUN chmod +x /usr/local/bin/3270Connect

# Define the entrypoint for the Linux container
ENTRYPOINT ["/usr/local/bin/3270Connect"]
