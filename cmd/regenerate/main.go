// Command regenerate derives every golden in parser/testdata from the pinned
// oracle (libpg_query 17-6.2.2 via pg_query_go v6.2.2, cgo). It is the only
// writer of that tree; expected outputs are NEVER edited by hand.
//
//	go run ./cmd/regenerate -libpg-query <checkout@17-6.2.2> -pg-query-go <checkout@v6.2.2>
//
// Corpus tiers extracted:
//
//   - test/sql/postgres_regress/*.sql  → parse, scan, normalize, fingerprint,
//     deparse suites (the PostgreSQL regression suite; psql metacommands and
//     COPY-FROM-stdin payloads filtered)
//   - test/sql/deparse/*.sql, deparse-depesz/*.sql → parse, scan, deparse
//   - inline test tables (test/*_tests.c) → their respective suites
//     (inputs transcribed mechanically; expectations always from the oracle)
//   - pg_query_go testdata/fingerprint.json → fingerprint suite (includes the
//     1.1 MB stress insert)
//
// Output must be byte-identical across runs — that reproducibility is
// milestone 2's acceptance gate and is enforced weekly in CI.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/sqlc-dev/oliphant/internal/testfile"
)

var stats struct{ files, cases int }

func main() {
	libpgQuery := flag.String("libpg-query", "", "path to a libpg_query checkout at tag 17-6.2.2")
	pgQueryGo := flag.String("pg-query-go", "", "path to a pg_query_go checkout at tag v6.2.2")
	oracleBin := flag.String("oracle", "", "prebuilt oracle binary (default: go build ./oracle)")
	out := flag.String("out", "parser/testdata", "output corpus root")
	flag.Parse()

	if *libpgQuery == "" || *pgQueryGo == "" {
		fmt.Fprintln(os.Stderr, "usage: regenerate -libpg-query <dir> -pg-query-go <dir> [-oracle <bin>]")
		os.Exit(1)
	}

	oracle, err := StartOracle("oracle", *oracleBin)
	check(err)
	defer oracle.Close()

	// Tier: PostgreSQL regression suite.
	regressDir := filepath.Join(*libpgQuery, "test", "sql", "postgres_regress")
	regressFiles := sqlFiles(regressDir)
	fmt.Fprintf(os.Stderr, "regenerate: postgres_regress: %d files\n", len(regressFiles))
	for _, f := range regressFiles {
		src, err := os.ReadFile(filepath.Join(regressDir, f))
		check(err)
		stmts := splitStatements(oracle, preprocessRegress(string(src)))
		base := strings.TrimSuffix(f, ".sql")
		emitSQLSuites(oracle, *out, "postgres_regress", base, stmts,
			[]string{"parse", "scan", "normalize", "fingerprint", "deparse"})
	}

	// Tier: deparser roundtrip corpora. deparse/ is flat .sql files;
	// deparse-depesz/ is <NN-topic>.d/ directories of .psql files.
	deparseDir := filepath.Join(*libpgQuery, "test", "sql", "deparse")
	deparseFiles := sqlFiles(deparseDir)
	fmt.Fprintf(os.Stderr, "regenerate: deparse: %d files\n", len(deparseFiles))
	for _, f := range deparseFiles {
		src, err := os.ReadFile(filepath.Join(deparseDir, f))
		check(err)
		stmts := splitStatements(oracle, string(src))
		base := strings.TrimSuffix(f, ".sql")
		emitSQLSuites(oracle, *out, "deparse", base, stmts, []string{"parse", "scan", "deparse"})
	}

	depeszDir := filepath.Join(*libpgQuery, "test", "sql", "deparse-depesz")
	var depeszFiles []string
	check(filepath.WalkDir(depeszDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.HasSuffix(path, ".psql") {
			depeszFiles = append(depeszFiles, path)
		}
		return nil
	}))
	sort.Strings(depeszFiles)
	fmt.Fprintf(os.Stderr, "regenerate: deparse-depesz: %d files\n", len(depeszFiles))
	for _, path := range depeszFiles {
		src, err := os.ReadFile(path)
		check(err)
		stmts := splitStatements(oracle, string(src))
		rel, err := filepath.Rel(depeszDir, path)
		check(err)
		base := strings.ReplaceAll(strings.TrimSuffix(strings.ReplaceAll(rel, ".d/", "_"), ".psql"), "/", "_")
		emitSQLSuites(oracle, *out, "deparse_depesz", base, stmts, []string{"parse", "scan", "deparse"})
	}

	// Tier: libpg_query inline test tables. Inputs are the even-indexed (or
	// all, for input-only tables) entries; expectations come from the oracle.
	inline := []struct {
		file   string
		suites []string
		pairs  bool
	}{
		{"parse_tests.c", []string{"parse"}, true},
		{"scan_tests.c", []string{"scan"}, true},
		{"normalize_tests.c", []string{"normalize"}, true},
		{"normalize_utility_tests.c", []string{"normalize_utility"}, true},
		{"fingerprint_tests.c", []string{"fingerprint"}, true},
		{"deparse_tests.c", []string{"deparse"}, false},
		{"split_tests.c", []string{"split_scanner", "split_parser"}, true},
	}
	for _, tbl := range inline {
		src, err := os.ReadFile(filepath.Join(*libpgQuery, "test", tbl.file))
		check(err)
		entries, err := extractCStrings(string(src))
		if err != nil {
			check(fmt.Errorf("%s: %w", tbl.file, err))
		}
		inputs := entries
		if tbl.pairs {
			if len(entries)%2 != 0 {
				check(fmt.Errorf("%s: odd number of entries (%d), expected input/expected pairs", tbl.file, len(entries)))
			}
			inputs = nil
			for i := 0; i < len(entries); i += 2 {
				inputs = append(inputs, entries[i])
			}
		}
		fmt.Fprintf(os.Stderr, "regenerate: %s: %d inputs\n", tbl.file, len(inputs))
		emitSQLSuites(oracle, *out, "", "libpg_query", inputs, tbl.suites)
	}

	// Tier: pg_query_go fingerprint corpus (incl. the 1.1 MB stress insert).
	var fpCases []struct {
		Input string `json:"input"`
	}
	fpData, err := os.ReadFile(filepath.Join(*pgQueryGo, "testdata", "fingerprint.json"))
	check(err)
	check(json.Unmarshal(fpData, &fpCases))
	var fpInputs []string
	for _, c := range fpCases {
		fpInputs = append(fpInputs, c.Input)
	}
	fmt.Fprintf(os.Stderr, "regenerate: pg_query_go fingerprint.json: %d inputs\n", len(fpInputs))
	emitSQLSuites(oracle, *out, "", "pg_query_go", fpInputs, []string{"fingerprint"})

	pruneStale(*out)
	fmt.Fprintf(os.Stderr, "regenerate: wrote %d files, %d cases\n", stats.files, stats.cases)
}

// pruneStale removes .test/.metadata.json files this run did not write —
// a source file dropped upstream must not leave a stale golden behind.
func pruneStale(out string) {
	filepath.WalkDir(out, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".test") && !strings.HasSuffix(path, ".metadata.json") {
			return nil
		}
		key := strings.TrimSuffix(strings.TrimSuffix(path, ".metadata.json"), ".test")
		if !written[key] {
			fmt.Fprintf(os.Stderr, "regenerate: pruning stale %s\n", path)
			check(os.Remove(path))
		}
		return nil
	})
}

var written = map[string]bool{}

// splitStatements splits a SQL script with the oracle's own scanner-level
// splitter (statements trimmed, empties dropped) — the same splitter
// consumers of SplitWithScanner get.
func splitStatements(oracle *Oracle, src string) []string {
	stmts, oerr, err := oracle.Split("split_scanner", src, true)
	check(err)
	if oerr != nil {
		// Scanner-level split failed (e.g. an unterminated literal the
		// preprocessing exposed): fall back to the whole text as one
		// statement; it will golden as an ERROR, deterministically.
		return []string{strings.TrimSpace(src)}
	}
	var out []string
	for _, s := range stmts {
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

// emitSQLSuites runs every statement through each suite's oracle op and
// writes <out>/<suite>/<subdir>/<base>.test plus its metadata sidecar.
//
// Todo bookkeeping: a case identical (name, input, expectation) to one that
// had already graduated out of todo stays passing; everything else — new
// cases, and cases whose golden changed under it — is todo. On a fresh tree
// that means everything, which is milestone 2's intended state; after a pin
// advance it means exactly the diff goes back on the todo list.
func emitSQLSuites(oracle *Oracle, out, subdir, base string, stmts []string, suites []string) {
	for _, suite := range suites {
		var cases []testfile.Case
		for i, sql := range stmts {
			name := fmt.Sprintf("%03d", i+1)
			expected := goldenFor(oracle, suite, sql)
			cases = append(cases, testfile.Case{Name: name, Input: sql, Expected: expected})
		}
		if len(cases) == 0 {
			continue
		}
		dir := filepath.Join(out, suite, subdir)
		check(os.MkdirAll(dir, 0o755))
		path := filepath.Join(dir, base+".test")

		passing := map[string]testfile.Case{}
		if old, err := testfile.Read(path); err == nil {
			oldMeta, err := testfile.ReadMetadata(path)
			check(err)
			todoSet := map[string]bool{}
			for _, n := range oldMeta.Todo {
				todoSet[n] = true
			}
			for _, c := range old {
				if !todoSet[c.Name] {
					passing[c.Name] = c
				}
			}
		}

		check(testfile.Write(path, cases))
		var todo []string
		for _, c := range cases {
			if p, ok := passing[c.Name]; ok && p.Input == c.Input && p.Expected == c.Expected {
				continue
			}
			todo = append(todo, c.Name)
		}
		check(testfile.WriteMetadata(path, testfile.Metadata{Todo: todo}))
		written[strings.TrimSuffix(path, ".test")] = true
		stats.files++
		stats.cases += len(cases)
	}
}

func goldenFor(oracle *Oracle, suite, sql string) string {
	switch suite {
	case "parse", "normalize", "normalize_utility", "fingerprint", "deparse":
		op := map[string]string{
			"parse":             "parse_json",
			"normalize":         "normalize",
			"normalize_utility": "normalize_utility",
			"fingerprint":       "fingerprint",
			"deparse":           "deparse",
		}[suite]
		text, oerr, err := oracle.Text(op, sql)
		check(err)
		if oerr != nil {
			return renderOracleError(oerr)
		}
		return text
	case "scan":
		toks, oerr, err := oracle.Scan(sql)
		check(err)
		if oerr != nil {
			return renderOracleError(oerr)
		}
		lines := make([]string, len(toks))
		for i, t := range toks {
			lines[i] = testfile.RenderScanToken(t.Start, t.End, t.Token, t.KeywordKind)
		}
		return strings.Join(lines, "\n")
	case "split_scanner", "split_parser":
		stmts, oerr, err := oracle.Split(suite, sql, true)
		check(err)
		if oerr != nil {
			return renderOracleError(oerr)
		}
		return testfile.RenderSplit(stmts)
	}
	panic("unknown suite " + suite)
}

func renderOracleError(e *oracleError) string {
	return testfile.RenderError(testfile.ErrorExpectation{
		Message:   e.Message,
		Cursorpos: e.Cursorpos,
		Filename:  e.Filename,
		Funcname:  e.Funcname,
		Context:   e.Context,
	})
}

func sqlFiles(dir string) []string {
	entries, err := os.ReadDir(dir)
	check(err)
	var files []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			files = append(files, e.Name())
		}
	}
	sort.Strings(files)
	return files
}

func check(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "regenerate:", err)
		os.Exit(1)
	}
}
