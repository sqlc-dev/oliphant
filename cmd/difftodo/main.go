// difftodo prints input, expected, and actual output for the still-failing
// todo cases of a .test file (any suite; the suite is inferred from the
// path). Debug aid for the corpus loop; not part of the public tooling.
package main

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	pg_query "github.com/sqlc-dev/oliphant"
	"github.com/sqlc-dev/oliphant/internal/testfile"
	"github.com/sqlc-dev/oliphant/parser"
)

func main() {
	path := os.Args[1]
	limit := 5
	if len(os.Args) > 2 {
		limit, _ = strconv.Atoi(os.Args[2])
	}
	suite := "parse"
	for _, s := range []string{"parse", "scan", "normalize", "normalize_utility",
		"fingerprint", "deparse", "split_scanner", "split_parser",
		"summary", "summary_truncate", "plpgsql"} {
		if strings.Contains(path, "/"+s+"/") {
			suite = s
		}
	}
	cases, err := testfile.Read(path)
	if err != nil {
		panic(err)
	}
	meta, err := testfile.ReadMetadata(path)
	if err != nil {
		panic(err)
	}
	todo := map[string]bool{}
	for _, name := range meta.Todo {
		todo[name] = true
	}
	shown := 0
	for _, c := range cases {
		if !todo[c.Name] {
			continue
		}
		got := evaluate(suite, c.Input)
		if got == c.Expected {
			continue
		}
		fmt.Printf("== %s\nINPUT: %s\nWANT: %s\nGOT:  %s\n\n", c.Name, c.Input, c.Expected, got)
		shown++
		if shown >= limit {
			break
		}
	}
	fmt.Printf("(%d todo cases in file)\n", len(meta.Todo))
}

func evaluate(suite, input string) string {
	var out string
	var err error
	switch suite {
	case "parse":
		out, err = pg_query.ParseToJSON(input)
	case "normalize":
		out, err = pg_query.Normalize(input)
	case "normalize_utility":
		out, err = pg_query.NormalizeUtility(input)
	case "fingerprint":
		out, err = pg_query.Fingerprint(input)
	case "deparse":
		var tree *pg_query.ParseResult
		tree, err = pg_query.Parse(input)
		if err == nil {
			out, err = pg_query.Deparse(tree)
		}
	case "summary", "summary_truncate":
		limit, sql := -1, input
		if suite == "summary_truncate" {
			limit, sql, err = testfile.SplitTruncateLimit(input)
			if err != nil {
				return "BAD CASE: " + err.Error()
			}
		}
		var res *pg_query.SummaryResult
		res, err = pg_query.Summary(sql, limit)
		if err == nil {
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
			out = testfile.RenderSummary(e)
		}
	case "plpgsql":
		out, err = pg_query.ParsePlPgSqlToJSON(input)
	default:
		panic("difftodo: unsupported suite " + suite)
	}
	if err != nil {
		var pe *parser.Error
		if errors.As(err, &pe) {
			return testfile.RenderError(testfile.ErrorExpectation{
				Message:   pe.Message,
				Cursorpos: pe.Cursorpos,
				Filename:  pe.Filename,
				Funcname:  pe.Funcname,
				Context:   pe.Context,
			})
		}
		return "UNIMPLEMENTED: " + err.Error()
	}
	return out
}
