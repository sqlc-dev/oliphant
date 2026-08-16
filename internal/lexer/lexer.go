// Package lexer is a hand-written port of PostgreSQL's scanner as patched
// and pinned by libpg_query 17-6.2.2 (internal/reference/scan.l), plus the
// base_yylex token-merge filter from internal/reference/parser.c.
//
// The scanner is byte-oriented and eager-capable but pull-based: each call
// to Next returns exactly what one core_yylex call would return, including
// the SQL_COMMENT / C_COMMENT tokens libpg_query's patches add. Token kinds
// are the protobuf Token enum values, which are defined to equal the bison
// token numbers of the pinned grammar.
package lexer

import (
	"github.com/sqlc-dev/oliphant/ast"
)

// NAMEDATALEN mirrors the PostgreSQL compile-time constant: identifiers are
// truncated to NAMEDATALEN-1 bytes, operators of NAMEDATALEN or more
// characters are rejected.
const nameDataLen = 64

// Token is one core_yylex result. Start/End are byte offsets into the input
// ([Start,End) spans the token text after any flex yyless put-back, matching
// yylloc and the patched scanner's yyllocend). Str/Ival carry the semantic
// value (core_YYSTYPE) for the token kinds that have one.
type Token struct {
	Kind        ast.Token
	Start, End  int32
	Str         string // IDENT/UIDENT/SCONST/USCONST/BCONST/XCONST/FCONST/Op: yylval.str; keywords: canonical spelling
	Ival        int32  // ICONST/PARAM: yylval.ival
	KeywordKind ast.KeywordKind
}

// Error is a scanner error, pre-formatted exactly as the reference reports
// it (scanner_yyerror / in-action ereport calls). Filename and Funcname
// mirror the C error data; Cursorpos is the 1-based character (not byte)
// position, per scanner_errposition.
type Error struct {
	Message   string
	Filename  string
	Funcname  string
	Cursorpos int
}

func (e *Error) Error() string { return e.Message }

// Scanner lexes one input string. The zero value is not usable; call New.
type Scanner struct {
	input string
	pos   int
}

// New returns a Scanner over input. Like the C scanner (which receives a
// NUL-terminated string), input is truncated at the first NUL byte.
func New(input string) *Scanner {
	for i := 0; i < len(input); i++ {
		if input[i] == 0 {
			input = input[:i]
			break
		}
	}
	return &Scanner{input: input}
}

// Input returns the input string as scanned (truncated at any NUL byte).
func (s *Scanner) Input() string { return s.input }

// Character classes from scan.l's named patterns.

// scan.l: space
func isSpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\f' || c == '\v'
}

// scan.l: non_newline_space
func isNonNewlineSpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\f' || c == '\v'
}

// scan.l: newline
func isNewline(c byte) bool { return c == '\n' || c == '\r' }

func isDigit(c byte) bool { return c >= '0' && c <= '9' }

func isHexDigit(c byte) bool {
	return isDigit(c) || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
}

func isOctDigit(c byte) bool { return c >= '0' && c <= '7' }

func isBinDigit(c byte) bool { return c == '0' || c == '1' }

// scan.l: ident_start [A-Za-z\200-\377_]
func isIdentStart(c byte) bool {
	return (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || c == '_' || c >= 0x80
}

// scan.l: ident_cont [A-Za-z\200-\377_0-9\$]
func isIdentCont(c byte) bool {
	return isIdentStart(c) || isDigit(c) || c == '$'
}

// scan.l: dolq_start [A-Za-z\200-\377_]
func isDolqStart(c byte) bool { return isIdentStart(c) }

// scan.l: dolq_cont [A-Za-z\200-\377_0-9]
func isDolqCont(c byte) bool { return isIdentStart(c) || isDigit(c) }

// scan.l: op_chars [\~\!\@\#\^\&\|\`\?\+\-\*\/\%\<\>\=]
func isOpChar(c byte) bool {
	switch c {
	case '~', '!', '@', '#', '^', '&', '|', '`', '?', '+', '-', '*', '/', '%', '<', '>', '=':
		return true
	}
	return false
}

// scan.l: self [,()\[\].;\:\+\-\*\/\%\^\<\>\=]
func isSelf(c byte) bool {
	switch c {
	case ',', '(', ')', '[', ']', '.', ';', ':', '+', '-', '*', '/', '%', '^', '<', '>', '=':
		return true
	}
	return false
}

// scan.l: the "non-SQL operator" characters of the {operator} action's
// trailing +/- rule.
func isExoticOpChar(c byte) bool {
	switch c {
	case '~', '!', '@', '#', '^', '&', '|', '`', '?', '%':
		return true
	}
	return false
}

func (s *Scanner) peek(off int) byte {
	if s.pos+off < len(s.input) {
		return s.input[s.pos+off]
	}
	return 0
}

func (s *Scanner) token(kind ast.Token, start int) (Token, *Error) {
	return Token{Kind: kind, Start: int32(start), End: int32(s.pos)}, nil
}

// Next returns the next token, exactly as core_yylex would. At end of input
// it returns a token with Kind 0 (and no error), matching yyterminate.
func (s *Scanner) Next() (Token, *Error) {
	in := s.input
	n := len(in)

	// scan.l: {whitespace} — ignore
	for s.pos < n && isSpace(in[s.pos]) {
		s.pos++
	}
	if s.pos >= n {
		// scan.l: <<EOF>> — SET_YYLLOC(); yyterminate();
		return Token{Kind: 0, Start: int32(n), End: int32(n)}, nil
	}

	start := s.pos
	c := in[s.pos]

	switch {
	case c == '-' && s.peek(1) == '-':
		// scan.l: {comment} — "--" through end of line, returned as a token
		// by libpg_query's patch 04.
		s.pos += 2
		for s.pos < n && !isNewline(in[s.pos]) {
			s.pos++
		}
		return s.token(ast.Token_SQL_COMMENT, start)

	case c == '/' && s.peek(1) == '*':
		// scan.l: {xcstart} — extended (possibly nested) comment.
		return s.scanComment(start)

	case (c == 'b' || c == 'B') && s.peek(1) == '\'':
		// scan.l: {xbstart} — bit string literal; value gets a leading "b".
		s.pos += 2
		return s.scanSimpleString(start, ast.Token_BCONST, "b", "unterminated bit string literal")

	case (c == 'x' || c == 'X') && s.peek(1) == '\'':
		// scan.l: {xhstart} — hex string literal; value gets a leading "x".
		s.pos += 2
		return s.scanSimpleString(start, ast.Token_XCONST, "x", "unterminated hexadecimal string literal")

	case (c == 'n' || c == 'N') && s.peek(1) == '\'':
		// scan.l: {xnstart} — national character: eat only the "n"
		// (yyless(1)) and return it as the NCHAR keyword; the string is
		// lexed as a separate token.
		s.pos++
		kw := keywordTokens["nchar"]
		return Token{
			Kind:        kw.token,
			Start:       int32(start),
			End:         int32(s.pos),
			Str:         "nchar",
			KeywordKind: kw.kind,
		}, nil

	case (c == 'e' || c == 'E') && s.peek(1) == '\'':
		// scan.l: {xestart} — extended string with backslash escapes.
		s.pos += 2
		return s.scanExtendedString(start)

	case c == '\'':
		// scan.l: {xqstart} — standard_conforming_strings is on, so this is
		// always a standard (xq) string.
		s.pos++
		return s.scanQuotedString(start, ast.Token_SCONST)

	case (c == 'u' || c == 'U') && s.peek(1) == '&':
		switch s.peek(2) {
		case '\'':
			// scan.l: {xusstart} — string with Unicode escapes. The
			// standard_conforming_strings=off error cannot fire (it is on).
			s.pos += 3
			return s.scanQuotedString(start, ast.Token_USCONST)
		case '"':
			// scan.l: {xuistart} — identifier with Unicode escapes.
			s.pos += 3
			return s.scanQuotedIdent(start, ast.Token_UIDENT)
		default:
			// scan.l: {xufailed} — throw back all but the initial u/U and
			// treat it as an identifier.
			s.pos++
			return Token{
				Kind:  ast.Token_IDENT,
				Start: int32(start),
				End:   int32(s.pos),
				Str:   downcaseTruncateIdentifier(in[start:s.pos]),
			}, nil
		}

	case c == '$':
		next := s.peek(1)
		switch {
		case isDigit(next):
			// scan.l: {param} — \${decdigit}+ (no underscores; and
			// param_junk was removed by libpg_query patch 09).
			s.pos++
			for s.pos < n && isDigit(in[s.pos]) {
				s.pos++
			}
			return Token{
				Kind:  ast.Token_PARAM,
				Start: int32(start),
				End:   int32(s.pos),
				Ival:  atoi32(in[start+1 : s.pos]),
			}, nil
		case next == '$':
			// scan.l: {dolqdelim} with an empty tag.
			s.pos += 2
			return s.scanDollarString(start, in[start:s.pos])
		case isDolqStart(next):
			j := s.pos + 2
			for j < n && isDolqCont(in[j]) {
				j++
			}
			if j < n && in[j] == '$' {
				// scan.l: {dolqdelim}
				s.pos = j + 1
				return s.scanDollarString(start, in[start:s.pos])
			}
			// scan.l: {dolqfailed} — throw back all but the initial "$" and
			// treat it as {other}.
			s.pos++
			return s.token(ast.Token(c), start)
		default:
			// scan.l: {other}
			s.pos++
			return s.token(ast.Token(c), start)
		}

	case c == '"':
		// scan.l: {xdstart} — delimited (double-quoted) identifier.
		s.pos++
		return s.scanQuotedIdent(start, ast.Token_IDENT)

	case c == '.':
		if s.peek(1) == '.' {
			// scan.l: {dot_dot}
			s.pos += 2
			return s.token(ast.Token_DOT_DOT, start)
		}
		if isDigit(s.peek(1)) {
			return s.scanNumber(start)
		}
		// scan.l: {self}
		s.pos++
		return s.token(ast.Token(c), start)

	case c == ':':
		switch s.peek(1) {
		case ':':
			// scan.l: {typecast}
			s.pos += 2
			return s.token(ast.Token_TYPECAST, start)
		case '=':
			// scan.l: {colon_equals}
			s.pos += 2
			return s.token(ast.Token_COLON_EQUALS, start)
		}
		// scan.l: {self}
		s.pos++
		return s.token(ast.Token(c), start)

	case isDigit(c):
		return s.scanNumber(start)

	case isIdentStart(c):
		// scan.l: {identifier}
		s.pos++
		for s.pos < n && isIdentCont(in[s.pos]) {
			s.pos++
		}
		text := in[start:s.pos]
		// scan.l: ScanKeywordLookup — ASCII-only downcasing, then keyword
		// table lookup.
		lower := asciiDowncase(text)
		if kw, ok := keywordTokens[lower]; ok {
			return Token{
				Kind:        kw.token,
				Start:       int32(start),
				End:         int32(s.pos),
				Str:         lower,
				KeywordKind: kw.kind,
			}, nil
		}
		return Token{
			Kind:  ast.Token_IDENT,
			Start: int32(start),
			End:   int32(s.pos),
			Str:   truncateIdentifier(lower),
		}, nil

	case c == ',' || c == '(' || c == ')' || c == '[' || c == ']' || c == ';':
		// scan.l: {self} — the self chars that cannot start an operator.
		s.pos++
		return s.token(ast.Token(c), start)

	case isOpChar(c):
		return s.scanOperator(start)

	default:
		// scan.l: {other}
		s.pos++
		return s.token(ast.Token(c), start)
	}
}

// scanComment implements the <xc> start condition: nested C-style comments,
// returned as C_COMMENT tokens (libpg_query patch 04).
func (s *Scanner) scanComment(start int) (Token, *Error) {
	in := s.input
	n := len(in)
	// scan.l: {xcstart} — yyless(2) keeps only the slash-star.
	s.pos += 2
	depth := 0
	for {
		if s.pos >= n {
			// scan.l: <xc><<EOF>>
			return Token{}, s.yyerror(start, "unterminated /* comment")
		}
		c := in[s.pos]
		if c == '/' && s.pos+1 < n && in[s.pos+1] == '*' {
			// scan.l: <xc>{xcstart} — nested open, again only 2 chars.
			depth++
			s.pos += 2
			continue
		}
		if c == '*' {
			j := s.pos
			for j < n && in[j] == '*' {
				j++
			}
			if j < n && in[j] == '/' {
				// scan.l: <xc>{xcstop} — \*+\/
				s.pos = j + 1
				if depth <= 0 {
					return s.token(ast.Token_C_COMMENT, start)
				}
				depth--
				continue
			}
			// scan.l: <xc>\*+ — ignore
			s.pos = j
			continue
		}
		// scan.l: <xc>{xcinside} / <xc>{op_chars} — ignore
		s.pos++
	}
}

// scanOperator implements the {operator} rule and its interaction with the
// special two-character tokens and the {self} set.
func (s *Scanner) scanOperator(start int) (Token, *Error) {
	in := s.input
	n := len(in)
	runEnd := s.pos
	for runEnd < n && isOpChar(in[runEnd]) {
		runEnd++
	}
	length := runEnd - start

	if length == 1 {
		s.pos = runEnd
		c := in[start]
		if isSelf(c) {
			// scan.l: {self}
			return s.token(ast.Token(c), start)
		}
		// scan.l: {operator}, single char
		return Token{Kind: ast.Token_Op, Start: int32(start), End: int32(s.pos), Str: in[start:runEnd]}, nil
	}

	if length == 2 {
		// scan.l: {equals_greater} / {less_equals} / {greater_equals} /
		// {less_greater} / {not_equals} — these rules tie with {operator} at
		// two chars and win by rule order.
		if tok, ok := specialTwoCharToken(in[start], in[start+1]); ok {
			s.pos = runEnd
			return s.token(tok, start)
		}
	}

	// scan.l: {operator} — check for embedded slash-star or dash-dash; the
	// operator must stop at the first one.
	nchars := length
	for i := start; i < start+nchars-1; i++ {
		if (in[i] == '/' && in[i+1] == '*') || (in[i] == '-' && in[i+1] == '-') {
			nchars = i - start
			break
		}
	}

	// scan.l: {operator} — '+' or '-' cannot end a multi-char operator
	// unless it contains one of the non-SQL operator characters.
	if nchars > 1 && (in[start+nchars-1] == '+' || in[start+nchars-1] == '-') {
		exotic := false
		for i := start + nchars - 2; i >= start; i-- {
			if isExoticOpChar(in[i]) {
				exotic = true
				break
			}
		}
		if !exotic {
			for nchars > 1 && (in[start+nchars-1] == '+' || in[start+nchars-1] == '-') {
				nchars--
			}
		}
	}

	s.pos = start + nchars

	if nchars < length {
		// scan.l: {operator} — after yyless, single self chars and the
		// special two-char tokens are returned as such.
		if nchars == 1 && isSelf(in[start]) {
			return s.token(ast.Token(in[start]), start)
		}
		if nchars == 2 {
			if tok, ok := specialTwoCharToken(in[start], in[start+1]); ok {
				return s.token(tok, start)
			}
		}
	}

	if nchars >= nameDataLen {
		// scan.l: {operator} — "operator too long"
		return Token{}, s.yyerror(start, "operator too long")
	}

	return Token{Kind: ast.Token_Op, Start: int32(start), End: int32(s.pos), Str: in[start : start+nchars]}, nil
}

func specialTwoCharToken(a, b byte) (ast.Token, bool) {
	switch {
	case a == '=' && b == '>':
		return ast.Token_EQUALS_GREATER, true
	case a == '<' && b == '=':
		return ast.Token_LESS_EQUALS, true
	case a == '>' && b == '=':
		return ast.Token_GREATER_EQUALS, true
	case a == '<' && b == '>':
		return ast.Token_NOT_EQUALS, true
	case a == '!' && b == '=':
		return ast.Token_NOT_EQUALS, true
	}
	return 0, false
}

// atoi32 mirrors the {param} action's atol truncated to the int32 yylval
// field.
func atoi32(digits string) int32 {
	var v int64
	for i := 0; i < len(digits); i++ {
		v = v*10 + int64(digits[i]-'0')
		if v > 1<<40 {
			// Saturate like C long overflow would wrap; parameters this
			// large are rejected by the grammar anyway. Keep the low bits.
			v &= (1 << 40) - 1
		}
	}
	return int32(v)
}

// asciiDowncase lowercases A-Z only, byte-wise — the transform both
// ScanKeywordLookup and downcase_truncate_identifier apply for a UTF-8
// (multi-byte) server encoding.
func asciiDowncase(text string) string {
	hasUpper := false
	for i := 0; i < len(text); i++ {
		if text[i] >= 'A' && text[i] <= 'Z' {
			hasUpper = true
			break
		}
	}
	if !hasUpper {
		return text
	}
	b := []byte(text)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + ('a' - 'A')
		}
	}
	return string(b)
}

// downcaseTruncateIdentifier is scansup.c's downcase_truncate_identifier.
func downcaseTruncateIdentifier(text string) string {
	return truncateIdentifier(asciiDowncase(text))
}

// truncateIdentifier is scansup.c's truncate_identifier: clip to
// NAMEDATALEN-1 bytes at a multibyte character boundary (pg_mbcliplen).
func truncateIdentifier(ident string) string {
	if len(ident) < nameDataLen {
		return ident
	}
	i := 0
	for i < len(ident) {
		l := mblen(ident[i])
		if i+l > nameDataLen-1 {
			break
		}
		i += l
	}
	return ident[:i]
}

// mblen is pg_utf_mblen: the byte length of a UTF-8 character judged from
// its first byte alone.
func mblen(c byte) int {
	switch {
	case c < 0x80:
		return 1
	case c&0xE0 == 0xC0:
		return 2
	case c&0xF0 == 0xE0:
		return 3
	case c&0xF8 == 0xF0:
		return 4
	default:
		return 1
	}
}
