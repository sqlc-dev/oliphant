package parse

import (
	"strings"

	"github.com/sqlc-dev/oliphant/ast"
	"github.com/sqlc-dev/oliphant/internal/lexer"
)

// parseFuncExprKeyword handles every c_expr alternative headed by a keyword
// with special function-like syntax (func_expr_common_subexpr, the SQL value
// functions, JSON aggregates) plus the ConstTypename-headed constants.
// Returns nil when the keyword doesn't commit here — the caller falls back
// to the generic name path (columnref / function call / const cast).
func (p *parser) parseFuncExprKeyword(tok lexer.Token) *ast.Node {
	paren := p.kindN(1) == ast.Token('(')
	svf := func(op ast.SQLValueFunctionOp, precN ast.SQLValueFunctionOp) *ast.Node {
		// CURRENT_TIME ['(' Iconst ')'] and friends
		p.next()
		typmod := int32(-1)
		op2 := op
		if precN != ast.SQLValueFunctionOp_SQLVALUE_FUNCTION_OP_UNDEFINED && p.have(ast.Token('(')) {
			typmod = p.iconst()
			p.expect(ast.Token(')'))
			op2 = precN
		}
		return makeSQLValueFunction(op2, typmod, tok.Start)
	}
	noParen := ast.SQLValueFunctionOp_SQLVALUE_FUNCTION_OP_UNDEFINED

	switch tok.Kind {
	case ast.Token_COLLATION:
		// func_expr_common_subexpr: COLLATION FOR '(' a_expr ')'
		if p.kindN(1) != ast.Token_FOR {
			return nil
		}
		p.next()
		p.next()
		p.expect(ast.Token('('))
		arg := p.parseAExpr(0)
		p.expect(ast.Token(')'))
		return nFuncCall(makeFuncCall(SystemFuncName("pg_collation_for"),
			[]*ast.Node{arg}, ast.CoercionForm_COERCE_SQL_SYNTAX, tok.Start))

	case ast.Token_CURRENT_DATE:
		return svf(ast.SQLValueFunctionOp_SVFOP_CURRENT_DATE, noParen)
	case ast.Token_CURRENT_TIME:
		return svf(ast.SQLValueFunctionOp_SVFOP_CURRENT_TIME, ast.SQLValueFunctionOp_SVFOP_CURRENT_TIME_N)
	case ast.Token_CURRENT_TIMESTAMP:
		return svf(ast.SQLValueFunctionOp_SVFOP_CURRENT_TIMESTAMP, ast.SQLValueFunctionOp_SVFOP_CURRENT_TIMESTAMP_N)
	case ast.Token_LOCALTIME:
		return svf(ast.SQLValueFunctionOp_SVFOP_LOCALTIME, ast.SQLValueFunctionOp_SVFOP_LOCALTIME_N)
	case ast.Token_LOCALTIMESTAMP:
		return svf(ast.SQLValueFunctionOp_SVFOP_LOCALTIMESTAMP, ast.SQLValueFunctionOp_SVFOP_LOCALTIMESTAMP_N)
	case ast.Token_CURRENT_ROLE:
		return svf(ast.SQLValueFunctionOp_SVFOP_CURRENT_ROLE, noParen)
	case ast.Token_CURRENT_USER:
		return svf(ast.SQLValueFunctionOp_SVFOP_CURRENT_USER, noParen)
	case ast.Token_SESSION_USER:
		return svf(ast.SQLValueFunctionOp_SVFOP_SESSION_USER, noParen)
	case ast.Token_USER:
		return svf(ast.SQLValueFunctionOp_SVFOP_USER, noParen)
	case ast.Token_CURRENT_CATALOG:
		return svf(ast.SQLValueFunctionOp_SVFOP_CURRENT_CATALOG, noParen)
	case ast.Token_CURRENT_SCHEMA:
		// CURRENT_SCHEMA is a type_func_name keyword (unlike the other SQL
		// value functions): current_schema() is a plain function call.
		if paren {
			return nil
		}
		return svf(ast.SQLValueFunctionOp_SVFOP_CURRENT_SCHEMA, noParen)
	case ast.Token_SYSTEM_USER:
		p.next()
		return nFuncCall(makeFuncCall(SystemFuncName("system_user"), nil,
			ast.CoercionForm_COERCE_SQL_SYNTAX, tok.Start))

	case ast.Token_CAST:
		// CAST '(' a_expr AS Typename ')'
		p.next()
		p.expect(ast.Token('('))
		arg := p.parseAExpr(0)
		p.expect(ast.Token_AS)
		t := p.parseTypename()
		p.expect(ast.Token(')'))
		return makeTypeCast(arg, t, tok.Start)

	case ast.Token_EXTRACT:
		// EXTRACT '(' extract_list ')'
		if !paren {
			return nil
		}
		p.next()
		p.next()
		args := p.parseExtractList()
		p.expect(ast.Token(')'))
		return nFuncCall(makeFuncCall(SystemFuncName("extract"), args,
			ast.CoercionForm_COERCE_SQL_SYNTAX, tok.Start))

	case ast.Token_NORMALIZE:
		// NORMALIZE '(' a_expr [',' unicode_normal_form] ')'
		if !paren {
			return nil
		}
		p.next()
		p.next()
		args := []*ast.Node{p.parseAExpr(0)}
		if p.have(ast.Token(',')) {
			nf := p.peek()
			switch nf.Kind {
			case ast.Token_NFC, ast.Token_NFD, ast.Token_NFKC, ast.Token_NFKD:
				p.next()
			default:
				p.syntaxErrorAt()
			}
			args = append(args, makeStringConst(unicodeNormalForm(nf), nf.Start))
		}
		p.expect(ast.Token(')'))
		return nFuncCall(makeFuncCall(SystemFuncName("normalize"), args,
			ast.CoercionForm_COERCE_SQL_SYNTAX, tok.Start))

	case ast.Token_OVERLAY:
		// OVERLAY '(' overlay_list ')' | OVERLAY '(' func_arg_list_opt ')'
		if !paren {
			return nil
		}
		p.next()
		p.next()
		return p.parseOverlayRest(tok)

	case ast.Token_POSITION:
		// POSITION '(' position_list ')'
		if !paren {
			return nil
		}
		p.next()
		p.next()
		var args []*ast.Node
		if p.kind() != ast.Token(')') {
			b1 := p.parseBExpr(0)
			p.expect(ast.Token_IN_P)
			b2 := p.parseBExpr(0)
			args = []*ast.Node{b2, b1}
		} else {
			// POSITION() with no args is not in the grammar; error at ')'.
			p.syntaxErrorAt()
		}
		p.expect(ast.Token(')'))
		return nFuncCall(makeFuncCall(SystemFuncName("position"), args,
			ast.CoercionForm_COERCE_SQL_SYNTAX, tok.Start))

	case ast.Token_SUBSTRING:
		if !paren {
			return nil
		}
		p.next()
		p.next()
		return p.parseSubstringRest(tok)

	case ast.Token_TREAT:
		// TREAT '(' a_expr AS Typename ')'
		if !paren {
			return nil
		}
		p.next()
		p.next()
		arg := p.parseAExpr(0)
		p.expect(ast.Token_AS)
		t := p.parseTypename()
		p.expect(ast.Token(')'))
		last := t.Names[len(t.Names)-1]
		return nFuncCall(makeFuncCall(SystemFuncName(asString(last).Sval),
			[]*ast.Node{arg}, ast.CoercionForm_COERCE_EXPLICIT_CALL, tok.Start))

	case ast.Token_TRIM:
		// TRIM '(' [BOTH|LEADING|TRAILING] trim_list ')'
		if !paren {
			return nil
		}
		p.next()
		p.next()
		name := "btrim"
		switch p.kind() {
		case ast.Token_BOTH:
			p.next()
		case ast.Token_LEADING:
			p.next()
			name = "ltrim"
		case ast.Token_TRAILING:
			p.next()
			name = "rtrim"
		}
		args := p.parseTrimList()
		p.expect(ast.Token(')'))
		return nFuncCall(makeFuncCall(SystemFuncName(name), args,
			ast.CoercionForm_COERCE_SQL_SYNTAX, tok.Start))

	case ast.Token_NULLIF:
		// NULLIF '(' a_expr ',' a_expr ')'
		if !paren {
			return nil
		}
		p.next()
		p.next()
		a := p.parseAExpr(0)
		p.expect(ast.Token(','))
		b := p.parseAExpr(0)
		p.expect(ast.Token(')'))
		return nAExpr(makeSimpleA_Expr(ast.A_Expr_Kind_AEXPR_NULLIF, "=", a, b, tok.Start))

	case ast.Token_COALESCE:
		if !paren {
			return nil
		}
		p.next()
		p.next()
		args := p.parseExprList()
		p.expect(ast.Token(')'))
		return nCoalesceExpr(&ast.CoalesceExpr{Args: args, Location: tok.Start})

	case ast.Token_GREATEST, ast.Token_LEAST:
		if !paren {
			return nil
		}
		p.next()
		p.next()
		args := p.parseExprList()
		p.expect(ast.Token(')'))
		op := ast.MinMaxOp_IS_GREATEST
		if tok.Kind == ast.Token_LEAST {
			op = ast.MinMaxOp_IS_LEAST
		}
		return nMinMaxExpr(&ast.MinMaxExpr{Op: op, Args: args, Location: tok.Start})

	case ast.Token_XMLCONCAT:
		if !paren {
			return nil
		}
		p.next()
		p.next()
		args := p.parseExprList()
		p.expect(ast.Token(')'))
		return makeXmlExpr(ast.XmlExprOp_IS_XMLCONCAT, "", nil, args, tok.Start)

	case ast.Token_XMLELEMENT:
		if !paren {
			return nil
		}
		p.next()
		p.next()
		return p.parseXmlElementRest(tok)

	case ast.Token_XMLEXISTS:
		// XMLEXISTS '(' c_expr xmlexists_argument ')'
		if !paren {
			return nil
		}
		p.next()
		p.next()
		c, _ := p.parseCExprEx()
		arg := p.parseXmlExistsArgument()
		p.expect(ast.Token(')'))
		return nFuncCall(makeFuncCall(SystemFuncName("xmlexists"),
			[]*ast.Node{c, arg}, ast.CoercionForm_COERCE_SQL_SYNTAX, tok.Start))

	case ast.Token_XMLFOREST:
		if !paren {
			return nil
		}
		p.next()
		p.next()
		args := p.parseXmlAttributeList()
		p.expect(ast.Token(')'))
		return makeXmlExpr(ast.XmlExprOp_IS_XMLFOREST, "", args, nil, tok.Start)

	case ast.Token_XMLPARSE:
		// XMLPARSE '(' document_or_content a_expr xml_whitespace_option ')'
		if !paren {
			return nil
		}
		p.next()
		p.next()
		opt := p.parseDocumentOrContent()
		arg := p.parseAExpr(0)
		ws := false
		if p.have(ast.Token_PRESERVE) {
			p.expect(ast.Token_WHITESPACE_P)
			ws = true
		} else if p.have(ast.Token_STRIP_P) {
			p.expect(ast.Token_WHITESPACE_P)
		}
		p.expect(ast.Token(')'))
		x := makeXmlExpr(ast.XmlExprOp_IS_XMLPARSE, "", nil,
			[]*ast.Node{arg, makeBoolAConst(ws, -1)}, tok.Start)
		x.GetXmlExpr().Xmloption = opt
		return x

	case ast.Token_XMLPI:
		// XMLPI '(' NAME_P ColLabel [',' a_expr] ')'
		if !paren {
			return nil
		}
		p.next()
		p.next()
		p.expect(ast.Token_NAME_P)
		name := p.colLabel()
		var args []*ast.Node
		if p.have(ast.Token(',')) {
			args = []*ast.Node{p.parseAExpr(0)}
		}
		p.expect(ast.Token(')'))
		return makeXmlExpr(ast.XmlExprOp_IS_XMLPI, name, nil, args, tok.Start)

	case ast.Token_XMLROOT:
		// XMLROOT '(' a_expr ',' xml_root_version opt_xml_root_standalone ')'
		if !paren {
			return nil
		}
		p.next()
		p.next()
		arg := p.parseAExpr(0)
		p.expect(ast.Token(','))
		p.expect(ast.Token_VERSION_P)
		var version *ast.Node
		if p.have(ast.Token_NO) {
			p.expect(ast.Token_VALUE_P)
			version = makeNullAConst(-1)
		} else {
			version = p.parseAExpr(0)
		}
		// opt_xml_root_standalone (XML_STANDALONE_* per xml.h)
		standalone := makeIntConst(3, -1) // XML_STANDALONE_OMITTED
		if p.have(ast.Token(',')) {
			p.expect(ast.Token_STANDALONE_P)
			switch {
			case p.have(ast.Token_YES_P):
				standalone = makeIntConst(0, -1) // XML_STANDALONE_YES
			case p.have(ast.Token_NO):
				if p.have(ast.Token_VALUE_P) {
					standalone = makeIntConst(2, -1) // XML_STANDALONE_NO_VALUE
				} else {
					standalone = makeIntConst(1, -1) // XML_STANDALONE_NO
				}
			default:
				p.syntaxErrorAt()
			}
		}
		p.expect(ast.Token(')'))
		return makeXmlExpr(ast.XmlExprOp_IS_XMLROOT, "", nil,
			[]*ast.Node{arg, version, standalone}, tok.Start)

	case ast.Token_XMLSERIALIZE:
		// XMLSERIALIZE '(' document_or_content a_expr AS SimpleTypename
		// xml_indent_option ')'
		if !paren {
			return nil
		}
		p.next()
		p.next()
		opt := p.parseDocumentOrContent()
		arg := p.parseAExpr(0)
		p.expect(ast.Token_AS)
		t := p.parseSimpleTypename()
		indent := false
		if p.have(ast.Token_INDENT) {
			indent = true
		} else if p.kind() == ast.Token_NO && p.kindN(1) == ast.Token_INDENT {
			p.next()
			p.next()
		}
		p.expect(ast.Token(')'))
		return nXmlSerialize(&ast.XmlSerialize{
			Xmloption: opt,
			Expr:      arg,
			TypeName:  t,
			Indent:    indent,
			Location:  tok.Start,
		})

	case ast.Token_MERGE_ACTION:
		// MERGE_ACTION '(' ')'
		if !paren {
			return nil
		}
		p.next()
		p.next()
		p.expect(ast.Token(')'))
		return &ast.Node{Node: &ast.Node_MergeSupportFunc{MergeSupportFunc: &ast.MergeSupportFunc{
			Msftype:  25, // TEXTOID
			Location: tok.Start,
		}}}

	case ast.Token_JSON_TABLE, ast.Token_XMLTABLE:
		// Table functions exist only in FROM; in an expression the '(' has
		// no production, so bison fails right there.
		if paren {
			p.next()
			p.syntaxErrorAt()
		}
		return nil

	case ast.Token_JSON_OBJECT:
		if !paren {
			return nil
		}
		p.next()
		p.next()
		return p.parseJsonObjectRest(tok)

	case ast.Token_JSON_ARRAY:
		if !paren {
			return nil
		}
		p.next()
		p.next()
		return p.parseJsonArrayRest(tok)

	case ast.Token_JSON:
		// JSON '(' json_value_expr json_key_uniqueness_constraint_opt ')'
		// (JsonType constants are handled by parseConstTypenameExpr.)
		if paren {
			p.next()
			p.next()
			expr := p.parseJsonValueExpr()
			unique := p.parseJsonKeyUniquenessOpt()
			p.expect(ast.Token(')'))
			return &ast.Node{Node: &ast.Node_JsonParseExpr{JsonParseExpr: &ast.JsonParseExpr{
				Expr:       expr.GetJsonValueExpr(),
				UniqueKeys: unique,
				Location:   tok.Start,
			}}}
		}
		return p.parseConstTypenameExpr()

	case ast.Token_JSON_SCALAR:
		if !paren {
			return nil
		}
		p.next()
		p.next()
		arg := p.parseAExpr(0)
		p.expect(ast.Token(')'))
		return &ast.Node{Node: &ast.Node_JsonScalarExpr{JsonScalarExpr: &ast.JsonScalarExpr{
			Expr:     arg,
			Location: tok.Start,
		}}}

	case ast.Token_JSON_SERIALIZE:
		if !paren {
			return nil
		}
		p.next()
		p.next()
		expr := p.parseJsonValueExpr()
		output := p.parseJsonReturningOpt()
		p.expect(ast.Token(')'))
		return &ast.Node{Node: &ast.Node_JsonSerializeExpr{JsonSerializeExpr: &ast.JsonSerializeExpr{
			Expr:     expr.GetJsonValueExpr(),
			Output:   output,
			Location: tok.Start,
		}}}

	case ast.Token_JSON_QUERY, ast.Token_JSON_EXISTS, ast.Token_JSON_VALUE:
		if !paren {
			return nil
		}
		p.next()
		p.next()
		return p.parseJsonFuncExprRest(tok)

	case ast.Token_JSON_OBJECTAGG, ast.Token_JSON_ARRAYAGG:
		// func_expr: json_aggregate_func filter_clause over_clause
		if !paren {
			return nil
		}
		agg := p.parseJsonAggregateFunc(tok)
		filter := p.parseFilterClause()
		over := p.parseOverClause()
		var ctor *ast.JsonAggConstructor
		if oa, ok := agg.Node.(*ast.Node_JsonObjectAgg); ok {
			ctor = oa.JsonObjectAgg.Constructor
		} else {
			ctor = agg.GetJsonArrayAgg().Constructor
		}
		ctor.AggFilter = filter
		ctor.Over = over
		return agg

	// ConstTypename-headed constants.
	case ast.Token_INT_P, ast.Token_INTEGER, ast.Token_SMALLINT, ast.Token_BIGINT,
		ast.Token_REAL, ast.Token_FLOAT_P, ast.Token_DOUBLE_P, ast.Token_DECIMAL_P,
		ast.Token_DEC, ast.Token_NUMERIC, ast.Token_BOOLEAN_P, ast.Token_BIT,
		ast.Token_CHARACTER, ast.Token_CHAR_P, ast.Token_VARCHAR, ast.Token_NCHAR,
		ast.Token_NATIONAL, ast.Token_TIMESTAMP, ast.Token_TIME, ast.Token_INTERVAL:
		return p.parseConstTypenameExpr()
	}
	return nil
}

// parseDocumentOrContent is gram.y's document_or_content.
func (p *parser) parseDocumentOrContent() ast.XmlOptionType {
	switch p.kind() {
	case ast.Token_DOCUMENT_P:
		p.next()
		return ast.XmlOptionType_XMLOPTION_DOCUMENT
	case ast.Token_CONTENT_P:
		p.next()
		return ast.XmlOptionType_XMLOPTION_CONTENT
	}
	p.syntaxErrorAt()
	return 0
}

// parseXmlElementRest parses XMLELEMENT's argument list after '('.
func (p *parser) parseXmlElementRest(tok lexer.Token) *ast.Node {
	p.expect(ast.Token_NAME_P)
	name := p.colLabel()
	var namedArgs, args []*ast.Node
	if p.have(ast.Token(',')) {
		if p.kind() == ast.Token_XMLATTRIBUTES {
			// xml_attributes: XMLATTRIBUTES '(' xml_attribute_list ')'
			p.next()
			p.expect(ast.Token('('))
			namedArgs = p.parseXmlAttributeList()
			p.expect(ast.Token(')'))
			if p.have(ast.Token(',')) {
				args = p.parseExprList()
			}
		} else {
			args = p.parseExprList()
		}
	}
	p.expect(ast.Token(')'))
	return makeXmlExpr(ast.XmlExprOp_IS_XMLELEMENT, name, namedArgs, args, tok.Start)
}

// parseXmlAttributeList is gram.y's xml_attribute_list.
func (p *parser) parseXmlAttributeList() []*ast.Node {
	list := []*ast.Node{p.parseXmlAttributeEl()}
	for p.have(ast.Token(',')) {
		list = append(list, p.parseXmlAttributeEl())
	}
	return list
}

// parseXmlAttributeEl is gram.y's xml_attribute_el: a_expr [AS ColLabel].
func (p *parser) parseXmlAttributeEl() *ast.Node {
	start := p.peek()
	val := p.parseAExpr(0)
	rt := &ast.ResTarget{Val: val, Location: start.Start}
	if p.have(ast.Token_AS) {
		rt.Name = p.colLabel()
	}
	return nResTarget(rt)
}

// parseXmlExistsArgument is gram.y's xmlexists_argument.
func (p *parser) parseXmlExistsArgument() *ast.Node {
	p.expect(ast.Token_PASSING)
	p.parseXmlPassingMech()
	c, _ := p.parseCExprEx()
	p.parseXmlPassingMech()
	return c
}

// parseXmlPassingMech is gram.y's xml_passing_mech: BY REF_P | BY VALUE_P.
func (p *parser) parseXmlPassingMech() {
	if p.kind() == ast.Token_BY {
		p.next()
		if !p.have(ast.Token_REF_P) {
			p.expect(ast.Token_VALUE_P)
		}
	}
}

// parseExtractList is gram.y's extract_list: extract_arg FROM a_expr.
func (p *parser) parseExtractList() []*ast.Node {
	tok := p.peek()
	var first *ast.Node
	switch tok.Kind {
	case ast.Token_IDENT:
		p.next()
		first = makeStringConst(tok.Str, tok.Start)
	case ast.Token_YEAR_P, ast.Token_MONTH_P, ast.Token_DAY_P,
		ast.Token_HOUR_P, ast.Token_MINUTE_P, ast.Token_SECOND_P:
		p.next()
		first = makeStringConst(tok.Str, tok.Start)
	case ast.Token_SCONST:
		p.next()
		first = makeStringConst(tok.Str, tok.Start)
	case ast.Token_PARAM:
		p.next()
		first = makeParamRef(tok.Ival, tok.Start)
	default:
		p.syntaxErrorAt()
	}
	p.expect(ast.Token_FROM)
	a := p.parseAExpr(0)
	return []*ast.Node{first, a}
}

// parseOverlayRest parses OVERLAY's two forms after the '(':
// overlay_list (PLACING syntax) vs func_arg_list_opt.
func (p *parser) parseOverlayRest(tok lexer.Token) *ast.Node {
	if p.kind() == ast.Token(')') {
		// OVERLAY '(' ')' — the func_arg_list_opt empty case
		p.next()
		return nFuncCall(makeFuncCall([]*ast.Node{nStr("overlay")}, nil,
			ast.CoercionForm_COERCE_EXPLICIT_CALL, tok.Start))
	}
	mark := p.mark()
	var args []*ast.Node
	err := p.try(func() {
		a := p.parseAExpr(0)
		if p.kind() != ast.Token_PLACING {
			p.syntaxErrorAt()
		}
		p.next()
		b := p.parseAExpr(0)
		p.expect(ast.Token_FROM)
		c := p.parseAExpr(0)
		args = []*ast.Node{a, b, c}
		if p.have(ast.Token_FOR) {
			args = append(args, p.parseAExpr(0))
		}
		p.expect(ast.Token(')'))
	})
	if err == nil {
		return nFuncCall(makeFuncCall(SystemFuncName("overlay"), args,
			ast.CoercionForm_COERCE_SQL_SYNTAX, tok.Start))
	}
	p.reset(mark)
	fargs := p.parseFuncArgList()
	p.expect(ast.Token(')'))
	return nFuncCall(makeFuncCall([]*ast.Node{nStr("overlay")}, fargs,
		ast.CoercionForm_COERCE_EXPLICIT_CALL, tok.Start))
}

// parseSubstringRest parses SUBSTRING's two forms after the '(':
// substr_list vs func_arg_list_opt.
func (p *parser) parseSubstringRest(tok lexer.Token) *ast.Node {
	if p.kind() == ast.Token(')') {
		p.next()
		return nFuncCall(makeFuncCall([]*ast.Node{nStr("substring")}, nil,
			ast.CoercionForm_COERCE_EXPLICIT_CALL, tok.Start))
	}
	mark := p.mark()
	var args []*ast.Node
	err := p.try(func() {
		args = p.parseSubstrList()
		p.expect(ast.Token(')'))
	})
	if err == nil {
		return nFuncCall(makeFuncCall(SystemFuncName("substring"), args,
			ast.CoercionForm_COERCE_SQL_SYNTAX, tok.Start))
	}
	p.reset(mark)
	fargs := p.parseFuncArgList()
	p.expect(ast.Token(')'))
	return nFuncCall(makeFuncCall([]*ast.Node{nStr("substring")}, fargs,
		ast.CoercionForm_COERCE_EXPLICIT_CALL, tok.Start))
}

// parseSubstrList is gram.y's substr_list.
func (p *parser) parseSubstrList() []*ast.Node {
	oldFlag := p.inSubstrList
	p.inSubstrList = true
	defer func() { p.inSubstrList = oldFlag }()

	a := p.parseAExpr(0)
	switch p.kind() {
	case ast.Token_FROM:
		p.next()
		b := p.parseAExpr(0)
		if p.have(ast.Token_FOR) {
			c := p.parseAExpr(0)
			return []*ast.Node{a, b, c}
		}
		return []*ast.Node{a, b}
	case ast.Token_FOR:
		p.next()
		b := p.parseAExpr(0)
		if p.have(ast.Token_FROM) {
			c := p.parseAExpr(0)
			return []*ast.Node{a, c, b}
		}
		// a_expr FOR a_expr: forcibly cast the length to int4
		return []*ast.Node{a, makeIntConst(1, -1),
			makeTypeCast(b, SystemTypeName("int4"), -1)}
	case ast.Token_SIMILAR:
		p.next()
		b := p.parseAExpr(0)
		p.expect(ast.Token_ESCAPE)
		c := p.parseAExpr(0)
		return []*ast.Node{a, b, c}
	}
	p.syntaxErrorAt()
	return nil
}

// parseTrimList is gram.y's trim_list.
func (p *parser) parseTrimList() []*ast.Node {
	if p.have(ast.Token_FROM) {
		return p.parseExprList()
	}
	a := p.parseAExpr(0)
	if p.have(ast.Token_FROM) {
		rest := p.parseExprList()
		return append(rest, a)
	}
	list := []*ast.Node{a}
	for p.have(ast.Token(',')) {
		list = append(list, p.parseAExpr(0))
	}
	return list
}

// parseFuncApplicationRest is gram.y's func_application with the func_name
// already parsed and '(' as the current token — plus the AexprConst
// "func_name '(' func_arg_list opt_sort_clause ')' Sconst/PARAM" forms,
// which can only be told apart after the closing paren, and the func_expr
// decorations (WITHIN GROUP, FILTER, OVER).
func (p *parser) parseFuncApplicationRest(name []*ast.Node, nameTok lexer.Token) *ast.Node {
	p.expect(ast.Token('('))
	fc := makeFuncCall(name, nil, ast.CoercionForm_COERCE_EXPLICIT_CALL, nameTok.Start)
	var sortTok lexer.Token
	switch {
	case p.have(ast.Token(')')):
		// func_name '(' ')'
	case p.have(ast.Token('*')):
		// func_name '(' '*' ')'
		p.expect(ast.Token(')'))
		fc.AggStar = true
	case p.kind() == ast.Token_ALL && !p.nextStartsNamedArg(1):
		p.next()
		fc.Args = p.parseFuncArgList()
		sortTok = p.peek()
		fc.AggOrder = p.parseOptSortClause()
		p.expect(ast.Token(')'))
	case p.kind() == ast.Token_DISTINCT:
		p.next()
		fc.Args = p.parseFuncArgList()
		sortTok = p.peek()
		fc.AggOrder = p.parseOptSortClause()
		p.expect(ast.Token(')'))
		fc.AggDistinct = true
	case p.kind() == ast.Token_VARIADIC:
		p.next()
		fc.Args = []*ast.Node{p.parseFuncArgExpr()}
		fc.FuncVariadic = true
		sortTok = p.peek()
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
		sortTok = p.peek()
		fc.AggOrder = p.parseOptSortClause()
		p.expect(ast.Token(')'))
	}

	// AexprConst: func_name '(' func_arg_list opt_sort_clause ')' Sconst —
	// only for the plain-args form (no star/distinct/variadic markers).
	if (p.kind() == ast.Token_SCONST || p.kind() == ast.Token_PARAM) &&
		!fc.AggStar && !fc.AggDistinct && !fc.FuncVariadic && len(fc.Args) > 0 {
		for _, arg := range fc.Args {
			if na, ok := arg.Node.(*ast.Node_NamedArgExpr); ok {
				p.ereport("base_yyparse", "type modifier cannot have parameter name", na.NamedArgExpr.Location)
			}
		}
		if fc.AggOrder != nil {
			p.ereport("base_yyparse", "type modifier cannot have ORDER BY", sortTok.Start)
		}
		t := makeTypeNameFromNameList(name)
		t.Typmods = fc.Args
		t.Location = nameTok.Start
		if p.kind() == ast.Token_SCONST {
			s := p.next()
			return makeStringConstCast(s.Str, s.Start, t)
		}
		prm := p.next()
		return makeParamRefCast(prm.Ival, prm.Start, t)
	}

	return p.parseFuncExprDecorations(fc)
}

// parseFuncExprDecorations is func_expr's within_group_clause filter_clause
// over_clause tail.
func (p *parser) parseFuncExprDecorations(fc *ast.FuncCall) *ast.Node {
	// within_group_clause: WITHIN GROUP_P '(' sort_clause ')'
	if p.kind() == ast.Token_WITHIN {
		wtok := p.peek()
		p.next()
		p.expect(ast.Token_GROUP_P)
		p.expect(ast.Token('('))
		order := p.parseSortClause()
		p.expect(ast.Token(')'))
		if fc.AggOrder != nil {
			p.ereport("base_yyparse", "cannot use multiple ORDER BY clauses with WITHIN GROUP", wtok.Start)
		}
		if fc.AggDistinct {
			p.ereport("base_yyparse", "cannot use DISTINCT with WITHIN GROUP", wtok.Start)
		}
		if fc.FuncVariadic {
			p.ereport("base_yyparse", "cannot use VARIADIC with WITHIN GROUP", wtok.Start)
		}
		fc.AggOrder = order
		fc.AggWithinGroup = true
	}
	fc.AggFilter = p.parseFilterClause()
	fc.Over = p.parseOverClause()
	return nFuncCall(fc)
}

// parseFilterClause is gram.y's filter_clause.
func (p *parser) parseFilterClause() *ast.Node {
	if p.kind() != ast.Token_FILTER || p.kindN(1) != ast.Token('(') {
		return nil
	}
	p.next()
	p.next()
	p.expect(ast.Token_WHERE)
	f := p.parseAExpr(0)
	p.expect(ast.Token(')'))
	return f
}

// nextStartsNamedArg reports whether the token at offset n starts a
// param_name COLON_EQUALS/EQUALS_GREATER named argument. (Used to keep ALL
// from being misread when it is a quoted identifier... it cannot be; ALL is
// reserved. Kept for symmetry.)
func (p *parser) nextStartsNamedArg(n int) bool {
	return false
}

// parseFuncArgList is gram.y's func_arg_list.
func (p *parser) parseFuncArgList() []*ast.Node {
	list := []*ast.Node{p.parseFuncArgExpr()}
	for p.have(ast.Token(',')) {
		list = append(list, p.parseFuncArgExpr())
	}
	return list
}

// parseFuncArgExpr is gram.y's func_arg_expr: a_expr | param_name
// COLON_EQUALS/EQUALS_GREATER a_expr.
func (p *parser) parseFuncArgExpr() *ast.Node {
	tok := p.peek()
	if isTypeFunctionNameToken(tok) {
		next := p.kindN(1)
		if next == ast.Token_COLON_EQUALS || next == ast.Token_EQUALS_GREATER {
			p.next()
			p.next()
			arg := p.parseAExpr(0)
			return nNamedArgExpr(&ast.NamedArgExpr{
				Name:      tok.Str,
				Arg:       arg,
				Argnumber: -1,
				Location:  tok.Start,
			})
		}
	}
	return p.parseAExpr(0)
}

// parseOverClause is gram.y's over_clause.
func (p *parser) parseOverClause() *ast.WindowDef {
	if !p.have(ast.Token_OVER) {
		return nil
	}
	if p.kind() == ast.Token('(') {
		return p.parseWindowSpecification()
	}
	tok := p.peek()
	name := p.colId()
	return &ast.WindowDef{
		Name:         name,
		FrameOptions: frameOptionDefaults,
		Location:     tok.Start,
	}
}

// Window frame option bits (parsenodes.h FRAMEOPTION_*).
const (
	frameOptionNondefault              = 0x00001
	frameOptionRange                   = 0x00002
	frameOptionRows                    = 0x00004
	frameOptionGroups                  = 0x00008
	frameOptionBetween                 = 0x00010
	frameOptionStartUnboundedPreceding = 0x00020
	frameOptionEndUnboundedPreceding   = 0x00040
	frameOptionStartUnboundedFollowing = 0x00080
	frameOptionEndUnboundedFollowing   = 0x00100
	frameOptionStartCurrentRow         = 0x00200
	frameOptionEndCurrentRow           = 0x00400
	frameOptionStartOffsetPreceding    = 0x00800
	frameOptionEndOffsetPreceding      = 0x01000
	frameOptionStartOffsetFollowing    = 0x02000
	frameOptionEndOffsetFollowing      = 0x04000
	frameOptionExcludeCurrentRow       = 0x08000
	frameOptionExcludeGroup            = 0x10000
	frameOptionExcludeTies             = 0x20000

	frameOptionDefaults = frameOptionRange |
		frameOptionStartUnboundedPreceding | frameOptionEndCurrentRow
)

// parseWindowSpecification is gram.y's window_specification.
func (p *parser) parseWindowSpecification() *ast.WindowDef {
	tok := p.expect(ast.Token('('))
	w := &ast.WindowDef{Location: tok.Start}

	// opt_existing_window_name: a ColId — but not one of the clause-starting
	// keywords, which have IDENT precedence so the empty production wins.
	if isColIdToken(p.peek()) {
		switch p.kind() {
		case ast.Token_PARTITION, ast.Token_RANGE, ast.Token_ROWS, ast.Token_GROUPS:
		default:
			w.Refname = p.colId()
		}
	}
	if p.kind() == ast.Token_PARTITION {
		p.next()
		p.expect(ast.Token_BY)
		w.PartitionClause = p.parseExprList()
	}
	w.OrderClause = p.parseOptSortClause()

	// opt_frame_clause
	frame := p.parseOptFrameClause()
	w.FrameOptions = frame.FrameOptions
	w.StartOffset = frame.StartOffset
	w.EndOffset = frame.EndOffset
	p.expect(ast.Token(')'))
	return w
}

// parseOptFrameClause is gram.y's opt_frame_clause; returns a WindowDef
// carrying only frameOptions/startOffset/endOffset.
func (p *parser) parseOptFrameClause() *ast.WindowDef {
	var mode int32
	switch p.kind() {
	case ast.Token_RANGE:
		mode = frameOptionRange
	case ast.Token_ROWS:
		mode = frameOptionRows
	case ast.Token_GROUPS:
		mode = frameOptionGroups
	default:
		return &ast.WindowDef{FrameOptions: frameOptionDefaults}
	}
	p.next()
	n := p.parseFrameExtent()
	n.FrameOptions |= frameOptionNondefault | mode
	// opt_window_exclusion_clause
	if p.have(ast.Token_EXCLUDE) {
		switch {
		case p.have(ast.Token_CURRENT_P):
			p.expect(ast.Token_ROW)
			n.FrameOptions |= frameOptionExcludeCurrentRow
		case p.have(ast.Token_GROUP_P):
			n.FrameOptions |= frameOptionExcludeGroup
		case p.have(ast.Token_TIES):
			n.FrameOptions |= frameOptionExcludeTies
		case p.have(ast.Token_NO):
			p.expect(ast.Token_OTHERS)
		default:
			p.syntaxErrorAt()
		}
	}
	return n
}

// parseFrameExtent is gram.y's frame_extent.
func (p *parser) parseFrameExtent() *ast.WindowDef {
	if p.kind() == ast.Token_BETWEEN {
		p.next()
		btok := p.peek()
		n1 := p.parseFrameBound()
		p.expect(ast.Token_AND)
		etok := p.peek()
		n2 := p.parseFrameBound()
		frameOptions := n1.FrameOptions
		// shift converts START_ options to END_ options
		frameOptions |= n2.FrameOptions << 1
		frameOptions |= frameOptionBetween
		if frameOptions&frameOptionStartUnboundedFollowing != 0 {
			p.ereport("base_yyparse", "frame start cannot be UNBOUNDED FOLLOWING", btok.Start)
		}
		if frameOptions&frameOptionEndUnboundedPreceding != 0 {
			p.ereport("base_yyparse", "frame end cannot be UNBOUNDED PRECEDING", etok.Start)
		}
		if frameOptions&frameOptionStartCurrentRow != 0 &&
			frameOptions&frameOptionEndOffsetPreceding != 0 {
			p.ereport("base_yyparse", "frame starting from current row cannot have preceding rows", etok.Start)
		}
		if frameOptions&frameOptionStartOffsetFollowing != 0 &&
			frameOptions&(frameOptionEndOffsetPreceding|frameOptionEndCurrentRow) != 0 {
			p.ereport("base_yyparse", "frame starting from following row cannot have preceding rows", etok.Start)
		}
		n1.FrameOptions = frameOptions
		n1.EndOffset = n2.StartOffset
		return n1
	}
	tok := p.peek()
	n := p.parseFrameBound()
	if n.FrameOptions&frameOptionStartUnboundedFollowing != 0 {
		p.ereport("base_yyparse", "frame start cannot be UNBOUNDED FOLLOWING", tok.Start)
	}
	if n.FrameOptions&frameOptionStartOffsetFollowing != 0 {
		p.ereport("base_yyparse", "frame starting from following row cannot end with current row", tok.Start)
	}
	n.FrameOptions |= frameOptionEndCurrentRow
	return n
}

// parseFrameBound is gram.y's frame_bound.
func (p *parser) parseFrameBound() *ast.WindowDef {
	switch p.kind() {
	case ast.Token_UNBOUNDED:
		// UNBOUNDED commits only when PRECEDING/FOLLOWING follows directly;
		// otherwise it's an ordinary identifier in the offset expression.
		switch p.kindN(1) {
		case ast.Token_PRECEDING:
			p.next()
			p.next()
			return &ast.WindowDef{FrameOptions: frameOptionStartUnboundedPreceding}
		case ast.Token_FOLLOWING:
			p.next()
			p.next()
			return &ast.WindowDef{FrameOptions: frameOptionStartUnboundedFollowing}
		}
	case ast.Token_CURRENT_P:
		if p.kindN(1) == ast.Token_ROW {
			p.next()
			p.next()
			return &ast.WindowDef{FrameOptions: frameOptionStartCurrentRow}
		}
	}
	offset := p.parseAExpr(0)
	switch {
	case p.have(ast.Token_PRECEDING):
		return &ast.WindowDef{FrameOptions: frameOptionStartOffsetPreceding, StartOffset: offset}
	case p.have(ast.Token_FOLLOWING):
		return &ast.WindowDef{FrameOptions: frameOptionStartOffsetFollowing, StartOffset: offset}
	}
	p.syntaxErrorAt()
	return nil
}

// --- SQL/JSON ---

// makefuncs.c: makeJsonFormat
func makeJsonFormat(t ast.JsonFormatType, encoding ast.JsonEncoding, location int32) *ast.JsonFormat {
	return &ast.JsonFormat{FormatType: t, Encoding: encoding, Location: location}
}

// makefuncs.c: makeJsonValueExpr
func makeJsonValueExpr(raw *ast.Node, formatted *ast.Node, format *ast.JsonFormat) *ast.Node {
	return &ast.Node{Node: &ast.Node_JsonValueExpr{JsonValueExpr: &ast.JsonValueExpr{
		RawExpr:       raw,
		FormattedExpr: formatted,
		Format:        format,
	}}}
}

// makefuncs.c: makeJsonBehavior
func makeJsonBehavior(btype ast.JsonBehaviorType, expr *ast.Node, location int32) *ast.JsonBehavior {
	return &ast.JsonBehavior{Btype: btype, Expr: expr, Location: location}
}

// makefuncs.c: makeJsonKeyValue
func makeJsonKeyValue(key, value *ast.Node) *ast.Node {
	return &ast.Node{Node: &ast.Node_JsonKeyValue{JsonKeyValue: &ast.JsonKeyValue{
		Key:   key,
		Value: value.GetJsonValueExpr(),
	}}}
}

// makefuncs.c: makeJsonIsPredicate
func makeJsonIsPredicate(expr *ast.Node, format *ast.JsonFormat, itemType ast.JsonValueType, uniqueKeys bool, location int32) *ast.Node {
	return &ast.Node{Node: &ast.Node_JsonIsPredicate{JsonIsPredicate: &ast.JsonIsPredicate{
		Expr:       expr,
		Format:     format,
		ItemType:   itemType,
		UniqueKeys: uniqueKeys,
		Location:   location,
	}}}
}

// parseJsonValueExpr is gram.y's json_value_expr: a_expr
// json_format_clause_opt.
func (p *parser) parseJsonValueExpr() *ast.Node {
	expr := p.parseAExpr(0)
	format := p.parseJsonFormatClauseOpt()
	return makeJsonValueExpr(expr, nil, format)
}

// parseJsonFormatClauseOpt is gram.y's json_format_clause_opt.
func (p *parser) parseJsonFormatClauseOpt() *ast.JsonFormat {
	if p.kind() != ast.Token_FORMAT_LA {
		return makeJsonFormat(ast.JsonFormatType_JS_FORMAT_DEFAULT, ast.JsonEncoding_JS_ENC_DEFAULT, -1)
	}
	return p.parseJsonFormatClause()
}

// parseJsonFormatClause is gram.y's json_format_clause: FORMAT_LA JSON
// [ENCODING name].
func (p *parser) parseJsonFormatClause() *ast.JsonFormat {
	tok := p.expect(ast.Token_FORMAT_LA)
	p.expect(ast.Token_JSON)
	if p.have(ast.Token_ENCODING) {
		name := p.name()
		var enc ast.JsonEncoding
		switch strings.ToLower(name) {
		case "utf8":
			enc = ast.JsonEncoding_JS_ENC_UTF8
		case "utf16":
			enc = ast.JsonEncoding_JS_ENC_UTF16
		case "utf32":
			enc = ast.JsonEncoding_JS_ENC_UTF32
		default:
			// gram.y: this ereport carries no parser_errposition.
			p.ereport("base_yyparse", "unrecognized JSON encoding: "+name, -1)
		}
		return makeJsonFormat(ast.JsonFormatType_JS_FORMAT_JSON, enc, tok.Start)
	}
	return makeJsonFormat(ast.JsonFormatType_JS_FORMAT_JSON, ast.JsonEncoding_JS_ENC_DEFAULT, tok.Start)
}

// parseJsonKeyUniquenessOpt is gram.y's json_key_uniqueness_constraint_opt.
func (p *parser) parseJsonKeyUniquenessOpt() bool {
	switch p.kind() {
	case ast.Token_WITH:
		if p.kindN(1) != ast.Token_UNIQUE {
			return false
		}
		p.next()
		p.next()
		p.have(ast.Token_KEYS)
		return true
	case ast.Token_WITHOUT:
		if p.kindN(1) != ast.Token_UNIQUE {
			return false
		}
		p.next()
		p.next()
		p.have(ast.Token_KEYS)
		return false
	}
	return false
}

// parseJsonReturningOpt is gram.y's json_returning_clause_opt.
func (p *parser) parseJsonReturningOpt() *ast.JsonOutput {
	if !p.have(ast.Token_RETURNING) {
		return nil
	}
	t := p.parseTypename()
	format := p.parseJsonFormatClauseOpt()
	return &ast.JsonOutput{
		TypeName:  t,
		Returning: &ast.JsonReturning{Format: format},
	}
}

// parseJsonObjectRest parses JSON_OBJECT's three forms after the '('.
func (p *parser) parseJsonObjectRest(tok lexer.Token) *ast.Node {
	// JSON_OBJECT '(' json_returning_clause_opt ')'
	if p.kind() == ast.Token(')') || p.kind() == ast.Token_RETURNING {
		output := p.parseJsonReturningOpt()
		p.expect(ast.Token(')'))
		return &ast.Node{Node: &ast.Node_JsonObjectConstructor{JsonObjectConstructor: &ast.JsonObjectConstructor{
			Output:   output,
			Location: tok.Start,
		}}}
	}

	// Distinguish json_name_and_value_list from legacy func_arg_list: try
	// the standard form first, mirroring the LALR states that keep both
	// alive until a ':' / VALUE_P or a ',' / ')' decides.
	mark := p.mark()
	var node *ast.Node
	err := p.try(func() {
		exprs := []*ast.Node{p.parseJsonNameAndValue()}
		for p.have(ast.Token(',')) {
			exprs = append(exprs, p.parseJsonNameAndValue())
		}
		absentOnNull := p.parseJsonObjectNullClauseOpt(false)
		unique := p.parseJsonKeyUniquenessOpt()
		output := p.parseJsonReturningOpt()
		p.expect(ast.Token(')'))
		node = &ast.Node{Node: &ast.Node_JsonObjectConstructor{JsonObjectConstructor: &ast.JsonObjectConstructor{
			Exprs:        exprs,
			AbsentOnNull: absentOnNull,
			Unique:       unique,
			Output:       output,
			Location:     tok.Start,
		}}}
	})
	if err == nil {
		return node
	}
	p.reset(mark)

	// Legacy json_object(func_arg_list)
	args := p.parseFuncArgList()
	p.expect(ast.Token(')'))
	return nFuncCall(makeFuncCall(SystemFuncName("json_object"), args,
		ast.CoercionForm_COERCE_EXPLICIT_CALL, tok.Start))
}

// parseJsonNameAndValue is gram.y's json_name_and_value.
func (p *parser) parseJsonNameAndValue() *ast.Node {
	key := p.parseAExpr(0)
	if !p.have(ast.Token(':')) {
		p.expect(ast.Token_VALUE_P)
	}
	val := p.parseJsonValueExpr()
	return makeJsonKeyValue(key, val)
}

// parseJsonObjectNullClauseOpt is json_object_constructor_null_clause_opt /
// json_array_constructor_null_clause_opt (they differ only in the default).
func (p *parser) parseJsonObjectNullClauseOpt(def bool) bool {
	switch p.kind() {
	case ast.Token_NULL_P:
		if p.kindN(1) == ast.Token_ON && p.kindN(2) == ast.Token_NULL_P {
			p.next()
			p.next()
			p.next()
			return false
		}
	case ast.Token_ABSENT:
		if p.kindN(1) == ast.Token_ON && p.kindN(2) == ast.Token_NULL_P {
			p.next()
			p.next()
			p.next()
			return true
		}
	}
	return def
}

// parseJsonArrayRest parses JSON_ARRAY's forms after the '('.
func (p *parser) parseJsonArrayRest(tok lexer.Token) *ast.Node {
	// JSON_ARRAY '(' json_returning_clause_opt ')'
	if p.kind() == ast.Token(')') || p.kind() == ast.Token_RETURNING {
		output := p.parseJsonReturningOpt()
		p.expect(ast.Token(')'))
		return &ast.Node{Node: &ast.Node_JsonArrayConstructor{JsonArrayConstructor: &ast.JsonArrayConstructor{
			AbsentOnNull: true,
			Output:       output,
			Location:     tok.Start,
		}}}
	}

	// JSON_ARRAY '(' select_no_parens ... ')'
	if p.looksLikeSelectAheadHereJson() {
		query := p.parseSelectNoParens()
		format := p.parseJsonFormatClauseOpt()
		output := p.parseJsonReturningOpt()
		p.expect(ast.Token(')'))
		return &ast.Node{Node: &ast.Node_JsonArrayQueryConstructor{JsonArrayQueryConstructor: &ast.JsonArrayQueryConstructor{
			Query:        query,
			Format:       format,
			AbsentOnNull: true,
			Output:       output,
			Location:     tok.Start,
		}}}
	}

	exprs := []*ast.Node{p.parseJsonValueExpr()}
	for p.have(ast.Token(',')) {
		exprs = append(exprs, p.parseJsonValueExpr())
	}
	absentOnNull := p.parseJsonObjectNullClauseOpt(true)
	output := p.parseJsonReturningOpt()
	p.expect(ast.Token(')'))
	return &ast.Node{Node: &ast.Node_JsonArrayConstructor{JsonArrayConstructor: &ast.JsonArrayConstructor{
		Exprs:        exprs,
		AbsentOnNull: absentOnNull,
		Output:       output,
		Location:     tok.Start,
	}}}
}

// looksLikeSelectAheadHereJson: JSON_ARRAY(select ...) takes a bare
// select_no_parens (no wrapping parens), so just check the current token.
func (p *parser) looksLikeSelectAheadHereJson() bool {
	switch p.kind() {
	case ast.Token_SELECT, ast.Token_VALUES, ast.Token_TABLE,
		ast.Token_WITH, ast.Token_WITH_LA:
		return true
	}
	return false
}

// parseJsonAggregateFunc is gram.y's json_aggregate_func; current token is
// JSON_OBJECTAGG/JSON_ARRAYAGG.
func (p *parser) parseJsonAggregateFunc(tok lexer.Token) *ast.Node {
	p.next()
	p.expect(ast.Token('('))
	if tok.Kind == ast.Token_JSON_OBJECTAGG {
		arg := p.parseJsonNameAndValue()
		absentOnNull := p.parseJsonObjectNullClauseOpt(false)
		unique := p.parseJsonKeyUniquenessOpt()
		output := p.parseJsonReturningOpt()
		p.expect(ast.Token(')'))
		return &ast.Node{Node: &ast.Node_JsonObjectAgg{JsonObjectAgg: &ast.JsonObjectAgg{
			Constructor: &ast.JsonAggConstructor{
				Output:   output,
				Location: tok.Start,
			},
			Arg:          arg.GetJsonKeyValue(),
			AbsentOnNull: absentOnNull,
			Unique:       unique,
		}}}
	}
	arg := p.parseJsonValueExpr()
	var aggOrder []*ast.Node
	if p.kind() == ast.Token_ORDER {
		aggOrder = p.parseSortClauseKeyword()
	}
	absentOnNull := p.parseJsonObjectNullClauseOpt(true)
	output := p.parseJsonReturningOpt()
	p.expect(ast.Token(')'))
	return &ast.Node{Node: &ast.Node_JsonArrayAgg{JsonArrayAgg: &ast.JsonArrayAgg{
		Constructor: &ast.JsonAggConstructor{
			Output:   output,
			AggOrder: aggOrder,
			Location: tok.Start,
		},
		Arg:          arg.GetJsonValueExpr(),
		AbsentOnNull: absentOnNull,
	}}}
}

// parseJsonFuncExprRest parses JSON_EXISTS/JSON_QUERY/JSON_VALUE after '('.
func (p *parser) parseJsonFuncExprRest(tok lexer.Token) *ast.Node {
	// Wrapper/quotes are always emitted; their C zero values are
	// JSW_UNSPEC / JS_QUOTES_UNSPEC.
	n := &ast.JsonFuncExpr{
		Wrapper:  ast.JsonWrapper_JSW_UNSPEC,
		Quotes:   ast.JsonQuotes_JS_QUOTES_UNSPEC,
		Location: tok.Start,
	}
	ctx := p.parseJsonValueExpr()
	n.ContextItem = ctx.GetJsonValueExpr()
	p.expect(ast.Token(','))
	n.Pathspec = p.parseAExpr(0)
	if p.have(ast.Token_PASSING) {
		n.Passing = []*ast.Node{p.parseJsonArgument()}
		for p.have(ast.Token(',')) {
			n.Passing = append(n.Passing, p.parseJsonArgument())
		}
	}
	switch tok.Kind {
	case ast.Token_JSON_EXISTS:
		n.Op = ast.JsonExprOp_JSON_EXISTS_OP
		// json_on_error_clause_opt
		if b, on := p.parseJsonBehaviorClauseOpt(); on != nil || b != nil {
			n.OnError = on
			if b != nil {
				// ON EMPTY is not accepted by JSON_EXISTS
				p.syntaxErrorAt()
			}
		}
	case ast.Token_JSON_QUERY:
		n.Op = ast.JsonExprOp_JSON_QUERY_OP
		n.Output = p.parseJsonReturningOpt()
		n.Wrapper = p.parseJsonWrapperBehavior()
		n.Quotes = p.parseJsonQuotesClauseOpt()
		n.OnEmpty, n.OnError = p.parseJsonBehaviorClauseOpt()
	case ast.Token_JSON_VALUE:
		n.Op = ast.JsonExprOp_JSON_VALUE_OP
		n.Output = p.parseJsonReturningOpt()
		n.OnEmpty, n.OnError = p.parseJsonBehaviorClauseOpt()
	}
	p.expect(ast.Token(')'))
	return &ast.Node{Node: &ast.Node_JsonFuncExpr{JsonFuncExpr: n}}
}

// parseJsonArgument is gram.y's json_argument.
func (p *parser) parseJsonArgument() *ast.Node {
	val := p.parseJsonValueExpr()
	p.expect(ast.Token_AS)
	name := p.colLabel()
	return &ast.Node{Node: &ast.Node_JsonArgument{JsonArgument: &ast.JsonArgument{
		Val:  val.GetJsonValueExpr(),
		Name: name,
	}}}
}

// parseJsonWrapperBehavior is gram.y's json_wrapper_behavior.
func (p *parser) parseJsonWrapperBehavior() ast.JsonWrapper {
	switch p.kind() {
	case ast.Token_WITHOUT:
		if p.kindN(1) == ast.Token_WRAPPER {
			p.next()
			p.next()
			return ast.JsonWrapper_JSW_NONE
		}
		if p.kindN(1) == ast.Token_ARRAY && p.kindN(2) == ast.Token_WRAPPER {
			p.next()
			p.next()
			p.next()
			return ast.JsonWrapper_JSW_NONE
		}
	case ast.Token_WITH:
		switch p.kindN(1) {
		case ast.Token_WRAPPER:
			p.next()
			p.next()
			return ast.JsonWrapper_JSW_UNCONDITIONAL
		case ast.Token_ARRAY:
			if p.kindN(2) == ast.Token_WRAPPER {
				p.next()
				p.next()
				p.next()
				return ast.JsonWrapper_JSW_UNCONDITIONAL
			}
		case ast.Token_CONDITIONAL, ast.Token_UNCONDITIONAL:
			cond := p.kindN(1) == ast.Token_CONDITIONAL
			i := 2
			if p.kindN(2) == ast.Token_ARRAY {
				i = 3
			}
			if p.kindN(i) == ast.Token_WRAPPER {
				for j := 0; j <= i; j++ {
					p.next()
				}
				if cond {
					return ast.JsonWrapper_JSW_CONDITIONAL
				}
				return ast.JsonWrapper_JSW_UNCONDITIONAL
			}
		}
	}
	return ast.JsonWrapper_JSW_UNSPEC
}

// parseJsonQuotesClauseOpt is gram.y's json_quotes_clause_opt.
func (p *parser) parseJsonQuotesClauseOpt() ast.JsonQuotes {
	var q ast.JsonQuotes
	switch p.kind() {
	case ast.Token_KEEP:
		q = ast.JsonQuotes_JS_QUOTES_KEEP
	case ast.Token_OMIT:
		q = ast.JsonQuotes_JS_QUOTES_OMIT
	default:
		return ast.JsonQuotes_JS_QUOTES_UNSPEC
	}
	if p.kindN(1) != ast.Token_QUOTES {
		return ast.JsonQuotes_JS_QUOTES_UNSPEC
	}
	p.next()
	p.next()
	if p.kind() == ast.Token_ON && p.kindN(1) == ast.Token_SCALAR && p.kindN(2) == ast.Token_STRING_P {
		p.next()
		p.next()
		p.next()
	}
	return q
}

// parseJsonBehaviorClauseOpt is gram.y's json_behavior_clause_opt: returns
// (onEmpty, onError).
func (p *parser) parseJsonBehaviorClauseOpt() (*ast.JsonBehavior, *ast.JsonBehavior) {
	b1 := p.parseJsonBehaviorOpt()
	if b1 == nil {
		return nil, nil
	}
	p.expect(ast.Token_ON)
	if p.have(ast.Token_ERROR_P) {
		return nil, b1
	}
	p.expect(ast.Token_EMPTY_P)
	b2 := p.parseJsonBehaviorOpt()
	if b2 == nil {
		return b1, nil
	}
	p.expect(ast.Token_ON)
	p.expect(ast.Token_ERROR_P)
	return b1, b2
}

// parseJsonBehaviorOpt parses json_behavior if the current token starts one.
func (p *parser) parseJsonBehaviorOpt() *ast.JsonBehavior {
	tok := p.peek()
	switch tok.Kind {
	case ast.Token_DEFAULT:
		p.next()
		expr := p.parseAExpr(0)
		return makeJsonBehavior(ast.JsonBehaviorType_JSON_BEHAVIOR_DEFAULT, expr, tok.Start)
	case ast.Token_ERROR_P:
		p.next()
		return makeJsonBehavior(ast.JsonBehaviorType_JSON_BEHAVIOR_ERROR, nil, tok.Start)
	case ast.Token_NULL_P:
		p.next()
		return makeJsonBehavior(ast.JsonBehaviorType_JSON_BEHAVIOR_NULL, nil, tok.Start)
	case ast.Token_TRUE_P:
		p.next()
		return makeJsonBehavior(ast.JsonBehaviorType_JSON_BEHAVIOR_TRUE, nil, tok.Start)
	case ast.Token_FALSE_P:
		p.next()
		return makeJsonBehavior(ast.JsonBehaviorType_JSON_BEHAVIOR_FALSE, nil, tok.Start)
	case ast.Token_UNKNOWN:
		p.next()
		return makeJsonBehavior(ast.JsonBehaviorType_JSON_BEHAVIOR_UNKNOWN, nil, tok.Start)
	case ast.Token_EMPTY_P:
		p.next()
		switch {
		case p.have(ast.Token_ARRAY):
			return makeJsonBehavior(ast.JsonBehaviorType_JSON_BEHAVIOR_EMPTY_ARRAY, nil, tok.Start)
		case p.have(ast.Token_OBJECT_P):
			return makeJsonBehavior(ast.JsonBehaviorType_JSON_BEHAVIOR_EMPTY_OBJECT, nil, tok.Start)
		}
		return makeJsonBehavior(ast.JsonBehaviorType_JSON_BEHAVIOR_EMPTY_ARRAY, nil, tok.Start)
	}
	return nil
}
