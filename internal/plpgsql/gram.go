package plpgsql

// The productions of pl_gram.y as recursive descent. Support functions live
// in gram_support.go; the scanner interface layer in scanner.go. Actions run
// where bison runs them (statement linenos are computed at the C action's
// point in the token stream, since plpgsql_latest_lineno is stateful and
// feeds the compile error context).

// parseFunction is pl_function: comp_options pl_block opt_semi.
func (c *compiler) parseFunction() *stmtBlock {
	tok := c.compOptions()

	// pl_block's opt_block_label.
	var label string
	if tok == LESS_LESS {
		label = c.anyIdentifier(c.lex())
		c.expect(GREATER_GREATER)
		tok = c.lex()
	}
	c.nsPush(label, labelBlock)
	blk := c.plBlock(label, tok)

	// opt_semi, then end of input.
	tok = c.lex()
	if tok == plToken(';') {
		tok = c.lex()
	}
	if tok != 0 {
		c.scan.yyerror("syntax error")
	}
	return blk
}

// compOptions is comp_options/comp_option: leading '#' directives. Returns
// the first token that is not part of an option.
func (c *compiler) compOptions() plToken {
	for {
		tok := c.lex()
		if tok != plToken('#') {
			return tok
		}
		tok = c.lex()
		switch tok {
		case K_OPTION:
			c.expect(K_DUMP)
			// plpgsql_DumpExecTree = true: no output effect here
		case K_PRINT_STRICT_PARAMS:
			val := c.optionValue()
			switch val {
			case "on":
				c.fn.printStrictParams = true
			case "off":
				c.fn.printStrictParams = false
			default:
				c.ereportGram("unrecognized print_strict_params option %s", val)
			}
		case K_VARIABLE_CONFLICT:
			tok = c.lex()
			switch tok {
			case K_ERROR:
				c.fn.resolveOption = resolveError
			case K_USE_VARIABLE:
				c.fn.resolveOption = resolveVariable
			case K_USE_COLUMN:
				c.fn.resolveOption = resolveColumn
			default:
				c.scan.yyerror("syntax error")
			}
		default:
			c.scan.yyerror("syntax error")
		}
	}
}

// option_value: T_WORD | unreserved_keyword.
func (c *compiler) optionValue() string {
	tok := c.lex()
	if tok == T_WORD {
		return c.cur().word.ident
	}
	if tokenIsUnreservedKeyword(tok) {
		return c.cur().keyword
	}
	c.scan.yyerror("syntax error")
	return ""
}

// plBlock is pl_block minus opt_block_label (the caller parsed the label and
// pushed the namespace); firstTok is the already-consumed K_DECLARE or
// K_BEGIN (or anything else, which is a syntax error exactly as bison finds
// when it can't shift into decl_sect/K_BEGIN).
func (c *compiler) plBlock(label string, firstTok plToken) *stmtBlock {
	var initVarnos []int

	tok := firstTok
	if tok == K_DECLARE {
		// decl_start action: forget variables created before the block and
		// disable identifier lookup during declarations.
		c.addInitdatums(false)
		c.scan.identifierLookup = lookupDeclare

		declared := false
	decls:
		for {
			tok = c.lex()
			switch {
			case tok == K_DECLARE:
				// We allow useless extra DECLAREs
			case tok == LESS_LESS:
				c.ereportGram("block label must be placed before DECLARE, not after")
			case tok == T_WORD || tokenIsUnreservedKeyword(tok):
				c.declStatement(tok)
				declared = true
			default:
				break decls
			}
		}
		if declared {
			initVarnos = c.addInitdatums(true)
		}
	}

	// decl_sect action: done with decls, so resume identifier lookup.
	c.scan.identifierLookup = lookupNormal

	if tok != K_BEGIN {
		c.scan.yyerror("syntax error")
	}
	beginLoc := c.loc()

	body := c.procSect()
	exceptions := c.exceptionSect()
	c.expect(K_END)
	endLabel := c.optLabel()

	blk := &stmtBlock{
		label:      label,
		initVarnos: initVarnos,
		body:       body,
		exceptions: exceptions,
	}
	blk.lineno = c.scan.locationToLineno(beginLoc)

	checkLabels(label, endLabel)
	c.nsPop()

	return blk
}

// declStatement is decl_statement; firstTok is the decl_varname's token.
func (c *compiler) declStatement(firstTok plToken) {
	name, lineno := c.declVarname(firstTok)

	tok := c.lex()
	switch tok {
	case K_ALIAS:
		// decl_varname K_ALIAS K_FOR decl_aliasitem ';'
		c.expect(K_FOR)
		nsi := c.declAliasitem()
		c.expect(plToken(';'))
		c.nsAdditem(nsi.itemtype, nsi.itemno, name)
		return

	case K_NO:
		c.expect(K_SCROLL)
		c.expect(K_CURSOR)
		c.declCursor(name, lineno, cursorOptNoScroll)
		return
	case K_SCROLL:
		c.expect(K_CURSOR)
		c.declCursor(name, lineno, cursorOptScroll)
		return
	case K_CURSOR:
		c.declCursor(name, lineno, 0)
		return
	}

	// Ordinary variable declaration.
	isConst := false
	if tok == K_CONSTANT {
		isConst = true
	} else {
		c.scan.pushBackToken(tok)
	}

	dtype := c.readDatatype(0, false)

	// decl_collate (get_collation_oid is mocked to the default collation).
	collation := uint32(0)
	tok = c.lex()
	if tok == K_COLLATE {
		tok = c.lex()
		if tok == T_WORD || tokenIsUnreservedKeyword(tok) || tok == T_CWORD {
			collation = oidDefaultCollation
		} else {
			c.scan.yyerror("syntax error")
		}
	} else {
		c.scan.pushBackToken(tok)
	}

	// decl_notnull.
	notnull := false
	tok = c.lex()
	if tok == K_NOT {
		c.expect(K_NULL)
		notnull = true
	} else {
		c.scan.pushBackToken(tok)
	}

	// decl_defval.
	var defval *plExpr
	tok = c.lex()
	switch tok {
	case plToken(';'):
	case plToken('='), tokCOLON_EQUALS, K_DEFAULT:
		defval = c.readSQLExpression(plToken(';'), ";")
	default:
		c.scan.yyerror("syntax error")
	}

	// decl_statement action.
	if collation != 0 {
		if dtype.collation == 0 {
			// format_type_be is mocked to "-".
			c.ereportGram("collations are not supported by type -")
		}
		dtype.collation = collation
	}

	v := c.buildVariable(name, lineno, dtype, true)
	if vr, ok := v.(*plVar); ok {
		vr.isconst = isConst
		vr.notnull = notnull
		vr.defaultVal = defval
		if vr.notnull && vr.defaultVal == nil {
			c.ereportGram(`variable "%s" must have a default value, since it's declared NOT NULL`, vr.refname)
		}
	} else if rec, ok := v.(*plRec); ok {
		// PLpgSQL_variable header fields: isconst/notnull/default_val live
		// on the C supertype; check_assignable reads isconst.
		rec.isconst = isConst
		if notnull && defval == nil {
			c.ereportGram(`variable "%s" must have a default value, since it's declared NOT NULL`, rec.refname)
		}
	}
}

// declVarname is decl_varname.
func (c *compiler) declVarname(tok plToken) (string, int) {
	var name string
	switch {
	case tok == T_WORD:
		name = c.cur().word.ident
	case tokenIsUnreservedKeyword(tok):
		name = c.cur().keyword
	default:
		c.scan.yyerror("syntax error")
	}
	lineno := c.scan.locationToLineno(c.loc())
	// Check to make sure name isn't already declared in the current block.
	if ns, _ := c.nsLookup(c.nsTop, true, name, "", ""); ns != nil {
		c.scan.yyerror("duplicate declaration")
	}
	// XCHECK_SHADOWVAR warnings are disabled (extra_warnings/errors are 0).
	return name, lineno
}

// declAliasitem is decl_aliasitem.
func (c *compiler) declAliasitem() *nsItem {
	tok := c.lex()
	switch {
	case tok == T_WORD:
		ident := c.cur().word.ident
		nsi, _ := c.nsLookup(c.nsTop, false, ident, "", "")
		if nsi == nil {
			c.ereportGram(`variable "%s" does not exist`, ident)
		}
		return nsi
	case tokenIsUnreservedKeyword(tok):
		ident := c.cur().keyword
		nsi, _ := c.nsLookup(c.nsTop, false, ident, "", "")
		if nsi == nil {
			c.ereportGram(`variable "%s" does not exist`, ident)
		}
		return nsi
	case tok == T_CWORD:
		idents := c.cur().cword
		var nsi *nsItem
		if len(idents) == 2 {
			nsi, _ = c.nsLookup(c.nsTop, false, idents[0], idents[1], "")
		} else if len(idents) == 3 {
			nsi, _ = c.nsLookup(c.nsTop, false, idents[0], idents[1], idents[2])
		}
		if nsi == nil {
			c.ereportGram(`variable "%s" does not exist`, nameListToString(idents))
		}
		return nsi
	default:
		c.scan.yyerror("syntax error")
		return nil
	}
}

// declCursor is the cursor arm of decl_statement (after K_CURSOR).
func (c *compiler) declCursor(name string, lineno int, scrollOption int) {
	// Mid-rule action: push a private namespace level for the cursor args.
	c.nsPush(name, labelOther)

	// decl_cursor_args.
	var argrow *plRow
	tok := c.lex()
	if tok == plToken('(') {
		openLoc := c.loc()
		var args []plVariable
		for {
			args = append(args, c.declCursorArg())
			tok = c.lex()
			if tok != plToken(',') {
				break
			}
		}
		if tok != plToken(')') {
			c.scan.yyerror("syntax error")
		}
		row := &plRow{
			refname: "(unnamed row)",
			lineno:  c.scan.locationToLineno(openLoc),
		}
		for _, arg := range args {
			row.fieldnames = append(row.fieldnames, arg.varRefname())
			row.varnos = append(row.varnos, arg.datumNo())
		}
		c.addDatum(row)
		argrow = row
	} else {
		c.scan.pushBackToken(tok)
	}

	// decl_is_for.
	tok = c.lex()
	if tok != K_IS && tok != K_FOR {
		c.scan.yyerror("syntax error")
	}

	// decl_cursor_query.
	query := c.readSQLStmt()

	// decl_statement's cursor action: pop the arg namespace, then build.
	c.nsPop()

	nv := c.buildVariable(name, lineno,
		c.plpgsqlBuildDatatype(oidRefcursor, -1, 0, nil), true).(*plVar)
	nv.cursorExplicitExpr = query
	if argrow == nil {
		nv.cursorExplicitArgrow = -1
	} else {
		nv.cursorExplicitArgrow = argrow.dno
	}
	nv.cursorOptions = cursorOptFastPlan | scrollOption
}

// decl_cursor_arg: decl_varname decl_datatype.
func (c *compiler) declCursorArg() plVariable {
	name, lineno := c.declVarname(c.lex())
	dtype := c.readDatatype(0, false)
	return c.buildVariable(name, lineno, dtype, true)
}

// optLabel is opt_label.
func (c *compiler) optLabel() string {
	tok := c.lex()
	if tok == T_WORD || tok == T_DATUM || tokenIsUnreservedKeyword(tok) {
		return c.anyIdentifier(tok)
	}
	c.scan.pushBackToken(tok)
	return ""
}

// anyIdentifier is any_identifier; tok is the already-consumed token.
func (c *compiler) anyIdentifier(tok plToken) string {
	switch {
	case tok == T_WORD:
		return c.cur().word.ident
	case tokenIsUnreservedKeyword(tok):
		return c.cur().keyword
	case tok == T_DATUM:
		if c.cur().wdatum.ident == "" {
			c.scan.yyerror("syntax error") // composite name not OK
		}
		return c.cur().wdatum.ident
	default:
		c.scan.yyerror("syntax error")
		return ""
	}
}

// procSect is proc_sect: statements until a token that cannot start one.
func (c *compiler) procSect() []plStmt {
	var stmts []plStmt
	for {
		tok := c.lex()
		if !isStmtStart(tok) {
			c.scan.pushBackToken(tok)
			return stmts
		}
		st := c.procStmt(tok)
		if st != nil {
			stmts = append(stmts, st)
		}
	}
}

// isStmtStart is FIRST(proc_stmt).
func isStmtStart(tok plToken) bool {
	switch tok {
	case LESS_LESS, K_DECLARE, K_BEGIN, T_DATUM, K_IF, K_CASE, K_LOOP,
		K_WHILE, K_FOR, K_FOREACH, K_EXIT, K_CONTINUE, K_RETURN, K_RAISE,
		K_ASSERT, K_IMPORT, K_INSERT, K_MERGE, T_WORD, T_CWORD, K_EXECUTE,
		K_PERFORM, K_CALL, K_DO, K_GET, K_OPEN, K_FETCH, K_MOVE, K_CLOSE,
		K_NULL, K_COMMIT, K_ROLLBACK:
		return true
	}
	return false
}

// procStmt is proc_stmt; tok is the statement's first token, consumed.
func (c *compiler) procStmt(tok plToken) plStmt {
	loc := c.loc()
	switch tok {
	case LESS_LESS:
		label := c.anyIdentifier(c.lex())
		c.expect(GREATER_GREATER)
		tok = c.lex()
		switch tok {
		case K_DECLARE, K_BEGIN:
			c.nsPush(label, labelBlock)
			blk := c.plBlock(label, tok)
			c.expect(plToken(';'))
			return blk
		case K_LOOP:
			c.nsPush(label, labelLoop)
			return c.stmtLoop(label)
		case K_WHILE:
			c.nsPush(label, labelLoop)
			return c.stmtWhile(label)
		case K_FOR:
			c.nsPush(label, labelLoop)
			return c.stmtFor(label)
		case K_FOREACH:
			c.nsPush(label, labelLoop)
			return c.stmtForeachA(label)
		default:
			c.scan.yyerror("syntax error")
			return nil
		}

	case K_DECLARE, K_BEGIN:
		c.nsPush("", labelBlock)
		blk := c.plBlock("", tok)
		c.expect(plToken(';'))
		return blk

	case K_LOOP:
		c.nsPush("", labelLoop)
		return c.stmtLoop("")
	case K_WHILE:
		c.nsPush("", labelLoop)
		return c.stmtWhile("")
	case K_FOR:
		c.nsPush("", labelLoop)
		return c.stmtFor("")
	case K_FOREACH:
		c.nsPush("", labelLoop)
		return c.stmtForeachA("")

	case T_DATUM:
		return c.stmtAssign()

	case K_IF:
		return c.stmtIf(loc)
	case K_CASE:
		return c.stmtCase(loc)
	case K_EXIT:
		return c.stmtExit(true, loc)
	case K_CONTINUE:
		return c.stmtExit(false, loc)
	case K_RETURN:
		return c.stmtReturn(loc)
	case K_RAISE:
		return c.stmtRaise(loc)
	case K_ASSERT:
		return c.stmtAssert(loc)

	case K_IMPORT, K_INSERT, K_MERGE:
		return c.makeExecsqlStmt(tok, loc, nil)
	case T_WORD:
		word := c.cur().word
		next := c.lex()
		c.scan.pushBackToken(next)
		if next == plToken('=') || next == tokCOLON_EQUALS ||
			next == plToken('[') || next == plToken('.') {
			wordIsNotVariable(&word)
		}
		return c.makeExecsqlStmt(T_WORD, loc, &word)
	case T_CWORD:
		cword := c.cur().cword
		next := c.lex()
		c.scan.pushBackToken(next)
		if next == plToken('=') || next == tokCOLON_EQUALS ||
			next == plToken('[') || next == plToken('.') {
			cwordIsNotVariable(cword)
		}
		return c.makeExecsqlStmt(T_CWORD, loc, nil)

	case K_EXECUTE:
		return c.stmtDynexecute(loc)
	case K_PERFORM:
		return c.stmtPerform(loc)
	case K_CALL:
		return c.stmtCallOrDo(loc, true)
	case K_DO:
		return c.stmtCallOrDo(loc, false)
	case K_GET:
		return c.stmtGetdiag(loc)
	case K_OPEN:
		return c.stmtOpen(loc)
	case K_FETCH:
		return c.stmtFetchOrMove(loc, false)
	case K_MOVE:
		return c.stmtFetchOrMove(loc, true)
	case K_CLOSE:
		return c.stmtClose(loc)
	case K_NULL:
		// stmt_null: K_NULL ';' — we do not bother building a node for NULL.
		c.expect(plToken(';'))
		return nil
	case K_COMMIT:
		return c.stmtCommit(loc)
	case K_ROLLBACK:
		return c.stmtRollback(loc)
	}
	c.scan.yyerror("syntax error")
	return nil
}

// stmt_perform.
func (c *compiler) stmtPerform(loc int) plStmt {
	st := &stmtPerform{}
	st.lineno = c.scan.locationToLineno(loc)
	c.scan.pushBackToken(K_PERFORM)

	// Substitute "SELECT" for "PERFORM" in the parsed text; syntax checking
	// happens after the substitution.
	var startloc int
	expr, _ := c.readSQLConstruct(plToken(';'), 0, 0, ";",
		rawParseDefault, false, false, &startloc)
	// overwrite "perform" (and the space the C memmove drops)
	expr.query = "SELECT" + expr.query[7:]
	c.checkSQLExpr(expr.query, expr.parseMode, startloc+1)
	st.expr = expr
	return st
}

// stmt_call: K_CALL / K_DO.
func (c *compiler) stmtCallOrDo(loc int, isCall bool) plStmt {
	st := &stmtCall{isCall: isCall}
	st.lineno = c.scan.locationToLineno(loc)
	if isCall {
		c.scan.pushBackToken(K_CALL)
	} else {
		c.scan.pushBackToken(K_DO)
	}
	st.expr = c.readSQLStmt()
	return st
}

// stmt_assign (current token is T_DATUM).
func (c *compiler) stmtAssign() plStmt {
	loc := c.loc()
	w := c.cur().wdatum

	// see how many names identify the datum
	nnames := 1
	if w.ident == "" {
		nnames = len(w.idents)
	}
	var pmode rawParseMode
	switch nnames {
	case 1:
		pmode = rawParsePlpgsqlAssign1
	case 2:
		pmode = rawParsePlpgsqlAssign2
	case 3:
		pmode = rawParsePlpgsqlAssign3
	}

	c.checkAssignable(w.datum)
	st := &stmtAssign{varno: w.datum.datumNo()}
	st.lineno = c.scan.locationToLineno(loc)
	// Push back the head name to include it in the stmt
	c.scan.pushBackToken(T_DATUM)
	st.expr, _ = c.readSQLConstruct(plToken(';'), 0, 0, ";", pmode, false, true, nil)
	return st
}

// stmt_getdiag.
func (c *compiler) stmtGetdiag(loc int) plStmt {
	st := &stmtGetdiag{}

	// getdiag_area_opt.
	tok := c.lex()
	switch tok {
	case K_CURRENT:
	case K_STACKED:
		st.isStacked = true
	default:
		c.scan.pushBackToken(tok)
	}
	c.expect(K_DIAGNOSTICS)

	// getdiag_list.
	for {
		item := &diagItem{}
		item.target = c.getdiagTarget()
		// assign_operator.
		tok = c.lex()
		if tok != plToken('=') && tok != tokCOLON_EQUALS {
			c.scan.yyerror("syntax error")
		}
		item.kind = c.getdiagItem()
		st.diagItems = append(st.diagItems, item)
		tok = c.lex()
		if tok != plToken(',') {
			break
		}
	}
	if tok != plToken(';') {
		c.scan.yyerror("syntax error")
	}

	st.lineno = c.scan.locationToLineno(loc)

	// Check information items are valid for area option.
	for _, ditem := range st.diagItems {
		switch ditem.kind {
		case getdiagRowCount, getdiagRoutineOid:
			if st.isStacked {
				c.ereportGram("diagnostics item %s is not allowed in GET STACKED DIAGNOSTICS",
					getdiagKindname(ditem.kind))
			}
		case getdiagErrorContext, getdiagErrorDetail, getdiagErrorHint,
			getdiagReturnedSqlstate, getdiagColumnName, getdiagConstraintName,
			getdiagDatatypeName, getdiagMessageText, getdiagTableName,
			getdiagSchemaName:
			if !st.isStacked {
				c.ereportGram("diagnostics item %s is not allowed in GET CURRENT DIAGNOSTICS",
					getdiagKindname(ditem.kind))
			}
		case getdiagContext:
		}
	}

	return st
}

// getdiag_item.
func (c *compiler) getdiagItem() getdiagKind {
	tok := c.lex()
	switch {
	case c.tokIsKeyword(tok, K_ROW_COUNT, "row_count"):
		return getdiagRowCount
	case c.tokIsKeyword(tok, K_PG_ROUTINE_OID, "pg_routine_oid"):
		return getdiagRoutineOid
	case c.tokIsKeyword(tok, K_PG_CONTEXT, "pg_context"):
		return getdiagContext
	case c.tokIsKeyword(tok, K_PG_EXCEPTION_DETAIL, "pg_exception_detail"):
		return getdiagErrorDetail
	case c.tokIsKeyword(tok, K_PG_EXCEPTION_HINT, "pg_exception_hint"):
		return getdiagErrorHint
	case c.tokIsKeyword(tok, K_PG_EXCEPTION_CONTEXT, "pg_exception_context"):
		return getdiagErrorContext
	case c.tokIsKeyword(tok, K_COLUMN_NAME, "column_name"):
		return getdiagColumnName
	case c.tokIsKeyword(tok, K_CONSTRAINT_NAME, "constraint_name"):
		return getdiagConstraintName
	case c.tokIsKeyword(tok, K_PG_DATATYPE_NAME, "pg_datatype_name"):
		return getdiagDatatypeName
	case c.tokIsKeyword(tok, K_MESSAGE_TEXT, "message_text"):
		return getdiagMessageText
	case c.tokIsKeyword(tok, K_TABLE_NAME, "table_name"):
		return getdiagTableName
	case c.tokIsKeyword(tok, K_SCHEMA_NAME, "schema_name"):
		return getdiagSchemaName
	case c.tokIsKeyword(tok, K_RETURNED_SQLSTATE, "returned_sqlstate"):
		return getdiagReturnedSqlstate
	default:
		c.scan.yyerror("unrecognized GET DIAGNOSTICS item")
		return 0
	}
}

// getdiag_target.
func (c *compiler) getdiagTarget() int {
	tok := c.lex()
	switch tok {
	case T_DATUM:
		w := c.cur().wdatum
		if w.datum.datumType() == dtypeRow || w.datum.datumType() == dtypeRec ||
			c.scan.peek() == plToken('[') {
			c.ereportGram(`"%s" is not a scalar variable`, nameOfDatum(&w))
		}
		c.checkAssignable(w.datum)
		return w.datum.datumNo()
	case T_WORD:
		wordIsNotVariable(&c.cur().word)
	case T_CWORD:
		cwordIsNotVariable(c.cur().cword)
	default:
		c.scan.yyerror("syntax error")
	}
	return 0
}

// stmt_if.
func (c *compiler) stmtIf(loc int) plStmt {
	st := &stmtIf{}
	st.cond = c.readSQLExpression(K_THEN, "THEN")
	st.thenBody = c.procSect()

	// stmt_elsifs (lineno computed at the C reduce point: after the body).
	tok := c.lex()
	for tok == K_ELSIF {
		elsifLoc := c.loc()
		e := &ifElsif{}
		e.cond = c.readSQLExpression(K_THEN, "THEN")
		e.stmts = c.procSect()
		e.lineno = c.scan.locationToLineno(elsifLoc)
		st.elsifList = append(st.elsifList, e)
		tok = c.lex()
	}

	// stmt_else.
	if tok == K_ELSE {
		st.elseBody = c.procSect()
		tok = c.lex()
	}
	if tok != K_END {
		c.scan.yyerror("syntax error")
	}
	c.expect(K_IF)
	c.expect(plToken(';'))

	st.lineno = c.scan.locationToLineno(loc)
	return st
}

// stmt_case.
func (c *compiler) stmtCase(loc int) plStmt {
	// opt_expr_until_when.
	var tExpr *plExpr
	tok := c.lex()
	if tok != K_WHEN {
		c.scan.pushBackToken(tok)
		tExpr = c.readSQLExpression(K_WHEN, "WHEN")
	}

	// case_when_list (lineno at the C reduce point: after the body).
	var whens []*caseWhen
	for {
		whenLoc := c.loc() // location of K_WHEN
		w := &caseWhen{}
		w.expr = c.readSQLExpression(K_THEN, "THEN")
		w.stmts = c.procSect()
		w.lineno = c.scan.locationToLineno(whenLoc)
		whens = append(whens, w)
		tok = c.lex()
		if tok != K_WHEN {
			break
		}
	}

	// opt_case_else.
	var elseStmts []plStmt
	if tok == K_ELSE {
		elseStmts = c.procSect()
		if elseStmts == nil {
			// distinguish empty ELSE from no ELSE: list with one NULL
			elseStmts = []plStmt{nil}
		}
		tok = c.lex()
	}
	if tok != K_END {
		c.scan.yyerror("syntax error")
	}
	c.expect(K_CASE)
	c.expect(plToken(';'))

	return c.makeCase(loc, tExpr, whens, elseStmts)
}

// loop_body: proc_sect K_END K_LOOP opt_label ';'.
func (c *compiler) loopBody() ([]plStmt, string) {
	stmts := c.procSect()
	c.expect(K_END)
	c.expect(K_LOOP)
	endLabel := c.optLabel()
	c.expect(plToken(';'))
	return stmts, endLabel
}

// stmt_loop (K_LOOP consumed; its location is current).
func (c *compiler) stmtLoop(label string) plStmt {
	loopLoc := c.loc()
	body, endLabel := c.loopBody()

	st := &stmtLoop{label: label, body: body}
	st.lineno = c.scan.locationToLineno(loopLoc)
	checkLabels(label, endLabel)
	c.nsPop()
	return st
}

// stmt_while.
func (c *compiler) stmtWhile(label string) plStmt {
	whileLoc := c.loc()
	cond := c.readSQLExpression(K_LOOP, "LOOP")
	body, endLabel := c.loopBody()

	st := &stmtWhile{label: label, cond: cond, body: body}
	st.lineno = c.scan.locationToLineno(whileLoc)
	checkLabels(label, endLabel)
	c.nsPop()
	return st
}

// stmt_for: opt_loop_label K_FOR for_control loop_body.
func (c *compiler) stmtFor(label string) plStmt {
	forLoc := c.loc()
	ctrl := c.forControl()
	body, endLabel := c.loopBody()

	lineno := c.scan.locationToLineno(forLoc)
	switch st := ctrl.(type) {
	case *stmtFori:
		st.lineno = lineno
		st.label = label
		st.body = body
	case *stmtFors:
		st.lineno = lineno
		st.label = label
		st.body = body
	case *stmtForc:
		st.lineno = lineno
		st.label = label
		st.body = body
	case *stmtDynfors:
		st.lineno = lineno
		st.label = label
		st.body = body
	}

	checkLabels(label, endLabel)
	c.nsPop()
	return ctrl
}

// forVariable is for_variable.
type forVariableResult struct {
	name   string
	lineno int
	scalar datum
	row    datum
}

func (c *compiler) forVariable() forVariableResult {
	var fv forVariableResult
	tok := c.lex()
	switch tok {
	case T_DATUM:
		w := c.cur().wdatum
		loc := c.loc()
		fv.name = nameOfDatum(&w)
		fv.lineno = c.scan.locationToLineno(loc)
		if w.datum.datumType() == dtypeRow || w.datum.datumType() == dtypeRec {
			fv.row = w.datum
		} else {
			fv.scalar = w.datum
			// check for comma-separated list
			tok = c.lex()
			c.scan.pushBackToken(tok)
			if tok == plToken(',') {
				fv.row = c.readIntoScalarList(fv.name, fv.scalar, loc)
			}
		}
	case T_WORD:
		word := c.cur().word
		fv.name = word.ident
		fv.lineno = c.scan.locationToLineno(c.loc())
		// check for comma-separated list
		tok = c.lex()
		c.scan.pushBackToken(tok)
		if tok == plToken(',') {
			wordIsNotVariable(&word)
		}
	case T_CWORD:
		cwordIsNotVariable(c.cur().cword)
	default:
		c.scan.yyerror("syntax error")
	}
	return fv
}

// forControl is for_control.
func (c *compiler) forControl() plStmt {
	fv := c.forVariable()
	c.expect(K_IN)

	tok := c.lex()
	if tok == K_EXECUTE {
		// EXECUTE means it's a dynamic FOR loop.
		expr, term := c.readSQLExpression2(K_LOOP, K_USING, "LOOP or USING")

		st := &stmtDynfors{}
		if fv.row != nil {
			st.varDat = fv.row.(plVariable)
			c.checkAssignable(fv.row)
		} else if fv.scalar != nil {
			st.varDat = c.makeScalarList1(fv.name, fv.scalar, fv.lineno)
		} else {
			c.ereportGram("loop variable of loop over rows must be a record variable or list of scalar variables")
		}
		st.query = expr

		if term == K_USING {
			for {
				expr, term = c.readSQLExpression2(plToken(','), K_LOOP, ", or LOOP")
				st.params = append(st.params, expr)
				if term != plToken(',') {
					break
				}
			}
		}
		return st
	}

	if tok == T_DATUM && c.cur().wdatum.datum.datumType() == dtypeVar &&
		c.cur().wdatum.datum.(*plVar).datatype.typoid == oidRefcursor {
		// It's FOR var IN cursor.
		cursor := c.cur().wdatum.datum.(*plVar)
		st := &stmtForc{curvar: cursor.dno}

		// Should have had a single variable name.
		if fv.scalar != nil && fv.row != nil {
			c.ereportGram("cursor FOR loop must have only one target variable")
		}

		// can't use an unbound cursor this way
		if cursor.cursorExplicitExpr == nil {
			c.ereportGram("cursor FOR loop must use a bound cursor variable")
		}

		// collect cursor's parameters if any
		st.argquery = c.readCursorArgs(cursor, K_LOOP)

		// create loop's private RECORD variable
		st.varDat = c.buildRecord(fv.name, fv.lineno, nil, oidRecord, true)
		return st
	}

	// FOR var IN a..b or FOR var IN query.
	reverse := false
	if c.tokIsKeyword(tok, K_REVERSE, "reverse") {
		reverse = true
	} else {
		c.scan.pushBackToken(tok)
	}

	var expr1loc int
	expr1, term := c.readSQLConstruct(tokDOT_DOT, K_LOOP, 0, "LOOP",
		rawParseDefault, true, false, &expr1loc)

	if term == tokDOT_DOT {
		// Saw "..", so it must be an integer loop.

		// Relabel first expression as an expression; then check its syntax.
		expr1.parseMode = rawParsePlpgsqlExpr
		c.checkSQLExpr(expr1.query, expr1.parseMode, expr1loc)

		// Read and check the second one
		expr2, term2 := c.readSQLExpression2(K_LOOP, K_BY, "LOOP")

		// Get the BY clause if any
		var exprBy *plExpr
		if term2 == K_BY {
			exprBy = c.readSQLExpression(K_LOOP, "LOOP")
		}

		// Should have had a single variable name
		if fv.scalar != nil && fv.row != nil {
			c.ereportGram("integer FOR loop must have only one target variable")
		}

		// create loop's private variable
		fvar := c.buildVariable(fv.name, fv.lineno,
			c.plpgsqlBuildDatatype(oidInt4, -1, 0, nil), true).(*plVar)

		st := &stmtFori{
			varDat:  fvar,
			reverse: reverse,
			lower:   expr1,
			upper:   expr2,
			step:    exprBy,
		}
		return st
	}

	// No "..", so it must be a query loop.
	if reverse {
		c.ereportGram("cannot specify REVERSE in query FOR loop")
	}

	// Check syntax as a regular query
	c.checkSQLExpr(expr1.query, expr1.parseMode, expr1loc)

	st := &stmtFors{}
	if fv.row != nil {
		st.varDat = fv.row.(plVariable)
		c.checkAssignable(fv.row)
	} else if fv.scalar != nil {
		st.varDat = c.makeScalarList1(fv.name, fv.scalar, fv.lineno)
	} else {
		c.ereportGram("loop variable of loop over rows must be a record variable or list of scalar variables")
	}
	st.query = expr1
	return st
}

// stmt_foreach_a.
func (c *compiler) stmtForeachA(label string) plStmt {
	foreachLoc := c.loc()
	fv := c.forVariable()

	// foreach_slice.
	slice := 0
	tok := c.lex()
	if tok == K_SLICE {
		c.expect(tokICONST)
		slice = int(c.cur().ival)
	} else {
		c.scan.pushBackToken(tok)
	}

	c.expect(K_IN)
	c.expect(K_ARRAY)
	expr := c.readSQLExpression(K_LOOP, "LOOP")
	body, endLabel := c.loopBody()

	st := &stmtForeachA{
		label: label,
		slice: slice,
		expr:  expr,
		body:  body,
	}
	st.lineno = c.scan.locationToLineno(foreachLoc)

	if fv.row != nil {
		st.varno = fv.row.datumNo()
		c.checkAssignable(fv.row)
	} else if fv.scalar != nil {
		st.varno = fv.scalar.datumNo()
		c.checkAssignable(fv.scalar)
	} else {
		c.ereportGram("loop variable of FOREACH must be a known variable or list of variables")
	}

	checkLabels(label, endLabel)
	c.nsPop()
	return st
}

// stmt_exit (exit_type consumed; loc is its location).
func (c *compiler) stmtExit(isExit bool, loc int) plStmt {
	st := &stmtExit{isExit: isExit}
	st.label = c.optLabel()

	// opt_exitcond.
	tok := c.lex()
	switch tok {
	case plToken(';'):
	case K_WHEN:
		st.cond = c.readSQLExpression(plToken(';'), ";")
	default:
		c.scan.yyerror("syntax error")
	}

	st.lineno = c.scan.locationToLineno(loc)

	if st.label != "" {
		// We have a label, so verify it exists
		labelItem := c.nsLookupLabel(c.nsTop, st.label)
		if labelItem == nil {
			c.ereportGram(`there is no label "%s" attached to any block or loop enclosing this statement`, st.label)
		}
		// CONTINUE only allows loop labels
		if labelItem.itemno != int(labelLoop) && !st.isExit {
			c.ereportGram(`block label "%s" cannot be used in CONTINUE`, st.label)
		}
	} else {
		// No label, so make sure there is some loop
		if c.nsFindNearestLoop(c.nsTop) == nil {
			if st.isExit {
				c.ereportGram("EXIT cannot be used outside a loop, unless it has a label")
			} else {
				c.ereportGram("CONTINUE cannot be used outside a loop")
			}
		}
	}

	return st
}

// stmt_return.
func (c *compiler) stmtReturn(loc int) plStmt {
	tok := c.lex()
	if tok == 0 {
		c.scan.yyerror("unexpected end of function definition")
	}
	if c.tokIsKeyword(tok, K_NEXT, "next") {
		return c.makeReturnNextStmt(loc)
	}
	if c.tokIsKeyword(tok, K_QUERY, "query") {
		return c.makeReturnQueryStmt(loc)
	}
	c.scan.pushBackToken(tok)
	return c.makeReturnStmt(loc)
}

// stmt_raise.
func (c *compiler) stmtRaise(loc int) plStmt {
	st := &stmtRaise{elogLevel: elogError}
	st.lineno = c.scan.locationToLineno(loc)

	tok := c.lex()
	if tok == 0 {
		c.scan.yyerror("unexpected end of function definition")
	}

	// We could have just RAISE, meaning to re-throw the current error.
	if tok != plToken(';') {
		// First is an optional elog severity level.
		levelSet := true
		switch {
		case c.tokIsKeyword(tok, K_EXCEPTION, "exception"):
			st.elogLevel = elogError
		case c.tokIsKeyword(tok, K_WARNING, "warning"):
			st.elogLevel = elogWarning
		case c.tokIsKeyword(tok, K_NOTICE, "notice"):
			st.elogLevel = elogNotice
		case c.tokIsKeyword(tok, K_INFO, "info"):
			st.elogLevel = elogInfo
		case c.tokIsKeyword(tok, K_LOG, "log"):
			st.elogLevel = elogLog
		case c.tokIsKeyword(tok, K_DEBUG, "debug"):
			st.elogLevel = elogDebug1
		default:
			levelSet = false
		}
		if levelSet {
			tok = c.lex()
		}
		if tok == 0 {
			c.scan.yyerror("unexpected end of function definition")
		}

		// Next: condition name, SQLSTATE 'xxxxx', old-style message string,
		// or USING.
		if tok == tokSCONST {
			// old style message and parameters
			st.message = c.cur().str
			st.hasMessage = true
			tok = c.lex()
			if tok != plToken(',') && tok != plToken(';') && tok != K_USING {
				c.scan.yyerror("syntax error")
			}
			for tok == plToken(',') {
				var expr *plExpr
				expr, tok = c.readSQLConstruct2(plToken(','), plToken(';'), K_USING,
					", or ; or USING")
				st.params = append(st.params, expr)
			}
		} else if tok != K_USING {
			// must be condition name or SQLSTATE
			if c.tokIsKeyword(tok, K_SQLSTATE, "sqlstate") {
				// next token should be a string literal
				if c.lex() != tokSCONST {
					c.scan.yyerror("syntax error")
				}
				sqlstatestr := c.cur().str
				if len(sqlstatestr) != 5 {
					c.scan.yyerror("invalid SQLSTATE code")
				}
				if !isSqlstate(sqlstatestr) {
					c.scan.yyerror("invalid SQLSTATE code")
				}
				st.condname = sqlstatestr
			} else {
				if tok == T_WORD {
					st.condname = c.cur().word.ident
				} else if tokenIsUnreservedKeyword(tok) {
					st.condname = c.cur().keyword
				} else {
					c.scan.yyerror("syntax error")
				}
				recognizeErrCondition(st.condname, false)
			}
			tok = c.lex()
			if tok != plToken(';') && tok != K_USING {
				c.scan.yyerror("syntax error")
			}
		}

		if tok == K_USING {
			st.options = c.readRaiseOptions()
		}
	}

	checkRaiseParameters(st)
	return st
}

// readSQLConstruct2 is read_sql_construct with three terminators and the
// PLPGSQL_EXPR parse mode (the RAISE parameter shape).
func (c *compiler) readSQLConstruct2(until, until2, until3 plToken, expected string) (*plExpr, plToken) {
	return c.readSQLConstruct(until, until2, until3, expected,
		rawParsePlpgsqlExpr, true, true, nil)
}

// stmt_assert.
func (c *compiler) stmtAssert(loc int) plStmt {
	st := &stmtAssert{}
	st.lineno = c.scan.locationToLineno(loc)

	var tok plToken
	st.cond, tok = c.readSQLExpression2(plToken(','), plToken(';'), ", or ;")
	if tok == plToken(',') {
		st.message = c.readSQLExpression(plToken(';'), ";")
	}
	return st
}

// stmt_dynexecute.
func (c *compiler) stmtDynexecute(loc int) plStmt {
	expr, endtoken := c.readSQLConstruct(K_INTO, K_USING, plToken(';'),
		"INTO or USING or ;", rawParsePlpgsqlExpr, true, true, nil)

	st := &stmtDynexecute{query: expr}
	st.lineno = c.scan.locationToLineno(loc)

	for {
		if endtoken == K_INTO {
			if st.into { // multiple INTO
				c.scan.yyerror("syntax error")
			}
			st.into = true
			st.target, st.strict = c.readIntoTarget(true)
			endtoken = c.lex()
		} else if endtoken == K_USING {
			if len(st.params) > 0 { // multiple USING
				c.scan.yyerror("syntax error")
			}
			for {
				expr, endtoken = c.readSQLConstruct(plToken(','), plToken(';'), K_INTO,
					", or ; or INTO", rawParsePlpgsqlExpr, true, true, nil)
				st.params = append(st.params, expr)
				if endtoken != plToken(',') {
					break
				}
			}
		} else if endtoken == plToken(';') {
			break
		} else {
			c.scan.yyerror("syntax error")
		}
	}

	return st
}

// cursor_variable.
func (c *compiler) cursorVariable() *plVar {
	tok := c.lex()
	switch tok {
	case T_DATUM:
		w := c.cur().wdatum
		if w.datum.datumType() != dtypeVar || c.scan.peek() == plToken('[') {
			c.ereportGram("cursor variable must be a simple variable")
		}
		v := w.datum.(*plVar)
		if v.datatype.typoid != oidRefcursor {
			c.ereportGram(`variable "%s" must be of type cursor or refcursor`, v.refname)
		}
		return v
	case T_WORD:
		wordIsNotVariable(&c.cur().word)
	case T_CWORD:
		cwordIsNotVariable(c.cur().cword)
	default:
		c.scan.yyerror("syntax error")
	}
	return nil
}

// stmt_open.
func (c *compiler) stmtOpen(loc int) plStmt {
	cursor := c.cursorVariable()
	st := &stmtOpen{cursorOptions: cursorOptFastPlan}
	st.lineno = c.scan.locationToLineno(loc)
	st.curvar = cursor.dno

	if cursor.cursorExplicitExpr == nil {
		// be nice if we could use opt_scrollable here
		tok := c.lex()
		if c.tokIsKeyword(tok, K_NO, "no") {
			tok = c.lex()
			if c.tokIsKeyword(tok, K_SCROLL, "scroll") {
				st.cursorOptions |= cursorOptNoScroll
				tok = c.lex()
			}
		} else if c.tokIsKeyword(tok, K_SCROLL, "scroll") {
			st.cursorOptions |= cursorOptScroll
			tok = c.lex()
		}

		if tok != K_FOR {
			c.scan.yyerror(`syntax error, expected "FOR"`)
		}

		tok = c.lex()
		if tok == K_EXECUTE {
			var endtoken plToken
			st.dynquery, endtoken = c.readSQLExpression2(K_USING, plToken(';'), "USING or ;")

			// If we found "USING", collect argument(s)
			if endtoken == K_USING {
				for {
					var expr *plExpr
					expr, endtoken = c.readSQLExpression2(plToken(','), plToken(';'), ", or ;")
					st.params = append(st.params, expr)
					if endtoken != plToken(',') {
						break
					}
				}
			}
		} else {
			c.scan.pushBackToken(tok)
			st.query = c.readSQLStmt()
		}
	} else {
		// predefined cursor query, so read args
		st.argquery = c.readCursorArgs(cursor, plToken(';'))
	}

	return st
}

// stmt_fetch / stmt_move.
func (c *compiler) stmtFetchOrMove(loc int, isMove bool) plStmt {
	fetch := c.readFetchDirection()
	cursor := c.cursorVariable()

	if !isMove {
		c.expect(K_INTO)
		target, _ := c.readIntoTarget(false)
		if c.lex() != plToken(';') {
			c.scan.yyerror("syntax error")
		}
		// We don't allow multiple rows in PL/pgSQL's FETCH statement.
		if fetch.returnsMultipleRows {
			c.ereportGram("FETCH statement cannot return multiple rows")
		}
		fetch.target = target
	} else {
		c.expect(plToken(';'))
	}

	fetch.lineno = c.scan.locationToLineno(loc)
	fetch.curvar = cursor.dno
	fetch.isMove = isMove
	return fetch
}

// stmt_close.
func (c *compiler) stmtClose(loc int) plStmt {
	curvar := c.cursorVariable().dno
	c.expect(plToken(';'))
	st := &stmtClose{curvar: curvar}
	st.lineno = c.scan.locationToLineno(loc)
	return st
}

// stmt_commit / stmt_rollback with opt_transaction_chain.
func (c *compiler) stmtCommit(loc int) plStmt {
	chain := c.optTransactionChain()
	c.expect(plToken(';'))
	st := &stmtCommit{chain: chain}
	st.lineno = c.scan.locationToLineno(loc)
	return st
}

func (c *compiler) stmtRollback(loc int) plStmt {
	chain := c.optTransactionChain()
	c.expect(plToken(';'))
	st := &stmtRollback{chain: chain}
	st.lineno = c.scan.locationToLineno(loc)
	return st
}

func (c *compiler) optTransactionChain() bool {
	tok := c.lex()
	if tok != K_AND {
		c.scan.pushBackToken(tok)
		return false
	}
	tok = c.lex()
	if tok == K_CHAIN {
		return true
	}
	if tok == K_NO {
		c.expect(K_CHAIN)
		return false
	}
	c.scan.yyerror("syntax error")
	return false
}

// exceptionSect is exception_sect.
func (c *compiler) exceptionSect() *exceptionBlock {
	tok := c.lex()
	if tok != K_EXCEPTION {
		c.scan.pushBackToken(tok)
		return nil
	}

	// Mid-rule action: create the sqlstate/sqlerrm variables before parsing
	// the WHEN clauses; their scope extends to the end of the current block.
	lineno := c.scan.locationToLineno(c.loc())
	blk := &exceptionBlock{}

	v := c.buildVariable("sqlstate", lineno,
		c.plpgsqlBuildDatatype(oidText, -1, 0, nil), true).(*plVar)
	v.isconst = true
	blk.sqlstateVarno = v.dno

	v = c.buildVariable("sqlerrm", lineno,
		c.plpgsqlBuildDatatype(oidText, -1, 0, nil), true).(*plVar)
	v.isconst = true
	blk.sqlerrmVarno = v.dno

	// proc_exceptions (lineno at the C reduce point: after the action body).
	for {
		c.expect(K_WHEN)
		whenLoc := c.loc()
		exc := &plException{}
		exc.conditions = c.procConditions()
		c.expect(K_THEN)
		exc.action = c.procSect()
		exc.lineno = c.scan.locationToLineno(whenLoc)
		blk.excList = append(blk.excList, exc)

		tok = c.lex()
		c.scan.pushBackToken(tok)
		if tok != K_WHEN {
			break
		}
	}

	return blk
}

// procConditions is proc_conditions: OR-joined condition chains.
func (c *compiler) procConditions() *plCondition {
	head := c.procCondition()
	for {
		tok := c.lex()
		if tok != K_OR {
			c.scan.pushBackToken(tok)
			return head
		}
		next := c.procCondition()
		old := head
		for old.next != nil {
			old = old.next
		}
		old.next = next
	}
}

// procCondition is proc_condition.
func (c *compiler) procCondition() *plCondition {
	name := c.anyIdentifier(c.lex())
	if name != "sqlstate" {
		return parseErrCondition(name)
	}
	// next token should be a string literal
	if c.lex() != tokSCONST {
		c.scan.yyerror("syntax error")
	}
	sqlstatestr := c.cur().str
	if len(sqlstatestr) != 5 {
		c.scan.yyerror("invalid SQLSTATE code")
	}
	if !isSqlstate(sqlstatestr) {
		c.scan.yyerror("invalid SQLSTATE code")
	}
	return &plCondition{condname: sqlstatestr}
}
