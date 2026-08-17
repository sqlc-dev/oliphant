package summary

import (
	"strings"

	"github.com/sqlc-dev/oliphant/ast"
	"github.com/sqlc-dev/oliphant/internal/deparse"
	"github.com/sqlc-dev/oliphant/internal/rawwalk"
)

// Error carries an ereport raised during the summary walk (there is exactly
// one reachable site: makeRangeVarFromNameList).
type Error struct {
	Message  string
	Filename string
	Funcname string
}

func (e *Error) Error() string { return e.Message }

// Summarize is pg_query_summary_internal (pg_query_summary.c) minus the raw
// parse, which the caller has already done: the summary walk, the
// statement-type walk, and — when truncateLimit is not -1 — the truncation
// pass.
func Summarize(tree *ast.ParseResult, truncateLimit int) (result *ast.SummaryResult, err error) {
	return SummarizeWithEmpties(tree, truncateLimit, nil)
}

// SummarizeWithEmpties is Summarize with the parser's present-but-empty
// string set, which the truncation deparse needs (the C summary deparses
// the original tree, where COMMENT ... IS ” is non-NULL).
func SummarizeWithEmpties(tree *ast.ParseResult, truncateLimit int, empties map[any]bool) (result *ast.SummaryResult, err error) {
	defer func() {
		if r := recover(); r != nil {
			if se, ok := r.(*Error); ok {
				result, err = nil, se
				return
			}
			panic(r)
		}
	}()

	s := &state{}
	root := rawwalk.RawStmtList(tree.Stmts)

	// pg_query_summary_walk: the main walk, then the deferred range-var pass
	// (CTEs can be referenced before they are defined, so tables are only
	// resolved against cte_names once the walk has seen everything).
	if len(root) > 0 {
		s.walk(root)
	}
	for _, rv := range s.rangeVars {
		s.handleRangeVar(rv.node, rv.context)
	}

	// pg_query_summary_statement_walk.
	if len(root) > 0 {
		s.stmtWalk(root)
	}

	res := &ast.SummaryResult{
		Tables:         s.tables,
		CteNames:       s.cteNames,
		Functions:      s.functions,
		FilterColumns:  s.filterColumns,
		StatementTypes: s.statementTypes,
	}
	if len(s.aliases) > 0 {
		// The C list becomes a protobuf map; duplicate keys collapse with the
		// last occurrence winning, exactly as protobuf map decoding does.
		res.Aliases = map[string]string{}
		for _, kv := range s.aliases {
			res.Aliases[kv.key] = kv.value
		}
	}

	if truncateLimit != -1 {
		res.TruncatedQuery = truncate(tree.Stmts, truncateLimit, empties)
	}
	return res, nil
}

type rangeVarCtx struct {
	node    *ast.RangeVar
	context ast.SummaryResult_Context
}

type aliasKV struct{ key, value string }

type state struct {
	rangeVars         []rangeVarCtx
	tables            []*ast.SummaryResult_Table
	aliases           []aliasKV
	cteNames          []string
	functions         []*ast.SummaryResult_Function
	filterColumns     []*ast.SummaryResult_FilterColumn
	statementTypes    []string
	saveFilterColumns bool
}

// addRangeVar is add_range_var (pg_query_summary.c): wrap a FROM-clause item
// (or statement target relation) and stash it for the deferred pass.
func (s *state) addRangeVar(item any, context ast.SummaryResult_Context) {
	c := rawwalk.Concrete(item)
	if rawwalk.IsNil(c) {
		return
	}
	switch v := c.(type) {
	case *ast.JoinExpr:
		s.addRangeVar(v.Larg, context)
		s.addRangeVar(v.Rarg, context)
		// je->quals is handled by the regular tree walk.
	case *ast.RangeTableSample:
		s.addRangeVar(v.Relation, context)
	case *ast.JsonTable, *ast.RangeTableFunc, *ast.RangeFunction, *ast.RangeSubselect:
		// These are handled by the tree walker.
	case *ast.RangeVar:
		s.rangeVars = append(s.rangeVars, rangeVarCtx{node: v, context: context})
	default:
		// elog(ERROR, "unexpected node type"): unreachable for grammar-built
		// trees (table_ref only produces the shapes above).
		panic(&Error{Message: "unexpected node type in add_range_var"})
	}
}

func (s *state) addRangeVarList(clause []*ast.Node, context ast.SummaryResult_Context) {
	for _, n := range clause {
		s.addRangeVar(n, context)
	}
}

// addFunction is add_function: name via NameListToString, schema from the
// qualification, function from the last name part.
func (s *state) addFunction(funcname []*ast.Node, context ast.SummaryResult_Context) {
	fn := &ast.SummaryResult_Function{
		Name:    nameListToString(funcname),
		Context: context,
	}
	switch len(funcname) {
	case 3:
		fn.SchemaName = funcname[1].GetString_().GetSval()
	case 2:
		fn.SchemaName = funcname[0].GetString_().GetSval()
	}
	if len(funcname) > 0 {
		fn.FunctionName = funcname[len(funcname)-1].GetString_().GetSval()
	}
	s.functions = append(s.functions, fn)
}

// walk is pg_query_summary_walk_impl. Returning true aborts the walk up the
// chain, exactly like a C tree-walker callback.
func (s *state) walk(item any) bool {
	c := rawwalk.Concrete(item)
	if rawwalk.IsNil(c) {
		return false
	}

	switch v := c.(type) {
	case *ast.CommonTableExpr:
		s.cteNames = append(s.cteNames, v.Ctename)

	case *ast.CallStmt:
		if v.Funccall != nil {
			return s.walk(v.Funccall)
		}
		// The C case falls through into the SelectStmt arm and reads garbage;
		// unreachable, since the grammar always sets funccall.
		return false

	case *ast.SelectStmt:
		// Don't descend into sub-SELECTs in the WHERE clause when saving
		// filter columns.
		if s.saveFilterColumns {
			return true
		}
		if v.Op == ast.SetOperation_SETOP_NONE {
			s.addRangeVarList(v.FromClause, ast.SummaryResult_Select)
		}
		// For SETOP_UNION/EXCEPT/INTERSECT the walkChildren call at the end
		// handles larg/rarg.
		if v.IntoClause != nil {
			s.addRangeVar(v.IntoClause.Rel, ast.SummaryResult_DDL)
		}
		if v.WhereClause != nil {
			// The WHERE clause is intentionally walked twice; when saving
			// filter columns we don't descend into sub-SELECTs.
			s.saveFilterColumns = true
			s.walk(v.WhereClause)
			s.saveFilterColumns = false
		}

	case *ast.InsertStmt:
		s.addRangeVar(v.Relation, ast.SummaryResult_DML)

	case *ast.UpdateStmt:
		s.addRangeVar(v.Relation, ast.SummaryResult_DML)
		s.addRangeVarList(v.FromClause, ast.SummaryResult_Select)

	case *ast.MergeStmt:
		s.addRangeVar(v.Relation, ast.SummaryResult_DML)

	case *ast.DeleteStmt:
		s.addRangeVar(v.Relation, ast.SummaryResult_DML)
		s.addRangeVarList(v.UsingClause, ast.SummaryResult_Select)

	case *ast.CopyStmt:
		s.addRangeVar(v.Relation, ast.SummaryResult_Select)
		s.walk(v.Query)

	case *ast.AlterTableStmt:
		// `ALTER INDEX index_name` is ignored.
		if v.Objtype != ast.ObjectType_OBJECT_INDEX {
			s.addRangeVar(v.Relation, ast.SummaryResult_DDL)
		}

	case *ast.CreateStmt:
		s.addRangeVar(v.Relation, ast.SummaryResult_DDL)

	case *ast.CreateTableAsStmt:
		if v.Into != nil && v.Into.Rel != nil {
			s.addRangeVar(v.Into.Rel, ast.SummaryResult_DDL)
		}
		s.walk(v.Query)

	case *ast.TruncateStmt:
		s.addRangeVarList(v.Relations, ast.SummaryResult_DDL)

	case *ast.ViewStmt:
		s.addRangeVar(v.View, ast.SummaryResult_DDL)
		s.walk(v.Query)

	case *ast.IndexStmt:
		s.addRangeVar(v.Relation, ast.SummaryResult_DDL)
		for _, ie := range v.IndexParams {
			s.walk(ie.GetIndexElem().GetExpr())
		}
		s.walk(v.WhereClause)

	case *ast.CreateTrigStmt:
		s.addRangeVar(v.Relation, ast.SummaryResult_DDL)

	case *ast.RuleStmt:
		s.addRangeVar(v.Relation, ast.SummaryResult_DDL)

	case *ast.VacuumStmt:
		for _, r := range v.Rels {
			s.addRangeVar(r.GetVacuumRelation().GetRelation(), ast.SummaryResult_DDL)
		}

	case *ast.RefreshMatViewStmt:
		s.addRangeVar(v.Relation, ast.SummaryResult_DDL)

	case *ast.DropStmt:
		switch v.RemoveType {
		case ast.ObjectType_OBJECT_TABLE:
			for _, obj := range v.Objects {
				if list := obj.GetList(); list != nil {
					if len(list.Items) == 0 {
						continue
					}
					s.addRangeVar(makeRangeVarFromNameList(list.Items), ast.SummaryResult_DDL)
				}
			}
		case ast.ObjectType_OBJECT_RULE, ast.ObjectType_OBJECT_TRIGGER:
			for _, obj := range v.Objects {
				if list := obj.GetList(); list != nil {
					// Ignore the last string (the rule/trigger name).
					items := list.Items[:max(len(list.Items)-1, 0)]
					if len(items) == 0 {
						continue
					}
					s.addRangeVar(makeRangeVarFromNameList(items), ast.SummaryResult_DDL)
				}
			}
		case ast.ObjectType_OBJECT_FUNCTION:
			if len(v.Objects) > 0 {
				if obj := v.Objects[0].GetObjectWithArgs(); obj != nil && len(obj.Objname) > 0 {
					s.addFunction(obj.Objname, ast.SummaryResult_DDL)
				}
			}
		}

	case *ast.GrantStmt:
		switch v.Objtype {
		case ast.ObjectType_OBJECT_TABLE:
			for _, obj := range v.Objects {
				if rv := obj.GetRangeVar(); rv != nil {
					s.addRangeVar(rv, ast.SummaryResult_DDL)
				}
			}
		case ast.ObjectType_OBJECT_FUNCTION, ast.ObjectType_OBJECT_PROCEDURE,
			ast.ObjectType_OBJECT_ROUTINE:
			for _, obj := range v.Objects {
				if owa := obj.GetObjectWithArgs(); owa != nil && len(owa.Objname) > 0 {
					s.addFunction(owa.Objname, ast.SummaryResult_DDL)
				}
			}
		}

	case *ast.LockStmt:
		s.addRangeVarList(v.Relations, ast.SummaryResult_DDL)

	case *ast.ExplainStmt:
		return s.walk(v.Query)

	case *ast.CreateFunctionStmt:
		s.addFunction(v.Funcname, ast.SummaryResult_DDL)

	case *ast.RenameStmt:
		switch v.RenameType {
		case ast.ObjectType_OBJECT_TABLE:
			s.addRangeVar(v.Relation, ast.SummaryResult_DDL)
			s.addRangeVar(&ast.RangeVar{
				Schemaname:     v.Relation.GetSchemaname(),
				Relname:        v.Newname,
				Inh:            true,
				Relpersistence: "p",
				Location:       -1,
			}, ast.SummaryResult_DDL)
		case ast.ObjectType_OBJECT_FUNCTION, ast.ObjectType_OBJECT_PROCEDURE,
			ast.ObjectType_OBJECT_ROUTINE:
			if owa := v.Object.GetObjectWithArgs(); owa != nil {
				s.addFunction(owa.Objname, ast.SummaryResult_DDL)
				s.addFunction([]*ast.Node{makeStringNode(v.Newname)}, ast.SummaryResult_DDL)
			}
		}

	case *ast.PrepareStmt:
		return s.walk(v.Query)

	case *ast.RawStmt:
		return s.walk(v.Stmt)

	case *ast.FuncCall:
		s.addFunction(v.Funcname, ast.SummaryResult_Call)

	case *ast.ColumnRef:
		// Added to avoid needing subselect_items, like Rust and Ruby use.
		if !s.saveFilterColumns {
			break
		}
		if len(v.Fields) == 0 {
			break
		}
		column := v.Fields[len(v.Fields)-1].GetString_()
		// Check that we are not in the A_Star case here. (Columns can be
		// stars, but nothing else can.)
		if column == nil {
			break
		}
		fc := &ast.SummaryResult_FilterColumn{Column: column.Sval}
		switch len(v.Fields) {
		case 2:
			fc.TableName = v.Fields[0].GetString_().GetSval()
		case 3:
			fc.SchemaName = v.Fields[0].GetString_().GetSval()
			fc.TableName = v.Fields[1].GetString_().GetSval()
		}
		s.filterColumns = append(s.filterColumns, fc)
	}

	return rawwalk.WalkChildren(item, s.walk)
}

// rangeVarToName is range_var_to_name: quote_identifier-rendered, dotted.
func rangeVarToName(rv *ast.RangeVar) string {
	var b strings.Builder
	if rv.Schemaname != "" {
		b.WriteString(deparse.QuoteIdentifier(rv.Schemaname))
		b.WriteByte('.')
	}
	b.WriteString(deparse.QuoteIdentifier(rv.Relname))
	return b.String()
}

// cteNamesInclude is cte_names_include: exact-match scan.
func (s *state) cteNamesInclude(relname string) bool {
	for _, name := range s.cteNames {
		if name == relname {
			return true
		}
	}
	return false
}

// handleRangeVar is handle_range_var: emit a table (unless the name is a CTE)
// and an alias entry.
func (s *state) handleRangeVar(rv *ast.RangeVar, context ast.SummaryResult_Context) {
	if rv == nil {
		return
	}
	if !s.cteNamesInclude(rv.Relname) {
		s.tables = append(s.tables, &ast.SummaryResult_Table{
			Name:       rangeVarToName(rv),
			SchemaName: rv.Schemaname,
			TableName:  rv.Relname,
			Context:    context,
		})
	}
	if rv.Alias != nil {
		s.aliases = append(s.aliases, aliasKV{key: rv.Alias.Aliasname, value: rangeVarToName(rv)})
	}
}

// nameListToString is NameListToString (catalog/namespace.c): dotted, no
// quoting; A_Star renders as "*".
func nameListToString(names []*ast.Node) string {
	var b strings.Builder
	for i, n := range names {
		if i > 0 {
			b.WriteByte('.')
		}
		if s := n.GetString_(); s != nil {
			b.WriteString(s.Sval)
		} else {
			b.WriteString("*")
		}
	}
	return b.String()
}

// makeRangeVarFromNameList (makefuncs.c; vendored by libpg_query into
// src_backend_catalog_namespace.c, which is the filename its ereport carries).
func makeRangeVarFromNameList(names []*ast.Node) *ast.RangeVar {
	rv := &ast.RangeVar{Inh: true, Relpersistence: "p", Location: -1}
	switch len(names) {
	case 1:
		rv.Relname = names[0].GetString_().GetSval()
	case 2:
		rv.Schemaname = names[0].GetString_().GetSval()
		rv.Relname = names[1].GetString_().GetSval()
	case 3:
		rv.Catalogname = names[0].GetString_().GetSval()
		rv.Schemaname = names[1].GetString_().GetSval()
		rv.Relname = names[2].GetString_().GetSval()
	default:
		panic(&Error{
			Message:  "improper relation name (too many dotted names): " + nameListToString(names),
			Filename: "src_backend_catalog_namespace.c",
			Funcname: "makeRangeVarFromNameList",
		})
	}
	return rv
}

func makeStringNode(s string) *ast.Node {
	return &ast.Node{Node: &ast.Node_String_{String_: &ast.String{Sval: s}}}
}
