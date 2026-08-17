# internal/reference — the pinned grammar, for humans

These files are **documentation only**: nothing in the build reads them except
the table generators (`kwlist.h`, `pl_*_kwlist.h`, `plerrcodes.h`). They are
the source of truth every `parse*` method's attribution comment points at.

Provenance — PostgreSQL **18.4** source tarball with libpg_query
**`18.0.0`**'s patches applied (the ones that touch these files):

| File | Upstream path | Patches applied |
|---|---|---|
| `gram.y` | `src/backend/parser/gram.y` | 01 (additional `$n` param-ref positions), 04 (SQL_COMMENT/C_COMMENT token declarations) |
| `scan.l` | `src/backend/parser/scan.l` | 03 (`yyllocend` token-end tracking), 04 (comments as tokens), 09 (`param_junk` removed) |
| `parser.c` | `src/backend/parser/parser.c` | 03, 04 (`base_yylex` lookahead filter — the NOT_LA/NULLS_LA/WITH_LA/WITHOUT_LA/FORMAT_LA merges live here) |
| `kwlist.h` | `src/include/parser/kwlist.h` | none (copied from libpg_query's extracted `src/postgres/include/parser/kwlist.h`) |
| `pl_gram.y` | `src/pl/plpgsql/src/pl_gram.y` | 04 (SQL_COMMENT/C_COMMENT token declarations) |
| `pl_scanner.c` | `src/pl/plpgsql/src/pl_scanner.c` | 04 (comment tokens skipped in `internal_yylex`) |
| `pl_reserved_kwlist.h` | `src/pl/plpgsql/src/pl_reserved_kwlist.h` | none (copied from libpg_query's `src/postgres/include/`) |
| `pl_unreserved_kwlist.h` | `src/pl/plpgsql/src/pl_unreserved_kwlist.h` | none (same) |
| `plerrcodes.h` | generated from `src/backend/utils/errcodes.txt` | none (copied from libpg_query's `src/postgres/include/`) |
| `pg_query_pg_type.c` | libpg_query `src/include/pg_query_pg_type.c` | none (the mini pg_type catalog the mocked syscache serves; generator input for `internal/plpgsql/tables.go`) |
| `pg_type_d.h` | `src/include/catalog/pg_type_d.h` | none (copied from libpg_query's extracted includes; resolves the OID macros in `pg_query_pg_type.c`) |

oliphant ports **patched** PostgreSQL, never vanilla: `$1` parameter references
are legal in more grammar positions, the lexer tracks token end offsets and
emits comments as tokens, and `param_junk` does not exist. The oracle for every
golden is the pinned libpg_query build, not upstream PostgreSQL.

For PL/pgSQL, the reference is further from vanilla than for SQL: libpg_query's
`extract_source.rb` **mocks** several pl_comp.c functions in the build the
oracle runs (see `internal/plpgsql`'s attribution comments) — `parse_datatype`
keeps the type text verbatim instead of consulting the catalogs,
`plpgsql_parse_wordtype`/`%ROWTYPE` lookups return NULL, `make_return_stmt`
drops the return-type checks, and `function_parse_error_transpose` always
fails (so compile errors carry a "near line N" context instead of a cursor).
The vendored `src/postgres/src_pl_plpgsql_src_pl_gram.c` at the pin is the
authority where it differs from `pl_gram.y`.

To re-derive: download `postgresql-18.4.tar.gz`, extract these paths, and
apply `patches/01,03,04,09` from the libpg_query tag (hunks touching other
files skipped).
