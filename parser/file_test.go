package parser_test

import (
	"errors"
	"path/filepath"
	"testing"

	"google.golang.org/protobuf/proto"

	pg_query "github.com/sqlc-dev/oliphant"
	"github.com/sqlc-dev/oliphant/ast"
	"github.com/sqlc-dev/oliphant/internal/testfile"
	"github.com/sqlc-dev/oliphant/parser"
)

// scanComments is the comment tokens Scan reports, which is what ParseFile
// must reproduce: Scan runs the core scanner over the whole input with no
// grammar attached, and its token stream is pinned against the oracle by the
// scan corpus.
func scanComments(t *testing.T, input string) []*ast.ScanToken {
	t.Helper()
	res, err := pg_query.Scan(input)
	if err != nil {
		return nil
	}
	var out []*ast.ScanToken
	for _, tok := range res.Tokens {
		if tok.Token == ast.Token_SQL_COMMENT || tok.Token == ast.Token_C_COMMENT {
			out = append(out, tok)
		}
	}
	return out
}

// TestParseFileMatchesScan is the contract: on every corpus input that
// parses, the comments ParseFile keeps from the parse's own scan are exactly
// the ones a separate Scan reports, and the tree is the one ParseToTree
// returns. Nothing is dropped by the grammar's filter, nothing is invented.
func TestParseFileMatchesScan(t *testing.T) {
	var parsed, withComments int
	for _, path := range treeCorpusFiles(t) {
		cases, err := testfile.Read(path)
		if err != nil {
			t.Fatal(err)
		}
		rel, _ := filepath.Rel("testdata", path)
		for _, c := range cases {
			file, fileErr := parser.ParseFile(c.Input)
			tree, treeErr := parser.ParseToTree(c.Input)
			if (fileErr == nil) != (treeErr == nil) {
				t.Errorf("%s case %s: ParseFile err=%v, ParseToTree err=%v\ninput:\n%s",
					rel, c.Name, fileErr, treeErr, c.Input)
				continue
			}
			if fileErr != nil {
				if fileErr.Error() != treeErr.Error() {
					t.Errorf("%s case %s: ParseFile err %q, ParseToTree err %q",
						rel, c.Name, fileErr, treeErr)
				}
				continue
			}
			parsed++
			if !proto.Equal(file.ParseResult, tree) {
				t.Errorf("%s case %s: ParseFile tree differs from ParseToTree\ninput:\n%s",
					rel, c.Name, c.Input)
			}
			want := scanComments(t, c.Input)
			if len(want) > 0 {
				withComments++
			}
			if len(file.Comments) != len(want) {
				t.Errorf("%s case %s: got %d comments, Scan reports %d\ninput:\n%s",
					rel, c.Name, len(file.Comments), len(want), c.Input)
				continue
			}
			for i := range want {
				if !proto.Equal(file.Comments[i], want[i]) {
					t.Errorf("%s case %s: comment %d = %v, Scan reports %v\ninput:\n%s",
						rel, c.Name, i, file.Comments[i], want[i], c.Input)
				}
			}
			// Comments are ordered, disjoint and inside the input.
			prev := int32(-1)
			for i, c2 := range file.Comments {
				if c2.Start < prev || c2.End < c2.Start || int(c2.End) > len(c.Input) {
					t.Errorf("%s case %s: comment %d spans [%d,%d), previous ended %d, input is %d bytes",
						rel, c.Name, i, c2.Start, c2.End, prev, len(c.Input))
				}
				prev = c2.End
			}
		}
	}
	t.Logf("compared %d parsed cases, %d of them carrying comments", parsed, withComments)
}

// TestParseFilePlacement covers the positions a query file puts comments in:
// above the first statement, trailing a statement on its terminator's line,
// inside a statement, between two statements, and after the last one. The
// last is the one a parse could plausibly miss, since nothing follows it but
// end of input.
func TestParseFilePlacement(t *testing.T) {
	const input = `-- name: GetAuthor :one
SELECT id, -- the primary key
       name
FROM authors
WHERE id = $1; -- the lookup

/* a block comment
   over two lines */
SELECT 2;
-- trailing`

	file, err := parser.ParseFile(input)
	if err != nil {
		t.Fatal(err)
	}
	if len(file.Stmts) != 2 {
		t.Fatalf("got %d statements, want 2", len(file.Stmts))
	}
	var got []string
	for _, c := range file.Comments {
		got = append(got, input[c.Start:c.End])
	}
	want := []string{
		"-- name: GetAuthor :one",
		"-- the primary key",
		"-- the lookup",
		"/* a block comment\n   over two lines */",
		"-- trailing",
	}
	if len(got) != len(want) {
		t.Fatalf("got %d comments %q, want %d %q", len(got), got, len(want), want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("comment %d = %q, want %q", i, got[i], want[i])
		}
	}
}

// TestParseFileNested checks a nested block comment arrives as the single
// token PostgreSQL scans it as, rather than ending at the inner "*/".
func TestParseFileNested(t *testing.T) {
	const input = "SELECT /* outer /* inner */ still outer */ 1"
	file, err := parser.ParseFile(input)
	if err != nil {
		t.Fatal(err)
	}
	if len(file.Comments) != 1 {
		t.Fatalf("got %d comments, want 1", len(file.Comments))
	}
	if got, want := input[file.Comments[0].Start:file.Comments[0].End], "/* outer /* inner */ still outer */"; got != want {
		t.Errorf("comment = %q, want %q", got, want)
	}
}

// TestParseFileError pins that a failed parse yields no half-scanned comment
// list: the scan stops where the grammar does, so what it collected covers
// only part of the input.
func TestParseFileError(t *testing.T) {
	file, err := parser.ParseFile("-- leading\nSELECT FROM WHERE; -- trailing")
	if err == nil {
		t.Fatalf("ParseFile = %v, want a syntax error", file)
	}
	if file != nil {
		t.Errorf("ParseFile returned %v alongside %v", file, err)
	}
	var perr *parser.Error
	if !errors.As(err, &perr) {
		t.Fatalf("error is %T, want *parser.Error", err)
	}
	if perr.Cursorpos == 0 {
		t.Errorf("error carries no cursor position: %+v", perr)
	}
}

// TestParseFileNoComments checks the common case allocates nothing.
func TestParseFileNoComments(t *testing.T) {
	file, err := parser.ParseFile("SELECT 1")
	if err != nil {
		t.Fatal(err)
	}
	if file.Comments != nil {
		t.Errorf("Comments = %v, want nil", file.Comments)
	}
}
