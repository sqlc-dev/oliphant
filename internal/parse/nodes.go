package parse

import (
	"github.com/sqlc-dev/oliphant/ast"
)

// Node-wrapper constructors: the grammar actions build the generated
// protobuf structs directly, and every Node-typed position needs the oneof
// wrapper. These helpers keep the productions readable.

func nStr(s string) *ast.Node {
	return &ast.Node{Node: &ast.Node_String_{String_: &ast.String{Sval: s}}}
}

func nInteger(i int32) *ast.Node {
	return &ast.Node{Node: &ast.Node_Integer{Integer: &ast.Integer{Ival: i}}}
}

func nList(items []*ast.Node) *ast.Node {
	return &ast.Node{Node: &ast.Node_List{List: &ast.List{Items: items}}}
}

func nAConst(c *ast.A_Const) *ast.Node {
	return &ast.Node{Node: &ast.Node_AConst{AConst: c}}
}

func nColumnRef(c *ast.ColumnRef) *ast.Node {
	return &ast.Node{Node: &ast.Node_ColumnRef{ColumnRef: c}}
}

func nParamRef(pr *ast.ParamRef) *ast.Node {
	return &ast.Node{Node: &ast.Node_ParamRef{ParamRef: pr}}
}

func nAExpr(e *ast.A_Expr) *ast.Node {
	return &ast.Node{Node: &ast.Node_AExpr{AExpr: e}}
}

func nBoolExpr(e *ast.BoolExpr) *ast.Node {
	return &ast.Node{Node: &ast.Node_BoolExpr{BoolExpr: e}}
}

func nTypeCast(tc *ast.TypeCast) *ast.Node {
	return &ast.Node{Node: &ast.Node_TypeCast{TypeCast: tc}}
}

func nCollateClause(c *ast.CollateClause) *ast.Node {
	return &ast.Node{Node: &ast.Node_CollateClause{CollateClause: c}}
}

func nFuncCall(f *ast.FuncCall) *ast.Node {
	return &ast.Node{Node: &ast.Node_FuncCall{FuncCall: f}}
}

func nAStar() *ast.Node {
	return &ast.Node{Node: &ast.Node_AStar{AStar: &ast.A_Star{}}}
}

func nAIndices(ai *ast.A_Indices) *ast.Node {
	return &ast.Node{Node: &ast.Node_AIndices{AIndices: ai}}
}

func nAIndirection(a *ast.A_Indirection) *ast.Node {
	return &ast.Node{Node: &ast.Node_AIndirection{AIndirection: a}}
}

func nAArrayExpr(a *ast.A_ArrayExpr) *ast.Node {
	return &ast.Node{Node: &ast.Node_AArrayExpr{AArrayExpr: a}}
}

func nResTarget(rt *ast.ResTarget) *ast.Node {
	return &ast.Node{Node: &ast.Node_ResTarget{ResTarget: rt}}
}

func nSubLink(s *ast.SubLink) *ast.Node {
	return &ast.Node{Node: &ast.Node_SubLink{SubLink: s}}
}

func nSelectStmt(s *ast.SelectStmt) *ast.Node {
	return &ast.Node{Node: &ast.Node_SelectStmt{SelectStmt: s}}
}

func nRangeVar(r *ast.RangeVar) *ast.Node {
	return &ast.Node{Node: &ast.Node_RangeVar{RangeVar: r}}
}

func nRangeSubselect(r *ast.RangeSubselect) *ast.Node {
	return &ast.Node{Node: &ast.Node_RangeSubselect{RangeSubselect: r}}
}

func nRangeFunction(r *ast.RangeFunction) *ast.Node {
	return &ast.Node{Node: &ast.Node_RangeFunction{RangeFunction: r}}
}

func nJoinExpr(j *ast.JoinExpr) *ast.Node {
	return &ast.Node{Node: &ast.Node_JoinExpr{JoinExpr: j}}
}

func nSortBy(s *ast.SortBy) *ast.Node {
	return &ast.Node{Node: &ast.Node_SortBy{SortBy: s}}
}

func nWindowDef(w *ast.WindowDef) *ast.Node {
	return &ast.Node{Node: &ast.Node_WindowDef{WindowDef: w}}
}

func nLockingClause(l *ast.LockingClause) *ast.Node {
	return &ast.Node{Node: &ast.Node_LockingClause{LockingClause: l}}
}

func nCommonTableExpr(c *ast.CommonTableExpr) *ast.Node {
	return &ast.Node{Node: &ast.Node_CommonTableExpr{CommonTableExpr: c}}
}

func nCaseExpr(c *ast.CaseExpr) *ast.Node {
	return &ast.Node{Node: &ast.Node_CaseExpr{CaseExpr: c}}
}

func nCaseWhen(w *ast.CaseWhen) *ast.Node {
	return &ast.Node{Node: &ast.Node_CaseWhen{CaseWhen: w}}
}

func nRowExpr(r *ast.RowExpr) *ast.Node {
	return &ast.Node{Node: &ast.Node_RowExpr{RowExpr: r}}
}

func nGroupingFunc(g *ast.GroupingFunc) *ast.Node {
	return &ast.Node{Node: &ast.Node_GroupingFunc{GroupingFunc: g}}
}

func nGroupingSet(g *ast.GroupingSet) *ast.Node {
	return &ast.Node{Node: &ast.Node_GroupingSet{GroupingSet: g}}
}

func nNullTest(n *ast.NullTest) *ast.Node {
	return &ast.Node{Node: &ast.Node_NullTest{NullTest: n}}
}

func nBooleanTest(b *ast.BooleanTest) *ast.Node {
	return &ast.Node{Node: &ast.Node_BooleanTest{BooleanTest: b}}
}

func nNamedArgExpr(na *ast.NamedArgExpr) *ast.Node {
	return &ast.Node{Node: &ast.Node_NamedArgExpr{NamedArgExpr: na}}
}

func nSQLValueFunction(s *ast.SQLValueFunction) *ast.Node {
	return &ast.Node{Node: &ast.Node_SqlvalueFunction{SqlvalueFunction: s}}
}

func nMinMaxExpr(m *ast.MinMaxExpr) *ast.Node {
	return &ast.Node{Node: &ast.Node_MinMaxExpr{MinMaxExpr: m}}
}

func nCoalesceExpr(c *ast.CoalesceExpr) *ast.Node {
	return &ast.Node{Node: &ast.Node_CoalesceExpr{CoalesceExpr: c}}
}

func nSetToDefault(s *ast.SetToDefault) *ast.Node {
	return &ast.Node{Node: &ast.Node_SetToDefault{SetToDefault: s}}
}

func nXmlExpr(x *ast.XmlExpr) *ast.Node {
	return &ast.Node{Node: &ast.Node_XmlExpr{XmlExpr: x}}
}

func nXmlSerialize(x *ast.XmlSerialize) *ast.Node {
	return &ast.Node{Node: &ast.Node_XmlSerialize{XmlSerialize: x}}
}

// asAConst returns the A_Const inside a node, or nil.
func asAConst(n *ast.Node) *ast.A_Const {
	if w, ok := n.Node.(*ast.Node_AConst); ok {
		return w.AConst
	}
	return nil
}

// asSubLink returns the SubLink inside a node, or nil.
func asSubLink(n *ast.Node) *ast.SubLink {
	if w, ok := n.Node.(*ast.Node_SubLink); ok {
		return w.SubLink
	}
	return nil
}

// asBoolExpr returns the BoolExpr inside a node, or nil.
func asBoolExpr(n *ast.Node) *ast.BoolExpr {
	if w, ok := n.Node.(*ast.Node_BoolExpr); ok {
		return w.BoolExpr
	}
	return nil
}

// asFuncCall returns the FuncCall inside a node, or nil.
func asFuncCall(n *ast.Node) *ast.FuncCall {
	if w, ok := n.Node.(*ast.Node_FuncCall); ok {
		return w.FuncCall
	}
	return nil
}

// asSelectStmt returns the SelectStmt inside a node, or nil.
func asSelectStmt(n *ast.Node) *ast.SelectStmt {
	if w, ok := n.Node.(*ast.Node_SelectStmt); ok {
		return w.SelectStmt
	}
	return nil
}

// asString returns the String inside a node, or nil.
func asString(n *ast.Node) *ast.String {
	if w, ok := n.Node.(*ast.Node_String_); ok {
		return w.String_
	}
	return nil
}

// asList returns the List inside a node, or nil.
func asList(n *ast.Node) *ast.List {
	if w, ok := n.Node.(*ast.Node_List); ok {
		return w.List
	}
	return nil
}

// asAStar reports whether the node is an A_Star.
func isAStar(n *ast.Node) bool {
	_, ok := n.Node.(*ast.Node_AStar)
	return ok
}

// isAIndices reports whether the node is an A_Indices.
func isAIndices(n *ast.Node) bool {
	_, ok := n.Node.(*ast.Node_AIndices)
	return ok
}

func nFloat(s string) *ast.Node {
	return &ast.Node{Node: &ast.Node_Float{Float: &ast.Float{Fval: s}}}
}

func nBoolean(b bool) *ast.Node {
	return &ast.Node{Node: &ast.Node_Boolean{Boolean: &ast.Boolean{Boolval: b}}}
}

func nTypeName(t *ast.TypeName) *ast.Node {
	return &ast.Node{Node: &ast.Node_TypeName{TypeName: t}}
}

func nDefElem(d *ast.DefElem) *ast.Node {
	return &ast.Node{Node: &ast.Node_DefElem{DefElem: d}}
}

func nIndexElem(e *ast.IndexElem) *ast.Node {
	return &ast.Node{Node: &ast.Node_IndexElem{IndexElem: e}}
}

func nMultiAssignRef(m *ast.MultiAssignRef) *ast.Node {
	return &ast.Node{Node: &ast.Node_MultiAssignRef{MultiAssignRef: m}}
}

func nCurrentOfExpr(c *ast.CurrentOfExpr) *ast.Node {
	return &ast.Node{Node: &ast.Node_CurrentOfExpr{CurrentOfExpr: c}}
}

func nMergeWhenClause(m *ast.MergeWhenClause) *ast.Node {
	return &ast.Node{Node: &ast.Node_MergeWhenClause{MergeWhenClause: m}}
}

func nInsertStmt(s *ast.InsertStmt) *ast.Node {
	return &ast.Node{Node: &ast.Node_InsertStmt{InsertStmt: s}}
}

func nUpdateStmt(s *ast.UpdateStmt) *ast.Node {
	return &ast.Node{Node: &ast.Node_UpdateStmt{UpdateStmt: s}}
}

func nDeleteStmt(s *ast.DeleteStmt) *ast.Node {
	return &ast.Node{Node: &ast.Node_DeleteStmt{DeleteStmt: s}}
}

func nMergeStmt(s *ast.MergeStmt) *ast.Node {
	return &ast.Node{Node: &ast.Node_MergeStmt{MergeStmt: s}}
}

func nCopyStmt(s *ast.CopyStmt) *ast.Node {
	return &ast.Node{Node: &ast.Node_CopyStmt{CopyStmt: s}}
}

func nPrepareStmt(s *ast.PrepareStmt) *ast.Node {
	return &ast.Node{Node: &ast.Node_PrepareStmt{PrepareStmt: s}}
}

func nExecuteStmt(s *ast.ExecuteStmt) *ast.Node {
	return &ast.Node{Node: &ast.Node_ExecuteStmt{ExecuteStmt: s}}
}

func nDeallocateStmt(s *ast.DeallocateStmt) *ast.Node {
	return &ast.Node{Node: &ast.Node_DeallocateStmt{DeallocateStmt: s}}
}

func nDeclareCursorStmt(s *ast.DeclareCursorStmt) *ast.Node {
	return &ast.Node{Node: &ast.Node_DeclareCursorStmt{DeclareCursorStmt: s}}
}

func nFetchStmt(s *ast.FetchStmt) *ast.Node {
	return &ast.Node{Node: &ast.Node_FetchStmt{FetchStmt: s}}
}

func nClosePortalStmt(s *ast.ClosePortalStmt) *ast.Node {
	return &ast.Node{Node: &ast.Node_ClosePortalStmt{ClosePortalStmt: s}}
}
