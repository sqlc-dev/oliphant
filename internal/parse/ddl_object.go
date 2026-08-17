package parse

// Object-address machinery shared by DROP/COMMENT/SECURITY LABEL and the
// function DDL: function_with_argtypes, aggregate_with_argtypes,
// operator_with_argtypes, func_arg, plus DropStmt and friends, CommentStmt,
// and SecLabelStmt.

import (
	"github.com/sqlc-dev/oliphant/ast"
)

func nObjectWithArgs(n *ast.ObjectWithArgs) *ast.Node {
	return &ast.Node{Node: &ast.Node_ObjectWithArgs{ObjectWithArgs: n}}
}

func nFunctionParameter(n *ast.FunctionParameter) *ast.Node {
	return &ast.Node{Node: &ast.Node_FunctionParameter{FunctionParameter: n}}
}

// parseAnyNameList is gram.y's any_name_list.
func (p *parser) parseAnyNameList() []*ast.Node {
	list := []*ast.Node{nList(p.anyName())}
	for p.have(ast.Token(',')) {
		list = append(list, nList(p.anyName()))
	}
	return list
}

// parseTypeNameList is gram.y's type_name_list.
func (p *parser) parseTypeNameList() []*ast.Node {
	list := []*ast.Node{nTypeName(p.parseTypename())}
	for p.have(ast.Token(',')) {
		list = append(list, nTypeName(p.parseTypename()))
	}
	return list
}

// parseFuncType is gram.y's func_type:
//
//	Typename | type_function_name attrs '%' TYPE_P
//	| SETOF type_function_name attrs '%' TYPE_P
func (p *parser) parseFuncType() *ast.TypeName {
	// Look for the %TYPE form: [SETOF] name ('.' name)+ '%' TYPE_P.
	i := 0
	setof := false
	if p.kind() == ast.Token_SETOF {
		i = 1
		setof = true
	}
	if isTypeFunctionNameToken(p.peekN(i)) {
		j := i
		for p.kindN(j+1) == ast.Token('.') && isColLabelToken(p.peekN(j+2)) {
			j += 2
		}
		if j > i && p.kindN(j+1) == ast.Token('%') && p.kindN(j+2) == ast.Token_TYPE_P {
			if setof {
				p.next()
			}
			tok := p.peek()
			names := []*ast.Node{nStr(p.next().Str)}
			for p.have(ast.Token('.')) {
				names = append(names, nStr(p.colLabel()))
			}
			p.expect(ast.Token('%'))
			p.expect(ast.Token_TYPE_P)
			t := makeTypeNameFromNameList(names)
			t.PctType = true
			t.Setof = setof
			t.Location = tok.Start
			return t
		}
	}
	return p.parseTypename()
}

// parseFuncArg is gram.y's func_arg. Every alternative sets location = @1,
// the parameter's first token.
func (p *parser) parseFuncArg() *ast.FunctionParameter {
	n := &ast.FunctionParameter{Mode: ast.FunctionParameterMode_FUNC_PARAM_DEFAULT, Location: p.loc()}
	mode, haveMode := p.parseArgClass()
	if haveMode {
		n.Mode = mode
	}
	// A type_function_name token here is a param_name unless the tokens
	// that follow close the argument (making it the type itself).
	if isTypeFunctionNameToken(p.peek()) && p.argNameFollowedByType() {
		n.Name = p.paramName()
		if !haveMode {
			if mode, haveMode = p.parseArgClass(); haveMode {
				n.Mode = mode
			}
		}
	}
	n.ArgType = p.parseFuncType()
	return n
}

// parseArgClass is gram.y's arg_class; reports whether one was present.
func (p *parser) parseArgClass() (ast.FunctionParameterMode, bool) {
	switch p.kind() {
	case ast.Token_IN_P:
		p.next()
		if p.have(ast.Token_OUT_P) {
			return ast.FunctionParameterMode_FUNC_PARAM_INOUT, true
		}
		return ast.FunctionParameterMode_FUNC_PARAM_IN, true
	case ast.Token_OUT_P:
		p.next()
		return ast.FunctionParameterMode_FUNC_PARAM_OUT, true
	case ast.Token_INOUT:
		p.next()
		return ast.FunctionParameterMode_FUNC_PARAM_INOUT, true
	case ast.Token_VARIADIC:
		p.next()
		return ast.FunctionParameterMode_FUNC_PARAM_VARIADIC, true
	}
	return 0, false
}

// argNameFollowedByType reports whether the token after the current one
// continues a func_arg (so the current token is a param_name rather than
// the argument's type).
func (p *parser) argNameFollowedByType() bool {
	switch p.kindN(1) {
	case ast.Token(','), ast.Token(')'), ast.Token('.'), ast.Token('%'),
		ast.Token('('), ast.Token('['), ast.Token_DEFAULT, ast.Token('='),
		ast.Token_ORDER, 0:
		return false
	}
	return true
}

// extractArgTypes is gram.y's extractArgTypes.
func extractArgTypes(params []*ast.Node) []*ast.Node {
	var result []*ast.Node
	for _, pn := range params {
		fp := pn.GetFunctionParameter()
		if fp.Mode != ast.FunctionParameterMode_FUNC_PARAM_OUT &&
			fp.Mode != ast.FunctionParameterMode_FUNC_PARAM_TABLE {
			result = append(result, nTypeName(fp.ArgType))
		}
	}
	return result
}

// parseFunctionWithArgtypes is gram.y's function_with_argtypes.
func (p *parser) parseFunctionWithArgtypes() *ast.Node {
	n := &ast.ObjectWithArgs{}
	// func_name func_args, or a bare name/type_func_name_keyword/ColId
	// indirection (args_unspecified).
	n.Objname = p.parseFuncName(p.peek())
	if p.kind() == ast.Token('(') {
		params := p.parseFuncArgsParens()
		n.Objargs = extractArgTypes(params)
		n.Objfuncargs = params
	} else {
		n.ArgsUnspecified = true
	}
	return nObjectWithArgs(n)
}

// parseFuncArgsParens is gram.y's func_args: '(' [func_args_list] ')'.
func (p *parser) parseFuncArgsParens() []*ast.Node {
	p.expect(ast.Token('('))
	var params []*ast.Node
	if p.kind() != ast.Token(')') {
		params = append(params, nFunctionParameter(p.parseFuncArg()))
		for p.have(ast.Token(',')) {
			params = append(params, nFunctionParameter(p.parseFuncArg()))
		}
	}
	p.expect(ast.Token(')'))
	return params
}

// parseFunctionWithArgtypesList is gram.y's function_with_argtypes_list.
func (p *parser) parseFunctionWithArgtypesList() []*ast.Node {
	list := []*ast.Node{p.parseFunctionWithArgtypes()}
	for p.have(ast.Token(',')) {
		list = append(list, p.parseFunctionWithArgtypes())
	}
	return list
}

// parseAggrArgs is gram.y's aggr_args; returns the FunctionParameter list
// and the ORDER BY split marker.
func (p *parser) parseAggrArgs() ([]*ast.Node, int32) {
	p.expect(ast.Token('('))
	if p.have(ast.Token('*')) {
		p.expect(ast.Token(')'))
		return nil, -1
	}
	if p.have(ast.Token_ORDER) {
		p.expect(ast.Token_BY)
		args := p.parseAggrArgsList()
		p.expect(ast.Token(')'))
		return args, 0
	}
	direct := p.parseAggrArgsList()
	if p.have(ast.Token_ORDER) {
		p.expect(ast.Token_BY)
		otok := p.peek()
		ordered := p.parseAggrArgsList()
		p.expect(ast.Token(')'))
		return p.makeOrderedSetArgs(direct, ordered, otok.Start)
	}
	p.expect(ast.Token(')'))
	return direct, -1
}

// parseAggrArgsList is gram.y's aggr_args_list.
func (p *parser) parseAggrArgsList() []*ast.Node {
	list := []*ast.Node{p.parseAggrArg()}
	for p.have(ast.Token(',')) {
		list = append(list, p.parseAggrArg())
	}
	return list
}

// parseAggrArg is gram.y's aggr_arg.
func (p *parser) parseAggrArg() *ast.Node {
	tok := p.peek()
	fp := p.parseFuncArg()
	switch fp.Mode {
	case ast.FunctionParameterMode_FUNC_PARAM_DEFAULT,
		ast.FunctionParameterMode_FUNC_PARAM_IN,
		ast.FunctionParameterMode_FUNC_PARAM_VARIADIC:
	default:
		p.ereport("base_yyparse", "aggregates cannot have output arguments", tok.Start)
	}
	return nFunctionParameter(fp)
}

// makeOrderedSetArgs is gram.y's makeOrderedSetArgs.
func (p *parser) makeOrderedSetArgs(direct, ordered []*ast.Node, orderedLoc int32) ([]*ast.Node, int32) {
	lastd := direct[len(direct)-1].GetFunctionParameter()
	if lastd.Mode == ast.FunctionParameterMode_FUNC_PARAM_VARIADIC {
		firsto := ordered[0].GetFunctionParameter()
		if len(ordered) != 1 ||
			firsto.Mode != ast.FunctionParameterMode_FUNC_PARAM_VARIADIC ||
			!typeNamesEqual(lastd.ArgType, firsto.ArgType) {
			p.ereport("makeOrderedSetArgs",
				"an ordered-set aggregate with a VARIADIC direct argument must have one VARIADIC aggregated argument of the same data type",
				firsto.ArgType.GetLocation())
		}
		ordered = nil
	}
	ndirect := int32(len(direct))
	return append(direct, ordered...), ndirect
}

// typeNamesEqual is equal() on two TypeNames ignoring locations, as PG's
// equalfuncs do.
func typeNamesEqual(a, b *ast.TypeName) bool {
	if len(a.GetNames()) != len(b.GetNames()) {
		return false
	}
	for i := range a.GetNames() {
		if asString(a.Names[i]).GetSval() != asString(b.Names[i]).GetSval() {
			return false
		}
	}
	return a.GetSetof() == b.GetSetof() && a.GetPctType() == b.GetPctType() &&
		len(a.GetArrayBounds()) == len(b.GetArrayBounds()) &&
		a.GetTypemod() == b.GetTypemod() &&
		len(a.GetTypmods()) == len(b.GetTypmods())
}

// parseAggregateWithArgtypes is gram.y's aggregate_with_argtypes.
func (p *parser) parseAggregateWithArgtypes() *ast.Node {
	n := &ast.ObjectWithArgs{}
	n.Objname = p.parseFuncName(p.peek())
	params, _ := p.parseAggrArgs()
	n.Objargs = extractArgTypes(params)
	n.Objfuncargs = params
	return nObjectWithArgs(n)
}

// parseAggregateWithArgtypesList is gram.y's aggregate_with_argtypes_list.
func (p *parser) parseAggregateWithArgtypesList() []*ast.Node {
	list := []*ast.Node{p.parseAggregateWithArgtypes()}
	for p.have(ast.Token(',')) {
		list = append(list, p.parseAggregateWithArgtypes())
	}
	return list
}

// parseOperArgtypes is gram.y's oper_argtypes.
func (p *parser) parseOperArgtypes() []*ast.Node {
	p.expect(ast.Token('('))
	var left, right *ast.Node
	if p.have(ast.Token_NONE) {
		left = nil
	} else {
		left = nTypeName(p.parseTypename())
	}
	if p.kind() == ast.Token(')') {
		ctok := p.peek()
		p.ereport("base_yyparse", "missing argument", ctok.Start)
	}
	p.expect(ast.Token(','))
	if p.have(ast.Token_NONE) {
		right = nil
	} else {
		right = nTypeName(p.parseTypename())
	}
	p.expect(ast.Token(')'))
	return []*ast.Node{left, right}
}

// parseOperatorWithArgtypes is gram.y's operator_with_argtypes.
func (p *parser) parseOperatorWithArgtypes() *ast.Node {
	n := &ast.ObjectWithArgs{
		Objname: p.parseAnyOperator(),
		Objargs: p.parseOperArgtypes(),
	}
	return nObjectWithArgs(n)
}

// parseOperatorWithArgtypesList is gram.y's operator_with_argtypes_list.
func (p *parser) parseOperatorWithArgtypesList() []*ast.Node {
	list := []*ast.Node{p.parseOperatorWithArgtypes()}
	for p.have(ast.Token(',')) {
		list = append(list, p.parseOperatorWithArgtypes())
	}
	return list
}

// tryObjectTypeAnyName consumes gram.y's object_type_any_name when the
// lookahead matches it.
func (p *parser) tryObjectTypeAnyName() (ast.ObjectType, bool) {
	switch p.kind() {
	case ast.Token_TABLE:
		p.next()
		return ast.ObjectType_OBJECT_TABLE, true
	case ast.Token_SEQUENCE:
		p.next()
		return ast.ObjectType_OBJECT_SEQUENCE, true
	case ast.Token_VIEW:
		p.next()
		return ast.ObjectType_OBJECT_VIEW, true
	case ast.Token_MATERIALIZED:
		if p.kindN(1) == ast.Token_VIEW {
			p.next()
			p.next()
			return ast.ObjectType_OBJECT_MATVIEW, true
		}
	case ast.Token_INDEX:
		p.next()
		return ast.ObjectType_OBJECT_INDEX, true
	case ast.Token_FOREIGN:
		if p.kindN(1) == ast.Token_TABLE {
			p.next()
			p.next()
			return ast.ObjectType_OBJECT_FOREIGN_TABLE, true
		}
	case ast.Token_COLLATION:
		p.next()
		return ast.ObjectType_OBJECT_COLLATION, true
	case ast.Token_CONVERSION_P:
		p.next()
		return ast.ObjectType_OBJECT_CONVERSION, true
	case ast.Token_STATISTICS:
		p.next()
		return ast.ObjectType_OBJECT_STATISTIC_EXT, true
	case ast.Token_TEXT_P:
		if p.kindN(1) == ast.Token_SEARCH {
			switch p.kindN(2) {
			case ast.Token_PARSER:
				p.next()
				p.next()
				p.next()
				return ast.ObjectType_OBJECT_TSPARSER, true
			case ast.Token_DICTIONARY:
				p.next()
				p.next()
				p.next()
				return ast.ObjectType_OBJECT_TSDICTIONARY, true
			case ast.Token_TEMPLATE:
				p.next()
				p.next()
				p.next()
				return ast.ObjectType_OBJECT_TSTEMPLATE, true
			case ast.Token_CONFIGURATION:
				p.next()
				p.next()
				p.next()
				return ast.ObjectType_OBJECT_TSCONFIGURATION, true
			}
		}
	}
	return 0, false
}

// tryDropTypeName consumes gram.y's drop_type_name when the lookahead
// matches it.
func (p *parser) tryDropTypeName() (ast.ObjectType, bool) {
	switch p.kind() {
	case ast.Token_ACCESS:
		if p.kindN(1) == ast.Token_METHOD {
			p.next()
			p.next()
			return ast.ObjectType_OBJECT_ACCESS_METHOD, true
		}
	case ast.Token_EVENT:
		if p.kindN(1) == ast.Token_TRIGGER {
			p.next()
			p.next()
			return ast.ObjectType_OBJECT_EVENT_TRIGGER, true
		}
	case ast.Token_EXTENSION:
		p.next()
		return ast.ObjectType_OBJECT_EXTENSION, true
	case ast.Token_FOREIGN:
		if p.kindN(1) == ast.Token_DATA_P && p.kindN(2) == ast.Token_WRAPPER {
			p.next()
			p.next()
			p.next()
			return ast.ObjectType_OBJECT_FDW, true
		}
	case ast.Token_PROCEDURAL:
		if p.kindN(1) == ast.Token_LANGUAGE {
			p.next()
			p.next()
			return ast.ObjectType_OBJECT_LANGUAGE, true
		}
	case ast.Token_LANGUAGE:
		p.next()
		return ast.ObjectType_OBJECT_LANGUAGE, true
	case ast.Token_PUBLICATION:
		p.next()
		return ast.ObjectType_OBJECT_PUBLICATION, true
	case ast.Token_SCHEMA:
		p.next()
		return ast.ObjectType_OBJECT_SCHEMA, true
	case ast.Token_SERVER:
		p.next()
		return ast.ObjectType_OBJECT_FOREIGN_SERVER, true
	}
	return 0, false
}

// tryObjectTypeName consumes gram.y's object_type_name when the lookahead
// matches it.
func (p *parser) tryObjectTypeName() (ast.ObjectType, bool) {
	if t, ok := p.tryDropTypeName(); ok {
		return t, ok
	}
	switch p.kind() {
	case ast.Token_DATABASE:
		p.next()
		return ast.ObjectType_OBJECT_DATABASE, true
	case ast.Token_ROLE:
		p.next()
		return ast.ObjectType_OBJECT_ROLE, true
	case ast.Token_SUBSCRIPTION:
		p.next()
		return ast.ObjectType_OBJECT_SUBSCRIPTION, true
	case ast.Token_TABLESPACE:
		p.next()
		return ast.ObjectType_OBJECT_TABLESPACE, true
	}
	return 0, false
}

// tryObjectTypeNameOnAnyName consumes gram.y's
// object_type_name_on_any_name when the lookahead matches it.
func (p *parser) tryObjectTypeNameOnAnyName() (ast.ObjectType, bool) {
	switch p.kind() {
	case ast.Token_POLICY:
		p.next()
		return ast.ObjectType_OBJECT_POLICY, true
	case ast.Token_RULE:
		p.next()
		return ast.ObjectType_OBJECT_RULE, true
	case ast.Token_TRIGGER:
		p.next()
		return ast.ObjectType_OBJECT_TRIGGER, true
	}
	return 0, false
}

// newDropStmt builds the common DropStmt shell.
func newDropStmt(removeType ast.ObjectType) *ast.DropStmt {
	return &ast.DropStmt{RemoveType: removeType}
}

func nDropStmt(n *ast.DropStmt) *ast.Node {
	return &ast.Node{Node: &ast.Node_DropStmt{DropStmt: n}}
}

// parseDropIfExists consumes IF_P EXISTS if present.
func (p *parser) parseDropIfExists() bool {
	if p.have(ast.Token_IF_P) {
		p.expect(ast.Token_EXISTS)
		return true
	}
	return false
}

// parseCommentStmt is gram.y's CommentStmt.
func (p *parser) parseCommentStmt() *ast.Node {
	p.expect(ast.Token_COMMENT)
	p.expect(ast.Token_ON)
	n := &ast.CommentStmt{}
	n.Objtype, n.Object = p.parseCommentOrLabelObject()
	p.expect(ast.Token_IS)
	// comment_text: Sconst | NULL_P
	if !p.have(ast.Token_NULL_P) {
		n.Comment = p.sconst()
	}
	return &ast.Node{Node: &ast.Node_CommentStmt{CommentStmt: n}}
}

// parseSecLabelStmt is gram.y's SecLabelStmt; SECURITY LABEL consumed by
// the caller.
func (p *parser) parseSecLabelStmt() *ast.Node {
	n := &ast.SecLabelStmt{}
	if p.have(ast.Token_FOR) {
		n.Provider = p.nonReservedWordOrSconst()
	}
	p.expect(ast.Token_ON)
	n.Objtype, n.Object = p.parseCommentOrLabelObject()
	p.expect(ast.Token_IS)
	if !p.have(ast.Token_NULL_P) {
		n.Label = p.sconst()
	}
	return &ast.Node{Node: &ast.Node_SecLabelStmt{SecLabelStmt: n}}
}

// parseCommentOrLabelObject parses the shared object reference of
// CommentStmt and SecLabelStmt (SecLabelStmt accepts a subset; the
// difference only shows as which error a bad object type raises, and both
// report at the same token).
func (p *parser) parseCommentOrLabelObject() (ast.ObjectType, *ast.Node) {
	switch p.kind() {
	case ast.Token_COLUMN:
		p.next()
		return ast.ObjectType_OBJECT_COLUMN, nList(p.anyName())
	case ast.Token_TYPE_P:
		p.next()
		return ast.ObjectType_OBJECT_TYPE, nTypeName(p.parseTypename())
	case ast.Token_DOMAIN_P:
		p.next()
		return ast.ObjectType_OBJECT_DOMAIN, nTypeName(p.parseTypename())
	case ast.Token_AGGREGATE:
		p.next()
		return ast.ObjectType_OBJECT_AGGREGATE, p.parseAggregateWithArgtypes()
	case ast.Token_FUNCTION:
		p.next()
		return ast.ObjectType_OBJECT_FUNCTION, p.parseFunctionWithArgtypes()
	case ast.Token_PROCEDURE:
		p.next()
		return ast.ObjectType_OBJECT_PROCEDURE, p.parseFunctionWithArgtypes()
	case ast.Token_ROUTINE:
		p.next()
		return ast.ObjectType_OBJECT_ROUTINE, p.parseFunctionWithArgtypes()
	case ast.Token_OPERATOR:
		p.next()
		switch p.kind() {
		case ast.Token_CLASS:
			p.next()
			names := p.anyName()
			p.expect(ast.Token_USING)
			return ast.ObjectType_OBJECT_OPCLASS,
				nList(append([]*ast.Node{nStr(p.name())}, names...))
		case ast.Token_FAMILY:
			p.next()
			names := p.anyName()
			p.expect(ast.Token_USING)
			return ast.ObjectType_OBJECT_OPFAMILY,
				nList(append([]*ast.Node{nStr(p.name())}, names...))
		}
		return ast.ObjectType_OBJECT_OPERATOR, p.parseOperatorWithArgtypes()
	case ast.Token_CONSTRAINT:
		p.next()
		name := p.name()
		p.expect(ast.Token_ON)
		if p.have(ast.Token_DOMAIN_P) {
			t := makeTypeNameFromNameList(p.anyName())
			return ast.ObjectType_OBJECT_DOMCONSTRAINT,
				nList([]*ast.Node{nTypeName(t), nStr(name)})
		}
		names := p.anyName()
		return ast.ObjectType_OBJECT_TABCONSTRAINT,
			nList(append(names, nStr(name)))
	case ast.Token_TRANSFORM:
		p.next()
		p.expect(ast.Token_FOR)
		t := p.parseTypename()
		p.expect(ast.Token_LANGUAGE)
		return ast.ObjectType_OBJECT_TRANSFORM,
			nList([]*ast.Node{nTypeName(t), nStr(p.name())})
	case ast.Token_LARGE_P:
		p.next()
		p.expect(ast.Token_OBJECT_P)
		return ast.ObjectType_OBJECT_LARGEOBJECT, p.parseNumericOnly()
	case ast.Token_CAST:
		p.next()
		p.expect(ast.Token('('))
		from := p.parseTypename()
		p.expect(ast.Token_AS)
		to := p.parseTypename()
		p.expect(ast.Token(')'))
		return ast.ObjectType_OBJECT_CAST,
			nList([]*ast.Node{nTypeName(from), nTypeName(to)})
	}
	if t, ok := p.tryObjectTypeNameOnAnyName(); ok {
		name := p.name()
		p.expect(ast.Token_ON)
		names := p.anyName()
		return t, nList(append(names, nStr(name)))
	}
	if t, ok := p.tryObjectTypeAnyName(); ok {
		return t, nList(p.anyName())
	}
	if t, ok := p.tryObjectTypeName(); ok {
		return t, nStr(p.name())
	}
	p.syntaxErrorAt()
	return 0, nil
}
