// Package oliphant is a pure Go (no cgo, no wasm) drop-in replacement for
// github.com/pganalyze/pg_query_go/v6: same exported functions, same
// generated protobuf types, same JSON output, same error type, same
// fingerprints.
//
// Consumers alias the import, exactly as pg_query_go users already do:
//
//	import pg_query "github.com/sqlc-dev/oliphant"
//
// This file mirrors pg_query_go's pg_query.go function-for-function; the
// bodies delegate to the parser subpackage just as upstream's do.
package oliphant

import (
	"google.golang.org/protobuf/proto"

	"github.com/sqlc-dev/oliphant/parser"
)

func Scan(input string) (result *ScanResult, err error) {
	protobufScan, err := parser.ScanToProtobuf(input)
	if err != nil {
		return
	}
	result = &ScanResult{}
	err = proto.Unmarshal(protobufScan, result)
	return
}

// ParseToJSON - Parses the given SQL statement into a parse tree (JSON format)
func ParseToJSON(input string) (result string, err error) {
	return parser.ParseToJSON(input)
}

// Parse the given SQL statement into a parse tree (Go struct format)
func Parse(input string) (tree *ParseResult, err error) {
	protobufTree, err := parser.ParseToProtobuf(input)
	if err != nil {
		return
	}

	tree = &ParseResult{}
	err = proto.Unmarshal(protobufTree, tree)
	return
}

// Deparse - Deparses a given Go parse tree into a SQL statement
func Deparse(tree *ParseResult) (output string, err error) {
	protobufTree, err := proto.Marshal(tree)
	if err != nil {
		return
	}

	output, err = parser.DeparseFromProtobuf(protobufTree)
	return
}

// ParsePlPgSqlToJSON - Parses the given PL/pgSQL function statement into a parse tree (JSON format)
func ParsePlPgSqlToJSON(input string) (result string, err error) {
	return parser.ParsePlPgSqlToJSON(input)
}

// Normalize the passed SQL statement to replace constant values with $n parameter references
func Normalize(input string) (result string, err error) {
	return parser.Normalize(input)
}

// NormalizeUtility - Normalize the passed utility statement to replace constant values with $n parameter references
func NormalizeUtility(input string) (result string, err error) {
	return parser.NormalizeUtility(input)
}

// Fingerprint - Fingerprint the passed SQL statement to a hex string
func Fingerprint(input string) (result string, err error) {
	return parser.FingerprintToHexStr(input)
}

// FingerprintToUInt64 - Fingerprint the passed SQL statement to a uint64
func FingerprintToUInt64(input string) (result uint64, err error) {
	return parser.FingerprintToUInt64(input)
}

// HashXXH3_64 - Helper method to run XXH3 hash function (64-bit variant) on the given bytes, with the specified seed
func HashXXH3_64(input []byte, seed uint64) (result uint64) {
	return parser.HashXXH3_64(input, seed)
}

func SplitWithScanner(input string, trimSpace bool) (result []string, err error) {
	return parser.SplitWithScanner(input, trimSpace)
}

func SplitWithParser(input string, trimSpace bool) (result []string, err error) {
	return parser.SplitWithParser(input, trimSpace)
}

// IsUtilityStmt - Determines whether each statement in the query is a utility statement
//
// Returns a slice of booleans, one for each statement in the query.
// true = utility statement / DDL, false = SELECT / INSERT / UPDATE / DELETE / MERGE
func IsUtilityStmt(input string) (result []bool, err error) {
	return parser.IsUtilityStmt(input)
}

// Summary - Extracts summary information from SQL statement
//
// Optionally, you can pass a positive numbered truncateLimit to return a
// "smart" truncated version of the input statement that is at most limit length.
func Summary(input string, truncateLimit int) (result *SummaryResult, err error) {
	protobufSummary, err := parser.SummaryToProtobuf(input, truncateLimit)
	if err != nil {
		return
	}
	result = &SummaryResult{}
	err = proto.Unmarshal(protobufSummary, result)
	return
}
