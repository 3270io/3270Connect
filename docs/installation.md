# Installation

To use the go3270 command-line utility, you need to install it on your system. Follow the steps below based on your platform:

## Linux

```shell
$ curl -LO https://github.com/yourusername/go3270/releases/latest/download/go3270-linux-amd64
$ chmod +x go3270-linux-amd64
$ sudo mv go3270-linux-amd64 /usr/local/bin/go3270
```

## macOS

```shell
$ curl -LO https://github.com/yourusername/go3270/releases/latest/download/go3270-darwin-amd64
$ chmod +x go3270-darwin-amd64
$ sudo mv go3270-darwin-amd64 /usr/local/bin/go3270
```

### Windows

Download the latest go3270-windows-amd64.exe from the Releases page and add it to your system's PATH.

Once installed, you can verify the installation by running:

``` shell
Copy code
$ go3270 -help
```