# oliphant development guide

Pure Go port of pg_query_go. Read `PLAN.md` first — it is the contract.
Pin: libpg_query **17-6.2.2** (PostgreSQL 17.7, patched). No cgo anywhere in
the main module; the only module allowed to link pg_query_go (cgo) is
`oracle/`, and only `cmd/regenerate`/`cmd/difftest` ever run it.

## The development loop

1. `go run ./cmd/next-test` — picks the next `todo` case from
   `parser/testdata/*/`.
2. Implement the production in `internal/parse/` (or lexer rule in
   `internal/lexer/`), with an attribution comment naming the `gram.y` /
   `scan.l` rule (`// gram.y: simple_select`).
3. `go test ./parser -run TestCorpus -check-parse` — runs the `todo` cases;
   newly passing cases are removed from the `metadata.json` sidecars
   automatically. Commit the sidecar diff with the code.
4. `go test ./...` must stay green; a case that regresses from passing to
   failing is a hard failure, never a new todo.

## Hard rules

- **Expected outputs are never edited by hand.** Goldens come from
  `cmd/regenerate` (the pinned oracle) or not at all.
- Generated files (`ast/pg_query.pb.go`, `aliases.go`,
  `internal/lexer/keywords.go`) are only ever written by `cmd/generate`.
- The public API mirrors pg_query_go exactly; consumers must migrate by
  changing one import path. Never rename, add, or remove exported symbols
  without checking pg_query_go v6.2.2.
- Location fields must byte-match the reference; when in doubt, check which
  `@N` the `gram.y` action uses.

## Layout crib

- `internal/reference/` — pinned `gram.y`, `scan.l`, `parser.c`, `kwlist.h`
  (docs only). The `base_yylex` token-merge filter is in `parser.c`.
- `srcdata/` — vendored libpg_query metadata JSON + `pg_query.proto`
  (generator inputs).
- `parser/testdata/<suite>/*.test` — corpus: cases split by `==`, input and
  expectation split by `--`. `metadata.json` sidecar per file tracks `todo`.
- `oracle/` — separate go module, cgo, never imported by the main module.

## Regenerating

- `go run ./cmd/generate -aliases -keywords` — pure Go, always safe.
- `go run ./cmd/generate -proto` — requires `protoc` + `protoc-gen-go`.
- `go run ./cmd/regenerate -libpg-query <checkout> -pg-query-go <checkout>` —
  builds the cgo oracle, rewrites `parser/testdata` from scratch. Output must
  be byte-identical run-to-run.
