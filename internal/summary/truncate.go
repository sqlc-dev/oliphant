package summary

import (
	"strings"

	"github.com/sqlc-dev/oliphant/ast"
	"github.com/sqlc-dev/oliphant/internal/deparse"
	"github.com/sqlc-dev/oliphant/internal/rawwalk"
)

// Port of pg_query_summary_truncate.c: deparse the statements; if the result
// is over the limit, collect the possible truncations (target lists, WHERE
// clauses, VALUES lists, INSERT column lists, CTE bodies), sort them deepest
// then longest first, and apply them cumulatively until the deparse fits;
// fall back to a plain multibyte-aware chop.

type truncationAttr int

const (
	truncTargetList truncationAttr = iota
	truncWhereClause
	truncValuesLists
	truncCols
	truncCTEQuery
)

type possibleTruncation struct {
	attr   truncationAttr
	node   any
	depth  int32
	length int32
}

type truncationState struct {
	truncations []*possibleTruncation
	depth       int32
	empties     map[any]bool
}

// truncate is pg_query_summary_truncate.
func truncate(stmts []*ast.RawStmt, truncateLimit int, empties map[any]bool) string {
	state := &truncationState{empties: empties}

	output := deparseRawStmtList(stmts, empties)
	if len(output) <= truncateLimit {
		return output
	}

	if len(stmts) > 0 {
		state.generate(rawwalk.RawStmtList(stmts))
	}
	pgQsortTruncations(state.truncations)
	return applyTruncations(stmts, state, truncateLimit)
}

// truncateMbstr is truncate_mbstr: chop to max_chars characters (multibyte
// aware, pg_mblen-style) with a "..." suffix. max_chars is a size_t in C, so
// a limit below 3 wraps around and the clip keeps everything, appending the
// ellipsis to the full string.
func truncateMbstr(s string, maxChars int) string {
	if mbstrlen(s) <= maxChars {
		return s
	}
	nBytes := len(s)
	if maxChars >= 3 {
		nBytes = mbCharClipLen(s, maxChars-3)
	}
	return s[:nBytes] + "..."
}

func (ts *truncationState) add(attr truncationAttr, node any, length int32) {
	// Don't bother truncating if it won't become shorter.
	if length <= 3 {
		return
	}
	ts.truncations = append(ts.truncations, &possibleTruncation{
		attr:   attr,
		node:   node,
		depth:  ts.depth,
		length: length,
	})
}

func (ts *truncationState) addWhereClause(node any, whereClause *ast.Node) {
	if whereClause == nil {
		return
	}
	ts.add(truncWhereClause, node, whereClauseLen(whereClause))
}

// generate is generate_possible_truncations.
func (ts *truncationState) generate(item any) bool {
	c := rawwalk.Concrete(item)
	if rawwalk.IsNil(c) {
		return false
	}

	switch v := c.(type) {
	case *ast.RawStmt:
		return ts.generate(v.Stmt)

	case *ast.SelectStmt:
		if len(v.TargetList) != 0 {
			ts.add(truncTargetList, v, selectTargetListLen(v.TargetList))
		}
		ts.addWhereClause(v, v.WhereClause)
		if len(v.ValuesLists) != 0 {
			ts.add(truncValuesLists, v, selectValuesListsLen(v.ValuesLists))
		}

	case *ast.InsertStmt:
		if len(v.Cols) != 0 {
			ts.add(truncCols, v, colsLen(v.Cols))
		}

	case *ast.UpdateStmt:
		if len(v.TargetList) != 0 {
			ts.add(truncTargetList, v, updateTargetListLen(v.TargetList))
		}
		ts.addWhereClause(v, v.WhereClause)

	case *ast.DeleteStmt:
		ts.addWhereClause(v, v.WhereClause)

	case *ast.CopyStmt:
		ts.addWhereClause(v, v.WhereClause)

	case *ast.IndexStmt:
		ts.addWhereClause(v, v.WhereClause)

	case *ast.RuleStmt:
		ts.addWhereClause(v, v.WhereClause)

	case *ast.CommonTableExpr:
		if v.Ctequery != nil {
			ts.add(truncCTEQuery, v, int32(deparseStmtLen(v.Ctequery)))
		}

	case *ast.InferClause:
		ts.addWhereClause(v, v.WhereClause)

	case *ast.OnConflictClause:
		if len(v.TargetList) != 0 {
			ts.add(truncTargetList, v, updateTargetListLen(v.TargetList))
		}
		ts.addWhereClause(v, v.WhereClause)
	}

	// The supports gate lives in walkChildren's default arm; the depth
	// save/restore brackets the descent exactly as the C walker call does
	// (list nodes count as a level, matching WALK on a List field).
	oldDepth := ts.depth
	ts.depth++
	result := rawwalk.WalkChildren(item, ts.generate)
	ts.depth = oldDepth
	return result
}

// cmpPossibleTruncations is cmp_possible_truncations: deepest first, then
// longest first.
func cmpPossibleTruncations(a, b *possibleTruncation) int {
	if d := b.depth - a.depth; d != 0 {
		return int(d)
	}
	return int(b.length - a.length)
}

// globalReplace is global_replace: in-place scan-and-splice; equivalent to a
// left-to-right non-overlapping replace when the replacement is no longer
// than the pattern (asserted in C).
func globalReplace(s, pattern, replacement string) string {
	return strings.ReplaceAll(s, pattern, replacement)
}

// applyTruncations is apply_truncations: apply cumulatively in sorted order,
// re-deparsing after each, until the output fits.
func applyTruncations(stmts []*ast.RawStmt, state *truncationState, truncationLimit int) string {
	output := ""
	deparsed := false

	for _, truncation := range state.truncations {
		switch v := truncation.node.(type) {
		case *ast.SelectStmt:
			switch truncation.attr {
			case truncTargetList:
				v.TargetList = []*ast.Node{dummyTargetNode()}
			case truncWhereClause:
				v.WhereClause = dummyColumnNode()
			case truncValuesLists:
				v.ValuesLists = []*ast.Node{{Node: &ast.Node_List{List: &ast.List{Items: []*ast.Node{dummyColumnNode()}}}}}
			}
		case *ast.UpdateStmt:
			switch truncation.attr {
			case truncTargetList:
				v.TargetList = []*ast.Node{dummyTargetNode()}
			case truncWhereClause:
				v.WhereClause = dummyColumnNode()
			}
		case *ast.InsertStmt:
			v.Cols = []*ast.Node{dummyTargetNode()}
		case *ast.DeleteStmt:
			v.WhereClause = dummyColumnNode()
		case *ast.CopyStmt:
			v.WhereClause = dummyColumnNode()
		case *ast.IndexStmt:
			v.WhereClause = dummyColumnNode()
		case *ast.RuleStmt:
			v.WhereClause = dummyColumnNode()
		case *ast.CommonTableExpr:
			v.Ctequery = dummySelectNode(nil, dummyColumnNode(), nil)
		case *ast.InferClause:
			v.WhereClause = dummyColumnNode()
		case *ast.OnConflictClause:
			switch truncation.attr {
			case truncTargetList:
				v.TargetList = []*ast.Node{dummyTargetNode()}
			case truncWhereClause:
				v.WhereClause = dummyColumnNode()
			}
		default:
			panic(&Error{Message: "apply_truncations() got unknown truncation type"})
		}

		output = deparseRawStmtList(stmts, state.empties)
		deparsed = true

		output = globalReplace(output, `SELECT "…" AS "…"`, `SELECT "…"`)
		output = globalReplace(output, `SELECT WHERE "…"`, `"…"`)
		output = globalReplace(output, `"…"`, `...`)

		if len(output) <= truncationLimit {
			return output
		}
	}

	if !deparsed {
		output = deparseRawStmtList(stmts, state.empties)
	}
	return truncateMbstr(output, truncationLimit)
}

// --- dummy nodes -------------------------------------------------------------

func dummyColumnNode() *ast.Node {
	return &ast.Node{Node: &ast.Node_ColumnRef{ColumnRef: &ast.ColumnRef{
		Fields:   []*ast.Node{makeStringNode("…")},
		Location: 0,
	}}}
}

func dummyTargetNode() *ast.Node {
	return &ast.Node{Node: &ast.Node_ResTarget{ResTarget: &ast.ResTarget{
		Name:     "…",
		Val:      dummyColumnNode(),
		Location: 0,
	}}}
}

// dummySelect leaves every other field zeroed, like makeNode does; the
// protobuf enums for op/limitOption are their explicit SETOP_NONE /
// LIMIT_OPTION_DEFAULT values (the C zero values at the pin, patch 05).
func dummySelect(targetList []*ast.Node, whereClause *ast.Node, valuesLists []*ast.Node) *ast.SelectStmt {
	return &ast.SelectStmt{
		TargetList:  targetList,
		WhereClause: whereClause,
		ValuesLists: valuesLists,
		LimitOption: ast.LimitOption_LIMIT_OPTION_DEFAULT,
		Op:          ast.SetOperation_SETOP_NONE,
	}
}

func dummySelectNode(targetList []*ast.Node, whereClause *ast.Node, valuesLists []*ast.Node) *ast.Node {
	return &ast.Node{Node: &ast.Node_SelectStmt{SelectStmt: dummySelect(targetList, whereClause, valuesLists)}}
}

func dummyRangeVarX() *ast.RangeVar {
	return &ast.RangeVar{Relname: "x", Inh: true, Relpersistence: "p", Location: 0}
}

// dummyInsert sets override = 1 exactly as the C code does — which is
// OVERRIDING_USER_VALUE in the C enum (protobuf value 2, after the UNDEFINED
// slot), inflating the measured length by "OVERRIDING USER VALUE ". A sort
// key quirk, ported faithfully.
func dummyInsert(cols []*ast.Node) *ast.Node {
	return &ast.Node{Node: &ast.Node_InsertStmt{InsertStmt: &ast.InsertStmt{
		Relation: dummyRangeVarX(),
		Cols:     cols,
		Override: ast.OverridingKind_OVERRIDING_USER_VALUE,
	}}}
}

func dummyUpdate(targetList []*ast.Node) *ast.Node {
	return &ast.Node{Node: &ast.Node_UpdateStmt{UpdateStmt: &ast.UpdateStmt{
		Relation:   dummyRangeVarX(),
		TargetList: targetList,
	}}}
}

// --- length measurement ------------------------------------------------------

func selectTargetListLen(nodes []*ast.Node) int32 {
	return int32(deparseStmtLen(dummySelectNode(nodes, nil, nil))) - 7 /* "SELECT " */
}

func selectValuesListsLen(nodes []*ast.Node) int32 {
	return int32(deparseStmtLen(dummySelectNode(nil, nil, nodes))) - 9 /* "VALUES ()" */
}

func updateTargetListLen(nodes []*ast.Node) int32 {
	return int32(deparseStmtLen(dummyUpdate(nodes))) - 13 /* "UPDATE x SET " */
}

func whereClauseLen(node *ast.Node) int32 {
	return int32(deparseStmtLen(dummySelectNode(nil, node, nil))) - 13 /* "SELECT WHERE " */
}

func colsLen(nodes []*ast.Node) int32 {
	return int32(deparseStmtLen(dummyInsert(nodes))) - 31 /* "INSERT INTO x () DEFAULT VALUES" */
}

// deparseStmtLen is deparse_stmt_len: wrap in a RawStmt and measure.
func deparseStmtLen(node *ast.Node) int {
	raw := &ast.RawStmt{Stmt: node, StmtLocation: -1, StmtLen: 0}
	return len(deparseRawStmtList([]*ast.RawStmt{raw}, nil))
}

// deparseRawStmtList is deparse_raw_stmt_list: statements joined with "; ".
// A deparse failure ereports in C and unwinds to pg_query_summary's catch
// block; the panic here unwinds to Summarize's recover the same way.
func deparseRawStmtList(stmts []*ast.RawStmt, empties map[any]bool) string {
	out, err := deparse.DeparseWithEmpties(&ast.ParseResult{Stmts: stmts}, empties)
	if err != nil {
		panic(&Error{Message: err.Error()})
	}
	return out
}

// --- multibyte helpers (mb/mbutils.c, UTF-8 database encoding) ---------------

// pgUtfMblen is pg_utf_mblen: sequence length from the first byte.
func pgUtfMblen(b byte) int {
	switch {
	case b&0x80 == 0:
		return 1
	case b&0xE0 == 0xC0:
		return 2
	case b&0xF0 == 0xE0:
		return 3
	case b&0xF8 == 0xF0:
		return 4
	default:
		return 1
	}
}

// mbstrlen is pg_mbstrlen.
func mbstrlen(s string) int {
	n := 0
	for i := 0; i < len(s); i += pgUtfMblen(s[i]) {
		n++
	}
	return n
}

// mbCharClipLen is pg_mbcharcliplen: byte length of the first limit
// characters.
func mbCharClipLen(s string, limit int) int {
	chars, i := 0, 0
	for i < len(s) && chars < limit {
		i += pgUtfMblen(s[i])
		chars++
	}
	if i > len(s) {
		return len(s)
	}
	return i
}
