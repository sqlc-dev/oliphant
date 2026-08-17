package plpgsql

import (
	"strings"

	"github.com/sqlc-dev/oliphant/ast"
	"github.com/sqlc-dev/oliphant/internal/lexer"
)

// plToken is a token code as pl_gram.y sees it: single characters are their
// byte values, core-lexer tokens keep the bison numbers of the pinned SQL
// grammar (the ast.Token values), and the PL-specific tokens (T_WORD,
// T_CWORD, T_DATUM, LESS_LESS, GREATER_GREATER, and the K_* keywords) get
// codes far above that range.
type plToken int32

const (
	tokEOF          = plToken(0)
	tokIDENT        = plToken(ast.Token_IDENT)
	tokUIDENT       = plToken(ast.Token_UIDENT)
	tokFCONST       = plToken(ast.Token_FCONST)
	tokSCONST       = plToken(ast.Token_SCONST)
	tokUSCONST      = plToken(ast.Token_USCONST)
	tokBCONST       = plToken(ast.Token_BCONST)
	tokXCONST       = plToken(ast.Token_XCONST)
	tokOp           = plToken(ast.Token_Op)
	tokICONST       = plToken(ast.Token_ICONST)
	tokPARAM        = plToken(ast.Token_PARAM)
	tokTYPECAST     = plToken(ast.Token_TYPECAST)
	tokDOT_DOT      = plToken(ast.Token_DOT_DOT)
	tokCOLON_EQUALS = plToken(ast.Token_COLON_EQUALS)
)

// pl_gram.y token declarations (the PL-specific ones).
const (
	T_WORD plToken = 20000 + iota
	T_CWORD
	T_DATUM
	LESS_LESS
	GREATER_GREATER
	K_ABSOLUTE
	K_ALIAS
	K_ALL
	K_AND
	K_ARRAY
	K_ASSERT
	K_BACKWARD
	K_BEGIN
	K_BY
	K_CALL
	K_CASE
	K_CHAIN
	K_CLOSE
	K_COLLATE
	K_COLUMN
	K_COLUMN_NAME
	K_COMMIT
	K_CONSTANT
	K_CONSTRAINT
	K_CONSTRAINT_NAME
	K_CONTINUE
	K_CURRENT
	K_CURSOR
	K_DATATYPE
	K_DEBUG
	K_DECLARE
	K_DEFAULT
	K_DETAIL
	K_DIAGNOSTICS
	K_DO
	K_DUMP
	K_ELSE
	K_ELSIF
	K_END
	K_ERRCODE
	K_ERROR
	K_EXCEPTION
	K_EXECUTE
	K_EXIT
	K_FETCH
	K_FIRST
	K_FOR
	K_FOREACH
	K_FORWARD
	K_FROM
	K_GET
	K_HINT
	K_IF
	K_IMPORT
	K_IN
	K_INFO
	K_INSERT
	K_INTO
	K_IS
	K_LAST
	K_LOG
	K_LOOP
	K_MERGE
	K_MESSAGE
	K_MESSAGE_TEXT
	K_MOVE
	K_NEXT
	K_NO
	K_NOT
	K_NOTICE
	K_NULL
	K_OPEN
	K_OPTION
	K_OR
	K_PERFORM
	K_PG_CONTEXT
	K_PG_DATATYPE_NAME
	K_PG_EXCEPTION_CONTEXT
	K_PG_EXCEPTION_DETAIL
	K_PG_EXCEPTION_HINT
	K_PG_ROUTINE_OID
	K_PRINT_STRICT_PARAMS
	K_PRIOR
	K_QUERY
	K_RAISE
	K_RELATIVE
	K_RETURN
	K_RETURNED_SQLSTATE
	K_REVERSE
	K_ROLLBACK
	K_ROW_COUNT
	K_ROWTYPE
	K_SCHEMA
	K_SCHEMA_NAME
	K_SCROLL
	K_SLICE
	K_SQLSTATE
	K_STACKED
	K_STRICT
	K_TABLE
	K_TABLE_NAME
	K_THEN
	K_TO
	K_TYPE
	K_USE_COLUMN
	K_USE_VARIABLE
	K_USING
	K_VARIABLE_CONFLICT
	K_WARNING
	K_WHEN
	K_WHILE
)

// IdentifierLookup modes (plpgsql.h).
type identifierLookup int

const (
	lookupNormal identifierLookup = iota
	lookupDeclare
	lookupExpr
)

// plWord / plCword / plWdatum are PLword / PLcword / PLwdatum.
type plWord struct {
	ident  string
	quoted bool
}

type plWdatum struct {
	datum  datum
	ident  string
	quoted bool
	idents []string
}

// tokenAux carries one token plus its auxiliary data (TokenAuxData: the
// YYSTYPE union collapses to a struct here; str doubles as the union's
// leading char* member, so it stays filled alongside word/keyword).
type tokenAux struct {
	tok  plToken
	lloc int
	leng int

	str     string
	ival    int32
	keyword string
	word    plWord
	cword   []string
	wdatum  plWdatum
}

// plScanner is pl_scanner.c's state: the core scanner (initialized with the
// PL reserved keyword list — modeled by re-classifying the SQL lexer's
// output), the pushback stack, and the location-to-lineno tracker.
type plScanner struct {
	src  string
	core *lexer.Scanner

	identifierLookup identifierLookup

	cur tokenAux // token last returned by yylex (yylval/yylloc/yyleng/yytoken)

	pushback []tokenAux

	curLineStart int
	curLineEnd   int // byte index of the next '\n', or -1
	curLineNum   int

	comp *compiler // for plpgsql_parse_word and friends
}

// newScanner is plpgsql_scanner_init.
func newScanner(src string, comp *compiler) *plScanner {
	core := lexer.New(src)
	s := &plScanner{
		src:              core.Input(),
		core:             core,
		identifierLookup: lookupNormal,
		comp:             comp,
	}
	s.locationLinenoInit()
	return s
}

// internalYylex wraps the core lexer: pushback stack, the << >> # operator
// translations, PARAM-as-text, and comment-token skipping. The keyword
// re-classification models scanner_init with ReservedPLKeywords: SQL
// keywords come back as plain identifiers unless they are PL-reserved.
func (s *plScanner) internalYylex(aux *tokenAux) plToken {
	if n := len(s.pushback); n > 0 {
		*aux = s.pushback[n-1]
		s.pushback = s.pushback[:n-1]
		return aux.tok
	}

	tok, lerr := s.core.Next()
	if lerr != nil {
		// A core-scanner ereport (scan.l): external error position, reported
		// through the compile error path with context attached.
		panic(&compileError{
			Message:   lerr.Message,
			Filename:  lerr.Filename,
			Funcname:  lerr.Funcname,
			Cursorpos: lerr.Cursorpos,
		})
	}

	*aux = tokenAux{
		tok:  plToken(tok.Kind),
		lloc: int(tok.Start),
		leng: int(tok.End - tok.Start),
		str:  tok.Str,
		ival: tok.Ival,
	}

	switch {
	case tok.Kind == 0:
		aux.tok = tokEOF

	case tok.Kind == ast.Token_SQL_COMMENT || tok.Kind == ast.Token_C_COMMENT:
		return s.internalYylex(aux)

	case tok.Kind == ast.Token_Op:
		// Check for << >> and #, which the core considers operators.
		switch tok.Str {
		case "<<":
			aux.tok = LESS_LESS
		case ">>":
			aux.tok = GREATER_GREATER
		case "#":
			aux.tok = plToken('#')
		}

	case tok.Kind == ast.Token_PARAM:
		// The core returns PARAM as ival, but we treat it like IDENT.
		aux.str = s.src[tok.Start:tok.End]

	case tok.KeywordKind != ast.KeywordKind_NO_KEYWORD:
		// A keyword of the SQL scanner. With the PL reserved list installed
		// instead, all of these lex as plain identifiers — except the ones
		// that are PL-reserved, and the n'...' NCHAR special (which falls
		// back to a 1-byte IDENT "n" when "nchar" is not in the list).
		if int(tok.End-tok.Start) != len(tok.Str) && tok.Str == "nchar" {
			aux.tok = tokIDENT
			aux.str = "n"
			break
		}
		if kw, ok := reservedKeywords[tok.Str]; ok {
			aux.tok = kw
			aux.keyword = tok.Str
			break
		}
		aux.tok = tokIDENT

	case tok.Kind == ast.Token_IDENT:
		// Unquoted identifiers can still match PL-reserved words the SQL
		// scanner has no keyword for (LOOP, FOREACH, WHILE, ...).
		if s.src[tok.Start] != '"' {
			if kw, ok := reservedKeywords[tok.Str]; ok {
				aux.tok = kw
				aux.keyword = tok.Str
			}
		}
	}

	return aux.tok
}

// pushBackTokenAux is push_back_token.
func (s *plScanner) pushBackTokenAux(tok plToken, aux *tokenAux) {
	a := *aux
	a.tok = tok
	s.pushback = append(s.pushback, a)
}

// pushBackToken is plpgsql_push_back_token: re-queue the current token.
func (s *plScanner) pushBackToken(tok plToken) {
	s.pushBackTokenAux(tok, &s.cur)
}

// yylex is plpgsql_yylex: compose A.B.C identifiers and classify them as
// T_DATUM / T_CWORD / T_WORD / unreserved keyword.
func (s *plScanner) yylex() plToken {
	var aux1 tokenAux
	tok1 := s.internalYylex(&aux1)
	if tok1 == tokIDENT || tok1 == tokPARAM {
		var aux2 tokenAux
		tok2 := s.internalYylex(&aux2)
		if tok2 == plToken('.') {
			var aux3 tokenAux
			tok3 := s.internalYylex(&aux3)
			if tok3 == tokIDENT {
				var aux4 tokenAux
				tok4 := s.internalYylex(&aux4)
				if tok4 == plToken('.') {
					var aux5 tokenAux
					tok5 := s.internalYylex(&aux5)
					if tok5 == tokIDENT {
						if s.comp.parseTripword(aux1.str, aux3.str, aux5.str, &aux1.wdatum, &aux1.cword) {
							tok1 = T_DATUM
						} else {
							tok1 = T_CWORD
						}
						// Adjust token length to include A.B.C
						aux1.leng = aux5.lloc - aux1.lloc + aux5.leng
					} else {
						// not A.B.C, so just process A.B
						s.pushBackTokenAux(tok5, &aux5)
						s.pushBackTokenAux(tok4, &aux4)
						if s.comp.parseDblword(aux1.str, aux3.str, &aux1.wdatum, &aux1.cword) {
							tok1 = T_DATUM
						} else {
							tok1 = T_CWORD
						}
						aux1.leng = aux3.lloc - aux1.lloc + aux3.leng
					}
				} else {
					// not A.B.C, so just process A.B
					s.pushBackTokenAux(tok4, &aux4)
					if s.comp.parseDblword(aux1.str, aux3.str, &aux1.wdatum, &aux1.cword) {
						tok1 = T_DATUM
					} else {
						tok1 = T_CWORD
					}
					aux1.leng = aux3.lloc - aux1.lloc + aux3.leng
				}
			} else {
				// not A.B, so just process A
				s.pushBackTokenAux(tok3, &aux3)
				s.pushBackTokenAux(tok2, &aux2)
				if s.comp.parseWord(aux1.str, s.quotedAt(aux1.lloc), true, &aux1.wdatum, &aux1.word) {
					tok1 = T_DATUM
				} else if kw, ok := s.unreservedCheck(&aux1); ok {
					tok1 = kw
				} else {
					tok1 = T_WORD
				}
			}
		} else {
			// not A.B, so just process A
			s.pushBackTokenAux(tok2, &aux2)

			// See if it matches a variable name, except at start of statement
			// when the next token isn't assignment or '['.
			lookup := !atStmtStart(s.cur.tok) ||
				(tok2 == plToken('=') || tok2 == tokCOLON_EQUALS || tok2 == plToken('['))
			if s.comp.parseWord(aux1.str, s.quotedAt(aux1.lloc), lookup, &aux1.wdatum, &aux1.word) {
				tok1 = T_DATUM
			} else if kw, ok := s.unreservedCheck(&aux1); ok {
				tok1 = kw
			} else {
				tok1 = T_WORD
			}
		}
	}

	s.cur = aux1
	s.cur.tok = tok1
	return tok1
}

// unreservedCheck applies the unreserved-keyword lookup that follows a
// failed variable lookup (aux.word was filled in by parseWord).
func (s *plScanner) unreservedCheck(aux *tokenAux) (plToken, bool) {
	if aux.word.quoted {
		return 0, false
	}
	if kw, ok := unreservedKeywords[aux.word.ident]; ok {
		aux.keyword = aux.word.ident
		return kw, true
	}
	return 0, false
}

// quotedAt reports whether the token starting at lloc was written quoted
// (yytxt[0] == '"' in the C checks).
func (s *plScanner) quotedAt(lloc int) bool {
	return lloc < len(s.src) && s.src[lloc] == '"'
}

// AT_STMT_START (pl_scanner.c).
func atStmtStart(prevToken plToken) bool {
	return prevToken == plToken(';') || prevToken == K_BEGIN ||
		prevToken == K_THEN || prevToken == K_ELSE || prevToken == K_LOOP
}

// tokenLength is plpgsql_token_length.
func (s *plScanner) tokenLength() int { return s.cur.leng }

// peek is plpgsql_peek.
func (s *plScanner) peek() plToken {
	var aux tokenAux
	tok := s.internalYylex(&aux)
	s.pushBackTokenAux(tok, &aux)
	return tok
}

// peek2 is plpgsql_peek2.
func (s *plScanner) peek2() (tok1, tok2 plToken, tok1loc, tok2loc int) {
	var aux1, aux2 tokenAux
	tok1 = s.internalYylex(&aux1)
	tok2 = s.internalYylex(&aux2)
	tok1loc, tok2loc = aux1.lloc, aux2.lloc
	s.pushBackTokenAux(tok2, &aux2)
	s.pushBackTokenAux(tok1, &aux1)
	return
}

// tokenIsUnreservedKeyword is plpgsql_token_is_unreserved_keyword.
func tokenIsUnreservedKeyword(tok plToken) bool {
	for _, t := range unreservedKeywords {
		if t == tok {
			return true
		}
	}
	return false
}

// appendSourceText is plpgsql_append_source_text.
func (s *plScanner) sourceText(startLocation, endLocation int) string {
	return s.src[startLocation:endLocation]
}

// yyerror is plpgsql_yyerror: report at the current token.
func (s *plScanner) yyerror(message string) {
	if s.cur.lloc >= len(s.src) {
		panic(&compileError{
			Message:  message + " at end of input",
			Filename: plScannerFilename,
			Funcname: "plpgsql_yyerror",
		})
	}
	yytext := s.src[s.cur.lloc:min(s.cur.lloc+s.cur.leng, len(s.src))]
	panic(&compileError{
		Message:  message + ` at or near "` + yytext + `"`,
		Filename: plScannerFilename,
		Funcname: "plpgsql_yyerror",
	})
}

// plScannerFilename is what __FILE__ expands to in the vendored build.
const plScannerFilename = "src_pl_plpgsql_src_pl_scanner.c"

// locationToLineno is plpgsql_location_to_lineno.
func (s *plScanner) locationToLineno(location int) int {
	if location < 0 {
		return 0
	}
	if location < s.curLineStart {
		s.locationLinenoInit()
	}
	for s.curLineEnd >= 0 && location > s.curLineEnd {
		s.curLineStart = s.curLineEnd + 1
		s.curLineNum++
		s.curLineEnd = nextNewline(s.src, s.curLineStart)
	}
	return s.curLineNum
}

func (s *plScanner) locationLinenoInit() {
	s.curLineStart = 0
	s.curLineNum = 1
	s.curLineEnd = nextNewline(s.src, 0)
}

// latestLineno is plpgsql_latest_lineno.
func (s *plScanner) latestLineno() int { return s.curLineNum }

func nextNewline(src string, from int) int {
	if from > len(src) {
		return -1
	}
	idx := strings.IndexByte(src[from:], '\n')
	if idx < 0 {
		return -1
	}
	return from + idx
}
