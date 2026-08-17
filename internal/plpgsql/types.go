package plpgsql

import (
	"fmt"
	"strings"

	"github.com/sqlc-dev/oliphant/ast"
	"github.com/sqlc-dev/oliphant/internal/deparse"
	"github.com/sqlc-dev/oliphant/internal/parse"
)

// This file ports the type-resolution pipeline of the 18.0.0 libpg_query
// build: the real parse_datatype / typeStringToTypeName / LookupTypeName /
// build_datatype flow, backed by the mocked mini-catalog
// (scripts/mocks/*.c + src/include/pg_query_pg_type.c). The catalog serves
// only pg_catalog's built-in types; any unqualified or public-schema name
// it does not know resolves to RECORD ("we assume that any unknown type in
// the public namespace is a row type").

// pg_type.h OIDs referenced directly by the compile flow.
const (
	oidName      = 19
	oidTextArray = 1009
	oidOid       = 26
	oidTrigger   = 2279
	oidEventTrig = 3838
	oidVoidType  = oidVoid

	oidInt4Array      = 1007
	oidInt4Range      = 3904
	oidInt4Multirange = 4451

	// Polymorphic pseudo-types (IsPolymorphicType).
	oidAnyElement          = 2283
	oidAnyArray            = 2277
	oidAnyNonArray         = 2776
	oidAnyEnum             = 3500
	oidAnyRange            = 3831
	oidAnyMultirange       = 4537
	oidAnyCompatible       = 5077
	oidAnyCompatibleArray  = 5078
	oidAnyCompatibleNonArr = 5079
	oidAnyCompatibleRange  = 5080
	oidAnyCompatibleMulti  = 4538
)

// isPolymorphicType is IsPolymorphicType (pseudo_types.h).
func isPolymorphicType(oid uint32) bool {
	switch oid {
	case oidAnyElement, oidAnyArray, oidAnyNonArray, oidAnyEnum,
		oidAnyRange, oidAnyMultirange, oidAnyCompatible, oidAnyCompatibleArray,
		oidAnyCompatibleNonArr, oidAnyCompatibleRange, oidAnyCompatibleMulti:
		return true
	}
	return false
}

// builtinTypeByOid is pg_query_builtin_type_by_oid.
func builtinTypeByOid(oid uint32) *builtinType {
	for i := range builtinTypes {
		if builtinTypes[i].oid == oid {
			return &builtinTypes[i]
		}
	}
	return nil
}

// builtinTypeOidByName is pg_query_builtin_type_oid_by_name.
func builtinTypeOidByName(name string) uint32 {
	for i := range builtinTypes {
		if builtinTypes[i].name == name {
			return builtinTypes[i].oid
		}
	}
	return 0
}

// typeNameToString is TypeNameToString/appendTypeNameToBuffer: the
// possibly-qualified name as-is, %TYPE and [] decoration as needed.
func typeNameToString(t *ast.TypeName) string {
	var b strings.Builder
	if len(t.Names) > 0 {
		for i, n := range t.Names {
			if i > 0 {
				b.WriteByte('.')
			}
			b.WriteString(n.GetString_().GetSval())
		}
	} else {
		// format_type_be is mocked to "-" in the vendored build.
		b.WriteString("-")
	}
	if t.PctType {
		b.WriteString("%TYPE")
	}
	if len(t.ArrayBounds) > 0 {
		b.WriteString("[]")
	}
	return b.String()
}

// compileErrorf builds a compileError attributed to the vendored pl_comp
// translation unit (the whole mocked lookup pipeline lands there).
func compErr(funcname, format string, args ...any) *compileError {
	return &compileError{
		Message:  fmt.Sprintf(format, args...),
		Filename: plCompFilename,
		Funcname: funcname,
	}
}

// lookupTypeNameOid is the mocked LookupTypeNameExtended: resolve a TypeName
// to a builtin type OID (or RECORD for unknown public-namespace names).
// Returns 0 when the type does not exist.
func lookupTypeNameOid(t *ast.TypeName) uint32 {
	var typoid uint32
	if len(t.Names) == 0 {
		// We have the OID already if it's an internally generated TypeName.
		typoid = uint32(t.TypeOid)
	} else if t.PctType {
		// CHANGED upstream: %TYPE through the SQL-level lookup is not
		// implemented in the mocked build.
		panic(compErr("LookupTypeNameExtended", "Not implemented"))
	} else {
		// DeconstructQualifiedName (mock: catalog name ignored unchecked).
		var schemaname, typname string
		switch len(t.Names) {
		case 1:
			typname = t.Names[0].GetString_().GetSval()
		case 2:
			schemaname = t.Names[0].GetString_().GetSval()
			typname = t.Names[1].GetString_().GetSval()
		case 3:
			schemaname = t.Names[1].GetString_().GetSval()
			typname = t.Names[2].GetString_().GetSval()
		default:
			panic(compErr("DeconstructQualifiedName",
				"improper qualified name (too many dotted names): %s",
				nodeNameListToString(t.Names)))
		}

		if schemaname != "" {
			// LookupExplicitNamespace (mock: pg_catalog and public only).
			switch schemaname {
			case "pg_catalog":
				typoid = builtinTypeOidByName(typname)
			case "public":
				// GetSysCacheOid mock: unknown public types are row types.
				typoid = oidRecord
			default:
				panic(compErr("LookupExplicitNamespace",
					"Not implemented (LookupExplicitNamespace only supports pg_catalog and public)"))
			}
		} else {
			// TypenameGetTypidExtended over the mocked search path
			// [pg_catalog, public].
			typoid = builtinTypeOidByName(typname)
			if typoid == 0 {
				typoid = oidRecord
			}
		}
	}

	// If an array reference, return the array type instead.
	if len(t.ArrayBounds) > 0 && typoid != 0 {
		typoid = getArrayType(typoid)
	}
	return typoid
}

// syscacheFilename is __FILE__ for the SearchSysCache1 mock's errors.
const syscacheFilename = "src_backend_utils_cache_syscache.c"

// getArrayType is get_array_type: a syscache lookup first, so an OID the
// mocked catalog does not serve (including InvalidOid, e.g. a %TYPE stub
// with array decoration) raises the SearchSysCache1 mock's error.
func getArrayType(oid uint32) uint32 {
	bt := builtinTypeByOid(oid)
	if bt == nil {
		panic(&compileError{
			Message:  fmt.Sprintf("Not implemented (SearchSysCache1 got TYPEOID cache request for type OID %d)", oid),
			Filename: syscacheFilename,
			Funcname: "SearchSysCache1",
		})
	}
	return bt.typarray
}

// nodeNameListToString is NameListToString (dotted, unquoted).
func nodeNameListToString(names []*ast.Node) string {
	parts := make([]string, len(names))
	for i, n := range names {
		parts[i] = n.GetString_().GetSval()
	}
	return strings.Join(parts, ".")
}

// typeStringToTypeName parses a type declaration string with the core
// parser in RAW_PARSE_TYPE_NAME mode. On failure the error is
// "invalid type name %q" (empty input, SETOF, or a syntax error — the C
// syntax error surfaces with the pts_error_callback context, but the
// message the tests see is the scanner's, decorated by the caller).
func (c *compiler) typeStringToTypeName(s string, location int) *ast.TypeName {
	if strings.TrimLeft(s, " \t\n\r\f\v") == "" {
		panic(compErr("typeStringToTypeName", "invalid type name \"%s\"", s))
	}
	tn, perr := parse.ParseTypeName(s)
	if perr != nil {
		// The core parser's error; plpgsql_sql_error_callback flushes the
		// external cursor, keeping message/filename/funcname (as
		// check_sql_expr does).
		panic(&compileError{
			Message:  perr.Message,
			Filename: perr.Filename,
			Funcname: perr.Funcname,
		})
	}
	if tn.Setof {
		panic(compErr("typeStringToTypeName", "invalid type name \"%s\"", s))
	}
	return tn
}

// parseDatatype18 is the real parse_datatype: core-parse the string, resolve
// it against the mocked catalog, and build the PLpgSQL_type.
func (c *compiler) parseDatatypeAt(s string, location int) *plType {
	typeName := c.typeStringToTypeName(s, location)

	// typenameTypeIdAndMod → typenameType → LookupTypeNameExtended;
	// typenameTypeMod is mocked to -1.
	typoid := lookupTypeNameOid(typeName)
	if typoid == 0 {
		panic(compErr("typenameType", "type \"%s\" does not exist",
			typeNameToString(typeName)))
	}

	return c.plpgsqlBuildDatatype(typoid, -1, c.fn.fnInputCollation, typeName)
}

// plpgsqlBuildDatatype is plpgsql_build_datatype + the build_datatype mock:
// the canonical catalog name, ttype per typtype, collation per the type.
func (c *compiler) plpgsqlBuildDatatype(typeOid uint32, typmod int32, collation uint32, origtypname *ast.TypeName) *plType {
	bt := builtinTypeByOid(typeOid)
	if bt == nil {
		panic(&compileError{
			Message:  fmt.Sprintf("Not implemented (SearchSysCache1 got TYPEOID cache request for type OID %d)", typeOid),
			Filename: syscacheFilename,
			Funcname: "SearchSysCache1",
		})
	}
	typ := &plType{
		typname:    bt.name,
		typoid:     typeOid,
		atttypmod:  typmod,
		collation:  bt.typcollation,
		typisarray: isTrueArrayType(typeOid),
	}
	if collation != 0 && typ.collation != 0 {
		typ.collation = collation
	}
	switch bt.typtype {
	case 'b', 'e', 'r', 'm':
		typ.ttype = ttypeScalar
	case 'c':
		typ.ttype = ttypeRec
	case 'd':
		typ.ttype = ttypeScalar
	case 'p':
		if typeOid == oidRecord {
			typ.ttype = ttypeRec
		} else {
			typ.ttype = ttypePseudo
		}
	}
	return typ
}

// resolvePolymorphic is cfunc_resolve_polymorphic_argtypes' validator branch
// (the only branch the mocked build reaches).
func resolvePolymorphic(oid uint32) uint32 {
	switch oid {
	case oidAnyElement, oidAnyNonArray, oidAnyEnum, oidAnyCompatible, oidAnyCompatibleNonArr:
		return oidInt4
	case oidAnyArray, oidAnyCompatibleArray:
		return oidInt4Array
	case oidAnyRange, oidAnyCompatibleRange:
		return oidInt4Range
	case oidAnyMultirange:
		return oidInt4Multirange
	}
	return oid
}

// formatTypeBe is format_type_be over the builtin table: the SQL-standard
// spellings for the types format_type_internal special-cases, the (possibly
// quoted) catalog name otherwise, with array types rendered as element[].
func formatTypeBe(oid uint32) string {
	// Array of a known element type renders as element[] — but only for
	// ordinary (extended-storage) arrays; pseudo-type arrays such as
	// _record print their own catalog name.
	if bt := builtinTypeByOid(oid); bt != nil && bt.typtype == 'b' {
		for i := range builtinTypes {
			if builtinTypes[i].typarray == oid && oid != 0 {
				return formatTypeBe(builtinTypes[i].oid) + "[]"
			}
		}
	}
	switch oid {
	case 1560: // BITOID
		return "bit"
	case oidBool:
		return "boolean"
	case 1042: // BPCHAROID
		return "character"
	case 700: // FLOAT4OID
		return "real"
	case 701: // FLOAT8OID
		return "double precision"
	case 21: // INT2OID
		return "smallint"
	case oidInt4:
		return "integer"
	case 20: // INT8OID
		return "bigint"
	case 1700: // NUMERICOID
		return "numeric"
	case 1186: // INTERVALOID
		return "interval"
	case 1083: // TIMEOID
		return "time without time zone"
	case 1266: // TIMETZOID
		return "time with time zone"
	case 1114: // TIMESTAMPOID
		return "timestamp without time zone"
	case 1184: // TIMESTAMPTZOID
		return "timestamp with time zone"
	case 1562: // VARBITOID
		return "bit varying"
	case 1043: // VARCHAROID
		return "character varying"
	}
	if bt := builtinTypeByOid(oid); bt != nil {
		return deparse.QuoteIdentifier(bt.name)
	}
	return "-"
}

// buildRowFromVars is build_row_from_vars: the row datum holding a
// function's OUT parameters.
func (c *compiler) buildRowFromVars(vars []plVariable) *plRow {
	row := &plRow{
		refname: "(unnamed row)",
		lineno:  -1,
	}
	for _, v := range vars {
		row.fieldnames = append(row.fieldnames, refnameOf(v))
		row.varnos = append(row.varnos, v.datumNo())
	}
	c.addDatum(row)
	return row
}

// refnameOf returns a variable's refname.
func refnameOf(v plVariable) string {
	switch d := v.(type) {
	case *plVar:
		return d.refname
	case *plRow:
		return d.refname
	case *plRec:
		return d.refname
	}
	return ""
}

// isTrueArrayType reports whether oid is some builtin type's true array
// type (IsTrueArrayType over the mocked catalog).
func isTrueArrayType(oid uint32) bool {
	if oid == 0 {
		return false
	}
	for i := range builtinTypes {
		if builtinTypes[i].typarray == oid {
			return true
		}
	}
	return false
}

// getElementType is get_element_type over the mocked catalog. The
// PgQueryBuiltinType rows carry no typelem, so the forged pg_type tuples
// always read typelem 0 — every lookup returns InvalidOid, arrays included
// (which makes VARIADIC of any concrete type an error upstream).
func getElementType(oid uint32) uint32 {
	return 0
}
