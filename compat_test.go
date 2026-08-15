package oliphant_test

import (
	"strings"
	"testing"

	pg_query "github.com/sqlc-dev/oliphant"
)

// skipUnlessImplemented gates the ported pg_query_go tests until the parser
// milestones land. Compiling their expected-tree literals against oliphant's
// types is what milestone 1 guarantees; running them green is milestones 3+.
// Remove this guard (and let the tests run) as each entry point comes alive.
func skipUnlessImplemented(t *testing.T) {
	t.Helper()
	_, err := pg_query.Parse("SELECT 1")
	if err != nil && strings.Contains(err.Error(), "not implemented") {
		t.Skip("parser not implemented yet (see PLAN.md milestones)")
	}
}

// TestNotImplementedErrors pins the milestone-1 API contract: every entry
// point exists with pg_query_go's exact signature and fails loudly (not
// silently) until its milestone lands.
func TestNotImplementedErrors(t *testing.T) {
	check := func(name string, err error) {
		t.Helper()
		if err == nil {
			t.Fatalf("%s: expected a not-implemented error, got nil", name)
		}
		if !strings.Contains(err.Error(), "not implemented") {
			t.Fatalf("%s: unexpected error %q", name, err)
		}
	}

	_, err := pg_query.Parse("SELECT 1")
	check("Parse", err)
	_, err = pg_query.ParseToJSON("SELECT 1")
	check("ParseToJSON", err)
	_, err = pg_query.Scan("SELECT 1")
	check("Scan", err)
	_, err = pg_query.Deparse(&pg_query.ParseResult{})
	check("Deparse", err)
	_, err = pg_query.Normalize("SELECT 1")
	check("Normalize", err)
	_, err = pg_query.NormalizeUtility("SELECT 1")
	check("NormalizeUtility", err)
	_, err = pg_query.Fingerprint("SELECT 1")
	check("Fingerprint", err)
	_, err = pg_query.FingerprintToUInt64("SELECT 1")
	check("FingerprintToUInt64", err)
	_, err = pg_query.SplitWithScanner("SELECT 1", false)
	check("SplitWithScanner", err)
	_, err = pg_query.SplitWithParser("SELECT 1", false)
	check("SplitWithParser", err)
	_, err = pg_query.IsUtilityStmt("SELECT 1")
	check("IsUtilityStmt", err)
	_, err = pg_query.ParsePlPgSqlToJSON("SELECT 1")
	check("ParsePlPgSqlToJSON", err)
	_, err = pg_query.Summary("SELECT 1", 0)
	check("Summary", err)

	defer func() {
		if recover() == nil {
			t.Fatal("HashXXH3_64: expected a not-implemented panic")
		}
	}()
	pg_query.HashXXH3_64([]byte("x"), 0)
}

// TestMakeFuncs smoke-checks the verbatim-ported constructors build the same
// node shapes as upstream.
func TestMakeFuncs(t *testing.T) {
	n := pg_query.MakeAConstIntNode(1, 7)
	if n.GetAConst().GetIval().GetIval() != 1 || n.GetAConst().GetLocation() != 7 {
		t.Fatalf("MakeAConstIntNode built unexpected node: %v", n)
	}
	s := pg_query.MakeStrNode("abc")
	if s.GetString_().GetSval() != "abc" {
		t.Fatalf("MakeStrNode built unexpected node: %v", s)
	}
}
