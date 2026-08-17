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
	fn.fnRettype = oidVoid
	fn.fnRetset = false

	// Create the magic FOUND variable.
	v := c.buildVariable("found", 0, c.plpgsqlBuildDatatype(oidBool, -1, 0, nil), true)
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

	// pg_query_create_function: trigger-ness is inferred from the RETURNS
	// clause by name.
	isDMLTrigger := false
	isEventTrigger := false
	if stmt.ReturnType != nil {
		for _, n := range stmt.ReturnType.Names {
			switch n.GetString_().GetSval() {
			case "trigger":
				isDMLTrigger = true
			case "event_trigger":
				isEventTrigger = true
			}
		}
	}

	// interpret_function_parameter_list and compute_return_type run in
	// pg_query_create_function, before plpgsql_compile_callback installs
	// the compile error context — their errors carry no context line.
	type argInfo struct {
		typeOid uint32
		mode    ast.FunctionParameterMode
		name    string
	}
	var args []argInfo
	var outCount int
	var outTypes []uint32
	for _, p := range stmt.Parameters {
		param := p.GetFunctionParameter()
		if param == nil {
			continue
		}
		toid := lookupTypeNameOid(param.ArgType)
		if toid == 0 {
			panic(compErr("interpret_function_parameter_list",
				"type %s does not exist", typeNameToString(param.ArgType)))
		}
		mode := param.Mode
		if mode == ast.FunctionParameterMode_FUNC_PARAM_DEFAULT {
			mode = ast.FunctionParameterMode_FUNC_PARAM_IN
		}
		if mode == ast.FunctionParameterMode_FUNC_PARAM_VARIADIC {
			// validate variadic parameter type
			const oidAny = 2276
			switch toid {
			case oidAnyArray, oidAnyCompatibleArray, oidAny:
			default:
				if getElementType(toid) == 0 {
					panic(&compileError{
						Message:  "VARIADIC parameter must be an array",
						Filename: "src_backend_commands_functioncmds.c",
						Funcname: "interpret_function_parameter_list",
					})
				}
			}
		}
		if mode == ast.FunctionParameterMode_FUNC_PARAM_OUT ||
			mode == ast.FunctionParameterMode_FUNC_PARAM_INOUT ||
			mode == ast.FunctionParameterMode_FUNC_PARAM_TABLE {
			outCount++
			outTypes = append(outTypes, toid)
		}
		args = append(args, argInfo{typeOid: toid, mode: mode, name: param.Name})
	}
	// requiredResultType: one OUT parameter is the result type itself; more
	// than one is RECORD.
	requiredResultType := uint32(0)
	if outCount == 1 {
		requiredResultType = outTypes[0]
	} else if outCount > 1 {
		requiredResultType = oidRecord
	}

	// compute_return_type / procedure result typing.
	var rettype uint32
	retset := false
	if stmt.IsProcedure {
		rettype = oidVoid
		if requiredResultType != 0 {
			rettype = requiredResultType
		}
	} else if stmt.ReturnType != nil {
		rettype = lookupTypeNameOid(stmt.ReturnType)
		if rettype == 0 {
			panic(compErr("compute_return_type",
				"type \"%s\" does not exist", typeNameToString(stmt.ReturnType)))
		}
		retset = stmt.ReturnType.Setof
		if requiredResultType != 0 && rettype != requiredResultType {
			panic(compErr("pg_query_create_function",
				"function result type must be %s because of OUT parameters",
				formatTypeBe(requiredResultType)))
		}
	}

	c.scan = newScanner(procSource, c)
	c.errFuncname = funcName
	c.checkSyntax = true

	fn := &plFunction{
		fnSignature:   funcName,
		fnIsProc:      stmt.IsProcedure,
		outParamVarno: -1,
		resolveOption: resolveError,
	}
	c.fn = fn

	c.nsInit()
	c.nsPush(funcName, labelBlock)
	c.startDatums()

	fn.fnRetset = retset
	fn.fnRettype = rettype

	switch {
	case isDMLTrigger:
		// do_compile PLPGSQL_DML_TRIGGER.
		fn.fnRettype = 0
		fn.fnRetset = false
		if len(args) != 0 {
			panic(compErr("plpgsql_compile_callback",
				"trigger functions cannot have declared arguments"))
		}
		rec := c.buildRecord("new", 0, nil, oidRecord, true)
		fn.newVarno = rec.dno
		rec = c.buildRecord("old", 0, nil, oidRecord, true)
		fn.oldVarno = rec.dno
		for _, tv := range [...]struct {
			name string
			oid  uint32
		}{
			{"tg_name", oidName},
			{"tg_when", oidText},
			{"tg_level", oidText},
			{"tg_op", oidText},
			{"tg_relid", oidOid},
			{"tg_relname", oidName},
			{"tg_table_name", oidName},
			{"tg_table_schema", oidName},
			{"tg_nargs", oidInt4},
			{"tg_argv", oidTextArray},
		} {
			v := c.buildVariable(tv.name, 0, c.plpgsqlBuildDatatype(tv.oid, -1, 0, nil), true)
			v.(*plVar).isPromise = true
		}

	case isEventTrigger:
		// do_compile PLPGSQL_EVENT_TRIGGER.
		fn.fnRettype = oidVoid
		fn.fnRetset = false
		if len(args) != 0 {
			panic(compErr("plpgsql_compile_callback",
				"event trigger functions cannot have declared arguments"))
		}
		for _, name := range [...]string{"tg_event", "tg_tag"} {
			v := c.buildVariable(name, 0, c.plpgsqlBuildDatatype(oidText, -1, 0, nil), true)
			v.(*plVar).isPromise = true
		}

	default:
		// do_compile PLPGSQL_NOT_TRIGGER: build the parameter variables.
		var outVars []plVariable
		numOutArgs := 0
		for i, arg := range args {
			buf := fmt.Sprintf("$%d", i+1)
			// cfunc_resolve_polymorphic_argtypes, validator branch.
			argtypeid := resolvePolymorphic(arg.typeOid)
			argdtype := c.plpgsqlBuildDatatype(argtypeid, -1, 0, nil)
			if argdtype.ttype == ttypePseudo {
				panic(compErr("plpgsql_compile_callback",
					"PL/pgSQL functions cannot accept type %s", formatTypeBe(argtypeid)))
			}
			name := arg.name
			if name == "" {
				name = buf
			}
			argvariable := c.buildVariable(name, 0, argdtype, false)
			argitemtype := nsTypeVar
			if argvariable.datumType() != dtypeVar {
				argitemtype = nsTypeRec
			}
			if arg.mode == ast.FunctionParameterMode_FUNC_PARAM_OUT ||
				arg.mode == ast.FunctionParameterMode_FUNC_PARAM_INOUT ||
				arg.mode == ast.FunctionParameterMode_FUNC_PARAM_TABLE {
				outVars = append(outVars, argvariable)
				numOutArgs++
			}
			// Add to namespace under the $n name, then the alias.
			c.nsAdditem(argitemtype, argvariable.datumNo(), buf)
			if arg.name != "" {
				c.nsAdditem(argitemtype, argvariable.datumNo(), arg.name)
			}
		}
		if numOutArgs > 1 || (numOutArgs == 1 && fn.fnIsProc) {
			row := c.buildRowFromVars(outVars)
			fn.outParamVarno = row.dno
		} else if numOutArgs == 1 {
			fn.outParamVarno = outVars[0].datumNo()
		}

		// Polymorphic return types resolve as in validator mode.
		rettypeid := fn.fnRettype
		if isPolymorphicType(rettypeid) {
			switch rettypeid {
			case oidAnyArray, oidAnyCompatibleArray:
				rettypeid = oidInt4Array
			case oidAnyRange, oidAnyCompatibleRange:
				rettypeid = oidInt4Range
			case oidAnyMultirange:
				rettypeid = oidInt4Multirange
			default:
				rettypeid = oidInt4
			}
		}
		if bt := builtinTypeByOid(rettypeid); bt != nil && bt.typtype == 'p' {
			// Disallow pseudotype result, except VOID or RECORD.
			switch rettypeid {
			case oidVoid, oidRecord:
			case oidTrigger, oidEventTrig:
				panic(compErr("plpgsql_compile_callback",
					"trigger functions can only be called as triggers"))
			default:
				panic(compErr("plpgsql_compile_callback",
					"PL/pgSQL functions cannot return type %s", formatTypeBe(rettypeid)))
			}
		}
		// install $0 reference for polymorphic return types without OUT args.
		if isPolymorphicType(fn.fnRettype) && numOutArgs == 0 {
			c.buildVariable("$0", 0, c.plpgsqlBuildDatatype(rettypeid, -1, 0, nil), true)
		}
		fn.fnRettype = rettypeid
		_ = retset
	}

	// Create the magic FOUND variable.
	v := c.buildVariable("found", 0, c.plpgsqlBuildDatatype(oidBool, -1, 0, nil), true)
	fn.foundVarno = v.datumNo()

	// Now parse the function's text.
	fn.action = c.parseFunction()

	// Allow control to fall off the end without an explicit RETURN when the
	// function has OUT parameters, returns VOID, or returns a set — via the
	// real add_dummy_return (fn.outParamVarno covers the OUT case).
	if fn.outParamVarno >= 0 || fn.fnRettype == oidVoid || fn.fnRetset {
		c.addDummyReturnInline()
	}

	c.finishDatums()
	c.errFuncname = ""
	return fn
}
