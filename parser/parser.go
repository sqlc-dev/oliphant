// Package parser mirrors pg_query_go's parser subpackage: the Error type and
// the byte-level (protobuf-encoded) entry points.
//
// Upstream these exist because cgo speaks bytes; here they are thin
// conveniences over the native Go implementations. The signatures are kept
// identical so code importing pg_query_go/v6/parser migrates with an import
// swap.
package parser

import (
	"fmt"
	"strings"

	"google.golang.org/protobuf/proto"

	"github.com/sqlc-dev/oliphant/ast"
	"github.com/sqlc-dev/oliphant/internal/emit"
	"github.com/sqlc-dev/oliphant/internal/lexer"
	"github.com/sqlc-dev/oliphant/internal/parse"
	"github.com/sqlc-dev/oliphant/internal/xxh3"
)

// pgVersionNum is the pinned PG_VERSION_NUM (PostgreSQL 17.7, libpg_query
// 17-6.2.2), reported in ScanResult.Version and ParseResult.Version.
const pgVersionNum = 170007

// scanErr converts a lexer error into the public Error, mirroring
// pg_query_scan.c's PG_CATCH block (Lineno is a C source line number
// upstream and deliberately not reproduced).
func scanErr(e *lexer.Error) *Error {
	return &Error{
		Message:   e.Message,
		Filename:  e.Filename,
		Funcname:  e.Funcname,
		Cursorpos: e.Cursorpos,
	}
}

// Error mirrors pg_query_go's parser.Error field-for-field.
type Error struct {
	Message   string // exception message
	Funcname  string // source function of exception (e.g. SearchSysCache)
	Filename  string // source of exception (e.g. parse.l)
	Lineno    int    // source of exception (e.g. 104)
	Cursorpos int    // char in query at which exception occurred
	Context   string // additional context (optional, can be NULL)
}

func (e *Error) Error() string {
	return e.Message
}

// errNotImplemented marks entry points whose milestone has not landed yet
// (see PLAN.md § Milestones). It deliberately does not return *Error: these
// are not parse failures of the input.
func errNotImplemented(name string) error {
	return fmt.Errorf("oliphant: %s is not implemented yet", name)
}

func ParseToJSON(input string) (result string, err error) {
	tree, perr := parse.Parse(input)
	if perr != nil {
		return "", scanErr(perr)
	}
	return emit.ParseResult(tree), nil
}

// ScanToProtobuf lexes the input with the core scanner exactly as
// pg_query_scan.c does: raw core_yylex tokens (comments included, no
// base_yylex filtering), start/end byte offsets, and the keyword kind.
func ScanToProtobuf(input string) (result []byte, err error) {
	s := lexer.New(input)
	scan := &ast.ScanResult{Version: pgVersionNum}
	for {
		tok, lerr := s.Next()
		if lerr != nil {
			return nil, scanErr(lerr)
		}
		if tok.Kind == 0 {
			break
		}
		scan.Tokens = append(scan.Tokens, &ast.ScanToken{
			Start:       tok.Start,
			End:         tok.End,
			Token:       tok.Kind,
			KeywordKind: tok.KeywordKind,
		})
	}
	return proto.Marshal(scan)
}

func ParseToProtobuf(input string) ([]byte, error) {
	tree, perr := parse.Parse(input)
	if perr != nil {
		return nil, scanErr(perr)
	}
	return proto.Marshal(tree)
}

func DeparseFromProtobuf(input []byte) (result string, err error) {
	return "", errNotImplemented("DeparseFromProtobuf")
}

func ParsePlPgSqlToJSON(input string) (result string, err error) {
	return "", errNotImplemented("ParsePlPgSqlToJSON")
}

func Normalize(input string) (result string, err error) {
	return "", errNotImplemented("Normalize")
}

func NormalizeUtility(input string) (result string, err error) {
	return "", errNotImplemented("NormalizeUtility")
}

// SplitWithScanner is pg_query_split.c's pg_query_split_with_scanner: a
// lexer-level statement splitter. A statement ends at a top-level ";" (or
// EOF) and is only emitted if it contained at least one keyword.
func SplitWithScanner(input string, trimSpace bool) (result []string, err error) {
	s := lexer.New(input)
	keywordBeforeTerminator := false
	openParens := 0
	stmtStart := 0
	for {
		tok, lerr := s.Next()
		if lerr != nil {
			return nil, scanErr(lerr)
		}
		kind := tok.Kind
		switch {
		case tok.KeywordKind != ast.KeywordKind_NO_KEYWORD:
			keywordBeforeTerminator = true
		case kind == '(':
			openParens++
		case kind == ')':
			openParens--
		case keywordBeforeTerminator && openParens == 0 && (kind == ';' || kind == 0):
			stmt := input[stmtStart:tok.Start]
			if trimSpace {
				stmt = strings.TrimSpace(stmt)
			}
			result = append(result, stmt)
			stmtStart = int(tok.Start) + 1
			keywordBeforeTerminator = false
		case openParens == 0 && kind == ';':
			// Advance the statement start past an empty statement.
			stmtStart = int(tok.Start) + 1
		}
		if kind == 0 {
			return result, nil
		}
	}
}

// SplitWithParser splits on the RawStmt boundaries of a real parse
// (pg_query_split.c: pg_query_split_with_parser); a zero stmt_len runs to
// the end of the input.
func SplitWithParser(input string, trimSpace bool) (result []string, err error) {
	tree, perr := parse.Parse(input)
	if perr != nil {
		return nil, scanErr(perr)
	}
	for _, raw := range tree.Stmts {
		end := raw.StmtLocation + raw.StmtLen
		if raw.StmtLen == 0 {
			end = int32(len(input))
		}
		stmt := input[raw.StmtLocation:end]
		if trimSpace {
			stmt = strings.TrimSpace(stmt)
		}
		result = append(result, stmt)
	}
	return result, nil
}

func FingerprintToUInt64(input string) (result uint64, err error) {
	return 0, errNotImplemented("FingerprintToUInt64")
}

func FingerprintToHexStr(input string) (result string, err error) {
	return "", errNotImplemented("FingerprintToHexStr")
}

// HashXXH3_64 runs the XXH3 hash function (64-bit variant) on the given
// bytes, with the specified seed.
func HashXXH3_64(input []byte, seed uint64) (result uint64) {
	return xxh3.Hash64Seed(input, seed)
}

// IsUtilityStmt reports, per statement, whether it is a utility statement
// (pg_query_is_utility_stmt.c: everything except SELECT/INSERT/UPDATE/
// DELETE/MERGE).
func IsUtilityStmt(input string) (result []bool, err error) {
	tree, perr := parse.Parse(input)
	if perr != nil {
		return nil, scanErr(perr)
	}
	result = make([]bool, 0, len(tree.Stmts))
	for _, raw := range tree.Stmts {
		switch raw.Stmt.Node.(type) {
		case *ast.Node_SelectStmt, *ast.Node_InsertStmt, *ast.Node_UpdateStmt,
			*ast.Node_DeleteStmt, *ast.Node_MergeStmt:
			result = append(result, false)
		default:
			result = append(result, true)
		}
	}
	return result, nil
}

func SummaryToProtobuf(input string, truncateLimit int) ([]byte, error) {
	return nil, errNotImplemented("SummaryToProtobuf")
}
