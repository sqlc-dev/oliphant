package plpgsql

import (
	"fmt"
	"strings"

	"github.com/sqlc-dev/oliphant/ast"
	"github.com/sqlc-dev/oliphant/internal/parse"
	"github.com/sqlc-dev/oliphant/internal/rawwalk"
)

// Error is the public error shape of a PL/pgSQL parse or compile failure.
type Error struct {
	Message   string
	Filename  string
	Funcname  string
	Cursorpos int
	Context   string
}

func (e *Error) Error() string { return e.Message }

// ParseToJSON is pg_query_parse_plpgsql: parse the whole input with the SQL
// parser, collect CREATE FUNCTION/PROCEDURE and DO statements, compile each
// with the PL/pgSQL parser, and emit the JSON array of function dumps.
func ParseToJSON(input string) (string, *Error) {
	tree, perr := parse.Parse(input)
	if perr != nil {
		return "", &Error{
			Message:   perr.Message,
			Filename:  perr.Filename,
			Funcname:  perr.Funcname,
			Cursorpos: perr.Cursorpos,
		}
	}

	// stmts_walker: collect CreateFunctionStmt and DoStmt nodes.
	var stmts []any
	var walk func(item any) bool
	walk = func(item any) bool {
		c := rawwalk.Concrete(item)
		if rawwalk.IsNil(c) {
			return false
		}
		switch v := c.(type) {
		case *ast.CreateFunctionStmt:
			stmts = append(stmts, v)
		case *ast.DoStmt:
			stmts = append(stmts, v)
		case *ast.RawStmt:
			return walk(v.Stmt)
		}
		return rawwalk.WalkChildren(item, walk)
	}
	if len(tree.Stmts) > 0 {
		walk(rawwalk.RawStmtList(tree.Stmts))
	}

	if len(stmts) == 0 {
		return "[]", nil
	}

	var out strings.Builder
	out.WriteString("[\n")
	for _, stmt := range stmts {
		fn, cerr := rawParsePlpgsql(stmt)
		if cerr != nil {
			return "", cerr
		}
		out.WriteString(toJSON(fn))
		out.WriteString(",\n")
	}
	// Replace the trailing ",\n" with "\n]".
	s := out.String()
	return s[:len(s)-2] + "\n]", nil
}

// rawParsePlpgsql is pg_query_raw_parse_plpgsql: compile one statement,
// catching ereports and decorating them with the compile error context
// (function_parse_error_transpose is mocked to false in this build, so the
// "near line N" context is always used).
func rawParsePlpgsql(stmt any) (fn *plFunction, outErr *Error) {
	c := &compiler{checkSyntax: true}

	defer func() {
		if r := recover(); r != nil {
			ce, ok := r.(*compileError)
			if !ok {
				panic(r)
			}
			e := &Error{
				Message:   ce.Message,
				Filename:  ce.Filename,
				Funcname:  ce.Funcname,
				Cursorpos: ce.Cursorpos,
			}
			// plpgsql_compile_error_callback.
			if c.errFuncname != "" {
				lineno := 0
				if c.scan != nil {
					lineno = c.scan.latestLineno()
				}
				e.Context = fmt.Sprintf("compilation of PL/pgSQL function \"%s\" near line %d",
					c.errFuncname, lineno)
			}
			fn, outErr = nil, e
		}
	}()

	switch v := stmt.(type) {
	case *ast.CreateFunctionStmt:
		return c.compileCreateFunctionStmt(v), nil
	case *ast.DoStmt:
		return c.compileDoStmt(v), nil
	}
	return nil, nil
}

// compileDoStmt is compile_do_stmt.
func (c *compiler) compileDoStmt(stmt *ast.DoStmt) *plFunction {
	procSource := ""
	haveSource := false
	language := "plpgsql"
	for _, arg := range stmt.Args {
		elem := arg.GetDefElem()
		if elem == nil {
			continue
		}
		switch elem.Defname {
		case "as":
			procSource = elem.Arg.GetString_().GetSval()
			haveSource = true
		case "language":
			language = elem.Arg.GetString_().GetSval()
		}
	}
	_ = haveSource

	if language != "plpgsql" {
		return &plFunction{}
	}
	return c.compileInline(procSource)
}

// compileInline is plpgsql_compile_inline (pl_comp.c).
func (c *compiler) compileInline(procSource string) *plFunction {
	funcName := "inline_code_block"

	c.scan = newScanner(procSource, c)
	c.errFuncname = funcName

	// Do extra syntax checking if check_function_bodies is on (it is).
	c.checkSyntax = true

	fn := &plFunction{
		fnSignature:   funcName,
		outParamVarno: -1,
		resolveOption: resolveError,
	}
	c.fn = fn

	c.nsInit()
	c.nsPush(funcName, labelBlock)
	c.startDatums()

	// Set up as though in a function returning VOID.
	fn.fnRetset = false

	// Create the magic FOUND variable.
	v := c.buildVariable("found", 0, buildDatatype(oidBool, -1, 0, nil), true)
	fn.foundVarno = v.datumNo()

	// Now parse the function's text.
	fn.action = c.parseFunction()

	// If it returns VOID (always true at the moment), we allow control to
	// fall off the end without an explicit RETURN statement.
	c.addDummyReturnInline()

	c.finishDatums()
	c.errFuncname = ""
	return fn
}

// addDummyReturnInline is pl_comp.c's add_dummy_return (checks label as well
// as exceptions when deciding to wrap the outer block).
func (c *compiler) addDummyReturnInline() {
	fn := c.fn
	if fn.action.exceptions != nil || fn.action.label != "" {
		wrap := &stmtBlock{body: []plStmt{fn.action}}
		fn.action = wrap
	}
	c.appendDummyReturn()
}

// addDummyReturnDriver is pg_query_parse_plpgsql.c's local add_dummy_return
// (only the EXCEPTION clause forces the wrapper block).
func (c *compiler) addDummyReturnDriver() {
	fn := c.fn
	if fn.action.exceptions != nil {
		wrap := &stmtBlock{body: []plStmt{fn.action}}
		fn.action = wrap
	}
	c.appendDummyReturn()
}

func (c *compiler) appendDummyReturn() {
	fn := c.fn
	body := fn.action.body
	needReturn := len(body) == 0
	if !needReturn {
		_, isReturn := body[len(body)-1].(*stmtReturn)
		needReturn = !isReturn
	}
	if needReturn {
		fn.action.body = append(fn.action.body, &stmtReturn{retvarno: fn.outParamVarno})
	}
}

// compileCreateFunctionStmt is pg_query_parse_plpgsql.c's
// compile_create_function_stmt.
func (c *compiler) compileCreateFunctionStmt(stmt *ast.CreateFunctionStmt) *plFunction {
	funcName := ""
	if len(stmt.Funcname) > 0 {
		funcName = stmt.Funcname[0].GetString_().GetSval()
	}

	procSource := ""
	language := "plpgsql"
	for _, opt := range stmt.Options {
		elem := opt.GetDefElem()
		if elem == nil {
			continue
		}
		switch elem.Defname {
		case "as":
			for _, item := range elem.Arg.GetList().GetItems() {
				procSource = item.GetString_().GetSval()
			}
		case "language":
			language = elem.Arg.GetString_().GetSval()
		}
	}

	if language != "plpgsql" {
		return &plFunction{}
	}

	isTrigger := false
	isSetof := false
	if stmt.ReturnType != nil {
		for _, n := range stmt.ReturnType.Names {
			if n.GetString_().GetSval() == "trigger" {
				isTrigger = true
			}
		}
		if stmt.ReturnType.Setof {
			isSetof = true
		}
	}

	c.scan = newScanner(procSource, c)
	c.errFuncname = funcName
	c.checkSyntax = true

	fn := &plFunction{
		fnSignature:   funcName,
		outParamVarno: -1,
		resolveOption: resolveError,
	}
	c.fn = fn

	c.nsInit()
	c.nsPush(funcName, labelBlock)
	c.startDatums()

	// Setup parameter names.
	for i, p := range stmt.Parameters {
		param := p.GetFunctionParameter()
		if param == nil {
			continue
		}
		buf := fmt.Sprintf("$%d", i+1)
		argdtype := buildDatatype(oidUnknown, -1, 0, param.ArgType)
		name := param.Name
		if name == "" {
			name = buf
		}
		argvariable := c.buildVariable(name, 0, argdtype, false)
		if param.Mode == ast.FunctionParameterMode_FUNC_PARAM_OUT ||
			param.Mode == ast.FunctionParameterMode_FUNC_PARAM_INOUT ||
			param.Mode == ast.FunctionParameterMode_FUNC_PARAM_TABLE {
			fn.outParamVarno = argvariable.datumNo()
		}
		argitemtype := nsTypeVar
		if argvariable.datumType() != dtypeVar {
			argitemtype = nsTypeRec
		}
		c.nsAdditem(argitemtype, argvariable.datumNo(), buf)
		if param.Name != "" {
			c.nsAdditem(argitemtype, argvariable.datumNo(), param.Name)
		}
	}

	// Set up as though in a function returning VOID.
	fn.fnRetset = isSetof

	// Create the magic FOUND variable.
	v := c.buildVariable("found", 0, buildDatatype(oidBool, -1, 0, nil), true)
	fn.foundVarno = v.datumNo()

	if isTrigger {
		// Add the records for referencing NEW and OLD.
		rec := c.buildRecord("new", 0, nil, oidRecord, true)
		fn.newVarno = rec.dno
		rec = c.buildRecord("old", 0, nil, oidRecord, true)
		fn.oldVarno = rec.dno
	}

	// Now parse the function's text.
	fn.action = c.parseFunction()

	// Allow control to fall off the end without an explicit RETURN.
	c.addDummyReturnDriver()

	c.finishDatums()
	c.errFuncname = ""
	return fn
}
