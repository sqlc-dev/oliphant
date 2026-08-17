// Command oracle wraps the pinned pg_query_go v6.2.2 (cgo — the real
// libpg_query 17-6.2.2) behind a JSONL stdin/stdout protocol. It is the
// oracle every golden in parser/testdata derives from; nothing in the main
// module ever links it.
//
// This is a SEPARATE go module on purpose: the main module must stay cgo-free
// forever, and `go build ./...` at the repo root must never compile C.
//
// Protocol: one JSON request per line on stdin, one JSON response per line on
// stdout, in order. See the req/res types.
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"

	pg_query "github.com/pganalyze/pg_query_go/v6"
	"github.com/pganalyze/pg_query_go/v6/parser"
)

type req struct {
	Op        string `json:"op"`
	SQL       string `json:"sql"`
	TrimSpace bool   `json:"trim_space,omitempty"`
	// TruncateLimit is the summary op's truncation limit; nil means -1
	// (no truncation), matching pg_query.Summary's convention.
	TruncateLimit *int `json:"truncate_limit,omitempty"`
}

type oracleError struct {
	Message   string `json:"message"`
	Cursorpos int    `json:"cursorpos,omitempty"`
	Filename  string `json:"filename,omitempty"`
	Funcname  string `json:"funcname,omitempty"`
	Context   string `json:"context,omitempty"`
}

type token struct {
	Start       int32  `json:"start"`
	End         int32  `json:"end"`
	Token       string `json:"token"`
	KeywordKind string `json:"keyword_kind"`
}

type res struct {
	Text    *string      `json:"text,omitempty"`
	Stmts   []string     `json:"stmts,omitempty"`
	Tokens  []token      `json:"tokens,omitempty"`
	Summary *summaryRes  `json:"summary,omitempty"`
	Err     *oracleError `json:"error,omitempty"`
}

// summaryRes mirrors SummaryResult field-for-field, with Context enums
// rendered as their protobuf value names.
type summaryTable struct {
	Name       string `json:"name"`
	SchemaName string `json:"schema_name"`
	TableName  string `json:"table_name"`
	Context    string `json:"context"`
}

type summaryFunction struct {
	Name         string `json:"name"`
	FunctionName string `json:"function_name"`
	SchemaName   string `json:"schema_name"`
	Context      string `json:"context"`
}

type summaryFilterColumn struct {
	SchemaName string `json:"schema_name"`
	TableName  string `json:"table_name"`
	Column     string `json:"column"`
}

type summaryRes struct {
	Tables         []summaryTable        `json:"tables"`
	Aliases        map[string]string     `json:"aliases"`
	CteNames       []string              `json:"cte_names"`
	Functions      []summaryFunction     `json:"functions"`
	FilterColumns  []summaryFilterColumn `json:"filter_columns"`
	StatementTypes []string              `json:"statement_types"`
	TruncatedQuery string                `json:"truncated_query"`
}

func main() {
	dec := json.NewDecoder(os.Stdin)
	enc := json.NewEncoder(os.Stdout)
	for {
		var r req
		if err := dec.Decode(&r); err != nil {
			if errors.Is(err, io.EOF) {
				return
			}
			fmt.Fprintln(os.Stderr, "oracle: bad request:", err)
			os.Exit(1)
		}
		if err := enc.Encode(handle(r)); err != nil {
			fmt.Fprintln(os.Stderr, "oracle: encode:", err)
			os.Exit(1)
		}
	}
}

func handle(r req) res {
	switch r.Op {
	case "parse_json":
		out, err := pg_query.ParseToJSON(r.SQL)
		if err != nil {
			return errRes(err)
		}
		return res{Text: &out}
	case "scan":
		sr, err := pg_query.Scan(r.SQL)
		if err != nil {
			return errRes(err)
		}
		toks := make([]token, 0, len(sr.Tokens))
		for _, t := range sr.Tokens {
			toks = append(toks, token{
				Start:       t.Start,
				End:         t.End,
				Token:       t.Token.String(),
				KeywordKind: t.KeywordKind.String(),
			})
		}
		return res{Tokens: toks}
	case "normalize":
		out, err := pg_query.Normalize(r.SQL)
		if err != nil {
			return errRes(err)
		}
		return res{Text: &out}
	case "normalize_utility":
		out, err := pg_query.NormalizeUtility(r.SQL)
		if err != nil {
			return errRes(err)
		}
		return res{Text: &out}
	case "fingerprint":
		out, err := pg_query.Fingerprint(r.SQL)
		if err != nil {
			return errRes(err)
		}
		return res{Text: &out}
	case "deparse":
		tree, err := pg_query.Parse(r.SQL)
		if err != nil {
			return errRes(err)
		}
		out, err := pg_query.Deparse(tree)
		if err != nil {
			return errRes(err)
		}
		return res{Text: &out}
	case "split_scanner":
		stmts, err := pg_query.SplitWithScanner(r.SQL, r.TrimSpace)
		if err != nil {
			return errRes(err)
		}
		return res{Stmts: emptyNotNil(stmts)}
	case "split_parser":
		stmts, err := pg_query.SplitWithParser(r.SQL, r.TrimSpace)
		if err != nil {
			return errRes(err)
		}
		return res{Stmts: emptyNotNil(stmts)}
	case "summary":
		limit := -1
		if r.TruncateLimit != nil {
			limit = *r.TruncateLimit
		}
		sr, err := pg_query.Summary(r.SQL, limit)
		if err != nil {
			return errRes(err)
		}
		out := &summaryRes{
			Aliases:        sr.Aliases,
			CteNames:       sr.CteNames,
			StatementTypes: sr.StatementTypes,
			TruncatedQuery: sr.TruncatedQuery,
		}
		for _, t := range sr.Tables {
			out.Tables = append(out.Tables, summaryTable{
				Name:       t.Name,
				SchemaName: t.SchemaName,
				TableName:  t.TableName,
				Context:    t.Context.String(),
			})
		}
		for _, f := range sr.Functions {
			out.Functions = append(out.Functions, summaryFunction{
				Name:         f.Name,
				FunctionName: f.FunctionName,
				SchemaName:   f.SchemaName,
				Context:      f.Context.String(),
			})
		}
		for _, f := range sr.FilterColumns {
			out.FilterColumns = append(out.FilterColumns, summaryFilterColumn{
				SchemaName: f.SchemaName,
				TableName:  f.TableName,
				Column:     f.Column,
			})
		}
		return res{Summary: out}
	case "parse_plpgsql":
		out, err := pg_query.ParsePlPgSqlToJSON(r.SQL)
		if err != nil {
			return errRes(err)
		}
		return res{Text: &out}
	default:
		return res{Err: &oracleError{Message: "unknown op " + r.Op}}
	}
}

func emptyNotNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func errRes(err error) res {
	var pe *parser.Error
	if errors.As(err, &pe) {
		return res{Err: &oracleError{
			Message:   pe.Message,
			Cursorpos: pe.Cursorpos,
			Filename:  pe.Filename,
			Funcname:  pe.Funcname,
			Context:   pe.Context,
		}}
	}
	return res{Err: &oracleError{Message: err.Error()}}
}
