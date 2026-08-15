package lexer

import "fmt"

// yyerror is scan.l's scanner_yyerror: the reported text runs from the error
// location (yylloc) to the end of the current match — flex's temporary NUL
// at the match end is what terminates the C "%s" — or to the end of input
// for the <<EOF>> rules. Callers must have s.pos at the match end.
func (s *Scanner) yyerror(loc int, message string) *Error {
	if loc >= len(s.input) {
		return &Error{
			Message:   message + " at end of input",
			Filename:  "scan.l",
			Funcname:  "scanner_yyerror",
			Cursorpos: s.cursorpos(loc),
		}
	}
	return &Error{
		Message:   message + ` at or near "` + s.input[loc:s.pos] + `"`,
		Filename:  "scan.l",
		Funcname:  "scanner_yyerror",
		Cursorpos: s.cursorpos(loc),
	}
}

// ereport is a direct ereport(ERROR, ... lexer_errposition()) from inside a
// scan.l rule action: no "at or near" decoration, and the C __func__ is the
// flex-generated core_yylex.
func (s *Scanner) ereport(loc int, message string) *Error {
	return &Error{
		Message:   message,
		Filename:  "scan.l",
		Funcname:  "core_yylex",
		Cursorpos: s.cursorpos(loc),
	}
}

// cursorpos is scanner_errposition: the 1-based character (not byte) number
// of the given byte offset, counted with pg_mbstrlen_with_len's per-lead-byte
// stride.
func (s *Scanner) cursorpos(loc int) int {
	count := 0
	for i := 0; i < loc && i < len(s.input); count++ {
		i += mblen(s.input[i])
	}
	return count + 1
}

// encodingError is mbutils.c's report_invalid_encoding for the UTF-8 server
// encoding: raised via pg_verifymbstr when an escape produced a NUL or
// high-bit byte and the resulting literal is not valid UTF-8. No error
// position is attached (scan.l installs no errposition callback around it).
func encodingError(seq []byte) *Error {
	limit := len(seq)
	if l := mblen(seq[0]); l < limit {
		limit = l
	}
	if limit > 8 {
		limit = 8
	}
	msg := `invalid byte sequence for encoding "UTF8":`
	for j := 0; j < limit; j++ {
		msg += fmt.Sprintf(" 0x%02x", seq[j])
	}
	return &Error{
		Message:  msg,
		Filename: "src_backend_utils_mb_mbutils.c",
		Funcname: "report_invalid_encoding",
	}
}

// verifyUTF8 is pg_verifymbstr for the UTF-8 server encoding
// (pg_utf8_verifystr semantics, one character at a time).
func verifyUTF8(b []byte) *Error {
	i := 0
	for i < len(b) {
		c := b[i]
		if c < 0x80 {
			if c == 0 {
				return encodingError(b[i:])
			}
			i++
			continue
		}
		l := mblen(c)
		if i+l > len(b) || !utf8CharLegal(b[i:i+l]) {
			return encodingError(b[i:])
		}
		i += l
	}
	return nil
}

// utf8CharLegal is wchar.c's pg_utf8_islegal.
func utf8CharLegal(source []byte) bool {
	switch len(source) {
	case 1:
		return source[0] < 0x80
	case 2:
		if source[0] < 0xC2 {
			return false
		}
		return source[1] >= 0x80 && source[1] <= 0xBF
	case 3:
		if source[2] < 0x80 || source[2] > 0xBF {
			return false
		}
		switch source[0] {
		case 0xE0:
			return source[1] >= 0xA0 && source[1] <= 0xBF
		case 0xED:
			return source[1] >= 0x80 && source[1] <= 0x9F
		default:
			return source[1] >= 0x80 && source[1] <= 0xBF
		}
	case 4:
		if source[3] < 0x80 || source[3] > 0xBF {
			return false
		}
		if source[2] < 0x80 || source[2] > 0xBF {
			return false
		}
		switch source[0] {
		case 0xF0:
			return source[1] >= 0x90 && source[1] <= 0xBF
		case 0xF4:
			return source[1] >= 0x80 && source[1] <= 0x8F
		default:
			return source[1] >= 0x80 && source[1] <= 0xBF
		}
	}
	return false
}
