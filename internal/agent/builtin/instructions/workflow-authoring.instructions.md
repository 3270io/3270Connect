# Writing workflow JSON

## The step keys

`Type`, `Coordinates` and `Text`. Not `Action`, not `Value`, not top-level
`Row` and `Column` — that shape appeared in this repository's own notes and no
workflow written that way has ever run. Call `describe_workflow_schema` rather
than working from memory of what 3270 automation formats usually look like.

Coordinates are **1-based**: the top-left cell is Row 1, Column 1.

```json
{ "Type": "FillString", "Coordinates": { "Row": 10, "Column": 20, "Length": 8 }, "Text": "{{username}}" }
```

## Check before you type

A `CheckValue` against something the first screen always shows is what turns a
workflow from a sequence of keystrokes into something that fails safely. Point
a workflow with no opening check at the wrong host and it types an account
number into whatever field happens to be under the cursor.

Check the result screen too. A workflow with no check after the transaction
can complete every step against a host that returned an error and report
success.

Check something stable. A date, a session number or a queue depth will fail on
the second run and look like a host problem.

## Values that vary

Use `{{placeholder}}` in `Text` and supply an injection file: an array of
objects, one per virtual user, mapping each placeholder to that user's value.

Under concurrency this matters more than it looks. A hundred workers sharing
one login usually measures the host's handling of a contended session rather
than the transaction, and on some applications it simply fails. Each entry is
handed to one worker at a time for the same reason.

`{{token}}` is separate: it is substituted from `-token` or the environment,
so a one-time password stays out of the workflow file.

## Pacing

Real users pause. `EveryStepDelay` inserts a randomised gap between steps and
`EndOfTaskDelay` between repeats, both taking `Min` and `Max`.

Without them a workflow runs as fast as the host will answer, which measures
maximum throughput rather than behaviour under a realistic arrival pattern.
Both are legitimate tests; they answer different questions, and a run without
pacing should be described as what it is.

## Always disconnect

End with `Disconnect`. Sessions that are never closed accumulate for the life
of the run, and under concurrency that is how a load test exhausts a region
rather than measuring it — a failure that looks like a capacity limit and is
the workflow's fault.

## Removed settings

The top-level `Delay` and the `HumanDelay` step no longer exist; use
`EveryStepDelay` and a `StepDelay` step. The validator rejects both by name
and says what replaced them, so a workflow that predates the change fails with
an explanation rather than being silently ignored.
