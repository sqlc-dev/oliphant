package lexer

import (
	"math"

	"github.com/sqlc-dev/oliphant/ast"
)

// The numeric literal rules of scan.l interact purely through flex's
// longest-match-wins discipline (ties broken by rule order). scanNumber
// reproduces that by computing every candidate rule's match length from the
// token start and picking the winner. Rule order (= tie-break priority):
//
//	decinteger, hexinteger, octinteger, bininteger,
//	hexfail, octfail, binfail,
//	numeric, numericfail, real, realfail,
//	integer_junk, numeric_junk, real_junk
const (
	numDec = iota
	numHex
	numOct
	numBin
	numHexFail
	numOctFail
	numBinFail
	numNumeric
	numNumericFail
	numReal
	numRealFail
	numIntegerJunk
	numNumericJunk
	numRealJunk
	numRuleCount
)

// scanNumber lexes at s.pos where the input starts with a digit, or with
// "." followed by a digit.
func (s *Scanner) scanNumber(start int) (Token, *Error) {
	in := s.input
	n := len(in)
	var ends [numRuleCount]int
	for i := range ends {
		ends[i] = -1
	}

	// scan.l: decinteger {decdigit}(_?{decdigit})*
	decEnd := -1
	if isDigit(in[start]) {
		decEnd = scanDecRun(in, start)
		ends[numDec] = decEnd
	}

	// scan.l: hexinteger/octinteger/bininteger and their *fail rules.
	if in[start] == '0' && start+1 < n {
		switch in[start+1] {
		case 'x', 'X':
			ends[numHex], ends[numHexFail] = scanPrefixedInt(in, start, isHexDigit)
		case 'o', 'O':
			ends[numOct], ends[numOctFail] = scanPrefixedInt(in, start, isOctDigit)
		case 'b', 'B':
			ends[numBin], ends[numBinFail] = scanPrefixedInt(in, start, isBinDigit)
		}
	}

	// scan.l: numeric (({decinteger}\.{decinteger}?)|(\.{decinteger})),
	// numericfail {decinteger}\.\.
	numericEnd := -1
	numericIntDot := -1 // end of the "{decinteger}." form with no fraction
	if in[start] == '.' {
		// Caller guarantees a digit follows.
		numericEnd = scanDecRun(in, start+1)
		ends[numNumeric] = numericEnd
	} else if decEnd >= 0 && decEnd < n && in[decEnd] == '.' {
		if decEnd+1 < n && in[decEnd+1] == '.' {
			ends[numNumericFail] = decEnd + 2
		}
		numericIntDot = decEnd + 1
		numericEnd = numericIntDot
		if decEnd+1 < n && isDigit(in[decEnd+1]) {
			numericEnd = scanDecRun(in, decEnd+1)
		}
		ends[numNumeric] = numericEnd
	}

	// scan.l: real ({decinteger}|{numeric})[Ee][-+]?{decinteger},
	// realfail ({decinteger}|{numeric})[Ee][-+]
	expStart := -1 // start of the exponent's digits, for real_junk
	for _, base := range [2]int{numericEnd, decEnd} {
		if base < 0 || base >= n || (in[base] != 'e' && in[base] != 'E') {
			continue
		}
		j := base + 1
		signed := false
		if j < n && (in[j] == '+' || in[j] == '-') {
			j++
			signed = true
		}
		if j < n && isDigit(in[j]) {
			if end := scanDecRun(in, j); end > ends[numReal] {
				ends[numReal] = end
				expStart = j
			}
		} else if signed && j > ends[numRealFail] {
			ends[numRealFail] = j
		}
	}

	// scan.l: integer_junk {decinteger}{identifier},
	// numeric_junk {numeric}{identifier}, real_junk {real}{identifier}.
	// {identifier} must begin at a position where the base rule has a valid
	// (possibly backtracked) match ending — i.e. right after any accepted
	// digit, or after the bare "{decinteger}." form's dot.
	ends[numIntegerJunk] = junkEnd(in, start, decEnd, -1)
	if numericEnd >= 0 {
		fracStart := start
		if in[start] != '.' {
			fracStart = decEnd
		}
		ends[numNumericJunk] = junkEnd(in, fracStart, numericEnd, numericIntDot)
	}
	if ends[numReal] >= 0 {
		ends[numRealJunk] = junkEnd(in, expStart-1, ends[numReal], -1)
	}

	// Longest match wins; ties go to the earlier rule.
	best := -1
	for rule, end := range ends {
		if end > 0 && (best < 0 || end > ends[best]) {
			best = rule
		}
	}

	switch best {
	case numDec:
		s.pos = ends[best]
		return s.processIntegerLiteral(start, in[start:s.pos], 10)
	case numHex:
		s.pos = ends[best]
		return s.processIntegerLiteral(start, in[start:s.pos], 16)
	case numOct:
		s.pos = ends[best]
		return s.processIntegerLiteral(start, in[start:s.pos], 8)
	case numBin:
		s.pos = ends[best]
		return s.processIntegerLiteral(start, in[start:s.pos], 2)
	case numHexFail:
		s.pos = ends[best]
		return Token{}, s.yyerror(start, "invalid hexadecimal integer")
	case numOctFail:
		s.pos = ends[best]
		return Token{}, s.yyerror(start, "invalid octal integer")
	case numBinFail:
		s.pos = ends[best]
		return Token{}, s.yyerror(start, "invalid binary integer")
	case numNumeric:
		s.pos = ends[best]
		return Token{Kind: ast.Token_FCONST, Start: int32(start), End: int32(s.pos), Str: in[start:s.pos]}, nil
	case numNumericFail:
		// scan.l: {numericfail} — throw back the "..", treat as integer.
		s.pos = ends[best] - 2
		return s.processIntegerLiteral(start, in[start:s.pos], 10)
	case numReal:
		s.pos = ends[best]
		return Token{Kind: ast.Token_FCONST, Start: int32(start), End: int32(s.pos), Str: in[start:s.pos]}, nil
	default:
		// realfail and the three junk rules share one report.
		s.pos = ends[best]
		return Token{}, s.yyerror(start, "trailing junk after numeric literal")
	}
}

// scanDecRun matches {decdigit}(_?{decdigit})* starting at a digit.
func scanDecRun(in string, i int) int {
	i++
	for {
		if i < len(in) && isDigit(in[i]) {
			i++
			continue
		}
		if i+1 < len(in) && in[i] == '_' && isDigit(in[i+1]) {
			i += 2
			continue
		}
		return i
	}
}

// scanPrefixedInt matches 0[xX](_?{hexdigit})+ etc. from start (where
// in[start:start+2] is the base prefix), returning the integer rule's end
// (or -1) and the matching *fail rule's end (0[xX]_?).
func scanPrefixedInt(in string, start int, digit func(byte) bool) (end, failEnd int) {
	i := start + 2
	count := 0
	for {
		j := i
		if j < len(in) && in[j] == '_' {
			j++
		}
		if j < len(in) && digit(in[j]) {
			i = j + 1
			count++
			continue
		}
		break
	}
	failEnd = start + 2
	if failEnd < len(in) && in[failEnd] == '_' {
		failEnd++
	}
	if count == 0 {
		return -1, failEnd
	}
	return i, failEnd
}

// junkEnd finds the longest match of {base}{identifier} given that valid
// base-match endings are the positions just after any digit in
// (digitLo, digitHi], plus the optional extraEnd (the bare "{decinteger}."
// numeric form). Flex backtracking means the identifier may begin at any of
// them; all lie in one contiguous ident_cont blob, so the first viable split
// determines the overall end. Returns -1 if no split works.
func junkEnd(in string, digitLo, digitHi, extraEnd int) int {
	if digitHi < 0 {
		return -1
	}
	for q := digitLo + 1; q <= digitHi && q < len(in); q++ {
		if (isDigit(in[q-1]) || q == extraEnd) && isIdentStart(in[q]) {
			end := q + 1
			for end < len(in) && isIdentCont(in[end]) {
				end++
			}
			return end
		}
	}
	return -1
}

// processIntegerLiteral is scan.l's process_integer_literal /
// numutils.c's pg_strtoint32_safe: an integer that fits in int32 is ICONST;
// anything else (in practice, overflow) becomes an FCONST carrying the
// source spelling.
func (s *Scanner) processIntegerLiteral(start int, text string, base int) (Token, *Error) {
	digits := text
	if base != 10 {
		digits = text[2:]
	}
	var v uint64
	overflow := false
	for i := 0; i < len(digits); i++ {
		c := digits[i]
		if c == '_' {
			continue
		}
		var d uint64
		switch {
		case c >= '0' && c <= '9':
			d = uint64(c - '0')
		case c >= 'a' && c <= 'f':
			d = uint64(c - 'a' + 10)
		default:
			d = uint64(c - 'A' + 10)
		}
		v = v*uint64(base) + d
		if v > math.MaxInt32 {
			overflow = true
			break
		}
	}
	if overflow {
		return Token{Kind: ast.Token_FCONST, Start: int32(start), End: int32(s.pos), Str: text}, nil
	}
	return Token{Kind: ast.Token_ICONST, Start: int32(start), End: int32(s.pos), Ival: int32(v)}, nil
}
