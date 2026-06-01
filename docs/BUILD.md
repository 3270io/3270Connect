# Build Instructions

Ensure MinGW-w64 with an updated Windows SDK is installed.

Use github.com/pkg/browser instead of webview. For example:

go get github.com/pkg/browser
go build -o 3270Connect.exe .

go build -ldflags="-H=windowsgui" -o 3270Connect.exe .

Alternatively, build on Windows where the Windows SDK is present.
