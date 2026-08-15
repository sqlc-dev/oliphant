// Command next-test picks the next todo case from the corpus — the entry
// point of the development loop (see CLAUDE.md):
//
//	go run ./cmd/next-test            # next todo overall (suite order below)
//	go run ./cmd/next-test -suite scan
//	go run ./cmd/next-test -n 5       # show several
//
// Suites are visited in milestone order: the lexer (scan, split_scanner)
// comes before parsing, parsing before normalize/fingerprint/deparse.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/sqlc-dev/oliphant/internal/testfile"
)

var suiteOrder = []string{
	"scan", "split_scanner", "parse", "deparse",
	"normalize", "normalize_utility", "fingerprint", "split_parser",
}

func main() {
	suite := flag.String("suite", "", "restrict to one suite (scan, parse, ...)")
	n := flag.Int("n", 1, "number of todo cases to show")
	root := flag.String("testdata", "parser/testdata", "corpus root")
	flag.Parse()

	shown := 0
	for _, s := range suiteOrder {
		if *suite != "" && s != *suite {
			continue
		}
		dir := filepath.Join(*root, s)
		if _, err := os.Stat(dir); os.IsNotExist(err) {
			continue
		}
		var files []string
		err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if !d.IsDir() && strings.HasSuffix(path, ".test") {
				files = append(files, path)
			}
			return nil
		})
		check(err)
		for _, path := range files {
			meta, err := testfile.ReadMetadata(path)
			check(err)
			if len(meta.Todo) == 0 {
				continue
			}
			todoSet := map[string]bool{}
			for _, name := range meta.Todo {
				todoSet[name] = true
			}
			cases, err := testfile.Read(path)
			check(err)
			for _, c := range cases {
				if !todoSet[c.Name] {
					continue
				}
				fmt.Printf("suite:    %s\nfile:     %s\ncase:     %s\n", s, path, c.Name)
				fmt.Printf("input:\n%s\n", indent(c.Input))
				fmt.Printf("expected:\n%s\n\n", indent(truncate(c.Expected, 2000)))
				shown++
				if shown >= *n {
					return
				}
			}
		}
	}
	if shown == 0 {
		fmt.Println("no todo cases — corpus is green")
	}
}

func indent(s string) string {
	return "    " + strings.ReplaceAll(s, "\n", "\n    ")
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + fmt.Sprintf("... (%d more bytes)", len(s)-n)
}

func check(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "next-test:", err)
		os.Exit(1)
	}
}
