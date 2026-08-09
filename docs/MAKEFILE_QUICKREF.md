---
description: >-
  The Makefile targets that build the x3270 binaries — s3270 and x3270if —
  for Linux and Windows, in one page.
---

# X3270 Makefile Quick Reference

## Quick Start

```bash
# Build Linux binaries (recommended)
make linux-only

# Show all available targets
make help

# Clean and rebuild
make clean && make linux-only
```

## All Targets

| Target | Description |
|--------|-------------|
| `all` | Build all binaries for Linux and Windows (default) |
| `linux-only` | Build and test only Linux binaries (recommended) |
| `linux` | Build Linux binaries only |
| `windows` | Build Windows binaries only |
| `test-binaries` | Test built binaries |
| `force` | Force rebuild ignoring version tracking |
| `clean` | Remove built binaries (safe, preserves source) |
| `deepclean` | Remove everything including cloned x3270 source |
| `help` | Show help message |

## Configuration

```bash
# Use a different version
make X3270_VERSION=4.3ga10 linux-only

# Check current version
make help
```

## Output Directories

- **Linux binaries:** `binaries/linux/`
  - s3270
  - x3270if

- **Windows binaries:** `binaries/windows/`
  - s3270.exe
  - x3270if.exe
  - ws3270.exe
  - wc3270.exe

## Version Tracking

The Makefile tracks the last built version in `.x3270_version`:
- Prevents redundant builds
- Use `make force` to rebuild the same version
- Use `make deepclean` for complete reset

## Common Workflows

### Build for the first time
```bash
make linux-only
```

### Update to a new version
```bash
make X3270_VERSION=4.4ga7 deepclean linux-only
```

### Rebuild without changing version
```bash
make force
```

### Build everything
```bash
make all
```

## Notes

- **Windows builds may fail** due to MinGW dependencies
- Use `linux-only` for most reliable results
- Build directory: `/tmp/x3270-build`
- Version file: `.x3270_version` (gitignored)

## See Also

- [MAKEFILE_README.md](MAKEFILE_README.md) - Full documentation
- [x3270 GitHub](https://github.com/pmattes/x3270) - Upstream project
