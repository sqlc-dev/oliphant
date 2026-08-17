package parse

import (
	"github.com/sqlc-dev/oliphant/ast"
	"github.com/sqlc-dev/oliphant/internal/lexer"
)

// Operator precedence ladder, from gram.y's %left/%right/%nonassoc
// declarations (higher binds tighter). The climber mirrors bison's conflict
// resolution: left-assoc parses its right operand one level up, nonassoc
// levels error when chained, and %prec annotations are reproduced at the
// call sites they annotate.
const (
	precOr      = 1  // %left OR
	precAnd     = 2  // %left AND
	precNot     = 3  // %right NOT
	precIs      = 4  // %nonassoc IS ISNULL NOTNULL
	precCmp     = 5  // %nonassoc '<' '>' '=' LESS_EQUALS GREATER_EQUALS NOT_EQUALS
	precLike    = 6  // %nonassoc BETWEEN IN_P LIKE ILIKE SIMILAR NOT_LA
	precEscape  = 7  // %nonassoc ESCAPE
	precOp      = 8  // %left Op OPERATOR
	precAdd     = 9  // %left '+' '-'
	precMul     = 10 // %left '*' '/' '%'
	precExp     = 11 // %left '^'
	precAt      = 12 // %left AT
	precCollate = 13 // %left COLLATE
	precUminus  = 14 // %right UMINUS
)

// parseAExpr is gram.y's a_expr, parsed by precedence climbing: operators
// bind only while their level is at least minPrec.
func (p *parser) parseAExpr(minPrec int) *ast.Node {
	start := p.peek()
	var lhs *ast.Node
	lhsIsRow := false

	switch start.Kind {
	case ast.Token_NOT, ast.Token_NOT_LA:
		// a_expr: NOT a_expr | NOT_LA a_expr %prec NOT
		p.next()
		lhs = makeNotExpr(p.parseAExpr(precNot), start.Start)
	case ast.Token('+'):
		// a_expr: '+' a_expr %prec UMINUS
		p.next()
		lhs = nAExpr(makeSimpleA_Expr(ast.A_Expr_Kind_AEXPR_OP, "+", nil, p.parseAExpr(precUminus), start.Start))
	case ast.Token('-'):
		// a_expr: '-' a_expr %prec UMINUS
		p.next()
		lhs = doNegate(p.parseAExpr(precUminus), start.Start)
	case ast.Token_Op:
		// a_expr: qual_Op a_expr %prec Op
		p.next()
		lhs = nAExpr(makeA_Expr(ast.A_Expr_Kind_AEXPR_OP, []*ast.Node{nStr(start.Str)}, nil, p.parseAExpr(precOp+1), start.Start))
	case ast.Token_OPERATOR:
		// a_expr: qual_Op a_expr %prec Op (OPERATOR '(' any_operator ')')
		name := p.parseQualOp()
		lhs = nAExpr(makeA_Expr(ast.A_Expr_Kind_AEXPR_OP, name, nil, p.parseAExpr(precOp+1), start.Start))
	case ast.Token_DEFAULT:
		// a_expr: DEFAULT
		p.next()
		lhs = nSetToDefault(&ast.SetToDefault{Location: start.Start})
	case ast.Token_UNIQUE:
		// a_expr: UNIQUE opt_unique_null_treatment select_with_parens
		p.ereport("base_yyparse", "UNIQUE predicate is not yet implemented", start.Start)
	default:
		lhs, lhsIsRow = p.parseCExprEx()
	}

	return p.parseAExprInfix(lhs, lhsIsRow, start, minPrec)
}

// parseAExprInfix runs the infix/postfix part of the a_expr climb over an
// already-parsed left operand.
func (p *parser) parseAExprInfix(lhs *ast.Node, lhsIsRow bool, start lexer.Token, minPrec int) *ast.Node {
	// lastNonassoc tracks the level of the last operator consumed by THIS
	// loop, to reproduce bison's %nonassoc behavior: chaining two operators
	// of the same nonassoc level is a syntax error at the second operator.
	lastLevel := 0
	nonassoc := func(level int) {
		if lastLevel == level {
			p.syntaxErrorAt()
		}
		lastLevel = level
	}

	for {
		tok := p.peek()
		switch tok.Kind {
		case ast.Token_TYPECAST:
			// a_expr: a_expr TYPECAST Typename
			p.next()
			lhs = makeTypeCast(lhs, p.parseTypename(), tok.Start)
			lhsIsRow = false
			continue

		case ast.Token_COLLATE:
			// a_expr: a_expr COLLATE any_name
			if precCollate < minPrec {
				return lhs
			}
			p.next()
			lhs = nCollateClause(&ast.CollateClause{Arg: lhs, Collname: p.anyName(), Location: tok.Start})
			lastLevel = 0
			lhsIsRow = false
			continue

		case ast.Token_AT:
			// a_expr: a_expr AT TIME ZONE a_expr %prec AT | a_expr AT LOCAL %prec AT
			if precAt < minPrec {
				return lhs
			}
			p.next()
			if p.have(ast.Token_LOCAL) {
				lhs = nFuncCall(makeFuncCall(SystemFuncName("timezone"),
					[]*ast.Node{lhs}, ast.CoercionForm_COERCE_SQL_SYNTAX, -1))
			} else {
				p.expect(ast.Token_TIME)
				p.expect(ast.Token_ZONE)
				rhs := p.parseAExpr(precAt + 1)
				lhs = nFuncCall(makeFuncCall(SystemFuncName("timezone"),
					[]*ast.Node{rhs, lhs}, ast.CoercionForm_COERCE_SQL_SYNTAX, tok.Start))
			}
			lastLevel = 0
			lhsIsRow = false
			continue

		case ast.Token('^'):
			if precExp < minPrec {
				return lhs
			}
			if lhs2, ok := p.mathOpSubquery(lhs, tok); ok {
				lhs, lastLevel, lhsIsRow = lhs2, precOp, false
				continue
			}
			lhs = p.mathOpInfix(lhs, precExp+1)
			lhsIsRow = false
			continue

		case ast.Token('*'), ast.Token('/'), ast.Token('%'):
			if precMul < minPrec {
				return lhs
			}
			if lhs2, ok := p.mathOpSubquery(lhs, tok); ok {
				lhs, lastLevel, lhsIsRow = lhs2, precOp, false
				continue
			}
			lhs = p.mathOpInfix(lhs, precMul+1)
			lhsIsRow = false
			continue

		case ast.Token('+'), ast.Token('-'):
			if precAdd < minPrec {
				return lhs
			}
			if lhs2, ok := p.mathOpSubquery(lhs, tok); ok {
				lhs, lastLevel, lhsIsRow = lhs2, precOp, false
				continue
			}
			lhs = p.mathOpInfix(lhs, precAdd+1)
			lhsIsRow = false
			continue

		case ast.Token('<'), ast.Token('>'), ast.Token('='),
			ast.Token_LESS_EQUALS, ast.Token_GREATER_EQUALS, ast.Token_NOT_EQUALS:
			if precCmp < minPrec {
				return lhs
			}
			if sub := p.subTypeAfterOp(1); sub != ast.SubLinkType_SUB_LINK_TYPE_UNDEFINED {
				// a_expr subquery_Op sub_type ... %prec Op — but the
				// shift/reduce decision happens at the comparison token's own
				// precedence.
				nonassoc(precCmp)
				p.next()
				lhs = p.parseSubqueryOpRest(lhs, []*ast.Node{nStr(mathOpName(tok))}, tok)
				lastLevel = precOp
				lhsIsRow = false
				continue
			}
			nonassoc(precCmp)
			lhs = p.mathOpInfix(lhs, precCmp+1)
			lhsIsRow = false
			continue

		case ast.Token_Op:
			if precOp < minPrec {
				return lhs
			}
			if sub := p.subTypeAfterOp(1); sub != ast.SubLinkType_SUB_LINK_TYPE_UNDEFINED {
				p.next()
				lhs = p.parseSubqueryOpRest(lhs, []*ast.Node{nStr(tok.Str)}, tok)
				lastLevel = precOp
				lhsIsRow = false
				continue
			}
			// a_expr qual_Op a_expr %prec Op
			p.next()
			lhs = nAExpr(makeA_Expr(ast.A_Expr_Kind_AEXPR_OP, []*ast.Node{nStr(tok.Str)}, lhs, p.parseAExpr(precOp+1), tok.Start))
			lastLevel = precOp
			lhsIsRow = false
			continue

		case ast.Token_OPERATOR:
			if precOp < minPrec {
				return lhs
			}
			name := p.parseQualOp()
			if sub := p.subTypeAfterOp(0); sub != ast.SubLinkType_SUB_LINK_TYPE_UNDEFINED {
				lhs = p.parseSubqueryOpRest(lhs, name, tok)
			} else {
				lhs = nAExpr(makeA_Expr(ast.A_Expr_Kind_AEXPR_OP, name, lhs, p.parseAExpr(precOp+1), tok.Start))
			}
			lastLevel = precOp
			lhsIsRow = false
			continue

		case ast.Token_AND:
			if precAnd < minPrec {
				return lhs
			}
			p.next()
			lhs = makeAndExpr(lhs, p.parseAExpr(precAnd+1), tok.Start)
			lastLevel = precAnd
			lhsIsRow = false
			continue

		case ast.Token_OR:
			if precOr < minPrec {
				return lhs
			}
			p.next()
			lhs = makeOrExpr(lhs, p.parseAExpr(precOr+1), tok.Start)
			lastLevel = precOr
			lhsIsRow = false
			continue

		case ast.Token_LIKE, ast.Token_ILIKE:
			if precLike < minPrec {
				return lhs
			}
			if sub := p.subTypeAfterOp(1); sub != ast.SubLinkType_SUB_LINK_TYPE_UNDEFINED {
				nonassoc(precLike)
				p.next()
				name := "~~"
				if tok.Kind == ast.Token_ILIKE {
					name = "~~*"
				}
				lhs = p.parseSubqueryOpRest(lhs, []*ast.Node{nStr(name)}, tok)
				lastLevel = precOp
				lhsIsRow = false
				continue
			}
			nonassoc(precLike)
			p.next()
			kind, name := ast.A_Expr_Kind_AEXPR_LIKE, "~~"
			if tok.Kind == ast.Token_ILIKE {
				kind, name = ast.A_Expr_Kind_AEXPR_ILIKE, "~~*"
			}
			rhs := p.parseAExpr(precEscape)
			if p.have(ast.Token_ESCAPE) {
				esc := p.parseAExpr(precEscape)
				fc := makeFuncCall(SystemFuncName("like_escape"),
					[]*ast.Node{rhs, esc}, ast.CoercionForm_COERCE_EXPLICIT_CALL, tok.Start)
				rhs = nFuncCall(fc)
			}
			lhs = nAExpr(makeSimpleA_Expr(kind, name, lhs, rhs, tok.Start))
			lhsIsRow = false
			continue

		case ast.Token_SIMILAR:
			if precLike < minPrec {
				return lhs
			}
			// a_expr SIMILAR TO a_expr [ESCAPE a_expr] %prec SIMILAR. SIMILAR
			// without TO exists only inside substr_list; leave it unconsumed
			// there so the SUBSTRING production can use it.
			if p.inSubstrList && p.peekN(1).Kind != ast.Token_TO {
				return lhs
			}
			nonassoc(precLike)
			p.next()
			p.expect(ast.Token_TO)
			rhs := p.parseAExpr(precEscape)
			args := []*ast.Node{rhs}
			if p.have(ast.Token_ESCAPE) {
				args = append(args, p.parseAExpr(precEscape))
			}
			fc := makeFuncCall(SystemFuncName("similar_to_escape"), args,
				ast.CoercionForm_COERCE_EXPLICIT_CALL, tok.Start)
			lhs = nAExpr(makeSimpleA_Expr(ast.A_Expr_Kind_AEXPR_SIMILAR, "~", lhs, nFuncCall(fc), tok.Start))
			lhsIsRow = false
			continue

		case ast.Token_NOT_LA:
			if precLike < minPrec {
				return lhs
			}
			nonassoc(precLike)
			p.next()
			lhs = p.parseNotLaRest(lhs, tok)
			lhsIsRow = false
			continue

		case ast.Token_BETWEEN:
			if precLike < minPrec {
				return lhs
			}
			nonassoc(precLike)
			p.next()
			lhs = p.parseBetweenRest(lhs, tok, false)
			lhsIsRow = false
			continue

		case ast.Token_IN_P:
			if precLike < minPrec {
				return lhs
			}
			nonassoc(precLike)
			p.next()
			lhs = p.parseInRest(lhs, tok, false)
			lhsIsRow = false
			continue

		case ast.Token_IS, ast.Token_ISNULL, ast.Token_NOTNULL:
			if precIs < minPrec {
				return lhs
			}
			nonassoc(precIs)
			lhs = p.parseIsRest(lhs, start)
			lhsIsRow = false
			continue

		case ast.Token_OVERLAPS:
			// a_expr: row OVERLAPS row
			if !lhsIsRow {
				return lhs
			}
			p.next()
			lhs = p.parseOverlapsRest(lhs, start, tok)
			lhsIsRow = false
			lastLevel = 0
			continue

		default:
			return lhs
		}
	}
}

// mathOpSubquery handles "a_expr subquery_Op sub_type ..." when the
// operator is one of the explicitly-known MathOps (all_Op includes them).
func (p *parser) mathOpSubquery(lhs *ast.Node, tok lexer.Token) (*ast.Node, bool) {
	if p.subTypeAfterOp(1) == ast.SubLinkType_SUB_LINK_TYPE_UNDEFINED {
		return nil, false
	}
	p.next()
	return p.parseSubqueryOpRest(lhs, []*ast.Node{nStr(mathOpName(tok))}, tok), true
}

// mathOpInfix consumes the current (explicitly-known) operator token and
// builds makeSimpleA_Expr(AEXPR_OP, op, lhs, rhs, @2).
func (p *parser) mathOpInfix(lhs *ast.Node, rhsPrec int) *ast.Node {
	tok := p.next()
	return nAExpr(makeSimpleA_Expr(ast.A_Expr_Kind_AEXPR_OP, mathOpName(tok), lhs, p.parseAExpr(rhsPrec), tok.Start))
}

// mathOpName is gram.y's MathOp: the operator's canonical spelling.
func mathOpName(tok lexer.Token) string {
	switch tok.Kind {
	case ast.Token_LESS_EQUALS:
		return "<="
	case ast.Token_GREATER_EQUALS:
		return ">="
	case ast.Token_NOT_EQUALS:
		// scan.l normalizes != to <> before the grammar ever sees it.
		return "<>"
	default:
		return string(rune(tok.Kind))
	}
}

// subTypeAfterOp checks whether the token n ahead is ANY/SOME/ALL — the
// marker of the a_expr subquery_Op sub_type productions.
func (p *parser) subTypeAfterOp(n int) ast.SubLinkType {
	switch p.kindN(n) {
	case ast.Token_ANY, ast.Token_SOME:
		return ast.SubLinkType_ANY_SUBLINK
	case ast.Token_ALL:
		return ast.SubLinkType_ALL_SUBLINK
	}
	return ast.SubLinkType_SUB_LINK_TYPE_UNDEFINED
}

// parseSubqueryOpRest parses the remainder of
//
//	a_expr subquery_Op sub_type select_with_parens %prec Op
//	a_expr subquery_Op sub_type '(' a_expr ')' %prec Op
//
// with the operator (and its location token) already consumed.
func (p *parser) parseSubqueryOpRest(lhs *ast.Node, opName []*ast.Node, opTok lexer.Token) *ast.Node {
	var subType ast.SubLinkType
	switch p.next().Kind {
	case ast.Token_ANY, ast.Token_SOME:
		subType = ast.SubLinkType_ANY_SUBLINK
	default:
		subType = ast.SubLinkType_ALL_SUBLINK
	}
	if sel, ok := p.trySelectWithParens(); ok {
		return nSubLink(&ast.SubLink{
			SubLinkType: subType,
			Testexpr:    lhs,
			OperName:    opName,
			Subselect:   sel,
			Location:    opTok.Start,
		})
	}
	p.expect(ast.Token('('))
	rhs := p.parseAExpr(0)
	p.expect(ast.Token(')'))
	kind := ast.A_Expr_Kind_AEXPR_OP_ANY
	if subType == ast.SubLinkType_ALL_SUBLINK {
		kind = ast.A_Expr_Kind_AEXPR_OP_ALL
	}
	return nAExpr(makeA_Expr(kind, opName, lhs, rhs, opTok.Start))
}

// parseNotLaRest handles the a_expr NOT_LA {BETWEEN,IN_P,LIKE,ILIKE,SIMILAR}
// productions; the NOT_LA token is already consumed and passed as notTok.
func (p *parser) parseNotLaRest(lhs *ast.Node, notTok lexer.Token) *ast.Node {
	switch p.peek().Kind {
	case ast.Token_BETWEEN:
		p.next()
		return p.parseBetweenRest(lhs, notTok, true)
	case ast.Token_IN_P:
		p.next()
		return p.parseInRest(lhs, notTok, true)
	case ast.Token_LIKE, ast.Token_ILIKE:
		opTok := p.next()
		kind, name := ast.A_Expr_Kind_AEXPR_LIKE, "!~~"
		if opTok.Kind == ast.Token_ILIKE {
			kind, name = ast.A_Expr_Kind_AEXPR_ILIKE, "!~~*"
		}
		if sub := p.subTypeAfterOp(0); sub != ast.SubLinkType_SUB_LINK_TYPE_UNDEFINED {
			// a_expr subquery_Op(NOT_LA LIKE/ILIKE) sub_type ...
			return p.parseSubqueryOpRest(lhs, []*ast.Node{nStr(name)}, notTok)
		}
		rhs := p.parseAExpr(precEscape)
		if p.have(ast.Token_ESCAPE) {
			esc := p.parseAExpr(precEscape)
			fc := makeFuncCall(SystemFuncName("like_escape"),
				[]*ast.Node{rhs, esc}, ast.CoercionForm_COERCE_EXPLICIT_CALL, notTok.Start)
			rhs = nFuncCall(fc)
		}
		return nAExpr(makeSimpleA_Expr(kind, name, lhs, rhs, notTok.Start))
	case ast.Token_SIMILAR:
		p.next()
		p.expect(ast.Token_TO)
		rhs := p.parseAExpr(precEscape)
		args := []*ast.Node{rhs}
		if p.have(ast.Token_ESCAPE) {
			args = append(args, p.parseAExpr(precEscape))
		}
		fc := makeFuncCall(SystemFuncName("similar_to_escape"), args,
			ast.CoercionForm_COERCE_EXPLICIT_CALL, notTok.Start)
		return nAExpr(makeSimpleA_Expr(ast.A_Expr_Kind_AEXPR_SIMILAR, "!~", lhs, nFuncCall(fc), notTok.Start))
	}
	p.syntaxErrorAt()
	return nil
}

// parseBetweenRest handles [NOT_LA] BETWEEN [SYMMETRIC|ASYMMETRIC] b_expr AND
// a_expr; opTok is the BETWEEN (or NOT) token for the node location.
func (p *parser) parseBetweenRest(lhs *ast.Node, opTok lexer.Token, not bool) *ast.Node {
	symmetric := false
	if p.have(ast.Token_SYMMETRIC) {
		symmetric = true
	} else {
		p.have(ast.Token_ASYMMETRIC) // opt_asymmetric
	}
	low := p.parseBExpr(0)
	p.expect(ast.Token_AND)
	high := p.parseAExpr(precLike + 1)
	kind := ast.A_Expr_Kind_AEXPR_BETWEEN
	name := "BETWEEN"
	switch {
	case not && symmetric:
		kind, name = ast.A_Expr_Kind_AEXPR_NOT_BETWEEN_SYM, "NOT BETWEEN SYMMETRIC"
	case not:
		kind, name = ast.A_Expr_Kind_AEXPR_NOT_BETWEEN, "NOT BETWEEN"
	case symmetric:
		kind, name = ast.A_Expr_Kind_AEXPR_BETWEEN_SYM, "BETWEEN SYMMETRIC"
	}
	return nAExpr(makeSimpleA_Expr(kind, name,
		lhs, nList([]*ast.Node{low, high}), opTok.Start))
}

// parseInRest handles a_expr [NOT_LA] IN_P (select_with_parens |
// '(' expr_list ')') — PG 18 inlines what used to be in_expr, and the scalar
// form records the parens as rexpr_list_start/rexpr_list_end.
func (p *parser) parseInRest(lhs *ast.Node, opTok lexer.Token, not bool) *ast.Node {
	if sel, ok := p.trySelectWithParens(); ok {
		n := &ast.SubLink{
			SubLinkType: ast.SubLinkType_ANY_SUBLINK,
			Testexpr:    lhs,
			Subselect:   sel,
			Location:    opTok.Start,
		}
		if not {
			// NOT (foo = ANY (subquery)), NOT at same parse location
			return makeNotExpr(nSubLink(n), opTok.Start)
		}
		return nSubLink(n)
	}
	ltok := p.expect(ast.Token('('))
	list := p.parseExprList()
	rtok := p.expect(ast.Token(')'))
	name := "="
	if not {
		name = "<>"
	}
	n := makeSimpleA_Expr(ast.A_Expr_Kind_AEXPR_IN, name, lhs, nList(list), opTok.Start)
	n.RexprListStart = ltok.Start
	n.RexprListEnd = rtok.Start
	return nAExpr(n)
}

// parseIsRest handles the postfix IS/ISNULL/NOTNULL family. exprStart is the
// token that began the a_expr (for the SQL/JSON predicate's location).
func (p *parser) parseIsRest(lhs *ast.Node, exprStart lexer.Token) *ast.Node {
	tok := p.next()
	switch tok.Kind {
	case ast.Token_ISNULL:
		// a_expr ISNULL
		return nNullTest(&ast.NullTest{Arg: lhs, Nulltesttype: ast.NullTestType_IS_NULL, Location: tok.Start})
	case ast.Token_NOTNULL:
		// a_expr NOTNULL
		return nNullTest(&ast.NullTest{Arg: lhs, Nulltesttype: ast.NullTestType_IS_NOT_NULL, Location: tok.Start})
	}

	// tok is IS
	not := false
	if p.peek().Kind == ast.Token_NOT {
		p.next()
		not = true
	}
	boolTest := func(t ast.BoolTestType) *ast.Node {
		p.next()
		return nBooleanTest(&ast.BooleanTest{Arg: lhs, Booltesttype: t, Location: tok.Start})
	}
	switch p.peek().Kind {
	case ast.Token_NULL_P:
		p.next()
		t := ast.NullTestType_IS_NULL
		if not {
			t = ast.NullTestType_IS_NOT_NULL
		}
		return nNullTest(&ast.NullTest{Arg: lhs, Nulltesttype: t, Location: tok.Start})
	case ast.Token_TRUE_P:
		if not {
			return boolTest(ast.BoolTestType_IS_NOT_TRUE)
		}
		return boolTest(ast.BoolTestType_IS_TRUE)
	case ast.Token_FALSE_P:
		if not {
			return boolTest(ast.BoolTestType_IS_NOT_FALSE)
		}
		return boolTest(ast.BoolTestType_IS_FALSE)
	case ast.Token_UNKNOWN:
		if not {
			return boolTest(ast.BoolTestType_IS_NOT_UNKNOWN)
		}
		return boolTest(ast.BoolTestType_IS_UNKNOWN)
	case ast.Token_DISTINCT:
		// a_expr IS [NOT] DISTINCT FROM a_expr %prec IS
		p.next()
		p.expect(ast.Token_FROM)
		rhs := p.parseAExpr(precCmp)
		kind := ast.A_Expr_Kind_AEXPR_DISTINCT
		if not {
			kind = ast.A_Expr_Kind_AEXPR_NOT_DISTINCT
		}
		return nAExpr(makeSimpleA_Expr(kind, "=", lhs, rhs, tok.Start))
	case ast.Token_DOCUMENT_P:
		// a_expr IS [NOT] DOCUMENT_P %prec IS
		p.next()
		x := makeXmlExpr(ast.XmlExprOp_IS_DOCUMENT, "", nil, []*ast.Node{lhs}, tok.Start)
		if not {
			return makeNotExpr(x, tok.Start)
		}
		return x
	case ast.Token_NORMALIZED:
		p.next()
		fc := nFuncCall(makeFuncCall(SystemFuncName("is_normalized"),
			[]*ast.Node{lhs}, ast.CoercionForm_COERCE_SQL_SYNTAX, tok.Start))
		if not {
			return makeNotExpr(fc, tok.Start)
		}
		return fc
	case ast.Token_NFC, ast.Token_NFD, ast.Token_NFKC, ast.Token_NFKD:
		nf := p.next()
		p.expect(ast.Token_NORMALIZED)
		fc := nFuncCall(makeFuncCall(SystemFuncName("is_normalized"),
			[]*ast.Node{lhs, makeStringConst(unicodeNormalForm(nf), nf.Start)},
			ast.CoercionForm_COERCE_SQL_SYNTAX, tok.Start))
		if not {
			return makeNotExpr(fc, tok.Start)
		}
		return fc
	case ast.Token_JSON:
		// a_expr IS [NOT] json_predicate_type_constraint
		// json_key_uniqueness_constraint_opt %prec IS
		p.next()
		itemType := ast.JsonValueType_JS_TYPE_ANY
		switch p.peek().Kind {
		case ast.Token_VALUE_P:
			p.next()
		case ast.Token_ARRAY:
			p.next()
			itemType = ast.JsonValueType_JS_TYPE_ARRAY
		case ast.Token_OBJECT_P:
			p.next()
			itemType = ast.JsonValueType_JS_TYPE_OBJECT
		case ast.Token_SCALAR:
			p.next()
			itemType = ast.JsonValueType_JS_TYPE_SCALAR
		}
		unique := p.parseJsonKeyUniquenessOpt()
		format := makeJsonFormat(ast.JsonFormatType_JS_FORMAT_DEFAULT, ast.JsonEncoding_JS_ENC_DEFAULT, -1)
		pred := makeJsonIsPredicate(lhs, format, itemType, unique, exprStart.Start)
		if not {
			return makeNotExpr(pred, exprStart.Start)
		}
		return pred
	}
	p.syntaxErrorAt()
	return nil
}

// unicodeNormalForm is gram.y's unicode_normal_form values.
func unicodeNormalForm(tok lexer.Token) string {
	switch tok.Kind {
	case ast.Token_NFC:
		return "NFC"
	case ast.Token_NFD:
		return "NFD"
	case ast.Token_NFKC:
		return "NFKC"
	default:
		return "NFKD"
	}
}

// parseOverlapsRest finishes a_expr: row OVERLAPS row. The left row has been
// parsed into a RowExpr; both sides contribute their raw argument lists.
func (p *parser) parseOverlapsRest(lhs *ast.Node, lhsStart, opTok lexer.Token) *ast.Node {
	larg := lhs.GetRowExpr().Args
	if len(larg) != 2 {
		p.ereport("base_yyparse", "wrong number of parameters on left side of OVERLAPS expression", lhsStart.Start)
	}
	rstart := p.peek()
	rhs, isRow := p.parseCExprEx()
	if !isRow {
		p.syntaxError(rstart)
	}
	rarg := rhs.GetRowExpr().Args
	if len(rarg) != 2 {
		p.ereport("base_yyparse", "wrong number of parameters on right side of OVERLAPS expression", rstart.Start)
	}
	return nFuncCall(makeFuncCall(SystemFuncName("overlaps"),
		append(append([]*ast.Node{}, larg...), rarg...),
		ast.CoercionForm_COERCE_SQL_SYNTAX, opTok.Start))
}

// parseBExpr is gram.y's b_expr: the restricted ladder without AND, OR, NOT,
// the LIKE/BETWEEN/IN family, and most IS forms.
func (p *parser) parseBExpr(minPrec int) *ast.Node {
	start := p.peek()
	var lhs *ast.Node

	switch start.Kind {
	case ast.Token('+'):
		p.next()
		lhs = nAExpr(makeSimpleA_Expr(ast.A_Expr_Kind_AEXPR_OP, "+", nil, p.parseBExpr(precUminus), start.Start))
	case ast.Token('-'):
		p.next()
		lhs = doNegate(p.parseBExpr(precUminus), start.Start)
	case ast.Token_Op:
		p.next()
		lhs = nAExpr(makeA_Expr(ast.A_Expr_Kind_AEXPR_OP, []*ast.Node{nStr(start.Str)}, nil, p.parseBExpr(precOp+1), start.Start))
	case ast.Token_OPERATOR:
		name := p.parseQualOp()
		lhs = nAExpr(makeA_Expr(ast.A_Expr_Kind_AEXPR_OP, name, nil, p.parseBExpr(precOp+1), start.Start))
	default:
		lhs, _ = p.parseCExprEx()
	}

	lastLevel := 0
	for {
		tok := p.peek()
		switch tok.Kind {
		case ast.Token_TYPECAST:
			p.next()
			lhs = makeTypeCast(lhs, p.parseTypename(), tok.Start)
			continue
		case ast.Token('^'):
			if precExp < minPrec {
				return lhs
			}
			lhs = p.bMathOpInfix(lhs, precExp+1)
			continue
		case ast.Token('*'), ast.Token('/'), ast.Token('%'):
			if precMul < minPrec {
				return lhs
			}
			lhs = p.bMathOpInfix(lhs, precMul+1)
			continue
		case ast.Token('+'), ast.Token('-'):
			if precAdd < minPrec {
				return lhs
			}
			lhs = p.bMathOpInfix(lhs, precAdd+1)
			continue
		case ast.Token('<'), ast.Token('>'), ast.Token('='),
			ast.Token_LESS_EQUALS, ast.Token_GREATER_EQUALS, ast.Token_NOT_EQUALS:
			if precCmp < minPrec {
				return lhs
			}
			if lastLevel == precCmp {
				p.syntaxErrorAt()
			}
			lastLevel = precCmp
			lhs = p.bMathOpInfix(lhs, precCmp+1)
			continue
		case ast.Token_Op:
			if precOp < minPrec {
				return lhs
			}
			p.next()
			lhs = nAExpr(makeA_Expr(ast.A_Expr_Kind_AEXPR_OP, []*ast.Node{nStr(tok.Str)}, lhs, p.parseBExpr(precOp+1), tok.Start))
			continue
		case ast.Token_OPERATOR:
			if precOp < minPrec {
				return lhs
			}
			name := p.parseQualOp()
			lhs = nAExpr(makeA_Expr(ast.A_Expr_Kind_AEXPR_OP, name, lhs, p.parseBExpr(precOp+1), tok.Start))
			continue
		case ast.Token_IS:
			// b_expr IS [NOT] DISTINCT FROM b_expr %prec IS
			// b_expr IS [NOT] DOCUMENT_P %prec IS
			if precIs < minPrec {
				return lhs
			}
			if lastLevel == precIs {
				p.syntaxErrorAt()
			}
			next := p.kindN(1)
			notOff := 0
			if next == ast.Token_NOT {
				notOff = 1
				next = p.kindN(2)
			}
			if next != ast.Token_DISTINCT && next != ast.Token_DOCUMENT_P {
				// IS NULL etc. are not part of b_expr; leave IS unconsumed.
				return lhs
			}
			lastLevel = precIs
			p.next() // IS
			not := notOff == 1
			if not {
				p.next() // NOT
			}
			if p.have(ast.Token_DISTINCT) {
				p.expect(ast.Token_FROM)
				rhs := p.parseBExpr(precCmp)
				kind := ast.A_Expr_Kind_AEXPR_DISTINCT
				if not {
					kind = ast.A_Expr_Kind_AEXPR_NOT_DISTINCT
				}
				lhs = nAExpr(makeSimpleA_Expr(kind, "=", lhs, rhs, tok.Start))
			} else {
				p.expect(ast.Token_DOCUMENT_P)
				x := makeXmlExpr(ast.XmlExprOp_IS_DOCUMENT, "", nil, []*ast.Node{lhs}, tok.Start)
				if not {
					x = makeNotExpr(x, tok.Start)
				}
				lhs = x
			}
			continue
		default:
			return lhs
		}
	}
}

func (p *parser) bMathOpInfix(lhs *ast.Node, rhsPrec int) *ast.Node {
	tok := p.next()
	return nAExpr(makeSimpleA_Expr(ast.A_Expr_Kind_AEXPR_OP, mathOpName(tok), lhs, p.parseBExpr(rhsPrec), tok.Start))
}

// parseQualOp is gram.y's qual_Op: Op | OPERATOR '(' any_operator ')'. The
// current token must be Op or OPERATOR; consumes it.
func (p *parser) parseQualOp() []*ast.Node {
	tok := p.next()
	if tok.Kind == ast.Token_Op {
		return []*ast.Node{nStr(tok.Str)}
	}
	// OPERATOR '(' any_operator ')'
	p.expect(ast.Token('('))
	name := p.parseAnyOperator()
	p.expect(ast.Token(')'))
	return name
}

// parseAnyOperator is gram.y's any_operator: all_Op | ColId '.' any_operator.
func (p *parser) parseAnyOperator() []*ast.Node {
	var names []*ast.Node
	for isColIdToken(p.peek()) {
		names = append(names, nStr(p.next().Str))
		p.expect(ast.Token('.'))
	}
	op := p.parseAllOp()
	return append(names, nStr(op))
}

// parseAllOp is gram.y's all_Op: Op | MathOp.
func (p *parser) parseAllOp() string {
	tok := p.peek()
	switch tok.Kind {
	case ast.Token_Op:
		p.next()
		return tok.Str
	case ast.Token('+'), ast.Token('-'), ast.Token('*'), ast.Token('/'),
		ast.Token('%'), ast.Token('^'), ast.Token('<'), ast.Token('>'),
		ast.Token('='), ast.Token_LESS_EQUALS, ast.Token_GREATER_EQUALS,
		ast.Token_NOT_EQUALS:
		p.next()
		return mathOpName(tok)
	}
	p.syntaxErrorAt()
	return ""
}

// parseExprList is gram.y's expr_list.
func (p *parser) parseExprList() []*ast.Node {
	list := []*ast.Node{p.parseAExpr(0)}
	for p.have(ast.Token(',')) {
		list = append(list, p.parseAExpr(0))
	}
	return list
}
