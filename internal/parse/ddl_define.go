package parse

// DefineStmt (CREATE AGGREGATE/OPERATOR/TYPE/TEXT SEARCH .../COLLATION),
// CompositeTypeStmt, CreateEnumStmt, CreateRangeStmt, CreateStatsStmt,
// and the operator class family.

import (
	"github.com/sqlc-dev/oliphant/ast"
)

func nDefineStmt(n *ast.DefineStmt) *ast.Node {
	return &ast.Node{Node: &ast.Node_DefineStmt{DefineStmt: n}}
}

// parseCreateAggregateStmt is gram.y's DefineStmt AGGREGATE arms; CREATE
// [OR REPLACE] AGGREGATE consumed.
func (p *parser) parseCreateAggregateStmt(replace bool) *ast.Node {
	n := &ast.DefineStmt{
		Kind:     ast.ObjectType_OBJECT_AGGREGATE,
		Replace:  replace,
		Defnames: p.parseFuncName(p.peek()),
	}
	// New style has aggr_args then definition; old style has '(' IDENT '='
	// ... ')' directly. Disambiguate on the token after '(': old_aggr_elem
	// requires IDENT '='.
	if p.kindN(1) == ast.Token_IDENT && p.kindN(2) == ast.Token('=') {
		n.Oldstyle = true
		p.expect(ast.Token('('))
		for {
			etok := p.peek()
			name := p.expect(ast.Token_IDENT).Str
			p.expect(ast.Token('='))
			n.Definition = append(n.Definition, makeDefElem(name, p.parseDefArg(), etok.Start))
			if !p.have(ast.Token(',')) {
				break
			}
		}
		p.expect(ast.Token(')'))
		return nDefineStmt(n)
	}
	params, ndirect := p.parseAggrArgs()
	// The zero-argument '(*)' form stores NIL — a null list element — not
	// an empty List node.
	argsNode := &ast.Node{}
	if params != nil {
		argsNode = nList(params)
	}
	n.Args = []*ast.Node{argsNode, nInteger(ndirect)}
	n.Definition = p.parseDefinition()
	return nDefineStmt(n)
}

// parseCreateOperatorStmt handles CREATE OPERATOR: the DefineStmt arm plus
// the CLASS/FAMILY statements; CREATE OPERATOR consumed.
func (p *parser) parseCreateOperatorStmt() *ast.Node {
	switch p.kind() {
	case ast.Token_CLASS:
		// gram.y: CreateOpClassStmt
		p.next()
		n := &ast.CreateOpClassStmt{Opclassname: p.anyName()}
		n.IsDefault = p.have(ast.Token_DEFAULT)
		p.expect(ast.Token_FOR)
		p.expect(ast.Token_TYPE_P)
		n.Datatype = p.parseTypename()
		p.expect(ast.Token_USING)
		n.Amname = p.name()
		if p.have(ast.Token_FAMILY) {
			n.Opfamilyname = p.anyName()
		}
		p.expect(ast.Token_AS)
		n.Items = p.parseOpclassItemList()
		return &ast.Node{Node: &ast.Node_CreateOpClassStmt{CreateOpClassStmt: n}}
	case ast.Token_FAMILY:
		// gram.y: CreateOpFamilyStmt
		p.next()
		n := &ast.CreateOpFamilyStmt{Opfamilyname: p.anyName()}
		p.expect(ast.Token_USING)
		n.Amname = p.name()
		return &ast.Node{Node: &ast.Node_CreateOpFamilyStmt{CreateOpFamilyStmt: n}}
	}
	// gram.y: DefineStmt: CREATE OPERATOR any_operator definition
	n := &ast.DefineStmt{
		Kind:       ast.ObjectType_OBJECT_OPERATOR,
		Defnames:   p.parseAnyOperator(),
		Definition: p.parseDefinition(),
	}
	return nDefineStmt(n)
}

// parseCreateTypeStmt is gram.y's DefineStmt TYPE_P arms; CREATE TYPE_P
// consumed.
func (p *parser) parseCreateTypeStmt() *ast.Node {
	ntok := p.peek()
	names := p.anyName()
	switch p.kind() {
	case ast.Token('('):
		n := &ast.DefineStmt{
			Kind:       ast.ObjectType_OBJECT_TYPE,
			Defnames:   names,
			Definition: p.parseDefinition(),
		}
		return nDefineStmt(n)
	case ast.Token_AS:
		p.next()
		switch p.kind() {
		case ast.Token_ENUM_P:
			p.next()
			n := &ast.CreateEnumStmt{TypeName: names}
			p.expect(ast.Token('('))
			if p.kind() != ast.Token(')') {
				n.Vals = append(n.Vals, nStr(p.sconst()))
				for p.have(ast.Token(',')) {
					n.Vals = append(n.Vals, nStr(p.sconst()))
				}
			}
			p.expect(ast.Token(')'))
			return &ast.Node{Node: &ast.Node_CreateEnumStmt{CreateEnumStmt: n}}
		case ast.Token_RANGE:
			p.next()
			n := &ast.CreateRangeStmt{TypeName: names, Params: p.parseDefinition()}
			return &ast.Node{Node: &ast.Node_CreateRangeStmt{CreateRangeStmt: n}}
		}
		// gram.y: CompositeTypeStmt
		return p.parseCompositeTypeStmt(names, ntok.Start)
	}
	// Shell type.
	return nDefineStmt(&ast.DefineStmt{
		Kind:     ast.ObjectType_OBJECT_TYPE,
		Defnames: names,
	})
}

// parseCompositeTypeStmt finishes CREATE TYPE any_name AS '(' ... ')'; the
// caller consumed AS and passes the any_name with its start location (@3).
func (p *parser) parseCompositeTypeStmt(names []*ast.Node, nameLoc int32) *ast.Node {
	n := &ast.CompositeTypeStmt{}
	n.Typevar = p.makeRangeVarFromAnyName(names, nameLoc)
	p.expect(ast.Token('('))
	if p.kind() != ast.Token(')') {
		n.Coldeflist = p.parseTableFuncElementList()
	}
	p.expect(ast.Token(')'))
	return &ast.Node{Node: &ast.Node_CompositeTypeStmt{CompositeTypeStmt: n}}
}

// parseOpclassItemList is gram.y's opclass_item_list.
func (p *parser) parseOpclassItemList() []*ast.Node {
	list := []*ast.Node{p.parseOpclassItem()}
	for p.have(ast.Token(',')) {
		list = append(list, p.parseOpclassItem())
	}
	return list
}

// OPCLASS_ITEM_* constants (parsenodes.h).
const (
	opclassItemOperator    = 1
	opclassItemFunction    = 2
	opclassItemStorageType = 3
)

// parseOpclassItem is gram.y's opclass_item.
func (p *parser) parseOpclassItem() *ast.Node {
	n := &ast.CreateOpClassItem{}
	switch {
	case p.have(ast.Token_OPERATOR):
		n.Itemtype = opclassItemOperator
		n.Number = p.iconst()
		op := p.parseAnyOperator()
		if p.kind() == ast.Token('(') {
			n.Name = &ast.ObjectWithArgs{Objname: op, Objargs: p.parseOperArgtypes()}
		} else {
			n.Name = &ast.ObjectWithArgs{Objname: op}
		}
		// opclass_purpose
		if p.kind() == ast.Token_FOR {
			p.next()
			switch {
			case p.have(ast.Token_SEARCH):
			case p.have(ast.Token_ORDER):
				p.expect(ast.Token_BY)
				n.OrderFamily = p.anyName()
			default:
				p.syntaxErrorAt()
			}
		}
	case p.have(ast.Token_FUNCTION):
		n.Itemtype = opclassItemFunction
		n.Number = p.iconst()
		if p.kind() == ast.Token('(') {
			// function_with_argtypes starts with a func_name, never '(',
			// so this must be the '(' type_list ')' prefix.
			p.next()
			n.ClassArgs = p.parseTypeList()
			p.expect(ast.Token(')'))
		}
		n.Name = p.parseFunctionWithArgtypes().GetObjectWithArgs()
	case p.have(ast.Token_STORAGE):
		n.Itemtype = opclassItemStorageType
		n.Storedtype = p.parseTypename()
	default:
		p.syntaxErrorAt()
	}
	return &ast.Node{Node: &ast.Node_CreateOpClassItem{CreateOpClassItem: n}}
}

// parseAlterOpFamilyStmt is gram.y's AlterOpFamilyStmt; ALTER OPERATOR
// FAMILY any_name USING name consumed and passed as obj (family name last
// as in the object list convention).
func (p *parser) parseAlterOpFamilyStmtImpl(opfamilyname []*ast.Node, amname string) *ast.Node {
	n := &ast.AlterOpFamilyStmt{Opfamilyname: opfamilyname, Amname: amname}
	switch {
	case p.have(ast.Token_ADD_P):
		n.Items = p.parseOpclassItemList()
	case p.have(ast.Token_DROP):
		n.IsDrop = true
		// opclass_drop_list
		for {
			d := &ast.CreateOpClassItem{}
			switch {
			case p.have(ast.Token_OPERATOR):
				d.Itemtype = opclassItemOperator
			case p.have(ast.Token_FUNCTION):
				d.Itemtype = opclassItemFunction
			default:
				p.syntaxErrorAt()
			}
			d.Number = p.iconst()
			p.expect(ast.Token('('))
			d.ClassArgs = p.parseTypeList()
			p.expect(ast.Token(')'))
			n.Items = append(n.Items,
				&ast.Node{Node: &ast.Node_CreateOpClassItem{CreateOpClassItem: d}})
			if !p.have(ast.Token(',')) {
				break
			}
		}
	default:
		p.syntaxErrorAt()
	}
	return &ast.Node{Node: &ast.Node_AlterOpFamilyStmt{AlterOpFamilyStmt: n}}
}

// parseCreateStatsStmt is gram.y's CreateStatsStmt; CREATE STATISTICS
// consumed.
func (p *parser) parseCreateStatsStmt() *ast.Node {
	n := &ast.CreateStatsStmt{}
	if p.have(ast.Token_IF_P) {
		p.expect(ast.Token_NOT)
		p.expect(ast.Token_EXISTS)
		n.IfNotExists = true
		n.Defnames = p.anyName()
	} else if p.kind() != ast.Token_ON && p.kind() != ast.Token('(') {
		n.Defnames = p.anyName()
	}
	if p.have(ast.Token('(')) {
		n.StatTypes = p.nameList()
		p.expect(ast.Token(')'))
	}
	p.expect(ast.Token_ON)
	n.Exprs = []*ast.Node{p.parseStatsParam()}
	for p.have(ast.Token(',')) {
		n.Exprs = append(n.Exprs, p.parseStatsParam())
	}
	p.expect(ast.Token_FROM)
	n.Relations = p.parseFromList()
	return &ast.Node{Node: &ast.Node_CreateStatsStmt{CreateStatsStmt: n}}
}

// parseStatsParam is gram.y's stats_param.
func (p *parser) parseStatsParam() *ast.Node {
	n := &ast.StatsElem{}
	switch {
	case p.kind() == ast.Token('('):
		p.next()
		n.Expr = p.parseAExpr(0)
		p.expect(ast.Token(')'))
	case p.startsFunctionCallAhead():
		n.Expr = p.parseFuncExprWindowless()
	default:
		n.Name = p.colId()
	}
	return &ast.Node{Node: &ast.Node_StatsElem{StatsElem: n}}
}
