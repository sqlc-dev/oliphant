package parse

import (
	"github.com/sqlc-dev/oliphant/ast"
	"github.com/sqlc-dev/oliphant/internal/lexer"
)

// selectLimit mirrors gram.y's SelectLimit struct.
type selectLimit struct {
	limitOffset *ast.Node
	limitCount  *ast.Node
	limitOption ast.LimitOption
	optionTok   lexer.Token
}

// parseSelectStmt is gram.y's SelectStmt:
// select_no_parens %prec UMINUS | select_with_parens %prec UMINUS
//
// A leading '(' is handled inside select_no_parens' select_clause, which
// takes select_with_parens as a set-operation operand; a bare
// select_with_parens is the degenerate case with no trailing clauses.
func (p *parser) parseSelectStmt() *ast.Node {
	return p.parseSelectNoParens()
}

// parseSelectWithParens is gram.y's select_with_parens:
// '(' select_no_parens ')' | '(' select_with_parens ')'
func (p *parser) parseSelectWithParens() *ast.Node {
	p.expect(ast.Token('('))
	sel := p.parseSelectNoParens()
	p.expect(ast.Token(')'))
	return sel
}

// parseSelectNoParens is gram.y's select_no_parens: a select_clause (set-op
// tree over simple_selects) plus the clauses that attach only at the top
// level of an unparenthesized select, merged via insertSelectOptions.
func (p *parser) parseSelectNoParens() *ast.Node {
	var with *ast.WithClause
	if p.kind() == ast.Token_WITH || p.kind() == ast.Token_WITH_LA {
		with = p.parseWithClause()
	}
	return p.parseSelectNoParensRest(with)
}

// parseSelectNoParensRest continues select_no_parens after opt_with_clause;
// split out so the WITH-prefixed statement dispatch can hand over a
// with_clause it already consumed.
func (p *parser) parseSelectNoParensRest(with *ast.WithClause) *ast.Node {
	sel := p.parseSelectClause(0)

	var sort []*ast.Node
	if p.kind() == ast.Token_ORDER {
		sort = p.parseSortClauseKeyword()
	}

	var locking []*ast.Node
	var limit *selectLimit
	// The locking clause may come before or after LIMIT/OFFSET.
	for {
		switch p.kind() {
		case ast.Token_FOR:
			locking = append(locking, p.parseForLockingClause()...)
			continue
		case ast.Token_LIMIT, ast.Token_OFFSET, ast.Token_FETCH:
			if limit != nil {
				// A second limit clause: bison would fail at this token.
				p.syntaxErrorAt()
			}
			limit = p.parseSelectLimit()
			continue
		}
		break
	}

	if with == nil && sort == nil && locking == nil && limit == nil {
		return sel
	}
	p.insertSelectOptions(sel.GetSelectStmt(), sort, locking, limit, with)
	return sel
}

// parseSelectClause is gram.y's select_clause with the UNION/INTERSECT/
// EXCEPT set operations, resolved by precedence climbing (INTERSECT binds
// tighter than UNION/EXCEPT; both %left).
func (p *parser) parseSelectClause(minPrec int) *ast.Node {
	var lhs *ast.Node
	if p.kind() == ast.Token('(') {
		lhs = p.parseSelectWithParens()
	} else {
		lhs = p.parseSimpleSelect()
	}
	for {
		var op ast.SetOperation
		var prec int
		switch p.kind() {
		case ast.Token_UNION:
			op, prec = ast.SetOperation_SETOP_UNION, 1
		case ast.Token_EXCEPT:
			op, prec = ast.SetOperation_SETOP_EXCEPT, 1
		case ast.Token_INTERSECT:
			op, prec = ast.SetOperation_SETOP_INTERSECT, 2
		default:
			return lhs
		}
		if prec < minPrec {
			return lhs
		}
		p.next()
		// set_quantifier
		all := p.have(ast.Token_ALL)
		if !all {
			p.have(ast.Token_DISTINCT)
		}
		rhs := p.parseSelectClause(prec + 1)
		lhs = makeSetOp(op, all, lhs, rhs)
	}
}

// parseSimpleSelect is gram.y's simple_select (minus the set operations,
// which parseSelectClause layers on top).
func (p *parser) parseSimpleSelect() *ast.Node {
	tok := p.peek()
	switch tok.Kind {
	case ast.Token_VALUES:
		return p.parseValuesClause()
	case ast.Token_TABLE:
		// simple_select: TABLE relation_expr
		p.next()
		rel := p.parseRelationExpr()
		cr := &ast.ColumnRef{Fields: []*ast.Node{nAStar()}, Location: -1}
		rt := &ast.ResTarget{Val: nColumnRef(cr), Location: -1}
		return nSelectStmt(&ast.SelectStmt{
			TargetList:  []*ast.Node{nResTarget(rt)},
			FromClause:  []*ast.Node{rel},
			LimitOption: ast.LimitOption_LIMIT_OPTION_DEFAULT,
			Op:          ast.SetOperation_SETOP_NONE,
		})
	}

	p.expect(ast.Token_SELECT)
	n := &ast.SelectStmt{
		LimitOption: ast.LimitOption_LIMIT_OPTION_DEFAULT,
		Op:          ast.SetOperation_SETOP_NONE,
	}

	// opt_all_clause opt_target_list | distinct_clause target_list
	requireTargets := false
	switch p.kind() {
	case ast.Token_ALL:
		p.next()
	case ast.Token_DISTINCT:
		p.next()
		if p.have(ast.Token_ON) {
			p.expect(ast.Token('('))
			n.DistinctClause = p.parseExprList()
			p.expect(ast.Token(')'))
		} else {
			// DISTINCT: list_make1(NIL) — a single empty-list placeholder.
			n.DistinctClause = []*ast.Node{{}}
		}
		requireTargets = true
	}

	if requireTargets || p.startsTargetEl() {
		n.TargetList = p.parseTargetList()
	}

	// into_clause
	if p.have(ast.Token_INTO) {
		n.IntoClause = p.parseIntoClauseRest()
	}
	// from_clause
	if p.have(ast.Token_FROM) {
		n.FromClause = p.parseFromList()
	}
	// where_clause
	if p.have(ast.Token_WHERE) {
		n.WhereClause = p.parseAExpr(0)
	}
	// group_clause
	if p.kind() == ast.Token_GROUP_P {
		p.next()
		p.expect(ast.Token_BY)
		if p.have(ast.Token_ALL) {
		} else if p.have(ast.Token_DISTINCT) {
			n.GroupDistinct = true
		}
		n.GroupClause = p.parseGroupByList()
	}
	// having_clause
	if p.have(ast.Token_HAVING) {
		n.HavingClause = p.parseAExpr(0)
	}
	// window_clause
	if p.have(ast.Token_WINDOW) {
		n.WindowClause = p.parseWindowDefinitionList()
	}
	return nSelectStmt(n)
}

// startsTargetEl reports whether the current token can begin target_el —
// needed because opt_target_list can be empty (SELECT;).
func (p *parser) startsTargetEl() bool {
	switch p.kind() {
	case 0, ast.Token(';'), ast.Token(')'), ast.Token_FROM, ast.Token_INTO,
		ast.Token_WHERE, ast.Token_GROUP_P, ast.Token_HAVING, ast.Token_WINDOW,
		ast.Token_ORDER, ast.Token_LIMIT, ast.Token_OFFSET, ast.Token_FETCH,
		ast.Token_FOR, ast.Token_UNION, ast.Token_INTERSECT, ast.Token_EXCEPT:
		return false
	}
	return true
}

// parseTargetList is gram.y's target_list.
func (p *parser) parseTargetList() []*ast.Node {
	list := []*ast.Node{p.parseTargetEl()}
	for p.have(ast.Token(',')) {
		list = append(list, p.parseTargetEl())
	}
	return list
}

// parseTargetEl is gram.y's target_el.
func (p *parser) parseTargetEl() *ast.Node {
	tok := p.peek()
	if tok.Kind == ast.Token('*') {
		p.next()
		cr := &ast.ColumnRef{Fields: []*ast.Node{nAStar()}, Location: tok.Start}
		return nResTarget(&ast.ResTarget{Val: nColumnRef(cr), Location: tok.Start})
	}
	val := p.parseAExpr(0)
	rt := &ast.ResTarget{Val: val, Location: tok.Start}
	if p.have(ast.Token_AS) {
		rt.Name = p.colLabel()
	} else if isBareColLabelToken(p.peek()) {
		rt.Name = p.next().Str
	}
	return nResTarget(rt)
}

// parseValuesClause is gram.y's values_clause.
func (p *parser) parseValuesClause() *ast.Node {
	p.expect(ast.Token_VALUES)
	n := &ast.SelectStmt{
		LimitOption: ast.LimitOption_LIMIT_OPTION_DEFAULT,
		Op:          ast.SetOperation_SETOP_NONE,
	}
	for {
		p.expect(ast.Token('('))
		list := p.parseExprList()
		p.expect(ast.Token(')'))
		n.ValuesLists = append(n.ValuesLists, nList(list))
		if !p.have(ast.Token(',')) {
			break
		}
	}
	return nSelectStmt(n)
}

// parseWithClause is gram.y's with_clause.
func (p *parser) parseWithClause() *ast.WithClause {
	tok := p.next() // WITH or WITH_LA
	w := &ast.WithClause{Location: tok.Start}
	if tok.Kind == ast.Token_WITH && p.have(ast.Token_RECURSIVE) {
		w.Recursive = true
	}
	w.Ctes = []*ast.Node{p.parseCommonTableExpr()}
	for p.have(ast.Token(',')) {
		w.Ctes = append(w.Ctes, p.parseCommonTableExpr())
	}
	return w
}

// parseCommonTableExpr is gram.y's common_table_expr.
func (p *parser) parseCommonTableExpr() *ast.Node {
	tok := p.peek()
	cte := &ast.CommonTableExpr{Location: tok.Start}
	cte.Ctename = p.name()
	if p.have(ast.Token('(')) {
		cte.Aliascolnames = p.nameList()
		p.expect(ast.Token(')'))
	}
	p.expect(ast.Token_AS)
	// opt_materialized
	cte.Ctematerialized = ast.CTEMaterialize_CTEMaterializeDefault
	if p.have(ast.Token_MATERIALIZED) {
		cte.Ctematerialized = ast.CTEMaterialize_CTEMaterializeAlways
	} else if p.kind() == ast.Token_NOT && p.kindN(1) == ast.Token_MATERIALIZED {
		p.next()
		p.next()
		cte.Ctematerialized = ast.CTEMaterialize_CTEMaterializeNever
	}
	p.expect(ast.Token('('))
	// PreparableStmt: SelectStmt (milestone 4; DML lands with milestone 5)
	cte.Ctequery = p.parsePreparableStmt()
	p.expect(ast.Token(')'))
	cte.SearchClause = p.parseOptSearchClause()
	cte.CycleClause = p.parseOptCycleClause()
	return nCommonTableExpr(cte)
}

// parsePreparableStmt is gram.y's PreparableStmt:
// SelectStmt | InsertStmt | UpdateStmt | DeleteStmt | MergeStmt.
func (p *parser) parsePreparableStmt() *ast.Node {
	switch p.kind() {
	case ast.Token_SELECT, ast.Token_TABLE, ast.Token_VALUES, ast.Token('('):
		return p.parseSelectStmt()
	case ast.Token_WITH, ast.Token_WITH_LA:
		return p.parseWithPrefixedStmt()
	case ast.Token_INSERT:
		return p.parseInsertStmt(nil)
	case ast.Token_UPDATE:
		return p.parseUpdateStmt(nil)
	case ast.Token_DELETE_P:
		return p.parseDeleteStmt(nil)
	case ast.Token_MERGE:
		return p.parseMergeStmt(nil)
	}
	p.syntaxErrorAt()
	return nil
}

// parseOptSearchClause is gram.y's opt_search_clause.
func (p *parser) parseOptSearchClause() *ast.CTESearchClause {
	if p.kind() != ast.Token_SEARCH {
		return nil
	}
	tok := p.next()
	s := &ast.CTESearchClause{Location: tok.Start}
	switch {
	case p.have(ast.Token_DEPTH):
	case p.have(ast.Token_BREADTH):
		s.SearchBreadthFirst = true
	default:
		p.syntaxErrorAt()
	}
	p.expect(ast.Token_FIRST_P)
	p.expect(ast.Token_BY)
	s.SearchColList = p.columnList()
	p.expect(ast.Token_SET)
	s.SearchSeqColumn = p.colId()
	return s
}

// parseOptCycleClause is gram.y's opt_cycle_clause.
func (p *parser) parseOptCycleClause() *ast.CTECycleClause {
	if p.kind() != ast.Token_CYCLE {
		return nil
	}
	tok := p.next()
	c := &ast.CTECycleClause{Location: tok.Start}
	c.CycleColList = p.columnList()
	p.expect(ast.Token_SET)
	c.CycleMarkColumn = p.colId()
	if p.have(ast.Token_TO) {
		c.CycleMarkValue = p.parseAexprConst()
		p.expect(ast.Token_DEFAULT)
		c.CycleMarkDefault = p.parseAexprConst()
	} else {
		c.CycleMarkValue = makeBoolAConst(true, -1)
		c.CycleMarkDefault = makeBoolAConst(false, -1)
	}
	p.expect(ast.Token_USING)
	c.CyclePathColumn = p.colId()
	return c
}

// parseAexprConst parses gram.y's AexprConst in isolation (used by the CTE
// cycle clause).
func (p *parser) parseAexprConst() *ast.Node {
	tok := p.peek()
	switch tok.Kind {
	case ast.Token_ICONST:
		p.next()
		return makeIntConst(tok.Ival, tok.Start)
	case ast.Token_FCONST:
		p.next()
		return makeFloatConst(tok.Str, tok.Start)
	case ast.Token_SCONST:
		p.next()
		return makeStringConst(tok.Str, tok.Start)
	case ast.Token_BCONST:
		p.next()
		return makeBitStringConst(tok.Str, tok.Start)
	case ast.Token_XCONST:
		p.next()
		return makeBitStringConst(tok.Str, tok.Start)
	case ast.Token_TRUE_P:
		p.next()
		return makeBoolAConst(true, tok.Start)
	case ast.Token_FALSE_P:
		p.next()
		return makeBoolAConst(false, tok.Start)
	case ast.Token_NULL_P:
		p.next()
		return makeNullAConst(tok.Start)
	case ast.Token('+'), ast.Token('-'):
		// AexprConst has no signed numbers, but doNegate applies via a_expr
		// in the callers that allow it; cycle clause uses bare AexprConst.
		p.syntaxErrorAt()
	}
	if c := p.parseConstTypenameExpr(); c != nil {
		return c
	}
	// func_name Sconst / func_name '(' ... ')' Sconst
	n := p.parseFuncOrColumnRef()
	if _, ok := n.Node.(*ast.Node_TypeCast); !ok {
		p.syntaxErrorAt()
	}
	return n
}

// parseIntoClauseRest is gram.y's into_clause/OptTempTableName after INTO.
func (p *parser) parseIntoClauseRest() *ast.IntoClause {
	into := &ast.IntoClause{OnCommit: ast.OnCommitAction_ONCOMMIT_NOOP}
	persistence := "p"
	switch p.kind() {
	case ast.Token_TEMPORARY, ast.Token_TEMP:
		p.next()
		p.have(ast.Token_TABLE)
		persistence = "t"
	case ast.Token_LOCAL:
		p.next()
		if !p.have(ast.Token_TEMPORARY) {
			p.expect(ast.Token_TEMP)
		}
		p.have(ast.Token_TABLE)
		persistence = "t"
	case ast.Token_GLOBAL:
		p.next()
		if !p.have(ast.Token_TEMPORARY) {
			p.expect(ast.Token_TEMP)
		}
		p.have(ast.Token_TABLE)
		persistence = "t"
	case ast.Token_UNLOGGED:
		p.next()
		p.have(ast.Token_TABLE)
		persistence = "u"
	case ast.Token_TABLE:
		p.next()
	}
	rel := p.parseQualifiedName()
	rel.Relpersistence = persistence
	into.Rel = rel
	return into
}

// parseQualifiedName is gram.y's qualified_name.
func (p *parser) parseQualifiedName() *ast.RangeVar {
	tok := p.peek()
	name := p.colId()
	if p.kind() == ast.Token('.') || p.kind() == ast.Token('[') {
		ind := p.parseOptIndirection()
		return p.makeRangeVarFromQualifiedName(name, ind, tok.Start)
	}
	return makeRangeVar("", name, tok.Start)
}

// parseFromList is gram.y's from_list.
func (p *parser) parseFromList() []*ast.Node {
	list := []*ast.Node{p.parseTableRef()}
	for p.have(ast.Token(',')) {
		list = append(list, p.parseTableRef())
	}
	return list
}

// parseTableRef is gram.y's table_ref, including joined_table layering.
func (p *parser) parseTableRef() *ast.Node {
	ref := p.parseTableRefAtom()
	return p.parseJoinedTableRest(ref)
}

// parseJoinedTableRest layers JOIN clauses onto a parsed table_ref.
func (p *parser) parseJoinedTableRest(larg *ast.Node) *ast.Node {
	for {
		switch p.kind() {
		case ast.Token_CROSS:
			p.next()
			p.expect(ast.Token_JOIN)
			rarg := p.parseTableRefAtom()
			larg = nJoinExpr(&ast.JoinExpr{
				Jointype: ast.JoinType_JOIN_INNER,
				Larg:     larg,
				Rarg:     rarg,
			})
		case ast.Token_JOIN:
			// A qual-requiring join cannot reduce until its join_qual
			// appears, so its right operand absorbs further joins first
			// (bison shifts); CROSS/NATURAL joins reduce immediately
			// (%left JOIN), keeping left associativity.
			p.next()
			rarg := p.parseTableRef()
			j := &ast.JoinExpr{Jointype: ast.JoinType_JOIN_INNER, Larg: larg, Rarg: rarg}
			p.parseJoinQual(j)
			larg = nJoinExpr(j)
		case ast.Token_INNER_P, ast.Token_LEFT, ast.Token_RIGHT, ast.Token_FULL:
			jt := p.parseJoinType()
			p.expect(ast.Token_JOIN)
			rarg := p.parseTableRef()
			j := &ast.JoinExpr{Jointype: jt, Larg: larg, Rarg: rarg}
			p.parseJoinQual(j)
			larg = nJoinExpr(j)
		case ast.Token_NATURAL:
			p.next()
			jt := ast.JoinType_JOIN_INNER
			switch p.kind() {
			case ast.Token_INNER_P, ast.Token_LEFT, ast.Token_RIGHT, ast.Token_FULL:
				jt = p.parseJoinType()
			}
			p.expect(ast.Token_JOIN)
			rarg := p.parseTableRefAtom()
			larg = nJoinExpr(&ast.JoinExpr{
				Jointype:  jt,
				IsNatural: true,
				Larg:      larg,
				Rarg:      rarg,
			})
		default:
			return larg
		}
	}
}

// parseJoinType is gram.y's join_type (the JOIN keyword not included).
func (p *parser) parseJoinType() ast.JoinType {
	switch p.next().Kind {
	case ast.Token_FULL:
		p.have(ast.Token_OUTER_P)
		return ast.JoinType_JOIN_FULL
	case ast.Token_LEFT:
		p.have(ast.Token_OUTER_P)
		return ast.JoinType_JOIN_LEFT
	case ast.Token_RIGHT:
		p.have(ast.Token_OUTER_P)
		return ast.JoinType_JOIN_RIGHT
	default: // INNER_P
		return ast.JoinType_JOIN_INNER
	}
}

// parseJoinQual is gram.y's join_qual, filling the JoinExpr.
func (p *parser) parseJoinQual(j *ast.JoinExpr) {
	if p.have(ast.Token_USING) {
		p.expect(ast.Token('('))
		j.UsingClause = p.nameList()
		p.expect(ast.Token(')'))
		// opt_alias_clause_for_join_using
		if p.have(ast.Token_AS) {
			j.JoinUsingAlias = &ast.Alias{Aliasname: p.colId()}
		}
		return
	}
	p.expect(ast.Token_ON)
	j.Quals = p.parseAExpr(0)
}

// parseTableRefAtom parses one table_ref without trailing JOINs.
func (p *parser) parseTableRefAtom() *ast.Node {
	switch p.kind() {
	case ast.Token('('):
		// select_with_parens or '(' joined_table ')'
		if sel, ok := p.trySelectWithParens(); ok {
			// table_ref: select_with_parens opt_alias_clause
			rs := &ast.RangeSubselect{Subquery: sel}
			rs.Alias = p.parseOptAliasClause()
			return nRangeSubselect(rs)
		}
		p.next()
		inner := p.parseTableRef()
		p.expect(ast.Token(')'))
		if j, ok := inner.Node.(*ast.Node_JoinExpr); ok {
			// table_ref: '(' joined_table ')' alias_clause
			if alias, ok := p.parseAliasClauseOpt(); ok {
				j.JoinExpr.Alias = alias
			}
			return inner
		}
		// A parenthesized non-join table_ref is only valid as joined_table
		// '(' joined_table ')' — bison rejects it at the ')'.
		p.syntaxErrorAt()
		return nil

	case ast.Token_XMLTABLE:
		if p.kindN(1) == ast.Token('(') {
			// table_ref: xmltable opt_alias_clause
			xt := p.parseXmlTable()
			xt.GetRangeTableFunc().Alias = p.parseOptAliasClause()
			return xt
		}

	case ast.Token_JSON_TABLE:
		if p.kindN(1) == ast.Token('(') {
			// table_ref: json_table opt_alias_clause
			jt := p.parseJsonTable()
			jt.GetJsonTable().Alias = p.parseOptAliasClause()
			return jt
		}

	case ast.Token_LATERAL_P:
		p.next()
		switch {
		case p.kind() == ast.Token('('):
			sel := p.mustSelectWithParens()
			rs := &ast.RangeSubselect{Lateral: true, Subquery: sel}
			rs.Alias = p.parseOptAliasClause()
			return nRangeSubselect(rs)
		case p.kind() == ast.Token_XMLTABLE:
			xt := p.parseXmlTable()
			xt.GetRangeTableFunc().Lateral = true
			xt.GetRangeTableFunc().Alias = p.parseOptAliasClause()
			return xt
		case p.kind() == ast.Token_JSON_TABLE:
			jt := p.parseJsonTable()
			jt.GetJsonTable().Lateral = true
			jt.GetJsonTable().Alias = p.parseOptAliasClause()
			return jt
		case p.kind() == ast.Token_ROWS:
			rf := p.parseRowsFrom()
			rf.Lateral = true
			p.parseFuncAliasClause(rf)
			return nRangeFunction(rf)
		default:
			// LATERAL_P func_table func_alias_clause
			fn := p.parseFuncTableFunc()
			fn.Lateral = true
			p.parseFuncAliasClause(fn)
			return nRangeFunction(fn)
		}

	case ast.Token_ROWS:
		// func_table: ROWS FROM '(' rowsfrom_list ')' opt_ordinality
		if p.kindN(1) == ast.Token_FROM {
			rf := p.parseRowsFrom()
			p.parseFuncAliasClause(rf)
			return nRangeFunction(rf)
		}

	case ast.Token_ONLY:
		return p.finishRelationTableRef(p.parseRelationExprVar())
	}

	// relation_expr or func_table (func_expr_windowless), distinguished by
	// what follows the qualified name: '(' means function.
	if p.startsFunctionCallAhead() {
		fn := p.parseFuncTableFunc()
		p.parseFuncAliasClause(fn)
		return nRangeFunction(fn)
	}
	return p.finishRelationTableRef(p.parseRelationExprVar())
}

// finishRelationTableRef attaches opt_alias_clause and tablesample_clause.
func (p *parser) finishRelationTableRef(rel *ast.RangeVar) *ast.Node {
	rel.Alias = p.parseOptAliasClause()
	if p.kind() == ast.Token_TABLESAMPLE {
		// gram.y: tablesample_clause — n->location = @2 (the method name).
		p.next()
		ftok := p.peek()
		method := p.parseFuncName(ftok)
		p.expect(ast.Token('('))
		args := p.parseExprList()
		p.expect(ast.Token(')'))
		ts := &ast.RangeTableSample{
			Relation: nRangeVar(rel),
			Method:   method,
			Args:     args,
			Location: ftok.Start,
		}
		if p.have(ast.Token_REPEATABLE) {
			p.expect(ast.Token('('))
			ts.Repeatable = p.parseAExpr(0)
			p.expect(ast.Token(')'))
		}
		return &ast.Node{Node: &ast.Node_RangeTableSample{RangeTableSample: ts}}
	}
	return nRangeVar(rel)
}

// parseFuncName parses func_name for contexts outside c_expr (TABLESAMPLE).
func (p *parser) parseFuncName(tok lexer.Token) []*ast.Node {
	if !isColIdToken(tok) {
		name := p.typeFunctionName()
		return []*ast.Node{nStr(name)}
	}
	p.next()
	names := []*ast.Node{nStr(tok.Str)}
	if ind := p.parseOptIndirection(); ind != nil {
		names = p.check_func_name(append(names, ind...))
	}
	return names
}

// startsFunctionCallAhead reports whether a func_table (rather than a
// relation) starts here: a name possibly dotted, followed by '('.
func (p *parser) startsFunctionCallAhead() bool {
	tok := p.peek()
	// Keywords with special function syntax are func_tables too
	// (func_expr_common_subexpr in func_expr_windowless); several are
	// reserved, so this check must run before the identifier gate.
	switch tok.Kind {
	case ast.Token_CURRENT_DATE, ast.Token_CURRENT_TIME, ast.Token_CURRENT_TIMESTAMP,
		ast.Token_LOCALTIME, ast.Token_LOCALTIMESTAMP, ast.Token_CURRENT_ROLE,
		ast.Token_CURRENT_USER, ast.Token_SESSION_USER, ast.Token_USER,
		ast.Token_CURRENT_CATALOG, ast.Token_CURRENT_SCHEMA, ast.Token_CAST,
		ast.Token_SYSTEM_USER:
		return true
	case ast.Token_COLLATION:
		// COLLATION FOR '(' a_expr ')'
		return p.kindN(1) == ast.Token_FOR
	}
	if !isColIdToken(tok) && !isTypeFunctionNameToken(tok) {
		return false
	}
	// Scan past the (possibly dotted) name: name ('.' attr)* '('.
	i := 1
	for {
		switch p.kindN(i) {
		case ast.Token('('):
			return true
		case ast.Token('.'):
			i++
			if isColLabelToken(p.peekN(i)) {
				i++
				continue
			}
			return false
		default:
			return false
		}
	}
}

// parseFuncTableFunc parses func_table's func_expr_windowless form into a
// RangeFunction (without alias handling).
func (p *parser) parseFuncTableFunc() *ast.RangeFunction {
	fn := p.parseFuncExprWindowless()
	rf := &ast.RangeFunction{
		Functions: []*ast.Node{nList([]*ast.Node{fn, nil})},
	}
	rf.Ordinality = p.parseOptOrdinality()
	return rf
}

// parseFuncExprWindowless is gram.y's func_expr_windowless.
func (p *parser) parseFuncExprWindowless() *ast.Node {
	tok := p.peek()
	if n := p.parseFuncExprKeyword(tok); n != nil {
		return n
	}
	// func_application without decorations.
	name := p.parseFuncName(tok)
	return p.parseFuncApplicationNoDecoration(name, tok)
}

// parseFuncApplicationNoDecoration parses just func_application.
func (p *parser) parseFuncApplicationNoDecoration(name []*ast.Node, nameTok lexer.Token) *ast.Node {
	p.expect(ast.Token('('))
	fc := makeFuncCall(name, nil, ast.CoercionForm_COERCE_EXPLICIT_CALL, nameTok.Start)
	switch {
	case p.have(ast.Token(')')):
	case p.have(ast.Token('*')):
		p.expect(ast.Token(')'))
		fc.AggStar = true
	case p.kind() == ast.Token_ALL:
		p.next()
		fc.Args = p.parseFuncArgList()
		fc.AggOrder = p.parseOptSortClause()
		p.expect(ast.Token(')'))
	case p.kind() == ast.Token_DISTINCT:
		p.next()
		fc.Args = p.parseFuncArgList()
		fc.AggOrder = p.parseOptSortClause()
		p.expect(ast.Token(')'))
		fc.AggDistinct = true
	case p.kind() == ast.Token_VARIADIC:
		p.next()
		fc.Args = []*ast.Node{p.parseFuncArgExpr()}
		fc.FuncVariadic = true
		fc.AggOrder = p.parseOptSortClause()
		p.expect(ast.Token(')'))
	default:
		fc.Args = []*ast.Node{p.parseFuncArgExpr()}
		for p.have(ast.Token(',')) {
			if p.have(ast.Token_VARIADIC) {
				fc.Args = append(fc.Args, p.parseFuncArgExpr())
				fc.FuncVariadic = true
				break
			}
			fc.Args = append(fc.Args, p.parseFuncArgExpr())
		}
		fc.AggOrder = p.parseOptSortClause()
		p.expect(ast.Token(')'))
	}
	return nFuncCall(fc)
}

// parseRowsFrom parses ROWS FROM '(' rowsfrom_list ')' opt_ordinality.
func (p *parser) parseRowsFrom() *ast.RangeFunction {
	p.expect(ast.Token_ROWS)
	p.expect(ast.Token_FROM)
	p.expect(ast.Token('('))
	rf := &ast.RangeFunction{IsRowsfrom: true}
	for {
		fn := p.parseFuncExprWindowless()
		var coldefs *ast.Node
		if p.kind() == ast.Token_AS {
			p.next()
			p.expect(ast.Token('('))
			defs := p.parseTableFuncElementList()
			p.expect(ast.Token(')'))
			coldefs = nList(defs)
		}
		if coldefs == nil {
			rf.Functions = append(rf.Functions, nList([]*ast.Node{fn, nil}))
		} else {
			rf.Functions = append(rf.Functions, nList([]*ast.Node{fn, coldefs}))
		}
		if !p.have(ast.Token(',')) {
			break
		}
	}
	p.expect(ast.Token(')'))
	rf.Ordinality = p.parseOptOrdinality()
	return rf
}

// parseOptOrdinality is gram.y's opt_ordinality: WITH_LA ORDINALITY.
func (p *parser) parseOptOrdinality() bool {
	if p.kind() == ast.Token_WITH_LA {
		p.next()
		p.expect(ast.Token_ORDINALITY)
		return true
	}
	return false
}

// parseFuncAliasClause is gram.y's func_alias_clause, applied to rf.
func (p *parser) parseFuncAliasClause(rf *ast.RangeFunction) {
	switch p.kind() {
	case ast.Token_AS:
		p.next()
		if p.have(ast.Token('(')) {
			rf.Coldeflist = p.parseTableFuncElementList()
			p.expect(ast.Token(')'))
			return
		}
		a := &ast.Alias{Aliasname: p.colId()}
		if p.have(ast.Token('(')) {
			if p.startsTableFuncElement() {
				rf.Alias = a
				rf.Coldeflist = p.parseTableFuncElementList()
				p.expect(ast.Token(')'))
				return
			}
			a.Colnames = p.nameList()
			p.expect(ast.Token(')'))
		}
		rf.Alias = a
	default:
		if !isColIdToken(p.peek()) {
			return
		}
		a := &ast.Alias{Aliasname: p.colId()}
		if p.have(ast.Token('(')) {
			if p.startsTableFuncElement() {
				rf.Alias = a
				rf.Coldeflist = p.parseTableFuncElementList()
				p.expect(ast.Token(')'))
				return
			}
			a.Colnames = p.nameList()
			p.expect(ast.Token(')'))
		}
		rf.Alias = a
	}
}

// startsTableFuncElement distinguishes a coldeflist "name Typename" from a
// plain column-name list "name, name": a TableFuncElement is ColId followed
// by a type name start.
func (p *parser) startsTableFuncElement() bool {
	if !isColIdToken(p.peek()) {
		return false
	}
	next := p.peekN(1)
	switch next.Kind {
	case ast.Token(','), ast.Token(')'):
		return false
	}
	return true
}

// parseTableFuncElementList is gram.y's TableFuncElementList.
func (p *parser) parseTableFuncElementList() []*ast.Node {
	list := []*ast.Node{p.parseTableFuncElement()}
	for p.have(ast.Token(',')) {
		list = append(list, p.parseTableFuncElement())
	}
	return list
}

// parseTableFuncElement is gram.y's TableFuncElement: ColId Typename
// opt_collate_clause.
func (p *parser) parseTableFuncElement() *ast.Node {
	tok := p.peek()
	name := p.colId()
	t := p.parseTypename()
	cd := &ast.ColumnDef{
		Colname:  name,
		TypeName: t,
		IsLocal:  true,
		Location: tok.Start,
	}
	if p.kind() == ast.Token_COLLATE {
		ctok := p.next()
		cd.CollClause = &ast.CollateClause{
			Collname: p.anyName(),
			Location: ctok.Start,
		}
	}
	return &ast.Node{Node: &ast.Node_ColumnDef{ColumnDef: cd}}
}

// parseRelationExpr is gram.y's relation_expr.
func (p *parser) parseRelationExpr() *ast.Node {
	if p.have(ast.Token_ONLY) {
		// ONLY qualified_name | ONLY '(' qualified_name ')'
		if p.have(ast.Token('(')) {
			rel := p.parseQualifiedName()
			p.expect(ast.Token(')'))
			rel.Inh = false
			return nRangeVar(rel)
		}
		rel := p.parseQualifiedName()
		rel.Inh = false
		return nRangeVar(rel)
	}
	rel := p.parseQualifiedName()
	rel.Inh = true
	if p.have(ast.Token('*')) {
		// qualified_name '*': explicit inheritance
	}
	return nRangeVar(rel)
}

// parseRelationExpr as *RangeVar for contexts that need the struct.
func (p *parser) parseRelationExprVar() *ast.RangeVar {
	return p.parseRelationExpr().GetRangeVar()
}

// parseOptAliasClause is gram.y's opt_alias_clause.
func (p *parser) parseOptAliasClause() *ast.Alias {
	a, _ := p.parseAliasClauseOpt()
	return a
}

// parseAliasClauseOpt parses alias_clause if present.
func (p *parser) parseAliasClauseOpt() (*ast.Alias, bool) {
	switch {
	case p.have(ast.Token_AS):
		a := &ast.Alias{Aliasname: p.colId()}
		if p.have(ast.Token('(')) {
			a.Colnames = p.nameList()
			p.expect(ast.Token(')'))
		}
		return a, true
	case isColIdToken(p.peek()):
		a := &ast.Alias{Aliasname: p.colId()}
		if p.have(ast.Token('(')) {
			a.Colnames = p.nameList()
			p.expect(ast.Token(')'))
		}
		return a, true
	}
	return nil, false
}

// parseGroupByList is gram.y's group_by_list.
func (p *parser) parseGroupByList() []*ast.Node {
	list := []*ast.Node{p.parseGroupByItem()}
	for p.have(ast.Token(',')) {
		list = append(list, p.parseGroupByItem())
	}
	return list
}

// parseGroupByItem is gram.y's group_by_item.
func (p *parser) parseGroupByItem() *ast.Node {
	tok := p.peek()
	switch tok.Kind {
	case ast.Token('('):
		// empty_grouping_set: '(' ')'
		if p.kindN(1) == ast.Token(')') {
			p.next()
			p.next()
			return nGroupingSet(&ast.GroupingSet{
				Kind:     ast.GroupingSetKind_GROUPING_SET_EMPTY,
				Location: tok.Start,
			})
		}
	case ast.Token_CUBE, ast.Token_ROLLUP:
		if p.kindN(1) == ast.Token('(') {
			p.next()
			p.next()
			content := p.parseExprList()
			p.expect(ast.Token(')'))
			kind := ast.GroupingSetKind_GROUPING_SET_CUBE
			if tok.Kind == ast.Token_ROLLUP {
				kind = ast.GroupingSetKind_GROUPING_SET_ROLLUP
			}
			return nGroupingSet(&ast.GroupingSet{Kind: kind, Content: content, Location: tok.Start})
		}
	case ast.Token_GROUPING:
		if p.kindN(1) == ast.Token_SETS {
			p.next()
			p.next()
			p.expect(ast.Token('('))
			content := p.parseGroupByList()
			p.expect(ast.Token(')'))
			return nGroupingSet(&ast.GroupingSet{
				Kind:     ast.GroupingSetKind_GROUPING_SET_SETS,
				Content:  content,
				Location: tok.Start,
			})
		}
	}
	return p.parseAExpr(0)
}

// parseWindowDefinitionList is gram.y's window_definition_list.
func (p *parser) parseWindowDefinitionList() []*ast.Node {
	list := []*ast.Node{p.parseWindowDefinition()}
	for p.have(ast.Token(',')) {
		list = append(list, p.parseWindowDefinition())
	}
	return list
}

// parseWindowDefinition is gram.y's window_definition.
func (p *parser) parseWindowDefinition() *ast.Node {
	name := p.colId()
	p.expect(ast.Token_AS)
	w := p.parseWindowSpecification()
	w.Name = name
	return nWindowDef(w)
}

// parseSortClauseKeyword parses ORDER BY sortby_list (ORDER is current).
func (p *parser) parseSortClauseKeyword() []*ast.Node {
	p.expect(ast.Token_ORDER)
	p.expect(ast.Token_BY)
	list := []*ast.Node{p.parseSortBy()}
	for p.have(ast.Token(',')) {
		list = append(list, p.parseSortBy())
	}
	return list
}

// parseSortClause is gram.y's sort_clause.
func (p *parser) parseSortClause() []*ast.Node {
	return p.parseSortClauseKeyword()
}

// parseOptSortClause is gram.y's opt_sort_clause.
func (p *parser) parseOptSortClause() []*ast.Node {
	if p.kind() == ast.Token_ORDER {
		return p.parseSortClauseKeyword()
	}
	return nil
}

// parseSortBy is gram.y's sortby.
func (p *parser) parseSortBy() *ast.Node {
	node := p.parseAExpr(0)
	sb := &ast.SortBy{Node: node, Location: -1}
	switch p.kind() {
	case ast.Token_USING:
		utok := p.next()
		optok := p.peek()
		_ = optok
		sb.SortbyDir = ast.SortByDir_SORTBY_USING
		sb.UseOp = p.parseQualAllOp()
		sb.Location = utok.Start
		// gram.y: @3 — the operator's location, not USING's.
		sb.Location = optok.Start
	case ast.Token_ASC:
		p.next()
		sb.SortbyDir = ast.SortByDir_SORTBY_ASC
	case ast.Token_DESC:
		p.next()
		sb.SortbyDir = ast.SortByDir_SORTBY_DESC
	default:
		sb.SortbyDir = ast.SortByDir_SORTBY_DEFAULT
	}
	// opt_nulls_order
	if p.kind() == ast.Token_NULLS_LA {
		p.next()
		if p.have(ast.Token_FIRST_P) {
			sb.SortbyNulls = ast.SortByNulls_SORTBY_NULLS_FIRST
		} else {
			p.expect(ast.Token_LAST_P)
			sb.SortbyNulls = ast.SortByNulls_SORTBY_NULLS_LAST
		}
	} else {
		sb.SortbyNulls = ast.SortByNulls_SORTBY_NULLS_DEFAULT
	}
	return nSortBy(sb)
}

// parseQualAllOp is gram.y's qual_all_Op.
func (p *parser) parseQualAllOp() []*ast.Node {
	if p.kind() == ast.Token_OPERATOR {
		return p.parseQualOp()
	}
	return []*ast.Node{nStr(p.parseAllOp())}
}

// parseForLockingClause is gram.y's for_locking_clause (one FOR item; the
// caller loops for for_locking_items).
func (p *parser) parseForLockingClause() []*ast.Node {
	p.expect(ast.Token_FOR)
	var strength ast.LockClauseStrength
	switch {
	case p.have(ast.Token_UPDATE):
		strength = ast.LockClauseStrength_LCS_FORUPDATE
	case p.have(ast.Token_NO):
		p.expect(ast.Token_KEY)
		p.expect(ast.Token_UPDATE)
		strength = ast.LockClauseStrength_LCS_FORNOKEYUPDATE
	case p.have(ast.Token_SHARE):
		strength = ast.LockClauseStrength_LCS_FORSHARE
	case p.have(ast.Token_KEY):
		p.expect(ast.Token_SHARE)
		strength = ast.LockClauseStrength_LCS_FORKEYSHARE
	case p.have(ast.Token_READ):
		// for_locking_clause: FOR READ ONLY => NIL
		p.expect(ast.Token_ONLY)
		return nil
	default:
		p.syntaxErrorAt()
	}
	lc := &ast.LockingClause{Strength: strength}
	if p.have(ast.Token_OF) {
		lc.LockedRels = []*ast.Node{nRangeVar(p.parseQualifiedName())}
		for p.have(ast.Token(',')) {
			lc.LockedRels = append(lc.LockedRels, nRangeVar(p.parseQualifiedName()))
		}
	}
	// opt_nowait_or_skip
	switch {
	case p.have(ast.Token_NOWAIT):
		lc.WaitPolicy = ast.LockWaitPolicy_LockWaitError
	case p.kind() == ast.Token_SKIP && p.kindN(1) == ast.Token_LOCKED:
		p.next()
		p.next()
		lc.WaitPolicy = ast.LockWaitPolicy_LockWaitSkip
	default:
		lc.WaitPolicy = ast.LockWaitPolicy_LockWaitBlock
	}
	return []*ast.Node{nLockingClause(lc)}
}

// parseSelectLimit is gram.y's select_limit.
func (p *parser) parseSelectLimit() *selectLimit {
	sl := &selectLimit{limitOption: ast.LimitOption_LIMIT_OPTION_UNDEFINED}
	seenLimit, seenOffset := false, false
	for {
		switch {
		case !seenLimit && p.kind() == ast.Token_LIMIT:
			tok := p.next()
			seenLimit = true
			sl.optionTok = tok
			if p.kind() == ast.Token_ALL {
				// LIMIT ALL is represented as a NULL constant
				all := p.next()
				sl.limitCount = makeNullAConst(all.Start)
			} else {
				sl.limitCount = p.parseAExpr(0)
			}
			sl.limitOption = ast.LimitOption_LIMIT_OPTION_COUNT
			if p.have(ast.Token(',')) {
				p.ereport("base_yyparse", "LIMIT #,# syntax is not supported", tok.Start)
			}
		case !seenOffset && p.kind() == ast.Token_OFFSET:
			p.next()
			seenOffset = true
			// select_offset_value: a_expr | select_fetch_first_value row_or_rows
			sl.limitOffset = p.parseAExpr(0)
			if p.kind() == ast.Token_ROW || p.kind() == ast.Token_ROWS {
				p.next()
			}
			if sl.limitOption == ast.LimitOption_LIMIT_OPTION_UNDEFINED {
				sl.limitOption = ast.LimitOption_LIMIT_OPTION_COUNT
			}
		case !seenLimit && p.kind() == ast.Token_FETCH:
			tok := p.next()
			seenLimit = true
			sl.optionTok = tok
			// first_or_next
			if !p.have(ast.Token_FIRST_P) {
				p.expect(ast.Token_NEXT)
			}
			// select_fetch_first_value or bare row_or_rows
			if p.kind() == ast.Token_ROW || p.kind() == ast.Token_ROWS {
				sl.limitCount = makeIntConst(1, -1)
			} else {
				sl.limitCount = p.parseFetchFirstValue()
			}
			if !p.have(ast.Token_ROW) {
				p.expect(ast.Token_ROWS)
			}
			if p.have(ast.Token_ONLY) {
				sl.limitOption = ast.LimitOption_LIMIT_OPTION_COUNT
			} else {
				p.expect(ast.Token_WITH)
				p.expect(ast.Token_TIES)
				sl.limitOption = ast.LimitOption_LIMIT_OPTION_WITH_TIES
			}
		default:
			if sl.limitOption == ast.LimitOption_LIMIT_OPTION_UNDEFINED {
				sl.limitOption = ast.LimitOption_LIMIT_OPTION_COUNT
			}
			return sl
		}
	}
}

// parseFetchFirstValue is gram.y's select_fetch_first_value.
func (p *parser) parseFetchFirstValue() *ast.Node {
	tok := p.peek()
	switch tok.Kind {
	case ast.Token('+'), ast.Token('-'):
		p.next()
		c := p.peek()
		switch c.Kind {
		case ast.Token_ICONST:
			p.next()
			v := makeIntConst(c.Ival, c.Start)
			if tok.Kind == ast.Token('-') {
				return doNegate(v, tok.Start)
			}
			return nAExpr(makeSimpleA_Expr(ast.A_Expr_Kind_AEXPR_OP, "+", nil, v, tok.Start))
		case ast.Token_FCONST:
			p.next()
			v := makeFloatConst(c.Str, c.Start)
			if tok.Kind == ast.Token('-') {
				return doNegate(v, tok.Start)
			}
			return nAExpr(makeSimpleA_Expr(ast.A_Expr_Kind_AEXPR_OP, "+", nil, v, tok.Start))
		}
		p.syntaxErrorAt()
	}
	c, _ := p.parseCExprEx()
	return c
}

// insertSelectOptions is gram.y's insertSelectOptions.
func (p *parser) insertSelectOptions(stmt *ast.SelectStmt, sortClause []*ast.Node,
	lockingClause []*ast.Node, limit *selectLimit, with *ast.WithClause) {

	if sortClause != nil {
		if stmt.SortClause != nil {
			p.ereport("insertSelectOptions", "multiple ORDER BY clauses not allowed",
				exprLocationList(sortClause))
		}
		stmt.SortClause = sortClause
	}
	stmt.LockingClause = append(stmt.LockingClause, lockingClause...)
	if limit != nil && limit.limitOffset != nil {
		if stmt.LimitOffset != nil {
			p.ereport("insertSelectOptions", "multiple OFFSET clauses not allowed",
				exprLocation(limit.limitOffset))
		}
		stmt.LimitOffset = limit.limitOffset
	}
	if limit != nil && limit.limitCount != nil {
		if stmt.LimitCount != nil {
			p.ereport("insertSelectOptions", "multiple LIMIT clauses not allowed",
				exprLocation(limit.limitCount))
		}
		stmt.LimitCount = limit.limitCount
	}
	if limit != nil {
		if stmt.LimitOption != ast.LimitOption_LIMIT_OPTION_DEFAULT {
			p.ereport("insertSelectOptions", "multiple limit options not allowed", -1)
		}
		if stmt.SortClause == nil && limit.limitOption == ast.LimitOption_LIMIT_OPTION_WITH_TIES {
			p.ereport("insertSelectOptions", "WITH TIES cannot be specified without ORDER BY clause", -1)
		}
		if limit.limitOption == ast.LimitOption_LIMIT_OPTION_WITH_TIES && stmt.LockingClause != nil {
			for _, lcn := range stmt.LockingClause {
				lc := lcn.GetLockingClause()
				if lc != nil && lc.WaitPolicy == ast.LockWaitPolicy_LockWaitSkip {
					p.ereport("insertSelectOptions",
						"SKIP LOCKED and WITH TIES options cannot be used together", -1)
				}
			}
		}
		stmt.LimitOption = limit.limitOption
	}
	if with != nil {
		if stmt.WithClause != nil {
			p.ereport("insertSelectOptions", "multiple WITH clauses not allowed", with.Location)
		}
		stmt.WithClause = with
	}
}
