// Ported from postgres_deparse.c (libpg_query 17-6.2.2).
// Expressions: BoolExpr, CaseExpr, TypeCast/TypeName, constants; first DDL statements.
package deparse

import (
	"github.com/sqlc-dev/oliphant/ast"
)

func deparseBoolExpr(st *state, bool_expr *ast.BoolExpr) {
	switch bool_expr.Boolop {
	case ast.BoolExprType_AND_EXPR:
		for i, item := range bool_expr.Args {
			// Put parantheses around AND + OR nodes that are inside
			need_parens := item.GetBoolExpr() != nil && (item.GetBoolExpr().Boolop == ast.BoolExprType_AND_EXPR || item.GetBoolExpr().Boolop == ast.BoolExprType_OR_EXPR)

			if need_parens {
				st.appendChar('(')
			}

			deparseExpr(st, item, contextAExpr)

			if need_parens {
				st.appendChar(')')
			}

			if i < len(bool_expr.Args)-1 {
				st.appendPart(true)
				st.appendString("AND ")
			}
		}
		return
	case ast.BoolExprType_OR_EXPR:
		for i, item := range bool_expr.Args {
			// Put parantheses around AND + OR nodes that are inside
			need_parens := item.GetBoolExpr() != nil && (item.GetBoolExpr().Boolop == ast.BoolExprType_AND_EXPR || item.GetBoolExpr().Boolop == ast.BoolExprType_OR_EXPR)

			if need_parens {
				st.appendChar('(')
			}

			deparseExpr(st, item, contextAExpr)

			if need_parens {
				st.appendChar(')')
			}

			if i < len(bool_expr.Args)-1 {
				st.appendString(" OR ")
			}
		}
		return
	case ast.BoolExprType_NOT_EXPR:
		need_parens := bool_expr.Args[0].GetBoolExpr() != nil && (bool_expr.Args[0].GetBoolExpr().Boolop == ast.BoolExprType_AND_EXPR || bool_expr.Args[0].GetBoolExpr().Boolop == ast.BoolExprType_OR_EXPR)
		st.appendString("NOT ")
		if need_parens {
			st.appendChar('(')
		}
		deparseExpr(st, bool_expr.Args[0], contextAExpr)
		if need_parens {
			st.appendChar(')')
		}
		return
	}
}

func deparseAStar(st *state, a_star *ast.A_Star) {
	st.appendChar('*')
}

func deparseCollateClause(st *state, collate_clause *ast.CollateClause) {
	if collate_clause.Arg != nil {
		need_parens := collate_clause.Arg.GetAExpr() != nil
		if need_parens {
			st.appendChar('(')
		}
		deparseExpr(st, collate_clause.Arg, contextAExpr)
		if need_parens {
			st.appendChar(')')
		}
		st.appendChar(' ')
	}
	st.appendString("COLLATE ")
	deparseAnyName(st, collate_clause.Collname)
}

func deparseSortBy(st *state, sort_by *ast.SortBy) {
	deparseExpr(st, sort_by.Node, contextAExpr)
	st.appendChar(' ')

	switch sort_by.SortbyDir {
	case ast.SortByDir_SORTBY_DEFAULT:
	case ast.SortByDir_SORTBY_ASC:
		st.appendString("ASC ")
	case ast.SortByDir_SORTBY_DESC:
		st.appendString("DESC ")
	case ast.SortByDir_SORTBY_USING:
		st.appendString("USING ")
		deparseQualOp(st, sort_by.UseOp)
	}

	switch sort_by.SortbyNulls {
	case ast.SortByNulls_SORTBY_NULLS_DEFAULT:
	case ast.SortByNulls_SORTBY_NULLS_FIRST:
		st.appendString("NULLS FIRST ")
	case ast.SortByNulls_SORTBY_NULLS_LAST:
		st.appendString("NULLS LAST ")
	}

	st.removeTrailingSpace()
}

func deparseParamRef(st *state, param_ref *ast.ParamRef) {
	if param_ref.Number == 0 {
		st.appendChar('?')
	} else {
		st.appendf("$%d", param_ref.Number)
	}
}

func deparseSQLValueFunction(st *state, sql_value_function *ast.SQLValueFunction) {
	switch sql_value_function.Op {
	case ast.SQLValueFunctionOp_SVFOP_CURRENT_DATE:
		st.appendString("current_date")
	case ast.SQLValueFunctionOp_SVFOP_CURRENT_TIME:
		st.appendString("current_time")
	case ast.SQLValueFunctionOp_SVFOP_CURRENT_TIME_N:
		st.appendString("current_time") // with precision
	case ast.SQLValueFunctionOp_SVFOP_CURRENT_TIMESTAMP:
		st.appendString("current_timestamp")
	case ast.SQLValueFunctionOp_SVFOP_CURRENT_TIMESTAMP_N:
		st.appendString("current_timestamp") // with precision
	case ast.SQLValueFunctionOp_SVFOP_LOCALTIME:
		st.appendString("localtime")
	case ast.SQLValueFunctionOp_SVFOP_LOCALTIME_N:
		st.appendString("localtime") // with precision
	case ast.SQLValueFunctionOp_SVFOP_LOCALTIMESTAMP:
		st.appendString("localtimestamp")
	case ast.SQLValueFunctionOp_SVFOP_LOCALTIMESTAMP_N:
		st.appendString("localtimestamp") // with precision
	case ast.SQLValueFunctionOp_SVFOP_CURRENT_ROLE:
		st.appendString("current_role")
	case ast.SQLValueFunctionOp_SVFOP_CURRENT_USER:
		st.appendString("current_user")
	case ast.SQLValueFunctionOp_SVFOP_USER:
		st.appendString("user")
	case ast.SQLValueFunctionOp_SVFOP_SESSION_USER:
		st.appendString("session_user")
	case ast.SQLValueFunctionOp_SVFOP_CURRENT_CATALOG:
		st.appendString("current_catalog")
	case ast.SQLValueFunctionOp_SVFOP_CURRENT_SCHEMA:
		st.appendString("current_schema")
	}

	if sql_value_function.Typmod != -1 {
		st.appendf("(%d)", sql_value_function.Typmod)
	}
}

func deparseWithClause(st *state, with_clause *ast.WithClause) {
	st.appendPartGroup("WITH", partNoIndent)
	if with_clause.Recursive {
		st.appendString("RECURSIVE ")
	}

	for i, item := range with_clause.Ctes {
		deparseCommonTableExpr(st, item.GetCommonTableExpr())
		if i < len(with_clause.Ctes)-1 {
			st.appendString(", ")
		}
	}

	st.removeTrailingSpace()
}

func deparseJoinExpr(st *state, join_expr *ast.JoinExpr) {
	need_alias_parens := join_expr.Alias != nil
	need_rarg_parens := join_expr.Rarg.GetJoinExpr() != nil && join_expr.Rarg.GetJoinExpr().Alias == nil

	if need_alias_parens {
		st.appendChar('(')
	}

	deparseTableRef(st, join_expr.Larg)
	st.appendPart(true)

	if join_expr.IsNatural {
		st.appendString("NATURAL ")
	}

	switch join_expr.Jointype {
	case ast.JoinType_JOIN_INNER: /* matching tuple pairs only */
		if !join_expr.IsNatural && join_expr.Quals == nil && len(join_expr.UsingClause) == 0 {
			st.appendString("CROSS ")
		}
	case ast.JoinType_JOIN_LEFT: /* pairs + unmatched LHS tuples */
		st.appendString("LEFT ")
	case ast.JoinType_JOIN_FULL: /* pairs + unmatched LHS + unmatched RHS */
		st.appendString("FULL ")
	case ast.JoinType_JOIN_RIGHT: /* pairs + unmatched RHS tuples */
		st.appendString("RIGHT ")
	case ast.JoinType_JOIN_SEMI,
		ast.JoinType_JOIN_ANTI,
		ast.JoinType_JOIN_RIGHT_SEMI,
		ast.JoinType_JOIN_RIGHT_ANTI,
		ast.JoinType_JOIN_UNIQUE_OUTER,
		ast.JoinType_JOIN_UNIQUE_INNER:
		// Only used by the planner/executor, not seen in parser output
	}

	st.appendString("JOIN ")

	if need_rarg_parens {
		st.appendChar('(')
	}
	deparseTableRef(st, join_expr.Rarg)
	if need_rarg_parens {
		st.appendChar(')')
	}
	st.appendChar(' ')

	if join_expr.Quals != nil {
		st.appendString("ON ")
		deparseExpr(st, join_expr.Quals, contextAExpr)
		st.appendChar(' ')
	}

	if len(join_expr.UsingClause) > 0 {
		st.appendString("USING (")
		deparseNameList(st, join_expr.UsingClause)
		st.appendString(") ")

		if join_expr.JoinUsingAlias != nil {
			st.appendString("AS ")
			st.appendString(join_expr.JoinUsingAlias.Aliasname)
		}
	}

	if need_alias_parens {
		st.appendString(") ")
	}

	if join_expr.Alias != nil {
		deparseAlias(st, join_expr.Alias)
	}

	st.removeTrailingSpace()
}

func deparseCTESearchClause(st *state, search_clause *ast.CTESearchClause) {
	st.appendString(" SEARCH ")
	if search_clause.SearchBreadthFirst {
		st.appendString("BREADTH ")
	} else {
		st.appendString("DEPTH ")
	}

	st.appendString("FIRST BY ")

	if len(search_clause.SearchColList) > 0 {
		deparseColumnList(st, search_clause.SearchColList)
	}

	st.appendString(" SET ")
	st.appendString(quoteIdentifier(search_clause.SearchSeqColumn))
}

func deparseCTECycleClause(st *state, cycle_clause *ast.CTECycleClause) {
	st.appendString(" CYCLE ")

	if len(cycle_clause.CycleColList) > 0 {
		deparseColumnList(st, cycle_clause.CycleColList)
	}

	st.appendString(" SET ")
	st.appendString(quoteIdentifier(cycle_clause.CycleMarkColumn))

	if cycle_clause.CycleMarkValue != nil {
		st.appendString(" TO ")
		deparseAexprConst(st, cycle_clause.CycleMarkValue)
	}

	if cycle_clause.CycleMarkDefault != nil {
		st.appendString(" DEFAULT ")
		deparseAexprConst(st, cycle_clause.CycleMarkDefault)
	}

	st.appendString(" USING ")
	st.appendString(quoteIdentifier(cycle_clause.CyclePathColumn))
}

func deparseCommonTableExpr(st *state, cte *ast.CommonTableExpr) {
	deparseColId(st, cte.Ctename)

	if len(cte.Aliascolnames) > 0 {
		st.appendChar('(')
		deparseNameList(st, cte.Aliascolnames)
		st.appendChar(')')
	}
	st.appendChar(' ')

	st.appendString("AS ")
	switch cte.Ctematerialized {
	case ast.CTEMaterialize_CTEMaterializeDefault: /* no option specified */
	case ast.CTEMaterialize_CTEMaterializeAlways:
		st.appendString("MATERIALIZED ")
	case ast.CTEMaterialize_CTEMaterializeNever:
		st.appendString("NOT MATERIALIZED ")
	}

	st.appendChar('(')
	deparsePreparableStmt(st, cte.Ctequery)
	st.appendChar(')')

	if cte.SearchClause != nil {
		deparseCTESearchClause(st, cte.SearchClause)
	}
	if cte.CycleClause != nil {
		deparseCTECycleClause(st, cte.CycleClause)
	}
}

func deparseRangeSubselect(st *state, range_subselect *ast.RangeSubselect) {
	if range_subselect.Lateral {
		st.appendString("LATERAL ")
	}

	st.appendChar('(')
	deparseSelectStmt(st, range_subselect.Subquery.GetSelectStmt(), contextNone)
	st.appendChar(')')

	if range_subselect.Alias != nil {
		st.appendChar(' ')
		deparseAlias(st, range_subselect.Alias)
	}
}

func deparseRangeFunction(st *state, range_func *ast.RangeFunction) {
	if range_func.Lateral {
		st.appendString("LATERAL ")
	}

	if range_func.IsRowsfrom {
		st.appendString("ROWS FROM ")
		st.appendChar('(')
		for i, item := range range_func.Functions {
			lfunc := item.GetList().Items
			deparseFuncExprWindowless(st, lfunc[0])
			st.appendChar(' ')
			coldeflist := lfunc[1].GetList().GetItems() // NIL when no coldeflist
			if len(coldeflist) > 0 {
				st.appendString("AS (")
				for i2, item2 := range coldeflist {
					deparseColumnDef(st, item2.GetColumnDef())
					if i2 < len(coldeflist)-1 {
						st.appendString(", ")
					}
				}
				st.appendChar(')')
			}
			if i < len(range_func.Functions)-1 {
				st.appendString(", ")
			}
		}
		st.appendChar(')')
	} else {
		deparseFuncExprWindowless(st, range_func.Functions[0].GetList().Items[0])
	}
	st.appendChar(' ')

	if range_func.Ordinality {
		st.appendString("WITH ORDINALITY ")
	}

	if range_func.Alias != nil {
		deparseAlias(st, range_func.Alias)
		st.appendChar(' ')
	}

	if len(range_func.Coldeflist) > 0 {
		if range_func.Alias == nil {
			st.appendString("AS ")
		}
		st.appendChar('(')
		for i, item := range range_func.Coldeflist {
			deparseColumnDef(st, item.GetColumnDef())
			if i < len(range_func.Coldeflist)-1 {
				st.appendString(", ")
			}
		}
		st.appendChar(')')
	}

	st.removeTrailingSpace()
}

func deparseAArrayExpr(st *state, array_expr *ast.A_ArrayExpr) {
	st.appendString("ARRAY[")
	deparseExprList(st, array_expr.Elements)
	st.appendChar(']')
}

func deparseRowExpr(st *state, row_expr *ast.RowExpr) {
	switch row_expr.RowFormat {
	case ast.CoercionForm_COERCE_EXPLICIT_CALL:
		st.appendString("ROW")
	case ast.CoercionForm_COERCE_SQL_SYNTAX,
		ast.CoercionForm_COERCE_EXPLICIT_CAST:
		// Not present in raw parser output
	case ast.CoercionForm_COERCE_IMPLICIT_CAST:
		// No prefix
	}

	st.appendString("(")
	deparseExprList(st, row_expr.Args)
	st.appendChar(')')
}

func deparseTypeCast(st *state, type_cast *ast.TypeCast, ctx nodeContext) {
	need_parens := needsParensAsBExpr(type_cast.Arg)

	if ctx == contextFuncExpr {
		st.appendString("CAST(")
		deparseExpr(st, type_cast.Arg, contextAExpr)
		st.appendString(" AS ")
		deparseTypeName(st, type_cast.TypeName)
		st.appendChar(')')
		return
	}

	if type_cast.Arg.GetAConst() != nil {
		a_const := type_cast.Arg.GetAConst()

		if len(type_cast.TypeName.Names) == 2 &&
			strVal(type_cast.TypeName.Names[0]) == "pg_catalog" {
			typename := strVal(type_cast.TypeName.Names[1])
			if typename == "bpchar" && len(type_cast.TypeName.Typmods) == 0 {
				st.appendString("char ")
				deparseAConst(st, a_const)
				return
			} else if typename == "bool" && a_const.GetSval() != nil {
				/*
				 * Handle "bool" or "false" in the statement, which is represented as a typecast
				 * (other boolean casts should be represented as a cast, i.e. don't need special handling)
				 */
				const_val := a_const.GetSval().GetSval()
				if const_val == "t" {
					st.appendString("true")
					return
				}
				if const_val == "f" {
					st.appendString("false")
					return
				}
			} else if typename == "interval" && ctx == contextSetStatement && a_const.GetSval() != nil {
				st.appendString("interval ")
				deparseAConst(st, a_const)
				deparseIntervalTypmods(st, type_cast.TypeName)
				return
			}
		}

		// Ensure negative values have wrapping parentheses
		if a_const.GetFval() != nil || (a_const.GetIval() != nil && int(a_const.GetIval().GetIval()) < 0) {
			need_parens = true
		}

		if len(type_cast.TypeName.Names) == 1 &&
			strVal(type_cast.TypeName.Names[0]) == "point" &&
			a_const.Location > type_cast.TypeName.Location {
			st.appendString(" point ")
			deparseAConst(st, a_const)
			return
		}
	}

	if need_parens {
		st.appendChar('(')
	}
	deparseExpr(st, type_cast.Arg, contextNone /* could be either a_expr or b_expr (we could pass this down, but that'd require two kinds of contexts most likely) */)
	if need_parens {
		st.appendChar(')')
	}

	st.appendString("::")
	deparseTypeName(st, type_cast.TypeName)
}

func deparseTypeName(st *state, type_name *ast.TypeName) {
	skip_typmods := false

	if type_name.Setof {
		st.appendString("SETOF ")
	}

	if len(type_name.Names) == 2 && strVal(type_name.Names[0]) == "pg_catalog" {
		name := strVal(type_name.Names[1])
		if name == "bpchar" {
			st.appendString("char")
		} else if name == "varchar" {
			st.appendString("varchar")
		} else if name == "numeric" {
			st.appendString("numeric")
		} else if name == "bool" {
			st.appendString("boolean")
		} else if name == "int2" {
			st.appendString("smallint")
		} else if name == "int4" {
			st.appendString("int")
		} else if name == "int8" {
			st.appendString("bigint")
		} else if name == "real" || name == "float4" {
			st.appendString("real")
		} else if name == "float8" {
			st.appendString("double precision")
		} else if name == "time" {
			st.appendString("time")
		} else if name == "timetz" {
			st.appendString("time ")
			if len(type_name.Typmods) > 0 {
				st.appendChar('(')
				for i, item := range type_name.Typmods {
					deparseSignedIconst(st, aConstVal(item.GetAConst()))
					if i < len(type_name.Typmods)-1 {
						st.appendString(", ")
					}
				}
				st.appendString(") ")
			}
			st.appendString("with time zone")
			skip_typmods = true
		} else if name == "timestamp" {
			st.appendString("timestamp")
		} else if name == "timestamptz" {
			st.appendString("timestamp ")
			if len(type_name.Typmods) > 0 {
				st.appendChar('(')
				for i, item := range type_name.Typmods {
					deparseSignedIconst(st, aConstVal(item.GetAConst()))
					if i < len(type_name.Typmods)-1 {
						st.appendString(", ")
					}
				}
				st.appendString(") ")
			}
			st.appendString("with time zone")
			skip_typmods = true
		} else if name == "interval" && len(type_name.Typmods) == 0 {
			st.appendString("interval")
		} else if name == "interval" && len(type_name.Typmods) >= 1 {
			st.appendString("interval")
			deparseIntervalTypmods(st, type_name)

			skip_typmods = true
		} else {
			st.appendString("pg_catalog.")
			st.appendString(name)
		}
	} else {
		deparseAnyName(st, type_name.Names)
	}

	if len(type_name.Typmods) > 0 && !skip_typmods {
		st.appendChar('(')
		for i, item := range type_name.Typmods {
			if item.GetAConst() != nil {
				deparseAConst(st, item.GetAConst())
			} else if item.GetParamRef() != nil {
				deparseParamRef(st, item.GetParamRef())
			} else if item.GetColumnRef() != nil {
				deparseColumnRef(st, item.GetColumnRef())
			}

			if i < len(type_name.Typmods)-1 {
				st.appendString(", ")
			}
		}
		st.appendChar(')')
	}

	for _, item := range type_name.ArrayBounds {
		st.appendChar('[')
		if item.GetInteger() != nil && intVal(item) != -1 {
			deparseSignedIconst(st, item)
		}
		st.appendChar(']')
	}

	if type_name.PctType {
		st.appendString("%type")
	}
}

func deparseIntervalTypmods(st *state, type_name *ast.TypeName) {
	fields := intVal(aConstVal(type_name.Typmods[0].GetAConst()))

	// This logic is based on intervaltypmodout in timestamp.c
	switch fields {
	case intervalMaskYear:
		st.appendString(" year")
	case intervalMaskMonth:
		st.appendString(" month")
	case intervalMaskDay:
		st.appendString(" day")
	case intervalMaskHour:
		st.appendString(" hour")
	case intervalMaskMinute:
		st.appendString(" minute")
	case intervalMaskSecond:
		st.appendString(" second")
	case intervalMaskYear | intervalMaskMonth:
		st.appendString(" year to month")
	case intervalMaskDay | intervalMaskHour:
		st.appendString(" day to hour")
	case intervalMaskDay | intervalMaskHour | intervalMaskMinute:
		st.appendString(" day to minute")
	case intervalMaskDay | intervalMaskHour | intervalMaskMinute | intervalMaskSecond:
		st.appendString(" day to second")
	case intervalMaskHour | intervalMaskMinute:
		st.appendString(" hour to minute")
	case intervalMaskHour | intervalMaskMinute | intervalMaskSecond:
		st.appendString(" hour to second")
	case intervalMaskMinute | intervalMaskSecond:
		st.appendString(" minute to second")
	case intervalFullRange:
		// Nothing
	default:
	}

	if len(type_name.Typmods) == 2 {
		precision := intVal(aConstVal(type_name.Typmods[1].GetAConst()))
		if precision != intervalFullPrecision {
			st.appendf("(%d)", precision)
		}
	}
}

func deparseNullTest(st *state, null_test *ast.NullTest) {
	// argisrow is always false in raw parser output
	deparseExpr(st, null_test.Arg, contextAExpr)
	switch null_test.Nulltesttype {
	case ast.NullTestType_IS_NULL:
		st.appendString(" IS NULL")
	case ast.NullTestType_IS_NOT_NULL:
		st.appendString(" IS NOT NULL")
	}
}

func deparseCaseExpr(st *state, case_expr *ast.CaseExpr) {
	var parent_level *nestingLevel

	st.appendString("CASE ")

	if case_expr.Arg != nil {
		deparseExpr(st, case_expr.Arg, contextAExpr)
		st.appendChar(' ')
	}

	parent_level = st.increaseNestingLevel()

	for _, item := range case_expr.Args {
		deparseCaseWhen(st, item.GetCaseWhen())
	}

	if case_expr.Defresult != nil {
		st.appendPartGroup("ELSE", partIndent)
		deparseExpr(st, case_expr.Defresult, contextAExpr)
	}

	st.appendChar(' ')
	st.decreaseNestingLevel(parent_level)

	st.appendString("END")
}

func deparseCaseWhen(st *state, case_when *ast.CaseWhen) {
	st.appendPartGroup("WHEN", partIndent)
	deparseExpr(st, case_when.Expr, contextAExpr)
	st.appendString(" THEN ")
	deparseExpr(st, case_when.Result, contextAExpr)
}

func deparseAIndirection(st *state, a_indirection *ast.A_Indirection) {
	need_parens := a_indirection.Arg.GetAIndirection() != nil ||
		a_indirection.Arg.GetFuncCall() != nil ||
		a_indirection.Arg.GetAExpr() != nil ||
		a_indirection.Arg.GetTypeCast() != nil ||
		a_indirection.Arg.GetRowExpr() != nil ||
		a_indirection.Arg.GetAArrayExpr() != nil ||
		(a_indirection.Arg.GetColumnRef() != nil && a_indirection.Indirection[0].GetAIndices() == nil) ||
		a_indirection.Arg.GetJsonFuncExpr() != nil

	if need_parens {
		st.appendChar('(')
	}

	if need_parens {
		deparseExpr(st, a_indirection.Arg, contextAExpr)
	} else {
		deparseExpr(st, a_indirection.Arg, contextNone)
	}

	if need_parens {
		st.appendChar(')')
	}

	deparseOptIndirection(st, a_indirection.Indirection, 0)
}

func deparseAIndices(st *state, a_indices *ast.A_Indices) {
	st.appendChar('[')
	if a_indices.Lidx != nil {
		deparseExpr(st, a_indices.Lidx, contextAExpr)
	}
	if a_indices.IsSlice {
		st.appendChar(':')
	}
	if a_indices.Uidx != nil {
		deparseExpr(st, a_indices.Uidx, contextAExpr)
	}
	st.appendChar(']')
}

func deparseCoalesceExpr(st *state, coalesce_expr *ast.CoalesceExpr) {
	st.appendString("COALESCE(")
	deparseExprList(st, coalesce_expr.Args)
	st.appendChar(')')
}

func deparseMinMaxExpr(st *state, min_max_expr *ast.MinMaxExpr) {
	switch min_max_expr.Op {
	case ast.MinMaxOp_IS_GREATEST:
		st.appendString("GREATEST(")
	case ast.MinMaxOp_IS_LEAST:
		st.appendString("LEAST(")
	}
	deparseExprList(st, min_max_expr.Args)
	st.appendChar(')')
}

func deparseBooleanTest(st *state, boolean_test *ast.BooleanTest) {
	need_parens := boolean_test.Arg.GetBoolExpr() != nil

	if need_parens {
		st.appendChar('(')
	}

	deparseExpr(st, boolean_test.Arg, contextAExpr)

	if need_parens {
		st.appendChar(')')
	}

	switch boolean_test.Booltesttype {
	case ast.BoolTestType_IS_TRUE:
		st.appendString(" IS TRUE")
	case ast.BoolTestType_IS_NOT_TRUE:
		st.appendString(" IS NOT TRUE")
	case ast.BoolTestType_IS_FALSE:
		st.appendString(" IS FALSE")
	case ast.BoolTestType_IS_NOT_FALSE:
		st.appendString(" IS NOT FALSE")
	case ast.BoolTestType_IS_UNKNOWN:
		st.appendString(" IS UNKNOWN")
	case ast.BoolTestType_IS_NOT_UNKNOWN:
		st.appendString(" IS NOT UNKNOWN")
	default:
	}
}

func deparseColumnDef(st *state, column_def *ast.ColumnDef) {
	if column_def.Colname != "" {
		st.appendString(quoteIdentifier(column_def.Colname))
		st.appendChar(' ')
	}

	if column_def.TypeName != nil {
		deparseTypeName(st, column_def.TypeName)
		st.appendChar(' ')
	}

	if column_def.StorageName != "" {
		st.appendString("STORAGE ")
		st.appendString(column_def.StorageName)
		st.appendChar(' ')
	}

	if column_def.RawDefault != nil {
		st.appendString("USING ")
		deparseExpr(st, column_def.RawDefault, contextAExpr)
		st.appendChar(' ')
	}

	if column_def.Compression != "" {
		st.appendString("COMPRESSION ")
		st.appendString(column_def.Compression)
		st.appendChar(' ')
	}

	if len(column_def.Fdwoptions) > 0 {
		deparseCreateGenericOptions(st, column_def.Fdwoptions)
		st.appendChar(' ')
	}

	for _, item := range column_def.Constraints {
		deparseConstraint(st, item.GetConstraint(), contextNone)
		st.appendChar(' ')
	}

	if column_def.CollClause != nil {
		deparseCollateClause(st, column_def.CollClause)
	}

	st.removeTrailingSpace()
}

func deparseInsertOverride(st *state, override ast.OverridingKind) {
	switch override {
	case ast.OverridingKind_OVERRIDING_NOT_SET:
		// Do nothing
	case ast.OverridingKind_OVERRIDING_USER_VALUE:
		st.appendString("OVERRIDING USER VALUE ")
	case ast.OverridingKind_OVERRIDING_SYSTEM_VALUE:
		st.appendString("OVERRIDING SYSTEM VALUE ")
	}
}

func deparseInsertStmt(st *state, insert_stmt *ast.InsertStmt) {
	parent_level := st.increaseNestingLevel()

	if insert_stmt.WithClause != nil {
		deparseWithClause(st, insert_stmt.WithClause)
		st.appendChar(' ')
	}

	st.appendPartGroup("INSERT INTO", partIndent)
	deparseRangeVar(st, insert_stmt.Relation, contextInsertRelation)
	st.appendChar(' ')

	if len(insert_stmt.Cols) > 0 {
		st.appendChar('(')
		deparseInsertColumnList(st, insert_stmt.Cols)
		st.appendString(") ")
	}

	deparseInsertOverride(st, insert_stmt.Override)

	if insert_stmt.SelectStmt != nil {
		deparseSelectStmt(st, insert_stmt.SelectStmt.GetSelectStmt(), contextInsertSelect)
		st.appendChar(' ')
	} else {
		st.appendString("DEFAULT VALUES ")
	}

	if insert_stmt.OnConflictClause != nil {
		deparseOnConflictClause(st, insert_stmt.OnConflictClause)
		st.appendChar(' ')
	}

	if insert_stmt.ReturningClause != nil {
		deparseReturningClause(st, insert_stmt.ReturningClause)
	}

	st.removeTrailingSpace()
	st.decreaseNestingLevel(parent_level)
}

// "returning_clause" and "returning_option" in gram.y
func deparseReturningClause(st *state, returning_clause *ast.ReturningClause) {
	st.appendPartGroup("RETURNING", partIndentAndMerge)
	if len(returning_clause.Options) > 0 {
		st.appendString("WITH (")
		for i, item := range returning_clause.Options {
			opt := item.GetReturningOption()
			switch opt.Option {
			case ast.ReturningOptionKind_RETURNING_OPTION_OLD:
				st.appendString("OLD AS ")
			case ast.ReturningOptionKind_RETURNING_OPTION_NEW:
				st.appendString("NEW AS ")
			}
			deparseColId(st, opt.Value)
			if i < len(returning_clause.Options)-1 {
				st.appendString(", ")
			}
		}
		st.appendString(") ")
	}
	deparseTargetList(st, returning_clause.Exprs)
}

func deparseInferClause(st *state, infer_clause *ast.InferClause) {
	if len(infer_clause.IndexElems) > 0 {
		st.appendChar('(')
		for i, item := range infer_clause.IndexElems {
			deparseIndexElem(st, item.GetIndexElem())
			if i < len(infer_clause.IndexElems)-1 {
				st.appendString(", ")
			}
		}
		st.appendString(") ")
	}

	if infer_clause.Conname != "" {
		st.appendString("ON CONSTRAINT ")
		st.appendString(quoteIdentifier(infer_clause.Conname))
		st.appendChar(' ')
	}

	deparseWhereClause(st, infer_clause.WhereClause)

	st.removeTrailingSpace()
}

func deparseOnConflictClause(st *state, on_conflict_clause *ast.OnConflictClause) {
	st.appendPartGroup("ON CONFLICT", partIndent)

	if on_conflict_clause.Infer != nil {
		deparseInferClause(st, on_conflict_clause.Infer)
		st.appendChar(' ')
	}

	switch on_conflict_clause.Action {
	case ast.OnConflictAction_ONCONFLICT_NONE:
	case ast.OnConflictAction_ONCONFLICT_NOTHING:
		st.appendString("DO NOTHING ")
	case ast.OnConflictAction_ONCONFLICT_UPDATE:
		st.appendString("DO UPDATE ")
	}

	if len(on_conflict_clause.TargetList) > 0 {
		st.appendPartGroup("SET", partIndent)
		deparseSetClauseList(st, on_conflict_clause.TargetList)
		st.appendChar(' ')
	}

	deparseWhereClause(st, on_conflict_clause.WhereClause)

	st.removeTrailingSpace()
}

func deparseUpdateStmt(st *state, update_stmt *ast.UpdateStmt) {
	parent_level := st.increaseNestingLevel()

	if update_stmt.WithClause != nil {
		deparseWithClause(st, update_stmt.WithClause)
		st.appendChar(' ')
	}

	st.appendPartGroup("UPDATE", partIndent)
	deparseRangeVar(st, update_stmt.Relation, contextNone)
	st.appendChar(' ')

	if len(update_stmt.TargetList) > 0 {
		st.appendPartGroup("SET", partIndent)
		deparseSetClauseList(st, update_stmt.TargetList)
		st.appendChar(' ')
	}

	deparseFromClause(st, update_stmt.FromClause)
	deparseWhereOrCurrentClause(st, update_stmt.WhereClause)

	if update_stmt.ReturningClause != nil {
		deparseReturningClause(st, update_stmt.ReturningClause)
	}

	st.removeTrailingSpace()
	st.decreaseNestingLevel(parent_level)
}

func deparseMergeStmt(st *state, merge_stmt *ast.MergeStmt) {
	parent_level := st.increaseNestingLevel()

	if merge_stmt.WithClause != nil {
		deparseWithClause(st, merge_stmt.WithClause)
		st.appendChar(' ')
	}

	st.appendPartGroup("MERGE", partIndent)
	st.appendString("INTO ")
	deparseRangeVar(st, merge_stmt.Relation, contextNone)
	st.appendChar(' ')

	st.appendPartGroup("USING", partIndent)
	deparseTableRef(st, merge_stmt.SourceRelation)
	st.appendChar(' ')

	st.appendString("ON ")
	deparseExpr(st, merge_stmt.JoinCondition, contextAExpr)
	st.appendChar(' ')

	for _, item := range merge_stmt.MergeWhenClauses {
		clause := item.GetMergeWhenClause()

		st.appendString("WHEN ")

		switch clause.MatchKind {
		case ast.MergeMatchKind_MERGE_WHEN_MATCHED:
			st.appendString("MATCHED ")
		case ast.MergeMatchKind_MERGE_WHEN_NOT_MATCHED_BY_SOURCE:
			st.appendString("NOT MATCHED BY SOURCE ")
		case ast.MergeMatchKind_MERGE_WHEN_NOT_MATCHED_BY_TARGET:
			st.appendString("NOT MATCHED ")
		}

		if clause.Condition != nil {
			st.appendString("AND ")
			deparseExpr(st, clause.Condition, contextAExpr)
			st.appendChar(' ')
		}

		st.appendString("THEN ")

		switch clause.CommandType {
		case ast.CmdType_CMD_INSERT:
			st.appendString("INSERT ")

			if len(clause.TargetList) > 0 {
				st.appendChar('(')
				deparseInsertColumnList(st, clause.TargetList)
				st.appendString(") ")
			}

			deparseInsertOverride(st, clause.Override)

			if len(clause.Values) > 0 {
				st.appendString("VALUES (")
				deparseExprList(st, clause.Values)
				st.appendString(")")
			} else {
				st.appendString("DEFAULT VALUES ")
			}
		case ast.CmdType_CMD_UPDATE:
			st.appendString("UPDATE SET ")
			deparseSetClauseList(st, clause.TargetList)
		case ast.CmdType_CMD_DELETE:
			st.appendString("DELETE")
		case ast.CmdType_CMD_NOTHING:
			st.appendString("DO NOTHING")
		default:
			st.errorf("deparse: unpermitted command type in merge statement: %d", clause.CommandType)
		}

		if item != merge_stmt.MergeWhenClauses[len(merge_stmt.MergeWhenClauses)-1] {
			st.appendChar(' ')
		}
	}

	if merge_stmt.ReturningClause != nil {
		deparseReturningClause(st, merge_stmt.ReturningClause)
	}

	st.decreaseNestingLevel(parent_level)
}

func deparseDeleteStmt(st *state, delete_stmt *ast.DeleteStmt) {
	parent_level := st.increaseNestingLevel()

	if delete_stmt.WithClause != nil {
		deparseWithClause(st, delete_stmt.WithClause)
		st.appendChar(' ')
	}

	st.appendPartGroup("DELETE", partIndent)
	st.appendString("FROM ")
	deparseRangeVar(st, delete_stmt.Relation, contextNone)
	st.appendChar(' ')

	if len(delete_stmt.UsingClause) > 0 {
		st.appendPartGroup("USING", partIndent)
		deparseFromList(st, delete_stmt.UsingClause)
		st.appendChar(' ')
	}

	deparseWhereOrCurrentClause(st, delete_stmt.WhereClause)

	if delete_stmt.ReturningClause != nil {
		deparseReturningClause(st, delete_stmt.ReturningClause)
	}

	st.removeTrailingSpace()
	st.decreaseNestingLevel(parent_level)
}

func deparseLockingClause(st *state, locking_clause *ast.LockingClause) {
	switch locking_clause.Strength {
	case ast.LockClauseStrength_LCS_NONE:
		/* no such clause - only used in PlanRowMark */
	case ast.LockClauseStrength_LCS_FORKEYSHARE:
		st.appendPartGroup("FOR KEY SHARE", partIndent)
	case ast.LockClauseStrength_LCS_FORSHARE:
		st.appendPartGroup("FOR SHARE", partIndent)
	case ast.LockClauseStrength_LCS_FORNOKEYUPDATE:
		st.appendPartGroup("FOR NO KEY UPDATE", partIndent)
	case ast.LockClauseStrength_LCS_FORUPDATE:
		st.appendPartGroup("FOR UPDATE", partIndent)
	}

	if len(locking_clause.LockedRels) > 0 {
		st.appendString("OF ")
		deparseQualifiedNameList(st, locking_clause.LockedRels)
		st.appendChar(' ')
	}

	switch locking_clause.WaitPolicy {
	case ast.LockWaitPolicy_LockWaitError:
		st.appendString("NOWAIT")
	case ast.LockWaitPolicy_LockWaitSkip:
		st.appendString("SKIP LOCKED")
	case ast.LockWaitPolicy_LockWaitBlock:
		// Default
	}

	st.removeTrailingSpace()
}

func deparseSetToDefault(st *state, set_to_default *ast.SetToDefault) {
	st.appendString("DEFAULT")
}

func deparseCreateCastStmt(st *state, create_cast_stmt *ast.CreateCastStmt) {
	st.appendString("CREATE CAST (")
	deparseTypeName(st, create_cast_stmt.Sourcetype)
	st.appendString(" AS ")
	deparseTypeName(st, create_cast_stmt.Targettype)
	st.appendString(") ")

	if create_cast_stmt.Func != nil {
		st.appendString("WITH FUNCTION ")
		deparseFunctionWithArgtypes(st, create_cast_stmt.Func)
		st.appendChar(' ')
	} else if create_cast_stmt.Inout {
		st.appendString("WITH INOUT ")
	} else {
		st.appendString("WITHOUT FUNCTION ")
	}

	switch create_cast_stmt.Context {
	case ast.CoercionContext_COERCION_IMPLICIT:
		st.appendString("AS IMPLICIT")
	case ast.CoercionContext_COERCION_ASSIGNMENT:
		st.appendString("AS ASSIGNMENT")
	case ast.CoercionContext_COERCION_PLPGSQL:
		// Not present in raw parser output
	case ast.CoercionContext_COERCION_EXPLICIT:
		// Default
	}
}

func deparseCreateOpClassStmt(st *state, create_op_class_stmt *ast.CreateOpClassStmt) {
	st.appendString("CREATE OPERATOR CLASS ")

	deparseAnyName(st, create_op_class_stmt.Opclassname)
	st.appendChar(' ')

	if create_op_class_stmt.IsDefault {
		st.appendString("DEFAULT ")
	}

	st.appendString("FOR TYPE ")
	deparseTypeName(st, create_op_class_stmt.Datatype)
	st.appendChar(' ')

	st.appendString("USING ")
	st.appendString(quoteIdentifier(create_op_class_stmt.Amname))
	st.appendChar(' ')

	if len(create_op_class_stmt.Opfamilyname) > 0 {
		st.appendString("FAMILY ")
		deparseAnyName(st, create_op_class_stmt.Opfamilyname)
		st.appendChar(' ')
	}

	st.appendString("AS ")
	deparseOpclassItemList(st, create_op_class_stmt.Items)
}
