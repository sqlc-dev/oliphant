package testfile

import (
	"fmt"
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
