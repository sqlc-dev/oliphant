package lexer

import (
	"testing"

	"github.com/sqlc-dev/oliphant/ast"
)

func filterKinds(t *testing.T, input string) []ast.Token {
	t.Helper()
	f := NewFilter(New(input))
	var kinds []ast.Token
	for {
		tok, err := f.Next()
		if err != nil {
			t.Fatalf("Next(%q): %v", input, err)
		}
		if tok.Kind == 0 {
			return kinds
		}
		kinds = append(kinds, tok.Kind)
	}
}

func TestFilterMerges(t *testing.T) {
	tests := []struct {
		input string
		want  []ast.Token
	}{
		// parser.c: NOT → NOT_LA only before BETWEEN/IN/LIKE/ILIKE/SIMILAR
		{"a NOT BETWEEN", []ast.Token{ast.Token_IDENT, ast.Token_NOT_LA, ast.Token_BETWEEN}},
		{"a NOT IN", []ast.Token{ast.Token_IDENT, ast.Token_NOT_LA, ast.Token_IN_P}},
		{"a NOT LIKE", []ast.Token{ast.Token_IDENT, ast.Token_NOT_LA, ast.Token_LIKE}},
		{"a NOT ILIKE", []ast.Token{ast.Token_IDENT, ast.Token_NOT_LA, ast.Token_ILIKE}},
		{"a NOT SIMILAR", []ast.Token{ast.Token_IDENT, ast.Token_NOT_LA, ast.Token_SIMILAR}},
		{"NOT x", []ast.Token{ast.Token_NOT, ast.Token_IDENT}},
		// parser.c: NULLS_P → NULLS_LA before FIRST/LAST
		{"NULLS FIRST", []ast.Token{ast.Token_NULLS_LA, ast.Token_FIRST_P}},
		{"NULLS LAST", []ast.Token{ast.Token_NULLS_LA, ast.Token_LAST_P}},
		{"NULLS x", []ast.Token{ast.Token_NULLS_P, ast.Token_IDENT}},
		// parser.c: WITH → WITH_LA before TIME/ORDINALITY
		{"WITH TIME", []ast.Token{ast.Token_WITH_LA, ast.Token_TIME}},
		{"WITH ORDINALITY", []ast.Token{ast.Token_WITH_LA, ast.Token_ORDINALITY}},
		{"WITH x", []ast.Token{ast.Token_WITH, ast.Token_IDENT}},
		// parser.c: WITHOUT → WITHOUT_LA before TIME
		{"WITHOUT TIME", []ast.Token{ast.Token_WITHOUT_LA, ast.Token_TIME}},
		// parser.c: FORMAT → FORMAT_LA before JSON
		{"FORMAT JSON", []ast.Token{ast.Token_FORMAT_LA, ast.Token_JSON}},
		// Comment tokens are dropped by the filter, but the lookahead is a
		// raw core_yylex call: a comment BETWEEN a merge pair blocks the
		// merge. That is real pinned-oracle behavior (libpg_query's
		// comments-as-tokens patch): pg_query_go v6.2.2 fails
		// "SELECT 1 WHERE 1 NOT /* c */ IN (2)" with a syntax error where
		// vanilla PostgreSQL would accept it.
		{"NOT /* c */ IN", []ast.Token{ast.Token_NOT, ast.Token_IN_P}},
		{"NOT -- c\nIN", []ast.Token{ast.Token_NOT, ast.Token_IN_P}},
	}
	for _, tc := range tests {
		got := filterKinds(t, tc.input)
		if len(got) != len(tc.want) {
			t.Errorf("%q: got %v, want %v", tc.input, got, tc.want)
			continue
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Errorf("%q: got %v, want %v", tc.input, got, tc.want)
				break
			}
		}
	}
}

func TestFilterUnicodeTokens(t *testing.T) {
	// parser.c: UIDENT/USCONST become IDENT/SCONST with escapes applied.
	check := func(input string, wantKind ast.Token, wantStr string) {
		t.Helper()
		f := NewFilter(New(input))
		tok, err := f.Next()
		if err != nil {
			t.Fatalf("Next(%q): %v", input, err)
		}
		if tok.Kind != wantKind || tok.Str != wantStr {
			t.Errorf("%q: got (%v, %q), want (%v, %q)", input, tok.Kind, tok.Str, wantKind, wantStr)
		}
	}
	check(`U&"d\0061t\+000061"`, ast.Token_IDENT, "data")
	check(`U&'d\0061t\+000061'`, ast.Token_SCONST, "data")
	check(`U&"d!0061t!+000061" UESCAPE '!'`, ast.Token_IDENT, "data")
	check(`U&'d!0061t!+000061' UESCAPE '!'`, ast.Token_SCONST, "data")
	// Surrogate pair (U+1D441 via UTF-16 halves), and doubled escape.
	check(`U&'\d835\dc41'`, ast.Token_SCONST, "\U0001D441")
	check(`U&'a\\b'`, ast.Token_SCONST, `a\b`)

	checkErr := func(input, wantMsg, wantFunc string, wantCursor int) {
		t.Helper()
		f := NewFilter(New(input))
		_, err := f.Next()
		if err == nil {
			t.Fatalf("Next(%q): expected error", input)
		}
		if err.Message != wantMsg || err.Funcname != wantFunc || err.Cursorpos != wantCursor {
			t.Errorf("%q: got (%q, %s, %d), want (%q, %s, %d)",
				input, err.Message, err.Funcname, err.Cursorpos, wantMsg, wantFunc, wantCursor)
		}
	}
	checkErr(`U&'\d835' UESCAPE 42`,
		`UESCAPE must be followed by a simple string literal at or near "42"`,
		"scanner_yyerror", 19)
	checkErr(`U&'\d835' UESCAPE 'zz'`,
		`invalid Unicode escape character at or near "'zz'"`,
		"scanner_yyerror", 19)
	checkErr(`U&'\d835'`, "invalid Unicode surrogate pair", "str_udeescape", 9)
	checkErr(`U&'\00'`, "invalid Unicode escape", "str_udeescape", 4)
	checkErr(`U&'\0000'`, "invalid Unicode escape value", "check_unicode_value", 4)
}
