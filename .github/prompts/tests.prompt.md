# Test additions/updates for Go workflow engine

## When to use
- Logic changes in `go3270Connect.go` workflow execution or config validation.
- Emulator behavior changes under `connect3270/`.
- Bug fixes requiring regression coverage near existing `*_test.go` files.

## Inputs
- <goal>
- <scope>
- <files>
- <__filter_complete__></__filter_complete__>

## Prompt
Add or update tests for <goal> in <scope> using existing Go testing patterns. Prefer extending `go3270Connect_test.go` or `connect3270/emulator_test.go` before creating new test files. Keep tests deterministic (control RNG/time where applicable) and table-driven for config/step permutations. Run targeted tests first, then `go test -v ./...`. Limit changes to <files> and respect filter state: <__filter_complete__></__filter_complete__>.
