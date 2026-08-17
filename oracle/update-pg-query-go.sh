#!/usr/bin/env bash
# Populates oracle/pg_query_go/ — the cgo oracle's pg_query_go dependency —
# because no pg_query_go release ships libpg_query 18 yet. This reproduces
# pg_query_go's own `make update_source` against the pinned libpg_query tag:
# pg_query_go (Go wrapper, at PG_QUERY_GO_COMMIT) + libpg_query 18.0.0
# (vendored C sources, protobuf definitions, testdata).
#
# oracle/go.mod points at the output with a `replace` directive; run this
# script before building the oracle (cmd/regenerate, cmd/difftest, oracle
# benchmarks). Requires: git, go, protoc on PATH.
#
# Usage: update-pg-query-go.sh [libpg_query-checkout] [pg_query_go-checkout]
#   Optional args reuse existing checkouts (at the pins below) instead of
#   cloning.
set -euo pipefail

LIBPG_QUERY_TAG=18.0.0
# pg_query_go main as of 2026-08-17 (last commit before this script was
# written; there is no PG 18 release tag to pin yet).
PG_QUERY_GO_COMMIT=41be2fe356797a0034247dc46a72a7aa3e7bbe92

cd "$(dirname "$0")"
DEST=$PWD/pg_query_go
TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT

LIBDIR=${1:-}
if [[ -z $LIBDIR ]]; then
  git clone --depth 1 --branch "$LIBPG_QUERY_TAG" \
    https://github.com/pganalyze/libpg_query "$TMP/libpg_query"
  LIBDIR=$TMP/libpg_query
fi

rm -rf "$DEST"
if [[ -n ${2:-} ]]; then
  cp -a "$2" "$DEST"
  rm -rf "$DEST/.git"
else
  git clone https://github.com/pganalyze/pg_query_go "$DEST"
  git -C "$DEST" checkout --quiet "$PG_QUERY_GO_COMMIT"
  rm -rf "$DEST/.git"
fi

# The steps of pg_query_go's `make update_source`, verbatim (its curl of the
# release tarball replaced by the git checkout above).
cd "$DEST"
rm -f parser/*.c parser/*.h
rm -rf parser/include
cp -a "$LIBDIR"/src/* parser/
mv parser/postgres/include parser/include/postgres
rm parser/pg_query_outfuncs_protobuf_cpp.cc
mv parser/postgres/* parser/
rmdir parser/postgres
cp -a "$LIBDIR"/pg_query.h "$LIBDIR"/postgres_deparse.h parser/include
mkdir -p bin
GOBIN=$PWD/bin go install google.golang.org/protobuf/cmd/protoc-gen-go@v1.36.12
PATH="$PWD/bin:$PATH" protoc --proto_path="$LIBDIR"/protobuf \
  --go_out=. --go_opt=Mpg_query.proto=/pg_query --go_opt=paths=source_relative \
  "$LIBDIR"/protobuf/pg_query.proto
mkdir -p parser/include/protobuf
cp -a "$LIBDIR"/protobuf/*.h parser/include/protobuf
cp -a "$LIBDIR"/protobuf/*.c parser/
mkdir -p parser/include/protobuf-c
cp -a "$LIBDIR"/vendor/protobuf-c/*.h parser/include
cp -a "$LIBDIR"/vendor/protobuf-c/*.h parser/include/protobuf-c
cp -a "$LIBDIR"/vendor/protobuf-c/*.c parser/
mkdir -p parser/include/xxhash
cp -a "$LIBDIR"/vendor/xxhash/*.h parser/include
cp -a "$LIBDIR"/vendor/xxhash/*.h parser/include/xxhash
cp -a "$LIBDIR"/vendor/xxhash/*.c parser/
rm -rf testdata
cp -a "$LIBDIR"/testdata testdata
bash scripts/gokeep.sh

echo "oracle/pg_query_go ready: pg_query_go@$PG_QUERY_GO_COMMIT + libpg_query $LIBPG_QUERY_TAG"
