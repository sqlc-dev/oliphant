package parser_test

import (
	"errors"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	pg_query "github.com/sqlc-dev/oliphant"
	"github.com/sqlc-dev/oliphant/internal/testfile"
	"github.com/sqlc-dev/oliphant/parser"
)

// -check-parse runs the todo cases too and harvests the ones that now pass:
// they are removed from the metadata.json sidecars (commit the diff with the
// code change that made them pass). Without it, todo cases are skipped and
// any NON-todo case that fails is a regression — a hard test failure.
var checkParse = flag.Bool("check-parse", false, "run todo cases and remove newly passing ones from metadata sidecars")

var suites = []string{
	"parse", "scan", "normalize", "normalize_utility",
	"fingerprint", "deparse", "split_scanner", "split_parser",
	"summary", "summary_truncate", "plpgsql",
}

func TestCorpus(t *testing.T) {
	total := struct{ files, cases, todo, harvested int }{}
	for _, suite := range suites {
		root := filepath.Join("testdata", suite)
		if _, err := os.Stat(root); os.IsNotExist(err) {
			continue
		}
		var files []string
		err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if !d.IsDir() && strings.HasSuffix(path, ".test") {
				files = append(files, path)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		for _, path := range files {
			rel, _ := filepath.Rel("testdata", path)
			t.Run(rel, func(t *testing.T) {
				cases, err := testfile.Read(path)
				if err != nil {
					t.Fatal(err)
				}
				meta, err := testfile.ReadMetadata(path)
				if err != nil {
					t.Fatal(err)
				}
				todoSet := map[string]bool{}
				for _, n := range meta.Todo {
					todoSet[n] = true
				}

				var stillTodo []string
				changed := false
				for _, c := range cases {
					total.cases++
					if todoSet[c.Name] {
						total.todo++
						if !*checkParse {
							stillTodo = append(stillTodo, c.Name)
							continue
						}
						if evaluate(suite, c.Input) == c.Expected {
							// Newly passing: harvest it out of the todo list.
							total.harvested++
							changed = true
						} else {
							stillTodo = append(stillTodo, c.Name)
						}
						continue
					}
					// A case that has graduated out of todo must stay green
					// forever: a mismatch here is a regression, never a new
					// todo.
					if got := evaluate(suite, c.Input); got != c.Expected {
						t.Errorf("%s case %s REGRESSED\ninput:\n%s\ngot:\n%s\nwant:\n%s",
							rel, c.Name, c.Input, got, c.Expected)
					}
				}
				if changed {
					if err := testfile.WriteMetadata(path, testfile.Metadata{Todo: stillTodo}); err != nil {
						t.Fatal(err)
					}
				}
			})
			total.files++
		}
	}
	if *checkParse && total.harvested > 0 {
		t.Logf("harvested %d newly passing cases (metadata sidecars updated)", total.harvested)
	}
	t.Logf("corpus: %d files, %d cases, %d todo", total.files, total.cases, total.todo)
}

// evaluate runs one corpus case through oliphant's public API and renders the
// result in the same byte format cmd/regenerate writes from the oracle.
func evaluate(suite, input string) string {
	switch suite {
	case "parse":
		out, err := pg_query.ParseToJSON(input)
		if err != nil {
			return renderErr(err)
		}
		return out
	case "scan":
		res, err := pg_query.Scan(input)
		if err != nil {
			return renderErr(err)
		}
		lines := make([]string, len(res.Tokens))
		for i, tok := range res.Tokens {
			lines[i] = testfile.RenderScanToken(tok.Start, tok.End, tok.Token.String(), tok.KeywordKind.String())
		}
		return strings.Join(lines, "\n")
	case "normalize":
		out, err := pg_query.Normalize(input)
		if err != nil {
			return renderErr(err)
		}
		return out
	case "normalize_utility":
		out, err := pg_query.NormalizeUtility(input)
		if err != nil {
			return renderErr(err)
		}
		return out
	case "fingerprint":
		out, err := pg_query.Fingerprint(input)
		if err != nil {
			return renderErr(err)
		}
		return out
	case "deparse":
		tree, err := pg_query.Parse(input)
		if err != nil {
			return renderErr(err)
		}
		out, err := pg_query.Deparse(tree)
		if err != nil {
			return renderErr(err)
		}
		return out
	case "split_scanner":
		stmts, err := pg_query.SplitWithScanner(input, true)
		if err != nil {
			return renderErr(err)
		}
		return testfile.RenderSplit(stmts)
	case "split_parser":
		stmts, err := pg_query.SplitWithParser(input, true)
		if err != nil {
			return renderErr(err)
		}
		return testfile.RenderSplit(stmts)
	case "summary":
		res, err := pg_query.Summary(input, -1)
		if err != nil {
			return renderErr(err)
		}
		return renderSummary(res)
	case "summary_truncate":
		limit, sql, err := testfile.SplitTruncateLimit(input)
		if err != nil {
			return "BAD CASE: " + err.Error()
		}
		res, err := pg_query.Summary(sql, limit)
		if err != nil {
			return renderErr(err)
		}
		return renderSummary(res)
	case "plpgsql":
		out, err := pg_query.ParsePlPgSqlToJSON(input)
		if err != nil {
			return renderErr(err)
		}
		return out
	}
	panic("unknown suite " + suite)
}

// renderSummary encodes a SummaryResult in the same byte format
// cmd/regenerate writes from the oracle.
func renderSummary(res *pg_query.SummaryResult) string {
	e := testfile.SummaryExpectation{
		Aliases:        res.Aliases,
		CteNames:       res.CteNames,
		StatementTypes: res.StatementTypes,
		TruncatedQuery: res.TruncatedQuery,
	}
	for _, t := range res.Tables {
		e.Tables = append(e.Tables, testfile.SummaryTable{
			Name: t.Name, SchemaName: t.SchemaName, TableName: t.TableName, Context: t.Context.String(),
		})
	}
	for _, f := range res.Functions {
		e.Functions = append(e.Functions, testfile.SummaryFunction{
			Name: f.Name, FunctionName: f.FunctionName, SchemaName: f.SchemaName, Context: f.Context.String(),
		})
	}
	for _, f := range res.FilterColumns {
		e.FilterColumns = append(e.FilterColumns, testfile.SummaryFilterColumn{
			SchemaName: f.SchemaName, TableName: f.TableName, Column: f.Column,
		})
	}
	return testfile.RenderSummary(e)
}

func renderErr(err error) string {
	var pe *parser.Error
	if errors.As(err, &pe) {
		// Lineno is deliberately excluded from conformance (a C source line
		// number upstream; their own tests zero it).
		return testfile.RenderError(testfile.ErrorExpectation{
			Message:   pe.Message,
			Cursorpos: pe.Cursorpos,
			Filename:  pe.Filename,
			Funcname:  pe.Funcname,
			Context:   pe.Context,
		})
	}
	// Not a parse error (e.g. a not-implemented stub): render distinctly so
	// it can never accidentally equal an oracle golden.
	return "UNIMPLEMENTED: " + err.Error()
}
