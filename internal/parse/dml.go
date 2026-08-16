package parse

import (
	"math"

	"github.com/sqlc-dev/oliphant/ast"
)

// Milestone 5: the DML statement family — INSERT (ON CONFLICT, OVERRIDING),
// UPDATE, DELETE, MERGE, RETURNING, COPY, PREPARE/EXECUTE/DEALLOCATE, and
// cursors (DECLARE/FETCH/MOVE/CLOSE).

// parseWithPrefixedStmt handles the statements that begin with
// opt_with_clause: the WITH clause is parsed once, then the head token of
// what follows picks the statement, exactly as the LALR tables keep all five
// alternatives alive through the with_clause.
// gram.y: InsertStmt/UpdateStmt/DeleteStmt/MergeStmt: opt_with_clause ...;
// select_no_parens: with_clause select_clause ...
func (p *parser) parseWithPrefixedStmt() *ast.Node {
	with := p.parseWithClause()
	switch p.kind() {
	case ast.Token_INSERT:
		return p.parseInsertStmt(with)
	case ast.Token_UPDATE:
		return p.parseUpdateStmt(with)
	case ast.Token_DELETE_P:
		return p.parseDeleteStmt(with)
	case ast.Token_MERGE:
		return p.parseMergeStmt(with)
	}
	return p.parseSelectNoParensRest(with)
}

// parseInsertStmt is gram.y's InsertStmt:
// opt_with_clause INSERT INTO insert_target insert_rest
// opt_on_conflict returning_clause
func (p *parser) parseInsertStmt(with *ast.WithClause) *ast.Node {
	p.expect(ast.Token_INSERT)
	p.expect(ast.Token_INTO)
	rel := p.parseInsertTarget()
	n := p.parseInsertRest()
	n.Relation = rel
	n.OnConflictClause = p.parseOptOnConflict()
	n.ReturningList = p.parseReturningClause()
	n.WithClause = with
	return nInsertStmt(n)
}

// parseInsertTarget is gram.y's insert_target: qualified_name, with the
// alias requiring AS (VALUES would conflict with an optional alias).
func (p *parser) parseInsertTarget() *ast.RangeVar {
	rel := p.parseQualifiedName()
	if p.have(ast.Token_AS) {
		rel.Alias = makeAlias(p.colId(), nil)
	}
	return rel
}

// parseInsertRest is gram.y's insert_rest.
func (p *parser) parseInsertRest() *ast.InsertStmt {
	n := &ast.InsertStmt{Override: ast.OverridingKind_OVERRIDING_NOT_SET}
	switch {
	case p.kind() == ast.Token_DEFAULT:
		// DEFAULT VALUES
		p.next()
		p.expect(ast.Token_VALUES)
		return n
	case p.kind() == ast.Token_OVERRIDING:
		p.next()
		n.Override = p.parseOverrideKind()
		p.expect(ast.Token_VALUE_P)
		n.SelectStmt = p.parseSelectStmt()
		return n
	case p.kind() == ast.Token('(') && !p.looksLikeSelectAhead() &&
		p.kindN(1) != ast.Token('('):
		// '(' insert_column_list ')' [OVERRIDING override_kind VALUE_P] SelectStmt
		// A '(' that opens a select (or another '(', which only a select can
		// absorb) belongs to SelectStmt instead — the LALR tables keep both
		// paths alive through the '('.
		p.next()
		n.Cols = p.parseInsertColumnList()
		p.expect(ast.Token(')'))
		if p.kind() == ast.Token_OVERRIDING {
			p.next()
			n.Override = p.parseOverrideKind()
			p.expect(ast.Token_VALUE_P)
		}
		n.SelectStmt = p.parseSelectStmt()
		return n
	}
	n.SelectStmt = p.parseSelectStmt()
	return n
}

// parseOverrideKind is gram.y's override_kind (OVERRIDING consumed).
func (p *parser) parseOverrideKind() ast.OverridingKind {
	switch {
	case p.have(ast.Token_USER):
		return ast.OverridingKind_OVERRIDING_USER_VALUE
	case p.have(ast.Token_SYSTEM_P):
		return ast.OverridingKind_OVERRIDING_SYSTEM_VALUE
	}
	p.syntaxErrorAt()
	return ast.OverridingKind_OVERRIDING_NOT_SET
}

// parseInsertColumnList is gram.y's insert_column_list.
func (p *parser) parseInsertColumnList() []*ast.Node {
	list := []*ast.Node{p.parseInsertColumnItem()}
	for p.have(ast.Token(',')) {
		list = append(list, p.parseInsertColumnItem())
	}
	return list
}

// parseInsertColumnItem is gram.y's insert_column_item: ColId opt_indirection.
func (p *parser) parseInsertColumnItem() *ast.Node {
	tok := p.peek()
	name := p.colId()
	rt := &ast.ResTarget{Name: name, Location: tok.Start}
	if ind := p.parseOptIndirection(); ind != nil {
		rt.Indirection = p.check_indirection(ind)
	}
	return nResTarget(rt)
}

// parseOptOnConflict is gram.y's opt_on_conflict.
func (p *parser) parseOptOnConflict() *ast.OnConflictClause {
	if p.kind() != ast.Token_ON {
		return nil
	}
	tok := p.next()
	p.expect(ast.Token_CONFLICT)
	oc := &ast.OnConflictClause{Location: tok.Start}
	oc.Infer = p.parseOptConfExpr()
	p.expect(ast.Token_DO)
	switch {
	case p.have(ast.Token_UPDATE):
		p.expect(ast.Token_SET)
		oc.Action = ast.OnConflictAction_ONCONFLICT_UPDATE
		oc.TargetList = p.parseSetClauseList()
		if p.have(ast.Token_WHERE) {
			oc.WhereClause = p.parseAExpr(0)
		}
	case p.have(ast.Token_NOTHING):
		oc.Action = ast.OnConflictAction_ONCONFLICT_NOTHING
	default:
		p.syntaxErrorAt()
	}
	return oc
}

// parseOptConfExpr is gram.y's opt_conf_expr.
func (p *parser) parseOptConfExpr() *ast.InferClause {
	switch p.kind() {
	case ast.Token('('):
		tok := p.next()
		ic := &ast.InferClause{Location: tok.Start}
		ic.IndexElems = p.parseIndexParams()
		p.expect(ast.Token(')'))
		if p.have(ast.Token_WHERE) {
			ic.WhereClause = p.parseAExpr(0)
		}
		return ic
	case ast.Token_ON:
		tok := p.next()
		p.expect(ast.Token_CONSTRAINT)
		return &ast.InferClause{Conname: p.name(), Location: tok.Start}
	}
	return nil
}

// parseIndexParams is gram.y's index_params.
func (p *parser) parseIndexParams() []*ast.Node {
	list := []*ast.Node{p.parseIndexElem()}
	for p.have(ast.Token(',')) {
		list = append(list, p.parseIndexElem())
	}
	return list
}

// parseIndexElem is gram.y's index_elem: a bare column, a parenthesized
// expression, or (for backwards compatibility) an unparenthesized function
// call.
func (p *parser) parseIndexElem() *ast.Node {
	switch {
	case p.kind() == ast.Token('('):
		p.next()
		expr := p.parseAExpr(0)
		p.expect(ast.Token(')'))
		el := p.parseIndexElemOptions()
		el.Expr = expr
		return nIndexElem(el)
	case p.startsFunctionCallAhead():
		expr := p.parseFuncExprWindowless()
		el := p.parseIndexElemOptions()
		el.Expr = expr
		return nIndexElem(el)
	}
	name := p.colId()
	el := p.parseIndexElemOptions()
	el.Name = name
	return nIndexElem(el)
}

// parseIndexElemOptions is gram.y's index_elem_options: opt_collate, then
// either opt_qualified_name or any_name reloptions, then opt_asc_desc and
// opt_nulls_order.
func (p *parser) parseIndexElemOptions() *ast.IndexElem {
	el := &ast.IndexElem{
		Ordering:      ast.SortByDir_SORTBY_DEFAULT,
		NullsOrdering: ast.SortByNulls_SORTBY_NULLS_DEFAULT,
	}
	if p.have(ast.Token_COLLATE) {
		el.Collation = p.anyName()
	}
	if isColIdToken(p.peek()) {
		el.Opclass = p.anyName()
		if p.kind() == ast.Token('(') {
			el.Opclassopts = p.parseReloptions()
		}
	}
	switch p.kind() {
	case ast.Token_ASC:
		p.next()
		el.Ordering = ast.SortByDir_SORTBY_ASC
	case ast.Token_DESC:
		p.next()
		el.Ordering = ast.SortByDir_SORTBY_DESC
	}
	if p.kind() == ast.Token_NULLS_LA {
		p.next()
		if p.have(ast.Token_FIRST_P) {
			el.NullsOrdering = ast.SortByNulls_SORTBY_NULLS_FIRST
		} else {
			p.expect(ast.Token_LAST_P)
			el.NullsOrdering = ast.SortByNulls_SORTBY_NULLS_LAST
		}
	}
	return el
}

// parseReloptions is gram.y's reloptions: '(' reloption_list ')'.
func (p *parser) parseReloptions() []*ast.Node {
	p.expect(ast.Token('('))
	list := []*ast.Node{p.parseReloptionElem()}
	for p.have(ast.Token(',')) {
		list = append(list, p.parseReloptionElem())
	}
	p.expect(ast.Token(')'))
	return list
}

// parseReloptionElem is gram.y's reloption_elem.
func (p *parser) parseReloptionElem() *ast.Node {
	tok := p.peek()
	name := p.colLabel()
	if p.have(ast.Token('.')) {
		name2 := p.colLabel()
		var arg *ast.Node
		if p.have(ast.Token('=')) {
			arg = p.parseDefArg()
		}
		return makeDefElemExtended(name, name2, arg, ast.DefElemAction_DEFELEM_UNSPEC, tok.Start)
	}
	var arg *ast.Node
	if p.have(ast.Token('=')) {
		arg = p.parseDefArg()
	}
	return makeDefElem(name, arg, tok.Start)
}

// parseDefArg is gram.y's def_arg: func_type | reserved_keyword |
// qual_all_Op | NumericOnly | Sconst | NONE.
func (p *parser) parseDefArg() *ast.Node {
	tok := p.peek()
	switch tok.Kind {
	case ast.Token_SCONST:
		p.next()
		return nStr(tok.Str)
	case ast.Token_NONE:
		p.next()
		return nStr("none")
	case ast.Token_ICONST, ast.Token_FCONST:
		return p.parseNumericOnly()
	case ast.Token('+'), ast.Token('-'):
		// '+'/'-' start NumericOnly when a number follows, all_Op otherwise.
		if p.kindN(1) == ast.Token_ICONST || p.kindN(1) == ast.Token_FCONST {
			return p.parseNumericOnly()
		}
		return nList(p.parseQualAllOp())
	case ast.Token_Op, ast.Token('*'), ast.Token('/'), ast.Token('%'),
		ast.Token('^'), ast.Token('<'), ast.Token('>'), ast.Token('='),
		ast.Token_LESS_EQUALS, ast.Token_GREATER_EQUALS, ast.Token_NOT_EQUALS,
		ast.Token_OPERATOR:
		return nList(p.parseQualAllOp())
	}
	if tok.KeywordKind == ast.KeywordKind_RESERVED_KEYWORD {
		p.next()
		return nStr(tok.Str)
	}
	return nTypeName(p.parseTypename())
}

// parseNumericOnly is gram.y's NumericOnly.
func (p *parser) parseNumericOnly() *ast.Node {
	tok := p.peek()
	switch tok.Kind {
	case ast.Token_FCONST:
		p.next()
		return nFloat(tok.Str)
	case ast.Token('+'):
		p.next()
		if p.kind() == ast.Token_FCONST {
			return nFloat(p.next().Str)
		}
		return nInteger(p.iconst())
	case ast.Token('-'):
		p.next()
		if p.kind() == ast.Token_FCONST {
			f := &ast.Float{Fval: p.next().Str}
			doNegateFloat(f)
			return &ast.Node{Node: &ast.Node_Float{Float: f}}
		}
		return nInteger(-p.iconst())
	}
	return nInteger(p.iconst())
}

// parseSignedIconst is gram.y's SignedIconst.
func (p *parser) parseSignedIconst() int32 {
	switch {
	case p.have(ast.Token('+')):
		return p.iconst()
	case p.have(ast.Token('-')):
		return -p.iconst()
	}
	return p.iconst()
}

// parseReturningClause is gram.y's returning_clause.
func (p *parser) parseReturningClause() []*ast.Node {
	if !p.have(ast.Token_RETURNING) {
		return nil
	}
	return p.parseTargetList()
}

// parseDeleteStmt is gram.y's DeleteStmt.
func (p *parser) parseDeleteStmt(with *ast.WithClause) *ast.Node {
	p.expect(ast.Token_DELETE_P)
	p.expect(ast.Token_FROM)
	n := &ast.DeleteStmt{WithClause: with}
	n.Relation = p.parseRelationExprOptAlias()
	// using_clause
	if p.have(ast.Token_USING) {
		n.UsingClause = p.parseFromList()
	}
	n.WhereClause = p.parseWhereOrCurrentClause()
	n.ReturningList = p.parseReturningClause()
	return nDeleteStmt(n)
}

// parseUpdateStmt is gram.y's UpdateStmt.
func (p *parser) parseUpdateStmt(with *ast.WithClause) *ast.Node {
	p.expect(ast.Token_UPDATE)
	n := &ast.UpdateStmt{WithClause: with}
	n.Relation = p.parseRelationExprOptAlias()
	p.expect(ast.Token_SET)
	n.TargetList = p.parseSetClauseList()
	if p.have(ast.Token_FROM) {
		n.FromClause = p.parseFromList()
	}
	n.WhereClause = p.parseWhereOrCurrentClause()
	n.ReturningList = p.parseReturningClause()
	return nUpdateStmt(n)
}

// parseRelationExprOptAlias is gram.y's relation_expr_opt_alias. The bare
// (no AS) alias form excludes SET: the production outranks the SET token's
// precedence so that "UPDATE foo set ..." reads SET as the keyword.
func (p *parser) parseRelationExprOptAlias() *ast.RangeVar {
	rel := p.parseRelationExprVar()
	switch {
	case p.have(ast.Token_AS):
		rel.Alias = makeAlias(p.colId(), nil)
	case isColIdToken(p.peek()) && p.kind() != ast.Token_SET:
		rel.Alias = makeAlias(p.next().Str, nil)
	}
	return rel
}

// parseWhereOrCurrentClause is gram.y's where_or_current_clause. CURRENT_P
// resolves to the cursor form only when OF follows (one-token lookahead,
// as bison shifts OF where a ColumnRef reduction would be dead).
func (p *parser) parseWhereOrCurrentClause() *ast.Node {
	if !p.have(ast.Token_WHERE) {
		return nil
	}
	if p.kind() == ast.Token_CURRENT_P && p.kindN(1) == ast.Token_OF {
		p.next()
		p.next()
		return nCurrentOfExpr(&ast.CurrentOfExpr{CursorName: p.name()})
	}
	return p.parseAExpr(0)
}

// parseSetClauseList is gram.y's set_clause_list (list_concat semantics:
// each multi-assignment set_clause contributes all its targets).
func (p *parser) parseSetClauseList() []*ast.Node {
	list := p.parseSetClause()
	for p.have(ast.Token(',')) {
		list = append(list, p.parseSetClause()...)
	}
	return list
}

// parseSetClause is gram.y's set_clause.
func (p *parser) parseSetClause() []*ast.Node {
	if p.have(ast.Token('(')) {
		// '(' set_target_list ')' '=' a_expr
		targets := []*ast.Node{p.parseSetTarget()}
		for p.have(ast.Token(',')) {
			targets = append(targets, p.parseSetTarget())
		}
		p.expect(ast.Token(')'))
		p.expect(ast.Token('='))
		source := p.parseAExpr(0)
		ncolumns := int32(len(targets))
		for i, t := range targets {
			t.GetResTarget().Val = nMultiAssignRef(&ast.MultiAssignRef{
				Source:   source,
				Colno:    int32(i) + 1,
				Ncolumns: ncolumns,
			})
		}
		return targets
	}
	t := p.parseSetTarget()
	p.expect(ast.Token('='))
	t.GetResTarget().Val = p.parseAExpr(0)
	return []*ast.Node{t}
}

// parseSetTarget is gram.y's set_target: ColId opt_indirection.
func (p *parser) parseSetTarget() *ast.Node {
	tok := p.peek()
	name := p.colId()
	rt := &ast.ResTarget{Name: name, Location: tok.Start}
	if ind := p.parseOptIndirection(); ind != nil {
		rt.Indirection = p.check_indirection(ind)
	}
	return nResTarget(rt)
}

// parseMergeStmt is gram.y's MergeStmt.
func (p *parser) parseMergeStmt(with *ast.WithClause) *ast.Node {
	p.expect(ast.Token_MERGE)
	p.expect(ast.Token_INTO)
	m := &ast.MergeStmt{WithClause: with}
	m.Relation = p.parseRelationExprOptAlias()
	p.expect(ast.Token_USING)
	m.SourceRelation = p.parseTableRef()
	p.expect(ast.Token_ON)
	m.JoinCondition = p.parseAExpr(0)
	// merge_when_list
	m.MergeWhenClauses = []*ast.Node{p.parseMergeWhenClause()}
	for p.kind() == ast.Token_WHEN {
		m.MergeWhenClauses = append(m.MergeWhenClauses, p.parseMergeWhenClause())
	}
	m.ReturningList = p.parseReturningClause()
	return nMergeStmt(m)
}

// parseMergeWhenClause is gram.y's merge_when_clause, covering
// merge_when_tgt_matched / merge_when_tgt_not_matched /
// opt_merge_when_condition and the four action forms.
func (p *parser) parseMergeWhenClause() *ast.Node {
	p.expect(ast.Token_WHEN)
	matchKind := ast.MergeMatchKind_MERGE_WHEN_MATCHED
	matched := true
	if !p.have(ast.Token_MATCHED) {
		p.expect(ast.Token_NOT)
		p.expect(ast.Token_MATCHED)
		matchKind = ast.MergeMatchKind_MERGE_WHEN_NOT_MATCHED_BY_TARGET
		matched = false
		if p.have(ast.Token_BY) {
			switch {
			case p.have(ast.Token_SOURCE):
				matchKind = ast.MergeMatchKind_MERGE_WHEN_NOT_MATCHED_BY_SOURCE
				matched = true
			case p.have(ast.Token_TARGET):
			default:
				p.syntaxErrorAt()
			}
		}
	}
	var cond *ast.Node
	if p.have(ast.Token_AND) {
		cond = p.parseAExpr(0)
	}
	p.expect(ast.Token_THEN)
	var m *ast.MergeWhenClause
	switch {
	case matched && p.kind() == ast.Token_UPDATE:
		// gram.y: merge_update
		p.next()
		p.expect(ast.Token_SET)
		m = &ast.MergeWhenClause{
			CommandType: ast.CmdType_CMD_UPDATE,
			Override:    ast.OverridingKind_OVERRIDING_NOT_SET,
			TargetList:  p.parseSetClauseList(),
		}
	case matched && p.kind() == ast.Token_DELETE_P:
		// gram.y: merge_delete
		p.next()
		m = &ast.MergeWhenClause{
			CommandType: ast.CmdType_CMD_DELETE,
			Override:    ast.OverridingKind_OVERRIDING_NOT_SET,
		}
	case !matched && p.kind() == ast.Token_INSERT:
		p.next()
		m = p.parseMergeInsertRest()
	case p.kind() == ast.Token_DO:
		p.next()
		p.expect(ast.Token_NOTHING)
		m = &ast.MergeWhenClause{
			CommandType: ast.CmdType_CMD_NOTHING,
			Override:    ast.OverridingKind_OVERRIDING_NOT_SET,
		}
	default:
		p.syntaxErrorAt()
	}
	m.MatchKind = matchKind
	m.Condition = cond
	return nMergeWhenClause(m)
}

// parseMergeInsertRest is gram.y's merge_insert (INSERT consumed).
func (p *parser) parseMergeInsertRest() *ast.MergeWhenClause {
	m := &ast.MergeWhenClause{
		CommandType: ast.CmdType_CMD_INSERT,
		Override:    ast.OverridingKind_OVERRIDING_NOT_SET,
	}
	switch {
	case p.kind() == ast.Token_DEFAULT:
		// INSERT DEFAULT VALUES
		p.next()
		p.expect(ast.Token_VALUES)
		return m
	case p.kind() == ast.Token_OVERRIDING:
		p.next()
		m.Override = p.parseOverrideKind()
		p.expect(ast.Token_VALUE_P)
	case p.have(ast.Token('(')):
		m.TargetList = p.parseInsertColumnList()
		p.expect(ast.Token(')'))
		if p.kind() == ast.Token_OVERRIDING {
			p.next()
			m.Override = p.parseOverrideKind()
			p.expect(ast.Token_VALUE_P)
		}
	}
	m.Values = p.parseMergeValuesClause()
	return m
}

// parseMergeValuesClause is gram.y's merge_values_clause.
func (p *parser) parseMergeValuesClause() []*ast.Node {
	p.expect(ast.Token_VALUES)
	p.expect(ast.Token('('))
	list := p.parseExprList()
	p.expect(ast.Token(')'))
	return list
}

// parseCopyStmt is gram.y's CopyStmt (both the relation and query forms).
func (p *parser) parseCopyStmt() *ast.Node {
	p.expect(ast.Token_COPY)
	n := &ast.CopyStmt{}

	if p.kind() == ast.Token('(') {
		// COPY '(' PreparableStmt ')' TO opt_program copy_file_name
		// opt_with copy_options
		p.next()
		n.Query = p.parsePreparableStmt()
		p.expect(ast.Token(')'))
		toTok := p.expect(ast.Token_TO)
		n.IsProgram = p.have(ast.Token_PROGRAM)
		filename, isNull := p.parseCopyFileName()
		n.Filename = filename
		if n.IsProgram && isNull {
			p.ereport("base_yyparse", "STDIN/STDOUT not allowed with PROGRAM", toTok.Start)
		}
		p.have(ast.Token_WITH) // opt_with
		n.Options = p.parseCopyOptions()
		return nCopyStmt(n)
	}

	// COPY opt_binary qualified_name opt_column_list copy_from opt_program
	// copy_file_name copy_delimiter opt_with copy_options where_clause
	var binary *ast.Node
	if p.kind() == ast.Token_BINARY {
		tok := p.next()
		binary = makeDefElem("format", nStr("binary"), tok.Start)
	}
	n.Relation = p.parseQualifiedName()
	if p.have(ast.Token('(')) {
		n.Attlist = p.columnList()
		p.expect(ast.Token(')'))
	}
	switch {
	case p.have(ast.Token_FROM):
		n.IsFrom = true
	case p.have(ast.Token_TO):
	default:
		p.syntaxErrorAt()
	}
	n.IsProgram = p.have(ast.Token_PROGRAM)
	fileTok := p.peek()
	filename, isNull := p.parseCopyFileName()
	n.Filename = filename
	// copy_delimiter: [USING] DELIMITERS Sconst. Bison's empty-production
	// location rule makes @8 (and the chain through empty copy_delimiter /
	// opt_using) fall back to the previous symbol's location.
	loc8 := fileTok.Start
	var delim *ast.Node
	if p.kind() == ast.Token_USING || p.kind() == ast.Token_DELIMITERS {
		if tok := p.peek(); tok.Kind == ast.Token_USING {
			p.next()
			loc8 = tok.Start
		}
		dtok := p.expect(ast.Token_DELIMITERS)
		delim = makeDefElem("delimiter", nStr(p.sconst()), dtok.Start)
	}
	if tok := p.peek(); tok.Kind == ast.Token_WITH {
		p.next()
		loc8 = tok.Start
	}
	opts := p.parseCopyOptions()
	whereLoc := int32(-1)
	if wtok := p.peek(); wtok.Kind == ast.Token_WHERE {
		p.next()
		n.WhereClause = p.parseAExpr(0)
		whereLoc = wtok.Start
	}
	// The action's checks, in order.
	if n.IsProgram && isNull {
		p.ereport("base_yyparse", "STDIN/STDOUT not allowed with PROGRAM", loc8)
	}
	if !n.IsFrom && n.WhereClause != nil {
		p.ereport("base_yyparse", "WHERE clause not allowed with COPY TO", whereLoc)
	}
	if binary != nil {
		n.Options = append(n.Options, binary)
	}
	if delim != nil {
		n.Options = append(n.Options, delim)
	}
	n.Options = append(n.Options, opts...)
	return nCopyStmt(n)
}

// parseCopyFileName is gram.y's copy_file_name; the bool reports the NULL
// (STDIN/STDOUT) cases.
func (p *parser) parseCopyFileName() (string, bool) {
	switch p.kind() {
	case ast.Token_STDIN, ast.Token_STDOUT:
		p.next()
		return "", true
	}
	return p.sconst(), false
}

// parseCopyOptions is gram.y's copy_options: either the parenthesized
// generic list or the legacy space-separated option keywords.
func (p *parser) parseCopyOptions() []*ast.Node {
	if p.kind() == ast.Token('(') {
		p.next()
		list := []*ast.Node{p.parseCopyGenericOptElem()}
		for p.have(ast.Token(',')) {
			list = append(list, p.parseCopyGenericOptElem())
		}
		p.expect(ast.Token(')'))
		return list
	}
	var list []*ast.Node
	for {
		item := p.parseCopyOptItem()
		if item == nil {
			return list
		}
		list = append(list, item)
	}
}

// parseCopyOptItem is gram.y's copy_opt_item; returns nil when the current
// token does not start one.
func (p *parser) parseCopyOptItem() *ast.Node {
	tok := p.peek()
	switch tok.Kind {
	case ast.Token_BINARY:
		p.next()
		return makeDefElem("format", nStr("binary"), tok.Start)
	case ast.Token_FREEZE:
		p.next()
		return makeDefElem("freeze", nBoolean(true), tok.Start)
	case ast.Token_DELIMITER:
		p.next()
		p.have(ast.Token_AS)
		return makeDefElem("delimiter", nStr(p.sconst()), tok.Start)
	case ast.Token_NULL_P:
		p.next()
		p.have(ast.Token_AS)
		return makeDefElem("null", nStr(p.sconst()), tok.Start)
	case ast.Token_CSV:
		p.next()
		return makeDefElem("format", nStr("csv"), tok.Start)
	case ast.Token_HEADER_P:
		p.next()
		return makeDefElem("header", nBoolean(true), tok.Start)
	case ast.Token_QUOTE:
		p.next()
		p.have(ast.Token_AS)
		return makeDefElem("quote", nStr(p.sconst()), tok.Start)
	case ast.Token_ESCAPE:
		p.next()
		p.have(ast.Token_AS)
		return makeDefElem("escape", nStr(p.sconst()), tok.Start)
	case ast.Token_FORCE:
		p.next()
		switch {
		case p.have(ast.Token_QUOTE):
			if p.have(ast.Token('*')) {
				return makeDefElem("force_quote", nAStar(), tok.Start)
			}
			return makeDefElem("force_quote", nList(p.columnList()), tok.Start)
		case p.have(ast.Token_NOT):
			p.expect(ast.Token_NULL_P)
			if p.have(ast.Token('*')) {
				return makeDefElem("force_not_null", nAStar(), tok.Start)
			}
			return makeDefElem("force_not_null", nList(p.columnList()), tok.Start)
		case p.have(ast.Token_NULL_P):
			if p.have(ast.Token('*')) {
				return makeDefElem("force_null", nAStar(), tok.Start)
			}
			return makeDefElem("force_null", nList(p.columnList()), tok.Start)
		}
		p.syntaxErrorAt()
	case ast.Token_ENCODING:
		p.next()
		return makeDefElem("encoding", nStr(p.sconst()), tok.Start)
	}
	return nil
}

// parseCopyGenericOptElem is gram.y's copy_generic_opt_elem.
func (p *parser) parseCopyGenericOptElem() *ast.Node {
	tok := p.peek()
	name := p.colLabel()
	return makeDefElem(name, p.parseCopyGenericOptArg(), tok.Start)
}

// parseCopyGenericOptArg is gram.y's copy_generic_opt_arg.
func (p *parser) parseCopyGenericOptArg() *ast.Node {
	tok := p.peek()
	switch tok.Kind {
	case ast.Token_TRUE_P:
		p.next()
		return nStr("true")
	case ast.Token_FALSE_P:
		p.next()
		return nStr("false")
	case ast.Token_ON:
		p.next()
		return nStr("on")
	case ast.Token_SCONST:
		p.next()
		return nStr(tok.Str)
	case ast.Token_ICONST, ast.Token_FCONST, ast.Token('+'), ast.Token('-'):
		return p.parseNumericOnly()
	case ast.Token('*'):
		p.next()
		return nAStar()
	case ast.Token_DEFAULT:
		p.next()
		return nStr("default")
	case ast.Token('('):
		p.next()
		list := []*ast.Node{nStr(p.parseOptBooleanOrString())}
		for p.have(ast.Token(',')) {
			list = append(list, nStr(p.parseOptBooleanOrString()))
		}
		p.expect(ast.Token(')'))
		return nList(list)
	}
	if isNonReservedWordToken(tok) {
		p.next()
		return nStr(tok.Str)
	}
	return nil
}

// parseOptBooleanOrString is gram.y's opt_boolean_or_string.
func (p *parser) parseOptBooleanOrString() string {
	switch p.kind() {
	case ast.Token_TRUE_P:
		p.next()
		return "true"
	case ast.Token_FALSE_P:
		p.next()
		return "false"
	case ast.Token_ON:
		p.next()
		return "on"
	case ast.Token_SCONST:
		return p.sconst()
	}
	return p.nonReservedWord()
}

// parsePrepareStmt is gram.y's PrepareStmt.
func (p *parser) parsePrepareStmt() *ast.Node {
	p.expect(ast.Token_PREPARE)
	n := &ast.PrepareStmt{Name: p.name()}
	// prep_type_clause
	if p.have(ast.Token('(')) {
		n.Argtypes = p.parseTypeList()
		p.expect(ast.Token(')'))
	}
	p.expect(ast.Token_AS)
	n.Query = p.parsePreparableStmt()
	return nPrepareStmt(n)
}

// parseTypeList is gram.y's type_list.
func (p *parser) parseTypeList() []*ast.Node {
	list := []*ast.Node{nTypeName(p.parseTypename())}
	for p.have(ast.Token(',')) {
		list = append(list, nTypeName(p.parseTypename()))
	}
	return list
}

// parseExecuteStmt is gram.y's ExecuteStmt (the CREATE TABLE AS EXECUTE
// forms land with the DDL milestone's CREATE machinery).
func (p *parser) parseExecuteStmt() *ast.Node {
	p.expect(ast.Token_EXECUTE)
	n := &ast.ExecuteStmt{Name: p.name()}
	// execute_param_clause
	if p.have(ast.Token('(')) {
		n.Params = p.parseExprList()
		p.expect(ast.Token(')'))
	}
	return nExecuteStmt(n)
}

// parseDeallocateStmt is gram.y's DeallocateStmt. A lone PREPARE with no
// name after it is itself the plan name ("DEALLOCATE prepare"), as the LALR
// lookahead resolves.
func (p *parser) parseDeallocateStmt() *ast.Node {
	p.expect(ast.Token_DEALLOCATE)
	if p.kind() == ast.Token_PREPARE &&
		(isColIdToken(p.peekN(1)) || p.kindN(1) == ast.Token_ALL) {
		p.next()
	}
	if p.have(ast.Token_ALL) {
		return nDeallocateStmt(&ast.DeallocateStmt{Isall: true, Location: -1})
	}
	tok := p.peek()
	return nDeallocateStmt(&ast.DeallocateStmt{Name: p.name(), Location: tok.Start})
}

// Cursor option flags (parsenodes.h).
const (
	cursorOptBinary      = 0x0001
	cursorOptScroll      = 0x0002
	cursorOptNoScroll    = 0x0004
	cursorOptInsensitive = 0x0008
	cursorOptAsensitive  = 0x0010
	cursorOptHold        = 0x0020
	cursorOptFastPlan    = 0x0100
)

// parseDeclareCursorStmt is gram.y's DeclareCursorStmt.
func (p *parser) parseDeclareCursorStmt() *ast.Node {
	p.expect(ast.Token_DECLARE)
	n := &ast.DeclareCursorStmt{Portalname: p.name()}
	// cursor_options — we always set FAST_PLAN, like the reference.
	options := int32(cursorOptFastPlan)
loop:
	for {
		switch p.kind() {
		case ast.Token_NO:
			p.next()
			p.expect(ast.Token_SCROLL)
			options |= cursorOptNoScroll
		case ast.Token_SCROLL:
			p.next()
			options |= cursorOptScroll
		case ast.Token_BINARY:
			p.next()
			options |= cursorOptBinary
		case ast.Token_ASENSITIVE:
			p.next()
			options |= cursorOptAsensitive
		case ast.Token_INSENSITIVE:
			p.next()
			options |= cursorOptInsensitive
		default:
			break loop
		}
	}
	p.expect(ast.Token_CURSOR)
	// opt_hold
	switch {
	case p.have(ast.Token_WITH):
		p.expect(ast.Token_HOLD)
		options |= cursorOptHold
	case p.have(ast.Token_WITHOUT):
		p.expect(ast.Token_HOLD)
	}
	n.Options = options
	p.expect(ast.Token_FOR)
	n.Query = p.parseSelectStmt()
	return nDeclareCursorStmt(n)
}

// parseClosePortalStmt is gram.y's ClosePortalStmt.
func (p *parser) parseClosePortalStmt() *ast.Node {
	p.expect(ast.Token_CLOSE)
	n := &ast.ClosePortalStmt{}
	if !p.have(ast.Token_ALL) {
		n.Portalname = p.name()
	}
	return nClosePortalStmt(n)
}

// fetchAll is FETCH_ALL (LONG_MAX on the 64-bit reference build).
const fetchAll = math.MaxInt64

// parseFetchStmt is gram.y's FetchStmt: FETCH fetch_args | MOVE fetch_args.
func (p *parser) parseFetchStmt(ismove bool) *ast.Node {
	p.next() // FETCH or MOVE
	n := p.parseFetchArgs()
	n.Ismove = ismove
	return nFetchStmt(n)
}

// fetchArgsFollows reports whether the token after a direction keyword
// continues fetch_args (opt_from_in cursor_name), which is what commits the
// keyword to its direction reading rather than being the cursor_name.
func (p *parser) fetchArgsFollows(n int) bool {
	tok := p.peekN(n)
	if tok.Kind == ast.Token_FROM || tok.Kind == ast.Token_IN_P {
		return true
	}
	return isColIdToken(tok)
}

// parseOptFromIn is gram.y's opt_from_in.
func (p *parser) parseOptFromIn() {
	if p.kind() == ast.Token_FROM || p.kind() == ast.Token_IN_P {
		p.next()
	}
}

// parseFetchArgs is gram.y's fetch_args. The direction keywords are all
// unreserved, so each doubles as a possible cursor_name; one token of
// lookahead decides, as in the LALR tables.
func (p *parser) parseFetchArgs() *ast.FetchStmt {
	tok := p.peek()
	switch tok.Kind {
	case ast.Token_FROM, ast.Token_IN_P:
		// from_in cursor_name
		p.next()
		return &ast.FetchStmt{Direction: ast.FetchDirection_FETCH_FORWARD, HowMany: 1, Portalname: p.name()}
	case ast.Token_NEXT:
		if p.fetchArgsFollows(1) {
			p.next()
			p.parseOptFromIn()
			return &ast.FetchStmt{Direction: ast.FetchDirection_FETCH_FORWARD, HowMany: 1, Portalname: p.name()}
		}
	case ast.Token_PRIOR:
		if p.fetchArgsFollows(1) {
			p.next()
			p.parseOptFromIn()
			return &ast.FetchStmt{Direction: ast.FetchDirection_FETCH_BACKWARD, HowMany: 1, Portalname: p.name()}
		}
	case ast.Token_FIRST_P:
		if p.fetchArgsFollows(1) {
			p.next()
			p.parseOptFromIn()
			return &ast.FetchStmt{Direction: ast.FetchDirection_FETCH_ABSOLUTE, HowMany: 1, Portalname: p.name()}
		}
	case ast.Token_LAST_P:
		if p.fetchArgsFollows(1) {
			p.next()
			p.parseOptFromIn()
			return &ast.FetchStmt{Direction: ast.FetchDirection_FETCH_ABSOLUTE, HowMany: -1, Portalname: p.name()}
		}
	case ast.Token_ABSOLUTE_P:
		if p.signedIconstAhead(1) {
			p.next()
			v := p.parseSignedIconst()
			p.parseOptFromIn()
			return &ast.FetchStmt{Direction: ast.FetchDirection_FETCH_ABSOLUTE, HowMany: int64(v), Portalname: p.name()}
		}
	case ast.Token_RELATIVE_P:
		if p.signedIconstAhead(1) {
			p.next()
			v := p.parseSignedIconst()
			p.parseOptFromIn()
			return &ast.FetchStmt{Direction: ast.FetchDirection_FETCH_RELATIVE, HowMany: int64(v), Portalname: p.name()}
		}
	case ast.Token_ICONST, ast.Token('+'), ast.Token('-'):
		v := p.parseSignedIconst()
		p.parseOptFromIn()
		return &ast.FetchStmt{Direction: ast.FetchDirection_FETCH_FORWARD, HowMany: int64(v), Portalname: p.name()}
	case ast.Token_ALL:
		p.next()
		p.parseOptFromIn()
		return &ast.FetchStmt{Direction: ast.FetchDirection_FETCH_FORWARD, HowMany: fetchAll, Portalname: p.name()}
	case ast.Token_FORWARD:
		switch {
		case p.kindN(1) == ast.Token_ALL:
			p.next()
			p.next()
			p.parseOptFromIn()
			return &ast.FetchStmt{Direction: ast.FetchDirection_FETCH_FORWARD, HowMany: fetchAll, Portalname: p.name()}
		case p.signedIconstAhead(1):
			p.next()
			v := p.parseSignedIconst()
			p.parseOptFromIn()
			return &ast.FetchStmt{Direction: ast.FetchDirection_FETCH_FORWARD, HowMany: int64(v), Portalname: p.name()}
		case p.fetchArgsFollows(1):
			p.next()
			p.parseOptFromIn()
			return &ast.FetchStmt{Direction: ast.FetchDirection_FETCH_FORWARD, HowMany: 1, Portalname: p.name()}
		}
	case ast.Token_BACKWARD:
		switch {
		case p.kindN(1) == ast.Token_ALL:
			p.next()
			p.next()
			p.parseOptFromIn()
			return &ast.FetchStmt{Direction: ast.FetchDirection_FETCH_BACKWARD, HowMany: fetchAll, Portalname: p.name()}
		case p.signedIconstAhead(1):
			p.next()
			v := p.parseSignedIconst()
			p.parseOptFromIn()
			return &ast.FetchStmt{Direction: ast.FetchDirection_FETCH_BACKWARD, HowMany: int64(v), Portalname: p.name()}
		case p.fetchArgsFollows(1):
			p.next()
			p.parseOptFromIn()
			return &ast.FetchStmt{Direction: ast.FetchDirection_FETCH_BACKWARD, HowMany: 1, Portalname: p.name()}
		}
	}
	// cursor_name
	return &ast.FetchStmt{Direction: ast.FetchDirection_FETCH_FORWARD, HowMany: 1, Portalname: p.name()}
}

// signedIconstAhead reports whether a SignedIconst starts n tokens ahead.
func (p *parser) signedIconstAhead(n int) bool {
	switch p.kindN(n) {
	case ast.Token_ICONST, ast.Token('+'), ast.Token('-'):
		return true
	}
	return false
}
