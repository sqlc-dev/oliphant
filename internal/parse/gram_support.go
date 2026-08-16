package parse

import (
	"strings"

	"github.com/sqlc-dev/oliphant/ast"
)

// Ports of the static support functions at the bottom of gram.y (and the few
// makefuncs.c helpers the actions use), same names, same node shapes.

// makefuncs.c: makeFuncCall
func makeFuncCall(name []*ast.Node, args []*ast.Node, funcformat ast.CoercionForm, location int32) *ast.FuncCall {
	return &ast.FuncCall{
		Funcname:   name,
		Args:       args,
		Funcformat: funcformat,
		Location:   location,
	}
}

// makefuncs.c: makeA_Expr (list-valued operator name)
func makeA_Expr(kind ast.A_Expr_Kind, name []*ast.Node, lexpr, rexpr *ast.Node, location int32) *ast.A_Expr {
	return &ast.A_Expr{
		Kind:     kind,
		Name:     name,
		Lexpr:    lexpr,
		Rexpr:    rexpr,
		Location: location,
	}
}

// makefuncs.c: makeSimpleA_Expr
func makeSimpleA_Expr(kind ast.A_Expr_Kind, name string, lexpr, rexpr *ast.Node, location int32) *ast.A_Expr {
	return makeA_Expr(kind, []*ast.Node{nStr(name)}, lexpr, rexpr, location)
}

// makefuncs.c: makeBoolExpr
func makeBoolExpr(boolop ast.BoolExprType, args []*ast.Node, location int32) *ast.Node {
	return nBoolExpr(&ast.BoolExpr{Boolop: boolop, Args: args, Location: location})
}

// makefuncs.c: makeRangeVar
func makeRangeVar(schemaname, relname string, location int32) *ast.RangeVar {
	return &ast.RangeVar{
		Schemaname:     schemaname,
		Relname:        relname,
		Inh:            true,
		Relpersistence: "p",
		Location:       location,
	}
}

// makefuncs.c: makeTypeNameFromNameList
func makeTypeNameFromNameList(names []*ast.Node) *ast.TypeName {
	return &ast.TypeName{Names: names, Typemod: -1, Location: -1}
}

// gram.y: SystemFuncName
func SystemFuncName(name string) []*ast.Node {
	return []*ast.Node{nStr("pg_catalog"), nStr(name)}
}

// gram.y: SystemTypeName
func SystemTypeName(name string) *ast.TypeName {
	return makeTypeNameFromNameList([]*ast.Node{nStr("pg_catalog"), nStr(name)})
}

// gram.y: makeColumnRef
func (p *parser) makeColumnRef(colname string, indirection []*ast.Node, location int32) *ast.Node {
	c := &ast.ColumnRef{Location: location}
	nfields := 0
	for i, el := range indirection {
		if isAIndices(el) {
			a := &ast.A_Indirection{}
			if nfields == 0 {
				c.Fields = []*ast.Node{nStr(colname)}
				a.Indirection = p.check_indirection(indirection)
			} else {
				a.Indirection = p.check_indirection(indirection[nfields:])
				c.Fields = append([]*ast.Node{nStr(colname)}, indirection[:nfields]...)
			}
			a.Arg = nColumnRef(c)
			return nAIndirection(a)
		}
		if isAStar(el) {
			// We only allow '*' at the end of a ColumnRef
			if i < len(indirection)-1 {
				p.parserYyerror(`improper use of "*"`)
			}
		}
		nfields++
	}
	c.Fields = append([]*ast.Node{nStr(colname)}, indirection...)
	return nColumnRef(c)
}

// gram.y: check_indirection — only allow '*' at the end of the list.
func (p *parser) check_indirection(indirection []*ast.Node) []*ast.Node {
	for i, el := range indirection {
		if isAStar(el) && i < len(indirection)-1 {
			p.parserYyerror(`improper use of "*"`)
		}
	}
	return indirection
}

// gram.y: check_func_name — reject subscripts and '*' from func_name.
func (p *parser) check_func_name(names []*ast.Node) []*ast.Node {
	for _, n := range names {
		if asString(n) == nil {
			p.parserYyerror("syntax error")
		}
	}
	return names
}

// gram.y: check_qualified_name
func (p *parser) check_qualified_name(names []*ast.Node) {
	for _, n := range names {
		if asString(n) == nil {
			p.parserYyerror("syntax error")
		}
	}
}

// parserYyerror is parser_yyerror(msg): scanner_yyerror at the current
// lookahead token.
func (p *parser) parserYyerror(msg string) {
	p.fail(p.filter.SyntaxErrorMsg(p.peek(), msg))
}

// makefuncs.c: makeAlias
func makeAlias(aliasname string, colnames []*ast.Node) *ast.Alias {
	return &ast.Alias{Aliasname: aliasname, Colnames: colnames}
}

// makefuncs.c: makeDefElemExtended
func makeDefElemExtended(nameSpace, name string, arg *ast.Node, defaction ast.DefElemAction, location int32) *ast.Node {
	return nDefElem(&ast.DefElem{
		Defnamespace: nameSpace,
		Defname:      name,
		Arg:          arg,
		Defaction:    defaction,
		Location:     location,
	})
}

// gram.y: makeTypeCast
func makeTypeCast(arg *ast.Node, typename *ast.TypeName, location int32) *ast.Node {
	return nTypeCast(&ast.TypeCast{Arg: arg, TypeName: typename, Location: location})
}

// gram.y: makeStringConst
func makeStringConst(str string, location int32) *ast.Node {
	return nAConst(&ast.A_Const{
		Val:      &ast.A_Const_Sval{Sval: &ast.String{Sval: str}},
		Location: location,
	})
}

// gram.y: makeStringConstCast
func makeStringConstCast(str string, location int32, typename *ast.TypeName) *ast.Node {
	return makeTypeCast(makeStringConst(str, location), typename, -1)
}

// gram.y: makeIntConst
func makeIntConst(val int32, location int32) *ast.Node {
	return nAConst(&ast.A_Const{
		Val:      &ast.A_Const_Ival{Ival: &ast.Integer{Ival: val}},
		Location: location,
	})
}

// gram.y: makeFloatConst
func makeFloatConst(str string, location int32) *ast.Node {
	return nAConst(&ast.A_Const{
		Val:      &ast.A_Const_Fval{Fval: &ast.Float{Fval: str}},
		Location: location,
	})
}

// gram.y: makeBoolAConst
func makeBoolAConst(state bool, location int32) *ast.Node {
	return nAConst(&ast.A_Const{
		Val:      &ast.A_Const_Boolval{Boolval: &ast.Boolean{Boolval: state}},
		Location: location,
	})
}

// gram.y: makeBitStringConst
func makeBitStringConst(str string, location int32) *ast.Node {
	return nAConst(&ast.A_Const{
		Val:      &ast.A_Const_Bsval{Bsval: &ast.BitString{Bsval: str}},
		Location: location,
	})
}

// gram.y: makeNullAConst
func makeNullAConst(location int32) *ast.Node {
	return nAConst(&ast.A_Const{Isnull: true, Location: location})
}

// gram.y: makeParamRef
func makeParamRef(number, location int32) *ast.Node {
	return nParamRef(&ast.ParamRef{Number: number, Location: location})
}

// gram.y: makeParamRefCast
func makeParamRefCast(number, location int32, typename *ast.TypeName) *ast.Node {
	return makeTypeCast(makeParamRef(number, location), typename, -1)
}

// gram.y: doNegate
func doNegate(n *ast.Node, location int32) *ast.Node {
	if con := asAConst(n); con != nil {
		if iv, ok := con.Val.(*ast.A_Const_Ival); ok {
			// report the constant's location as that of the '-' sign
			con.Location = location
			iv.Ival.Ival = -iv.Ival.Ival
			return n
		}
		if fv, ok := con.Val.(*ast.A_Const_Fval); ok {
			con.Location = location
			doNegateFloat(fv.Fval)
			return n
		}
	}
	return nAExpr(makeSimpleA_Expr(ast.A_Expr_Kind_AEXPR_OP, "-", nil, n, location))
}

// gram.y: doNegateFloat
func doNegateFloat(v *ast.Float) {
	oldval := v.Fval
	oldval = strings.TrimPrefix(oldval, "+")
	if strings.HasPrefix(oldval, "-") {
		v.Fval = oldval[1:] // just strip the '-'
	} else {
		v.Fval = "-" + oldval
	}
}

// gram.y: makeAndExpr — flatten "a AND b AND c" on sight.
func makeAndExpr(lexpr, rexpr *ast.Node, location int32) *ast.Node {
	if bl := asBoolExpr(lexpr); bl != nil && bl.Boolop == ast.BoolExprType_AND_EXPR {
		bl.Args = append(bl.Args, rexpr)
		return lexpr
	}
	return makeBoolExpr(ast.BoolExprType_AND_EXPR, []*ast.Node{lexpr, rexpr}, location)
}

// gram.y: makeOrExpr — flatten "a OR b OR c" on sight.
func makeOrExpr(lexpr, rexpr *ast.Node, location int32) *ast.Node {
	if bl := asBoolExpr(lexpr); bl != nil && bl.Boolop == ast.BoolExprType_OR_EXPR {
		bl.Args = append(bl.Args, rexpr)
		return lexpr
	}
	return makeBoolExpr(ast.BoolExprType_OR_EXPR, []*ast.Node{lexpr, rexpr}, location)
}

// gram.y: makeNotExpr
func makeNotExpr(expr *ast.Node, location int32) *ast.Node {
	return makeBoolExpr(ast.BoolExprType_NOT_EXPR, []*ast.Node{expr}, location)
}

// gram.y: makeAArrayExpr
func makeAArrayExpr(elements []*ast.Node, location int32) *ast.Node {
	return nAArrayExpr(&ast.A_ArrayExpr{Elements: elements, Location: location})
}

// gram.y: makeSQLValueFunction
func makeSQLValueFunction(op ast.SQLValueFunctionOp, typmod int32, location int32) *ast.Node {
	return nSQLValueFunction(&ast.SQLValueFunction{Op: op, Typmod: typmod, Location: location})
}

// gram.y: makeXmlExpr — xmloption is only meaningful for XMLPARSE, but the
// C struct's zero value is XMLOPTION_DOCUMENT and the emitter writes enums
// unconditionally.
func makeXmlExpr(op ast.XmlExprOp, name string, namedArgs, args []*ast.Node, location int32) *ast.Node {
	return nXmlExpr(&ast.XmlExpr{
		Op:        op,
		Name:      name,
		NamedArgs: namedArgs,
		Args:      args,
		Xmloption: ast.XmlOptionType_XMLOPTION_DOCUMENT,
		Location:  location,
	})
}

// gram.y: makeSetOp
func makeSetOp(op ast.SetOperation, all bool, larg, rarg *ast.Node) *ast.Node {
	return nSelectStmt(&ast.SelectStmt{
		Op:          op,
		All:         all,
		Larg:        asSelectStmt(larg),
		Rarg:        asSelectStmt(rarg),
		LimitOption: ast.LimitOption_LIMIT_OPTION_DEFAULT,
	})
}

// gram.y: makeRangeVarFromAnyName
func (p *parser) makeRangeVarFromAnyName(names []*ast.Node, position int32) *ast.RangeVar {
	r := makeRangeVar("", "", position)
	switch len(names) {
	case 1:
		r.Relname = asString(names[0]).Sval
	case 2:
		r.Schemaname = asString(names[0]).Sval
		r.Relname = asString(names[1]).Sval
	case 3:
		r.Catalogname = asString(names[0]).Sval
		r.Schemaname = asString(names[1]).Sval
		r.Relname = asString(names[2]).Sval
	default:
		p.ereport("makeRangeVarFromAnyName",
			"improper qualified name (too many dotted names): "+nameListToString(names), position)
	}
	return r
}

// gram.y: makeRangeVarFromQualifiedName — relation_name '.' {...} forms.
func (p *parser) makeRangeVarFromQualifiedName(name string, namelist []*ast.Node, location int32) *ast.RangeVar {
	p.check_qualified_name(namelist)
	r := makeRangeVar("", "", location)
	switch len(namelist) {
	case 1:
		r.Schemaname = name
		r.Relname = asString(namelist[0]).Sval
	case 2:
		r.Catalogname = name
		r.Schemaname = asString(namelist[0]).Sval
		r.Relname = asString(namelist[1]).Sval
	default:
		all := append([]*ast.Node{nStr(name)}, namelist...)
		p.ereport("makeRangeVarFromQualifiedName",
			"improper qualified name (too many dotted names): "+nameListToString(all), location)
	}
	return r
}

// namestrcpy/NameListToString (namespace.c): dotted, no quoting.
func nameListToString(names []*ast.Node) string {
	var b strings.Builder
	for i, n := range names {
		if i > 0 {
			b.WriteByte('.')
		}
		if s := asString(n); s != nil {
			b.WriteString(s.Sval)
		} else {
			b.WriteString("*") // A_Star; matches NameListToString's else branch
		}
	}
	return b.String()
}

// nodeFuncs.c: exprLocation, for the node shapes the SELECT-milestone error
// paths can reach.
func exprLocation(n *ast.Node) int32 {
	if n == nil {
		return -1
	}
	switch w := n.Node.(type) {
	case *ast.Node_AConst:
		return w.AConst.Location
	case *ast.Node_ColumnRef:
		return w.ColumnRef.Location
	case *ast.Node_ParamRef:
		return w.ParamRef.Location
	case *ast.Node_AExpr:
		return w.AExpr.Location
	case *ast.Node_FuncCall:
		return w.FuncCall.Location
	case *ast.Node_BoolExpr:
		return leftmostLoc(w.BoolExpr.Location, exprLocationList(w.BoolExpr.Args))
	case *ast.Node_TypeCast:
		// TypeCast: use the argument's location if the cast has none
		l := exprLocation(w.TypeCast.Arg)
		if w.TypeCast.Location >= 0 && (l < 0 || w.TypeCast.Location < l) {
			l = w.TypeCast.Location
		}
		return l
	case *ast.Node_CollateClause:
		return w.CollateClause.Location
	case *ast.Node_SortBy:
		// just use argument's location
		return exprLocation(w.SortBy.Node)
	case *ast.Node_WindowDef:
		return w.WindowDef.Location
	case *ast.Node_SubLink:
		return w.SubLink.Location
	case *ast.Node_CaseExpr:
		return w.CaseExpr.Location
	case *ast.Node_RowExpr:
		return w.RowExpr.Location
	case *ast.Node_AIndirection:
		return exprLocation(w.AIndirection.Arg)
	case *ast.Node_AArrayExpr:
		return w.AArrayExpr.Location
	case *ast.Node_List:
		return exprLocationList(w.List.Items)
	case *ast.Node_WithClause:
		return w.WithClause.Location
	case *ast.Node_NullTest:
		return leftmostLoc(w.NullTest.Location, exprLocation(w.NullTest.Arg))
	case *ast.Node_BooleanTest:
		return leftmostLoc(w.BooleanTest.Location, exprLocation(w.BooleanTest.Arg))
	case *ast.Node_SetToDefault:
		return w.SetToDefault.Location
	case *ast.Node_SqlvalueFunction:
		return w.SqlvalueFunction.Location
	case *ast.Node_MinMaxExpr:
		return w.MinMaxExpr.Location
	case *ast.Node_CoalesceExpr:
		return w.CoalesceExpr.Location
	case *ast.Node_GroupingFunc:
		return w.GroupingFunc.Location
	case *ast.Node_NamedArgExpr:
		return leftmostLoc(w.NamedArgExpr.Location, exprLocation(w.NamedArgExpr.Arg))
	case *ast.Node_XmlExpr:
		return w.XmlExpr.Location
	case *ast.Node_XmlSerialize:
		return w.XmlSerialize.Location
	case *ast.Node_TypeName:
		return w.TypeName.Location
	}
	return -1
}

// exprLocation over a list: leftmost location of the members (nodeFuncs.c
// treats a List by recursing over elements keeping the leftmost).
func exprLocationList(items []*ast.Node) int32 {
	loc := int32(-1)
	for _, it := range items {
		loc = leftmostLoc(loc, exprLocation(it))
	}
	return loc
}

// nodeFuncs.c: leftmostLoc
func leftmostLoc(loc1, loc2 int32) int32 {
	if loc1 < 0 {
		return loc2
	}
	if loc2 < 0 {
		return loc1
	}
	if loc1 < loc2 {
		return loc1
	}
	return loc2
}
