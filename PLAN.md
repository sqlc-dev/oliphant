# oliphant — a pure Go port of pg_query_go

*(an oliphaunt, for the elephant that is PostgreSQL's grammar)*

oliphant is a hand-written, pure Go (no cgo, no wasm) implementation of
[pganalyze/pg_query_go](https://github.com/pganalyze/pg_query_go), in the same
family as:

- [sqlc-dev/zetajones](https://github.com/sqlc-dev/zetajones) — GoogleSQL (BigQuery)
- [sqlc-dev/meyer](https://github.com/sqlc-dev/meyer) — SQLite
- [sqlc-dev/teesql](https://github.com/sqlc-dev/teesql) — T-SQL (SQL Server)
- [sqlc-dev/doubleclick](https://github.com/sqlc-dev/doubleclick) — ClickHouse
- [sqlc-dev/marino](https://github.com/sqlc-dev/marino) — MySQL (TiDB fork, goyacc)

Its immediate purpose is to replace the `pganalyze/pg_query_go/v6` +
`wasilibs/go-pgquery` build-tag pair in
[`sqlc/internal/engine/postgresql`](https://github.com/sqlc-dev/sqlc/tree/main/internal/engine/postgresql)
— the last sqlc engine still backed by a foreign parser (C via cgo, wasm via
wazero everywhere else). PostgreSQL is the only dialect where sqlc pays a cgo
tax, a wasm tax on Windows, and a multi-minute first-build tax (pg_query_go
compiles ~100 vendored C files, `gram.c` alone is 2.7 MB).

**The API does not change.** oliphant is a drop-in replacement for
`pg_query_go`: same exported functions, same generated protobuf types, same
JSON output, same error type, same fingerprints. Consumers change an import
path and delete a build constraint. sqlc's 92 KB `convert.go` — written
against pg_query's protobuf node shapes — must work unmodified after the
import swap.

## Reference implementation and pin

The reference is [pganalyze/libpg_query](https://github.com/pganalyze/libpg_query),
which vendors ~220,000 lines of extracted PostgreSQL C (the real
`gram.c`/`scan.c` generated from `gram.y`/`scan.l`, plus node infrastructure)
behind a small hand-written API layer, with **11 patches** applied to upstream
PostgreSQL before extraction.

Initial pin: **libpg_query `17-6.2.2` (PostgreSQL 17.7)** — the exact tag
pg_query_go v6.2.2 ships today and the version sqlc builds against
(`ParseResult.Version == 170007`). libpg_query's main branch is already on
PostgreSQL 18.4; the plan treats "advance the pin" as a first-class workflow
(§ Regeneration) rather than targeting a moving version now. When pg_query_go
cuts a PG 18 major, oliphant re-pins and re-derives its goldens.

Two consequences of the patches matter and are easy to miss:

- oliphant ports **patched** PostgreSQL, not vanilla: `$1` parameter
  references are legal in more grammar positions (patch 01, what makes
  normalized queries re-parseable), `param_junk` is removed from the scanner
  (09), the lexer tracks token end positions (03) and comments as tokens
  (04), and `LIMIT_OPTION_DEFAULT` is reordered for proto3 zero-value
  semantics (05).
- The oracle for every golden output is the **pinned libpg_query build**,
  never upstream PostgreSQL.

## What carries over from the sibling parsers

Adopted unchanged from zetajones/meyer/doubleclick:

1. **Hand-written recursive descent.** No parser generators, no grammar files
   feeding tools. One `parse*` method per production, each carrying an
   attribution comment naming the `gram.y` rule it implements
   (`simple_select:`, `a_expr:`, `opt_sort_clause:`, …). The pinned `gram.y`
   and `scan.l` (post-patch) are vendored under `internal/reference/` for
   documentation only.
2. **Corpus-driven development loop.** Vendored corpus in consolidated
   `.test` files (cases separated by `==`, input/expectation by `--`),
   per-file `metadata.json` sidecars with `todo` tracking, `cmd/next-test` to
   pick the next failing case, `go test ./parser -check-parse` to harvest
   newly passing cases, and the hard rule that **expected outputs are never
   edited by hand** — they come from the oracle or not at all.
3. **Eager lexer.** Whole-input tokenization up front; tokens carry byte
   offsets. (Here the token stream is itself public API — see `Scan`.)
4. **Fail-fast, single-error reporting**, exactly like the reference (bison
   aborts at first syntax error).
5. **CI**: GitHub Actions, `go build ./...` + `go test -race ./...`, no cgo
   anywhere in the module.

## How oliphant differs from the sibling parsers

### 1. The public API is pg_query_go's, not the family's

The siblings expose `parser.Parse(ctx, io.Reader) ([]ast.Stmt, error)`.
oliphant instead reproduces pg_query_go's root package verbatim:

```go
package oliphant // module github.com/sqlc-dev/oliphant

func Parse(input string) (*ParseResult, error)
func ParseToJSON(input string) (string, error)
func Scan(input string) (*ScanResult, error)
func Deparse(tree *ParseResult) (string, error)
func Normalize(input string) (string, error)
func NormalizeUtility(input string) (string, error)
func Fingerprint(input string) (string, error)
func FingerprintToUInt64(input string) (uint64, error)
func HashXXH3_64(input []byte, seed uint64) uint64
func SplitWithScanner(input string, trimSpace bool) ([]string, error)
func SplitWithParser(input string, trimSpace bool) ([]string, error)
func IsUtilityStmt(input string) ([]bool, error)
func ParsePlPgSqlToJSON(input string) (string, error)
func Summary(input string, truncateLimit int) (*SummaryResult, error)
```

plus the 28 `Make*` AST constructors from `makefuncs.go` (ported verbatim —
they are already pure Go) and the `parser` subpackage's `Error` type:

```go
type Error struct {
    Message   string
    Funcname  string
    Filename  string
    Lineno    int
    Cursorpos int
    Context   string
}
```

The root package is named `oliphant`; consumers alias it at the import site,
which is what pg_query_go users already do, so migration is exactly one line
and no call site changes:

```diff
-import pg_query "github.com/pganalyze/pg_query_go/v6"
+import pg_query "github.com/sqlc-dev/oliphant"
```

The `parser` subpackage's byte-level entry points (`ParseToProtobuf`,
`DeparseFromProtobuf`, `ScanToProtobuf`, …) are kept for compatibility, as
thin `proto.Marshal`/`Unmarshal` wrappers — they exist upstream only because
cgo speaks bytes; here they are conveniences.

### 2. The AST is the generated protobuf types — one dependency, not zero

The siblings' "zero dependencies, ever" rule bends once: pg_query_go's API
returns `*ParseResult` and friends generated by `protoc-gen-go`, and
`google.golang.org/protobuf` is load-bearing for API parity (users call
`proto.Marshal` on parse results today). `go.mod` therefore carries exactly
one runtime dependency: `google.golang.org/protobuf`. Nothing else, ever —
XXH3 is implemented in-house (§ Fingerprinting) rather than imported.

Layout implication: the generated types cannot live in the root package
(the internal parser must build them, and root calls the parser — that would
be an import cycle). So:

- `ast/` holds `pg_query.pb.go`, regenerated from the pinned
  `pg_query.proto` (276 messages, 73 enums; `json_name` annotations and
  field numbers unchanged, so wire format and protobuf-JSON are identical to
  upstream's).
- The root package exposes generated **type and const aliases**
  (`type ParseResult = ast.ParseResult`, one line per exported type/enum
  value) so `pg_query.SelectStmt`, `pg_query.JoinType_JOIN_INNER`, etc. all
  resolve exactly as they do in pg_query_go (with the import aliased to
  `pg_query`). The alias file is emitted by `cmd/generate` — never
  hand-maintained.

The grammar actions build these protobuf structs **directly**. libpg_query's
whole outfuncs/readfuncs layer (C parse nodes → protobuf) exists because the
C parser builds C structs; a Go parser building the Go structs natively
deletes that entire translation layer.

### 3. The source grammar is a 54k-line LALR bison grammar

This is the largest grammar in the family — bigger than GoogleSQL, far bigger
than SQLite. What recursive descent must consciously replicate:

- **The precedence ladder** — `%left`/`%right`/`%nonassoc` declarations in
  `gram.y`, implemented as one precedence-climbing expression loop
  (doubleclick-style Pratt), with PostgreSQL's quirks: unary minus binding,
  `AT TIME ZONE`, `COLLATE`, `AT LOCAL`, postfix `IS NULL`/`ISNULL`,
  `BETWEEN`/`IN`/`LIKE`/`ILIKE`/`SIMILAR` via the `NOT_LA` mechanism,
  qualified operators (`OPERATOR(schema.+)`), and the multi-character
  operator rules that live half in the scanner.
- **`base_yylex` lookahead filtering** — PostgreSQL keeps its grammar LALR(1)
  by merging token pairs in a filter between lexer and parser
  (`parser.c`): `NOT` → `NOT_LA` before `BETWEEN/IN/LIKE/...`, `NULLS` →
  `NULLS_LA` before `FIRST/LAST`, `WITH` → `WITH_LA` before
  `TIME/ORDINALITY`, `WITHOUT` → `WITHOUT_LA` before `TIME`, `FORMAT` →
  `FORMAT_LA` before `JSON`. oliphant reproduces this as a filter layer
  between lexer and parser so the parser sees exactly the reference token
  stream.
- **494 keywords in four reserved-ness categories** (unreserved, column-name,
  type-function-name, reserved) plus the orthogonal `bare_label` attribute
  (PG 14+ `SELECT expr alias` without `AS`). As in meyer, every
  identifier-position consumption routes through a small set of helpers
  encoding exactly these sets; the tables are generated from the pinned
  `kwlist.h`, and `Scan`'s `KeywordKind` output cross-checks them on the
  whole corpus.
- **The ~3,000 lines of support functions at the bottom of `gram.y`** —
  `insertSelectOptions`, `makeOrderedSetArgs`, `doNegate`,
  `SplitColQualList`, `SystemTypeName`, `makeIntConst`, precision/typmod
  packing for `INTERVAL`/`NUMERIC`, `check_func_name`, … These build the
  exact node shapes and must be ported faithfully; each gets a Go
  counterpart with the same name and an attribution comment.
- **Location fidelity.** Every node's `location` field (plus PG 17's
  `list_start`/`list_end`, `rexpr_list_start`/`rexpr_list_end`) must
  byte-match the reference — sqlc slices source text with these, and the
  tree-diff oracle catches every deviation. The grammar is precise about
  *which* token a node's location points at (e.g. an `A_Expr`'s location is
  the operator's, a `TypeCast`'s is the expression's start); these choices
  fall out of `@N` references in `gram.y` actions and are ported rule by
  rule.
- **Literal semantics**: integer literals that overflow `int32` become
  `Float` nodes (`process_integer_literal`), numeric literals keep their
  source spelling, string continuation across newlines (`'a'\n'b'`),
  `standard_conforming_strings` on — all exactly as the pinned build
  behaves.

### 4. The scanner is a port of (patched) `scan.l`

Flex's 12 exclusive start conditions become an explicit state machine:
extended (`E''`) and standard strings, dollar-quoted strings (`$tag$...$tag$`),
quoted identifiers, Unicode escapes (`U&'...'` / `U&"..."` + `UESCAPE`),
bit/hex strings, nested `/* */` comments, and PostgreSQL's operator munching
(longest-match with the "trailing `+`/`-` only after non-SQL operator chars"
rule, `!=` → `<>` normalization). Numeric literals include PG 16+
underscore separators and hex/octal/binary integers. The patches apply:
token end offsets are tracked (patch 03) and `param_junk` does not exist
(09).

The scanner has its own byte-exact oracle: `Scan()` returns
`ScanResult{Tokens: []*ScanToken{Start, End, Token, KeywordKind}}`, and the
reference emits the same protobuf. Token-stream equality over the entire
corpus is the acceptance gate for the lexer milestone, before any parsing
work starts. `SplitWithScanner` (a ~220-line lexer-level statement splitter)
ships in the same milestone.

### 5. The oracle is total: byte-identical trees, errors, and JSON

meyer's oracle could only say accept/reject + error message. libpg_query
dumps everything, so conformance here is the strongest in the family — for
every corpus statement:

- **Parse**: the protobuf tree equals the reference tree exactly
  (`proto.Equal`, diffs rendered via protobuf-JSON).
- **ParseToJSON**: byte-identical output. Upstream's JSON does **not** come
  from protojson — it is a hand-rolled C emitter (`pg_query_outfuncs_json.c`)
  with its own conventions (fields in struct order under the C type's
  `json_name`, zero/NULL fields omitted, specific float and string escaping).
  oliphant generates an equivalent emitter from the same metadata rather
  than bending protojson into shape.
- **Errors**: same `Error{Message, Filename, Funcname, Cursorpos}` —
  `syntax error at or near "x"` with the same cursor position, scanner errors
  attributed to `scan.l`/`scanner_yyerror`, grammar-action errors
  (`ereport` calls in `gram.y`, e.g. "improper qualified name") ported with
  their exact strings. `Lineno` is excluded from conformance (upstream's own
  tests zero it — it is a C source line number; oliphant emits the
  reference's value where stable, and nothing depends on it).
- **Version**: `ParseResult.Version` reports the pinned `PG_VERSION_NUM`
  (170007).

## Fingerprinting: port it (recommendation)

Fingerprinting is in scope, for three reasons:

1. `HashXXH3_64` is exported API, so oliphant needs a pure-Go **XXH3-64**
   implementation regardless (implemented in `internal/xxh3`, ~500 lines,
   verified against upstream's published test vectors and the three
   hard-coded vectors in pg_query_go's tests). The marginal cost of
   fingerprinting proper is only the tree walk.
2. The walk is generated, not written: libpg_query produces its 15k-line
   `fingerprint_defs.c` from `srcdata/struct_defs.json`; oliphant's
   `cmd/generate` emits the equivalent Go from the same vendored metadata.
   The hand-written parts are small and well-understood: version-3 seed,
   depth cutoff at 100, empty-subtree elision (hash-state snapshot/restore),
   sort-and-dedup of list elements for `fromClause`/`targetList`/`cols`/
   `rexpr`/`valuesLists`/`args` (with the pointer-keyed cache that makes the
   1.1 MB stress query tractable), the skip-list (locations, prepared
   statement names, cursor names, `NOTIFY` payloads, …), pre-PG15 legacy
   field names (`str`), the `RangeVar` alias/schema jumbling rules, and
   `AEXPR_OP_ANY`/`AEXPR_IN` folding to `AEXPR_OP`.
3. The corpus is ready-made: ~96 query/hash pairs in libpg_query's
   `fingerprint_tests.c`, 78 in pg_query_go's `testdata/fingerprint.json`
   (including the 1.1 MB insert), plus differential fuzzing against the
   oracle for free.

Fingerprints are stable across libpg_query majors by design (that is the
point of the legacy-name shims), so this work is not redone per PG version.

## Test corpus

The corpus question has a better answer for PostgreSQL than for any sibling:
**libpg_query already vendors the full PostgreSQL regression suite**, and the
oracle can classify and golden-ify every statement mechanically.

Inventory (all at the pinned tag, sizes measured):

| Source | Contents |
|---|---|
| `libpg_query/test/sql/postgres_regress/` | **233 files, ~118,400 lines** — PostgreSQL's own `src/test/regress/sql/`, the single densest grammar-coverage corpus that exists; includes deliberately-invalid statements (negative cases with exact error goldens) |
| `libpg_query/test/sql/plpgsql_regress/` | 13 files, ~3,500 lines (PL/pgSQL milestone) |
| `libpg_query/test/sql/deparse/` + `deparse-depesz/` | 13 + 150 files — deparser roundtrip corpora |
| libpg_query inline test tables | 72 parse, 62 normalize, ~96 fingerprint, 419 deparse, plus scan/split/normalize_utility/summary cases — transcribed by the corpus tool, never by hand |
| pg_query_go tests | `testdata/fingerprint.json` (78 cases), `parse_test.go`/`normalize_test.go`/`split_test.go` tables — the drop-in-compatibility smoke set |
| sqlc `internal/endtoend/testdata/` | **1,049 PostgreSQL `.sql` files / ~929 named queries** — exactly the inputs oliphant must keep accepting for sqlc; imported as a corpus tier and later exercised for real by sqlc's own `TestReplay` during integration |
| PostgreSQL tarball `contrib/*/sql/`, `src/pl/*/sql/` | thousands more statements, extracted by the same tool from the pinned tarball (stretch tier — added when regress conformance nears 100%) |
| Differential fuzzing | `cmd/difftest`: mutation fuzzing with the **live oracle in-process** — every corpus statement is a seed; the invariant is full tree/error equality, not "doesn't panic". This turns ~120k corpus lines into effectively unlimited cases and is the answer to "is the corpus large enough?" |

**Pipeline** (`cmd/regenerate`): a separate Go module under `oracle/` (so the
main module never depends on cgo) links pg_query_go v6.2.2, splits each
corpus file with the oracle's own splitter, runs every statement through
parse/scan/normalize/fingerprint/deparse, and writes consolidated `.test`
files: SQL, then expected protobuf-JSON tree (or `ERROR:` message +
cursor position), in the family's `==`/`--` format with `metadata.json` todo
sidecars. psql backslash commands and `COPY ... FROM stdin` payloads in the
regression files are filtered at extraction. Goldens are committed;
conformance CI is pure Go with no oracle present. A scheduled CI job re-runs
regeneration against the pinned oracle to prove goldens are reproducible.

## Repository layout

```
oliphant/
├── go.mod                    # github.com/sqlc-dev/oliphant; dep: google.golang.org/protobuf
├── LICENSE                   # MIT (oliphant's own code)
├── LICENSE.POSTGRESQL        # PostgreSQL License (ported grammar/scanner logic)
├── LICENSE.LIBPG_QUERY       # BSD-3 (pganalyze; ported glue/deparser/fingerprint logic)
├── PLAN.md                   # this file
├── CLAUDE.md                 # dev loop: next-test → implement → -check-parse
├── oliphant.go               # public API (package oliphant) — mirrors pg_query_go
├── makefuncs.go              # ported verbatim from pg_query_go
├── aliases.go                # generated type/const aliases into ast/
├── ast/
│   └── pg_query.pb.go        # generated from pinned pg_query.proto
├── parser/                   # public subpackage: Error + protobuf-bytes entry points
├── internal/
│   ├── lexer/                # scan.l port; keyword tables (generated)
│   ├── parse/                # the recursive-descent grammar, one file per gram.y region
│   │   ├── parse_select.go   #   select_stmt, set ops, CTEs, VALUES, locking
│   │   ├── parse_expr.go     #   a_expr/b_expr/c_expr precedence climbing, func_expr
│   │   ├── parse_dml.go      #   insert/update/delete/merge/copy
│   │   ├── parse_ddl_*.go    #   create table/alter table/index/…, ~150 stmt types
│   │   ├── parse_utility.go  #   set/show/vacuum/explain/grant/…
│   │   └── gram_support.go   #   ports of gram.y's static helper functions
│   ├── emit/                 # generated JSON emitter (ParseToJSON parity)
│   ├── fingerprint/          # generated walk + hand-written special cases
│   ├── xxh3/                 # in-house XXH3-64
│   ├── normalize/            # pg_query_normalize.c port (constant → $n)
│   ├── deparse/              # postgres_deparse.c port (12k lines C → Go)
│   ├── plpgsql/              # pl_gram.y port + JSON output (last milestone)
│   ├── summary/              # summary API port
│   └── reference/            # vendored gram.y, scan.l, kwlist.h (docs only)
├── srcdata/                  # vendored libpg_query srcdata/*.json (generator input)
├── oracle/                   # SEPARATE go module; links pg_query_go v6 (cgo)
│   └── ...                   # oracle binary used by cmd/regenerate & cmd/difftest
├── parser/testdata/          # consolidated corpus + goldens + metadata sidecars
└── cmd/
    ├── generate/             # emits ast aliases, JSON emitter, fingerprint walk,
    │                         #   keyword tables from srcdata + proto (replaces the
    │                         #   Ruby generators; this is the PG-upgrade story)
    ├── regenerate/           # corpus extraction + golden generation via oracle/
    ├── next-test/            # pick next todo case
    ├── debug-parse/          # parse argv SQL, print tree/JSON/error
    └── difftest/             # mutation differential fuzzing vs live oracle
```

## Milestones

Ordered so that every milestone lands with its own oracle-backed acceptance
gate; the corpus harness exists before the first line of the lexer.

1. **Scaffolding + codegen.** ✅ *Landed 2026-08-15.* Module, licenses, CI;
   vendor `srcdata/`, `pg_query.proto`, reference grammar files at the pin;
   `cmd/generate` producing `ast/pg_query.pb.go`, root aliases, keyword
   tables; `oliphant.go` API stubs returning not-implemented; `makefuncs.go`
   and the `parser.Error` type ported. **Gate:** pg_query_go's
   `parse_test.go` expected-tree literals compile unchanged against
   oliphant's types — met (`parse_test.go`, import swap only).
2. **Corpus + oracle.** ✅ *Landed 2026-08-15.* `oracle/` module wrapping
   pg_query_go v6.2.2; `cmd/regenerate` extracting the corpus tiers into
   `.test` goldens (trees, errors, scan tokens, normalize, fingerprint,
   deparse, splits); `internal/testfile` reader; harness running everything
   as `todo`. **Gate:** goldens reproducible byte-for-byte across two
   regeneration runs — met, and re-verified weekly by the `regenerate` CI
   workflow. See "As-built notes" below for measured counts and deferrals.
3. **Lexer.** ✅ *Landed 2026-08-15.* `scan.l` port + `base_yylex` filter
   layer; `Scan`, `SplitWithScanner`, `HashXXH3_64` (+ `internal/xxh3`)
   ship. **Gate:** token streams byte-identical to the oracle across the
   entire corpus, including scanner error messages and cursor positions —
   met: all 43,373 scan cases and all 8 split_scanner cases pass; xxh3 is
   verified against 126 oracle-generated vectors covering every length
   class and seed path. See "As-built notes (milestone 3)".
4. **Expressions + SELECT.** `a_expr`/`b_expr`/`c_expr` precedence machinery,
   constants and typecasts, `func_expr` (including `json_*`, `xml*`,
   aggregate `FILTER`/`WITHIN GROUP`, window functions), `SELECT` end-to-end:
   target list, `FROM` (joins, `LATERAL`, table functions, `TABLESAMPLE`),
   grouping sets, set operations, CTEs (`MATERIALIZED`, `SEARCH`/`CYCLE`),
   `VALUES`, locking clauses. The largest single chunk of work.
5. **DML.** `INSERT` (`ON CONFLICT`, `OVERRIDING`), `UPDATE`, `DELETE`,
   `MERGE`, `RETURNING`, `COPY`, `PREPARE`/`EXECUTE`, cursors.
6. **DDL, part 1.** `CREATE`/`ALTER TABLE` (the second-biggest grammar
   region: constraints, partitioning, identity, storage options),
   `CREATE INDEX`, views, sequences, schemas.
7. **DDL, part 2 + utility.** The long tail of ~150 statement types:
   functions/procedures, triggers, policies, roles/grants, types/domains,
   FDWs, publications/subscriptions, `EXPLAIN`/`VACUUM`/`SET`/`SHOW`, event
   triggers, … `SplitWithParser` and `IsUtilityStmt` fall out here.
   **Gate for 4–7:** 100% of the regress corpus — trees, JSON bytes, error
   messages, cursor positions.
8. **Normalize + Fingerprint.** Port `pg_query_normalize.c` (constant
   locations → `$n`, `NormalizeUtility`); generated fingerprint walk + the
   hand-written special cases. **Gate:** all normalize/fingerprint goldens,
   including the 1.1 MB stress case.
9. **Deparse.** Port `postgres_deparse.c` (12,220 lines, mechanical
   node-by-node) targeting the protobuf structs directly. **Gate:** the
   reference's own roundtrip criterion over its deparse allowlists — parse →
   deparse → reparse yields an identical tree — plus byte-equality with the
   oracle's deparse output on the corpus.
10. **Summary.** Port the summary API (classification + smart truncation).
11. **PL/pgSQL.** Port `pl_gram.y` + `pl_comp` subset + the JSON serializer
    behind `ParsePlPgSqlToJSON`. Deliberately last: self-contained, and the
    only piece whose deferral wouldn't block sqlc. If v1.0 ships without it,
    the function returns a clear unimplemented error — flagged for an
    explicit scope call rather than silently dropped.
12. **Hardening.** `cmd/difftest` mutation fuzzing vs the live oracle as a
    scheduled CI job; `go test -race` over the parallel corpus run;
    benchmarks vs pg_query_go (cgo) and wasilibs (wasm) — expect wins from
    no cgo crossings and true in-process parallelism; memory profiling on
    the stress queries.
13. **sqlc integration** (in the sqlc repo). Replace
    `parse_default.go`/`parse_wasi.go` with one unconditional file; swap the
    import path in `convert.go` et al.; drop `wasilibs/go-pgquery`,
    `tetratelabs/wazero`, and the cgo requirement; sqlc's endtoend suite is
    the acceptance gate. Windows and `CGO_ENABLED=0` become first-class.

### As-built notes (milestones 1–2)

Measured at the pin, where the plan's estimates differ:

- The corpus as regenerated: **1,525 `.test` files / 217,229 cases (~80 MB)**
  across eight suites (`parse`, `scan`, `normalize`, `normalize_utility`,
  `fingerprint`, `deparse`, `split_scanner`, `split_parser`).
- `postgres_regress/` has **224** files at `17-6.2.2` (not 233); `kwlist.h`
  yields **491** keywords (not 494); the proto has **273** messages and
  **71** enums (not 276/73). The inline tables carry 36 parse, 12 scan,
  25 normalize, 25 normalize_utility, 78 fingerprint, 418 deparse, and
  8 split inputs; `deparse-depesz/` is 150 `.psql` files under `*.d/`
  subdirectories.
- The `.test` format needed one addition over the family convention: input
  or expectation lines that collide with the `== ` / `--` markers are
  escaped with a leading `|` (SQL comment banners of exactly `--` are
  everywhere in the regress files). `internal/testfile` round-trips this
  bijectively.
- `cmd/regenerate` preserves passing status across runs: a case identical in
  name, input, and expectation to one that had graduated out of `todo` stays
  passing, so a pin advance puts exactly the diff back on the todo list.
- `cmd/generate -proto` requires `protoc` + `protoc-gen-go` on PATH; the
  pure-Go `-aliases`/`-keywords` modes are what CI verifies.
- Deferred, deliberately: the **sqlc endtoend tier** (needs per-directory
  engine classification in the sqlc repo; import it alongside milestone 4),
  **`plpgsql_regress/`** (milestone 11), and **summary** golden extraction
  (milestone 10). The stretch tarball tier remains stretch.

### As-built notes (milestone 3)

- The scanner (`internal/lexer`) is pull-based (`Scanner.Next`), one token
  per call like `core_yylex`; `Scan`/`SplitWithScanner` drain it eagerly,
  but milestone 4's parser must pull lazily so grammar errors can win over
  scanner errors that lie further right, as they do under bison.
- Flex's longest-match/rule-order discipline is reproduced structurally
  except for the numeric-literal rules, whose fourteen overlapping patterns
  (`decinteger` … `real_junk`, including flex backtracking inside the
  `*_junk` rules) are resolved by computing every candidate match length
  and picking the winner (`internal/lexer/numbers.go`).
- Two pinned-oracle behaviors worth knowing: token End offsets come from
  the patch-03 `yyllocend` for multi-rule tokens
  (`pg_query_scan.c` uses it for SCONST/USCONST/BCONST/XCONST/IDENT/
  UIDENT/C_COMMENT and `yylloc + yyleng` for the rest — with the eager
  scanner both reduce to "position after the token"), and a comment
  *between* a `base_yylex` merge pair blocks the merge (the lookahead is a
  raw `core_yylex` call, so pg_query_go v6.2.2 rejects
  `SELECT 1 WHERE 1 NOT /* c */ IN (2)` where vanilla PostgreSQL does not;
  the filter reproduces this, pinned by a unit test).
- Scanner error cursor positions are character-based
  (`pg_mbstrlen_with_len` semantics, per-lead-byte stride, not rune
  count); the "at or near" text runs from the error location to the end of
  the current match (flex's hold-char NUL), or to end of input in `<<EOF>>`
  rules.
- `internal/xxh3` implements only `XXH3_64bits_withSeed` (scalar paths),
  the sole entry point the API needs.

## Regeneration (the PostgreSQL-upgrade story)

Everything derived is derived by committed tooling from the pin:
`cmd/generate` (from `srcdata/` + `pg_query.proto`) and `cmd/regenerate`
(from the corpus + oracle). Advancing to a new libpg_query tag is a
defined workflow, not a rewrite:

1. Bump the pin; re-vendor `srcdata/`, proto, reference files, corpus.
2. Re-run `cmd/generate` (new nodes/fields appear in `ast/`, aliases, JSON
   emitter, fingerprint walk, keyword tables).
3. Re-run `cmd/regenerate` against the new oracle; every diff is either a
   new grammar feature (goes on the todo list, driven down by `next-test`)
   or a changed node shape (mechanical).
4. Grammar diffs are read from `git diff` of the vendored `gram.y`/`scan.l`
   — upstream's parser changes per major are small relative to the whole.

Module versioning follows pg_query_go's convention once stable: a PostgreSQL
major bump is an oliphant major bump.

## Non-goals

- **No semantic analysis** — no catalog lookups, no type checking, no name
  resolution. Errors PostgreSQL raises after raw parsing are out of scope,
  exactly as in libpg_query (whose analysis-phase entry points are mocked).
- **Not a formatter.** Deparse matches the reference deparser's output
  byte-for-byte; it makes no beautification promises beyond it.
- **No error recovery / multi-error reporting** — first error aborts, like
  bison and every sibling.
- **No options plumbing upstream doesn't expose in Go**: the `*_opts`
  C variants and deparse pretty-printing are not part of pg_query_go's Go
  API and are not part of oliphant's v1 (revisit if pg_query_go adds them).
- **No new dialect.** oliphant parses exactly what the pinned libpg_query
  accepts — including its patches — and nothing else.

## Risks and mitigations

- **Grammar scale.** This is the family's biggest port (estimate 35–50k
  lines of hand-written Go vs ~25–29k for zetajones/teesql). Mitigation: the
  corpus loop makes progress strictly incremental and measurable; the
  regress suite's file-per-feature layout gives natural work units; nothing
  blocks on completeness (every unimplemented production fails as a todo,
  not a crash).
- **Error-message/cursor parity with bison.** A recursive-descent parser
  does not naturally fail on the same token bison does in every case. The
  regress corpus's negative cases + difftest give exhaustive coverage;
  where RD and LALR genuinely diverge, the parser tracks the
  furthest-consumed token to reproduce bison's report point. meyer proved
  this bar reachable; zetajones hit byte-parity including positions.
- **JSON byte-parity.** Field ordering and omission rules are generated
  from the same metadata upstream uses, and diffed byte-wise over the whole
  corpus — divergence is caught the day it appears, not at release.
- **Fingerprint stability.** Version-3 fingerprints are frozen by test
  vectors; the generated walk plus special cases are verified against both
  upstream corpora and difftest.
- **Upstream drift.** libpg_query majors track PostgreSQL majors on a
  ~yearly cadence; § Regeneration bounds the upgrade cost. The pin is
  explicit everywhere (goldens record the tag; `Version` field asserts it).
- **License.** PostgreSQL License for ported grammar/scanner/deparser
  logic and BSD-3 for libpg_query-derived glue are both permissive and
  compatible with MIT for oliphant's own code; all three notices ship in the
  repo root, and ported files carry per-file attribution headers (the
  sibling convention).
