package main

import (
	"fmt"
	"regexp"
	"strings"
)

// --- C test-table extraction -------------------------------------------------
//
// libpg_query's inline test tables (test/parse_tests.c etc.) are C arrays of
// string literals: alternating input/expected pairs, or inputs only. The
// inputs are transcribed mechanically here; the EXPECTATIONS always come from
// the live oracle, never from the C file, so a table format drift can only
// lose cases (visible in review), not corrupt goldens.

// extractCStrings returns the top-level string-literal entries of the first
// `tests[] = { ... };` array in a C file. Adjacent literals (C concatenation)
// form a single entry; entries are comma-separated.
func extractCStrings(src string) ([]string, error) {
	start := strings.Index(src, "tests[] = {")
	if start < 0 {
		return nil, fmt.Errorf("no tests[] array found")
	}
	src = src[start+len("tests[] = {"):]

	var entries []string
	var cur strings.Builder
	inEntry := false
	i := 0
	for i < len(src) {
		c := src[i]
		switch {
		case c == '"':
			lit, rest, err := readCStringLiteral(src[i:])
			if err != nil {
				return nil, err
			}
			cur.WriteString(lit)
			inEntry = true
			i = len(src) - len(rest)
		case c == ',':
			if inEntry {
				entries = append(entries, cur.String())
				cur.Reset()
				inEntry = false
			}
			i++
		case c == '}':
			if inEntry {
				entries = append(entries, cur.String())
			}
			return entries, nil
		case c == '#':
			// Preprocessor conditionals (#ifndef _MSC_VER around the 1.1 MB
			// stress insert). We extract for the non-MSVC build: the guarded
			// entries are included, the directive lines skipped.
			nl := strings.IndexByte(src[i:], '\n')
			if nl < 0 {
				return nil, fmt.Errorf("unterminated preprocessor line")
			}
			i += nl + 1
		case c == '/' && i+1 < len(src) && src[i+1] == '/':
			nl := strings.IndexByte(src[i:], '\n')
			if nl < 0 {
				return nil, fmt.Errorf("unterminated // comment")
			}
			i += nl + 1
		case c == '/' && i+1 < len(src) && src[i+1] == '*':
			end := strings.Index(src[i:], "*/")
			if end < 0 {
				return nil, fmt.Errorf("unterminated /* comment")
			}
			i += end + 2
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			i++
		default:
			return nil, fmt.Errorf("unexpected byte %q in tests[] array", c)
		}
	}
	return nil, fmt.Errorf("unterminated tests[] array")
}

// readCStringLiteral decodes one C string literal starting at `"`, returning
// the decoded value and the remaining input after the closing quote.
func readCStringLiteral(src string) (string, string, error) {
	if src[0] != '"' {
		return "", "", fmt.Errorf("not at a string literal")
	}
	var b strings.Builder
	i := 1
	for i < len(src) {
		c := src[i]
		switch c {
		case '"':
			return b.String(), src[i+1:], nil
		case '\\':
			i++
			if i >= len(src) {
				return "", "", fmt.Errorf("unterminated escape")
			}
			e := src[i]
			switch e {
			case 'n':
				b.WriteByte('\n')
			case 't':
				b.WriteByte('\t')
			case 'r':
				b.WriteByte('\r')
			case '"', '\\', '\'':
				b.WriteByte(e)
			case '0', '1', '2', '3', '4', '5', '6', '7':
				// octal escape, up to 3 digits
				val := int(e - '0')
				for n := 0; n < 2 && i+1 < len(src) && src[i+1] >= '0' && src[i+1] <= '7'; n++ {
					i++
					val = val*8 + int(src[i]-'0')
				}
				b.WriteByte(byte(val))
			case 'x':
				val := 0
				n := 0
				for i+1 < len(src) && isHex(src[i+1]) {
					i++
					val = val*16 + hexVal(src[i])
					n++
				}
				if n == 0 {
					return "", "", fmt.Errorf("bad hex escape")
				}
				b.WriteByte(byte(val))
			default:
				return "", "", fmt.Errorf("unsupported escape \\%c", e)
			}
			i++
		default:
			b.WriteByte(c)
			i++
		}
	}
	return "", "", fmt.Errorf("unterminated string literal")
}

func isHex(c byte) bool {
	return c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F'
}

func hexVal(c byte) int {
	switch {
	case c >= '0' && c <= '9':
		return int(c - '0')
	case c >= 'a' && c <= 'f':
		return int(c-'a') + 10
	default:
		return int(c-'A') + 10
	}
}

// --- postgres_regress preprocessing -----------------------------------------

var (
	// psql send-commands that terminate a statement without a semicolon
	// (`SELECT 1 AS x \gset`): everything from the backslash on is psql's,
	// the statement itself ends there.
	gsetRe = regexp.MustCompile(`\\g(set|exec|desc)?\b[^"']*$`)
	// COPY ... FROM stdin — the payload lines that follow (until `\.`) are
	// data, not SQL.
	copyStdinRe = regexp.MustCompile(`(?i)\bfrom\s+stdin\b`)
)

// preprocessRegress filters a PostgreSQL regression-suite .sql file down to
// plain SQL: psql backslash metacommands are dropped, `\gset`-style
// terminators become semicolons, and COPY-FROM-stdin payloads are removed.
// Statements the filter can't fully clean simply parse to ERROR goldens —
// deterministic either way.
func preprocessRegress(src string) string {
	var out []string
	inCopyData := false
	pendingCopy := false
	for _, line := range strings.Split(src, "\n") {
		trimmed := strings.TrimSpace(line)
		if inCopyData {
			if trimmed == `\.` {
				inCopyData = false
			}
			continue
		}
		if strings.HasPrefix(trimmed, `\`) {
			// psql metacommand (\set, \d, \if, \echo, ..., and stray \.)
			continue
		}
		if loc := gsetRe.FindStringIndex(line); loc != nil {
			line = line[:loc[0]] + ";"
			trimmed = strings.TrimSpace(line)
		}
		out = append(out, line)
		if copyStdinRe.MatchString(line) || pendingCopy {
			if strings.HasSuffix(trimmed, ";") {
				pendingCopy = false
				inCopyData = true
			} else {
				pendingCopy = true
			}
		}
	}
	return strings.Join(out, "\n")
}
