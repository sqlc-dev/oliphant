package parse

// CreateFunctionStmt (CREATE FUNCTION/PROCEDURE), AlterFunctionStmt, and
// the routine-body machinery.

import (
	"github.com/sqlc-dev/oliphant/ast"
)

// parseFuncArgsWithDefaults is gram.y's func_args_with_defaults.
func (p *parser) parseFuncArgsWithDefaults() []*ast.Node {
	p.expect(ast.Token('('))
	var params []*ast.Node
	if p.kind() != ast.Token(')') {
		params = append(params, p.parseFuncArgWithDefault())
		for p.have(ast.Token(',')) {
			params = append(params, p.parseFuncArgWithDefault())
		}
	}
	p.expect(ast.Token(')'))
	return params
}

// parseFuncArgWithDefault is gram.y's func_arg_with_default.
func (p *parser) parseFuncArgWithDefault() *ast.Node {
	fp := p.parseFuncArg()
	if p.have(ast.Token_DEFAULT) || p.have(ast.Token('=')) {
		fp.Defexpr = p.parseAExpr(0)
	}
	return nFunctionParameter(fp)
}

// parseCreateFunctionStmt is gram.y's CreateFunctionStmt. CREATE
// [OR REPLACE] is consumed; FUNCTION or PROCEDURE is the lookahead.
func (p *parser) parseCreateFunctionStmt(replace bool) *ast.Node {
	n := &ast.CreateFunctionStmt{Replace: replace}
	n.IsProcedure = p.next().Kind == ast.Token_PROCEDURE
	n.Funcname = p.parseFuncName(p.peek())
	n.Parameters = p.parseFuncArgsWithDefaults()
	if !n.IsProcedure && p.kind() == ast.Token_RETURNS && p.kindN(1) != ast.Token_NULL_P {
		p.next()
		if p.kind() == ast.Token_TABLE && p.kindN(1) == ast.Token('(') {
			ttok := p.next()
			p.next()
			// table_func_column_list
			var cols []*ast.Node
			cols = append(cols, p.parseTableFuncColumn())
			for p.have(ast.Token(',')) {
				cols = append(cols, p.parseTableFuncColumn())
			}
			p.expect(ast.Token(')'))
			// gram.y: mergeTableFuncParameters
			for _, arg := range n.Parameters {
				switch arg.GetFunctionParameter().GetMode() {
				case ast.FunctionParameterMode_FUNC_PARAM_DEFAULT,
					ast.FunctionParameterMode_FUNC_PARAM_IN,
					ast.FunctionParameterMode_FUNC_PARAM_VARIADIC:
				default:
					p.ereport("base_yyparse", "OUT and INOUT arguments aren't allowed in TABLE functions", -1)
				}
			}
			n.Parameters = append(n.Parameters, cols...)
			// gram.y: TableFuncTypeName
			if len(cols) == 1 {
				src := cols[0].GetFunctionParameter().GetArgType()
				n.ReturnType = &ast.TypeName{
					Names:       src.Names,
					TypeOid:     src.TypeOid,
					Setof:       true,
					PctType:     src.PctType,
					Typmods:     src.Typmods,
					Typemod:     src.Typemod,
					ArrayBounds: src.ArrayBounds,
				}
			} else {
				n.ReturnType = SystemTypeName("record")
				n.ReturnType.Setof = true
			}
			n.ReturnType.Location = ttok.Start
		} else {
			n.ReturnType = p.parseFuncType()
		}
	}
	n.Options = p.parseCreateFuncOptList()
	n.SqlBody = p.parseOptRoutineBody()
	return &ast.Node{Node: &ast.Node_CreateFunctionStmt{CreateFunctionStmt: n}}
}

// parseTableFuncColumn is gram.y's table_func_column.
func (p *parser) parseTableFuncColumn() *ast.Node {
	return nFunctionParameter(&ast.FunctionParameter{
		Name:    p.paramName(),
		ArgType: p.parseFuncType(),
		Mode:    ast.FunctionParameterMode_FUNC_PARAM_TABLE,
	})
}

// parseCommonFuncOptItem is gram.y's common_func_opt_item; returns nil when
// the lookahead is not one.
func (p *parser) parseCommonFuncOptItem() *ast.Node {
	tok := p.peek()
	switch tok.Kind {
	case ast.Token_CALLED:
		p.next()
		p.expect(ast.Token_ON)
		p.expect(ast.Token_NULL_P)
		p.expect(ast.Token_INPUT_P)
		return makeDefElem("strict", nBoolean(false), tok.Start)
	case ast.Token_RETURNS:
		if p.kindN(1) != ast.Token_NULL_P {
			return nil
		}
		p.next()
		p.next()
		p.expect(ast.Token_ON)
		p.expect(ast.Token_NULL_P)
		p.expect(ast.Token_INPUT_P)
		return makeDefElem("strict", nBoolean(true), tok.Start)
	case ast.Token_STRICT_P:
		p.next()
		return makeDefElem("strict", nBoolean(true), tok.Start)
	case ast.Token_IMMUTABLE:
		p.next()
		return makeDefElem("volatility", nStr("immutable"), tok.Start)
	case ast.Token_STABLE:
		p.next()
		return makeDefElem("volatility", nStr("stable"), tok.Start)
	case ast.Token_VOLATILE:
		p.next()
		return makeDefElem("volatility", nStr("volatile"), tok.Start)
	case ast.Token_EXTERNAL:
		p.next()
		p.expect(ast.Token_SECURITY)
		switch {
		case p.have(ast.Token_DEFINER):
			return makeDefElem("security", nBoolean(true), tok.Start)
		case p.have(ast.Token_INVOKER):
			return makeDefElem("security", nBoolean(false), tok.Start)
		}
		p.syntaxErrorAt()
	case ast.Token_SECURITY:
		p.next()
		switch {
		case p.have(ast.Token_DEFINER):
			return makeDefElem("security", nBoolean(true), tok.Start)
		case p.have(ast.Token_INVOKER):
			return makeDefElem("security", nBoolean(false), tok.Start)
		}
		p.syntaxErrorAt()
	case ast.Token_LEAKPROOF:
		p.next()
		return makeDefElem("leakproof", nBoolean(true), tok.Start)
	case ast.Token_NOT:
		if p.kindN(1) != ast.Token_LEAKPROOF {
			return nil
		}
		p.next()
		p.next()
		return makeDefElem("leakproof", nBoolean(false), tok.Start)
	case ast.Token_COST:
		p.next()
		return makeDefElem("cost", p.parseNumericOnly(), tok.Start)
	case ast.Token_ROWS:
		p.next()
		return makeDefElem("rows", p.parseNumericOnly(), tok.Start)
	case ast.Token_SUPPORT:
		p.next()
		return makeDefElem("support", nList(p.anyName()), tok.Start)
	case ast.Token_SET, ast.Token_RESET:
		set := p.parseFunctionSetResetClause()
		return makeDefElem("set", &ast.Node{Node: &ast.Node_VariableSetStmt{VariableSetStmt: set}}, tok.Start)
	case ast.Token_PARALLEL:
		p.next()
		return makeDefElem("parallel", nStr(p.colId()), tok.Start)
	}
	return nil
}

// parseCreateFuncOptList is gram.y's opt_createfunc_opt_list.
func (p *parser) parseCreateFuncOptList() []*ast.Node {
	var list []*ast.Node
	for {
		tok := p.peek()
		switch tok.Kind {
		case ast.Token_AS:
			p.next()
			args := []*ast.Node{nStr(p.sconst())}
			if p.have(ast.Token(',')) {
				args = append(args, nStr(p.sconst()))
			}
			list = append(list, makeDefElem("as", nList(args), tok.Start))
			continue
		case ast.Token_LANGUAGE:
			p.next()
			list = append(list, makeDefElem("language", nStr(p.nonReservedWordOrSconst()), tok.Start))
			continue
		case ast.Token_TRANSFORM:
			p.next()
			var types []*ast.Node
			p.expect(ast.Token_FOR)
			p.expect(ast.Token_TYPE_P)
			types = append(types, nTypeName(p.parseTypename()))
			for p.have(ast.Token(',')) {
				p.expect(ast.Token_FOR)
				p.expect(ast.Token_TYPE_P)
				types = append(types, nTypeName(p.parseTypename()))
			}
			list = append(list, makeDefElem("transform", nList(types), tok.Start))
			continue
		case ast.Token_WINDOW:
			p.next()
			list = append(list, makeDefElem("window", nBoolean(true), tok.Start))
			continue
		}
		if item := p.parseCommonFuncOptItem(); item != nil {
			list = append(list, item)
			continue
		}
		return list
	}
}

// parseOptRoutineBody is gram.y's opt_routine_body.
func (p *parser) parseOptRoutineBody() *ast.Node {
	switch p.kind() {
	case ast.Token_RETURN:
		p.next()
		return &ast.Node{Node: &ast.Node_ReturnStmt{ReturnStmt: &ast.ReturnStmt{
			Returnval: p.parseAExpr(0),
		}}}
	case ast.Token_BEGIN_P:
		if p.kindN(1) != ast.Token_ATOMIC {
			return nil
		}
		p.next()
		p.next()
		var stmts []*ast.Node
		for p.kind() != ast.Token_END_P {
			var stmt *ast.Node
			if p.kind() == ast.Token_RETURN {
				p.next()
				stmt = &ast.Node{Node: &ast.Node_ReturnStmt{ReturnStmt: &ast.ReturnStmt{
					Returnval: p.parseAExpr(0),
				}}}
			} else {
				stmt = p.parseToplevelStmt()
			}
			// Every routine_body_stmt carries a trailing semicolon; empty
			// statements are discarded as in stmtmulti.
			p.expect(ast.Token(';'))
			if stmt != nil {
				stmts = append(stmts, stmt)
			}
		}
		p.next()
		return nList([]*ast.Node{nList(stmts)})
	}
	return nil
}

// parseAlterFunctionStmt is gram.y's AlterFunctionStmt; ALTER FUNCTION/
// PROCEDURE/ROUTINE and the function_with_argtypes have been consumed by
// the dispatcher.
func (p *parser) parseAlterFunctionStmt(objtype ast.ObjectType, fn *ast.ObjectWithArgs) *ast.Node {
	n := &ast.AlterFunctionStmt{Objtype: objtype, Func: fn}
	item := p.parseCommonFuncOptItem()
	if item == nil {
		p.syntaxErrorAt()
	}
	for item != nil {
		n.Actions = append(n.Actions, item)
		item = p.parseCommonFuncOptItem()
	}
	p.have(ast.Token_RESTRICT)
	return &ast.Node{Node: &ast.Node_AlterFunctionStmt{AlterFunctionStmt: n}}
}
