package lexer

import (
	"unicode/utf8"

	"github.com/sqlc-dev/oliphant/ast"
)

// parserDotC is the vendored source file name libpg_query compiles parser.c
// as; ereport's __FILE__ (directory-stripped) is what error data carries for
// the str_udeescape/check_unicode_value error paths.
const parserDotC = "src_backend_parser_parser.c"

// Filter is parser.c's base_yylex: the one-token-lookahead layer between the
// core scanner and the grammar that keeps the grammar LALR(1) by merging
// token pairs (NOT_LA, NULLS_LA, WITH_LA, WITHOUT_LA, FORMAT_LA), converts
// UIDENT/USCONST (with optional UESCAPE) into plain IDENT/SCONST, and drops
// the comment tokens the patched scanner emits.
type Filter struct {
	s             *Scanner
	haveLookahead bool
	lookahead     Token
	lookaheadErr  *Error
}

// NewFilter wraps a Scanner in the base_yylex behavior.
func NewFilter(s *Scanner) *Filter { return &Filter{s: s} }

func (f *Filter) next() (Token, *Error) {
	if f.haveLookahead {
		f.haveLookahead = false
		if f.lookaheadErr != nil {
			return Token{}, f.lookaheadErr
		}
		return f.lookahead, nil
	}
	return f.s.Next()
}

// peekKind fetches the lookahead token's kind. A scanner error during the
// lookahead aborts immediately, exactly as the C lookahead core_yylex call
// would throw before the current token ever reaches the grammar.
func (f *Filter) peekKind() (ast.Token, *Error) {
	if !f.haveLookahead {
		f.lookahead, f.lookaheadErr = f.s.Next()
		f.haveLookahead = true
	}
	if f.lookaheadErr != nil {
		return 0, f.lookaheadErr
	}
	return f.lookahead.Kind, nil
}

// Next returns the next post-filter token.
func (f *Filter) Next() (Token, *Error) {
	for {
		cur, err := f.next()
		if err != nil {
			return Token{}, err
		}

		switch cur.Kind {
		case ast.Token_SQL_COMMENT, ast.Token_C_COMMENT:
			// parser.c: base_yylex recurses past comment tokens.
			continue

		case ast.Token_FORMAT:
			// parser.c: FORMAT → FORMAT_LA before JSON
			next, err := f.peekKind()
			if err != nil {
				return Token{}, err
			}
			if next == ast.Token_JSON {
				cur.Kind = ast.Token_FORMAT_LA
			}

		case ast.Token_NOT:
			// parser.c: NOT → NOT_LA before BETWEEN, IN, LIKE, ILIKE, SIMILAR
			next, err := f.peekKind()
			if err != nil {
				return Token{}, err
			}
			switch next {
			case ast.Token_BETWEEN, ast.Token_IN_P, ast.Token_LIKE,
				ast.Token_ILIKE, ast.Token_SIMILAR:
				cur.Kind = ast.Token_NOT_LA
			}

		case ast.Token_NULLS_P:
			// parser.c: NULLS_P → NULLS_LA before FIRST or LAST
			next, err := f.peekKind()
			if err != nil {
				return Token{}, err
			}
			switch next {
			case ast.Token_FIRST_P, ast.Token_LAST_P:
				cur.Kind = ast.Token_NULLS_LA
			}

		case ast.Token_WITH:
			// parser.c: WITH → WITH_LA before TIME or ORDINALITY
			next, err := f.peekKind()
			if err != nil {
				return Token{}, err
			}
			switch next {
			case ast.Token_TIME, ast.Token_ORDINALITY:
				cur.Kind = ast.Token_WITH_LA
			}

		case ast.Token_WITHOUT:
			// parser.c: WITHOUT → WITHOUT_LA before TIME
			next, err := f.peekKind()
			if err != nil {
				return Token{}, err
			}
			if next == ast.Token_TIME {
				cur.Kind = ast.Token_WITHOUT_LA
			}

		case ast.Token_UIDENT, ast.Token_USCONST:
			return f.resolveUnicodeToken(cur)
		}

		return cur, nil
	}
}

// resolveUnicodeToken is base_yylex's UIDENT/USCONST arm: look ahead for
// UESCAPE 'x', de-escape, and rewrite the token as IDENT/SCONST.
func (f *Filter) resolveUnicodeToken(cur Token) (Token, *Error) {
	escape := byte('\\')
	next, err := f.peekKind()
	if err != nil {
		return Token{}, err
	}
	if next == ast.Token_UESCAPE {
		// Consume UESCAPE; the third token had better be a simple string.
		if _, err := f.next(); err != nil {
			return Token{}, err
		}
		esc, err := f.next()
		if err != nil {
			return Token{}, err
		}
		if esc.Kind != ast.Token_SCONST {
			// parser.c: errors here point at the third token.
			return Token{}, f.s.yyerrorToken(esc, "UESCAPE must be followed by a simple string literal")
		}
		if len(esc.Str) != 1 || !checkUescapechar(esc.Str[0]) {
			return Token{}, f.s.yyerrorToken(esc, "invalid Unicode escape character")
		}
		escape = esc.Str[0]
	}

	str, err := f.s.udeescape(cur.Str, escape, int(cur.Start))
	if err != nil {
		return Token{}, err
	}
	if cur.Kind == ast.Token_UIDENT {
		// parser.c: it's an identifier, so truncate as appropriate.
		str = truncateIdentifier(str)
		cur.Kind = ast.Token_IDENT
	} else {
		cur.Kind = ast.Token_SCONST
	}
	cur.Str = str
	return cur, nil
}

// yyerrorToken is scanner_yyerror invoked from base_yylex, where yylloc is
// the given token's location and the flex hold char sits at that token's
// match end.
func (s *Scanner) yyerrorToken(tok Token, message string) *Error {
	loc := int(tok.Start)
	if tok.Kind == 0 || loc >= len(s.input) {
		return &Error{
			Message:   message + " at end of input",
			Filename:  "scan.l",
			Funcname:  "scanner_yyerror",
			Cursorpos: s.cursorpos(len(s.input)),
		}
	}
	return &Error{
		Message:   message + ` at or near "` + s.input[loc:tok.End] + `"`,
		Filename:  "scan.l",
		Funcname:  "scanner_yyerror",
		Cursorpos: s.cursorpos(loc),
	}
}

// checkUescapechar is parser.c's check_uescapechar: hex digits, '+', quotes
// and whitespace cannot serve as the UESCAPE character.
func checkUescapechar(escape byte) bool {
	if isHexDigit(escape) || escape == '+' || escape == '\'' || escape == '"' || isSpace(escape) {
		return false
	}
	return true
}

// udeescape is parser.c's str_udeescape: process Unicode escapes in a
// UIDENT/USCONST body. position is the token's start (the U&" or U&'), and
// error cursors point at the escape inside the literal — in - str +
// position + 3, "3 for U&\"".
func (s *Scanner) udeescape(str string, escape byte, position int) (string, *Error) {
	var out []byte
	var pairFirst uint32
	i := 0

	errPos := func(off int) int { return off + position + 3 }
	invalidPair := func(off int) *Error {
		return &Error{
			Message:   "invalid Unicode surrogate pair",
			Filename:  parserDotC,
			Funcname:  "str_udeescape",
			Cursorpos: s.cursorpos(errPos(off)),
		}
	}

	appendCp := func(cp uint32, off int) *Error {
		// parser.c: check_unicode_value
		if cp == 0 || cp > 0x10FFFF {
			return &Error{
				Message:   "invalid Unicode escape value",
				Filename:  parserDotC,
				Funcname:  "check_unicode_value",
				Cursorpos: s.cursorpos(errPos(off)),
			}
		}
		if pairFirst != 0 {
			if isUTF16SurrogateSecond(cp) {
				cp = ((pairFirst & 0x3FF) << 10) + 0x10000 + (cp & 0x3FF)
				pairFirst = 0
			} else {
				return invalidPair(off)
			}
		} else if isUTF16SurrogateSecond(cp) {
			return invalidPair(off)
		}
		if isUTF16SurrogateFirst(cp) {
			pairFirst = cp
		} else {
			out = utf8.AppendRune(out, rune(cp))
		}
		return nil
	}

	for i < len(str) {
		if str[i] == escape {
			switch {
			case i+1 < len(str) && str[i+1] == escape:
				if pairFirst != 0 {
					return "", invalidPair(i)
				}
				out = append(out, escape)
				i += 2
			case i+4 < len(str) &&
				isHexDigit(str[i+1]) && isHexDigit(str[i+2]) &&
				isHexDigit(str[i+3]) && isHexDigit(str[i+4]):
				cp := parseHex(str[i+1 : i+5])
				if err := appendCp(cp, i); err != nil {
					return "", err
				}
				i += 5
			case i+7 < len(str) && str[i+1] == '+' &&
				isHexDigit(str[i+2]) && isHexDigit(str[i+3]) &&
				isHexDigit(str[i+4]) && isHexDigit(str[i+5]) &&
				isHexDigit(str[i+6]) && isHexDigit(str[i+7]):
				cp := parseHex(str[i+2 : i+8])
				if err := appendCp(cp, i); err != nil {
					return "", err
				}
				i += 8
			default:
				return "", &Error{
					Message:   "invalid Unicode escape",
					Filename:  parserDotC,
					Funcname:  "str_udeescape",
					Cursorpos: s.cursorpos(errPos(i)),
				}
			}
		} else {
			if pairFirst != 0 {
				return "", invalidPair(i)
			}
			out = append(out, str[i])
			i++
		}
	}

	// parser.c: unfinished surrogate pair?
	if pairFirst != 0 {
		return "", invalidPair(i)
	}
	return string(out), nil
}
