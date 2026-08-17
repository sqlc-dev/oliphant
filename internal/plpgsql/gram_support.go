package plpgsql

import (
	"fmt"
	"strings"

	"github.com/sqlc-dev/oliphant/internal/deparse"
	"github.com/sqlc-dev/oliphant/internal/parse"
)

// The support functions from the bottom half of pl_gram.y, as built by
// libpg_query (parse_datatype and make_return_stmt are the mocked versions;
// %TYPE/%ROWTYPE lookups return NULL).

// lex/expect/ereport plumbing ------------------------------------------------

func (c *compiler) lex() plToken   { return c.scan.yylex() }
func (c *compiler) cur() *tokenAux { return &c.scan.cur }
func (c *compiler) loc() int       { return c.scan.cur.lloc }

// expect consumes one token and reports bison's plain "syntax error" at it
// when it is not the wanted one.
func (c *compiler) expect(tok plToken) {
	if c.lex() != tok {
		c.scan.yyerror("syntax error")
	}
}

// ereportGram is an ereport from a grammar action (funcname plpgsql_yyparse).
func (c *compiler) ereportGram(format string, args ...any) {
	panic(&compileError{
		Message:  fmt.Sprintf(format, args...),
		Filename: plGramFilename,
		Funcname: "plpgsql_yyparse",
	})
}

// ereportFn is an ereport from a named pl_gram.y support function.
func ereportFn(funcname, format string, args ...any) {
	panic(&compileError{
		Message:  fmt.Sprintf(format, args...),
		Filename: plGramFilename,
		Funcname: funcname,
	})
}

// tok_is_keyword (pl_gram.y).
func (c *compiler) tokIsKeyword(token plToken, kwToken plToken, kwStr string) bool {
	if token == kwToken {
		return true
	}
	if token == T_DATUM {
		w := &c.cur().wdatum
		if !w.quoted && w.ident != "" && w.ident == kwStr {
			return true
		}
	}
	return false
}

// word_is_not_variable / cword_is_not_variable / current_token_is_not_variable.
func wordIsNotVariable(word *plWord) {
	ereportFn("word_is_not_variable", `"%s" is not a known variable`, word.ident)
}

func cwordIsNotVariable(idents []string) {
	ereportFn("cword_is_not_variable", `"%s" is not a known variable`, nameListToString(idents))
}

func (c *compiler) currentTokenIsNotVariable(tok plToken) {
	switch tok {
	case T_WORD:
		wordIsNotVariable(&c.cur().word)
	case T_CWORD:
		cwordIsNotVariable(c.cur().cword)
	default:
		c.scan.yyerror("syntax error")
	}
}

// read_sql_expression / read_sql_expression2 / read_sql_stmt ------------------

func (c *compiler) readSQLExpression(until plToken, expected string) *plExpr {
	expr, _ := c.readSQLConstruct(until, 0, 0, expected, rawParsePlpgsqlExpr, true, true, nil)
	return expr
}

func (c *compiler) readSQLExpression2(until, until2 plToken, expected string) (*plExpr, plToken) {
	return c.readSQLConstruct(until, until2, 0, expected, rawParsePlpgsqlExpr, true, true, nil)
}

func (c *compiler) readSQLStmt() *plExpr {
	expr, _ := c.readSQLConstruct(plToken(';'), 0, 0, ";", rawParseDefault, false, true, nil)
	return expr
}

// read_sql_construct (pl_gram.y).
func (c *compiler) readSQLConstruct(until, until2, until3 plToken, expected string,
	parsemode rawParseMode, isexpression, validSQL bool, startloc *int) (*plExpr, plToken) {

	saveLookup := c.scan.identifierLookup
	c.scan.identifierLookup = lookupExpr

	startlocation := -1
	endlocation := -1
	parenlevel := 0
	var tok plToken

	for {
		tok = c.lex()
		if startlocation < 0 {
			startlocation = c.loc()
		}
		if tok == until && parenlevel == 0 {
			break
		}
		if tok == until2 && parenlevel == 0 {
			break
		}
		if until3 != 0 && tok == until3 && parenlevel == 0 {
			break
		}
		if tok == plToken('(') || tok == plToken('[') {
			parenlevel++
		} else if tok == plToken(')') || tok == plToken(']') {
			parenlevel--
			if parenlevel < 0 {
				c.scan.yyerror("mismatched parentheses")
			}
		}
		if tok == 0 || tok == plToken(';') {
			if parenlevel != 0 {
				c.scan.yyerror("mismatched parentheses")
			}
			if isexpression {
				c.ereportGram(`missing "%s" at end of SQL expression`, expected)
			}
			c.ereportGram(`missing "%s" at end of SQL statement`, expected)
		}
		endlocation = c.loc() + c.scan.tokenLength()
	}

	c.scan.identifierLookup = saveLookup

	if startloc != nil {
		*startloc = startlocation
	}

	if startlocation >= endlocation {
		if isexpression {
			c.scan.yyerror("missing expression")
		}
		c.scan.yyerror("missing SQL statement")
	}

	expr := &plExpr{
		query:     c.scan.sourceText(startlocation, endlocation),
		parseMode: parsemode,
	}

	if validSQL {
		c.checkSQLExpr(expr.query, expr.parseMode, startlocation)
	}

	return expr, tok
}

// read_datatype (pl_gram.y). tok < 0 means no lookahead (YYEMPTY).
func (c *compiler) readDatatype(tok plToken, haveTok bool) *plType {
	var result *plType

	if !haveTok {
		tok = c.lex()
	}

	startlocation := c.loc()
	parenlevel := 0

	// If we have a simple or composite identifier, check for %TYPE and
	// %ROWTYPE constructs (stubbed to reference-text typenames here).
	if tok == T_WORD {
		dtname := c.cur().word.ident
		tok = c.lex()
		if tok == plToken('%') {
			tok = c.lex()
			if c.tokIsKeyword(tok, K_TYPE, "type") {
				result = c.parseWordType(dtname)
			} else if c.tokIsKeyword(tok, K_ROWTYPE, "rowtype") {
				result = c.parseWordRowType(dtname)
			}
		}
	} else if tokenIsUnreservedKeyword(tok) {
		dtname := c.cur().keyword
		tok = c.lex()
		if tok == plToken('%') {
			tok = c.lex()
			if c.tokIsKeyword(tok, K_TYPE, "type") {
				result = c.parseWordType(dtname)
			} else if c.tokIsKeyword(tok, K_ROWTYPE, "rowtype") {
				result = c.parseWordRowType(dtname)
			}
		}
	} else if tok == T_CWORD {
		dtnames := c.cur().cword
		tok = c.lex()
		if tok == plToken('%') {
			tok = c.lex()
			if c.tokIsKeyword(tok, K_TYPE, "type") {
				result = c.parseCwordType(dtnames)
			} else if c.tokIsKeyword(tok, K_ROWTYPE, "rowtype") {
				result = c.parseCwordRowType(dtnames)
			}
		}
	}

	if result != nil {
		// %TYPE/%ROWTYPE recognized: check for array decoration.
		isArray := false
		tok = c.lex()
		if c.tokIsKeyword(tok, K_ARRAY, "array") {
			isArray = true
			tok = c.lex()
		}
		for tok == plToken('[') {
			isArray = true
			tok = c.lex()
			if tok == tokICONST {
				tok = c.lex()
			}
			if tok != plToken(']') {
				c.scan.yyerror(`syntax error, expected "]"`)
			}
			tok = c.lex()
		}
		c.scan.pushBackToken(tok)
		if isArray {
			result = c.buildDatatypeArrayof(result)
		}
		return result
	}

	// Not %TYPE or %ROWTYPE: scan to the end of the datatype declaration.
	for tok != plToken(';') {
		if tok == 0 {
			if parenlevel != 0 {
				c.scan.yyerror("mismatched parentheses")
			}
			c.scan.yyerror("incomplete data type declaration")
		}
		// Possible followers for datatype in a declaration
		if tok == K_COLLATE || tok == K_NOT ||
			tok == plToken('=') || tok == tokCOLON_EQUALS || tok == K_DEFAULT {
			break
		}
		// Possible followers for datatype in a cursor_arg list
		if (tok == plToken(',') || tok == plToken(')')) && parenlevel == 0 {
			break
		}
		if tok == plToken('(') {
			parenlevel++
		} else if tok == plToken(')') {
			parenlevel--
		}
		tok = c.lex()
	}

	typeName := c.scan.sourceText(startlocation, c.loc())
	if typeName == "" {
		c.scan.yyerror("missing data type declaration")
	}

	result = c.parseDatatypeAt(typeName, startlocation)

	c.scan.pushBackToken(tok)
	return result
}

// parse_datatype — the vendored mock: no core-parser round trip; the type
// name is kept verbatim, with only RECORD/REFCURSOR/CURSOR/TEXT recognized
// by (length-limited, case-insensitive) prefix comparison.
func parseDatatype(s string) *plType {
	n := len(s)
	for n > 0 && isSpaceByte(s[n-1]) {
		n--
	}
	typ := &plType{typname: s, ttype: ttypeScalar}
	if pgStrncasecmpEqual(s, "RECORD", n) {
		typ.ttype = ttypeRec
	}
	if pgStrncasecmpEqual(s, "REFCURSOR", n) || pgStrncasecmpEqual(s, "CURSOR", n) {
		typ.typoid = oidRefcursor
	} else if pgStrncasecmpEqual(s, "TEXT", n) {
		typ.typoid = oidText
		typ.collation = oidDefaultCollation
	}
	return typ
}

// pgStrncasecmpEqual is pg_strncasecmp(s, target, n) == 0, with C string
// semantics (target's NUL participates in the comparison).
func pgStrncasecmpEqual(s, target string, n int) bool {
	for i := 0; i < n; i++ {
		var a, b byte
		if i < len(s) {
			a = asciiLower(s[i])
		}
		if i < len(target) {
			b = asciiLower(target[i])
		}
		if a != b {
			return false
		}
		if a == 0 {
			return true
		}
	}
	return true
}

func asciiLower(c byte) byte {
	if c >= 'A' && c <= 'Z' {
		return c + ('a' - 'A')
	}
	return c
}

// scanner_isspace (scansup.c).
func isSpaceByte(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\f' || c == '\v'
}

// make_execsql_stmt (pl_gram.y): read a generic SQL statement, spotting a
// PL/pgSQL INTO clause.
func (c *compiler) makeExecsqlStmt(firsttoken plToken, location int, word *plWord) plStmt {
	saveLookup := c.scan.identifierLookup
	c.scan.identifierLookup = lookupExpr

	var target plVariable
	haveInto := false
	haveStrict := false
	intoStartLoc := -1
	intoEndLoc := -1
	parenDepth := 0
	beginDepth := 0
	inRoutineDefinition := false
	tokenCount := 0
	var tokens [4]byte

	tok := firsttoken
	if tok == T_WORD && word != nil && word.ident == "create" {
		tokens[tokenCount] = 'c'
	}
	tokenCount++

	for {
		prevTok := tok
		tok = c.lex()
		if haveInto && intoEndLoc < 0 {
			intoEndLoc = c.loc() // token after the INTO part
		}
		// Detect CREATE [OR REPLACE] {FUNCTION|PROCEDURE}
		if tokens[0] == 'c' && tokenCount < len(tokens) {
			if tok == K_OR {
				tokens[tokenCount] = 'o'
			} else if tok == T_WORD && c.cur().word.ident == "replace" {
				tokens[tokenCount] = 'r'
			} else if tok == T_WORD && c.cur().word.ident == "function" {
				tokens[tokenCount] = 'f'
			} else if tok == T_WORD && c.cur().word.ident == "procedure" {
				tokens[tokenCount] = 'f' // treat same as "function"
			}
			if tokens[1] == 'f' ||
				(tokens[1] == 'o' && tokens[2] == 'r' && tokens[3] == 'f') {
				inRoutineDefinition = true
			}
			tokenCount++
		}
		// Track paren nesting (needed for CREATE RULE syntax)
		if tok == plToken('(') {
			parenDepth++
		} else if tok == plToken(')') && parenDepth > 0 {
			parenDepth--
		}
		// We need track BEGIN/END nesting only in a routine definition
		if inRoutineDefinition && parenDepth == 0 {
			if tok == K_BEGIN || tok == K_CASE {
				beginDepth++
			} else if tok == K_END && beginDepth > 0 {
				beginDepth--
			}
		}
		// Command-ending semicolon?
		if tok == plToken(';') && parenDepth == 0 && beginDepth == 0 {
			break
		}
		if tok == 0 {
			c.scan.yyerror("unexpected end of function definition")
		}
		if tok == K_INTO {
			if prevTok == K_INSERT {
				continue // INSERT INTO is not an INTO-target
			}
			if prevTok == K_MERGE {
				continue // MERGE INTO is not an INTO-target
			}
			if firsttoken == K_IMPORT {
				continue // IMPORT ... INTO is not an INTO-target
			}
			if haveInto {
				c.scan.yyerror("INTO specified more than once")
			}
			haveInto = true
			intoStartLoc = c.loc()
			c.scan.identifierLookup = lookupNormal
			target, haveStrict = c.readIntoTarget(true)
			c.scan.identifierLookup = lookupExpr
		}
	}

	c.scan.identifierLookup = saveLookup

	var ds string
	if haveInto {
		// Insert spaces for the INTO text so locations still line up.
		ds = c.scan.sourceText(location, intoStartLoc) +
			strings.Repeat(" ", intoEndLoc-intoStartLoc) +
			c.scan.sourceText(intoEndLoc, c.loc())
	} else {
		ds = c.scan.sourceText(location, c.loc())
	}

	// trim any trailing whitespace, for neatness
	for len(ds) > 0 && isSpaceByte(ds[len(ds)-1]) {
		ds = ds[:len(ds)-1]
	}

	expr := &plExpr{query: ds, parseMode: rawParseDefault}
	c.checkSQLExpr(expr.query, expr.parseMode, location)

	execsql := &stmtExecsql{
		sqlstmt: expr,
		into:    haveInto,
		strict:  haveStrict,
		target:  target,
	}
	execsql.lineno = c.scan.locationToLineno(location)
	return execsql
}

// read_fetch_direction (pl_gram.y).
func (c *compiler) readFetchDirection() *stmtFetch {
	fetch := &stmtFetch{
		direction: fetchForward,
		howMany:   1,
	}
	checkFROM := true

	tok := c.lex()
	if tok == 0 {
		c.scan.yyerror("unexpected end of function definition")
	}

	switch {
	case c.tokIsKeyword(tok, K_NEXT, "next"):
		// use defaults
	case c.tokIsKeyword(tok, K_PRIOR, "prior"):
		fetch.direction = fetchBackward
	case c.tokIsKeyword(tok, K_FIRST, "first"):
		fetch.direction = fetchAbsolute
	case c.tokIsKeyword(tok, K_LAST, "last"):
		fetch.direction = fetchAbsolute
		fetch.howMany = -1
	case c.tokIsKeyword(tok, K_ABSOLUTE, "absolute"):
		fetch.direction = fetchAbsolute
		fetch.expr, _ = c.readSQLExpression2(K_FROM, K_IN, "FROM or IN")
		checkFROM = false
	case c.tokIsKeyword(tok, K_RELATIVE, "relative"):
		fetch.direction = fetchRelative
		fetch.expr, _ = c.readSQLExpression2(K_FROM, K_IN, "FROM or IN")
		checkFROM = false
	case c.tokIsKeyword(tok, K_ALL, "all"):
		fetch.howMany = fetchAll
		fetch.returnsMultipleRows = true
	case c.tokIsKeyword(tok, K_FORWARD, "forward"):
		c.completeDirection(fetch, &checkFROM)
	case c.tokIsKeyword(tok, K_BACKWARD, "backward"):
		fetch.direction = fetchBackward
		c.completeDirection(fetch, &checkFROM)
	case tok == K_FROM || tok == K_IN:
		// empty direction
		checkFROM = false
	case tok == T_DATUM:
		// Assume there's no direction clause and tok is a cursor name
		c.scan.pushBackToken(tok)
		checkFROM = false
	default:
		// Assume it's a count expression with no preceding keyword.
		c.scan.pushBackToken(tok)
		fetch.expr, _ = c.readSQLExpression2(K_FROM, K_IN, "FROM or IN")
		fetch.returnsMultipleRows = true
		checkFROM = false
	}

	if checkFROM {
		tok = c.lex()
		if tok != K_FROM && tok != K_IN {
			c.scan.yyerror("expected FROM or IN")
		}
	}

	return fetch
}

// complete_direction (pl_gram.y).
func (c *compiler) completeDirection(fetch *stmtFetch, checkFROM *bool) {
	tok := c.lex()
	if tok == 0 {
		c.scan.yyerror("unexpected end of function definition")
	}
	if tok == K_FROM || tok == K_IN {
		*checkFROM = false
		return
	}
	if tok == K_ALL {
		fetch.howMany = fetchAll
		fetch.returnsMultipleRows = true
		*checkFROM = true
		return
	}
	c.scan.pushBackToken(tok)
	fetch.expr, _ = c.readSQLExpression2(K_FROM, K_IN, "FROM or IN")
	fetch.returnsMultipleRows = true
	*checkFROM = false
}

// make_return_stmt (pl_gram.y, unmocked at 18): RETURN of a simple
// variable/row/rec datum captures retvarno; SETOF/VOID/OUT-parameter
// functions reject a parameter with their specific errors.
func (c *compiler) makeReturnStmt(location int) plStmt {
	st := &stmtReturn{retvarno: -1}
	st.lineno = c.scan.locationToLineno(location)
	switch {
	case c.fn.fnRetset:
		if c.lex() != plToken(';') {
			ereportFn("make_return_stmt", "RETURN cannot have a parameter in function returning set")
		}
	case c.fn.fnRettype == oidVoid:
		if c.lex() != plToken(';') {
			if c.fn.fnIsProc {
				ereportFn("make_return_stmt", "RETURN cannot have a parameter in a procedure")
			}
			ereportFn("make_return_stmt", "RETURN cannot have a parameter in function returning void")
		}
	case c.fn.outParamVarno >= 0:
		if c.lex() != plToken(';') {
			ereportFn("make_return_stmt", "RETURN cannot have a parameter in function with OUT parameters")
		}
		st.retvarno = c.fn.outParamVarno
	default:
		// Special-case simple variable references for efficiency.
		tok := c.lex()
		if tok == T_DATUM && c.scan.peek() == plToken(';') &&
			isSimpleReturnDatum(c.cur().wdatum.datum) {
			st.retvarno = c.cur().wdatum.datum.datumNo()
			c.lex() // eat the semicolon token that we only peeked at above
		} else {
			c.scan.pushBackToken(tok)
			st.expr = c.readSQLExpression(plToken(';'), ";")
		}
	}
	return st
}

// make_return_next_stmt (pl_gram.y, unmocked).
func (c *compiler) makeReturnNextStmt(location int) plStmt {
	if !c.fn.fnRetset {
		ereportFn("make_return_next_stmt", "cannot use RETURN NEXT in a non-SETOF function")
	}
	st := &stmtReturnNext{retvarno: -1}
	st.lineno = c.scan.locationToLineno(location)
	if c.fn.outParamVarno >= 0 {
		if c.lex() != plToken(';') {
			ereportFn("make_return_next_stmt", "RETURN NEXT cannot have a parameter in function with OUT parameters")
		}
		st.retvarno = c.fn.outParamVarno
	} else {
		tok := c.lex()
		if tok == T_DATUM && c.scan.peek() == plToken(';') &&
			isSimpleReturnDatum(c.cur().wdatum.datum) {
			st.retvarno = c.cur().wdatum.datum.datumNo()
			c.lex() // eat the semicolon token that we only peeked at above
		} else {
			c.scan.pushBackToken(tok)
			st.expr = c.readSQLExpression(plToken(';'), ";")
		}
	}
	return st
}

func isSimpleReturnDatum(d datum) bool {
	switch d.datumType() {
	case dtypeVar, dtypePromise, dtypeRow, dtypeRec:
		return true
	}
	return false
}

// make_return_query_stmt (pl_gram.y).
func (c *compiler) makeReturnQueryStmt(location int) plStmt {
	if !c.fn.fnRetset {
		ereportFn("make_return_query_stmt", "cannot use RETURN QUERY in a non-SETOF function")
	}
	st := &stmtReturnQuery{}
	st.lineno = c.scan.locationToLineno(location)

	if tok := c.lex(); tok != K_EXECUTE {
		c.scan.pushBackToken(tok)
		st.query = c.readSQLStmt()
	} else {
		var term plToken
		st.dynquery, term = c.readSQLExpression2(plToken(';'), K_USING, "; or USING")
		if term == K_USING {
			for {
				expr, t := c.readSQLExpression2(plToken(','), plToken(';'), ", or ;")
				st.params = append(st.params, expr)
				if t != plToken(',') {
					break
				}
			}
		}
	}
	return st
}

// NameOfDatum (pl_gram.y).
func nameOfDatum(w *plWdatum) string {
	if w.ident != "" {
		return w.ident
	}
	return nameListToString(w.idents)
}

// check_assignable (pl_gram.y).
func (c *compiler) checkAssignable(d datum) {
	switch d.datumType() {
	case dtypeVar, dtypePromise, dtypeRec:
		v := d.(plVariable)
		if v.varIsConst() {
			ereportFn("check_assignable", `variable "%s" is declared CONSTANT`, v.varRefname())
		}
	case dtypeRow:
		// always assignable; member vars were checked at compile time
	case dtypeRecfield:
		// assignable if parent record is
		c.checkAssignable(c.datums[d.(*plRecfield).recparentno])
	}
}

// read_into_target (pl_gram.y). wantStrict selects whether STRICT is legal
// (the C signature's strict != NULL).
func (c *compiler) readIntoTarget(wantStrict bool) (target plVariable, strict bool) {
	tok := c.lex()
	if wantStrict && tok == K_STRICT {
		strict = true
		tok = c.lex()
	}

	switch tok {
	case T_DATUM:
		w := c.cur().wdatum
		if w.datum.datumType() == dtypeRow || w.datum.datumType() == dtypeRec {
			c.checkAssignable(w.datum)
			target = w.datum.(plVariable)
			if tok = c.lex(); tok == plToken(',') {
				c.ereportGram("record variable cannot be part of multiple-item INTO list")
			}
			c.scan.pushBackToken(tok)
		} else {
			target = c.readIntoScalarList(nameOfDatum(&w), w.datum, c.loc())
		}
	default:
		// just to give a better message than "syntax error"
		c.currentTokenIsNotVariable(tok)
	}
	return target, strict
}

// read_into_scalar_list (pl_gram.y).
func (c *compiler) readIntoScalarList(initialName string, initialDatum datum, initialLocation int) *plRow {
	c.checkAssignable(initialDatum)
	fieldnames := []string{initialName}
	varnos := []int{initialDatum.datumNo()}

	var tok plToken
	for {
		tok = c.lex()
		if tok != plToken(',') {
			break
		}
		if len(fieldnames) >= 1024 {
			c.ereportGram("too many INTO variables specified")
		}
		tok = c.lex()
		switch tok {
		case T_DATUM:
			w := c.cur().wdatum
			c.checkAssignable(w.datum)
			if w.datum.datumType() == dtypeRow || w.datum.datumType() == dtypeRec {
				c.ereportGram(`"%s" is not a scalar variable`, nameOfDatum(&w))
			}
			fieldnames = append(fieldnames, nameOfDatum(&w))
			varnos = append(varnos, w.datum.datumNo())
		default:
			c.currentTokenIsNotVariable(tok)
		}
	}
	c.scan.pushBackToken(tok)

	row := &plRow{
		refname:    "(unnamed row)",
		lineno:     c.scan.locationToLineno(initialLocation),
		fieldnames: fieldnames,
		varnos:     varnos,
	}
	c.addDatum(row)
	return row
}

// make_scalar_list1 (pl_gram.y).
func (c *compiler) makeScalarList1(initialName string, initialDatum datum, lineno int) *plRow {
	c.checkAssignable(initialDatum)
	row := &plRow{
		refname:    "(unnamed row)",
		lineno:     lineno,
		fieldnames: []string{initialName},
		varnos:     []int{initialDatum.datumNo()},
	}
	c.addDatum(row)
	return row
}

// check_sql_expr (pl_gram.y): run the embedded SQL through the main parser.
// On failure, plpgsql_sql_error_callback flushes the external error cursor
// (only an internal one is kept), so the reported error loses its position
// but keeps message/filename/funcname.
func (c *compiler) checkSQLExpr(stmt string, parseMode rawParseMode, location int) {
	if !c.checkSyntax {
		return
	}
	var mode parse.Mode
	switch parseMode {
	case rawParsePlpgsqlExpr:
		mode = parse.ModePlpgsqlExpr
	case rawParsePlpgsqlAssign1:
		mode = parse.ModePlpgsqlAssign1
	case rawParsePlpgsqlAssign2:
		mode = parse.ModePlpgsqlAssign2
	case rawParsePlpgsqlAssign3:
		mode = parse.ModePlpgsqlAssign3
	default:
		mode = parse.ModeDefault
	}
	if _, err := parse.ParseWithMode(stmt, mode); err != nil {
		panic(&compileError{
			Message:  err.Message,
			Filename: err.Filename,
			Funcname: err.Funcname,
		})
	}
}

// check_labels (pl_gram.y).
func checkLabels(startLabel, endLabel string) {
	if endLabel != "" {
		if startLabel == "" {
			ereportFn("check_labels", `end label "%s" specified for unlabeled block`, endLabel)
		}
		if startLabel != endLabel {
			ereportFn("check_labels", `end label "%s" differs from block's label "%s"`, endLabel, startLabel)
		}
	}
}

// read_cursor_args (pl_gram.y).
func (c *compiler) readCursorArgs(cursor *plVar, until plToken) *plExpr {
	tok := c.lex()
	if cursor.cursorExplicitArgrow < 0 {
		// No arguments expected
		if tok == plToken('(') {
			c.ereportGram(`cursor "%s" has no arguments`, cursor.refname)
		}
		if tok != until {
			c.scan.yyerror("syntax error")
		}
		return nil
	}

	// Else better provide arguments
	if tok != plToken('(') {
		c.ereportGram(`cursor "%s" has arguments`, cursor.refname)
	}

	row := c.datums[cursor.cursorExplicitArgrow].(*plRow)
	argv := make([]string, len(row.fieldnames))
	filled := make([]bool, len(row.fieldnames))
	anyNamed := false

	for argc := 0; argc < len(row.fieldnames); argc++ {
		argpos := argc

		// Check if it's a named parameter: "param := value"
		tok1, tok2, _, _ := c.scan.peek2()
		if tok1 == tokIDENT && tok2 == tokCOLON_EQUALS {
			// Read the argument name, ignoring any matching variable
			saveLookup := c.scan.identifierLookup
			c.scan.identifierLookup = lookupDeclare
			c.lex()
			argname := c.cur().str
			c.scan.identifierLookup = saveLookup

			// Match argument name to cursor arguments
			argpos = -1
			for i, fn := range row.fieldnames {
				if fn == argname {
					argpos = i
					break
				}
			}
			if argpos < 0 {
				c.ereportGram(`cursor "%s" has no argument named "%s"`, cursor.refname, argname)
			}

			// Eat the ":=".
			if c.lex() != tokCOLON_EQUALS {
				c.scan.yyerror("syntax error")
			}

			anyNamed = true
		}

		if filled[argpos] {
			c.ereportGram(`value for parameter "%s" of cursor "%s" specified more than once`,
				row.fieldnames[argpos], cursor.refname)
		}

		item, endtoken := c.readSQLConstruct(plToken(','), plToken(')'), 0,
			",\" or \")", rawParsePlpgsqlExpr, true, true, nil)

		argv[argpos] = item.query
		filled[argpos] = true

		if endtoken == plToken(')') && argc != len(row.fieldnames)-1 {
			c.ereportGram(`not enough arguments for cursor "%s"`, cursor.refname)
		}
		if endtoken == plToken(',') && argc == len(row.fieldnames)-1 {
			c.ereportGram(`too many arguments for cursor "%s"`, cursor.refname)
		}
	}

	// Make positional argument list
	var ds strings.Builder
	for argc := 0; argc < len(row.fieldnames); argc++ {
		ds.WriteString(argv[argc])
		if anyNamed {
			ds.WriteString(" AS " + deparse.QuoteIdentifier(row.fieldnames[argc]))
		}
		if argc < len(row.fieldnames)-1 {
			ds.WriteString(", ")
		}
	}

	expr := &plExpr{query: ds.String(), parseMode: rawParsePlpgsqlExpr}

	// Next we'd better find the until token
	if c.lex() != until {
		c.scan.yyerror("syntax error")
	}

	return expr
}

// read_raise_options (pl_gram.y).
func (c *compiler) readRaiseOptions() []*raiseOption {
	var result []*raiseOption
	for {
		tok := c.lex()
		if tok == 0 {
			c.scan.yyerror("unexpected end of function definition")
		}

		opt := &raiseOption{}
		switch {
		case c.tokIsKeyword(tok, K_ERRCODE, "errcode"):
			opt.optType = raiseOptionErrcode
		case c.tokIsKeyword(tok, K_MESSAGE, "message"):
			opt.optType = raiseOptionMessage
		case c.tokIsKeyword(tok, K_DETAIL, "detail"):
			opt.optType = raiseOptionDetail
		case c.tokIsKeyword(tok, K_HINT, "hint"):
			opt.optType = raiseOptionHint
		case c.tokIsKeyword(tok, K_COLUMN, "column"):
			opt.optType = raiseOptionColumn
		case c.tokIsKeyword(tok, K_CONSTRAINT, "constraint"):
			opt.optType = raiseOptionConstraint
		case c.tokIsKeyword(tok, K_DATATYPE, "datatype"):
			opt.optType = raiseOptionDatatype
		case c.tokIsKeyword(tok, K_TABLE, "table"):
			opt.optType = raiseOptionTable
		case c.tokIsKeyword(tok, K_SCHEMA, "schema"):
			opt.optType = raiseOptionSchema
		default:
			c.scan.yyerror("unrecognized RAISE statement option")
		}

		tok = c.lex()
		if tok != plToken('=') && tok != tokCOLON_EQUALS {
			c.scan.yyerror(`syntax error, expected "="`)
		}

		var endTok plToken
		opt.expr, endTok = c.readSQLExpression2(plToken(','), plToken(';'), ", or ;")

		result = append(result, opt)

		if endTok == plToken(';') {
			break
		}
	}
	return result
}

// check_raise_parameters (pl_gram.y).
func checkRaiseParameters(st *stmtRaise) {
	if !st.hasMessage {
		return
	}
	expected := 0
	msg := st.message
	for i := 0; i < len(msg); i++ {
		if msg[i] == '%' {
			if i+1 < len(msg) && msg[i+1] == '%' {
				i++
			} else {
				expected++
			}
		}
	}
	if expected < len(st.params) {
		ereportFn("check_raise_parameters", "too many parameters specified for RAISE")
	}
	if expected > len(st.params) {
		ereportFn("check_raise_parameters", "too few parameters specified for RAISE")
	}
}

// make_case (pl_gram.y).
func (c *compiler) makeCase(location int, tExpr *plExpr, caseWhenList []*caseWhen, elseStmts []plStmt) plStmt {
	st := &stmtCase{
		tExpr:        tExpr,
		caseWhenList: caseWhenList,
		haveElse:     elseStmts != nil,
	}
	st.lineno = c.scan.locationToLineno(location)
	// Get rid of list-with-NULL hack
	if len(elseStmts) == 1 && elseStmts[0] == nil {
		st.elseStmts = nil
	} else {
		st.elseStmts = elseStmts
	}

	if tExpr != nil {
		// use a name unlikely to collide with any user names
		varname := fmt.Sprintf("__Case__Variable_%d__", len(c.datums))

		tVar := c.buildVariable(varname, st.lineno,
			c.plpgsqlBuildDatatype(oidInt4, -1, 0, nil), true).(*plVar)
		st.tVarno = tVar.dno

		for _, cwt := range caseWhenList {
			expr := cwt.expr
			expr.query = fmt.Sprintf(`"%s" IN (%s)`, varname, expr.query)
		}
	}

	return st
}
