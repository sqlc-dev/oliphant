package parser_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"google.golang.org/protobuf/proto"

	pg_query "github.com/sqlc-dev/oliphant"
	"github.com/sqlc-dev/oliphant/ast"
	"github.com/sqlc-dev/oliphant/internal/testfile"
	"github.com/sqlc-dev/oliphant/parser"
)

// treeCorpusFiles lists every .test file of the parse suite — the corpus of
// inputs whose trees are the thing under test here.
func treeCorpusFiles(t *testing.T) []string {
	t.Helper()
	var files []string
	err := filepath.WalkDir(filepath.Join("testdata", "parse"), func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.HasSuffix(path, ".test") {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatal("no corpus files found")
	}
	return files
}

// roundTrip is the body Parse had before ParseToTree: marshal the tree and
// decode it again, which is what the cgo boundary forces upstream.
func roundTrip(input string) (*ast.ParseResult, error) {
	b, err := parser.ParseToProtobuf(input)
	if err != nil {
		return nil, err
	}
	tree := &ast.ParseResult{}
	if err := proto.Unmarshal(b, tree); err != nil {
		return nil, err
	}
	return tree, nil
}

// TestParseToTreeMatchesProtobuf pins the premise of skipping the round trip:
// across the corpus, the tree the parser hands back directly and the tree
// that survives a marshal/unmarshal are equal, and the two paths agree on
// which inputs fail.
func TestParseToTreeMatchesProtobuf(t *testing.T) {
	var parsed int
	for _, path := range treeCorpusFiles(t) {
		cases, err := testfile.Read(path)
		if err != nil {
			t.Fatal(err)
		}
		rel, _ := filepath.Rel("testdata", path)
		for _, c := range cases {
			direct, directErr := parser.ParseToTree(c.Input)
			round, roundErr := roundTrip(c.Input)
			if (directErr == nil) != (roundErr == nil) {
				t.Errorf("%s case %s: direct err=%v, round trip err=%v\ninput:\n%s",
					rel, c.Name, directErr, roundErr, c.Input)
				continue
			}
			if directErr != nil {
				if directErr.Error() != roundErr.Error() {
					t.Errorf("%s case %s: direct err %q, round trip err %q",
						rel, c.Name, directErr, roundErr)
				}
				continue
			}
			parsed++
			if !proto.Equal(direct, round) {
				t.Errorf("%s case %s: tree differs after the protobuf round trip\ninput:\n%s",
					rel, c.Name, c.Input)
			}
		}
	}
	t.Logf("compared %d parsed cases", parsed)
}

// TestParseToTreeInvalidUTF8 covers the one input class where the round trip
// is load-bearing rather than redundant. proto3 string fields must be valid
// UTF-8, so a raw invalid byte in the input makes upstream's Parse fail at
// the decode; ParseToTree keeps that by falling back to the round trip
// whenever the input is not valid UTF-8.
func TestParseToTreeInvalidUTF8(t *testing.T) {
	for _, input := range []string{
		"SELECT '\xff'",
		"SELECT * FROM \"tab\xffle\"",
	} {
		tree, err := parser.ParseToTree(input)
		if err == nil {
			t.Errorf("ParseToTree(%q) = %v, want an error", input, tree)
			continue
		}
		if !strings.Contains(err.Error(), "invalid UTF-8") {
			t.Errorf("ParseToTree(%q) error = %q, want the protobuf UTF-8 error", input, err)
		}
		if tree != nil {
			t.Errorf("ParseToTree(%q) returned a tree alongside %v", input, err)
		}
	}

	// A valid-UTF-8 input can never produce an invalid-UTF-8 string field:
	// the scanner rejects escapes that would synthesize one, and identifier
	// truncation is pg_mbcliplen-equivalent, so it never splits a character.
	// These take the direct path and must still parse.
	for _, input := range []string{
		`SELECT E'\303\251'`,
		`SELECT E'\xc3\xa9'`,
		`SELECT U&'\00e9'`,
		`SELECT "` + strings.Repeat("é", 40) + `"`,
	} {
		if _, err := parser.ParseToTree(input); err != nil {
			t.Errorf("ParseToTree(%q) = %v, want no error", input, err)
		}
	}
}

// TestParseToTreeSharesMultiAssignSource documents the one way a tree from
// ParseToTree differs from one that has been through protobuf: it can share
// subtrees. A multi-column assignment points every ResTarget's
// MultiAssignRef at one source node, exactly as the C tree does; protobuf has
// no pointers, so encoding and decoding silently deep-copied it. Read-only
// callers — which is what pg_query_go's API admits — cannot tell.
func TestParseToTreeSharesMultiAssignSource(t *testing.T) {
	const input = "UPDATE t SET (a, b, c) = (1, 2, 3)"

	sources := func(tree *ast.ParseResult) []*ast.Node {
		t.Helper()
		var out []*ast.Node
		for _, target := range tree.Stmts[0].Stmt.GetUpdateStmt().TargetList {
			out = append(out, target.GetResTarget().Val.GetMultiAssignRef().Source)
		}
		if len(out) != 3 {
			t.Fatalf("got %d targets, want 3", len(out))
		}
		return out
	}

	direct, err := parser.ParseToTree(input)
	if err != nil {
		t.Fatal(err)
	}
	got := sources(direct)
	if got[0] != got[1] || got[1] != got[2] {
		t.Errorf("ParseToTree: multi-assign sources are distinct nodes, want one shared node")
	}

	round, err := roundTrip(input)
	if err != nil {
		t.Fatal(err)
	}
	got = sources(round)
	if got[0] == got[1] || got[1] == got[2] {
		t.Errorf("round trip: multi-assign sources are shared, want the deep copy protobuf forces")
	}

	// Sharing or not, the trees are equal by value.
	if !proto.Equal(direct, round) {
		t.Errorf("trees differ by value")
	}
}

// TestParseUsesTree checks the root package's Parse is the same call, so the
// properties above describe what consumers actually get.
func TestParseUsesTree(t *testing.T) {
	const input = "SELECT 1"
	viaRoot, err := pg_query.Parse(input)
	if err != nil {
		t.Fatal(err)
	}
	viaParser, err := parser.ParseToTree(input)
	if err != nil {
		t.Fatal(err)
	}
	if !proto.Equal(viaRoot, viaParser) {
		t.Errorf("oliphant.Parse and parser.ParseToTree disagree")
	}
}
