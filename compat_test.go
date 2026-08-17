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

// TestScanTokens pins one token stream end-to-end through the public Scan
// API (pg_query_go's TestScan is a smoke test; the corpus goldens carry the
// exhaustive coverage).
func TestScanTokens(t *testing.T) {
	result, err := pg_query.Scan("SELECT update AS left /* comment */ FROM between")
	if err != nil {
		t.Fatal(err)
	}
	if result.Version != 180004 {
		t.Fatalf("ScanResult.Version = %d, want 180004", result.Version)
	}
	type tok struct {
		start, end  int32
		token       pg_query.Token
		keywordKind pg_query.KeywordKind
	}
	expected := []tok{
		{0, 6, pg_query.Token_SELECT, pg_query.KeywordKind_RESERVED_KEYWORD},
		{7, 13, pg_query.Token_UPDATE, pg_query.KeywordKind_UNRESERVED_KEYWORD},
		{14, 16, pg_query.Token_AS, pg_query.KeywordKind_RESERVED_KEYWORD},
		{17, 21, pg_query.Token_LEFT, pg_query.KeywordKind_TYPE_FUNC_NAME_KEYWORD},
		{22, 35, pg_query.Token_C_COMMENT, pg_query.KeywordKind_NO_KEYWORD},
		{36, 40, pg_query.Token_FROM, pg_query.KeywordKind_RESERVED_KEYWORD},
		{41, 48, pg_query.Token_BETWEEN, pg_query.KeywordKind_COL_NAME_KEYWORD},
	}
	if len(result.Tokens) != len(expected) {
		t.Fatalf("got %d tokens, want %d", len(result.Tokens), len(expected))
	}
	for i, e := range expected {
		g := result.Tokens[i]
		if g.Start != e.start || g.End != e.end || g.Token != e.token || g.KeywordKind != e.keywordKind {
			t.Errorf("token %d = (%d %d %v %v), want (%d %d %v %v)",
				i, g.Start, g.End, g.Token, g.KeywordKind, e.start, e.end, e.token, e.keywordKind)
		}
	}
}

// TestSplitWithParser mirrors pg_query_go's parser-based split tests.
func TestSplitWithParser(t *testing.T) {
	stmts, err := pg_query.SplitWithParser("CREATE RULE x AS ON SELECT TO tbl DO (SELECT 1; SELECT 2); SELECT 3", true)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"CREATE RULE x AS ON SELECT TO tbl DO (SELECT 1; SELECT 2)", "SELECT 3"}
	if len(stmts) != len(want) || stmts[0] != want[0] || stmts[1] != want[1] {
		t.Fatalf("SplitWithParser = %q, want %q", stmts, want)
	}
}

// TestIsUtilityStmt pins the statement classification split.
func TestIsUtilityStmt(t *testing.T) {
	got, err := pg_query.IsUtilityStmt("SELECT 1; VACUUM; INSERT INTO t VALUES (1); CREATE TABLE t (a int)")
	if err != nil {
		t.Fatal(err)
	}
	want := []bool{false, true, false, true}
	if len(got) != len(want) {
		t.Fatalf("IsUtilityStmt returned %d results, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("IsUtilityStmt[%d] = %v, want %v", i, got[i], want[i])
		}
	}
}

// TestSplitWithScanner mirrors pg_query_go's scanner-based split tests.
func TestSplitWithScanner(t *testing.T) {
	stmts, err := pg_query.SplitWithScanner("SELECT /* comment with ; */ 1; SELECT 2", true)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"SELECT /* comment with ; */ 1", "SELECT 2"}
	if len(stmts) != len(want) || stmts[0] != want[0] || stmts[1] != want[1] {
		t.Fatalf("SplitWithScanner = %q, want %q", stmts, want)
	}
}

// TestHashXXH3_64 carries pg_query_go's exact hard-coded vectors
// (fingerprint_test.go, v6.2.2).
func TestHashXXH3_64(t *testing.T) {
	for _, v := range []struct {
		input    string
		seed     uint64
		expected uint64
	}{
		{"TEST", 0, 11717748491247689214},
		{"TEST", 42, 10412276358662179996},
		{"Something else", 0, 14679351602596009561},
	} {
		if got := pg_query.HashXXH3_64([]byte(v.input), v.seed); got != v.expected {
			t.Errorf("HashXXH3_64(%q, %d) = %d, want %d", v.input, v.seed, got, v.expected)
		}
	}
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
