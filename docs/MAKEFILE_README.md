---
seo_title: "The x3270 binary build system used by 3270Connect"
description: >-
  The build system behind the embedded x3270 binaries: prerequisites,
  targets, output directories and version tracking, for both the Linux and
  the Windows build.
---

# X3270 Binary Build System

This Makefile automates the process of building x3270 suite binaries (s3270, x3270if, etc.) for Linux and Windows platforms.

## Prerequisites

### Linux
- GCC and build tools
- Git
- Make

### Windows Cross-Compilation
- MinGW-w64 cross-compiler (automatically installed if missing)
- The Makefile will install MinGW if needed using: `sudo apt-get install mingw-w64`

## Quick Start

Build all binaries for both platforms:
```bash
make
```

This will:
1. Clone the x3270 repository from https://github.com/pmattes/x3270
2. Checkout version 4.4ga6 (configurable)
3. Build Linux binaries (s3270, x3270if, x3270)
4. Build Windows binaries (s3270.exe, x3270if.exe, ws3270.exe, wc3270.exe)
5. Strip binaries to reduce size
6. Test all binaries
7. Save version info to `.x3270_version`

## Usage

### Build Specific Platforms

Build Linux binaries only:
```bash
make linux
```

Build Windows binaries only:
```bash
make windows
```

### Version Management

Specify a different x3270 version:
```bash
make X3270_VERSION=4.3ga10 all
```

The Makefile tracks the last built version in `.x3270_version`. If you try to build the same version again, it will skip the build unless you run `make deepclean` first.

### Testing

Test that all binaries are present and working:
```bash
make test-binaries
```

### Cleaning

Safe clean (removes built binaries, keeps source):
```bash
make clean
```

Deep clean (removes everything including cloned source):
```bash
make deepclean
```

Force rebuild of the same version:
```bash
make deepclean
make
```

### Help

Show all available targets and current configuration:
```bash
make help
```

## Directory Structure

```
binaries/
├── linux/          # Linux binaries
│   ├── s3270       # Scripting version
│   ├── x3270if     # Interface program
│   └── x3270       # Full version
└── windows/        # Windows binaries
    ├── s3270.exe   # Scripting version
    ├── x3270if.exe # Interface program
    ├── ws3270.exe  # Windows scripting version
    └── wc3270.exe  # Windows console version
```

## Build Process Details

### Linux Build
1. Clones x3270 repository to `/tmp/x3270-build`
2. Runs `./configure --prefix=/usr/local --enable-s3270 --enable-x3270if`
3. Builds using `make -j$(nproc)` for parallel compilation
4. Copies binaries to `binaries/linux/`
5. Strips binaries to reduce size
6. Sets executable permissions

### Windows Build
1. Checks for MinGW cross-compiler, installs if missing
2. Runs `./configure --host=x86_64-w64-mingw32 --prefix=/usr/local --enable-s3270 --enable-x3270if`
3. Builds using `make -j$(nproc)` for parallel compilation
4. Copies binaries to `binaries/windows/`
5. Strips binaries using MinGW strip tool

**Note:** Windows cross-compilation from Linux may fail due to missing dependencies (libiconv, OpenSSL for MinGW). If the build fails:
- Use the existing pre-built Windows binaries already in the repository
- Build natively on Windows using Visual Studio or MinGW
- Use the VisualStudio project files included in the x3270 repository

## Version Tracking

The Makefile maintains a `.x3270_version` file that records the last successfully built version. This prevents redundant rebuilds when you run `make` multiple times with the same version.

To check the current version:
```bash
make help
```

## Configuration Variables

- `X3270_VERSION`: x3270 version to build (default: 4.4ga6)
- `X3270_REPO`: Repository URL (default: https://github.com/pmattes/x3270)
- `X3270_DIR`: Build directory (default: /tmp/x3270-build)
- `BINARIES_LINUX`: Linux binaries output directory (default: binaries/linux)
- `BINARIES_WINDOWS`: Windows binaries output directory (default: binaries/windows)

## Troubleshooting

### Build Failures
If the build fails, check:
1. All prerequisites are installed
2. Internet connection is available for cloning
3. Disk space is sufficient in `/tmp`

Clean and retry:
```bash
make deepclean
make
```

### Windows Cross-Compilation Issues
Windows builds may fail on Linux due to missing MinGW dependencies:
- `libiconv` for MinGW
- `OpenSSL` for MinGW  
- Other Windows-specific libraries

**Solutions:**
1. Use the pre-built Windows binaries already in the repository
2. Build on Windows natively (recommended for Windows binaries)
3. Install additional MinGW libraries (if available for your distribution)

For native Windows builds, use:
- Visual Studio project files in the x3270 repository's `VisualStudio/` directory
- MinGW-w64 with MSYS2 on Windows
- Windows Subsystem for Linux (WSL) with native Windows toolchain

### Missing Binaries
If some binaries are missing after build, the x3270 version might not include all components. Check the x3270 release notes for the version you're building.

### Permission Issues
If binaries aren't executable:
```bash
chmod +x binaries/linux/*
chmod +x binaries/windows/*
```

## Integration with 3270Connect

After building binaries, regenerate the embedded assets:
```bash
go install github.com/go-bindata/go-bindata/...@latest
go-bindata -o binaries/bindata.go -pkg binaries ./binaries/...
```

Or use the PowerShell script:
```powershell
.\update-binaries.ps1
```

## Examples

### Building a Specific Version
```bash
# Build version 4.3ga10
make X3270_VERSION=4.3ga10 deepclean all

# Verify the version
./binaries/linux/s3270 -v
```

### Cross-Platform Build
```bash
# On Linux, build for both platforms
make all

# Verify Linux binaries
ls -lh binaries/linux/

# Verify Windows binaries
ls -lh binaries/windows/
```

### Continuous Integration
```bash
# In CI pipeline
make X3270_VERSION=4.4ga6 all
make test-binaries
```

## Notes

- The build directory (`/tmp/x3270-build`) is preserved between builds for faster subsequent builds
- Use `make clean` to remove binaries without removing the source
- Use `make deepclean` to remove everything and start fresh
- The Makefile uses color output for better visibility of build steps
- Parallel compilation is enabled by default using all available CPU cores

## License

This Makefile is part of the 3270Connect project and follows the same license (MIT).
