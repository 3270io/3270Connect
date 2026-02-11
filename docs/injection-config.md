# Dynamic Field Injection 

## Overview

The injection configuration feature in `3270Connect` allows users to dynamically inject data into workflows. This is particularly useful for scenarios where workflows need to be parameterized or customized based on external inputs.

## Configuration Format

The injection configuration is defined in a JSON file. Below is an example structure:

```json
[
    {
      "{{firstname}}": "user1-firstname",
      "{{lastname}}": "user1-lastname"
    },
    {
      "{{firstname}}": "user2-firstname",
      "{{lastname}}": "user2-lastname"
    }
]
```

The workflow configuration is then define to use the key names of the injection configuration file. Below is an example structure:

```json
  "Steps": [
    {
      "Type": "FillString",
      "Coordinates": {"Row": 5, "Column": 21},
      "Text": "{{firstname}}"
    },
    {
      "Type": "FillString",
      "Coordinates": {"Row": 6, "Column": 21},
      "Text": "{{lastname}}"
    }
  ]
```

## Usage

To use an injection configuration file, pass it as a parameter when running `3270Connect`:

```bash
3270Connect -config workflow.json -injectionConfig injection.json
```

## Example

Here is an example of running a workflow with an injection configuration:

```bash
3270Connect -config workflow.json -injectionConfig injection.json
```

This will replace the specified fields in the workflow with the values provided in the injection configuration.

## Concurrent Workflow Locking

When running with `-concurrent` and `-runtime` while using `-injectionConfig`, each injection entry is locked while a workflow is actively using it. This prevents a second active workflow from reusing the same injection values at the same time.

Behavior details:

- An injection entry is locked at workflow scheduling/start time and released when that workflow completes (or is skipped before execution).
- If all injection entries are currently locked and in use, the workflow start for that slot is skipped for the current scheduling cycle.
- Processing continues normally on the next cycle.

## Warning Messages

`3270Connect` emits `WARNING` terminal messages (same terminal log style and layout as existing warnings) in two cases:

- Startup mapping warning: if loaded injection entries are fewer than requested concurrency, for example `2` entries with `-concurrent 5`.
- Runtime lock warning: when all injection entries are locked/in use at a scheduling moment, so one or more workflow starts are skipped for that cycle.

## Conclusion

The injection configuration feature enhances the flexibility of `3270Connect` by allowing workflows to be dynamically customized. This is especially useful for testing and automation scenarios where inputs vary across runs.
