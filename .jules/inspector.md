## 2026-10-09 - Injection numbers went through float64
**Finding:** `loadInjectionData` decoded bare JSON numbers as float64, so `12345678901` was typed as `1.2345678901e+10` and `1.50` as `1.5`.
**Learning:** Any value that is typed onto a host screen must keep the digits as written; decode with `UseNumber`, never via float64.
**Prevention:** Keep `TestLoadInjectionDataKeepsNumbersAsWritten` passing; check other JSON ingest paths that feed `Text`.
