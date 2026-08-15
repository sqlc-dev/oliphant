package parse

import (
	"github.com/sqlc-dev/oliphant/ast"
	"github.com/sqlc-dev/oliphant/internal/lexer"
)

// parseCExprEx is gram.y's c_expr. The second result reports whether the
// node is a "row" in the grammar sense (explicit_row or implicit_row), which
// the a_expr OVERLAPS production needs.
func (p *parser) parseCExprEx() (*ast.Node, bool) {
	tok := p.peek()
	switch tok.Kind {
	case ast.Token_PARAM:
		// c_expr: PARAM opt_indirection
		p.next()
		pr := nParamRef(&ast.ParamRef{Number: tok.Ival, Location: tok.Start})
		if ind := p.parseOptIndirection(); ind != nil {
			return nAIndirection(&ast.A_Indirection{Arg: pr, Indirection: p.check_indirection(ind)}), false
		}
		return pr, false

	case ast.Token('('):
		// c_expr: select_with_parens [indirection] | '(' a_expr ')'
		// opt_indirection | implicit_row
		if sel, ok := p.trySelectWithParens(); ok {
			// c_expr: select_with_parens %prec UMINUS / select_with_parens indirection
			sl := nSubLink(&ast.SubLink{
				SubLinkType: ast.SubLinkType_EXPR_SUBLINK,
				Subselect:   sel,
				Location:    tok.Start,
			})
			if ind := p.parseOptIndirection(); ind != nil {
				return nAIndirection(&ast.A_Indirection{Arg: sl, Indirection: p.check_indirection(ind)}), false
			}
			return sl, false
		}
		p.next()
		expr := p.parseAExpr(0)
		if p.have(ast.Token(',')) {
			// implicit_row: '(' expr_list ',' a_expr ')'
			args := []*ast.Node{expr}
			args = append(args, p.parseAExpr(0))
			for p.have(ast.Token(',')) {
				args = append(args, p.parseAExpr(0))
			}
			p.expect(ast.Token(')'))
			return nRowExpr(&ast.RowExpr{
				Args:      args,
				RowFormat: ast.CoercionForm_COERCE_IMPLICIT_CAST,
				Location:  tok.Start,
			}), true
		}
		p.expect(ast.Token(')'))
		if ind := p.parseOptIndirection(); ind != nil {
			return nAIndirection(&ast.A_Indirection{Arg: expr, Indirection: p.check_indirection(ind)}), false
		}
		return expr, false

	case ast.Token_CASE:
		return p.parseCaseExpr(), false

	case ast.Token_EXISTS:
		// c_expr: EXISTS select_with_parens — but EXISTS is a col_name
		// keyword, so bare EXISTS falls through to columnref.
		if p.kindN(1) == ast.Token('(') {
			p.next()
			sel := p.mustSelectWithParens()
			return nSubLink(&ast.SubLink{
				SubLinkType: ast.SubLinkType_EXISTS_SUBLINK,
				Subselect:   sel,
				Location:    tok.Start,
			}), false
		}

	case ast.Token_ARRAY:
		// c_expr: ARRAY select_with_parens | ARRAY array_expr
		p.next()
		if p.kind() == ast.Token('[') {
			arr := p.parseArrayExpr()
			// point outermost A_ArrayExpr to the ARRAY keyword
			arr.GetAArrayExpr().Location = tok.Start
			return arr, false
		}
		sel := p.mustSelectWithParens()
		return nSubLink(&ast.SubLink{
			SubLinkType: ast.SubLinkType_ARRAY_SUBLINK,
			Subselect:   sel,
			Location:    tok.Start,
		}), false

	case ast.Token_ROW:
		// c_expr: explicit_row — ROW is a col_name keyword; bare ROW is a
		// columnref.
		if p.kindN(1) == ast.Token('(') {
			p.next()
			p.next() // '('
			var args []*ast.Node
			if p.kind() != ast.Token(')') {
				args = p.parseExprList()
			}
			p.expect(ast.Token(')'))
			return nRowExpr(&ast.RowExpr{
				Args:      args,
				RowFormat: ast.CoercionForm_COERCE_EXPLICIT_CALL,
				Location:  tok.Start,
			}), true
		}

	case ast.Token_GROUPING:
		// c_expr: GROUPING '(' expr_list ')' — col_name keyword, same deal.
		if p.kindN(1) == ast.Token('(') {
			p.next()
			p.next()
			args := p.parseExprList()
			p.expect(ast.Token(')'))
			return nGroupingFunc(&ast.GroupingFunc{Args: args, Location: tok.Start}), false
		}

	case ast.Token_ICONST:
		p.next()
		return makeIntConst(tok.Ival, tok.Start), false
	case ast.Token_FCONST:
		p.next()
		return makeFloatConst(tok.Str, tok.Start), false
	case ast.Token_SCONST:
		p.next()
		return makeStringConst(tok.Str, tok.Start), false
	case ast.Token_BCONST:
		p.next()
		return makeBitStringConst(tok.Str, tok.Start), false
	case ast.Token_XCONST:
		// AexprConst: XCONST is a bit constant per SQL99
		p.next()
		return makeBitStringConst(tok.Str, tok.Start), false
	case ast.Token_TRUE_P:
		p.next()
		return makeBoolAConst(true, tok.Start), false
	case ast.Token_FALSE_P:
		p.next()
		return makeBoolAConst(false, tok.Start), false
	case ast.Token_NULL_P:
		p.next()
		return makeNullAConst(tok.Start), false
	}

	if n := p.parseFuncExprKeyword(tok); n != nil {
		return n, false
	}
	return p.parseFuncOrColumnRef(), false
}

// parseCaseExpr is gram.y's case_expr:
// CASE case_arg when_clause_list case_default END_P
func (p *parser) parseCaseExpr() *ast.Node {
	tok := p.expect(ast.Token_CASE)
	c := &ast.CaseExpr{Location: tok.Start}
	if p.kind() != ast.Token_WHEN {
		c.Arg = p.parseAExpr(0)
	}
	// when_clause_list: there must be at least one
	w := p.expect(ast.Token_WHEN)
	for {
		expr := p.parseAExpr(0)
		p.expect(ast.Token_THEN)
		result := p.parseAExpr(0)
		c.Args = append(c.Args, nCaseWhen(&ast.CaseWhen{
			Expr: expr, Result: result, Location: w.Start,
		}))
		if p.kind() != ast.Token_WHEN {
			break
		}
		w = p.next()
	}
	if p.have(ast.Token_ELSE) {
		c.Defresult = p.parseAExpr(0)
	}
	p.expect(ast.Token_END_P)
	return nCaseExpr(c)
}

// parseArrayExpr is gram.y's array_expr:
// '[' expr_list ']' | '[' array_expr_list ']' | '[' ']'
func (p *parser) parseArrayExpr() *ast.Node {
	tok := p.expect(ast.Token('['))
	var elems []*ast.Node
	switch p.kind() {
	case ast.Token(']'):
	case ast.Token('['):
		elems = []*ast.Node{p.parseArrayExpr()}
		for p.have(ast.Token(',')) {
			elems = append(elems, p.parseArrayExpr())
		}
	default:
		elems = p.parseExprList()
	}
	p.expect(ast.Token(']'))
	return makeAArrayExpr(elems, tok.Start)
}

// parseOptIndirection is gram.y's opt_indirection; returns nil when empty.
func (p *parser) parseOptIndirection() []*ast.Node {
	var list []*ast.Node
	for {
		el, ok := p.parseIndirectionEl()
		if !ok {
			return list
		}
		list = append(list, el)
	}
}

// parseIndirectionEl is gram.y's indirection_el; reports false when the
// current token does not start one.
func (p *parser) parseIndirectionEl() (*ast.Node, bool) {
	switch p.kind() {
	case ast.Token('.'):
		p.next()
		if p.have(ast.Token('*')) {
			return nAStar(), true
		}
		return nStr(p.attrName()), true
	case ast.Token('['):
		p.next()
		ai := &ast.A_Indices{}
		if p.kind() == ast.Token(':') {
			// '[' opt_slice_bound ':' opt_slice_bound ']' with empty lower
			p.next()
			ai.IsSlice = true
			if p.kind() != ast.Token(']') {
				ai.Uidx = p.parseAExpr(0)
			}
			p.expect(ast.Token(']'))
			return nAIndices(ai), true
		}
		idx := p.parseAExpr(0)
		if p.have(ast.Token(':')) {
			ai.IsSlice = true
			ai.Lidx = idx
			if p.kind() != ast.Token(']') {
				ai.Uidx = p.parseAExpr(0)
			}
		} else {
			ai.Uidx = idx
		}
		p.expect(ast.Token(']'))
		return nAIndices(ai), true
	}
	return nil, false
}

// parseFuncOrColumnRef handles the identifier-led c_expr alternatives, which
// LALR only separates by lookahead after the (possibly qualified) name:
//
//	columnref:  ColId | ColId indirection
//	func_application: func_name '(' ...
//	AexprConst: func_name Sconst | func_name '(' ... ')' Sconst
//	            | func_name PARAM | func_name '(' ... ')' PARAM
//
// func_name is type_function_name | ColId indirection, so a
// type_func_name_keyword can head a function call or constant but never a
// columnref.
func (p *parser) parseFuncOrColumnRef() *ast.Node {
	tok := p.peek()

	if !isColIdToken(tok) {
		if !isTypeFunctionNameToken(tok) {
			p.syntaxError(tok)
		}
		// type_func_name_keyword: only func_name forms are possible, and
		// func_name from type_function_name takes no attrs.
		p.next()
		name := []*ast.Node{nStr(tok.Str)}
		switch p.kind() {
		case ast.Token('('):
			return p.parseFuncApplicationRest(name, tok)
		case ast.Token_SCONST:
			s := p.next()
			t := makeTypeNameFromNameList(name)
			t.Location = tok.Start
			return makeStringConstCast(s.Str, s.Start, t)
		case ast.Token_PARAM:
			prm := p.next()
			t := makeTypeNameFromNameList(name)
			t.Location = tok.Start
			return makeParamRefCast(prm.Ival, prm.Start, t)
		}
		p.syntaxErrorAt()
	}

	p.next()
	indirection := p.parseOptIndirection()

	// Only pure dotted-name indirection can form a func_name or a const's
	// type name; anything with subscripts or '.*' is a columnref.
	allNames := true
	for _, el := range indirection {
		if asString(el) == nil {
			allNames = false
			break
		}
	}

	if allNames {
		switch p.kind() {
		case ast.Token('('):
			name := append([]*ast.Node{nStr(tok.Str)}, indirection...)
			return p.parseFuncApplicationRest(p.check_func_name(name), tok)
		case ast.Token_SCONST:
			s := p.next()
			t := makeTypeNameFromNameList(append([]*ast.Node{nStr(tok.Str)}, indirection...))
			t.Location = tok.Start
			return makeStringConstCast(s.Str, s.Start, t)
		case ast.Token_PARAM:
			prm := p.next()
			t := makeTypeNameFromNameList(append([]*ast.Node{nStr(tok.Str)}, indirection...))
			t.Location = tok.Start
			return makeParamRefCast(prm.Ival, prm.Start, t)
		}
	}

	// columnref: ColId | ColId indirection
	return p.makeColumnRef(tok.Str, indirection, tok.Start)
}

// looksLikeSelectAhead reports whether the '(' at the current position opens
// a select_with_parens: skipping consecutive '('s, the first other token
// must begin a SelectStmt.
func (p *parser) looksLikeSelectAhead() bool {
	if p.kind() != ast.Token('(') {
		return false
	}
	i := 0
	for p.kindN(i) == ast.Token('(') {
		i++
	}
	switch p.kindN(i) {
	case ast.Token_SELECT, ast.Token_VALUES, ast.Token_TABLE,
		ast.Token_WITH, ast.Token_WITH_LA:
		return true
	}
	return false
}

// trySelectWithParens attempts select_with_parens at the current position.
// LALR keeps the sub-select and parenthesized-expression paths alive
// simultaneously; recursive descent reproduces that with bounded
// backtracking. The trial is only attempted when the parenthesized content
// can begin a select, and its parse error wins if it got past the point
// where the two paths diverge (the select keyword itself).
func (p *parser) trySelectWithParens() (*ast.Node, bool) {
	if !p.looksLikeSelectAhead() {
		return nil, false
	}
	mark := p.mark()
	var sel *ast.Node
	err := p.try(func() {
		sel = p.parseSelectWithParens()
	})
	if err == nil {
		return sel, true
	}
	p.reset(mark)
	return nil, false
}

// mustSelectWithParens parses select_with_parens, reporting the inner error
// on failure (no fallback path exists at the call sites).
func (p *parser) mustSelectWithParens() *ast.Node {
	return p.parseSelectWithParens()
}

// try runs f, catching a parse bail and returning its error.
func (p *parser) try(f func()) (err *lexer.Error) {
	defer func() {
		if r := recover(); r != nil {
			if b, ok := r.(bail); ok {
				err = b.err
				return
			}
			panic(r)
		}
	}()
	f()
	return nil
}
