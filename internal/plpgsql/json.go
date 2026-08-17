package plpgsql

import (
	"fmt"
	"strings"

	"github.com/sqlc-dev/oliphant/internal/emit"
)

// Port of pg_query_json_plpgsql.c: the hand-rolled JSON dump of a compiled
// function. Field order, zero-suppression, and the trailing-delimiter dance
// are the C macros' exactly; strings escape through the same _outToken as
// the parse-tree emitter.

type jbuf struct {
	b strings.Builder
}

func (j *jbuf) s(s string)   { j.b.WriteString(s) }
func (j *jbuf) c(c byte)     { j.b.WriteByte(c) }
func (j *jbuf) tok(s string) { emit.Token(&j.b, s) }

// removeTrailingDelimiter is the helper from pg_query_json_helper.c.
func (j *jbuf) removeTrailingDelimiter() {
	s := j.b.String()
	if len(s) > 0 && s[len(s)-1] == ',' {
		j.b.Reset()
		j.b.WriteString(s[:len(s)-1])
	}
}

func (j *jbuf) nodeType(label string) { j.s(`"` + label + `":{`) }

func (j *jbuf) intField(name string, v int) {
	if v != 0 {
		fmt.Fprintf(&j.b, `"%s":%d,`, name, v)
	}
}

func (j *jbuf) longField(name string, v int64) {
	if v != 0 {
		fmt.Fprintf(&j.b, `"%s":%d,`, name, v)
	}
}

func (j *jbuf) enumField(name string, v int) {
	fmt.Fprintf(&j.b, `"%s":%d,`, name, v)
}

func (j *jbuf) boolField(name string, v bool) {
	if v {
		j.s(`"` + name + `":true,`)
	}
}

func (j *jbuf) stringField(name, v string) {
	j.s(`"` + name + `":`)
	j.tok(v)
	j.s(",")
}

// objField is WRITE_OBJ_FIELD: `"name":{` + dump + `}},`.
func (j *jbuf) objField(name string, dump func()) {
	j.s(`"` + name + `":{`)
	dump()
	j.removeTrailingDelimiter()
	j.s("}},")
}

func (j *jbuf) exprField(name string, e *plExpr) {
	if e != nil {
		j.objField(name, func() { j.dumpExpr(e) })
	}
}

func (j *jbuf) variableField(name string, v plVariable) {
	if v != nil {
		j.objField(name, func() { j.dumpVariable(v) })
	}
}

// stmtsField is WRITE_STATEMENTS_FIELD.
func (j *jbuf) stmtsField(name string, stmts []plStmt) {
	if len(stmts) == 0 {
		return
	}
	j.s(`"` + name + `":[`)
	for _, st := range stmts {
		j.dumpStmt(st)
	}
	j.removeTrailingDelimiter()
	j.s("],")
}

// listField is WRITE_LIST_FIELD.
func listField[T any](j *jbuf, name string, items []T, dump func(T)) {
	if len(items) == 0 {
		return
	}
	j.s(`"` + name + `":[`)
	for _, it := range items {
		j.s("{")
		dump(it)
		j.removeTrailingDelimiter()
		j.s("}},")
	}
	j.removeTrailingDelimiter()
	j.s("],")
}

func (j *jbuf) dumpStmt(st plStmt) {
	j.c('{')
	switch v := st.(type) {
	case *stmtBlock:
		j.dumpBlock(v)
	case *stmtAssign:
		j.nodeType("PLpgSQL_stmt_assign")
		j.intField("lineno", v.lineno)
		j.intField("varno", v.varno)
		j.exprField("expr", v.expr)
	case *stmtIf:
		j.nodeType("PLpgSQL_stmt_if")
		j.intField("lineno", v.lineno)
		j.exprField("cond", v.cond)
		j.stmtsField("then_body", v.thenBody)
		listField(j, "elsif_list", v.elsifList, func(e *ifElsif) {
			j.nodeType("PLpgSQL_if_elsif")
			j.intField("lineno", e.lineno)
			j.exprField("cond", e.cond)
			j.stmtsField("stmts", e.stmts)
		})
		j.stmtsField("else_body", v.elseBody)
	case *stmtCase:
		j.nodeType("PLpgSQL_stmt_case")
		j.intField("lineno", v.lineno)
		j.exprField("t_expr", v.tExpr)
		j.intField("t_varno", v.tVarno)
		listField(j, "case_when_list", v.caseWhenList, func(w *caseWhen) {
			j.nodeType("PLpgSQL_case_when")
			j.intField("lineno", w.lineno)
			j.exprField("expr", w.expr)
			j.stmtsField("stmts", w.stmts)
		})
		j.boolField("have_else", v.haveElse)
		j.stmtsField("else_stmts", v.elseStmts)
	case *stmtLoop:
		j.nodeType("PLpgSQL_stmt_loop")
		j.intField("lineno", v.lineno)
		j.labelField(v.label)
		j.stmtsField("body", v.body)
	case *stmtWhile:
		j.nodeType("PLpgSQL_stmt_while")
		j.intField("lineno", v.lineno)
		j.labelField(v.label)
		j.exprField("cond", v.cond)
		j.stmtsField("body", v.body)
	case *stmtFori:
		j.nodeType("PLpgSQL_stmt_fori")
		j.intField("lineno", v.lineno)
		j.labelField(v.label)
		if v.varDat != nil {
			j.objField("var", func() { j.dumpVar(v.varDat) })
		}
		j.exprField("lower", v.lower)
		j.exprField("upper", v.upper)
		j.exprField("step", v.step)
		j.boolField("reverse", v.reverse)
		j.stmtsField("body", v.body)
	case *stmtFors:
		j.nodeType("PLpgSQL_stmt_fors")
		j.intField("lineno", v.lineno)
		j.labelField(v.label)
		j.variableField("var", v.varDat)
		j.stmtsField("body", v.body)
		j.exprField("query", v.query)
	case *stmtForc:
		j.nodeType("PLpgSQL_stmt_forc")
		j.intField("lineno", v.lineno)
		j.labelField(v.label)
		j.variableField("var", v.varDat)
		j.stmtsField("body", v.body)
		j.intField("curvar", v.curvar)
		j.exprField("argquery", v.argquery)
	case *stmtDynfors:
		j.nodeType("PLpgSQL_stmt_dynfors")
		j.intField("lineno", v.lineno)
		j.labelField(v.label)
		j.variableField("var", v.varDat)
		j.stmtsField("body", v.body)
		j.exprField("query", v.query)
		listField(j, "params", v.params, func(e *plExpr) { j.dumpExpr(e) })
	case *stmtForeachA:
		j.nodeType("PLpgSQL_stmt_foreach_a")
		j.intField("lineno", v.lineno)
		j.labelField(v.label)
		j.intField("varno", v.varno)
		j.intField("slice", v.slice)
		j.exprField("expr", v.expr)
		j.stmtsField("body", v.body)
	case *stmtExit:
		j.nodeType("PLpgSQL_stmt_exit")
		j.intField("lineno", v.lineno)
		j.boolField("is_exit", v.isExit)
		j.labelField(v.label)
		j.exprField("cond", v.cond)
	case *stmtReturn:
		j.nodeType("PLpgSQL_stmt_return")
		j.intField("lineno", v.lineno)
		j.exprField("expr", v.expr)
	case *stmtReturnNext:
		j.nodeType("PLpgSQL_stmt_return_next")
		j.intField("lineno", v.lineno)
		j.exprField("expr", v.expr)
	case *stmtReturnQuery:
		j.nodeType("PLpgSQL_stmt_return_query")
		j.intField("lineno", v.lineno)
		j.exprField("query", v.query)
		j.exprField("dynquery", v.dynquery)
		listField(j, "params", v.params, func(e *plExpr) { j.dumpExpr(e) })
	case *stmtRaise:
		j.nodeType("PLpgSQL_stmt_raise")
		j.intField("lineno", v.lineno)
		j.intField("elog_level", v.elogLevel)
		if v.condname != "" {
			j.stringField("condname", v.condname)
		}
		if v.hasMessage {
			j.stringField("message", v.message)
		}
		listField(j, "params", v.params, func(e *plExpr) { j.dumpExpr(e) })
		listField(j, "options", v.options, func(o *raiseOption) {
			j.nodeType("PLpgSQL_raise_option")
			j.enumField("opt_type", int(o.optType))
			j.exprField("expr", o.expr)
		})
	case *stmtAssert:
		j.nodeType("PLpgSQL_stmt_assert")
		j.intField("lineno", v.lineno)
		j.exprField("cond", v.cond)
		j.exprField("message", v.message)
	case *stmtExecsql:
		j.nodeType("PLpgSQL_stmt_execsql")
		j.intField("lineno", v.lineno)
		j.exprField("sqlstmt", v.sqlstmt)
		j.boolField("into", v.into)
		j.boolField("strict", v.strict)
		j.variableField("target", v.target)
	case *stmtDynexecute:
		j.nodeType("PLpgSQL_stmt_dynexecute")
		j.intField("lineno", v.lineno)
		j.exprField("query", v.query)
		j.boolField("into", v.into)
		j.boolField("strict", v.strict)
		j.variableField("target", v.target)
		listField(j, "params", v.params, func(e *plExpr) { j.dumpExpr(e) })
	case *stmtGetdiag:
		j.nodeType("PLpgSQL_stmt_getdiag")
		j.intField("lineno", v.lineno)
		j.boolField("is_stacked", v.isStacked)
		listField(j, "diag_items", v.diagItems, func(d *diagItem) {
			j.nodeType("PLpgSQL_diag_item")
			j.stringField("kind", getdiagKindname(d.kind))
			j.intField("target", d.target)
		})
	case *stmtOpen:
		j.nodeType("PLpgSQL_stmt_open")
		j.intField("lineno", v.lineno)
		j.intField("curvar", v.curvar)
		j.intField("cursor_options", v.cursorOptions)
		j.exprField("argquery", v.argquery)
		j.exprField("query", v.query)
		j.exprField("dynquery", v.dynquery)
		listField(j, "params", v.params, func(e *plExpr) { j.dumpExpr(e) })
	case *stmtFetch:
		j.nodeType("PLpgSQL_stmt_fetch")
		j.intField("lineno", v.lineno)
		j.variableField("target", v.target)
		j.intField("curvar", v.curvar)
		j.enumField("direction", int(v.direction))
		j.longField("how_many", v.howMany)
		j.exprField("expr", v.expr)
		j.boolField("is_move", v.isMove)
		j.boolField("returns_multiple_rows", v.returnsMultipleRows)
	case *stmtClose:
		j.nodeType("PLpgSQL_stmt_close")
		j.intField("lineno", v.lineno)
		j.intField("curvar", v.curvar)
	case *stmtPerform:
		j.nodeType("PLpgSQL_stmt_perform")
		j.intField("lineno", v.lineno)
		j.exprField("expr", v.expr)
	case *stmtCall:
		j.nodeType("PLpgSQL_stmt_call")
		j.intField("lineno", v.lineno)
		j.exprField("expr", v.expr)
		j.boolField("is_call", v.isCall)
		j.variableField("target", v.target)
	case *stmtCommit:
		j.nodeType("PLpgSQL_stmt_commit")
		j.intField("lineno", v.lineno)
		j.boolField("chain", v.chain)
	case *stmtRollback:
		j.nodeType("PLpgSQL_stmt_rollback")
		j.intField("lineno", v.lineno)
		j.boolField("chain", v.chain)
	}
	j.removeTrailingDelimiter()
	j.s("}},")
}

// labelField is WRITE_STRING_FIELD(label): NULL labels ("" here) are omitted.
func (j *jbuf) labelField(label string) {
	if label != "" {
		j.stringField("label", label)
	}
}

func (j *jbuf) dumpBlock(v *stmtBlock) {
	j.nodeType("PLpgSQL_stmt_block")
	j.intField("lineno", v.lineno)
	j.labelField(v.label)
	j.stmtsField("body", v.body)
	if v.exceptions != nil {
		j.objField("exceptions", func() {
			j.nodeType("PLpgSQL_exception_block")
			listField(j, "exc_list", v.exceptions.excList, func(e *plException) {
				j.nodeType("PLpgSQL_exception")
				j.s(`"conditions":[`)
				for cond := e.conditions; cond != nil; cond = cond.next {
					j.s("{")
					j.nodeType("PLpgSQL_condition")
					if cond.condname != "" {
						j.stringField("condname", cond.condname)
					}
					j.removeTrailingDelimiter()
					j.s("}},")
				}
				j.removeTrailingDelimiter()
				j.s("],")
				j.stmtsField("action", e.action)
			})
		})
	}
	j.removeTrailingDelimiter()
}

func (j *jbuf) dumpExpr(e *plExpr) {
	j.nodeType("PLpgSQL_expr")
	j.stringField("query", e.query)
	j.enumField("parseMode", int(e.parseMode))
}

func (j *jbuf) dumpVariable(v plVariable) {
	switch d := v.(type) {
	case *plRec:
		j.dumpRecord(d)
	case *plVar:
		j.dumpVar(d)
	case *plRow:
		j.dumpRow(d)
	}
}

func (j *jbuf) dumpType(t *plType) {
	j.nodeType("PLpgSQL_type")
	if t.typname != "" {
		j.stringField("typname", t.typname)
	}
}

func (j *jbuf) dumpVar(v *plVar) {
	j.nodeType("PLpgSQL_var")
	j.stringField("refname", v.refname)
	j.intField("lineno", v.lineno)
	if v.datatype != nil {
		j.objField("datatype", func() { j.dumpType(v.datatype) })
	}
	j.boolField("isconst", v.isconst)
	j.boolField("notnull", v.notnull)
	j.exprField("default_val", v.defaultVal)
	j.exprField("cursor_explicit_expr", v.cursorExplicitExpr)
	j.intField("cursor_explicit_argrow", v.cursorExplicitArgrow)
	j.intField("cursor_options", v.cursorOptions)
}

func (j *jbuf) dumpRow(v *plRow) {
	j.nodeType("PLpgSQL_row")
	j.stringField("refname", v.refname)
	j.intField("lineno", v.lineno)
	j.s(`"fields":`)
	j.c('[')
	for i := range v.fieldnames {
		if v.fieldnames[i] != "" {
			j.c('{')
			j.stringField("name", v.fieldnames[i])
			j.intField("varno", v.varnos[i])
			j.removeTrailingDelimiter()
			j.s("},")
		} else {
			j.s("null,")
		}
	}
	j.removeTrailingDelimiter()
	j.s("],")
}

func (j *jbuf) dumpRecord(v *plRec) {
	j.nodeType("PLpgSQL_rec")
	j.stringField("refname", v.refname)
	j.intField("dno", v.dno)
	j.intField("lineno", v.lineno)
}

func (j *jbuf) dumpRecordField(v *plRecfield) {
	j.nodeType("PLpgSQL_recfield")
	j.stringField("fieldname", v.fieldname)
	j.intField("recparentno", v.recparentno)
}

// toJSON is plpgsqlToJSON.
func toJSON(fn *plFunction) string {
	j := &jbuf{}
	j.c('{')

	j.nodeType("PLpgSQL_function")
	j.intField("new_varno", fn.newVarno)
	j.intField("old_varno", fn.oldVarno)

	j.s(`"datums":`)
	j.c('[')
	for _, d := range fn.datums {
		j.c('{')
		switch v := d.(type) {
		case *plVar:
			j.dumpVar(v)
		case *plRow:
			j.dumpRow(v)
		case *plRec:
			j.dumpRecord(v)
		case *plRecfield:
			j.dumpRecordField(v)
		}
		j.removeTrailingDelimiter()
		j.s("}},")
	}
	j.removeTrailingDelimiter()
	j.s("],")

	if fn.action != nil {
		j.objField("action", func() { j.dumpBlock(fn.action) })
	}

	j.removeTrailingDelimiter()
	j.s("}}")
	return j.b.String()
}
