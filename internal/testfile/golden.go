package testfile

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// Golden expectation encodings shared by cmd/regenerate (writing, from the
// oracle) and the parser corpus harness (comparing, from oliphant's own
// output). Both sides must render identically — bytes are the contract.

// ErrorExpectation is the golden form of a parser.Error. Lineno is excluded
// from conformance by design (upstream's own tests zero it: it is a C source
// line number).
type ErrorExpectation struct {
	Message   string
	Cursorpos int
	Filename  string
	Funcname  string
	Context   string
}

// RenderError encodes an error expectation block:
//
//	ERROR: syntax error at or near "$"
//	CURSORPOS: 8
//	FILENAME: scan.l
//	FUNCNAME: scanner_yyerror
func RenderError(e ErrorExpectation) string {
	if strings.Contains(e.Message, "\n") {
		// Never observed from the oracle; quote defensively rather than
		// producing an unparseable golden.
		e.Message = strconv.Quote(e.Message)
	}
	lines := []string{"ERROR: " + e.Message}
	if e.Cursorpos != 0 {
		lines = append(lines, fmt.Sprintf("CURSORPOS: %d", e.Cursorpos))
	}
	if e.Filename != "" {
		lines = append(lines, "FILENAME: "+e.Filename)
	}
	if e.Funcname != "" {
		lines = append(lines, "FUNCNAME: "+e.Funcname)
	}
	if e.Context != "" {
		lines = append(lines, "CONTEXT: "+strconv.Quote(e.Context))
	}
	return strings.Join(lines, "\n")
}

// IsError reports whether a golden expectation is an error block.
func IsError(expected string) bool {
	return strings.HasPrefix(expected, "ERROR: ")
}

// ParseError decodes an error expectation block written by RenderError.
func ParseError(expected string) (ErrorExpectation, error) {
	var e ErrorExpectation
	if !IsError(expected) {
		return e, fmt.Errorf("not an error expectation: %q", expected)
	}
	for i, line := range strings.Split(expected, "\n") {
		switch {
		case i == 0:
			e.Message = strings.TrimPrefix(line, "ERROR: ")
		case strings.HasPrefix(line, "CURSORPOS: "):
			n, err := strconv.Atoi(strings.TrimPrefix(line, "CURSORPOS: "))
			if err != nil {
				return e, err
			}
			e.Cursorpos = n
		case strings.HasPrefix(line, "FILENAME: "):
			e.Filename = strings.TrimPrefix(line, "FILENAME: ")
		case strings.HasPrefix(line, "FUNCNAME: "):
			e.Funcname = strings.TrimPrefix(line, "FUNCNAME: ")
		case strings.HasPrefix(line, "CONTEXT: "):
			s, err := strconv.Unquote(strings.TrimPrefix(line, "CONTEXT: "))
			if err != nil {
				return e, err
			}
			e.Context = s
		default:
			return e, fmt.Errorf("bad error expectation line %d: %q", i+1, line)
		}
	}
	return e, nil
}

// RenderScanToken encodes one token of a scan golden:
//
//	0 6 SELECT RESERVED_KEYWORD
//
// Token and keyword kind are the protobuf enum value names; a full scan
// golden is these lines joined with "\n".
func RenderScanToken(start, end int32, token, keywordKind string) string {
	return fmt.Sprintf("%d %d %s %s", start, end, token, keywordKind)
}

// RenderSplit encodes a statement-split golden as one statement per line,
// each Go-quoted (statements routinely contain newlines).
func RenderSplit(stmts []string) string {
	lines := make([]string, len(stmts))
	for i, s := range stmts {
		lines[i] = strconv.Quote(s)
	}
	return strings.Join(lines, "\n")
}

// --- summary goldens ---------------------------------------------------------

// SummaryTable / SummaryFunction / SummaryFilterColumn / SummaryExpectation
// mirror the SummaryResult protobuf field-for-field, with Context enums as
// their protobuf value names ("None", "Select", "DML", "DDL", "Call").
type SummaryTable struct {
	Name       string
	SchemaName string
	TableName  string
	Context    string
}

type SummaryFunction struct {
	Name         string
	FunctionName string
	SchemaName   string
	Context      string
}

type SummaryFilterColumn struct {
	SchemaName string
	TableName  string
	Column     string
}

type SummaryExpectation struct {
	Tables         []SummaryTable
	Aliases        map[string]string
	CteNames       []string
	Functions      []SummaryFunction
	FilterColumns  []SummaryFilterColumn
	StatementTypes []string
	TruncatedQuery string
}

// RenderSummary encodes a SummaryResult golden, one entry per line. Repeated
// fields keep their protobuf order; aliases (a protobuf map, unordered by
// definition) are sorted by key. The empty summary renders as "".
//
//	TABLE: name="public.x" schema="public" table="x" ctx=DDL
//	ALIAS: "a"="b"
//	CTE: "cte"
//	FUNCTION: name="pg_catalog.substr" function="substr" schema="pg_catalog" ctx=Call
//	FILTER: schema="" table="t" column="c"
//	STMT: SelectStmt
//	TRUNCATED: "SELECT ..."
func RenderSummary(s SummaryExpectation) string {
	var lines []string
	for _, t := range s.Tables {
		lines = append(lines, fmt.Sprintf("TABLE: name=%q schema=%q table=%q ctx=%s",
			t.Name, t.SchemaName, t.TableName, t.Context))
	}
	keys := make([]string, 0, len(s.Aliases))
	for k := range s.Aliases {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		lines = append(lines, fmt.Sprintf("ALIAS: %q=%q", k, s.Aliases[k]))
	}
	for _, c := range s.CteNames {
		lines = append(lines, fmt.Sprintf("CTE: %q", c))
	}
	for _, f := range s.Functions {
		lines = append(lines, fmt.Sprintf("FUNCTION: name=%q function=%q schema=%q ctx=%s",
			f.Name, f.FunctionName, f.SchemaName, f.Context))
	}
	for _, f := range s.FilterColumns {
		lines = append(lines, fmt.Sprintf("FILTER: schema=%q table=%q column=%q",
			f.SchemaName, f.TableName, f.Column))
	}
	for _, t := range s.StatementTypes {
		lines = append(lines, "STMT: "+t)
	}
	if s.TruncatedQuery != "" {
		lines = append(lines, fmt.Sprintf("TRUNCATED: %q", s.TruncatedQuery))
	}
	return strings.Join(lines, "\n")
}

// --- summary_truncate case inputs --------------------------------------------

// The summary_truncate suite carries the truncation limit as a leading
// comment line in the case input; both cmd/regenerate and the corpus harness
// strip it before calling Summary, so the SQL the oracle and oliphant see is
// byte-identical to the raw statement.
const TruncateLimitPrefix = "-- truncate_limit: "

// WithTruncateLimit prepends the truncation-limit directive to a statement.
func WithTruncateLimit(limit int, sql string) string {
	return fmt.Sprintf("%s%d\n%s", TruncateLimitPrefix, limit, sql)
}

// SplitTruncateLimit strips the truncation-limit directive from a
// summary_truncate case input.
func SplitTruncateLimit(input string) (limit int, sql string, err error) {
	if !strings.HasPrefix(input, TruncateLimitPrefix) {
		return 0, "", fmt.Errorf("summary_truncate input missing %q directive", TruncateLimitPrefix)
	}
	rest := input[len(TruncateLimitPrefix):]
	line, sql, ok := strings.Cut(rest, "\n")
	if !ok {
		return 0, "", fmt.Errorf("summary_truncate input is only a directive line")
	}
	limit, err = strconv.Atoi(line)
	if err != nil {
		return 0, "", fmt.Errorf("bad truncate_limit %q: %w", line, err)
	}
	return limit, sql, nil
}
