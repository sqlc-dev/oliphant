// Command difftest will run mutation differential fuzzing against the live
// oracle (milestone 12). For now it provides the -summary mode used during
// grammar bring-up: it walks the parse corpus's remaining todo cases, runs
// them through the current parser, and classifies the failures so the
// development loop can attack the biggest buckets first.
package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	pg_query "github.com/sqlc-dev/oliphant"
	"github.com/sqlc-dev/oliphant/internal/testfile"
	"github.com/sqlc-dev/oliphant/parser"
)

func main() {
	var (
		summary = flag.Bool("summary", false, "classify remaining parse todo failures")
		show    = flag.String("show", "", "print the first N failing cases whose bucket matches this substring")
		limit   = flag.Int("n", 5, "how many cases to print with -show")
		root    = flag.String("root", "parser/testdata/parse", "corpus root")
	)
	flag.Parse()
	if !*summary && *show == "" {
		fmt.Fprintln(os.Stderr, "usage: difftest -summary | -show <bucket> [-n N]")
		os.Exit(2)
	}

	buckets := map[string]int{}
	type sample struct{ file, name, input, got, want string }
	var samples []sample

	var files []string
	filepath.WalkDir(*root, func(path string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.HasSuffix(path, ".test") {
			files = append(files, path)
		}
		return nil
	})
	for _, path := range files {
		cases, err := testfile.Read(path)
		if err != nil {
			panic(err)
		}
		meta, err := testfile.ReadMetadata(path)
		if err != nil {
			panic(err)
		}
		todo := map[string]bool{}
		for _, n := range meta.Todo {
			todo[n] = true
		}
		for _, c := range cases {
			if !todo[c.Name] {
				continue
			}
			got := evaluate(c.Input)
			if got == c.Expected {
				continue
			}
			b := classify(got, c.Expected)
			buckets[b]++
			if *show != "" && strings.Contains(b, *show) && len(samples) < *limit {
				samples = append(samples, sample{path, c.Name, c.Input, got, c.Expected})
			}
		}
	}

	if *summary {
		type kv struct {
			k string
			v int
		}
		var list []kv
		total := 0
		for k, v := range buckets {
			list = append(list, kv{k, v})
			total += v
		}
		sort.Slice(list, func(i, j int) bool { return list[i].v > list[j].v })
		for _, e := range list {
			fmt.Printf("%7d  %s\n", e.v, e.k)
		}
		fmt.Printf("%7d  TOTAL failing\n", total)
	}
	for _, s := range samples {
		fmt.Printf("=== %s %s\ninput:\n%s\ngot:\n%s\nwant:\n%s\n\n", s.file, s.name, s.input, s.got, s.want)
	}
}

func evaluate(input string) string {
	out, err := pg_query.ParseToJSON(input)
	if err != nil {
		if pe, ok := errAsParser(err); ok {
			return testfile.RenderError(testfile.ErrorExpectation{
				Message:   pe.Message,
				Cursorpos: pe.Cursorpos,
				Filename:  pe.Filename,
				Funcname:  pe.Funcname,
				Context:   pe.Context,
			})
		}
		return "UNIMPLEMENTED: " + err.Error()
	}
	return out
}

var firstDiffRe = regexp.MustCompile(`"([A-Za-z_0-9]+)":`)

// classify produces a coarse failure bucket.
func classify(got, want string) string {
	gotErr := strings.HasPrefix(got, "ERROR: ")
	wantErr := strings.HasPrefix(want, "ERROR: ")
	switch {
	case gotErr && wantErr:
		g := strings.SplitN(got, "\n", 2)[0]
		w := strings.SplitN(want, "\n", 2)[0]
		if g == w {
			return "error-detail-mismatch (message equal)"
		}
		return "error-vs-error message differs"
	case gotErr && !wantErr:
		// We reject what the oracle accepts: the interesting bucket.
		msg := strings.SplitN(got, "\n", 2)[0]
		if m := regexp.MustCompile(`at or near "(.*)"$`).FindStringSubmatch(msg); m != nil {
			word := m[1]
			if len(word) > 20 {
				word = word[:20]
			}
			return `reject: at or near "` + word + `"`
		}
		return "reject: " + msg
	case !gotErr && wantErr:
		return "accept-but-should-reject"
	default:
		// Tree mismatch: name the first differing JSON key.
		i := 0
		for i < len(got) && i < len(want) && got[i] == want[i] {
			i++
		}
		start := i
		if start > 40 {
			start -= 40
		} else {
			start = 0
		}
		ctx := want[start:min(len(want), i+20)]
		if m := firstDiffRe.FindAllString(ctx, -1); len(m) > 0 {
			return "tree-diff near " + m[len(m)-1]
		}
		return "tree-diff"
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func errAsParser(err error) (*parser.Error, bool) {
	var pe *parser.Error
	if errors.As(err, &pe) {
		return pe, true
	}
	return nil, false
}
