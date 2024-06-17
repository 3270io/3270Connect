# Installation

To use the 3270Connect command-line utility, you need to install it on your system. Follow the steps below based on your platform:

## Linux

```shell
# Fetch the latest release URL
LATEST_URL=$(curl -s https://api.github.com/repos/3270io/3270Connect/releases/latest \
| jq -r '.assets[] | select(.name == "3270Connect") | .browser_download_url')

# Download the latest release
curl -LO $LATEST_URL

# Make it executable
chmod +x 3270Connect

# Move to a directory in PATH
sudo mv 3270Connect /usr/local/bin/3270Connect

```

## Windows

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