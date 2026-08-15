// Package parser mirrors pg_query_go's parser subpackage: the Error type and
// the byte-level (protobuf-encoded) entry points.
//
// Upstream these exist because cgo speaks bytes; here they are thin
// conveniences over the native Go implementations. The signatures are kept
// identical so code importing pg_query_go/v6/parser migrates with an import
// swap.
package parser

import "fmt"

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
	return "", errNotImplemented("ParseToJSON")
}

func ScanToProtobuf(input string) (result []byte, err error) {
	return nil, errNotImplemented("ScanToProtobuf")
}

func ParseToProtobuf(input string) ([]byte, error) {
	return nil, errNotImplemented("ParseToProtobuf")
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

func SplitWithScanner(input string, trimSpace bool) (result []string, err error) {
	return nil, errNotImplemented("SplitWithScanner")
}

func SplitWithParser(input string, trimSpace bool) (result []string, err error) {
	return nil, errNotImplemented("SplitWithParser")
}

func FingerprintToUInt64(input string) (result uint64, err error) {
	return 0, errNotImplemented("FingerprintToUInt64")
}

func FingerprintToHexStr(input string) (result string, err error) {
	return "", errNotImplemented("FingerprintToHexStr")
}

// HashXXH3_64 runs the XXH3 hash function (64-bit variant) on the given
// bytes, with the specified seed. Ships with the lexer milestone
// (internal/xxh3).
func HashXXH3_64(input []byte, seed uint64) (result uint64) {
	panic("oliphant: HashXXH3_64 is not implemented yet")
}

func IsUtilityStmt(input string) (result []bool, err error) {
	return nil, errNotImplemented("IsUtilityStmt")
}

func SummaryToProtobuf(input string, truncateLimit int) ([]byte, error) {
	return nil, errNotImplemented("SummaryToProtobuf")
}
