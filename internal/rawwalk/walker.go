// Package rawwalk is raw_expression_tree_walker (src/backend/nodes/
// nodeFuncs.c, PostgreSQL 17) over the protobuf tree, gated exactly as
// pg_query_raw_tree_walker_supports allows: node types outside the walker's
// switch return false without descending. It drives the summary walks and
// the PL/pgSQL driver's statement collection.
package rawwalk

import (
	"reflect"

	"github.com/sqlc-dev/oliphant/ast"
)

// The C walks operate on Node* where a node may be a T_List; the callback is
// invoked for list nodes too (which matters for the truncation walk's depth
// accounting). NodeList and RawStmtList model those list nodes over the
// protobuf tree.
type NodeList []*ast.Node

type RawStmtList []*ast.RawStmt

// concrete unwraps an *ast.Node into its inner concrete pointer (the oneof
// member), so callbacks can switch on one representation regardless of
// whether a child arrived generically (*ast.Node) or as a typed field
// (*ast.RangeVar, *ast.FuncCall, ...).
func Concrete(item any) any {
	n, ok := item.(*ast.Node)
	if !ok {
		return item
	}
	if n == nil || n.Node == nil {
		return nil
	}
	// Every oneof wrapper (ast.Node_SelectStmt, ...) has exactly one field.
	return reflect.ValueOf(n.Node).Elem().Field(0).Interface()
}

// wl boxes a list-valued child the way the C walker passes a List node to the
// callback: NIL (empty) lists short-circuit to "no child".
func wl(list []*ast.Node) any {
	if len(list) == 0 {
		return nil
	}
	return NodeList(list)
}

// isNil reports whether a child slot is empty (NULL in the C tree).
func IsNil(item any) bool {
	if item == nil {
		return true
	}
	v := reflect.ValueOf(item)
	return v.Kind() == reflect.Pointer && v.IsNil()
}

// walkChildren is raw_expression_tree_walker_impl (src/backend/nodes/
// nodeFuncs.c, PostgreSQL 17) gated exactly as pg_query_raw_tree_walker_supports
// allows: node types outside the walker's switch return false without
// descending (the summary C files check the gate before calling the walker;
// folding it into the default arm is equivalent). Child order is the C
// walker's, verbatim — it decides the order of every summary list.
func WalkChildren(item any, cb func(any) bool) bool {
	w := func(children ...any) bool {
		for _, c := range children {
			if IsNil(c) {
				continue
			}
			if cb(c) {
				return true
			}
		}
		return false
	}

	switch v := Concrete(item).(type) {
	case RawStmtList:
		for _, rs := range v {
			if cb(rs) {
				return true
			}
		}
		return false
	case NodeList:
		for _, n := range v {
			if n == nil || n.Node == nil {
				continue
			}
			if cb(n) {
				return true
			}
		}
		return false
	case *ast.List:
		// A List that appears as a node (e.g. the [function, coldeflist]
		// sublists of RangeFunction->functions).
		for _, n := range v.Items {
			if n == nil || n.Node == nil {
				continue
			}
			if cb(n) {
				return true
			}
		}
		return false
	case *ast.JsonFormat, *ast.SetToDefault, *ast.CurrentOfExpr,
		*ast.SQLValueFunction, *ast.Integer, *ast.Float, *ast.Boolean,
		*ast.String, *ast.BitString, *ast.ParamRef, *ast.A_Const, *ast.A_Star,
		*ast.MergeSupportFunc:
		// primitive node types with no subnodes
		return false
	case *ast.Alias:
		// we assume the colnames list isn't interesting
		return false
	case *ast.RangeVar:
		return w(v.Alias)
	case *ast.GroupingFunc:
		return w(wl(v.Args))
	case *ast.SubLink:
		return w(v.Testexpr, v.Subselect)
	case *ast.CaseExpr:
		if w(v.Arg) {
			return true
		}
		// we assume walker doesn't care about CaseWhens, either
		for _, item := range v.Args {
			when := item.GetCaseWhen()
			if w(when.GetExpr(), when.GetResult()) {
				return true
			}
		}
		return w(v.Defresult)
	case *ast.RowExpr:
		// Assume colnames isn't interesting
		return w(wl(v.Args))
	case *ast.CoalesceExpr:
		return w(wl(v.Args))
	case *ast.MinMaxExpr:
		return w(wl(v.Args))
	case *ast.XmlExpr:
		// we assume walker doesn't care about arg_names
		return w(wl(v.NamedArgs), wl(v.Args))
	case *ast.JsonReturning:
		return w(v.Format)
	case *ast.JsonValueExpr:
		return w(v.RawExpr, v.FormattedExpr, v.Format)
	case *ast.JsonParseExpr:
		return w(v.Expr, v.Output)
	case *ast.JsonScalarExpr:
		return w(v.Expr, v.Output)
	case *ast.JsonSerializeExpr:
		return w(v.Expr, v.Output)
	case *ast.JsonConstructorExpr:
		return w(wl(v.Args), v.Func, v.Coercion, v.Returning)
	case *ast.JsonIsPredicate:
		return w(v.Expr)
	case *ast.JsonArgument:
		return w(v.Val)
	case *ast.JsonFuncExpr:
		return w(v.ContextItem, v.Pathspec, wl(v.Passing), v.Output, v.OnEmpty, v.OnError)
	case *ast.JsonBehavior:
		return w(v.Expr)
	case *ast.JsonTable:
		return w(v.ContextItem, v.Pathspec, wl(v.Passing), wl(v.Columns), v.OnError)
	case *ast.JsonTableColumn:
		return w(v.TypeName, v.OnEmpty, v.OnError, wl(v.Columns))
	case *ast.JsonTablePathSpec:
		return w(v.String_)
	case *ast.NullTest:
		return w(v.Arg)
	case *ast.BooleanTest:
		return w(v.Arg)
	case *ast.JoinExpr:
		// using list is deemed uninteresting
		return w(v.Larg, v.Rarg, v.Quals, v.Alias)
	case *ast.IntoClause:
		// colNames, options are deemed uninteresting
		// viewQuery should be null in raw parsetree, but check it
		return w(v.Rel, v.ViewQuery)
	case *ast.InsertStmt:
		return w(v.Relation, wl(v.Cols), v.SelectStmt, v.OnConflictClause,
			wl(v.ReturningList), v.WithClause)
	case *ast.DeleteStmt:
		return w(v.Relation, wl(v.UsingClause), v.WhereClause,
			wl(v.ReturningList), v.WithClause)
	case *ast.UpdateStmt:
		return w(v.Relation, wl(v.TargetList), v.WhereClause, wl(v.FromClause),
			wl(v.ReturningList), v.WithClause)
	case *ast.MergeStmt:
		return w(v.Relation, v.SourceRelation, v.JoinCondition,
			wl(v.MergeWhenClauses), wl(v.ReturningList), v.WithClause)
	case *ast.MergeWhenClause:
		return w(v.Condition, wl(v.TargetList), wl(v.Values))
	case *ast.SelectStmt:
		return w(wl(v.DistinctClause), v.IntoClause, wl(v.TargetList),
			wl(v.FromClause), v.WhereClause, wl(v.GroupClause), v.HavingClause,
			wl(v.WindowClause), wl(v.ValuesLists), wl(v.SortClause),
			v.LimitOffset, v.LimitCount, wl(v.LockingClause), v.WithClause,
			v.Larg, v.Rarg)
	case *ast.PLAssignStmt:
		return w(wl(v.Indirection), v.Val)
	case *ast.A_Expr:
		// operator name is deemed uninteresting
		return w(v.Lexpr, v.Rexpr)
	case *ast.BoolExpr:
		return w(wl(v.Args))
	case *ast.ColumnRef:
		// we assume the fields contain nothing interesting
		return false
	case *ast.FuncCall:
		// function name is deemed uninteresting
		return w(wl(v.Args), wl(v.AggOrder), v.AggFilter, v.Over)
	case *ast.NamedArgExpr:
		return w(v.Arg)
	case *ast.A_Indices:
		return w(v.Lidx, v.Uidx)
	case *ast.A_Indirection:
		return w(v.Arg, wl(v.Indirection))
	case *ast.A_ArrayExpr:
		return w(wl(v.Elements))
	case *ast.ResTarget:
		return w(wl(v.Indirection), v.Val)
	case *ast.MultiAssignRef:
		return w(v.Source)
	case *ast.TypeCast:
		return w(v.Arg, v.TypeName)
	case *ast.CollateClause:
		return w(v.Arg)
	case *ast.SortBy:
		return w(v.Node)
	case *ast.WindowDef:
		return w(wl(v.PartitionClause), wl(v.OrderClause), v.StartOffset, v.EndOffset)
	case *ast.RangeSubselect:
		return w(v.Subquery, v.Alias)
	case *ast.RangeFunction:
		return w(wl(v.Functions), v.Alias, wl(v.Coldeflist))
	case *ast.RangeTableSample:
		// method name is deemed uninteresting
		return w(v.Relation, wl(v.Args), v.Repeatable)
	case *ast.RangeTableFunc:
		return w(v.Docexpr, v.Rowexpr, wl(v.Namespaces), wl(v.Columns), v.Alias)
	case *ast.RangeTableFuncCol:
		return w(v.Colexpr, v.Coldefexpr)
	case *ast.TypeName:
		// type name itself is deemed uninteresting
		return w(wl(v.Typmods), wl(v.ArrayBounds))
	case *ast.ColumnDef:
		// for now, constraints are ignored
		return w(v.TypeName, v.RawDefault, v.CollClause)
	case *ast.IndexElem:
		// collation and opclass names are deemed uninteresting
		return w(v.Expr)
	case *ast.GroupingSet:
		return w(wl(v.Content))
	case *ast.LockingClause:
		return w(wl(v.LockedRels))
	case *ast.XmlSerialize:
		return w(v.Expr, v.TypeName)
	case *ast.WithClause:
		return w(wl(v.Ctes))
	case *ast.InferClause:
		return w(wl(v.IndexElems), v.WhereClause)
	case *ast.OnConflictClause:
		return w(v.Infer, wl(v.TargetList), v.WhereClause)
	case *ast.CommonTableExpr:
		// search_clause and cycle_clause are not interesting here
		return w(v.Ctequery)
	case *ast.JsonOutput:
		return w(v.TypeName, v.Returning)
	case *ast.JsonKeyValue:
		return w(v.Key, v.Value)
	case *ast.JsonObjectConstructor:
		return w(v.Output, wl(v.Exprs))
	case *ast.JsonArrayConstructor:
		return w(v.Output, wl(v.Exprs))
	case *ast.JsonAggConstructor:
		return w(v.Output, wl(v.AggOrder), v.AggFilter, v.Over)
	case *ast.JsonObjectAgg:
		return w(v.Constructor, v.Arg)
	case *ast.JsonArrayAgg:
		return w(v.Constructor, v.Arg)
	case *ast.JsonArrayQueryConstructor:
		return w(v.Output, v.Query)
	default:
		// Not covered by pg_query_raw_tree_walker_supports: no descent.
		return false
	}
}
