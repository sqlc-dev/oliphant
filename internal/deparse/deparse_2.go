// Ported from postgres_deparse.c (libpg_query 17-6.2.2).
// FROM/WHERE clauses, SELECT, function calls, window definitions, A_Expr.
package deparse

import (
	"github.com/sqlc-dev/oliphant/ast"
)

func deparseFromClause(st *state, l []*ast.Node) {
	if len(l) > 0 {
		st.appendPartGroup("FROM", partIndent)
		deparseFromList(st, l)
		st.appendChar(' ')
	}
}

func deparseWhereClause(st *state, node *ast.Node) {
	if node != nil {
		st.appendPartGroup("WHERE", partIndent)
		deparseExpr(st, node, contextAExpr)
		st.appendChar(' ')
	}
}

func deparseWhereOrCurrentClause(st *state, node *ast.Node) {
	if node == nil {
		return
	}

	st.appendPartGroup("WHERE", partIndent)

	if node.GetCurrentOfExpr() != nil {
		current_of_expr := node.GetCurrentOfExpr()
		st.appendString("CURRENT OF ")
		st.appendString(quoteIdentifier(current_of_expr.CursorName))
	} else {
		deparseExpr(st, node, contextAExpr)
	}

	st.appendChar(' ')
}

func deparseGroupByList(st *state, l []*ast.Node) {
	for i, item := range l {
		if item.GetGroupingSet() != nil {
			deparseGroupingSet(st, item.GetGroupingSet())
		} else {
			deparseExpr(st, item, contextAExpr)
		}

		if i < len(l)-1 {
			st.appendCommaAndPart()
		}
	}
}

func deparseSetTarget(st *state, res_target *ast.ResTarget) {
	deparseColId(st, res_target.Name)
	deparseOptIndirection(st, res_target.Indirection, 0)
}

func deparseAnyNameList(st *state, l []*ast.Node) {
	for i, item := range l {
		deparseAnyName(st, item.GetList().Items)
		if i < len(l)-1 {
			st.appendString(", ")
		}
	}
}

func deparseNameList(st *state, l []*ast.Node) {
	for i, item := range l {
		deparseColId(st, strVal(item))
		if i < len(l)-1 {
			st.appendString(", ")
		}
	}
}

func deparseOptSortClause(st *state, l []*ast.Node, ctx nodeContext) {
	if len(l) > 0 {
		if ctx == contextSelectSortClause {
			st.appendPartGroup("ORDER BY", partIndentAndMerge)
		} else {
			st.appendString("ORDER BY ")
		}

		for i, item := range l {
			deparseSortBy(st, item.GetSortBy())
			if i < len(l)-1 {
				if ctx == contextSelectSortClause {
					st.appendCommaAndPart()
				} else {
					st.appendString(", ")
				}
			}
		}
		st.appendChar(' ')
	}
}

func deparseFuncArgExpr(st *state, node *ast.Node) {
	if node.GetNamedArgExpr() != nil {
		named_arg_expr := node.GetNamedArgExpr()
		st.appendString(named_arg_expr.Name)
		st.appendString(" := ")
		deparseExpr(st, named_arg_expr.Arg, contextAExpr)
	} else {
		deparseExpr(st, node, contextAExpr)
	}
}

func deparseSetClauseList(st *state, target_list []*ast.Node) {
	skip_next_n_elems := 0

	for i, item := range target_list {
		if skip_next_n_elems > 0 {
			skip_next_n_elems--
			continue
		}

		if i != 0 {
			st.appendCommaAndPart()
		}

		res_target := item.GetResTarget()

		if res_target.Val.GetMultiAssignRef() != nil {
			r := res_target.Val.GetMultiAssignRef()
			st.appendString("(")
			for j := i; j < len(target_list); j++ {
				deparseSetTarget(st, target_list[j].GetResTarget())
				if j-i == int(r.Ncolumns)-1 { // Last element in this multi-assign
					break
				} else if j < len(target_list)-1 {
					st.appendString(", ")
				}
			}
			st.appendString(") = ")
			deparseExpr(st, r.Source, contextAExpr)
			skip_next_n_elems = int(r.Ncolumns) - 1
		} else {
			deparseSetTarget(st, res_target)
			st.appendString(" = ")
			deparseExpr(st, res_target.Val, contextAExpr)
		}
	}
}

func deparseFuncExprWindowless(st *state, node *ast.Node) {
	switch {
	case node.GetFuncCall() != nil:
		deparseFuncCall(st, node.GetFuncCall(), contextNone /* we don't know which kind of expression */)
	case node.GetSqlvalueFunction() != nil:
		deparseSQLValueFunction(st, node.GetSqlvalueFunction())
	case node.GetTypeCast() != nil:
		deparseTypeCast(st, node.GetTypeCast(), contextFuncExpr)
	case node.GetCoalesceExpr() != nil:
		deparseCoalesceExpr(st, node.GetCoalesceExpr())
	case node.GetMinMaxExpr() != nil:
		deparseMinMaxExpr(st, node.GetMinMaxExpr())
	case node.GetXmlExpr() != nil:
		deparseXmlExpr(st, node.GetXmlExpr(), contextNone /* we don't know which kind of expression */)
	case node.GetXmlSerialize() != nil:
		deparseXmlSerialize(st, node.GetXmlSerialize())
	}
}

func deparseOptCollate(st *state, l []*ast.Node) {
	if len(l) > 0 {
		st.appendString("COLLATE ")
		deparseAnyName(st, l)
		st.appendChar(' ')
	}
}

func deparseIndexElem(st *state, index_elem *ast.IndexElem) {
	if index_elem.Name != "" {
		deparseColId(st, index_elem.Name)
		st.appendChar(' ')
	} else if index_elem.Expr != nil {
		switch {
		// Simple function calls can be written without wrapping parens
		case index_elem.Expr.GetFuncCall() != nil, // func_application
			index_elem.Expr.GetSqlvalueFunction() != nil, // func_expr_common_subexpr
			index_elem.Expr.GetCoalesceExpr() != nil,     // func_expr_common_subexpr
			index_elem.Expr.GetMinMaxExpr() != nil,       // func_expr_common_subexpr
			index_elem.Expr.GetXmlExpr() != nil,          // func_expr_common_subexpr
			index_elem.Expr.GetXmlSerialize() != nil:     // func_expr_common_subexpr
			deparseFuncExprWindowless(st, index_elem.Expr)
			st.appendString(" ")
		default:
			st.appendChar('(')
			deparseExpr(st, index_elem.Expr, contextAExpr)
			st.appendString(") ")
		}
	}

	deparseOptCollate(st, index_elem.Collation)

	if len(index_elem.Opclass) > 0 {
		deparseAnyName(st, index_elem.Opclass)

		if len(index_elem.Opclassopts) > 0 {
			deparseRelOptions(st, index_elem.Opclassopts)
		}

		st.appendChar(' ')
	}

	switch index_elem.Ordering {
	case ast.SortByDir_SORTBY_DEFAULT:
		// Default
	case ast.SortByDir_SORTBY_ASC:
		st.appendString("ASC ")
	case ast.SortByDir_SORTBY_DESC:
		st.appendString("DESC ")
	case ast.SortByDir_SORTBY_USING:
		// Not allowed in CREATE INDEX
	}

	switch index_elem.NullsOrdering {
	case ast.SortByNulls_SORTBY_NULLS_DEFAULT:
		// Default
	case ast.SortByNulls_SORTBY_NULLS_FIRST:
		st.appendString("NULLS FIRST ")
	case ast.SortByNulls_SORTBY_NULLS_LAST:
		st.appendString("NULLS LAST ")
	}

	st.removeTrailingSpace()
}

func deparseQualifiedNameList(st *state, l []*ast.Node) {
	for i, item := range l {
		deparseRangeVar(st, item.GetRangeVar(), contextNone)
		if i < len(l)-1 {
			st.appendString(", ")
		}
	}
}

func deparseOptInherit(st *state, l []*ast.Node) {
	if len(l) > 0 {
		st.appendString("INHERITS (")
		deparseQualifiedNameList(st, l)
		st.appendString(") ")
	}
}

func deparsePrivilegeTarget(st *state, targtype ast.GrantTargetType, objtype ast.ObjectType, objs []*ast.Node) {
	switch targtype {
	case ast.GrantTargetType_ACL_TARGET_OBJECT:
		switch objtype {
		case ast.ObjectType_OBJECT_TABLE:
			deparseQualifiedNameList(st, objs)
		case ast.ObjectType_OBJECT_SEQUENCE:
			st.appendString("SEQUENCE ")
			deparseQualifiedNameList(st, objs)
		case ast.ObjectType_OBJECT_FDW:
			st.appendString("FOREIGN DATA WRAPPER ")
			deparseNameList(st, objs)
		case ast.ObjectType_OBJECT_FOREIGN_SERVER:
			st.appendString("FOREIGN SERVER ")
			deparseNameList(st, objs)
		case ast.ObjectType_OBJECT_FUNCTION:
			st.appendString("FUNCTION ")
			deparseFunctionWithArgtypesList(st, objs)
		case ast.ObjectType_OBJECT_PROCEDURE:
			st.appendString("PROCEDURE ")
			deparseFunctionWithArgtypesList(st, objs)
		case ast.ObjectType_OBJECT_ROUTINE:
			st.appendString("ROUTINE ")
			deparseFunctionWithArgtypesList(st, objs)
		case ast.ObjectType_OBJECT_DATABASE:
			st.appendString("DATABASE ")
			deparseNameList(st, objs)
		case ast.ObjectType_OBJECT_DOMAIN:
			st.appendString("DOMAIN ")
			deparseAnyNameList(st, objs)
		case ast.ObjectType_OBJECT_LANGUAGE:
			st.appendString("LANGUAGE ")
			deparseNameList(st, objs)
		case ast.ObjectType_OBJECT_LARGEOBJECT:
			st.appendString("LARGE OBJECT ")
			deparseNumericOnlyList(st, objs)
		case ast.ObjectType_OBJECT_SCHEMA:
			st.appendString("SCHEMA ")
			deparseNameList(st, objs)
		case ast.ObjectType_OBJECT_TABLESPACE:
			st.appendString("TABLESPACE ")
			deparseNameList(st, objs)
		case ast.ObjectType_OBJECT_TYPE:
			st.appendString("TYPE ")
			deparseAnyNameList(st, objs)
		default:
			// Other types are not supported here
		}
	case ast.GrantTargetType_ACL_TARGET_ALL_IN_SCHEMA:
		switch objtype {
		case ast.ObjectType_OBJECT_TABLE:
			st.appendString("ALL TABLES IN SCHEMA ")
			deparseNameList(st, objs)
		case ast.ObjectType_OBJECT_SEQUENCE:
			st.appendString("ALL SEQUENCES IN SCHEMA ")
			deparseNameList(st, objs)
		case ast.ObjectType_OBJECT_FUNCTION:
			st.appendString("ALL FUNCTIONS IN SCHEMA ")
			deparseNameList(st, objs)
		case ast.ObjectType_OBJECT_PROCEDURE:
			st.appendString("ALL PROCEDURES IN SCHEMA ")
			deparseNameList(st, objs)
		case ast.ObjectType_OBJECT_ROUTINE:
			st.appendString("ALL ROUTINES IN SCHEMA ")
			deparseNameList(st, objs)
		default:
			// Other types are not supported here
		}
	case ast.GrantTargetType_ACL_TARGET_DEFAULTS: // defacl_privilege_target
		switch objtype {
		case ast.ObjectType_OBJECT_TABLE:
			st.appendString("TABLES")
		case ast.ObjectType_OBJECT_FUNCTION:
			st.appendString("FUNCTIONS")
		case ast.ObjectType_OBJECT_SEQUENCE:
			st.appendString("SEQUENCES")
		case ast.ObjectType_OBJECT_TYPE:
			st.appendString("TYPES")
		case ast.ObjectType_OBJECT_SCHEMA:
			st.appendString("SCHEMAS")
		case ast.ObjectType_OBJECT_LARGEOBJECT:
			st.appendString("LARGE OBJECTS")
		default:
			// Other types are not supported here
		}
	}
}

func deparseOpclassItemList(st *state, items []*ast.Node) {
	for i, item := range items {
		deparseCreateOpClassItem(st, item.GetCreateOpClassItem())
		if i < len(items)-1 {
			st.appendString(", ")
		}
	}
}

func deparseCreatedbOptList(st *state, l []*ast.Node) {
	for i, item := range l {
		def_elem := item.GetDefElem()
		if def_elem.Defname == "connection_limit" {
			st.appendString("CONNECTION LIMIT")
		} else {
			deparseGenericDefElemName(st, def_elem.Defname)
		}

		st.appendChar(' ')

		if def_elem.Arg == nil {
			st.appendString("DEFAULT")
		} else if def_elem.Arg.GetInteger() != nil {
			deparseSignedIconst(st, def_elem.Arg)
		} else if def_elem.Arg.GetString_() != nil {
			deparseOptBooleanOrString(st, strVal(def_elem.Arg))
		}

		if i < len(l)-1 {
			st.appendChar(' ')
		}
	}
}

func deparseUtilityOptionList(st *state, options []*ast.Node) {
	if len(options) > 0 {
		st.appendChar('(')
		for i, item := range options {
			def_elem := item.GetDefElem()
			deparseGenericDefElemName(st, def_elem.Defname)

			if def_elem.Arg != nil {
				st.appendChar(' ')
				if def_elem.Arg.GetInteger() != nil || def_elem.Arg.GetFloat() != nil {
					deparseNumericOnly(st, def_elem.Arg)
				} else if def_elem.Arg.GetString_() != nil {
					deparseOptBooleanOrString(st, strVal(def_elem.Arg))
				}
			}

			if i < len(options)-1 {
				st.appendString(", ")
			}
		}
		st.appendString(") ")
	}
}

func deparseSelectStmt(st *state, stmt *ast.SelectStmt, ctx nodeContext) {
	need_parens := ctx == contextSelectSetop && (len(stmt.SortClause) > 0 ||
		stmt.LimitOffset != nil ||
		stmt.LimitCount != nil ||
		len(stmt.LockingClause) > 0 ||
		stmt.WithClause != nil ||
		stmt.Op != ast.SetOperation_SETOP_NONE)
	var parent_level *nestingLevel

	if need_parens {
		st.appendPart(true)
		st.appendChar('(')
	}
	if need_parens || (ctx != contextSelectSetop && ctx != contextInsertSelect) {
		parent_level = st.increaseNestingLevel()
	}

	if stmt.WithClause != nil {
		deparseWithClause(st, stmt.WithClause)
		st.appendChar(' ')
	}

	switch stmt.Op {
	case ast.SetOperation_SETOP_NONE:
		if len(stmt.ValuesLists) > 0 {
			st.appendPartGroup("VALUES", partIndentAndMerge)

			for i, item := range stmt.ValuesLists {
				st.appendChar('(')
				deparseExprList(st, item.GetList().Items)
				st.appendChar(')')
				if i < len(stmt.ValuesLists)-1 {
					st.appendCommaAndPart()
				}
			}
			st.appendChar(' ')
			break
		}

		st.appendPartGroup("SELECT", partIndentAndMerge)

		if len(stmt.TargetList) > 0 {
			if len(stmt.DistinctClause) > 0 {
				st.markCurrentPartNonMergeable()
				st.appendString("DISTINCT ")

				if len(stmt.DistinctClause) > 0 && stmt.DistinctClause[0].Node != nil {
					st.appendString("ON (")
					deparseExprList(st, stmt.DistinctClause)
					st.appendString(") ")
				}
				st.appendPart(true)
			}

			deparseTargetList(st, stmt.TargetList)
			st.appendChar(' ')
		}

		if stmt.IntoClause != nil {
			st.appendPartGroup("INTO", partIndent)
			deparseOptTemp(st, stmt.IntoClause.Rel.Relpersistence)
			deparseIntoClause(st, stmt.IntoClause)
			st.appendChar(' ')
		}

		deparseFromClause(st, stmt.FromClause)
		deparseWhereClause(st, stmt.WhereClause)

		if len(stmt.GroupClause) > 0 {
			st.appendPartGroup("GROUP BY", partIndentAndMerge)
			if stmt.GroupDistinct {
				st.appendString("DISTINCT ")
			}
			deparseGroupByList(st, stmt.GroupClause)
			st.appendChar(' ')
		}

		if stmt.HavingClause != nil {
			st.appendPartGroup("HAVING", partIndent)
			deparseExpr(st, stmt.HavingClause, contextAExpr)
			st.appendChar(' ')
		}

		if len(stmt.WindowClause) > 0 {
			st.appendPartGroup("WINDOW", partIndent)
			for i, item := range stmt.WindowClause {
				window_def := item.GetWindowDef()
				st.appendString(window_def.Name)
				st.appendString(" AS ")
				deparseWindowDef(st, window_def)
				if i < len(stmt.WindowClause)-1 {
					st.appendString(", ")
				}
			}
			st.appendChar(' ')
		}
	case ast.SetOperation_SETOP_UNION,
		ast.SetOperation_SETOP_INTERSECT,
		ast.SetOperation_SETOP_EXCEPT:
		{
			deparseSelectStmt(st, stmt.Larg, contextSelectSetop)
			switch stmt.Op {
			case ast.SetOperation_SETOP_UNION:
				if stmt.All {
					st.appendPartGroup("UNION ALL", partNoIndent)
				} else {
					st.appendPartGroup("UNION", partNoIndent)
				}
			case ast.SetOperation_SETOP_INTERSECT:
				if stmt.All {
					st.appendPartGroup("INTERSECT ALL", partNoIndent)
				} else {
					st.appendPartGroup("INTERSECT", partNoIndent)
				}
			case ast.SetOperation_SETOP_EXCEPT:
				if stmt.All {
					st.appendPartGroup("EXCEPT ALL", partNoIndent)
				} else {
					st.appendPartGroup("EXCEPT", partNoIndent)
				}
			}
			st.appendPart(true)
			deparseSelectStmt(st, stmt.Rarg, contextSelectSetop)
			st.appendChar(' ')
		}
	}

	deparseOptSortClause(st, stmt.SortClause, contextSelectSortClause)

	if stmt.LimitCount != nil {
		if stmt.LimitOption == ast.LimitOption_LIMIT_OPTION_COUNT {
			st.appendPartGroup("LIMIT", partIndent)
		} else if stmt.LimitOption == ast.LimitOption_LIMIT_OPTION_WITH_TIES {
			st.appendString("FETCH FIRST ")
		}

		if stmt.LimitCount.GetAConst() != nil && stmt.LimitCount.GetAConst().Isnull {
			st.appendString("ALL")
		} else if stmt.LimitOption == ast.LimitOption_LIMIT_OPTION_WITH_TIES {
			deparseCExpr(st, stmt.LimitCount)
		} else {
			deparseExpr(st, stmt.LimitCount, contextNone /* c_expr */)
		}

		st.appendChar(' ')

		if stmt.LimitOption == ast.LimitOption_LIMIT_OPTION_WITH_TIES {
			st.appendString("ROWS WITH TIES ")
		}
	}

	if stmt.LimitOffset != nil {
		st.appendPartGroup("OFFSET", partIndent)
		deparseExpr(st, stmt.LimitOffset, contextAExpr)
		st.appendChar(' ')
	}

	if len(stmt.LockingClause) > 0 {
		for i, item := range stmt.LockingClause {
			deparseLockingClause(st, item.GetLockingClause())
			if i < len(stmt.LockingClause)-1 {
				st.appendString(" ")
			}
		}
		st.appendChar(' ')
	}

	st.removeTrailingSpace()

	/* Note that parent_level can be NULL, so we repeat the full if condition here */
	if need_parens || (ctx != contextSelectSetop && ctx != contextInsertSelect) {
		st.decreaseNestingLevel(parent_level)
	}
	if need_parens {
		st.appendChar(')')
	}
}

func deparseIntoClause(st *state, into_clause *ast.IntoClause) {
	deparseRangeVar(st, into_clause.Rel, contextNone) /* target relation name */

	if len(into_clause.ColNames) > 0 {
		st.appendChar('(')
		deparseColumnList(st, into_clause.ColNames)
		st.appendChar(')')
	}
	st.appendChar(' ')

	if into_clause.AccessMethod != "" {
		st.appendString("USING ")
		st.appendString(quoteIdentifier(into_clause.AccessMethod))
		st.appendChar(' ')
	}

	deparseOptWith(st, into_clause.Options)

	switch into_clause.OnCommit {
	case ast.OnCommitAction_ONCOMMIT_NOOP:
		// No clause
	case ast.OnCommitAction_ONCOMMIT_PRESERVE_ROWS:
		st.appendString("ON COMMIT PRESERVE ROWS ")
	case ast.OnCommitAction_ONCOMMIT_DELETE_ROWS:
		st.appendString("ON COMMIT DELETE ROWS ")
	case ast.OnCommitAction_ONCOMMIT_DROP:
		st.appendString("ON COMMIT DROP ")
	}

	if into_clause.TableSpaceName != "" {
		st.appendString("TABLESPACE ")
		st.appendString(quoteIdentifier(into_clause.TableSpaceName))
		st.appendChar(' ')
	}

	st.removeTrailingSpace()
}

func deparseRangeVar(st *state, range_var *ast.RangeVar, ctx nodeContext) {
	if !range_var.Inh && ctx != contextCreateType && ctx != contextAlterType {
		st.appendString("ONLY ")
	}

	if range_var.Catalogname != "" {
		st.appendString(quoteIdentifier(range_var.Catalogname))
		st.appendChar('.')
	}

	if range_var.Schemaname != "" {
		st.appendString(quoteIdentifier(range_var.Schemaname))
		st.appendChar('.')
	}

	st.appendString(quoteIdentifier(range_var.Relname))
	st.appendChar(' ')

	if range_var.Alias != nil {
		if ctx == contextInsertRelation {
			st.appendString("AS ")
		}
		deparseAlias(st, range_var.Alias)
		st.appendChar(' ')
	}

	st.removeTrailingSpace()
}

func deparseAlias(st *state, alias *ast.Alias) {
	st.appendString(quoteIdentifier(alias.Aliasname))

	if len(alias.Colnames) > 0 {
		st.appendChar('(')
		deparseNameList(st, alias.Colnames)
		st.appendChar(')')
	}
}

func deparseAConst(st *state, a_const *ast.A_Const) {
	val := aConstVal(a_const)
	deparseValue(st, val, contextConstant)
}

func deparseFuncCall(st *state, func_call *ast.FuncCall, ctx nodeContext) {
	if len(func_call.Funcname) == 2 &&
		strVal(func_call.Funcname[0]) == "pg_catalog" &&
		strVal(func_call.Funcname[1]) == "overlay" &&
		len(func_call.Args) == 4 {
		/*
		 * Note that this is a bit odd, but "OVERLAY" is a keyword on its own merit, and only accepts the
		 * keyword parameter style when its called as a keyword, not as a regular function (i.e. pg_catalog.overlay)
		 */
		st.appendString("OVERLAY(")
		deparseExpr(st, func_call.Args[0], contextAExpr)
		st.appendString(" PLACING ")
		deparseExpr(st, func_call.Args[1], contextAExpr)
		st.appendString(" FROM ")
		deparseExpr(st, func_call.Args[2], contextAExpr)
		st.appendString(" FOR ")
		deparseExpr(st, func_call.Args[3], contextAExpr)
		st.appendChar(')')
		return
	} else if func_call.Funcformat == ast.CoercionForm_COERCE_SQL_SYNTAX &&
		len(func_call.Funcname) == 2 &&
		strVal(func_call.Funcname[0]) == "pg_catalog" &&
		strVal(func_call.Funcname[1]) == "substring" {
		/*
		 * "SUBSTRING" is a keyword on its own merit, and only accepts the
		 * keyword parameter style when its called as a keyword, not as a regular function (i.e. pg_catalog.substring)
		 */
		st.appendString("SUBSTRING(")
		deparseExpr(st, func_call.Args[0], contextAExpr)
		st.appendString(" FROM ")
		deparseExpr(st, func_call.Args[1], contextAExpr)
		if len(func_call.Args) == 3 {
			st.appendString(" FOR ")
			deparseExpr(st, func_call.Args[2], contextAExpr)
		}
		st.appendChar(')')
		return
	} else if func_call.Funcformat == ast.CoercionForm_COERCE_SQL_SYNTAX &&
		len(func_call.Funcname) == 2 &&
		strVal(func_call.Funcname[0]) == "pg_catalog" &&
		strVal(func_call.Funcname[1]) == "position" &&
		len(func_call.Args) == 2 {
		/*
		 * "POSITION" is a keyword on its own merit, and only accepts the
		 * keyword parameter style when its called as a keyword, not as a regular function (i.e. pg_catalog.position)
		 * Note that the first and second arguments are switched in this format
		 */
		st.appendString("POSITION(")
		deparseBExpr(st, func_call.Args[1])
		st.appendString(" IN ")
		deparseBExpr(st, func_call.Args[0])
		st.appendChar(')')
		return
	} else if func_call.Funcformat == ast.CoercionForm_COERCE_SQL_SYNTAX &&
		len(func_call.Funcname) == 2 &&
		strVal(func_call.Funcname[0]) == "pg_catalog" &&
		strVal(func_call.Funcname[1]) == "overlay" &&
		len(func_call.Args) == 3 {
		/*
		 * "OVERLAY" is a keyword on its own merit, and only accepts the
		 * keyword parameter style when its called as a keyword, not as a regular function (i.e. pg_catalog.overlay)
		 */
		st.appendString("overlay(")
		deparseExpr(st, func_call.Args[0], contextAExpr)
		st.appendString(" placing ")
		deparseExpr(st, func_call.Args[1], contextAExpr)
		st.appendString(" from ")
		deparseExpr(st, func_call.Args[2], contextAExpr)
		st.appendChar(')')
		return
	} else if func_call.Funcformat == ast.CoercionForm_COERCE_SQL_SYNTAX &&
		len(func_call.Funcname) == 2 &&
		strVal(func_call.Funcname[0]) == "pg_catalog" &&
		strVal(func_call.Funcname[1]) == "pg_collation_for" &&
		len(func_call.Args) == 1 {
		/*
		 * "collation for" is a keyword on its own merit, and only accepts the
		 * keyword parameter style when its called as a keyword, not as a regular function (i.e. pg_catalog.overlay)
		 */
		st.appendString("collation for (")
		deparseExpr(st, func_call.Args[0], contextAExpr)
		st.appendChar(')')
		return
	} else if func_call.Funcformat == ast.CoercionForm_COERCE_SQL_SYNTAX &&
		len(func_call.Funcname) == 2 &&
		strVal(func_call.Funcname[0]) == "pg_catalog" &&
		strVal(func_call.Funcname[1]) == "extract" &&
		len(func_call.Args) == 2 {
		/*
		 * "EXTRACT" is a keyword on its own merit, and only accepts the
		 * keyword parameter style when its called as a keyword, not as a regular function (i.e. pg_catalog.extract)
		 */
		st.appendString("extract (")
		deparseExpr(st, func_call.Args[0], contextAExpr)
		st.appendString(" FROM ")
		deparseExpr(st, func_call.Args[1], contextAExpr)
		st.appendChar(')')
		return
	} else if func_call.Funcformat == ast.CoercionForm_COERCE_SQL_SYNTAX &&
		len(func_call.Funcname) == 2 &&
		strVal(func_call.Funcname[0]) == "pg_catalog" &&
		strVal(func_call.Funcname[1]) == "overlaps" &&
		len(func_call.Args) == 4 {
		/*
		 * "OVERLAPS" is a keyword on its own merit, and only accepts the
		 * keyword parameter style when its called as a keyword, not as a regular function (i.e. pg_catalog.overlaps)
		 * format: (start_1, end_1) overlaps (start_2, end_2)
		 */
		st.appendChar('(')
		deparseExpr(st, func_call.Args[0], contextAExpr)
		st.appendString(", ")
		deparseExpr(st, func_call.Args[1], contextAExpr)
		st.appendString(") ")

		st.appendString("overlaps ")
		st.appendChar('(')
		deparseExpr(st, func_call.Args[2], contextAExpr)
		st.appendString(", ")
		deparseExpr(st, func_call.Args[3], contextAExpr)
		st.appendString(") ")
		return
	} else if func_call.Funcformat == ast.CoercionForm_COERCE_SQL_SYNTAX &&
		len(func_call.Funcname) == 2 &&
		strVal(func_call.Funcname[0]) == "pg_catalog" &&
		(strVal(func_call.Funcname[1]) == "ltrim" ||
			strVal(func_call.Funcname[1]) == "btrim" ||
			strVal(func_call.Funcname[1]) == "rtrim") {
		/*
		 * "TRIM " is a keyword on its own merit, and only accepts the
		 * keyword parameter style when its called as a keyword, not as a regular function (i.e. pg_catalog.ltrim)
		 * Note that the first and second arguments are switched in this format
		 */
		st.appendString("TRIM (")
		if strVal(func_call.Funcname[1]) == "ltrim" {
			st.appendString("LEADING ")
		} else if strVal(func_call.Funcname[1]) == "btrim" {
			st.appendString("BOTH ")
		} else if strVal(func_call.Funcname[1]) == "rtrim" {
			st.appendString("TRAILING ")
		}

		if len(func_call.Args) == 2 {
			deparseExpr(st, func_call.Args[1], contextAExpr)
		}
		st.appendString(" FROM ")
		deparseExpr(st, func_call.Args[0], contextAExpr)
		st.appendChar(')')
		return
	} else if func_call.Funcformat == ast.CoercionForm_COERCE_SQL_SYNTAX &&
		len(func_call.Funcname) == 2 &&
		strVal(func_call.Funcname[0]) == "pg_catalog" &&
		strVal(func_call.Funcname[1]) == "timezone" &&
		len(func_call.Args) > 0 &&
		len(func_call.Args) <= 2 {
		/*
		 * "AT TIME ZONE" is a keyword on its own merit, and only accepts the
		 * keyword parameter style when its called as a keyword, not as a regular function (i.e. pg_catalog.timezone)
		 * Note that the arguments are swapped in this case
		 */
		var e *ast.Node
		isLocal := len(func_call.Args) == 1

		if isLocal {
			e = func_call.Args[0]
		} else {
			e = func_call.Args[1]
		}

		// If we're not inside an a_expr context, we must add wrapping parenthesis around the AT ... syntax
		if ctx != contextAExpr {
			st.appendChar('(')
		}

		if e.GetAExpr() != nil {
			st.appendChar('(')
		}

		deparseExpr(st, e, contextAExpr)

		if e.GetAExpr() != nil {
			st.appendChar(')')
		}

		if isLocal {
			st.appendString(" AT LOCAL")
		} else {
			st.appendString(" AT TIME ZONE ")
			deparseExpr(st, func_call.Args[0], contextAExpr)
		}

		if ctx != contextAExpr {
			st.appendChar(')')
		}

		return
	} else if func_call.Funcformat == ast.CoercionForm_COERCE_SQL_SYNTAX &&
		len(func_call.Funcname) == 2 &&
		strVal(func_call.Funcname[0]) == "pg_catalog" &&
		strVal(func_call.Funcname[1]) == "normalize" {
		/*
		 * "NORMALIZE" is a keyword on its own merit, and only accepts the
		 * keyword parameter style when its called as a keyword, not as a regular function (i.e. pg_catalog.normalize)
		 */
		st.appendString("normalize (")

		deparseExpr(st, func_call.Args[0], contextAExpr)
		if len(func_call.Args) == 2 {
			st.appendString(", ")
			aconst := func_call.Args[1].GetAConst()
			deparseValue(st, aConstVal(aconst), contextNone)
		}
		st.appendChar(')')
		return
	} else if func_call.Funcformat == ast.CoercionForm_COERCE_SQL_SYNTAX &&
		len(func_call.Funcname) == 2 &&
		strVal(func_call.Funcname[0]) == "pg_catalog" &&
		strVal(func_call.Funcname[1]) == "is_normalized" {
		/*
		 * "IS NORMALIZED" is a keyword on its own merit, and only accepts the
		 * keyword parameter style when its called as a keyword, not as a regular function (i.e. pg_catalog.is_normalized)
		 */
		deparseExpr(st, func_call.Args[0], contextAExpr)
		st.appendString(" IS ")
		if len(func_call.Args) == 2 {
			aconst := func_call.Args[1].GetAConst()
			deparseValue(st, aConstVal(aconst), contextNone)
		}
		st.appendString(" NORMALIZED ")
		return
	} else if func_call.Funcformat == ast.CoercionForm_COERCE_SQL_SYNTAX &&
		len(func_call.Funcname) == 2 &&
		strVal(func_call.Funcname[0]) == "pg_catalog" &&
		strVal(func_call.Funcname[1]) == "xmlexists" &&
		len(func_call.Args) == 2 {
		st.appendString("xmlexists (")
		deparseExpr(st, func_call.Args[0], contextNone /* c_expr */)
		st.appendString(" PASSING ")
		deparseExpr(st, func_call.Args[1], contextNone /* c_expr */)
		st.appendChar(')')
		return
	} else if func_call.Funcformat == ast.CoercionForm_COERCE_SQL_SYNTAX &&
		len(func_call.Funcname) == 2 &&
		strVal(func_call.Funcname[0]) == "pg_catalog" &&
		strVal(func_call.Funcname[1]) == "system_user" {
		st.appendString("SYSTEM_USER")
		return
	}

	deparseFuncName(st, func_call.Funcname)
	st.appendChar('(')

	if func_call.AggDistinct {
		st.appendString("DISTINCT ")
	}

	if func_call.AggStar {
		st.appendChar('*')
	} else if len(func_call.Args) > 0 {
		for i, item := range func_call.Args {
			if func_call.FuncVariadic && i == len(func_call.Args)-1 {
				st.appendString("VARIADIC ")
			}
			deparseFuncArgExpr(st, item)
			if i < len(func_call.Args)-1 {
				st.appendString(", ")
			}
		}
	}
	st.appendChar(' ')

	if len(func_call.AggOrder) > 0 && !func_call.AggWithinGroup {
		deparseOptSortClause(st, func_call.AggOrder, contextNone)
	}

	st.removeTrailingSpace()
	st.appendString(") ")

	if len(func_call.AggOrder) > 0 && func_call.AggWithinGroup {
		st.appendString("WITHIN GROUP (")
		deparseOptSortClause(st, func_call.AggOrder, contextNone)
		st.removeTrailingSpace()
		st.appendString(") ")
	}

	if func_call.AggFilter != nil {
		st.appendString("FILTER (WHERE ")
		deparseExpr(st, func_call.AggFilter, contextAExpr)
		st.appendString(") ")
	}

	if func_call.Over != nil {
		st.appendString("OVER ")
		if func_call.Over.Name != "" {
			st.appendString(func_call.Over.Name)
		} else {
			deparseWindowDef(st, func_call.Over)
		}
	}

	st.removeTrailingSpace()
}

// Frame option flags for WindowDef.FrameOptions (parsenodes.h).
const (
	frameOptionNondefault              = 0x00001 /* any specified? */
	frameOptionRange                   = 0x00002 /* RANGE behavior */
	frameOptionRows                    = 0x00004 /* ROWS behavior */
	frameOptionGroups                  = 0x00008 /* GROUPS behavior */
	frameOptionBetween                 = 0x00010 /* BETWEEN given? */
	frameOptionStartUnboundedPreceding = 0x00020 /* start is U. P. */
	frameOptionEndUnboundedPreceding   = 0x00040 /* (disallowed) */
	frameOptionStartUnboundedFollowing = 0x00080 /* (disallowed) */
	frameOptionEndUnboundedFollowing   = 0x00100 /* end is U. F. */
	frameOptionStartCurrentRow         = 0x00200 /* start is C. R. */
	frameOptionEndCurrentRow           = 0x00400 /* end is C. R. */
	frameOptionStartOffsetPreceding    = 0x00800 /* start is O. P. */
	frameOptionEndOffsetPreceding      = 0x01000 /* end is O. P. */
	frameOptionStartOffsetFollowing    = 0x02000 /* start is O. F. */
	frameOptionEndOffsetFollowing      = 0x04000 /* end is O. F. */
	frameOptionExcludeCurrentRow       = 0x08000 /* omit C.R. */
	frameOptionExcludeGroup            = 0x10000 /* omit C.R. & peers */
	frameOptionExcludeTies             = 0x20000 /* omit C.R.'s peers */
)

func deparseWindowDef(st *state, window_def *ast.WindowDef) {
	// The parent node is responsible for outputting window_def->name

	// Special case completely empty window clauses and return early
	if window_def.Refname == "" && len(window_def.PartitionClause) == 0 &&
		len(window_def.OrderClause) == 0 &&
		window_def.FrameOptions&frameOptionNondefault == 0 {
		st.appendString("()")
		return
	}

	st.appendChar('(')
	parent_level := st.increaseNestingLevel()

	if window_def.Refname != "" {
		st.appendString(quoteIdentifier(window_def.Refname))
		st.appendChar(' ')
	}

	if len(window_def.PartitionClause) > 0 {
		st.appendString("PARTITION BY ")
		deparseExprList(st, window_def.PartitionClause)
		st.appendChar(' ')
	}

	st.removeTrailingSpace()
	st.appendPart(true)
	deparseOptSortClause(st, window_def.OrderClause, contextNone)

	if window_def.FrameOptions&frameOptionNondefault != 0 {
		st.appendPartGroup("", partNoIndent)
		st.currentPartGroup().hasKeyword = false // C passes a NULL keyword here
		if window_def.FrameOptions&frameOptionRange != 0 {
			st.appendString("RANGE ")
		} else if window_def.FrameOptions&frameOptionRows != 0 {
			st.appendString("ROWS ")
		} else if window_def.FrameOptions&frameOptionGroups != 0 {
			st.appendString("GROUPS ")
		}

		if window_def.FrameOptions&frameOptionBetween != 0 {
			st.appendString("BETWEEN ")
		}

		// frame_start
		if window_def.FrameOptions&frameOptionStartUnboundedPreceding != 0 {
			st.appendString("UNBOUNDED PRECEDING ")
		} else if window_def.FrameOptions&frameOptionStartUnboundedFollowing != 0 {
			// disallowed
		} else if window_def.FrameOptions&frameOptionStartCurrentRow != 0 {
			st.appendString("CURRENT ROW ")
		} else if window_def.FrameOptions&frameOptionStartOffsetPreceding != 0 {
			deparseExpr(st, window_def.StartOffset, contextAExpr)
			st.appendString(" PRECEDING ")
		} else if window_def.FrameOptions&frameOptionStartOffsetFollowing != 0 {
			deparseExpr(st, window_def.StartOffset, contextAExpr)
			st.appendString(" FOLLOWING ")
		}

		if window_def.FrameOptions&frameOptionBetween != 0 {
			st.appendString("AND ")

			// frame_end
			if window_def.FrameOptions&frameOptionEndUnboundedPreceding != 0 {
				// disallowed
			} else if window_def.FrameOptions&frameOptionEndUnboundedFollowing != 0 {
				st.appendString("UNBOUNDED FOLLOWING ")
			} else if window_def.FrameOptions&frameOptionEndCurrentRow != 0 {
				st.appendString("CURRENT ROW ")
			} else if window_def.FrameOptions&frameOptionEndOffsetPreceding != 0 {
				deparseExpr(st, window_def.EndOffset, contextAExpr)
				st.appendString(" PRECEDING ")
			} else if window_def.FrameOptions&frameOptionEndOffsetFollowing != 0 {
				deparseExpr(st, window_def.EndOffset, contextAExpr)
				st.appendString(" FOLLOWING ")
			}
		}

		if window_def.FrameOptions&frameOptionExcludeCurrentRow != 0 {
			st.appendString("EXCLUDE CURRENT ROW ")
		} else if window_def.FrameOptions&frameOptionExcludeGroup != 0 {
			st.appendString("EXCLUDE GROUP ")
		} else if window_def.FrameOptions&frameOptionExcludeTies != 0 {
			st.appendString("EXCLUDE TIES ")
		}
	}

	st.removeTrailingSpace()
	st.decreaseNestingLevel(parent_level)
	st.appendChar(')')
}

func deparseColumnRef(st *state, column_ref *ast.ColumnRef) {
	if column_ref.Fields[0].GetAStar() != nil {
		deparseAStar(st, column_ref.Fields[0].GetAStar())
	} else if column_ref.Fields[0].GetString_() != nil {
		deparseColLabel(st, strVal(column_ref.Fields[0]))
	}

	deparseOptIndirection(st, column_ref.Fields, 1)
}

func deparseSubLink(st *state, sub_link *ast.SubLink) {
	switch sub_link.SubLinkType {
	case ast.SubLinkType_EXISTS_SUBLINK:
		st.appendString("EXISTS (")
		deparseSelectStmt(st, sub_link.Subselect.GetSelectStmt(), contextNone)
		st.appendChar(')')
		return
	case ast.SubLinkType_ALL_SUBLINK:
		deparseExpr(st, sub_link.Testexpr, contextAExpr)
		st.appendChar(' ')
		deparseSubqueryOp(st, sub_link.OperName)
		st.appendString(" ALL (")
		deparseSelectStmt(st, sub_link.Subselect.GetSelectStmt(), contextNone)
		st.appendChar(')')
		return
	case ast.SubLinkType_ANY_SUBLINK:
		deparseExpr(st, sub_link.Testexpr, contextAExpr)
		if len(sub_link.OperName) > 0 {
			st.appendChar(' ')
			deparseSubqueryOp(st, sub_link.OperName)
			st.appendString(" ANY ")
		} else {
			st.appendString(" IN ")
		}
		st.appendChar('(')
		deparseSelectStmt(st, sub_link.Subselect.GetSelectStmt(), contextNone)
		st.appendChar(')')
		return
	case ast.SubLinkType_ROWCOMPARE_SUBLINK:
		// Not present in raw parse trees
		return
	case ast.SubLinkType_EXPR_SUBLINK:
		st.appendString("(")
		deparseSelectStmt(st, sub_link.Subselect.GetSelectStmt(), contextNone)
		st.appendChar(')')
		return
	case ast.SubLinkType_MULTIEXPR_SUBLINK:
		// Not present in raw parse trees
		return
	case ast.SubLinkType_ARRAY_SUBLINK:
		st.appendString("ARRAY(")
		deparseSelectStmt(st, sub_link.Subselect.GetSelectStmt(), contextNone)
		st.appendChar(')')
		return
	case ast.SubLinkType_CTE_SUBLINK: /* for SubPlans only */
		// Not present in raw parse trees
		return
	}
}

func needsParensAsBExpr(node *ast.Node) bool {
	if node == nil {
		return false
	}
	return node.GetBoolExpr() != nil || node.GetBooleanTest() != nil || node.GetNullTest() != nil || node.GetAExpr() != nil
}

func deparseAExpr(st *state, a_expr *ast.A_Expr, ctx nodeContext) {
	need_lexpr_parens := needsParensAsBExpr(a_expr.Lexpr)
	need_rexpr_parens := needsParensAsBExpr(a_expr.Rexpr)

	switch a_expr.Kind {
	case ast.A_Expr_Kind_AEXPR_OP: /* normal operator */
		{
			if a_expr.Lexpr != nil {
				if need_lexpr_parens {
					st.appendChar('(')
				}
				deparseExpr(st, a_expr.Lexpr, ctx)
				if need_lexpr_parens {
					st.appendChar(')')
				}
				st.appendChar(' ')
			}
			deparseQualOp(st, a_expr.Name)
			if a_expr.Rexpr != nil {
				st.appendChar(' ')
				if need_rexpr_parens {
					st.appendChar('(')
				}
				deparseExpr(st, a_expr.Rexpr, ctx)
				if need_rexpr_parens {
					st.appendChar(')')
				}
			}
		}
		return
	case ast.A_Expr_Kind_AEXPR_OP_ANY: /* scalar op ANY (array) */
		deparseExpr(st, a_expr.Lexpr, ctx)
		st.appendChar(' ')
		deparseSubqueryOp(st, a_expr.Name)
		st.appendString(" ANY(")
		deparseExpr(st, a_expr.Rexpr, ctx)
		st.appendChar(')')
		return
	case ast.A_Expr_Kind_AEXPR_OP_ALL: /* scalar op ALL (array) */
		deparseExpr(st, a_expr.Lexpr, ctx)
		st.appendChar(' ')
		deparseSubqueryOp(st, a_expr.Name)
		st.appendString(" ALL(")
		deparseExpr(st, a_expr.Rexpr, ctx)
		st.appendChar(')')
		return
	case ast.A_Expr_Kind_AEXPR_DISTINCT: /* IS DISTINCT FROM - name must be "=" */
		if need_lexpr_parens {
			st.appendChar('(')
		}
		deparseExpr(st, a_expr.Lexpr, ctx)
		if need_lexpr_parens {
			st.appendChar(')')
		}
		st.appendString(" IS DISTINCT FROM ")
		if need_rexpr_parens {
			st.appendChar('(')
		}
		deparseExpr(st, a_expr.Rexpr, ctx)
		if need_rexpr_parens {
			st.appendChar(')')
		}
		return
	case ast.A_Expr_Kind_AEXPR_NOT_DISTINCT: /* IS NOT DISTINCT FROM - name must be "=" */
		deparseExpr(st, a_expr.Lexpr, ctx)
		st.appendString(" IS NOT DISTINCT FROM ")
		deparseExpr(st, a_expr.Rexpr, ctx)
		return
	case ast.A_Expr_Kind_AEXPR_NULLIF: /* NULLIF - name must be "=" */
		st.appendString("NULLIF(")
		deparseExpr(st, a_expr.Lexpr, ctx)
		st.appendString(", ")
		deparseExpr(st, a_expr.Rexpr, ctx)
		st.appendChar(')')
		return
	case ast.A_Expr_Kind_AEXPR_IN: /* [NOT] IN - name must be "=" or "<>" */
		deparseExpr(st, a_expr.Lexpr, ctx)
		st.appendChar(' ')
		name := a_expr.Name[0].GetString_().GetSval()
		if name == "=" {
			st.appendString("IN ")
		} else if name == "<>" {
			st.appendString("NOT IN ")
		}
		st.appendChar('(')
		if a_expr.Rexpr.GetSubLink() != nil {
			deparseSubLink(st, a_expr.Rexpr.GetSubLink())
		} else {
			deparseExprList(st, a_expr.Rexpr.GetList().Items)
		}
		st.appendChar(')')
		return
	case ast.A_Expr_Kind_AEXPR_LIKE: /* [NOT] LIKE - name must be "~~" or "!~~" */
		deparseExpr(st, a_expr.Lexpr, ctx)
		st.appendChar(' ')

		name := a_expr.Name[0].GetString_().GetSval()
		if name == "~~" {
			st.appendString("LIKE ")
		} else if name == "!~~" {
			st.appendString("NOT LIKE ")
		}

		deparseExpr(st, a_expr.Rexpr, ctx)
		return
	case ast.A_Expr_Kind_AEXPR_ILIKE: /* [NOT] ILIKE - name must be "~~*" or "!~~*" */
		deparseExpr(st, a_expr.Lexpr, ctx)
		st.appendChar(' ')

		name := a_expr.Name[0].GetString_().GetSval()
		if name == "~~*" {
			st.appendString("ILIKE ")
		} else if name == "!~~*" {
			st.appendString("NOT ILIKE ")
		}

		deparseExpr(st, a_expr.Rexpr, ctx)
		return
	case ast.A_Expr_Kind_AEXPR_SIMILAR: /* [NOT] SIMILAR - name must be "~" or "!~" */
		deparseExpr(st, a_expr.Lexpr, ctx)
		st.appendChar(' ')

		name := a_expr.Name[0].GetString_().GetSval()
		if name == "~" {
			st.appendString("SIMILAR TO ")
		} else if name == "!~" {
			st.appendString("NOT SIMILAR TO ")
		}

		n := a_expr.Rexpr.GetFuncCall()

		deparseExpr(st, n.Args[0], ctx)
		if len(n.Args) == 2 {
			st.appendString(" ESCAPE ")
			deparseExpr(st, n.Args[1], ctx)
		}

		return
	case ast.A_Expr_Kind_AEXPR_BETWEEN, /* name must be "BETWEEN" */
		ast.A_Expr_Kind_AEXPR_NOT_BETWEEN,     /* name must be "NOT BETWEEN" */
		ast.A_Expr_Kind_AEXPR_BETWEEN_SYM,     /* name must be "BETWEEN SYMMETRIC" */
		ast.A_Expr_Kind_AEXPR_NOT_BETWEEN_SYM: /* name must be "NOT BETWEEN SYMMETRIC" */
		deparseExpr(st, a_expr.Lexpr, ctx)
		st.appendChar(' ')
		st.appendString(strVal(a_expr.Name[0]))
		st.appendChar(' ')

		items := a_expr.Rexpr.GetList().Items
		for i, item := range items {
			deparseExpr(st, item, ctx)
			if i < len(items)-1 {
				st.appendString(" AND ")
			}
		}
		return
	}
}
