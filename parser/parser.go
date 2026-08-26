// Package parser mirrors pg_query_go's parser subpackage: the Error type and
// the byte-level (protobuf-encoded) entry points.
//
// Upstream these exist because cgo speaks bytes; here they are thin
// conveniences over the native Go implementations. The signatures are kept
// identical so code importing pg_query_go/v6/parser migrates with an import
// swap.
package parser

import (
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"google.golang.org/protobuf/proto"

	"github.com/sqlc-dev/oliphant/ast"
	"github.com/sqlc-dev/oliphant/internal/deparse"
	"github.com/sqlc-dev/oliphant/internal/emit"
	"github.com/sqlc-dev/oliphant/internal/fingerprint"
	"github.com/sqlc-dev/oliphant/internal/lexer"
	"github.com/sqlc-dev/oliphant/internal/normalize"
	"github.com/sqlc-dev/oliphant/internal/parse"
	"github.com/sqlc-dev/oliphant/internal/plpgsql"
	"github.com/sqlc-dev/oliphant/internal/summary"
	"github.com/sqlc-dev/oliphant/internal/xxh3"
)

// pgVersionNum is the pinned PG_VERSION_NUM (PostgreSQL 18.4, libpg_query
// 18.0.0), reported in ScanResult.Version and ParseResult.Version.
const pgVersionNum = 180004

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

// ParseToTree parses the input and returns the tree the Go parser built,
// without the protobuf round trip Parse would otherwise make.
//
// This is oliphant's one addition to pg_query_go's surface (see PLAN.md § 1).
// Upstream's Parse decodes bytes because the parse tree is built in C and
// crosses the cgo boundary as protobuf; here the tree is already a Go value,
// so marshalling it only to unmarshal it back costs about three quarters of
// Parse's time and doubles its allocations for no observable difference.
//
// "No observable difference" holds with one exception, which is why the
// round trip survives as a fallback: proto3 string fields must be valid
// UTF-8, so a tree carrying a raw invalid byte fails to marshal and upstream
// reports that as an error from Parse. Such a byte can only have come from
// the input verbatim — the scanner rejects escape sequences that would
// synthesize one (E'\xff' raises "invalid byte sequence for encoding
// UTF8"), and identifier truncation is pg_mbcliplen-equivalent, so it never
// splits a character — which makes utf8.ValidString a sufficient guard.
//
// One difference remains, and is not observable through pg_query_go's own
// API: the tree may share subtrees, where the round trip silently deep-copied
// them (a multi-column UPDATE ... SET (a, b, c) = (...) points every
// ResTarget at one MultiAssignRef source, exactly as the C tree does).
// Callers that only read the tree cannot tell; callers that mutate one now
// can. See TestParseToTreeSharesMultiAssignSource.
func ParseToTree(input string) (*ast.ParseResult, error) {
	if !utf8.ValidString(input) {
		return parseViaProtobuf(input)
	}
	tree, perr := parse.Parse(input)
	if perr != nil {
		return nil, scanErr(perr)
	}
	return tree, nil
}

// ParseFileResult is a parse tree together with the comments the grammar
// never sees, both from one pass over the input.
type ParseFileResult struct {
	*ast.ParseResult

	// Comments holds the input's SQL_COMMENT and C_COMMENT tokens in source
	// order, positioned by the same byte offsets the tree's location fields
	// carry. Token.Start and Token.End bound each comment as written, marker
	// included and terminating newline excluded; the text is input[Start:End].
	Comments []*ast.ScanToken
}

// ParseFile parses the input and returns its tree along with its comments.
//
// This is a second oliphant addition to pg_query_go's surface (see PLAN.md
// § 1), for consumers that need the comments back — a formatter reprinting a
// query file has to put them where they were written. They are already
// scanned: libpg_query's patch 04 has the scanner emit them as tokens and
// base_yylex drop them on the way to the grammar, which is exactly what Scan
// exposes. Scan and Parse together would answer this, at the cost of lexing
// the input twice and marshalling the whole token stream to protobuf to
// recover a handful of comments; ParseFile keeps them from the pass the
// parse already makes.
//
// A parse error returns no comments: the scan stops where the grammar does,
// so what had been collected covers only part of the input.
//
// Unlike Parse, ParseFile does not reject an input whose strings are not
// valid UTF-8. That constraint is protobuf's, and upstream surfaces it only
// because the tree reaches Go over the wire; it is not the grammar's, and
// nothing here encodes the tree.
func ParseFile(input string) (*ParseFileResult, error) {
	tree, comments, perr := parse.ParseWithComments(input)
	if perr != nil {
		return nil, scanErr(perr)
	}
	out := &ParseFileResult{ParseResult: tree}
	if len(comments) > 0 {
		out.Comments = make([]*ast.ScanToken, 0, len(comments))
		for _, c := range comments {
			out.Comments = append(out.Comments, &ast.ScanToken{
				Start:       c.Start,
				End:         c.End,
				Token:       c.Kind,
				KeywordKind: c.KeywordKind,
			})
		}
	}
	return out, nil
}

// parseViaProtobuf is upstream's Parse body: marshal the tree and decode it
// again, so an input that cannot round-trip fails exactly as it does there.
func parseViaProtobuf(input string) (tree *ast.ParseResult, err error) {
	protobufTree, err := ParseToProtobuf(input)
	if err != nil {
		return
	}
	tree = &ast.ParseResult{}
	err = proto.Unmarshal(protobufTree, tree)
	return
}

func ParseToProtobuf(input string) ([]byte, error) {
	tree, perr := parse.Parse(input)
	if perr != nil {
		return nil, scanErr(perr)
	}
	return proto.Marshal(tree)
}

// DeparseFromProtobuf is pg_query_deparse_protobuf: decode the tree and
// render it back to SQL, statements joined with "; ".
func DeparseFromProtobuf(input []byte) (result string, err error) {
	tree := &ast.ParseResult{}
	if err := proto.Unmarshal(input, tree); err != nil {
		return "", err
	}
	out, derr := deparse.Deparse(tree)
	if derr != nil {
		// The C path surfaces elog messages as PgQueryError; only Message is
		// populated meaningfully here (deparse errors carry no cursor).
		return "", &Error{Message: derr.Error()}
	}
	return out, nil
}

// ParsePlPgSqlToJSON is pg_query_parse_plpgsql: compile every CREATE
// FUNCTION/PROCEDURE and DO statement with the PL/pgSQL parser and render
// the JSON array of function dumps.
func ParsePlPgSqlToJSON(input string) (result string, err error) {
	out, perr := plpgsql.ParseToJSON(input)
	if perr != nil {
		return "", &Error{
			Message:   perr.Message,
			Filename:  perr.Filename,
			Funcname:  perr.Funcname,
			Cursorpos: perr.Cursorpos,
			Context:   perr.Context,
		}
	}
	return out, nil
}

// Normalize is pg_query_normalize: constants become $n parameter references.
func Normalize(input string) (result string, err error) {
	out, perr := normalize.Normalize(input, false)
	if perr != nil {
		return "", scanErr(perr)
	}
	return out, nil
}

// NormalizeUtility is pg_query_normalize_utility: only utility statements
// are normalized.
func NormalizeUtility(input string) (result string, err error) {
	out, perr := normalize.Normalize(input, true)
	if perr != nil {
		return "", scanErr(perr)
	}
	return out, nil
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

// FingerprintToUInt64 is pg_query_fingerprint: parse, then hash the raw tree
// (version-3 fingerprint).
func FingerprintToUInt64(input string) (result uint64, err error) {
	tree, empties, perr := parse.ParseTracked(input)
	if perr != nil {
		return 0, scanErr(perr)
	}
	return fingerprint.TreeWithEmpties(tree, empties), nil
}

// FingerprintToHexStr renders the fingerprint the way the C implementation
// prints XXH64_canonical_t: big-endian, zero-padded hex.
func FingerprintToHexStr(input string) (result string, err error) {
	fp, err := FingerprintToUInt64(input)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%016x", fp), nil
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

// SummaryToProtobuf is pg_query_summary: parse, walk for tables/aliases/
// CTEs/functions/filter columns/statement types, and — unless truncateLimit
// is -1 — produce the smart-truncated query text.
func SummaryToProtobuf(input string, truncateLimit int) ([]byte, error) {
	tree, empties, perr := parse.ParseTracked(input)
	if perr != nil {
		return nil, scanErr(perr)
	}
	res, serr := summary.SummarizeWithEmpties(tree, truncateLimit, empties)
	if serr != nil {
		var se *summary.Error
		if errors.As(serr, &se) {
			return nil, &Error{
				Message:  se.Message,
				Filename: se.Filename,
				Funcname: se.Funcname,
			}
		}
		return nil, &Error{Message: serr.Error()}
	}
	return proto.Marshal(res)
}
