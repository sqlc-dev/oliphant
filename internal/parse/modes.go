package parse

import (
	"fmt"

	"github.com/sqlc-dev/oliphant/ast"
	"github.com/sqlc-dev/oliphant/internal/lexer"
)

// Mode mirrors RawParseMode for the entry points raw_parser exposes beyond
// RAW_PARSE_DEFAULT. The PL/pgSQL compiler's check_sql_expr drives the
// non-default modes; RAW_PARSE_TYPE_NAME has no caller here (libpg_query
// mocks parse_datatype away).
type Mode int

const (
	ModeDefault Mode = iota
	ModeTypeName
	ModePlpgsqlExpr
	ModePlpgsqlAssign1
	ModePlpgsqlAssign2
	ModePlpgsqlAssign3
)

// ParseWithMode is raw_parser: parse_toplevel's MODE_* alternatives, each
// wrapping its result in a RawStmt exactly as the grammar actions do.
func ParseWithMode(input string, mode Mode) (res *ast.ParseResult, err *lexer.Error) {
	if mode == ModeDefault {
		return Parse(input)
	}
	s := lexer.New(input)
	p := &parser{src: s.Input(), filter: lexer.NewFilter(s)}
	defer func() {
		if r := recover(); r != nil {
			if b, ok := r.(bail); ok {
				res, err = nil, b.err
				return
			}
			panic(r)
		}
	}()

	res = &ast.ParseResult{Version: pgVersionNum}
	var stmt *ast.Node
	switch mode {
	case ModePlpgsqlExpr:
		// parse_toplevel: MODE_PLPGSQL_EXPR PLpgSQL_Expr
		stmt = p.parsePLpgSQLExpr()
	case ModePlpgsqlAssign1, ModePlpgsqlAssign2, ModePlpgsqlAssign3:
		// parse_toplevel: MODE_PLPGSQL_ASSIGNn PLAssignStmt
		nnames := int32(1 + int(mode-ModePlpgsqlAssign1))
		stmt = p.parsePLAssignStmt(nnames)
	default:
		panic(fmt.Sprintf("parse mode %d not supported", mode))
	}

	// The mode productions cover the whole input; anything left over is a
	// syntax error at that token, as in bison.
	if tok := p.peek(); tok.Kind != 0 {
		p.syntaxError(tok)
	}

	res.Stmts = []*ast.RawStmt{{Stmt: stmt, StmtLocation: 0}}
	return res, nil
}

// parsePLpgSQLExpr is gram.y's PLpgSQL_Expr: everything that can follow
// SELECT, without the SELECT keyword; the result is a SelectStmt.
func (p *parser) parsePLpgSQLExpr() *ast.Node {
	n := &ast.SelectStmt{
		LimitOption: ast.LimitOption_LIMIT_OPTION_DEFAULT,
		Op:          ast.SetOperation_SETOP_NONE,
	}

	// opt_distinct_clause: distinct_clause | opt_all_clause
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

	// opt_target_list
	if requireTargets || p.startsTargetEl() {
		n.TargetList = p.parseTargetList()
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
	// opt_sort_clause
	if p.kind() == ast.Token_ORDER {
		n.SortClause = p.parseSortClauseKeyword()
	}
	// opt_select_limit
	if p.kind() == ast.Token_LIMIT || p.kind() == ast.Token_OFFSET ||
		p.kind() == ast.Token_FETCH {
		limit := p.parseSelectLimit()
		n.LimitOffset = limit.limitOffset
		n.LimitCount = limit.limitCount
		if n.SortClause == nil && limit.limitOption == ast.LimitOption_LIMIT_OPTION_WITH_TIES {
			p.ereport("base_yyparse", "WITH TIES cannot be specified without ORDER BY clause", -1)
		}
		n.LimitOption = limit.limitOption
	}
	// opt_for_locking_clause
	for p.kind() == ast.Token_FOR {
		n.LockingClause = append(n.LockingClause, p.parseForLockingClause()...)
	}

	return nSelectStmt(n)
}

// parsePLAssignStmt is gram.y's PLAssignStmt:
// plassign_target opt_indirection plassign_equals PLpgSQL_Expr.
func (p *parser) parsePLAssignStmt(nnames int32) *ast.Node {
	n := &ast.PLAssignStmt{Nnames: nnames}

	// plassign_target: ColId | PARAM
	tok := p.peek()
	n.Location = tok.Start
	if tok.Kind == ast.Token_PARAM {
		p.next()
		n.Name = fmt.Sprintf("$%d", tok.Ival)
	} else {
		n.Name = p.colId()
	}

	n.Indirection = p.check_indirection(p.parseOptIndirection())

	// plassign_equals: COLON_EQUALS | '='
	tok = p.peek()
	if tok.Kind != ast.Token_COLON_EQUALS && tok.Kind != ast.Token('=') {
		p.syntaxError(tok)
	}
	p.next()

	val := p.parsePLpgSQLExpr()
	n.Val = val.GetSelectStmt()

	return &ast.Node{Node: &ast.Node_PlassignStmt{PlassignStmt: n}}
}
