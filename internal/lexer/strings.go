package lexer

import (
	"unicode/utf8"

	"github.com/sqlc-dev/oliphant/ast"
)

// quoteContinues implements the <xqs> "quote stop" state: after a closing
// quote, a string literal continues if the next quote is separated only by
// whitespace containing at least one newline (scan.l: {quotecontinue}). It
// returns the position just past the continuation quote, or ok=false with
// everything thrown back (yyless(0)).
func (s *Scanner) quoteContinues(afterQuote int) (int, bool) {
	in := s.input
	n := len(in)
	i := afterQuote
	for i < n && isNonNewlineSpace(in[i]) {
		i++
	}
	if i < n && isNewline(in[i]) {
		i++
		for i < n && isSpace(in[i]) {
			i++
		}
		if i < n && in[i] == '\'' {
			return i + 1, true
		}
	}
	return afterQuote, false
}

// scanSimpleString handles the <xb> and <xh> states: no escapes, no quote
// doubling, contents passed through with a marker prefix ("b"/"x") exactly
// as the {xbstart}/{xhstart} actions do with addlitchar.
func (s *Scanner) scanSimpleString(start int, kind ast.Token, prefix, unterminated string) (Token, *Error) {
	in := s.input
	n := len(in)
	val := []byte(prefix)
	for {
		if s.pos >= n {
			// scan.l: <xb><<EOF>> / <xh><<EOF>>
			return Token{}, s.yyerror(start, unterminated)
		}
		if in[s.pos] == '\'' {
			// scan.l: <xb,xh,...>{quote} → <xqs>
			if resume, ok := s.quoteContinues(s.pos + 1); ok {
				s.pos = resume
				continue
			}
			s.pos++
			return Token{Kind: kind, Start: int32(start), End: int32(s.pos), Str: string(val)}, nil
		}
		// scan.l: <xb>{xbinside} / <xh>{xhinside}
		j := s.pos
		for j < n && in[j] != '\'' {
			j++
		}
		val = append(val, in[s.pos:j]...)
		s.pos = j
	}
}

// scanQuotedString handles the <xq> (SCONST) and <xus> (USCONST) states:
// quote doubling, no backslash processing.
func (s *Scanner) scanQuotedString(start int, kind ast.Token) (Token, *Error) {
	in := s.input
	n := len(in)
	var val []byte
	for {
		if s.pos >= n {
			// scan.l: <xq,xe,xus><<EOF>>
			return Token{}, s.yyerror(start, "unterminated quoted string")
		}
		if in[s.pos] == '\'' {
			if s.pos+1 < n && in[s.pos+1] == '\'' {
				// scan.l: <xq,xe,xus>{xqdouble}
				val = append(val, '\'')
				s.pos += 2
				continue
			}
			if resume, ok := s.quoteContinues(s.pos + 1); ok {
				s.pos = resume
				continue
			}
			s.pos++
			return Token{Kind: kind, Start: int32(start), End: int32(s.pos), Str: string(val)}, nil
		}
		// scan.l: <xq,xus>{xqinside}
		j := s.pos
		for j < n && in[j] != '\'' {
			j++
		}
		val = append(val, in[s.pos:j]...)
		s.pos = j
	}
}

// scanQuotedIdent handles the <xd> (IDENT) and <xui> (UIDENT) states.
func (s *Scanner) scanQuotedIdent(start int, kind ast.Token) (Token, *Error) {
	in := s.input
	n := len(in)
	var val []byte
	for {
		if s.pos >= n {
			// scan.l: <xd,xui><<EOF>>
			return Token{}, s.yyerror(start, "unterminated quoted identifier")
		}
		if in[s.pos] == '"' {
			if s.pos+1 < n && in[s.pos+1] == '"' {
				// scan.l: <xd,xui>{xddouble}
				val = append(val, '"')
				s.pos += 2
				continue
			}
			// scan.l: <xd>{xdstop} / <xui>{dquote}
			s.pos++
			if len(val) == 0 {
				return Token{}, s.yyerror(start, "zero-length delimited identifier")
			}
			str := string(val)
			if kind == ast.Token_IDENT {
				// scan.l: <xd>{xdstop} — truncate but do not downcase.
				// (UIDENT is truncated only after de-escaping, in the
				// base_yylex filter.)
				str = truncateIdentifier(str)
			}
			return Token{Kind: kind, Start: int32(start), End: int32(s.pos), Str: str}, nil
		}
		// scan.l: <xd,xui>{xdinside}
		j := s.pos
		for j < n && in[j] != '"' {
			j++
		}
		val = append(val, in[s.pos:j]...)
		s.pos = j
	}
}

// scanDollarString handles the <xdolq> state. tag is the full opening
// delimiter including both dollar signs; s.pos is just past it.
func (s *Scanner) scanDollarString(start int, tag string) (Token, *Error) {
	in := s.input
	n := len(in)
	var val []byte
	for {
		if s.pos >= n {
			// scan.l: <xdolq><<EOF>>
			return Token{}, s.yyerror(start, "unterminated dollar-quoted string")
		}
		if in[s.pos] == '$' {
			j := s.pos + 1
			if j < n && isDolqStart(in[j]) {
				j++
				for j < n && isDolqCont(in[j]) {
					j++
				}
			}
			if j < n && in[j] == '$' {
				// scan.l: <xdolq>{dolqdelim}
				if in[s.pos:j+1] == tag {
					s.pos = j + 1
					return Token{Kind: ast.Token_SCONST, Start: int32(start), End: int32(s.pos), Str: string(val)}, nil
				}
				// Not our delimiter: keep the $... part, put back the final
				// "$" for rescanning (yyless(yyleng - 1)).
				val = append(val, in[s.pos:j]...)
				s.pos = j
				continue
			}
			if j > s.pos+1 {
				// scan.l: <xdolq>{dolqfailed}
				val = append(val, in[s.pos:j]...)
				s.pos = j
				continue
			}
			// scan.l: <xdolq>. — lone $ inside the quoted text
			val = append(val, '$')
			s.pos++
			continue
		}
		// scan.l: <xdolq>{dolqinside}
		j := s.pos
		for j < n && in[j] != '$' {
			j++
		}
		val = append(val, in[s.pos:j]...)
		s.pos = j
	}
}

func isUTF16SurrogateFirst(c uint32) bool  { return c >= 0xD800 && c <= 0xDBFF }
func isUTF16SurrogateSecond(c uint32) bool { return c >= 0xDC00 && c <= 0xDFFF }

// scanExtendedString handles the <xe> and <xeu> states: E'...' strings with
// backslash escapes, Unicode escapes and surrogate pairs, and the deferred
// pg_verifymbstr check when an escape produced a NUL or high-bit byte.
func (s *Scanner) scanExtendedString(start int) (Token, *Error) {
	in := s.input
	n := len(in)
	var val []byte
	sawNonASCII := false
	for {
		if s.pos >= n {
			// scan.l: <xq,xe,xus><<EOF>>
			return Token{}, s.yyerror(start, "unterminated quoted string")
		}
		c := in[s.pos]
		if c == '\'' {
			if s.pos+1 < n && in[s.pos+1] == '\'' {
				// scan.l: <xq,xe,xus>{xqdouble}
				val = append(val, '\'')
				s.pos += 2
				continue
			}
			if resume, ok := s.quoteContinues(s.pos + 1); ok {
				s.pos = resume
				continue
			}
			s.pos++
			// scan.l: <xqs> xe return path — check the data remains valid
			// if unescaping might have made it invalid.
			if sawNonASCII {
				if err := verifyUTF8(val); err != nil {
					return Token{}, err
				}
			}
			return Token{Kind: ast.Token_SCONST, Start: int32(start), End: int32(s.pos), Str: string(val)}, nil
		}
		if c != '\\' {
			// scan.l: <xe>{xeinside}
			j := s.pos
			for j < n && in[j] != '\\' && in[j] != '\'' {
				j++
			}
			val = append(val, in[s.pos:j]...)
			s.pos = j
			continue
		}

		// Backslash escapes.
		e := s.pos
		if e+1 >= n {
			// scan.l: <xe>. — a backslash just before EOF
			val = append(val, '\\')
			s.pos++
			continue
		}
		nc := in[e+1]
		switch {
		case nc == 'u' || nc == 'U':
			want := 4
			if nc == 'U' {
				want = 8
			}
			k := 0
			for k < want && e+2+k < n && isHexDigit(in[e+2+k]) {
				k++
			}
			if k < want {
				// scan.l: <xe,xeu>{xeunicodefail}
				s.pos = e + 2 + k
				return Token{}, s.ereport(e, "invalid Unicode escape")
			}
			cp := parseHex(in[e+2 : e+2+want])
			s.pos = e + 2 + want
			if isUTF16SurrogateFirst(cp) {
				// scan.l: <xe>{xeunicode} → BEGIN(xeu)
				tok, err, done := s.scanSurrogateSecond(start, cp, &val)
				if done {
					return tok, err
				}
				continue
			}
			if isUTF16SurrogateSecond(cp) {
				// scan.l: <xe>{xeunicode} — orphaned second half
				return Token{}, s.yyerror(e, "invalid Unicode surrogate pair")
			}
			if err := appendUnicode(&val, cp, s, e); err != nil {
				return Token{}, err
			}
		case nc >= '0' && nc <= '7':
			// scan.l: <xe>{xeoctesc}
			k := 1
			for k < 3 && e+1+k < n && isOctDigit(in[e+1+k]) {
				k++
			}
			v := byte(parseOct(in[e+1 : e+1+k]))
			val = append(val, v)
			if v == 0 || v >= 0x80 {
				sawNonASCII = true
			}
			s.pos = e + 1 + k
		case nc == 'x' && e+2 < n && isHexDigit(in[e+2]):
			// scan.l: <xe>{xehexesc}
			k := 1
			if e+3 < n && isHexDigit(in[e+3]) {
				k = 2
			}
			v := byte(parseHex(in[e+2 : e+2+k]))
			val = append(val, v)
			if v == 0 || v >= 0x80 {
				sawNonASCII = true
			}
			s.pos = e + 2 + k
		default:
			// scan.l: <xe>{xeescape} → unescape_single_char
			var out byte
			switch nc {
			case 'b':
				out = '\b'
			case 'f':
				out = '\f'
			case 'n':
				out = '\n'
			case 'r':
				out = '\r'
			case 't':
				out = '\t'
			case 'v':
				out = '\v'
			default:
				out = nc
				if nc >= 0x80 {
					sawNonASCII = true
				}
			}
			val = append(val, out)
			s.pos = e + 2
		}
	}
}

// scanSurrogateSecond is the <xeu> state: after a first UTF-16 surrogate
// half, the only acceptable continuation is another \uXXXX/\UXXXXXXXX escape
// carrying the second half. done=false means the pair was combined and
// appended and xe scanning continues.
func (s *Scanner) scanSurrogateSecond(strStart int, first uint32, val *[]byte) (Token, *Error, bool) {
	in := s.input
	n := len(in)
	if s.pos >= n {
		// scan.l: <xeu><<EOF>> — SET_YYLLOC() puts the cursor at EOF.
		return Token{}, s.yyerror(n, "invalid Unicode surrogate pair"), true
	}
	e := s.pos
	if in[e] == '\\' && e+1 < n && (in[e+1] == 'u' || in[e+1] == 'U') {
		want := 4
		if in[e+1] == 'U' {
			want = 8
		}
		k := 0
		for k < want && e+2+k < n && isHexDigit(in[e+2+k]) {
			k++
		}
		if k < want {
			// scan.l: <xe,xeu>{xeunicodefail}
			s.pos = e + 2 + k
			return Token{}, s.ereport(e, "invalid Unicode escape"), true
		}
		cp := parseHex(in[e+2 : e+2+want])
		s.pos = e + 2 + want
		if !isUTF16SurrogateSecond(cp) {
			// scan.l: <xeu>{xeunicode} — not a second half
			return Token{}, s.yyerror(e, "invalid Unicode surrogate pair"), true
		}
		// pg_wchar.h: surrogate_pair_to_codepoint
		combined := ((first & 0x3FF) << 10) + 0x10000 + (cp & 0x3FF)
		if err := appendUnicode(val, combined, s, e); err != nil {
			return Token{}, err, true
		}
		return Token{}, nil, false
	}
	// scan.l: <xeu>. | <xeu>\n
	s.pos = e + 1
	return Token{}, s.yyerror(e, "invalid Unicode surrogate pair"), true
}

// appendUnicode is scan.l's addunicode: validate the code point and append
// its UTF-8 encoding (pg_unicode_to_server with a UTF-8 server encoding).
func appendUnicode(val *[]byte, cp uint32, s *Scanner, escLoc int) *Error {
	// pg_wchar.h: is_valid_unicode_codepoint
	if cp == 0 || cp > 0x10FFFF {
		return s.yyerror(escLoc, "invalid Unicode escape value")
	}
	*val = utf8.AppendRune(*val, rune(cp))
	return nil
}

func parseHex(digits string) uint32 {
	var v uint32
	for i := 0; i < len(digits); i++ {
		c := digits[i]
		switch {
		case c >= '0' && c <= '9':
			v = v<<4 | uint32(c-'0')
		case c >= 'a' && c <= 'f':
			v = v<<4 | uint32(c-'a'+10)
		default:
			v = v<<4 | uint32(c-'A'+10)
		}
	}
	return v
}

func parseOct(digits string) uint32 {
	var v uint32
	for i := 0; i < len(digits); i++ {
		v = v<<3 | uint32(digits[i]-'0')
	}
	return v & 0xFF
}
