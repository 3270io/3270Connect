---
name: author-workflow
description: Write a workflow JSON document that drives a 3270 application, using the schema and the validator rather than guessing at the format.
invocation: [write-workflow, create-workflow, build-workflow]
tools: [describe_workflow_schema, validate_workflow, save_workflow, run_workflow_once]
instructions: [workflow-authoring]
---

# Writing a workflow

## When to use

The user wants a new workflow, or a change to one, and has described what it
should do rather than handing over the JSON.

## Get the schema first

Call `describe_workflow_schema`. Do not write a workflow from memory of what
3270 automation formats usually look like: this one's step keys are `Type`,
`Coordinates` and `Text`, and a shape using `Action`, `Value` and top-level
`Row`/`Column` was published in this repository's own notes for long enough
that it is worth assuming you have seen it. No workflow written that way has
ever run.

Coordinates are **1-based**. Row 1, Column 1 is the top-left cell.

## Build it

A workflow that drives an application usually looks like:

1. `Connect`
2. `CheckValue` against something the first screen always shows, so a
   workflow pointed at the wrong host fails immediately and says why rather
   than typing into whatever is in front of it.
3. `FillString` for each input, then `PressEnter` or the right PF key.
4. `CheckValue` on the result screen — this is what makes it a test rather
   than a script that presses keys.
5. `AsciiScreenGrab` where a captured screen would help someone reading the
   output.
6. `Disconnect`.

Use `{{placeholder}}` in `Text` for anything that varies per user, and say so
— those are filled from an injection file, one entry per virtual user, which
is what stops a hundred concurrent workers logging in as the same person.

## Verify before handing it over

1. `validate_workflow`. Fix what it reports and validate again. The validator
   is the same code the runner uses, so anything it accepts will at least
   start.
2. `run_workflow_once` against a real host if there is one to use. Validation
   proves the document is well-formed; only a run proves the screens are
   where the workflow thinks they are.
3. Read the captured output. A workflow can complete every step and still
   have done nothing useful if its `CheckValue` steps are too loose.

## Anti-patterns

- Writing the JSON and handing it over without validating.
- `CheckValue` on something that changes — a date, a session number, a queue
  depth. It will fail on the second run and look like a host problem.
- Hard-coding one person's credentials instead of using placeholders.
- Omitting `Disconnect`. Under concurrency, sessions that are never closed
  are how a load test exhausts a region rather than measuring it.
