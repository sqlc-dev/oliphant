// Command generate emits every generated file in the module from the pinned
// inputs (srcdata/, internal/reference/kwlist.h, ast/pg_query.pb.go). It is
// the PostgreSQL-upgrade story: advance the pin, re-vendor, re-run.
//
// Modes (default: -aliases -keywords, the pure-Go ones):
//
//	-proto      regenerate ast/pg_query.pb.go from srcdata/pg_query.proto
//	            (requires protoc and protoc-gen-go on PATH)
//	-aliases    regenerate aliases.go (root package aliases into ast/)
//	-keywords   regenerate internal/lexer/keywords.go from kwlist.h
package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
)

func main() {
	proto := flag.Bool("proto", false, "regenerate ast/pg_query.pb.go (needs protoc + protoc-gen-go)")
	aliases := flag.Bool("aliases", false, "regenerate aliases.go")
	keywords := flag.Bool("keywords", false, "regenerate internal/lexer/keywords.go")
	flag.Parse()

	if !*proto && !*aliases && !*keywords {
		*aliases, *keywords = true, true
	}

	if *proto {
		run("protoc", "-I", "srcdata",
			"--go_out=.",
			"--go_opt=Mpg_query.proto=github.com/sqlc-dev/oliphant/ast",
			"--go_opt=module=github.com/sqlc-dev/oliphant",
			"pg_query.proto")
		fmt.Println("wrote ast/pg_query.pb.go")
	}
	if *aliases {
		if err := generateAliases("ast/pg_query.pb.go", "aliases.go"); err != nil {
			fatal(err)
		}
		fmt.Println("wrote aliases.go")
	}
	if *keywords {
		if err := generateKeywords("internal/reference/kwlist.h", "internal/lexer/keywords.go"); err != nil {
			fatal(err)
		}
		fmt.Println("wrote internal/lexer/keywords.go")
	}
}

func run(name string, args ...string) {
	cmd := exec.Command(name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		fatal(fmt.Errorf("%s: %w", name, err))
	}
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "generate:", err)
	os.Exit(1)
}
