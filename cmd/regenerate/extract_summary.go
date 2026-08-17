package main

import (
	"regexp"
	"strconv"
	"strings"
)

// --- summary test-call extraction --------------------------------------------
//
// libpg_query's summary tests (test/summary_tests.c, test/summary_truncate.c)
// are not tests[] tables: each test body calls summary("...", 0, limit) or
// summary(query, 0, limit) after a `char *query = "...";` assignment. The
// INPUTS (query text + truncation limit) are transcribed mechanically here;
// the EXPECTATIONS always come from the live oracle, never from the C
// asserts, so a pattern drift can only lose cases (visible in review), not
// corrupt goldens.

type summaryCall struct {
	SQL   string
	Limit int
}

var (
	// `char *query = "...";` (or `char* sql = "..."`): the identifier and its
	// string literal, which summary(<ident>, ...) calls resolve against.
	summaryAssignRe = regexp.MustCompile(`\*\s*(\w+)\s*=\s*("(?:[^"\\]|\\.)*")`)
	// summary("...", 0, -1) / summary_internal(query, 0, 40): first argument
	// is a string literal or an identifier; second is parser_options; third
	// is the truncation limit.
	summaryCallRe = regexp.MustCompile(`\bsummary(?:_internal)?\s*\(\s*(?:("(?:[^"\\]|\\.)*")|(\w+))\s*,\s*(-?\d+)\s*,\s*(-?\d+)\s*\)`)
)

// extractSummaryCalls returns the (query, limit) pairs of every summary()/
// summary_internal() call with parser_options 0 whose query is a string
// literal or resolvable single-assignment identifier. C line splices
// (backslash-newline, translation phase 2) are removed up front, exactly as
// the C compiler does before tokenization.
func extractSummaryCalls(src string) ([]summaryCall, error) {
	src = strings.ReplaceAll(src, "\\\n", "")

	assigns := summaryAssignRe.FindAllStringSubmatchIndex(src, -1)
	calls := summaryCallRe.FindAllStringSubmatchIndex(src, -1)

	var out []summaryCall
	for _, m := range calls {
		var lit string
		switch {
		case m[2] >= 0: // literal first argument
			lit = src[m[2]:m[3]]
		case m[4] >= 0: // identifier: latest preceding assignment wins
			ident := src[m[4]:m[5]]
			for _, a := range assigns {
				if a[0] > m[0] {
					break
				}
				if src[a[2]:a[3]] == ident {
					lit = src[a[4]:a[5]]
				}
			}
			if lit == "" {
				continue // unresolvable identifier: lose the case, visibly
			}
		}
		opts := src[m[6]:m[7]]
		if opts != "0" {
			continue // oracle always calls with parser_options 0
		}
		limit, err := strconv.Atoi(src[m[8]:m[9]])
		if err != nil {
			return nil, err
		}
		sql, _, err := readCStringLiteral(lit)
		if err != nil {
			return nil, err
		}
		out = append(out, summaryCall{SQL: sql, Limit: limit})
	}
	return out, nil
}
