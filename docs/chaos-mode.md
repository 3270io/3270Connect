# Chaos Mode

Chaos Mode explores a live 3270 session automatically, records discovered screens and transitions, and helps you turn what it learns into repeatable 3270Connect workflows.

You can control a chaos run from either the 3270Web toolbar or AI Chat mode. Both surfaces operate on the same run state, so you can start with toolbar controls, inspect status in chat, then export a workflow without losing progress.

## Toolbar Controls

The toolbar exposes the core exploration lifecycle:

- Start a run with configurable time or step limits.
- Stop an active run without discarding the collected graph.
- Resume a stopped or previously loaded run.
- Review progress, unique screens, and transitions as the exploration advances.
- Export discovered paths as 3270Connect-compatible JSON.

## Discovery Report

The discovery report summarizes what the explorer has learned so far. It includes:

- A markdown report of visited screens and transitions.
- An ASCII screen graph for quick review in text-based workflows.
- Per-screen statistics to highlight heavily visited or dead-end states.
- Suggested next experiments based on the current graph and saved hints.

## Running Chaos via AI Chat

AI Chat mode can drive the same exploration tools conversationally.

| From AI Chat | Equivalent tool | Result |
| --- | --- | --- |
| Start exploration | `chaos_start` | Starts a new automated exploration run |
| Stop exploration | `chaos_stop` | Pauses the active run |
| Resume exploration | `chaos_resume` | Continues a stopped or loaded run |
| Check progress | `chaos_status` | Returns steps, transitions, and unique screens |
| Generate a report | `chaos_report` | Produces the discovery report in markdown |
| Save a hint | `chaos_save_screen_hint` / `chaos_update_hints` | Guides future exploration with known values or key assignments |
| Export the workflow | `chaos_export_workflow` | Downloads a JSON workflow based on learned paths |

The default AI Chat prompt follows a five-phase loop: read the screen, review hints, ask you which run mode to use, start exploration, then export the results. You can replace that flow with your own instructions whenever you need a different exploration strategy.

## Hints and Safe Operation

Hints improve coverage by teaching the explorer about known transaction codes, data values, and key assignments. Whether you launch exploration from the toolbar or AI Chat, approve writes carefully when the system proposes field updates or key presses against a live host.