package emit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"google.golang.org/protobuf/encoding/protojson"

	"github.com/sqlc-dev/oliphant/ast"
	"github.com/sqlc-dev/oliphant/internal/testfile"
)

// TestGoldenRoundTrip proves the emitter's byte-parity over the entire parse
// corpus without needing the parser: every tree golden (written verbatim from
// the oracle's ParseToJSON) is decoded via protojson — which tolerates any
// field order and escaping — and re-emitted; the bytes must match the golden
// exactly. This exercises every node type, field-order, zero-omission, and
// escaping rule the corpus reaches.
func TestGoldenRoundTrip(t *testing.T) {
	root := filepath.Join("..", "..", "parser", "testdata", "parse")
	if _, err := os.Stat(root); err != nil {
		t.Skip("parse corpus not present")
	}
	var files []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
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
	trees, mismatches := 0, 0
	for _, path := range files {
		cases, err := testfile.Read(path)
		if err != nil {
			t.Fatal(err)
		}
		for _, c := range cases {
			if testfile.IsError(c.Expected) {
				continue
			}
			tree := &ast.ParseResult{}
			if err := protojson.Unmarshal([]byte(c.Expected), tree); err != nil {
				t.Fatalf("%s case %s: protojson: %v", path, c.Name, err)
			}
			trees++
			if got := ParseResult(tree); got != c.Expected {
				mismatches++
				if mismatches <= 5 {
					t.Errorf("%s case %s: emit mismatch\ngot:  %s\nwant: %s",
						path, c.Name, got, c.Expected)
				}
			}
		}
	}
	if mismatches > 5 {
		t.Errorf("...and %d more mismatches", mismatches-5)
	}
	t.Logf("round-tripped %d trees from %d files", trees, len(files))
}
