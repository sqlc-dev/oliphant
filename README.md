# oliphant

A hand-written, pure Go (no cgo, no wasm) drop-in replacement for
[pganalyze/pg_query_go](https://github.com/pganalyze/pg_query_go): same
exported functions, same generated protobuf types, same JSON output, same
error type, same fingerprints. Consumers change one import path:

```diff
-import pg_query "github.com/pganalyze/pg_query_go/v6"
+import pg_query "github.com/sqlc-dev/oliphant"
```

Pinned to **libpg_query `17-6.2.2`** (PostgreSQL 17.7, patched) — the exact
build pg_query_go v6.2.2 ships. Every golden in `parser/testdata` derives
from that oracle, byte for byte, and is never edited by hand.

See [`PLAN.md`](PLAN.md) for the full design and [`CLAUDE.md`](CLAUDE.md)
for the development loop.

## Status

| Milestone | State |
|---|---|
| 1. Scaffolding + codegen | ✅ module, licenses, CI, vendored pin, `ast/` protobuf types, aliases, keyword tables, API surface, `parser.Error`; pg_query_go's parse_test tree literals compile unchanged |
| 2. Corpus + oracle | ✅ cgo oracle (`oracle/`), `cmd/regenerate`, 1,525 golden files / 217k cases across 8 suites, byte-reproducible; harness + `cmd/next-test` running everything as todo |
| 3. Lexer | — |
| 4. Expressions + SELECT | — |
| 5–7. DML, DDL, utility | — |
| 8. Normalize + fingerprint | — |
| 9. Deparse | — |
| 10–13. Summary, PL/pgSQL, hardening, sqlc integration | — |

Until the parser milestones land, every public entry point returns (or
panics with, for `HashXXH3_64`) a clear not-implemented error.
