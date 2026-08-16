package parse

import (
	"github.com/sqlc-dev/oliphant/ast"
)

// makefuncs.c: makeDefElem
func makeDefElem(name string, arg *ast.Node, location int32) *ast.Node {
	return &ast.Node{Node: &ast.Node_DefElem{DefElem: &ast.DefElem{
		Defname:   name,
		Arg:       arg,
		Defaction: ast.DefElemAction_DEFELEM_UNSPEC,
		Location:  location,
	}}}
}

// makefuncs.c: makeJsonTablePathSpec
func makeJsonTablePathSpec(pathstring, name string, stringLocation, nameLocation int32) *ast.JsonTablePathSpec {
	return &ast.JsonTablePathSpec{
		String_:      makeStringConst(pathstring, stringLocation),
		Name:         name,
		NameLocation: nameLocation,
		Location:     stringLocation,
	}
}

// parseXmlTable is gram.y's xmltable; current token is XMLTABLE, '(' next.
func (p *parser) parseXmlTable() *ast.Node {
	tok := p.next() // XMLTABLE
	p.expect(ast.Token('('))
	n := &ast.RangeTableFunc{Location: tok.Start}

	if p.kind() == ast.Token_XMLNAMESPACES {
		p.next()
		p.expect(ast.Token('('))
		n.Namespaces = []*ast.Node{p.parseXmlNamespaceEl()}
		for p.have(ast.Token(',')) {
			n.Namespaces = append(n.Namespaces, p.parseXmlNamespaceEl())
		}
		p.expect(ast.Token(')'))
		p.expect(ast.Token(','))
	}
	rowexpr, _ := p.parseCExprEx()
	n.Rowexpr = rowexpr
	n.Docexpr = p.parseXmlExistsArgument()
	p.expect(ast.Token_COLUMNS)
	n.Columns = []*ast.Node{p.parseXmlTableColumnEl()}
	for p.have(ast.Token(',')) {
		n.Columns = append(n.Columns, p.parseXmlTableColumnEl())
	}
	p.expect(ast.Token(')'))
	return &ast.Node{Node: &ast.Node_RangeTableFunc{RangeTableFunc: n}}
}

// parseXmlNamespaceEl is gram.y's xml_namespace_el.
func (p *parser) parseXmlNamespaceEl() *ast.Node {
	tok := p.peek()
	if p.have(ast.Token_DEFAULT) {
		val := p.parseBExpr(0)
		return nResTarget(&ast.ResTarget{Val: val, Location: tok.Start})
	}
	val := p.parseBExpr(0)
	p.expect(ast.Token_AS)
	name := p.colLabel()
	return nResTarget(&ast.ResTarget{Name: name, Val: val, Location: tok.Start})
}

// parseXmlTableColumnEl is gram.y's xmltable_column_el.
func (p *parser) parseXmlTableColumnEl() *ast.Node {
	tok := p.peek()
	name := p.colId()
	fc := &ast.RangeTableFuncCol{Colname: name, Location: tok.Start}
	if p.kind() == ast.Token_FOR {
		p.next()
		p.expect(ast.Token_ORDINALITY)
		fc.ForOrdinality = true
		return &ast.Node{Node: &ast.Node_RangeTableFuncCol{RangeTableFuncCol: fc}}
	}
	fc.TypeName = p.parseTypename()

	nullabilitySeen := false
	for {
		otok := p.peek()
		var defname string
		var arg *ast.Node
		switch otok.Kind {
		case ast.Token_IDENT:
			p.next()
			if otok.Str == "__pg__is_not_null" {
				p.ereport("base_yyparse",
					`option name "`+otok.Str+`" cannot be used in XMLTABLE`, otok.Start)
			}
			defname = otok.Str
			arg = p.parseBExpr(0)
		case ast.Token_DEFAULT:
			p.next()
			defname = "default"
			arg = p.parseBExpr(0)
		case ast.Token_NOT:
			p.next()
			p.expect(ast.Token_NULL_P)
			defname = "__pg__is_not_null"
			arg = &ast.Node{Node: &ast.Node_Boolean{Boolean: &ast.Boolean{Boolval: true}}}
		case ast.Token_NULL_P:
			p.next()
			defname = "__pg__is_not_null"
			arg = &ast.Node{Node: &ast.Node_Boolean{Boolean: &ast.Boolean{Boolval: false}}}
		case ast.Token_PATH:
			p.next()
			defname = "path"
			arg = p.parseBExpr(0)
		default:
			return &ast.Node{Node: &ast.Node_RangeTableFuncCol{RangeTableFuncCol: fc}}
		}
		// gram.y: xmltable_column_el's option-merging loop
		switch defname {
		case "default":
			if fc.Coldefexpr != nil {
				p.ereport("base_yyparse", "only one DEFAULT value is allowed", otok.Start)
			}
			fc.Coldefexpr = arg
		case "path":
			if fc.Colexpr != nil {
				p.ereport("base_yyparse", "only one PATH value per column is allowed", otok.Start)
			}
			fc.Colexpr = arg
		case "__pg__is_not_null":
			if nullabilitySeen {
				p.ereport("base_yyparse",
					`conflicting or redundant NULL / NOT NULL declarations for column "`+fc.Colname+`"`, otok.Start)
			}
			fc.IsNotNull = arg.GetBoolean().Boolval
			nullabilitySeen = true
		default:
			p.ereport("base_yyparse",
				`unrecognized column option "`+defname+`"`, otok.Start)
		}
	}
}

// parseJsonTable is gram.y's json_table; current token is JSON_TABLE.
func (p *parser) parseJsonTable() *ast.Node {
	tok := p.next() // JSON_TABLE
	p.expect(ast.Token('('))
	n := &ast.JsonTable{Location: tok.Start}
	ctx := p.parseJsonValueExpr()
	n.ContextItem = ctx.GetJsonValueExpr()
	p.expect(ast.Token(','))
	ptok := p.peek()
	path := p.parseAExpr(0)
	pconst := asAConst(path)
	var pathstring string
	if pconst == nil || pconst.GetSval() == nil {
		p.ereport("base_yyparse",
			"only string constants are supported in JSON_TABLE path specification", ptok.Start)
	} else {
		pathstring = pconst.GetSval().Sval
	}
	// json_table_path_name_opt — @6 is the production's start, i.e. the AS
	// token's location.
	name := ""
	nameLoc := int32(-1)
	if p.kind() == ast.Token_AS {
		nameLoc = p.next().Start
		name = p.name()
	}
	n.Pathspec = makeJsonTablePathSpec(pathstring, name, ptok.Start, nameLoc)
	if p.have(ast.Token_PASSING) {
		n.Passing = []*ast.Node{p.parseJsonArgument()}
		for p.have(ast.Token(',')) {
			n.Passing = append(n.Passing, p.parseJsonArgument())
		}
	}
	p.expect(ast.Token_COLUMNS)
	p.expect(ast.Token('('))
	n.Columns = p.parseJsonTableColumnDefinitionList()
	p.expect(ast.Token(')'))
	// json_on_error_clause_opt
	if b := p.parseJsonBehaviorOpt(); b != nil {
		p.expect(ast.Token_ON)
		p.expect(ast.Token_ERROR_P)
		n.OnError = b
	}
	p.expect(ast.Token(')'))
	return &ast.Node{Node: &ast.Node_JsonTable{JsonTable: n}}
}

// parseJsonTableColumnDefinitionList is json_table_column_definition_list.
func (p *parser) parseJsonTableColumnDefinitionList() []*ast.Node {
	list := []*ast.Node{p.parseJsonTableColumnDefinition()}
	for p.have(ast.Token(',')) {
		list = append(list, p.parseJsonTableColumnDefinition())
	}
	return list
}

// parseJsonTableColumnDefinition is gram.y's json_table_column_definition.
func (p *parser) parseJsonTableColumnDefinition() *ast.Node {
	tok := p.peek()

	if tok.Kind == ast.Token_NESTED {
		// NESTED path_opt Sconst [AS name] COLUMNS '(' ... ')'
		p.next()
		p.have(ast.Token_PATH)
		stok := p.peek()
		pathstring := p.sconst()
		name := ""
		nameLoc := int32(-1)
		if p.have(ast.Token_AS) {
			ntok := p.peek()
			name = p.name()
			nameLoc = ntok.Start
		}
		p.expect(ast.Token_COLUMNS)
		p.expect(ast.Token('('))
		cols := p.parseJsonTableColumnDefinitionList()
		p.expect(ast.Token(')'))
		return jsonTableColumnNode(&ast.JsonTableColumn{
			Coltype:  ast.JsonTableColumnType_JTC_NESTED,
			Pathspec: makeJsonTablePathSpec(pathstring, name, stok.Start, nameLoc),
			Columns:  cols,
			Wrapper:  ast.JsonWrapper_JSW_UNSPEC,
			Quotes:   ast.JsonQuotes_JS_QUOTES_UNSPEC,
			Location: tok.Start,
		})
	}

	name := p.colId()
	if p.kind() == ast.Token_FOR {
		p.next()
		p.expect(ast.Token_ORDINALITY)
		return jsonTableColumnNode(&ast.JsonTableColumn{
			Coltype:  ast.JsonTableColumnType_JTC_FOR_ORDINALITY,
			Name:     name,
			Wrapper:  ast.JsonWrapper_JSW_UNSPEC,
			Quotes:   ast.JsonQuotes_JS_QUOTES_UNSPEC,
			Location: tok.Start,
		})
	}

	typeName := p.parseTypename()
	n := &ast.JsonTableColumn{
		Name:     name,
		TypeName: typeName,
		Location: tok.Start,
	}
	switch {
	case p.kind() == ast.Token_EXISTS:
		p.next()
		n.Coltype = ast.JsonTableColumnType_JTC_EXISTS
		n.Format = makeJsonFormat(ast.JsonFormatType_JS_FORMAT_DEFAULT, ast.JsonEncoding_JS_ENC_DEFAULT, -1)
		n.Wrapper = ast.JsonWrapper_JSW_NONE
		n.Quotes = ast.JsonQuotes_JS_QUOTES_UNSPEC
		n.Pathspec = p.parseJsonTableColumnPathOpt()
		if b := p.parseJsonBehaviorOpt(); b != nil {
			p.expect(ast.Token_ON)
			p.expect(ast.Token_ERROR_P)
			n.OnError = b
		}
	case p.kind() == ast.Token_FORMAT_LA:
		n.Coltype = ast.JsonTableColumnType_JTC_FORMATTED
		n.Format = p.parseJsonFormatClause()
		n.Pathspec = p.parseJsonTableColumnPathOpt()
		n.Wrapper = p.parseJsonWrapperBehavior()
		n.Quotes = p.parseJsonQuotesClauseOpt()
		n.OnEmpty, n.OnError = p.parseJsonBehaviorClauseOpt()
	default:
		n.Coltype = ast.JsonTableColumnType_JTC_REGULAR
		n.Format = makeJsonFormat(ast.JsonFormatType_JS_FORMAT_DEFAULT, ast.JsonEncoding_JS_ENC_DEFAULT, -1)
		n.Pathspec = p.parseJsonTableColumnPathOpt()
		n.Wrapper = p.parseJsonWrapperBehavior()
		n.Quotes = p.parseJsonQuotesClauseOpt()
		n.OnEmpty, n.OnError = p.parseJsonBehaviorClauseOpt()
	}
	return jsonTableColumnNode(n)
}

func jsonTableColumnNode(c *ast.JsonTableColumn) *ast.Node {
	return &ast.Node{Node: &ast.Node_JsonTableColumn{JsonTableColumn: c}}
}

// parseJsonTableColumnPathOpt is json_table_column_path_clause_opt.
func (p *parser) parseJsonTableColumnPathOpt() *ast.JsonTablePathSpec {
	if p.kind() != ast.Token_PATH {
		return nil
	}
	p.next()
	stok := p.peek()
	s := p.sconst()
	return makeJsonTablePathSpec(s, "", stok.Start, -1)
}
