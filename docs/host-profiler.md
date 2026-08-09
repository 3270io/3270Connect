---
description: >-
  The -profile mode connects once to a TN3270 host and writes a
  CompatibilityProfile: negotiated terminal model, protocol options,
  capabilities and timing.
---

# Host Compatibility Profiler

The `-profile` mode connects once to a TN3270 host, probes its
negotiated terminal model, protocol options, and discovered
capabilities, and writes a `CompatibilityProfile` JSON document. The
schema is shared byte-for-byte with 3270Web, so profiles produced
by either tool can be compared side-by-side (for example, to spot
divergence between an IBM z/OS image and a stripped-down Rocket
Enterprise Server stand-in).

See [Compatibility Profile Schema](compatibility-profile-schema.md) for
the full field reference.

## Quick start

```bash
3270Connect -profile -profileHost mvs01.example.com -profilePort 992 -profileTLS \
            -profileOut mvs01.profile.json
```

The probe connects, runs through s3270 `Query` requests against the host,
captures the first screen's banner signature, records timing, and writes
the JSON to `-profileOut` (or stdout when omitted). The process exits
non-zero on failure so CI can fail fast.

## Flags

| Flag | Description |
|---|---|
| `-profile` | Enables one-shot profiler mode. Replaces the workflow runner for this invocation. |
| `-profileHost <host>` | Host to profile. If omitted, falls back to `Host` in the workflow config. |
| `-profilePort <port>` | Port to profile. If omitted, falls back to `Port` in the workflow config. |
| `-profileTLS` | Mark the profiled host as TLS-protected in the output (`host.tls: true`). |
| `-profileOut <path>` | Write the JSON to this path. When omitted, the document is emitted on stdout. |
| `-profileCollectRaw` | Include raw s3270 `Query` responses in the `raw` block of the JSON. Useful for debugging parser drift against new s3270 versions. |

The probe uses a 30-second total timeout for capability discovery and
the standard 5-second TCP dial timeout for the initial connection.

## Reusing a workflow config

You can omit `-profileHost`/`-profilePort` and let the profiler pull
them from `-config`:

```bash
3270Connect -profile -config workflow.json -profileOut current.profile.json
```

This makes it easy to drop `-profile` into an existing pipeline without
duplicating connection metadata.

## Example output

```json
{
  "schema_version": "1.0.0",
  "run_id": "8f3b2c1a...",
  "timestamp": "2026-05-18T10:11:12Z",
  "profiler": { "tool": "3270Connect", "version": "1.8.6" },
  "host": {
    "host": "mvs01.example.com",
    "port": 992,
    "tls": true,
    "banner_signature": "9b6f...",
    "banner_preview": "WELCOME TO MVS01 ..."
  },
  "device": {
    "terminal_type": "IBM-3279-2-E",
    "rows": 24,
    "cols": 80,
    "color": true,
    "extended_attributes": true,
    "alt_screen": false
  },
  "protocol": { "tn3270e": true, "bind_image_present": true, "structured_fields": true },
  "capabilities": { "ind_file": "unknown", "query_reply_ids": ["..."], "unknown": [] },
  "timing": { "connect_ms": 142, "first_screen_ms": 38, "keyboard_unlock_ms": 38 }
}
```

## Cross-environment comparison

Two profiles whose
`(device.terminal_type, host.banner_signature, protocol.tn3270e, protocol.structured_fields)`
tuples match are compatible at the wire level. Differences in
`capabilities.unknown` highlight optional-feature gaps (e.g.
IND$FILE on z/OS but not on a Rocket Enterprise Server image).

3270Web ships a complementary
**Chaos Mind-Map Compare** endpoint that visualises these differences
across hosts — feed both profiles to its `POST /chaos/mindmap/compare`
to get a divergence report.

## When to use which

- **`3270Connect -profile`** — CI/CD or shell-friendly one-shot probe.
  No web UI needed; perfect for nightly fleet sweeps.
- **3270Web `POST /profile`** — interactive use from the web UI or
  programmatic use from automation that already talks to 3270Web.

Both produce identical JSON; pick whichever fits the surrounding
pipeline.
