// raw_expression_tree_walker (src/backend/nodes/nodeFuncs.c, PostgreSQL 17)
// ported over the protobuf tree. Every WALK(child) goes back through
// state.walk, so the const_record_walker interceptions (A_Const, ParamRef,
// TypeName, SelectStmt, DefElem, …) apply at every level, exactly as the C
// callback does.
//
// Node types missing from the switch are the ones raw_expression_tree_walker
// elogs about; pg_query_normalize.c catches that error per-node and stops
// walking the subtree, so here they simply return false.
package normalize

import "github.com/sqlc-dev/oliphant/ast"

// rawWalk is raw_expression_tree_walker_impl for the node types
// const_record_walker does not intercept.
func (s *state) rawWalk(n *ast.Node) bool {
	switch v := n.Node.(type) {
	case *ast.Node_JsonFormat, *ast.Node_SetToDefault, *ast.Node_CurrentOfExpr,
		*ast.Node_SqlvalueFunction, *ast.Node_Integer, *ast.Node_Float,
		*ast.Node_Boolean, *ast.Node_String_, *ast.Node_BitString,
		*ast.Node_AStar, *ast.Node_MergeSupportFunc:
		// primitive node types with no subnodes
		return false
	case *ast.Node_Alias:
		// we assume the colnames list isn't interesting
		return false
	case *ast.Node_RangeVar:
		return s.walkAlias(v.RangeVar.Alias)
	case *ast.Node_GroupingFunc:
		return s.walkList(v.GroupingFunc.Args)
	case *ast.Node_SubLink:
		if s.walk(v.SubLink.Testexpr) {
			return true
		}
		// we assume the operName is not interesting
		return s.walk(v.SubLink.Subselect)
	case *ast.Node_CaseExpr:
		if s.walk(v.CaseExpr.Arg) {
			return true
		}
		// we assume walker doesn't care about CaseWhens, either
		for _, item := range v.CaseExpr.Args {
			when := item.GetCaseWhen()
			if when == nil {
				continue
			}
			if s.walk(when.Expr) {
				return true
			}
			if s.walk(when.Result) {
				return true
			}
		}
		return s.walk(v.CaseExpr.Defresult)
	case *ast.Node_RowExpr:
		// Assume colnames isn't interesting
		return s.walkList(v.RowExpr.Args)
	case *ast.Node_CoalesceExpr:
		return s.walkList(v.CoalesceExpr.Args)
	case *ast.Node_MinMaxExpr:
		return s.walkList(v.MinMaxExpr.Args)
	case *ast.Node_XmlExpr:
		if s.walkList(v.XmlExpr.NamedArgs) {
			return true
		}
		// we assume walker doesn't care about arg_names
		return s.walkList(v.XmlExpr.Args)
	case *ast.Node_JsonReturning:
		return s.walkJsonReturning(v.JsonReturning)
	case *ast.Node_JsonValueExpr:
		return s.walkJsonValueExpr(v.JsonValueExpr)
	case *ast.Node_JsonParseExpr:
		if s.walkJsonValueExpr(v.JsonParseExpr.Expr) {
			return true
		}
		return s.walkJsonOutput(v.JsonParseExpr.Output)
	case *ast.Node_JsonScalarExpr:
		if s.walk(v.JsonScalarExpr.Expr) {
			return true
		}
		return s.walkJsonOutput(v.JsonScalarExpr.Output)
	case *ast.Node_JsonSerializeExpr:
		if s.walkJsonValueExpr(v.JsonSerializeExpr.Expr) {
			return true
		}
		return s.walkJsonOutput(v.JsonSerializeExpr.Output)
	case *ast.Node_JsonConstructorExpr:
		c := v.JsonConstructorExpr
		if s.walkList(c.Args) {
			return true
		}
		if s.walk(c.Func) {
			return true
		}
		if s.walk(c.Coercion) {
			return true
		}
		return s.walkJsonReturning(c.Returning)
	case *ast.Node_JsonIsPredicate:
		return s.walk(v.JsonIsPredicate.Expr)
	case *ast.Node_JsonArgument:
		return s.walkJsonValueExpr(v.JsonArgument.Val)
	case *ast.Node_JsonFuncExpr:
		f := v.JsonFuncExpr
		if s.walkJsonValueExpr(f.ContextItem) {
			return true
		}
		if s.walk(f.Pathspec) {
			return true
		}
		if s.walkList(f.Passing) {
			return true
		}
		if s.walkJsonOutput(f.Output) {
			return true
		}
		if s.walkJsonBehavior(f.OnEmpty) {
			return true
		}
		return s.walkJsonBehavior(f.OnError)
	case *ast.Node_JsonBehavior:
		return s.walkJsonBehavior(v.JsonBehavior)
	case *ast.Node_JsonTable:
		jt := v.JsonTable
		if s.walkJsonValueExpr(jt.ContextItem) {
			return true
		}
		if s.walkJsonTablePathSpec(jt.Pathspec) {
			return true
		}
		if s.walkList(jt.Passing) {
			return true
		}
		if s.walkList(jt.Columns) {
			return true
		}
		return s.walkJsonBehavior(jt.OnError)
	case *ast.Node_JsonTableColumn:
		c := v.JsonTableColumn
		if s.walkTypeName(c.TypeName) {
			return true
		}
		if s.walkJsonBehavior(c.OnEmpty) {
			return true
		}
		if s.walkJsonBehavior(c.OnError) {
			return true
		}
		return s.walkList(c.Columns)
	case *ast.Node_JsonTablePathSpec:
		return s.walkJsonTablePathSpec(v.JsonTablePathSpec)
	case *ast.Node_NullTest:
		return s.walk(v.NullTest.Arg)
	case *ast.Node_BooleanTest:
		return s.walk(v.BooleanTest.Arg)
	case *ast.Node_JoinExpr:
		j := v.JoinExpr
		if s.walk(j.Larg) {
			return true
		}
		if s.walk(j.Rarg) {
			return true
		}
		if s.walk(j.Quals) {
			return true
		}
		// using list is deemed uninteresting
		return s.walkAlias(j.Alias)
	case *ast.Node_IntoClause:
		return s.walkIntoClause(v.IntoClause)
	case *ast.Node_List:
		return s.walkList(v.List.Items)
	case *ast.Node_InsertStmt:
		stmt := v.InsertStmt
		if s.walkRangeVar(stmt.Relation) {
			return true
		}
		if s.walkList(stmt.Cols) {
			return true
		}
		if s.walk(stmt.SelectStmt) {
			return true
		}
		if s.walkOnConflictClause(stmt.OnConflictClause) {
			return true
		}
		if s.walkList(stmt.ReturningList) {
			return true
		}
		return s.walkWithClause(stmt.WithClause)
	case *ast.Node_DeleteStmt:
		stmt := v.DeleteStmt
		if s.walkRangeVar(stmt.Relation) {
			return true
		}
		if s.walkList(stmt.UsingClause) {
			return true
		}
		if s.walk(stmt.WhereClause) {
			return true
		}
		if s.walkList(stmt.ReturningList) {
			return true
		}
		return s.walkWithClause(stmt.WithClause)
	case *ast.Node_UpdateStmt:
		stmt := v.UpdateStmt
		if s.walkRangeVar(stmt.Relation) {
			return true
		}
		if s.walkList(stmt.TargetList) {
			return true
		}
		if s.walk(stmt.WhereClause) {
			return true
		}
		if s.walkList(stmt.FromClause) {
			return true
		}
		if s.walkList(stmt.ReturningList) {
			return true
		}
		return s.walkWithClause(stmt.WithClause)
	case *ast.Node_MergeStmt:
		stmt := v.MergeStmt
		if s.walkRangeVar(stmt.Relation) {
			return true
		}
		if s.walk(stmt.SourceRelation) {
			return true
		}
		if s.walk(stmt.JoinCondition) {
			return true
		}
		if s.walkList(stmt.MergeWhenClauses) {
			return true
		}
		if s.walkList(stmt.ReturningList) {
			return true
		}
		return s.walkWithClause(stmt.WithClause)
	case *ast.Node_MergeWhenClause:
		m := v.MergeWhenClause
		if s.walk(m.Condition) {
			return true
		}
		if s.walkList(m.TargetList) {
			return true
		}
		return s.walkList(m.Values)
	case *ast.Node_PlassignStmt:
		if s.walkList(v.PlassignStmt.Indirection) {
			return true
		}
		if v.PlassignStmt.Val != nil {
			return s.walkSelect(v.PlassignStmt.Val)
		}
		return false
	case *ast.Node_AExpr:
		if s.walk(v.AExpr.Lexpr) {
			return true
		}
		// operator name is deemed uninteresting
		return s.walk(v.AExpr.Rexpr)
	case *ast.Node_BoolExpr:
		return s.walkList(v.BoolExpr.Args)
	case *ast.Node_ColumnRef:
		// we assume the fields contain nothing interesting
		return false
	case *ast.Node_FuncCall:
		return s.walkFuncCall(v.FuncCall)
	case *ast.Node_NamedArgExpr:
		return s.walk(v.NamedArgExpr.Arg)
	case *ast.Node_AIndices:
		if s.walk(v.AIndices.Lidx) {
			return true
		}
		return s.walk(v.AIndices.Uidx)
	case *ast.Node_AIndirection:
		if s.walk(v.AIndirection.Arg) {
			return true
		}
		return s.walkList(v.AIndirection.Indirection)
	case *ast.Node_AArrayExpr:
		return s.walkList(v.AArrayExpr.Elements)
	case *ast.Node_ResTarget:
		if s.walkList(v.ResTarget.Indirection) {
			return true
		}
		return s.walk(v.ResTarget.Val)
	case *ast.Node_MultiAssignRef:
		return s.walk(v.MultiAssignRef.Source)
	case *ast.Node_TypeCast:
		if s.walk(v.TypeCast.Arg) {
			return true
		}
		return s.walkTypeName(v.TypeCast.TypeName)
	case *ast.Node_CollateClause:
		return s.walk(v.CollateClause.Arg)
	case *ast.Node_SortBy:
		return s.walk(v.SortBy.Node)
	case *ast.Node_WindowDef:
		return s.walkWindowDef(v.WindowDef)
	case *ast.Node_RangeSubselect:
		if s.walk(v.RangeSubselect.Subquery) {
			return true
		}
		return s.walkAlias(v.RangeSubselect.Alias)
	case *ast.Node_RangeFunction:
		rf := v.RangeFunction
		if s.walkList(rf.Functions) {
			return true
		}
		if s.walkAlias(rf.Alias) {
			return true
		}
		return s.walkList(rf.Coldeflist)
	case *ast.Node_RangeTableSample:
		rts := v.RangeTableSample
		if s.walk(rts.Relation) {
			return true
		}
		// method name is deemed uninteresting
		if s.walkList(rts.Args) {
			return true
		}
		return s.walk(rts.Repeatable)
	case *ast.Node_RangeTableFunc:
		rtf := v.RangeTableFunc
		if s.walk(rtf.Docexpr) {
			return true
		}
		if s.walk(rtf.Rowexpr) {
			return true
		}
		if s.walkList(rtf.Namespaces) {
			return true
		}
		if s.walkList(rtf.Columns) {
			return true
		}
		return s.walkAlias(rtf.Alias)
	case *ast.Node_RangeTableFuncCol:
		if s.walk(v.RangeTableFuncCol.Colexpr) {
			return true
		}
		return s.walk(v.RangeTableFuncCol.Coldefexpr)
	case *ast.Node_ColumnDef:
		cd := v.ColumnDef
		if s.walkTypeName(cd.TypeName) {
			return true
		}
		if s.walk(cd.RawDefault) {
			return true
		}
		if cd.CollClause != nil && s.walk(cd.CollClause.Arg) {
			return true
		}
		// for now, constraints are ignored
		return false
	case *ast.Node_IndexElem:
		// collation and opclass names are deemed uninteresting
		return s.walk(v.IndexElem.Expr)
	case *ast.Node_GroupingSet:
		return s.walkList(v.GroupingSet.Content)
	case *ast.Node_LockingClause:
		return s.walkList(v.LockingClause.LockedRels)
	case *ast.Node_XmlSerialize:
		if s.walk(v.XmlSerialize.Expr) {
			return true
		}
		return s.walkTypeName(v.XmlSerialize.TypeName)
	case *ast.Node_WithClause:
		return s.walkWithClause(v.WithClause)
	case *ast.Node_InferClause:
		return s.walkInferClause(v.InferClause)
	case *ast.Node_OnConflictClause:
		return s.walkOnConflictClause(v.OnConflictClause)
	case *ast.Node_CommonTableExpr:
		// search_clause and cycle_clause are not interesting here
		return s.walk(v.CommonTableExpr.Ctequery)
	case *ast.Node_JsonOutput:
		return s.walkJsonOutput(v.JsonOutput)
	case *ast.Node_JsonKeyValue:
		return s.walkJsonKeyValue(v.JsonKeyValue)
	case *ast.Node_JsonObjectConstructor:
		if s.walkJsonOutput(v.JsonObjectConstructor.Output) {
			return true
		}
		return s.walkList(v.JsonObjectConstructor.Exprs)
	case *ast.Node_JsonArrayConstructor:
		if s.walkJsonOutput(v.JsonArrayConstructor.Output) {
			return true
		}
		return s.walkList(v.JsonArrayConstructor.Exprs)
	case *ast.Node_JsonAggConstructor:
		return s.walkJsonAggConstructor(v.JsonAggConstructor)
	case *ast.Node_JsonObjectAgg:
		if s.walkJsonAggConstructor(v.JsonObjectAgg.Constructor) {
			return true
		}
		return s.walkJsonKeyValue(v.JsonObjectAgg.Arg)
	case *ast.Node_JsonArrayAgg:
		if s.walkJsonAggConstructor(v.JsonArrayAgg.Constructor) {
			return true
		}
		return s.walkJsonValueExpr(v.JsonArrayAgg.Arg)
	case *ast.Node_JsonArrayQueryConstructor:
		if s.walkJsonOutput(v.JsonArrayQueryConstructor.Output) {
			return true
		}
		return s.walk(v.JsonArrayQueryConstructor.Query)
	default:
		// "unrecognized node type": the elog is swallowed per-node, so the
		// subtree is simply not walked.
		return false
	}
}

// Typed-field helpers: in C these children are reached as Node pointers
// through the same walker callback; the protobuf schema types them, so each
// gets a small wrapper implementing the same case.

func (s *state) walkAlias(*ast.Alias) bool { return false }

func (s *state) walkRangeVar(rv *ast.RangeVar) bool {
	if rv == nil {
		return false
	}
	return s.walkAlias(rv.Alias)
}

func (s *state) walkTypeName(*ast.TypeName) bool {
	// const_record_walker intercepts TypeName: constants in typmods and
	// arrayBounds are not normalized.
	return false
}

func (s *state) walkIntoClause(into *ast.IntoClause) bool {
	if into == nil {
		return false
	}
	if s.walkRangeVar(into.Rel) {
		return true
	}
	// colNames, options are deemed uninteresting
	// viewQuery should be null in raw parsetree, but check it
	return s.walk(into.ViewQuery)
}

func (s *state) walkWithClause(w *ast.WithClause) bool {
	if w == nil {
		return false
	}
	return s.walkList(w.Ctes)
}

func (s *state) walkInferClause(ic *ast.InferClause) bool {
	if ic == nil {
		return false
	}
	if s.walkList(ic.IndexElems) {
		return true
	}
	return s.walk(ic.WhereClause)
}

func (s *state) walkOnConflictClause(oc *ast.OnConflictClause) bool {
	if oc == nil {
		return false
	}
	if s.walkInferClause(oc.Infer) {
		return true
	}
	if s.walkList(oc.TargetList) {
		return true
	}
	return s.walk(oc.WhereClause)
}

func (s *state) walkWindowDef(wd *ast.WindowDef) bool {
	if wd == nil {
		return false
	}
	if s.walkList(wd.PartitionClause) {
		return true
	}
	if s.walkList(wd.OrderClause) {
		return true
	}
	if s.walk(wd.StartOffset) {
		return true
	}
	return s.walk(wd.EndOffset)
}

func (s *state) walkFuncCall(fc *ast.FuncCall) bool {
	if fc == nil {
		return false
	}
	if s.walkList(fc.Args) {
		return true
	}
	if s.walkList(fc.AggOrder) {
		return true
	}
	if s.walk(fc.AggFilter) {
		return true
	}
	// function name is deemed uninteresting
	return s.walkWindowDef(fc.Over)
}

func (s *state) walkJsonReturning(jr *ast.JsonReturning) bool {
	// WALK(returning->format): JsonFormat has no subnodes.
	return false
}

func (s *state) walkJsonOutput(out *ast.JsonOutput) bool {
	if out == nil {
		return false
	}
	if s.walkTypeName(out.TypeName) {
		return true
	}
	return s.walkJsonReturning(out.Returning)
}

func (s *state) walkJsonValueExpr(jve *ast.JsonValueExpr) bool {
	if jve == nil {
		return false
	}
	if s.walk(jve.RawExpr) {
		return true
	}
	if s.walk(jve.FormattedExpr) {
		return true
	}
	// WALK(jve->format): JsonFormat has no subnodes.
	return false
}

func (s *state) walkJsonBehavior(jb *ast.JsonBehavior) bool {
	if jb == nil {
		return false
	}
	return s.walk(jb.Expr)
}

func (s *state) walkJsonTablePathSpec(sp *ast.JsonTablePathSpec) bool {
	if sp == nil {
		return false
	}
	return s.walk(sp.String_)
}

func (s *state) walkJsonKeyValue(kv *ast.JsonKeyValue) bool {
	if kv == nil {
		return false
	}
	if s.walk(kv.Key) {
		return true
	}
	return s.walkJsonValueExpr(kv.Value)
}

func (s *state) walkJsonAggConstructor(c *ast.JsonAggConstructor) bool {
	if c == nil {
		return false
	}
	if s.walkJsonOutput(c.Output) {
		return true
	}
	if s.walkList(c.AggOrder) {
		return true
	}
	if s.walk(c.AggFilter) {
		return true
	}
	return s.walkWindowDef(c.Over)
}
