package lexer

// Grammar-side error support: the recursive-descent parser reports errors
// through the scanner because that is how the C stack works — gram.y's
// base_yyerror is parser_yyerror, which calls scanner_yyerror, so every
// "syntax error" carries scan.l/scanner_yyerror error data and a
// character-based cursor position.

// SyntaxError builds bison's yyerror report for the given lookahead token:
// `syntax error at or near "<token text>"` (or "at end of input"), positioned
// at the token's start. The quoted text spans the token's match — parser.c's
// base_yylex pokes a NUL at the current token's end before any lookahead, so
// merged tokens (NOT_LA etc.) report only their first word.
func (f *Filter) SyntaxError(tok Token) *Error {
	return f.s.yyerrorToken(tok, "syntax error")
}

// SyntaxErrorMsg is parser_yyerror(msg) with a message other than "syntax
// error" (e.g. `improper use of "*"`), decorated the same way.
func (f *Filter) SyntaxErrorMsg(tok Token, msg string) *Error {
	return f.s.yyerrorToken(tok, msg)
}

// ParserError is an ereport(ERROR, ...) from a gram.y action or support
// function: no "at or near" decoration; funcname names the C function
// containing the ereport (base_yyparse for inline rule actions).
func (f *Filter) ParserError(funcname, message string, loc int) *Error {
	e := &Error{
		Message:  message,
		Filename: "gram.y",
		Funcname: funcname,
	}
	if loc >= 0 {
		// parser_errposition: -1 means "no position available".
		e.Cursorpos = f.s.cursorpos(loc)
	}
	return e
}

// Cursorpos is scanner_errposition for a byte offset: the 1-based character
// position.
func (f *Filter) Cursorpos(loc int) int {
	return f.s.cursorpos(loc)
}
