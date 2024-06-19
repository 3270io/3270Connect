
# Installation

To use the 3270Connect command-line utility, you need to install it on your system. Follow the steps below based on your platform:

## Linux

### Direct Installation

```shell
# Fetch the latest release URL
LATEST_URL=$(curl -s https://api.github.com/repos/3270io/3270Connect/releases/latest | jq -r '.assets[] | select(.name == "3270Connect") | .browser_download_url')

# Download the latest release
curl -LO $LATEST_URL

# Make it executable
chmod +x 3270Connect

# Move to a directory in PATH
sudo mv 3270Connect /usr/local/bin/3270Connect
```

### Docker Installation

```shell
# Pull the Docker image
docker pull 3270io/3270connect-linux:latest

# Run the Docker container
docker run --rm -it 3270io/3270connect-linux:latest
```

#### Additional Docker Run Examples (Linux)

Run the container with a configuration file and expose the port:

```shell
docker run -it -v $(pwd)/workflow.json:/app/workflow.json -v $(pwd)/output.html:/app/output.html -p 3270:3270 3270io/3270connect-linux:latest -config /app/workflow.json
```

Run in headless mode:

```shell
docker run -it -v $(pwd)/workflow.json:/app/workflow.json -v $(pwd)/output.html:/app/output.html -p 3270:3270 3270io/3270connect-linux:latest -config /app/workflow.json -headless
```

Run in verbose mode:

```shell
docker run -it -v $(pwd)/workflow.json:/app/workflow.json -v $(pwd)/output.html:/app/output.html -p 3270:3270 3270io/3270connect-linux:latest -config /app/workflow.json -verbose
```

Run multiple workflows concurrently:

```shell
docker run -it -v $(pwd)/workflow.json:/app/workflow.json -v $(pwd)/output.html:/app/output.html -p 3270:3270 3270io/3270connect-linux:latest -config /app/workflow.json -concurrent 2 -runtime 60
```

Run a test 3270 sample application:

```shell
docker run -it -p 3270:3270 3270io/3270connect-linux:latest -runApp
```

Run a specific test 3270 sample application:

```shell
docker run -it -p 3270:3270 3270io/3270connect-linux:latest -runApp [number]
```

## Windows

### Direct Installation

```shell
# Fetch the latest release URL
$latest_url = Invoke-RestMethod -Uri https://api.github.com/repos/3270io/3270Connect/releases/latest | `
    Select-Object -ExpandProperty assets | `
    Where-Object { $_.name -eq "3270Connect.exe" } | `
    Select-Object -ExpandProperty browser_download_url

# Download the latest release
Invoke-WebRequest -Uri $latest_url -OutFile 3270Connect.exe

# Optionally, move to a directory in PATH (requires admin privileges)
# Move-Item -Path 3270Connect.exe -Destination "C:\Program Files\3270Connect.exe"
```

### Docker Installation

```shell
# Pull the Docker image
docker pull 3270io/3270connect-windows:latest

# Run the Docker container
docker run --rm -it 3270io/3270connect-windows:latest
```

#### Additional Docker Run Examples (Windows)

Run the container with a configuration file and expose the port:

```shell
docker run -it -v ${PWD}/workflow.json:/app/workflow.json -v ${PWD}/output.html:/app/output.html -p 3270:3270 3270io/3270connect-windows:latest -config /app/workflow.json
```

Run in headless mode:

```shell
docker run -it -v ${PWD}/workflow.json:/app/workflow.json -v ${PWD}/output.html:/app/output.html -p 3270:3270 3270io/3270connect-windows:latest -config /app/workflow.json -headless
```

Run in verbose mode:

```shell
docker run -it -v ${PWD}/workflow.json:/app/workflow.json -v ${PWD}/output.html:/app/output.html -p 3270:3270 3270io/3270connect-windows:latest -config /app/workflow.json -verbose
```

Run multiple workflows concurrently:

```shell
docker run -it -v ${PWD}/workflow.json:/app/workflow.json -v ${PWD}/output.html:/app/output.html -p 3270:3270 3270io/3270connect-windows:latest -config /app/workflow.json -concurrent 2 -runtime 60
```

Run a test 3270 sample application:

```shell
docker run -it -p 3270:3270 3270io/3270connect-windows:latest -runApp
```

Run a specific test 3270 sample application:

```shell
docker run -it -p 3270:3270 3270io/3270connect-windows:latest -runApp [number]
```
