// difftodo prints input, expected, and actual output for the still-failing
// todo cases of parse-suite .test files. Debug aid for the corpus loop; not
// part of the public tooling.
package main

import (
	"errors"
	"fmt"
	"os"
	"strconv"

	pg_query "github.com/sqlc-dev/oliphant"
	"github.com/sqlc-dev/oliphant/internal/testfile"
	"github.com/sqlc-dev/oliphant/parser"
)

func main() {
	path := os.Args[1]
	limit := 5
	if len(os.Args) > 2 {
		limit, _ = strconv.Atoi(os.Args[2])
	}
	cases, err := testfile.Read(path)
	if err != nil {
		panic(err)
	}
	meta, err := testfile.ReadMetadata(path)
	if err != nil {
		panic(err)
	}
	todo := map[string]bool{}
	for _, name := range meta.Todo {
		todo[name] = true
	}
	shown := 0
	for _, c := range cases {
		if !todo[c.Name] {
			continue
		}
		got, perr := pg_query.ParseToJSON(c.Input)
		if perr != nil {
			var pe *parser.Error
			if errors.As(perr, &pe) {
				got = testfile.RenderError(testfile.ErrorExpectation{
					Message:   pe.Message,
					Cursorpos: pe.Cursorpos,
					Filename:  pe.Filename,
					Funcname:  pe.Funcname,
					Context:   pe.Context,
				})
			} else {
				got = "UNIMPLEMENTED: " + perr.Error()
			}
		}
		if got == c.Expected {
			continue
		}
		fmt.Printf("== %s\nINPUT: %s\nWANT: %s\nGOT:  %s\n\n", c.Name, c.Input, c.Expected, got)
		shown++
		if shown >= limit {
			break
		}
	}
}
