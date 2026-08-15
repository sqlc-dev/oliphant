# internal/reference — the pinned grammar, for humans

These files are **documentation only**: nothing in the build reads them except
the keyword-table generator (`kwlist.h`). They are the source of truth every
`parse*` method's attribution comment points at.

Provenance — PostgreSQL **17.7** source tarball with libpg_query
**`17-6.2.2`**'s patches applied (the ones that touch these files):

| File | Upstream path | Patches applied |
|---|---|---|
| `gram.y` | `src/backend/parser/gram.y` | 01 (additional `$n` param-ref positions) |
| `scan.l` | `src/backend/parser/scan.l` | 03 (`yyllocend` token-end tracking), 04 (comments as tokens), 09 (`param_junk` removed) |
| `parser.c` | `src/backend/parser/parser.c` | 03, 04 (`base_yylex` lookahead filter — the NOT_LA/NULLS_LA/WITH_LA/WITHOUT_LA/FORMAT_LA merges live here) |
| `kwlist.h` | `src/include/parser/kwlist.h` | none (copied from libpg_query's extracted `src/postgres/include/parser/kwlist.h`) |

oliphant ports **patched** PostgreSQL, never vanilla: `$1` parameter references
are legal in more grammar positions, the lexer tracks token end offsets and
emits comments as tokens, and `param_junk` does not exist. The oracle for every
golden is the pinned libpg_query build, not upstream PostgreSQL.

To re-derive: download `postgresql-17.7.tar.gz`, extract these four paths, and
apply `patches/01,03,04,09` from the libpg_query tag (hunks touching other
files skipped).
