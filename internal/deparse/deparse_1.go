// Ported from postgres_deparse.c (libpg_query 18.0.0).
// Clause helpers: any_name, expression lists, target lists, from lists.
package deparse

import (
	"strings"

	"github.com/sqlc-dev/oliphant/ast"
)

func deparseAnyName(st *state, parts []*ast.Node) {
	for i, item := range parts {
		st.appendString(quoteIdentifier(strVal(item)))
		if i < len(parts)-1 {
			st.appendChar('.')
		}
	}
}

func deparseAnyNameSkipFirst(st *state, parts []*ast.Node) {
	for i := 1; i < len(parts); i++ {
		st.appendString(quoteIdentifier(strVal(parts[i])))
		if i < len(parts)-1 {
			st.appendChar('.')
		}
	}
}

func deparseAnyNameSkipLast(st *state, parts []*ast.Node) {
	for i, item := range parts {
		if i < len(parts)-1 {
			st.appendString(quoteIdentifier(strVal(item)))
			if i < len(parts)-2 {
				st.appendChar('.')
			}
		}
	}
}

func deparseFuncExpr(st *state, node *ast.Node, ctx nodeContext) {
	switch {
	case node.GetFuncCall() != nil:
		deparseFuncCall(st, node.GetFuncCall(), ctx)
	case node.GetSqlvalueFunction() != nil:
		deparseSQLValueFunction(st, node.GetSqlvalueFunction())
	case node.GetMinMaxExpr() != nil:
		deparseMinMaxExpr(st, node.GetMinMaxExpr())
	case node.GetCoalesceExpr() != nil:
		deparseCoalesceExpr(st, node.GetCoalesceExpr())
	case node.GetXmlExpr() != nil:
		deparseXmlExpr(st, node.GetXmlExpr(), ctx)
	case node.GetXmlSerialize() != nil:
		deparseXmlSerialize(st, node.GetXmlSerialize())
	case node.GetJsonObjectAgg() != nil:
		deparseJsonObjectAgg(st, node.GetJsonObjectAgg())
	case node.GetJsonArrayAgg() != nil:
		deparseJsonArrayAgg(st, node.GetJsonArrayAgg())
	case node.GetJsonObjectConstructor() != nil:
		deparseJsonObjectConstructor(st, node.GetJsonObjectConstructor())
	case node.GetJsonArrayConstructor() != nil:
		deparseJsonArrayConstructor(st, node.GetJsonArrayConstructor())
	case node.GetJsonArrayQueryConstructor() != nil:
		deparseJsonArrayQueryConstructor(st, node.GetJsonArrayQueryConstructor())
	default:
		st.errorf("deparse: unpermitted node type in func_expr: %s",
			nodeTag(node))
	}
}

func deparseExpr(st *state, node *ast.Node, ctx nodeContext) {
	if node == nil {
		return
	}
	switch {
	case node.GetColumnRef() != nil,
		node.GetAConst() != nil,
		node.GetParamRef() != nil,
		node.GetAIndirection() != nil,
		node.GetCaseExpr() != nil,
		node.GetSubLink() != nil,
		node.GetAArrayExpr() != nil,
		node.GetRowExpr() != nil,
		node.GetGroupingFunc() != nil:
		deparseCExpr(st, node)
	case node.GetTypeCast() != nil:
		deparseTypeCast(st, node.GetTypeCast(), contextNone)
	case node.GetCollateClause() != nil:
		deparseCollateClause(st, node.GetCollateClause())
	case node.GetAExpr() != nil:
		deparseAExpr(st, node.GetAExpr(), contextAExpr)
	case node.GetBoolExpr() != nil:
		deparseBoolExpr(st, node.GetBoolExpr())
	case node.GetNullTest() != nil:
		deparseNullTest(st, node.GetNullTest())
	case node.GetBooleanTest() != nil:
		deparseBooleanTest(st, node.GetBooleanTest())
	case node.GetJsonIsPredicate() != nil:
		deparseJsonIsPredicate(st, node.GetJsonIsPredicate())
	case node.GetSetToDefault() != nil:
		deparseSetToDefault(st, node.GetSetToDefault())
	case node.GetMergeSupportFunc() != nil:
		st.appendString("merge_action() ")
	case node.GetJsonParseExpr() != nil:
		deparseJsonParseExpr(st, node.GetJsonParseExpr())
	case node.GetJsonScalarExpr() != nil:
		deparseJsonScalarExpr(st, node.GetJsonScalarExpr())
	case node.GetJsonSerializeExpr() != nil:
		deparseJsonSerializeExpr(st, node.GetJsonSerializeExpr())
	case node.GetJsonFuncExpr() != nil:
		deparseJsonFuncExpr(st, node.GetJsonFuncExpr())
	case node.GetFuncCall() != nil,
		node.GetSqlvalueFunction() != nil,
		node.GetMinMaxExpr() != nil,
		node.GetCoalesceExpr() != nil,
		node.GetXmlExpr() != nil,
		node.GetXmlSerialize() != nil,
		node.GetJsonObjectAgg() != nil,
		node.GetJsonArrayAgg() != nil,
		node.GetJsonObjectConstructor() != nil,
		node.GetJsonArrayConstructor() != nil,
		node.GetJsonArrayQueryConstructor() != nil:
		deparseFuncExpr(st, node, ctx)
	default:
		// Note that this is also the fallthrough for deparseBExpr and deparseCExpr
		st.errorf("deparse: unpermitted node type in a_expr/b_expr/c_expr: %s",
			nodeTag(node))
	}
}

func deparseBExpr(st *state, node *ast.Node) {
	if node.GetXmlExpr() != nil {
		deparseXmlExpr(st, node.GetXmlExpr(), contextNone)
		return
	}

	if node.GetAExpr() != nil {
		a_expr := node.GetAExpr()
		// Other kinds are handled by "c_expr", with parens added around them
		if a_expr.Kind == ast.A_Expr_Kind_AEXPR_OP || a_expr.Kind == ast.A_Expr_Kind_AEXPR_DISTINCT || a_expr.Kind == ast.A_Expr_Kind_AEXPR_NOT_DISTINCT {
			deparseAExpr(st, a_expr, contextNone)
			return
		}
	}

	if node.GetBoolExpr() != nil {
		bool_expr := node.GetBoolExpr()
		if bool_expr.Boolop == ast.BoolExprType_NOT_EXPR {
			deparseBoolExpr(st, bool_expr)
			return
		}
	}

	deparseCExpr(st, node)
}

func deparseAexprConst(st *state, node *ast.Node) {
	switch {
	case node.GetAConst() != nil:
		deparseAConst(st, node.GetAConst())
	case node.GetTypeCast() != nil:
		deparseTypeCast(st, node.GetTypeCast(), contextNone)
	default:
		st.errorf("deparse: unpermitted node type in AexprConst: %s",
			nodeTag(node))
	}
}

func deparseCExpr(st *state, node *ast.Node) {
	switch {
	case node.GetColumnRef() != nil:
		deparseColumnRef(st, node.GetColumnRef())
	case node.GetAConst() != nil:
		deparseAConst(st, node.GetAConst())
	case node.GetParamRef() != nil:
		deparseParamRef(st, node.GetParamRef())
	case node.GetAIndirection() != nil:
		deparseAIndirection(st, node.GetAIndirection())
	case node.GetCaseExpr() != nil:
		deparseCaseExpr(st, node.GetCaseExpr())
	case node.GetSubLink() != nil:
		deparseSubLink(st, node.GetSubLink())
	case node.GetAArrayExpr() != nil:
		deparseAArrayExpr(st, node.GetAArrayExpr())
	case node.GetRowExpr() != nil:
		deparseRowExpr(st, node.GetRowExpr())
	case node.GetGroupingFunc() != nil:
		deparseGroupingFunc(st, node.GetGroupingFunc())
	case node.GetFuncCall() != nil,
		node.GetSqlvalueFunction() != nil,
		node.GetMinMaxExpr() != nil,
		node.GetCoalesceExpr() != nil,
		node.GetXmlExpr() != nil,
		node.GetXmlSerialize() != nil,
		node.GetJsonObjectAgg() != nil,
		node.GetJsonArrayAgg() != nil,
		node.GetJsonObjectConstructor() != nil,
		node.GetJsonArrayConstructor() != nil,
		node.GetJsonArrayQueryConstructor() != nil:
		deparseFuncExpr(st, node, contextNone)
	default:
		st.appendChar('(')
		// Because we wrap this in parenthesis, the expression inside follows "a_expr" parser rules
		deparseExpr(st, node, contextAExpr)
		st.appendChar(')')
	}
}

func deparseExprList(st *state, exprs []*ast.Node) {
	for i, item := range exprs {
		deparseExpr(st, item, contextAExpr)
		if i < len(exprs)-1 {
			st.appendString(", ")
		}
	}
}

func deparseColId(st *state, s string) {
	st.appendString(quoteIdentifier(s))
}

func deparseColLabel(st *state, s string) {
	st.appendString(quoteIdentifier(s))
}

func deparseSignedIconst(st *state, node *ast.Node) {
	st.appendf("%d", intVal(node))
}

func deparseOptIndirection(st *state, indirection []*ast.Node, N int) {
	for i := N; i < len(indirection); i++ {
		if indirection[i].GetString_() != nil {
			st.appendChar('.')
			deparseColLabel(st, strVal(indirection[i]))
		} else if indirection[i].GetAStar() != nil {
			st.appendString(".*")
		} else if indirection[i].GetAIndices() != nil {
			deparseAIndices(st, indirection[i].GetAIndices())
		} else {
			// No other nodes should appear here
		}
	}
}

func deparseRoleList(st *state, roles []*ast.Node) {
	for i, item := range roles {
		role_spec := item.GetRoleSpec()
		deparseRoleSpec(st, role_spec)
		if i < len(roles)-1 {
			st.appendString(", ")
		}
	}
}

func deparseSimpleTypename(st *state, node *ast.Node) {
	deparseTypeName(st, node.GetTypeName())
}

func deparseNumericOnly(st *state, value *ast.Node) {
	switch {
	case value.GetInteger() != nil:
		st.appendf("%d", value.GetInteger().Ival)
	case value.GetFloat() != nil:
		st.appendString(value.GetFloat().Fval)
	}
}

func deparseNumericOnlyList(st *state, l []*ast.Node) {
	for i, item := range l {
		deparseNumericOnly(st, item)
		if i < len(l)-1 {
			st.appendString(", ")
		}
	}
}

func deparseSeqOptElem(st *state, def_elem *ast.DefElem) {
	if def_elem.Defname == "as" {
		st.appendString("AS ")
		deparseSimpleTypename(st, def_elem.Arg)
	} else if def_elem.Defname == "cache" {
		st.appendString("CACHE ")
		deparseNumericOnly(st, def_elem.Arg)
	} else if def_elem.Defname == "cycle" && boolVal(def_elem.Arg) {
		st.appendString("CYCLE")
	} else if def_elem.Defname == "cycle" && !boolVal(def_elem.Arg) {
		st.appendString("NO CYCLE")
	} else if def_elem.Defname == "increment" {
		st.appendString("INCREMENT ")
		deparseNumericOnly(st, def_elem.Arg)
	} else if def_elem.Defname == "maxvalue" && def_elem.Arg != nil {
		st.appendString("MAXVALUE ")
		deparseNumericOnly(st, def_elem.Arg)
	} else if def_elem.Defname == "maxvalue" && def_elem.Arg == nil {
		st.appendString("NO MAXVALUE")
	} else if def_elem.Defname == "minvalue" && def_elem.Arg != nil {
		st.appendString("MINVALUE ")
		deparseNumericOnly(st, def_elem.Arg)
	} else if def_elem.Defname == "minvalue" && def_elem.Arg == nil {
		st.appendString("NO MINVALUE")
	} else if def_elem.Defname == "owned_by" {
		st.appendString("OWNED BY ")
		deparseAnyName(st, def_elem.Arg.GetList().Items)
	} else if def_elem.Defname == "sequence_name" {
		st.appendString("SEQUENCE NAME ")
		deparseAnyName(st, def_elem.Arg.GetList().Items)
	} else if def_elem.Defname == "start" {
		st.appendString("START ")
		deparseNumericOnly(st, def_elem.Arg)
	} else if def_elem.Defname == "restart" && def_elem.Arg == nil {
		st.appendString("RESTART")
	} else if def_elem.Defname == "restart" && def_elem.Arg != nil {
		st.appendString("RESTART ")
		deparseNumericOnly(st, def_elem.Arg)
	}
}

func deparseSeqOptList(st *state, options []*ast.Node) {
	for _, item := range options {
		deparseSeqOptElem(st, item.GetDefElem())
		st.appendChar(' ')
	}
}

func deparseOptSeqOptList(st *state, options []*ast.Node) {
	if len(options) > 0 {
		deparseSeqOptList(st, options)
	}
}

func deparseOptParenthesizedSeqOptList(st *state, options []*ast.Node) {
	if len(options) > 0 {
		st.appendChar('(')
		deparseSeqOptList(st, options)
		st.appendChar(')')
	}
}

func deparseOptDropBehavior(st *state, behavior ast.DropBehavior) {
	switch behavior {
	case ast.DropBehavior_DROP_RESTRICT:
		// Default
	case ast.DropBehavior_DROP_CASCADE:
		st.appendString("CASCADE ")
	}
}

func deparseAnyOperator(st *state, op []*ast.Node) {
	if len(op) == 2 {
		st.appendString(quoteIdentifier(strVal(op[0])))
		st.appendChar('.')
		st.appendString(strVal(op[len(op)-1]))
	} else if len(op) == 1 {
		st.appendString(strVal(op[len(op)-1]))
	}
}

func deparseQualOp(st *state, op []*ast.Node) {
	if len(op) == 1 && isOp(strVal(op[0])) {
		st.appendString(strVal(op[0]))
	} else {
		st.appendString("OPERATOR(")
		deparseAnyOperator(st, op)
		st.appendString(")")
	}
}

func deparseSubqueryOp(st *state, op []*ast.Node) {
	if len(op) == 1 && strVal(op[0]) == "~~" {
		st.appendString("LIKE")
	} else if len(op) == 1 && strVal(op[0]) == "!~~" {
		st.appendString("NOT LIKE")
	} else if len(op) == 1 && strVal(op[0]) == "~~*" {
		st.appendString("ILIKE")
	} else if len(op) == 1 && strVal(op[0]) == "!~~*" {
		st.appendString("NOT ILIKE")
	} else if len(op) == 1 && isOp(strVal(op[0])) {
		st.appendString(strVal(op[0]))
	} else {
		st.appendString("OPERATOR(")
		deparseAnyOperator(st, op)
		st.appendString(")")
	}
}

func deparseGenericDefElemName(st *state, in string) {
	val := []byte(in)
	for i, c := range val {
		if c >= 'a' && c <= 'z' {
			val[i] = c - ('a' - 'A')
		}
	}
	st.appendString(string(val))
}

func deparseDefArg(st *state, arg *ast.Node, is_operator_def_arg bool) {
	if arg.GetTypeName() != nil { // func_type
		deparseTypeName(st, arg.GetTypeName())
	} else if arg.GetList() != nil { // qual_all_Op
		l := arg.GetList().Items

		// Schema qualified operator
		if len(l) == 2 {
			st.appendString("OPERATOR(")
			deparseAnyOperator(st, l)
			st.appendChar(')')
		} else if len(l) == 1 {
			st.appendString(strVal(l[0]))
		}
	} else if arg.GetFloat() != nil || arg.GetInteger() != nil { // NumericOnly
		deparseValue(st, arg, contextNone)
	} else if arg.GetString_() != nil {
		s := strVal(arg)
		if !is_operator_def_arg && arg.GetString_() != nil && s == "none" { // NONE
			st.appendString("NONE")
		} else if isReservedKeyword(s) { // reserved_keyword
			st.appendString(s)
		} else { // Sconst
			deparseStringLiteral(st, s)
		}
	}
}

func deparseDefinition(st *state, options []*ast.Node) {
	st.appendChar('(')
	for i, item := range options {
		def_elem := item.GetDefElem()
		st.appendString(quoteIdentifier(def_elem.Defname))
		if def_elem.Arg != nil {
			st.appendString(" = ")
			deparseDefArg(st, def_elem.Arg, false)
		}

		if i < len(options)-1 {
			st.appendString(", ")
		}
	}
	st.appendChar(')')
}

func deparseOptDefinition(st *state, options []*ast.Node) {
	if len(options) > 0 {
		st.appendString("WITH ")
		deparseDefinition(st, options)
	}
}

func deparseCreateGenericOptions(st *state, options []*ast.Node) {
	if len(options) == 0 {
		return
	}

	st.appendString("OPTIONS (")
	for i, item := range options {
		def_elem := item.GetDefElem()
		st.appendString(quoteIdentifier(def_elem.Defname))
		st.appendChar(' ')
		deparseStringLiteral(st, strVal(def_elem.Arg))
		if i < len(options)-1 {
			st.appendString(", ")
		}
	}
	st.appendString(")")
}

func deparseCommonFuncOptItem(st *state, def_elem *ast.DefElem) {
	if def_elem.Defname == "strict" && boolVal(def_elem.Arg) {
		st.appendString("RETURNS NULL ON NULL INPUT")
	} else if def_elem.Defname == "strict" && !boolVal(def_elem.Arg) {
		st.appendString("CALLED ON NULL INPUT")
	} else if def_elem.Defname == "volatility" && strVal(def_elem.Arg) == "immutable" {
		st.appendString("IMMUTABLE")
	} else if def_elem.Defname == "volatility" && strVal(def_elem.Arg) == "stable" {
		st.appendString("STABLE")
	} else if def_elem.Defname == "volatility" && strVal(def_elem.Arg) == "volatile" {
		st.appendString("VOLATILE")
	} else if def_elem.Defname == "security" && boolVal(def_elem.Arg) {
		st.appendString("SECURITY DEFINER")
	} else if def_elem.Defname == "security" && !boolVal(def_elem.Arg) {
		st.appendString("SECURITY INVOKER")
	} else if def_elem.Defname == "leakproof" && boolVal(def_elem.Arg) {
		st.appendString("LEAKPROOF")
	} else if def_elem.Defname == "leakproof" && !boolVal(def_elem.Arg) {
		st.appendString("NOT LEAKPROOF")
	} else if def_elem.Defname == "cost" {
		st.appendString("COST ")
		deparseValue(st, def_elem.Arg, contextNone)
	} else if def_elem.Defname == "rows" {
		st.appendString("ROWS ")
		deparseValue(st, def_elem.Arg, contextNone)
	} else if def_elem.Defname == "support" {
		st.appendString("SUPPORT ")
		deparseAnyName(st, def_elem.Arg.GetList().Items)
	} else if def_elem.Defname == "set" && def_elem.Arg.GetVariableSetStmt() != nil { // FunctionSetResetClause
		deparseVariableSetStmt(st, def_elem.Arg.GetVariableSetStmt())
	} else if def_elem.Defname == "parallel" {
		st.appendString("PARALLEL ")
		st.appendString(quoteIdentifier(strVal(def_elem.Arg)))
	}
}

func deparseNonReservedWordOrSconst(st *state, val string) {
	const NAMEDATALEN = 64

	if len(val) == 0 {
		st.appendString("''")
	} else if len(val) >= NAMEDATALEN {
		deparseStringLiteral(st, val)
	} else {
		st.appendString(quoteIdentifier(val))
	}
}

func deparseFuncAs(st *state, l []*ast.Node) {
	for i, item := range l {
		strval := strVal(item)
		if !strings.Contains(strval, "$$") {
			st.appendString("$$")
			st.appendString(strval)
			st.appendString("$$")
		} else {
			deparseStringLiteral(st, strval)
		}

		if i < len(l)-1 {
			st.appendString(", ")
		}
	}
}

func deparseCreateFuncOptItem(st *state, def_elem *ast.DefElem) {
	if def_elem.Defname == "as" {
		st.appendString("AS ")
		deparseFuncAs(st, def_elem.Arg.GetList().Items)
	} else if def_elem.Defname == "language" {
		st.appendString("LANGUAGE ")
		deparseNonReservedWordOrSconst(st, strVal(def_elem.Arg))
	} else if def_elem.Defname == "transform" {
		l := def_elem.Arg.GetList().Items
		st.appendString("TRANSFORM ")
		for i, item := range l {
			st.appendString("FOR TYPE ")
			deparseTypeName(st, item.GetTypeName())
			if i < len(l)-1 {
				st.appendString(", ")
			}
		}
	} else if def_elem.Defname == "window" {
		st.appendString("WINDOW")
	} else {
		deparseCommonFuncOptItem(st, def_elem)
	}
}

func deparseAlterGenericOptions(st *state, options []*ast.Node) {
	st.appendString("OPTIONS (")
	for i, item := range options {
		def_elem := item.GetDefElem()
		switch def_elem.Defaction {
		case ast.DefElemAction_DEFELEM_UNSPEC:
			st.appendString(quoteIdentifier(def_elem.Defname))
			st.appendChar(' ')
			deparseStringLiteral(st, strVal(def_elem.Arg))
		case ast.DefElemAction_DEFELEM_SET:
			st.appendString("SET ")
			st.appendString(quoteIdentifier(def_elem.Defname))
			st.appendChar(' ')
			deparseStringLiteral(st, strVal(def_elem.Arg))
		case ast.DefElemAction_DEFELEM_ADD:
			st.appendString("ADD ")
			st.appendString(quoteIdentifier(def_elem.Defname))
			st.appendChar(' ')
			deparseStringLiteral(st, strVal(def_elem.Arg))
		case ast.DefElemAction_DEFELEM_DROP:
			st.appendString("DROP ")
			st.appendString(quoteIdentifier(def_elem.Defname))
		}

		if i < len(options)-1 {
			st.appendString(", ")
		}
	}
	st.appendString(") ")
}

func deparseFuncName(st *state, func_name []*ast.Node) {
	for i, item := range func_name {
		st.appendString(quoteIdentifier(strVal(item)))
		if i < len(func_name)-1 {
			st.appendChar('.')
		}
	}
}

func deparseFunctionWithArgtypes(st *state, object_with_args *ast.ObjectWithArgs) {
	deparseFuncName(st, object_with_args.Objname)

	if !object_with_args.ArgsUnspecified {
		st.appendChar('(')
		objargs := object_with_args.Objargs
		if len(object_with_args.Objfuncargs) > 0 {
			objargs = object_with_args.Objfuncargs
		}

		for i, item := range objargs {
			if item.GetFunctionParameter() != nil {
				deparseFunctionParameter(st, item.GetFunctionParameter())
			} else {
				deparseTypeName(st, item.GetTypeName())
			}
			if i < len(objargs)-1 {
				st.appendString(", ")
			}
		}
		st.appendChar(')')
	}
}

func deparseFunctionWithArgtypesList(st *state, l []*ast.Node) {
	for i, item := range l {
		deparseFunctionWithArgtypes(st, item.GetObjectWithArgs())
		if i < len(l)-1 {
			st.appendString(", ")
		}
	}
}

func deparseOperatorWithArgtypes(st *state, object_with_args *ast.ObjectWithArgs) {
	deparseAnyOperator(st, object_with_args.Objname)

	st.appendChar('(')
	if object_with_args.Objargs[0] == nil || object_with_args.Objargs[0].Node == nil {
		st.appendString("NONE")
	} else {
		deparseTypeName(st, object_with_args.Objargs[0].GetTypeName())
	}
	st.appendString(", ")
	if object_with_args.Objargs[1] == nil || object_with_args.Objargs[1].Node == nil {
		st.appendString("NONE")
	} else {
		deparseTypeName(st, object_with_args.Objargs[1].GetTypeName())
	}
	st.appendChar(')')
}

func deparseAggrArgs(st *state, aggr_args []*ast.Node) {
	args := aggr_args[0].GetList().GetItems()
	order_by_pos := intVal(aggr_args[1])

	st.appendChar('(')
	if len(args) == 0 {
		st.appendChar('*')
	} else {
		for i, item := range args {
			if i == order_by_pos {
				if i > 0 {
					st.appendChar(' ')
				}
				st.appendString("ORDER BY ")
			} else if i > 0 {
				st.appendString(", ")
			}

			deparseFunctionParameter(st, item.GetFunctionParameter())
		}

		// Repeat the last direct arg as a ordered arg to handle the
		// simplification done by makeOrderedSetArgs in gram.y
		if order_by_pos == len(args) {
			st.appendString(" ORDER BY ")
			deparseFunctionParameter(st, args[len(args)-1].GetFunctionParameter())
		}
	}
	st.appendChar(')')
}

func deparseAggregateWithArgtypes(st *state, object_with_args *ast.ObjectWithArgs) {
	deparseFuncName(st, object_with_args.Objname)

	st.appendChar('(')
	if len(object_with_args.Objargs) == 0 && len(object_with_args.Objfuncargs) == 0 {
		st.appendChar('*')
	} else {
		objargs := object_with_args.Objargs
		if len(object_with_args.Objfuncargs) > 0 {
			objargs = object_with_args.Objfuncargs
		}

		for i, item := range objargs {
			if item.GetFunctionParameter() != nil {
				deparseFunctionParameter(st, item.GetFunctionParameter())
			} else {
				deparseTypeName(st, item.GetTypeName())
			}
			if i < len(objargs)-1 {
				st.appendString(", ")
			}
		}
	}
	st.appendChar(')')
}

// "opt_column_and_period_list" and "optionalPeriodName" in gram.y
func deparseColumnListWithPeriod(st *state, columns []*ast.Node) {
	for i, column := range columns {
		if i == len(columns)-1 {
			st.appendString("PERIOD ")
		}
		st.appendString(quoteIdentifier(strVal(column)))
		if i < len(columns)-1 {
			st.appendString(", ")
		}
	}
}

func deparseColumnList(st *state, columns []*ast.Node) {
	for i, item := range columns {
		st.appendString(quoteIdentifier(strVal(item)))
		if i < len(columns)-1 {
			st.appendString(", ")
		}
	}
}

func deparseOptTemp(st *state, relpersistence string) {
	switch relpersistence {
	case "p":
		// Default
	case "u":
		st.appendString("UNLOGGED ")
	case "t":
		st.appendString("TEMPORARY ")
	}
}

func deparseRelationExprList(st *state, relation_exprs []*ast.Node) {
	for i, item := range relation_exprs {
		deparseRangeVar(st, item.GetRangeVar(), contextNone)
		if i < len(relation_exprs)-1 {
			st.appendString(", ")
		}
	}
}

func deparseHandlerName(st *state, handler_name []*ast.Node) {
	for i, item := range handler_name {
		st.appendString(quoteIdentifier(strVal(item)))
		if i < len(handler_name)-1 {
			st.appendChar('.')
		}
	}
}

func deparseFdwOptions(st *state, fdw_options []*ast.Node) {
	for i, item := range fdw_options {
		def_elem := item.GetDefElem()
		if def_elem.Defname == "handler" && def_elem.Arg != nil {
			st.appendString("HANDLER ")
			deparseHandlerName(st, def_elem.Arg.GetList().Items)
		} else if def_elem.Defname == "handler" && def_elem.Arg == nil {
			st.appendString("NO HANDLER ")
		} else if def_elem.Defname == "validator" && def_elem.Arg != nil {
			st.appendString("VALIDATOR ")
			deparseHandlerName(st, def_elem.Arg.GetList().Items)
		} else if def_elem.Defname == "validator" && def_elem.Arg == nil {
			st.appendString("NO VALIDATOR ")
		}

		if i < len(fdw_options)-1 {
			st.appendChar(' ')
		}
	}
}

func deparseTypeList(st *state, type_list []*ast.Node) {
	for i, item := range type_list {
		deparseTypeName(st, item.GetTypeName())
		if i < len(type_list)-1 {
			st.appendString(", ")
		}
	}
}

func deparseOptBooleanOrString(st *state, s string) {
	// The C guards against NULL, which no call site produces (all pass
	// strVal); an empty string must fall through to print ''.
	if s == "true" {
		st.appendString("TRUE")
	} else if s == "false" {
		st.appendString("FALSE")
	} else if s == "on" {
		st.appendString("ON")
	} else if s == "off" {
		st.appendString("OFF")
	} else {
		deparseNonReservedWordOrSconst(st, s)
	}
}

func deparseOptBoolean(st *state, node *ast.Node) {
	if node == nil {
		return
	}

	switch {
	case node.GetString_() != nil:
		st.appendf(" %s", strVal(node))
	case node.GetInteger() != nil:
		st.appendf(" %d", intVal(node))
	case node.GetBoolean() != nil:
		s := "FALSE"
		if boolVal(node) {
			s = "TRUE"
		}
		st.appendf(" %s", s)
	}
}

func optBooleanValue(node *ast.Node) bool {
	if node == nil {
		return true
	}

	switch {
	case node.GetString_() != nil:
		// Longest valid string is "off\0"
		lower := strVal(node)
		if len(lower) > 3 {
			lower = lower[:3]
		}

		if lower == "on" {
			return true
		} else if lower == "off" {
			return false
		}

		// No sane way to handle this.
		return false
	case node.GetInteger() != nil:
		return intVal(node) != 0
	case node.GetBoolean() != nil:
		return boolVal(node)
	default:
		return false
	}
}

func deparseVarName(st *state, s string) {
	deparseColId(st, s)
}

func deparseVarList(st *state, l []*ast.Node) {
	for i, item := range l {
		if item.GetParamRef() != nil {
			deparseParamRef(st, item.GetParamRef())
		} else if item.GetAConst() != nil {
			a_const := item.GetAConst()
			if a_const.GetIval() != nil || a_const.GetFval() != nil {
				deparseNumericOnly(st, aConstVal(a_const))
			} else if a_const.GetSval() != nil {
				deparseOptBooleanOrString(st, a_const.GetSval().GetSval())
			}
		} else if item.GetTypeCast() != nil {
			deparseTypeCast(st, item.GetTypeCast(), contextSetStatement)
		}

		if i < len(l)-1 {
			st.appendString(", ")
		}
	}
}

func deparseTransactionModeList(st *state, l []*ast.Node) {
	for i, item := range l {
		def_elem := item.GetDefElem()

		if def_elem.Defname == "transaction_isolation" {
			s := def_elem.Arg.GetAConst().GetSval().GetSval()
			st.appendString("ISOLATION LEVEL ")
			if s == "read uncommitted" {
				st.appendString("READ UNCOMMITTED")
			} else if s == "read committed" {
				st.appendString("READ COMMITTED")
			} else if s == "repeatable read" {
				st.appendString("REPEATABLE READ")
			} else if s == "serializable" {
				st.appendString("SERIALIZABLE")
			}
		} else if def_elem.Defname == "transaction_read_only" && int(def_elem.Arg.GetAConst().GetIval().GetIval()) == 1 {
			st.appendString("READ ONLY")
		} else if def_elem.Defname == "transaction_read_only" && int(def_elem.Arg.GetAConst().GetIval().GetIval()) == 0 {
			st.appendString("READ WRITE")
		} else if def_elem.Defname == "transaction_deferrable" && int(def_elem.Arg.GetAConst().GetIval().GetIval()) == 1 {
			st.appendString("DEFERRABLE")
		} else if def_elem.Defname == "transaction_deferrable" && int(def_elem.Arg.GetAConst().GetIval().GetIval()) == 0 {
			st.appendString("NOT DEFERRABLE")
		}

		if i < len(l)-1 {
			st.appendString(", ")
		}
	}
}

func deparseAlterIdentityColumnOptionList(st *state, l []*ast.Node) {
	for i, item := range l {
		def_elem := item.GetDefElem()
		if def_elem.Defname == "restart" && def_elem.Arg == nil {
			st.appendString("RESTART")
		} else if def_elem.Defname == "restart" && def_elem.Arg != nil {
			st.appendString("RESTART ")
			deparseNumericOnly(st, def_elem.Arg)
		} else if def_elem.Defname == "generated" {
			st.appendString("SET GENERATED ")
			if def_elem.Arg == nil {
				st.errorf("deparse: missing argument for identity generation specification")
			}
			if intVal(def_elem.Arg) == 'a' {
				st.appendString("ALWAYS")
			} else if intVal(def_elem.Arg) == 'd' {
				st.appendString("BY DEFAULT")
			}
		} else {
			st.appendString("SET ")
			deparseSeqOptElem(st, def_elem)
		}
		if i < len(l)-1 {
			st.appendChar(' ')
		}
	}
}

func deparseRelOptions(st *state, l []*ast.Node) {
	st.appendChar('(')
	for i, item := range l {
		def_elem := item.GetDefElem()
		if def_elem.Defnamespace != "" {
			st.appendString(quoteIdentifier(def_elem.Defnamespace))
			st.appendChar('.')
		}
		if def_elem.Defname != "" {
			st.appendString(quoteIdentifier(def_elem.Defname))
		}
		if def_elem.Defname != "" && def_elem.Arg != nil {
			st.appendChar('=')
		}
		if def_elem.Arg != nil {
			deparseDefArg(st, def_elem.Arg, false)
		}

		if i < len(l)-1 {
			st.appendString(", ")
		}
	}
	st.appendChar(')')
}

func deparseOptWith(st *state, l []*ast.Node) {
	if len(l) > 0 {
		st.appendString("WITH ")
		deparseRelOptions(st, l)
		st.appendChar(' ')
	}
}

func deparseTargetList(st *state, l []*ast.Node) {
	for i, item := range l {
		res_target := item.GetResTarget()

		if res_target.Val == nil {
			st.errorf("deparse: error in deparseTargetList: ResTarget without val")
		} else if res_target.Val.GetColumnRef() != nil {
			deparseColumnRef(st, res_target.Val.GetColumnRef())
		} else {
			deparseExpr(st, res_target.Val, contextAExpr)
		}

		if res_target.Name != "" {
			st.appendString(" AS ")
			st.appendString(quoteIdentifier(res_target.Name))
		}

		if i < len(l)-1 {
			st.appendCommaAndPart()
		}
	}
}

func deparseInsertColumnList(st *state, l []*ast.Node) {
	for i, item := range l {
		res_target := item.GetResTarget()
		st.appendString(quoteIdentifier(res_target.Name))
		deparseOptIndirection(st, res_target.Indirection, 0)
		if i < len(l)-1 {
			st.appendString(", ")
		}
	}
}

func deparseXmlAttributeList(st *state, l []*ast.Node) {
	for i, item := range l {
		res_target := item.GetResTarget()

		deparseExpr(st, res_target.Val, contextAExpr)

		if res_target.Name != "" {
			st.appendString(" AS ")
			st.appendString(quoteIdentifier(res_target.Name))
		}

		if i < len(l)-1 {
			st.appendString(", ")
		}
	}
}

func deparseXmlNamespaceList(st *state, l []*ast.Node) {
	for i, item := range l {
		res_target := item.GetResTarget()

		if res_target.Name == "" {
			st.appendString("DEFAULT ")
		}

		deparseExpr(st, res_target.Val, contextNone /* b_expr */)

		if res_target.Name != "" {
			st.appendString(" AS ")
			st.appendString(quoteIdentifier(res_target.Name))
		}

		if i < len(l)-1 {
			st.appendString(", ")
		}
	}
}

func deparseTableRef(st *state, node *ast.Node) {
	switch {
	case node.GetRangeVar() != nil:
		deparseRangeVar(st, node.GetRangeVar(), contextNone)
	case node.GetRangeTableSample() != nil:
		deparseRangeTableSample(st, node.GetRangeTableSample())
	case node.GetRangeFunction() != nil:
		deparseRangeFunction(st, node.GetRangeFunction())
	case node.GetRangeTableFunc() != nil:
		deparseRangeTableFunc(st, node.GetRangeTableFunc())
	case node.GetRangeSubselect() != nil:
		deparseRangeSubselect(st, node.GetRangeSubselect())
	case node.GetJoinExpr() != nil:
		deparseJoinExpr(st, node.GetJoinExpr())
	case node.GetJsonTable() != nil:
		deparseJsonTable(st, node.GetJsonTable())
	}
}

func deparseFromList(st *state, l []*ast.Node) {
	for i, item := range l {
		deparseTableRef(st, item)
		if i < len(l)-1 {
			st.appendCommaAndPart()
		}
	}
}
