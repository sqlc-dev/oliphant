# srcdata — vendored generator inputs

Everything in this directory is vendored **verbatim** from
[pganalyze/libpg_query](https://github.com/pganalyze/libpg_query) at tag
**`18.0.0`** (PostgreSQL 18.4). Do not edit by hand; re-vendor when the pin
advances (see `PLAN.md` § Regeneration).

| File | Upstream path | Consumed by |
|---|---|---|
| `pg_query.proto` | `protobuf/pg_query.proto` | `cmd/generate` → `ast/pg_query.pb.go` |
| `struct_defs.json` | `srcdata/struct_defs.json` | `cmd/generate` (JSON emitter, fingerprint walk) |
| `enum_defs.json` | `srcdata/enum_defs.json` | `cmd/generate` |
| `nodetypes.json` | `srcdata/nodetypes.json` | `cmd/generate` |
| `typedefs.json` | `srcdata/typedefs.json` | `cmd/generate` |
| `all_known_enums.json` | `srcdata/all_known_enums.json` | `cmd/generate` |

The keyword table generator reads `internal/reference/kwlist.h` (same pin).
