// Package plpgsql is the hand-written port of libpg_query's PL/pgSQL support
// as pinned at 17-6.2.2: pl_gram.y + pl_scanner.c (patched, see
// internal/reference/), the pl_comp.c / pl_funcs.c subset libpg_query
// extracts (including its mocks — parse_datatype and friends do not consult
// the catalogs), pg_query_json_plpgsql.c, and the pg_query_parse_plpgsql.c
// driver behind ParsePlPgSqlToJSON.
package plpgsql

// The execution-tree node structs mirror plpgsql.h field-for-field, limited
// to what the parser fills in and the JSON dump reads.

// plpgsql.h: PLpgSQL_datum_type
type datumType int

const (
	dtypeVar datumType = iota
	dtypeRow
	dtypeRec
	dtypeRecfield
	dtypePromise
)

// plpgsql.h: PLpgSQL_type_type
type typeType int

const (
	ttypeScalar typeType = iota
	ttypeRec
	ttypePseudo
)

// plpgsql.h: PLpgSQL_nsitem_type / PLpgSQL_label_type
type nsItemType int

const (
	nsTypeLabel nsItemType = iota
	nsTypeVar
	nsTypeRec
)

type labelType int

const (
	labelBlock labelType = iota
	labelLoop
	labelOther
)

// plpgsql.h: PLpgSQL_getdiag_kind
type getdiagKind int

const (
	getdiagRowCount getdiagKind = iota
	getdiagRoutineOid
	getdiagContext
	getdiagErrorContext
	getdiagErrorDetail
	getdiagErrorHint
	getdiagReturnedSqlstate
	getdiagColumnName
	getdiagConstraintName
	getdiagDatatypeName
	getdiagMessageText
	getdiagTableName
	getdiagSchemaName
)

// pl_funcs.c: plpgsql_getdiag_kindname
func getdiagKindname(kind getdiagKind) string {
	switch kind {
	case getdiagRowCount:
		return "ROW_COUNT"
	case getdiagRoutineOid:
		return "PG_ROUTINE_OID"
	case getdiagContext:
		return "PG_CONTEXT"
	case getdiagErrorContext:
		return "PG_EXCEPTION_CONTEXT"
	case getdiagErrorDetail:
		return "PG_EXCEPTION_DETAIL"
	case getdiagErrorHint:
		return "PG_EXCEPTION_HINT"
	case getdiagReturnedSqlstate:
		return "RETURNED_SQLSTATE"
	case getdiagColumnName:
		return "COLUMN_NAME"
	case getdiagConstraintName:
		return "CONSTRAINT_NAME"
	case getdiagDatatypeName:
		return "PG_DATATYPE_NAME"
	case getdiagMessageText:
		return "MESSAGE_TEXT"
	case getdiagTableName:
		return "TABLE_NAME"
	case getdiagSchemaName:
		return "SCHEMA_NAME"
	}
	return "unknown"
}

// plpgsql.h: PLpgSQL_raise_option_type
type raiseOptionType int

const (
	raiseOptionErrcode raiseOptionType = iota
	raiseOptionMessage
	raiseOptionDetail
	raiseOptionHint
	raiseOptionColumn
	raiseOptionConstraint
	raiseOptionDatatype
	raiseOptionTable
	raiseOptionSchema
)

// utils/elog.h severity levels (the subset RAISE uses).
const (
	elogDebug1  = 14
	elogLog     = 15
	elogInfo    = 17
	elogNotice  = 18
	elogWarning = 19
	elogError   = 21
)

// nodes/parsenodes.h: cursor options and FETCH_ALL
const (
	cursorOptScroll   = 0x0002
	cursorOptNoScroll = 0x0004
	cursorOptFastPlan = 0x0100

	fetchAll = int64(^uint64(0) >> 1) // FETCH_ALL = LONG_MAX
)

// nodes/parsenodes.h: FetchDirection
type fetchDirection int

const (
	fetchForward fetchDirection = iota
	fetchBackward
	fetchAbsolute
	fetchRelative
)

// nodes/parsenodes.h: RawParseMode
type rawParseMode int

const (
	rawParseDefault rawParseMode = iota
	rawParseTypeName
	rawParsePlpgsqlExpr
	rawParsePlpgsqlAssign1
	rawParsePlpgsqlAssign2
	rawParsePlpgsqlAssign3
)

// plpgsql.h: PLpgSQL_resolve_option
type resolveOption int

const (
	resolveError resolveOption = iota
	resolveVariable
	resolveColumn
)

// --- datums ------------------------------------------------------------------

// datum is the common interface of PLpgSQL_datum structs (dno + dtype).
type datum interface {
	datumNo() int
	setDatumNo(int)
	datumType() datumType
}

type datumCommon struct {
	dno int
}

func (d *datumCommon) datumNo() int     { return d.dno }
func (d *datumCommon) setDatumNo(n int) { d.dno = n }

// plType is PLpgSQL_type (the fields the libpg_query build fills).
type plType struct {
	typname    string // patched in: the source spelling (or quoted qualified name)
	ttype      typeType
	typoid     uint32 // only refcursor/text/bool/int4 distinctions matter
	atttypmod  int32
	collation  uint32
	typisarray bool
}

// plVar is PLpgSQL_var.
type plVar struct {
	datumCommon
	refname  string
	lineno   int
	datatype *plType
	isconst  bool
	notnull  bool
	// isPromise marks the trigger/event-trigger special variables whose
	// dtype do_compile rewrites to PLPGSQL_DTYPE_PROMISE.
	isPromise bool

	defaultVal           *plExpr
	cursorExplicitExpr   *plExpr
	cursorExplicitArgrow int
	cursorOptions        int
}

func (v *plVar) datumType() datumType {
	if v.isPromise {
		return dtypePromise
	}
	return dtypeVar
}

// plRow is PLpgSQL_row.
type plRow struct {
	datumCommon
	refname    string
	lineno     int
	fieldnames []string
	varnos     []int
}

func (*plRow) datumType() datumType { return dtypeRow }

// plRec is PLpgSQL_rec.
type plRec struct {
	datumCommon
	refname    string
	lineno     int
	datatype   *plType
	rectypeid  uint32
	firstfield int
	isconst    bool
}

func (*plRec) datumType() datumType { return dtypeRec }

// plRecfield is PLpgSQL_recfield.
type plRecfield struct {
	datumCommon
	fieldname   string
	recparentno int
	nextfield   int
}

func (*plRecfield) datumType() datumType { return dtypeRecfield }

// variable() reports whether a datum is a PLpgSQL_variable (var/row/rec) and
// gives access to the shared header fields.
type plVariable interface {
	datum
	varRefname() string
	varIsConst() bool
}

func (v *plVar) varRefname() string { return v.refname }
func (v *plVar) varIsConst() bool   { return v.isconst }
func (r *plRow) varRefname() string { return r.refname }
func (r *plRow) varIsConst() bool   { return false }
func (r *plRec) varRefname() string { return r.refname }
func (r *plRec) varIsConst() bool   { return r.isconst }

// plExpr is PLpgSQL_expr.
type plExpr struct {
	query     string
	parseMode rawParseMode
}

// --- statements --------------------------------------------------------------

type plStmt interface{ stmtLineno() int }

type stmtCommon struct {
	lineno int
}

func (s *stmtCommon) stmtLineno() int { return s.lineno }

type stmtBlock struct {
	stmtCommon
	label      string
	body       []plStmt
	initVarnos []int // n_initvars/initvarnos (not serialized)
	exceptions *exceptionBlock
}

type exceptionBlock struct {
	sqlstateVarno int
	sqlerrmVarno  int
	excList       []*plException
}

type plException struct {
	lineno     int
	conditions *plCondition
	action     []plStmt
}

type plCondition struct {
	sqlerrstate int
	condname    string
	next        *plCondition
}

type stmtAssign struct {
	stmtCommon
	varno int
	expr  *plExpr
}

type stmtIf struct {
	stmtCommon
	cond      *plExpr
	thenBody  []plStmt
	elsifList []*ifElsif
	elseBody  []plStmt
}

type ifElsif struct {
	lineno int
	cond   *plExpr
	stmts  []plStmt
}

type stmtCase struct {
	stmtCommon
	tExpr        *plExpr
	tVarno       int
	caseWhenList []*caseWhen
	haveElse     bool
	elseStmts    []plStmt
}

type caseWhen struct {
	lineno int
	expr   *plExpr
	stmts  []plStmt
}

type stmtLoop struct {
	stmtCommon
	label string
	body  []plStmt
}

type stmtWhile struct {
	stmtCommon
	label string
	cond  *plExpr
	body  []plStmt
}

type stmtFori struct {
	stmtCommon
	label   string
	varDat  *plVar
	lower   *plExpr
	upper   *plExpr
	step    *plExpr
	reverse bool
	body    []plStmt
}

type stmtFors struct {
	stmtCommon
	label  string
	varDat plVariable
	body   []plStmt
	query  *plExpr
}

type stmtForc struct {
	stmtCommon
	label    string
	varDat   plVariable
	body     []plStmt
	curvar   int
	argquery *plExpr
}

type stmtDynfors struct {
	stmtCommon
	label  string
	varDat plVariable
	body   []plStmt
	query  *plExpr
	params []*plExpr
}

type stmtForeachA struct {
	stmtCommon
	label string
	varno int
	slice int
	expr  *plExpr
	body  []plStmt
}

type stmtExit struct {
	stmtCommon
	isExit bool
	label  string
	cond   *plExpr
}

type stmtReturn struct {
	stmtCommon
	expr     *plExpr
	retvarno int
}

type stmtReturnNext struct {
	stmtCommon
	expr     *plExpr
	retvarno int
}

type stmtReturnQuery struct {
	stmtCommon
	query    *plExpr
	dynquery *plExpr
	params   []*plExpr
}

type stmtRaise struct {
	stmtCommon
	elogLevel  int
	condname   string
	message    string
	hasMessage bool
	params     []*plExpr
	options    []*raiseOption
}

type raiseOption struct {
	optType raiseOptionType
	expr    *plExpr
}

type stmtAssert struct {
	stmtCommon
	cond    *plExpr
	message *plExpr
}

type stmtExecsql struct {
	stmtCommon
	sqlstmt *plExpr
	into    bool
	strict  bool
	target  plVariable
}

type stmtDynexecute struct {
	stmtCommon
	query  *plExpr
	into   bool
	strict bool
	target plVariable
	params []*plExpr
}

type stmtGetdiag struct {
	stmtCommon
	isStacked bool
	diagItems []*diagItem
}

type diagItem struct {
	kind   getdiagKind
	target int
}

type stmtOpen struct {
	stmtCommon
	curvar        int
	cursorOptions int
	argquery      *plExpr
	query         *plExpr
	dynquery      *plExpr
	params        []*plExpr
}

type stmtFetch struct {
	stmtCommon
	target              plVariable
	curvar              int
	direction           fetchDirection
	howMany             int64
	expr                *plExpr
	isMove              bool
	returnsMultipleRows bool
}

type stmtClose struct {
	stmtCommon
	curvar int
}

type stmtPerform struct {
	stmtCommon
	expr *plExpr
}

type stmtCall struct {
	stmtCommon
	expr   *plExpr
	isCall bool
	target plVariable
}

type stmtCommit struct {
	stmtCommon
	chain bool
}

type stmtRollback struct {
	stmtCommon
	chain bool
}

// plFunction is PLpgSQL_function (the serialized subset).
type plFunction struct {
	fnSignature string
	fnRetset    bool
	fnIsProc    bool // PROKIND_PROCEDURE
	fnRettype   uint32
	// fnInputCollation is fcinfo->fncollation, always InvalidOid in the
	// libpg_query driver.
	fnInputCollation uint32

	newVarno      int
	oldVarno      int
	foundVarno    int
	outParamVarno int

	datums      []datum
	action      *stmtBlock
	nstatements uint32

	printStrictParams bool
	resolveOption     resolveOption
}

// builtinType is one row of pg_query_pg_type.c's inline pg_type data.
type builtinType struct {
	oid          uint32
	name         string
	typtype      byte
	typcollation uint32
	typarray     uint32
}
