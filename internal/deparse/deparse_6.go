// Ported from postgres_deparse.c (libpg_query 17-6.2.2).
// DO, functions/procedures, roles, GRANT, transactions, views, policies.
package deparse

import (
	"strings"

	"github.com/sqlc-dev/oliphant/ast"
)

func deparseDoStmt(st *state, do_stmt *ast.DoStmt) {
	st.appendString("DO ")

	for _, item := range do_stmt.Args {
		defel := item.GetDefElem()
		if defel.Defname == "language" {
			st.appendString("LANGUAGE ")
			st.appendString(quoteIdentifier(strVal(defel.Arg)))
			st.appendChar(' ')
		} else if defel.Defname == "as" {
			strval := strVal(defel.Arg)
			delim := "$$"
			if strings.Contains(strval, "$$") {
				delim = "$outer$"
			}
			st.appendString(delim)
			st.appendString(strval)
			st.appendString(delim)
			st.appendChar(' ')
		}
	}

	st.removeTrailingSpace()
}

func deparseDiscardStmt(st *state, discard_stmt *ast.DiscardStmt) {
	st.appendString("DISCARD ")
	switch discard_stmt.Target {
	case ast.DiscardMode_DISCARD_ALL:
		st.appendString("ALL")
	case ast.DiscardMode_DISCARD_PLANS:
		st.appendString("PLANS")
	case ast.DiscardMode_DISCARD_SEQUENCES:
		st.appendString("SEQUENCES")
	case ast.DiscardMode_DISCARD_TEMP:
		st.appendString("TEMP")
	}
}

func deparseDefineStmt(st *state, define_stmt *ast.DefineStmt) {
	st.appendString("CREATE ")

	if define_stmt.Replace {
		st.appendString("OR REPLACE ")
	}

	switch define_stmt.Kind {
	case ast.ObjectType_OBJECT_AGGREGATE:
		st.appendString("AGGREGATE ")
	case ast.ObjectType_OBJECT_OPERATOR:
		st.appendString("OPERATOR ")
	case ast.ObjectType_OBJECT_TYPE:
		st.appendString("TYPE ")
	case ast.ObjectType_OBJECT_TSPARSER:
		st.appendString("TEXT SEARCH PARSER ")
	case ast.ObjectType_OBJECT_TSDICTIONARY:
		st.appendString("TEXT SEARCH DICTIONARY ")
	case ast.ObjectType_OBJECT_TSTEMPLATE:
		st.appendString("TEXT SEARCH TEMPLATE ")
	case ast.ObjectType_OBJECT_TSCONFIGURATION:
		st.appendString("TEXT SEARCH CONFIGURATION ")
	case ast.ObjectType_OBJECT_COLLATION:
		st.appendString("COLLATION ")
	default:
		// This shouldn't happen
	}

	if define_stmt.IfNotExists {
		st.appendString("IF NOT EXISTS ")
	}

	switch define_stmt.Kind {
	case ast.ObjectType_OBJECT_AGGREGATE:
		deparseFuncName(st, define_stmt.Defnames)
	case ast.ObjectType_OBJECT_OPERATOR:
		deparseAnyOperator(st, define_stmt.Defnames)
	case ast.ObjectType_OBJECT_TYPE,
		ast.ObjectType_OBJECT_TSPARSER,
		ast.ObjectType_OBJECT_TSDICTIONARY,
		ast.ObjectType_OBJECT_TSTEMPLATE,
		ast.ObjectType_OBJECT_TSCONFIGURATION,
		ast.ObjectType_OBJECT_COLLATION:
		deparseAnyName(st, define_stmt.Defnames)
	}
	st.appendChar(' ')

	if !define_stmt.Oldstyle && define_stmt.Kind == ast.ObjectType_OBJECT_AGGREGATE {
		deparseAggrArgs(st, define_stmt.Args)
		st.appendChar(' ')
	}

	if define_stmt.Kind == ast.ObjectType_OBJECT_COLLATION &&
		len(define_stmt.Definition) == 1 &&
		define_stmt.Definition[0].GetDefElem().Defname == "from" {
		st.appendString("FROM ")
		deparseAnyName(st, define_stmt.Definition[0].GetDefElem().Arg.GetList().Items)
	} else if len(define_stmt.Definition) > 0 {
		deparseDefinition(st, define_stmt.Definition)
	}

	st.removeTrailingSpace()
}

func deparseCompositeTypeStmt(st *state, composite_type_stmt *ast.CompositeTypeStmt) {
	var parentLevel *nestingLevel

	st.appendString("CREATE TYPE ")
	deparseRangeVar(st, composite_type_stmt.Typevar, contextCreateType)

	st.appendString(" AS (")
	parentLevel = st.increaseNestingLevel()
	for i, item := range composite_type_stmt.Coldeflist {
		deparseColumnDef(st, item.GetColumnDef())
		if i < len(composite_type_stmt.Coldeflist)-1 {
			st.appendCommaAndPart()
		}
	}
	st.decreaseNestingLevel(parentLevel)
	st.appendChar(')')
}

func deparseCreateEnumStmt(st *state, create_enum_stmt *ast.CreateEnumStmt) {
	var parentLevel *nestingLevel

	st.appendString("CREATE TYPE ")

	deparseAnyName(st, create_enum_stmt.TypeName)
	st.appendString(" AS ENUM (")
	parentLevel = st.increaseNestingLevel()
	for i, item := range create_enum_stmt.Vals {
		deparseStringLiteral(st, strVal(item))
		if i < len(create_enum_stmt.Vals)-1 {
			st.appendCommaAndPart()
		}
	}
	st.decreaseNestingLevel(parentLevel)
	st.appendChar(')')
}

func deparseCreateRangeStmt(st *state, create_range_stmt *ast.CreateRangeStmt) {
	st.appendString("CREATE TYPE ")
	deparseAnyName(st, create_range_stmt.TypeName)
	st.appendString(" AS RANGE ")
	deparseDefinition(st, create_range_stmt.Params)
}

func deparseAlterEnumStmt(st *state, alter_enum_stmt *ast.AlterEnumStmt) {
	st.appendString("ALTER TYPE ")
	deparseAnyName(st, alter_enum_stmt.TypeName)
	st.appendChar(' ')

	if alter_enum_stmt.OldVal == "" {
		st.appendString("ADD VALUE ")
		if alter_enum_stmt.SkipIfNewValExists {
			st.appendString("IF NOT EXISTS ")
		}

		deparseStringLiteral(st, alter_enum_stmt.NewVal)
		st.appendChar(' ')

		if alter_enum_stmt.NewValNeighbor != "" {
			if alter_enum_stmt.NewValIsAfter {
				st.appendString("AFTER ")
			} else {
				st.appendString("BEFORE ")
			}
			deparseStringLiteral(st, alter_enum_stmt.NewValNeighbor)
		}
	} else {
		st.appendString("RENAME VALUE ")
		deparseStringLiteral(st, alter_enum_stmt.OldVal)
		st.appendString(" TO ")
		deparseStringLiteral(st, alter_enum_stmt.NewVal)
	}

	st.removeTrailingSpace()
}

func deparseAlterExtensionStmt(st *state, alter_extension_stmt *ast.AlterExtensionStmt) {
	st.appendString("ALTER EXTENSION ")
	deparseColId(st, alter_extension_stmt.Extname)
	st.appendString(" UPDATE ")
	for _, item := range alter_extension_stmt.Options {
		def_elem := item.GetDefElem()
		if def_elem.Defname == "new_version" {
			st.appendString("TO ")
			deparseNonReservedWordOrSconst(st, strVal(def_elem.Arg))
		}
		st.appendChar(' ')
	}
	st.removeTrailingSpace()
}

func deparseAlterExtensionContentsStmt(st *state, alter_extension_contents_stmt *ast.AlterExtensionContentsStmt) {
	var l []*ast.Node

	st.appendString("ALTER EXTENSION ")
	deparseColId(st, alter_extension_contents_stmt.Extname)
	st.appendChar(' ')

	if alter_extension_contents_stmt.Action == 1 {
		st.appendString("ADD ")
	} else if alter_extension_contents_stmt.Action == -1 {
		st.appendString("DROP ")
	}

	switch alter_extension_contents_stmt.Objtype {
	case ast.ObjectType_OBJECT_ACCESS_METHOD:
		st.appendString("ACCESS METHOD ")
	case ast.ObjectType_OBJECT_AGGREGATE:
		st.appendString("AGGREGATE ")
	case ast.ObjectType_OBJECT_CAST:
		st.appendString("CAST ")
	case ast.ObjectType_OBJECT_COLLATION:
		st.appendString("COLLATION ")
	case ast.ObjectType_OBJECT_CONVERSION:
		st.appendString("CONVERSION ")
	case ast.ObjectType_OBJECT_DOMAIN:
		st.appendString("DOMAIN ")
	case ast.ObjectType_OBJECT_FUNCTION:
		st.appendString("FUNCTION ")
	case ast.ObjectType_OBJECT_LANGUAGE:
		st.appendString("LANGUAGE ")
	case ast.ObjectType_OBJECT_OPERATOR:
		st.appendString("OPERATOR ")
	case ast.ObjectType_OBJECT_OPCLASS:
		st.appendString("OPERATOR CLASS ")
	case ast.ObjectType_OBJECT_OPFAMILY:
		st.appendString("OPERATOR FAMILY ")
	case ast.ObjectType_OBJECT_PROCEDURE:
		st.appendString("PROCEDURE ")
	case ast.ObjectType_OBJECT_ROUTINE:
		st.appendString("ROUTINE ")
	case ast.ObjectType_OBJECT_SCHEMA:
		st.appendString("SCHEMA ")
	case ast.ObjectType_OBJECT_EVENT_TRIGGER:
		st.appendString("EVENT TRIGGER ")
	case ast.ObjectType_OBJECT_TABLE:
		st.appendString("TABLE ")
	case ast.ObjectType_OBJECT_TSPARSER:
		st.appendString("TEXT SEARCH PARSER ")
	case ast.ObjectType_OBJECT_TSDICTIONARY:
		st.appendString("TEXT SEARCH DICTIONARY ")
	case ast.ObjectType_OBJECT_TSTEMPLATE:
		st.appendString("TEXT SEARCH TEMPLATE ")
	case ast.ObjectType_OBJECT_TSCONFIGURATION:
		st.appendString("TEXT SEARCH CONFIGURATION ")
	case ast.ObjectType_OBJECT_SEQUENCE:
		st.appendString("SEQUENCE ")
	case ast.ObjectType_OBJECT_VIEW:
		st.appendString("VIEW ")
	case ast.ObjectType_OBJECT_MATVIEW:
		st.appendString("MATERIALIZED VIEW ")
	case ast.ObjectType_OBJECT_FOREIGN_TABLE:
		st.appendString("FOREIGN TABLE ")
	case ast.ObjectType_OBJECT_FDW:
		st.appendString("FOREIGN DATA WRAPPER ")
	case ast.ObjectType_OBJECT_FOREIGN_SERVER:
		st.appendString("SERVER ")
	case ast.ObjectType_OBJECT_TRANSFORM:
		st.appendString("TRANSFORM ")
	case ast.ObjectType_OBJECT_TYPE:
		st.appendString("TYPE ")
	default:
		// No other object types are supported here in the parser
	}

	switch alter_extension_contents_stmt.Objtype {
	// any_name
	case ast.ObjectType_OBJECT_COLLATION,
		ast.ObjectType_OBJECT_CONVERSION,
		ast.ObjectType_OBJECT_TABLE,
		ast.ObjectType_OBJECT_TSPARSER,
		ast.ObjectType_OBJECT_TSDICTIONARY,
		ast.ObjectType_OBJECT_TSTEMPLATE,
		ast.ObjectType_OBJECT_TSCONFIGURATION,
		ast.ObjectType_OBJECT_SEQUENCE,
		ast.ObjectType_OBJECT_VIEW,
		ast.ObjectType_OBJECT_MATVIEW,
		ast.ObjectType_OBJECT_FOREIGN_TABLE:
		deparseAnyName(st, alter_extension_contents_stmt.Object.GetList().Items)
	// name
	case ast.ObjectType_OBJECT_ACCESS_METHOD,
		ast.ObjectType_OBJECT_LANGUAGE,
		ast.ObjectType_OBJECT_SCHEMA,
		ast.ObjectType_OBJECT_EVENT_TRIGGER,
		ast.ObjectType_OBJECT_FDW,
		ast.ObjectType_OBJECT_FOREIGN_SERVER:
		deparseColId(st, strVal(alter_extension_contents_stmt.Object))
	case ast.ObjectType_OBJECT_AGGREGATE:
		deparseAggregateWithArgtypes(st, alter_extension_contents_stmt.Object.GetObjectWithArgs())
	case ast.ObjectType_OBJECT_CAST:
		l = alter_extension_contents_stmt.Object.GetList().Items
		st.appendChar('(')
		deparseTypeName(st, l[0].GetTypeName())
		st.appendString(" AS ")
		deparseTypeName(st, l[1].GetTypeName())
		st.appendChar(')')
	case ast.ObjectType_OBJECT_DOMAIN, ast.ObjectType_OBJECT_TYPE:
		deparseTypeName(st, alter_extension_contents_stmt.Object.GetTypeName())
	case ast.ObjectType_OBJECT_FUNCTION,
		ast.ObjectType_OBJECT_PROCEDURE,
		ast.ObjectType_OBJECT_ROUTINE:
		deparseFunctionWithArgtypes(st, alter_extension_contents_stmt.Object.GetObjectWithArgs())
	case ast.ObjectType_OBJECT_OPERATOR:
		deparseOperatorWithArgtypes(st, alter_extension_contents_stmt.Object.GetObjectWithArgs())
	case ast.ObjectType_OBJECT_OPFAMILY, ast.ObjectType_OBJECT_OPCLASS:
		l = alter_extension_contents_stmt.Object.GetList().Items
		deparseAnyNameSkipFirst(st, l)
		st.appendString(" USING ")
		deparseColId(st, strVal(l[0]))
	case ast.ObjectType_OBJECT_TRANSFORM:
		l = alter_extension_contents_stmt.Object.GetList().Items
		st.appendString("FOR ")
		deparseTypeName(st, l[0].GetTypeName())
		st.appendString(" LANGUAGE ")
		deparseColId(st, strVal(l[1]))
	}
}

func deparseAccessPriv(st *state, access_priv *ast.AccessPriv) {
	if access_priv.PrivName != "" {
		if access_priv.PrivName == "select" {
			st.appendString("select")
		} else if access_priv.PrivName == "references" {
			st.appendString("references")
		} else if access_priv.PrivName == "create" {
			st.appendString("create")
		} else {
			st.appendString(quoteIdentifier(access_priv.PrivName))
		}
	} else {
		st.appendString("ALL")
	}
	st.appendChar(' ')

	if len(access_priv.Cols) > 0 {
		st.appendChar('(')
		deparseColumnList(st, access_priv.Cols)
		st.appendChar(')')
	}

	st.removeTrailingSpace()
}

func deparseGrantStmt(st *state, grant_stmt *ast.GrantStmt) {
	if grant_stmt.IsGrant {
		st.appendString("GRANT ")
	} else {
		st.appendString("REVOKE ")
	}

	if !grant_stmt.IsGrant && grant_stmt.GrantOption {
		st.appendString("GRANT OPTION FOR ")
	}

	if len(grant_stmt.Privileges) > 0 {
		for i, item := range grant_stmt.Privileges {
			deparseAccessPriv(st, item.GetAccessPriv())
			if i < len(grant_stmt.Privileges)-1 {
				st.appendString(", ")
			}
		}
		st.appendChar(' ')
	} else {
		st.appendString("ALL ")
	}

	st.appendString("ON ")

	deparsePrivilegeTarget(st, grant_stmt.Targtype, grant_stmt.Objtype, grant_stmt.Objects)
	st.appendChar(' ')

	if grant_stmt.IsGrant {
		st.appendString("TO ")
	} else {
		st.appendString("FROM ")
	}

	for i, item := range grant_stmt.Grantees {
		deparseRoleSpec(st, item.GetRoleSpec())
		if i < len(grant_stmt.Grantees)-1 {
			st.appendChar(',')
		}
		st.appendChar(' ')
	}

	if grant_stmt.IsGrant && grant_stmt.GrantOption {
		st.appendString("WITH GRANT OPTION ")
	}

	deparseOptDropBehavior(st, grant_stmt.Behavior)

	if grant_stmt.Grantor != nil {
		st.appendString("GRANTED BY ")
		deparseRoleSpec(st, grant_stmt.Grantor)
	}

	st.removeTrailingSpace()
}

func deparseGrantRoleStmt(st *state, grant_role_stmt *ast.GrantRoleStmt) {
	if grant_role_stmt.IsGrant {
		st.appendString("GRANT ")
	} else {
		st.appendString("REVOKE ")
	}

	if !grant_role_stmt.IsGrant && len(grant_role_stmt.Opt) > 0 {
		defelem := grant_role_stmt.Opt[0].GetDefElem()

		if "admin" == defelem.Defname {
			st.appendString("ADMIN ")
		} else if "inherit" == defelem.Defname {
			st.appendString("INHERIT ")
		} else if "set" == defelem.Defname {
			st.appendString("SET ")
		}

		st.appendString("OPTION FOR ")
	}

	for i, item := range grant_role_stmt.GrantedRoles {
		deparseAccessPriv(st, item.GetAccessPriv())
		if i < len(grant_role_stmt.GrantedRoles)-1 {
			st.appendChar(',')
		}
		st.appendChar(' ')
	}

	if grant_role_stmt.IsGrant {
		st.appendString("TO ")
	} else {
		st.appendString("FROM ")
	}

	deparseRoleList(st, grant_role_stmt.GranteeRoles)
	st.appendChar(' ')

	if grant_role_stmt.IsGrant {
		if len(grant_role_stmt.Opt) > 0 {
			st.appendString("WITH ")
		}

		for i, item := range grant_role_stmt.Opt {
			defelem := item.GetDefElem()
			if "admin" == defelem.Defname {
				st.appendString("ADMIN ")
				if defelem.Arg.GetBoolean().Boolval {
					st.appendString("OPTION")
				} else {
					st.appendString("FALSE")
				}
			} else if "inherit" == defelem.Defname {
				st.appendString("INHERIT ")
				if defelem.Arg.GetBoolean().Boolval {
					st.appendString("OPTION")
				} else {
					st.appendString("FALSE")
				}
			} else if "set" == defelem.Defname {
				st.appendString("SET ")
				if defelem.Arg.GetBoolean().Boolval {
					st.appendString("OPTION")
				} else {
					st.appendString("FALSE")
				}
			}

			if i < len(grant_role_stmt.Opt)-1 {
				st.appendChar(',')
			}

			st.appendChar(' ')
		}
	}

	if grant_role_stmt.Grantor != nil {
		st.appendString("GRANTED BY ")
		deparseRoleSpec(st, grant_role_stmt.Grantor)
	}

	if grant_role_stmt.Behavior == ast.DropBehavior_DROP_CASCADE {
		st.appendString("CASCADE ")
	}

	st.removeTrailingSpace()
}

func deparseDropRoleStmt(st *state, drop_role_stmt *ast.DropRoleStmt) {
	st.appendString("DROP ROLE ")

	if drop_role_stmt.MissingOk {
		st.appendString("IF EXISTS ")
	}

	deparseRoleList(st, drop_role_stmt.Roles)
}

func deparseIndexStmt(st *state, index_stmt *ast.IndexStmt) {
	st.appendString("CREATE ")

	if index_stmt.Unique {
		st.appendString("UNIQUE ")
	}

	st.appendString("INDEX ")

	if index_stmt.Concurrent {
		st.appendString("CONCURRENTLY ")
	}

	if index_stmt.IfNotExists {
		st.appendString("IF NOT EXISTS ")
	}

	if index_stmt.Idxname != "" {
		st.appendString(quoteIdentifier(index_stmt.Idxname))
		st.appendChar(' ')
	}

	st.appendPartGroup("ON", partIndent)
	deparseRangeVar(st, index_stmt.Relation, contextNone)
	st.appendChar(' ')

	if index_stmt.AccessMethod != "" {
		st.appendString("USING ")
		st.appendString(quoteIdentifier(index_stmt.AccessMethod))
		st.appendChar(' ')
	}

	st.appendChar('(')
	for i, item := range index_stmt.IndexParams {
		deparseIndexElem(st, item.GetIndexElem())
		if i < len(index_stmt.IndexParams)-1 {
			st.appendString(", ")
		}
	}
	st.appendString(") ")

	if len(index_stmt.IndexIncludingParams) > 0 {
		st.appendString("INCLUDE (")
		for i, item := range index_stmt.IndexIncludingParams {
			deparseIndexElem(st, item.GetIndexElem())
			if i < len(index_stmt.IndexIncludingParams)-1 {
				st.appendString(", ")
			}
		}
		st.appendString(") ")
	}

	if index_stmt.NullsNotDistinct {
		st.appendString("NULLS NOT DISTINCT ")
	}

	deparseOptWith(st, index_stmt.Options)

	if index_stmt.TableSpace != "" {
		st.appendString("TABLESPACE ")
		st.appendString(quoteIdentifier(index_stmt.TableSpace))
		st.appendChar(' ')
	}

	deparseWhereClause(st, index_stmt.WhereClause)

	st.removeTrailingSpace()
}

func deparseAlterOpFamilyStmt(st *state, alter_op_family_stmt *ast.AlterOpFamilyStmt) {
	st.appendString("ALTER OPERATOR FAMILY ")
	deparseAnyName(st, alter_op_family_stmt.Opfamilyname)
	st.appendChar(' ')

	st.appendString("USING ")
	st.appendString(quoteIdentifier(alter_op_family_stmt.Amname))
	st.appendChar(' ')

	if alter_op_family_stmt.IsDrop {
		st.appendString("DROP ")
	} else {
		st.appendString("ADD ")
	}

	deparseOpclassItemList(st, alter_op_family_stmt.Items)
}

func deparsePrepareStmt(st *state, prepare_stmt *ast.PrepareStmt) {
	st.appendString("PREPARE ")
	deparseColId(st, prepare_stmt.Name)
	if len(prepare_stmt.Argtypes) > 0 {
		st.appendChar('(')
		deparseTypeList(st, prepare_stmt.Argtypes)
		st.appendChar(')')
	}
	st.appendString(" AS ")
	deparsePreparableStmt(st, prepare_stmt.Query)
}

func deparseExecuteStmt(st *state, execute_stmt *ast.ExecuteStmt) {
	st.appendString("EXECUTE ")
	st.appendString(quoteIdentifier(execute_stmt.Name))
	if len(execute_stmt.Params) > 0 {
		st.appendChar('(')
		deparseExprList(st, execute_stmt.Params)
		st.appendChar(')')
	}
}

func deparseDeallocateStmt(st *state, deallocate_stmt *ast.DeallocateStmt) {
	st.appendString("DEALLOCATE ")
	if deallocate_stmt.Name != "" {
		st.appendString(quoteIdentifier(deallocate_stmt.Name))
	} else {
		st.appendString("ALL")
	}
}

func deparseAlterRoleElem(st *state, def_elem *ast.DefElem) {
	if def_elem.Defname == "password" {
		st.appendString("PASSWORD ")
		if def_elem.Arg == nil {
			st.appendString("NULL")
		} else if def_elem.Arg.GetParamRef() != nil {
			deparseParamRef(st, def_elem.Arg.GetParamRef())
		} else if def_elem.Arg.GetString_() != nil {
			deparseStringLiteral(st, strVal(def_elem.Arg))
		}
	} else if def_elem.Defname == "connectionlimit" {
		st.appendf("CONNECTION LIMIT %d", intVal(def_elem.Arg))
	} else if def_elem.Defname == "validUntil" {
		st.appendString("VALID UNTIL ")
		deparseStringLiteral(st, strVal(def_elem.Arg))
	} else if def_elem.Defname == "superuser" && boolVal(def_elem.Arg) {
		st.appendString("SUPERUSER")
	} else if def_elem.Defname == "superuser" && !boolVal(def_elem.Arg) {
		st.appendString("NOSUPERUSER")
	} else if def_elem.Defname == "createrole" && boolVal(def_elem.Arg) {
		st.appendString("CREATEROLE")
	} else if def_elem.Defname == "createrole" && !boolVal(def_elem.Arg) {
		st.appendString("NOCREATEROLE")
	} else if def_elem.Defname == "isreplication" && boolVal(def_elem.Arg) {
		st.appendString("REPLICATION")
	} else if def_elem.Defname == "isreplication" && !boolVal(def_elem.Arg) {
		st.appendString("NOREPLICATION")
	} else if def_elem.Defname == "createdb" && boolVal(def_elem.Arg) {
		st.appendString("CREATEDB")
	} else if def_elem.Defname == "createdb" && !boolVal(def_elem.Arg) {
		st.appendString("NOCREATEDB")
	} else if def_elem.Defname == "canlogin" && boolVal(def_elem.Arg) {
		st.appendString("LOGIN")
	} else if def_elem.Defname == "canlogin" && !boolVal(def_elem.Arg) {
		st.appendString("NOLOGIN")
	} else if def_elem.Defname == "bypassrls" && boolVal(def_elem.Arg) {
		st.appendString("BYPASSRLS")
	} else if def_elem.Defname == "bypassrls" && !boolVal(def_elem.Arg) {
		st.appendString("NOBYPASSRLS")
	} else if def_elem.Defname == "inherit" && boolVal(def_elem.Arg) {
		st.appendString("INHERIT")
	} else if def_elem.Defname == "inherit" && !boolVal(def_elem.Arg) {
		st.appendString("NOINHERIT")
	}
}

func deparseCreateRoleElem(st *state, def_elem *ast.DefElem) {
	if def_elem.Defname == "sysid" {
		st.appendf("SYSID %d", intVal(def_elem.Arg))
	} else if def_elem.Defname == "adminmembers" {
		st.appendString("ADMIN ")
		deparseRoleList(st, def_elem.Arg.GetList().Items)
	} else if def_elem.Defname == "rolemembers" {
		st.appendString("ROLE ")
		deparseRoleList(st, def_elem.Arg.GetList().Items)
	} else if def_elem.Defname == "addroleto" {
		st.appendString("IN ROLE ")
		deparseRoleList(st, def_elem.Arg.GetList().Items)
	} else {
		deparseAlterRoleElem(st, def_elem)
	}
}

func deparseCreatePLangStmt(st *state, create_p_lang_stmt *ast.CreatePLangStmt) {
	st.appendString("CREATE ")

	if create_p_lang_stmt.Replace {
		st.appendString("OR REPLACE ")
	}

	if create_p_lang_stmt.Pltrusted {
		st.appendString("TRUSTED ")
	}

	st.appendString("LANGUAGE ")
	deparseNonReservedWordOrSconst(st, create_p_lang_stmt.Plname)
	st.appendChar(' ')

	st.appendString("HANDLER ")
	deparseHandlerName(st, create_p_lang_stmt.Plhandler)
	st.appendChar(' ')

	if len(create_p_lang_stmt.Plinline) > 0 {
		st.appendString("INLINE ")
		deparseHandlerName(st, create_p_lang_stmt.Plinline)
		st.appendChar(' ')
	}

	if len(create_p_lang_stmt.Plvalidator) > 0 {
		st.appendString("VALIDATOR ")
		deparseHandlerName(st, create_p_lang_stmt.Plvalidator)
		st.appendChar(' ')
	}

	st.removeTrailingSpace()
}

func deparseCreateRoleStmt(st *state, create_role_stmt *ast.CreateRoleStmt) {
	st.appendString("CREATE ")

	switch create_role_stmt.StmtType {
	case ast.RoleStmtType_ROLESTMT_ROLE:
		st.appendString("ROLE ")
	case ast.RoleStmtType_ROLESTMT_USER:
		st.appendString("USER ")
	case ast.RoleStmtType_ROLESTMT_GROUP:
		st.appendString("GROUP ")
	}

	st.appendString(quoteIdentifier(create_role_stmt.Role))
	st.appendChar(' ')

	if len(create_role_stmt.Options) > 0 {
		st.appendString("WITH ")
		for _, item := range create_role_stmt.Options {
			deparseCreateRoleElem(st, item.GetDefElem())
			st.appendChar(' ')
		}
	}

	st.removeTrailingSpace()
}

func deparseAlterRoleStmt(st *state, alter_role_stmt *ast.AlterRoleStmt) {
	st.appendString("ALTER ")

	if len(alter_role_stmt.Options) == 1 && alter_role_stmt.Options[0].GetDefElem().Defname == "rolemembers" {
		st.appendString("GROUP ")
		deparseRoleSpec(st, alter_role_stmt.Role)
		st.appendChar(' ')

		if alter_role_stmt.Action == 1 {
			st.appendString("ADD USER ")
		} else if alter_role_stmt.Action == -1 {
			st.appendString("DROP USER ")
		}

		deparseRoleList(st, alter_role_stmt.Options[0].GetDefElem().Arg.GetList().Items)
	} else {
		st.appendString("ROLE ")
		deparseRoleSpec(st, alter_role_stmt.Role)
		st.appendChar(' ')

		st.appendString("WITH ")
		for _, item := range alter_role_stmt.Options {
			deparseAlterRoleElem(st, item.GetDefElem())
			st.appendChar(' ')
		}
	}

	st.removeTrailingSpace()
}

func deparseDeclareCursorStmt(st *state, declare_cursor_stmt *ast.DeclareCursorStmt) {
	const (
		cursorOptBinary      = 0x0001
		cursorOptScroll      = 0x0002
		cursorOptNoScroll    = 0x0004
		cursorOptInsensitive = 0x0008
		cursorOptHold        = 0x0020
	)

	st.appendString("DECLARE ")
	st.appendString(quoteIdentifier(declare_cursor_stmt.Portalname))
	st.appendChar(' ')

	if declare_cursor_stmt.Options&cursorOptBinary != 0 {
		st.appendString("BINARY ")
	}

	if declare_cursor_stmt.Options&cursorOptScroll != 0 {
		st.appendString("SCROLL ")
	}

	if declare_cursor_stmt.Options&cursorOptNoScroll != 0 {
		st.appendString("NO SCROLL ")
	}

	if declare_cursor_stmt.Options&cursorOptInsensitive != 0 {
		st.appendString("INSENSITIVE ")
	}

	st.appendString("CURSOR ")

	if declare_cursor_stmt.Options&cursorOptHold != 0 {
		st.appendString("WITH HOLD ")
	}

	st.appendString("FOR ")

	deparseSelectStmt(st, declare_cursor_stmt.Query.GetSelectStmt(), contextNone)
}

func deparseFetchStmt(st *state, fetch_stmt *ast.FetchStmt) {
	const fetchAll = 9223372036854775807

	if fetch_stmt.Ismove {
		st.appendString("MOVE ")
	} else {
		st.appendString("FETCH ")
	}

	switch fetch_stmt.Direction {
	case ast.FetchDirection_FETCH_FORWARD:
		if fetch_stmt.HowMany == 1 {
			// Default
		} else if fetch_stmt.HowMany == fetchAll {
			st.appendString("ALL ")
		} else {
			st.appendf("FORWARD %d ", fetch_stmt.HowMany)
		}
	case ast.FetchDirection_FETCH_BACKWARD:
		if fetch_stmt.HowMany == 1 {
			st.appendString("PRIOR ")
		} else if fetch_stmt.HowMany == fetchAll {
			st.appendString("BACKWARD ALL ")
		} else {
			st.appendf("BACKWARD %d ", fetch_stmt.HowMany)
		}
	case ast.FetchDirection_FETCH_ABSOLUTE:
		if fetch_stmt.HowMany == 1 {
			st.appendString("FIRST ")
		} else if fetch_stmt.HowMany == -1 {
			st.appendString("LAST ")
		} else {
			st.appendf("ABSOLUTE %d ", fetch_stmt.HowMany)
		}
	case ast.FetchDirection_FETCH_RELATIVE:
		st.appendf("RELATIVE %d ", fetch_stmt.HowMany)
	}

	st.appendString(quoteIdentifier(fetch_stmt.Portalname))
}

func deparseAlterDefaultPrivilegesStmt(st *state, alter_default_privileges_stmt *ast.AlterDefaultPrivilegesStmt) {
	st.appendString("ALTER DEFAULT PRIVILEGES ")

	for _, item := range alter_default_privileges_stmt.Options {
		defelem := item.GetDefElem()
		if defelem.Defname == "schemas" {
			st.appendString("IN SCHEMA ")
			deparseNameList(st, defelem.Arg.GetList().Items)
			st.appendChar(' ')
		} else if defelem.Defname == "roles" {
			st.appendString("FOR ROLE ")
			deparseRoleList(st, defelem.Arg.GetList().Items)
			st.appendChar(' ')
		}
		// No other DefElems are supported
	}

	deparseGrantStmt(st, alter_default_privileges_stmt.Action)
}

func deparseReindexStmt(st *state, reindex_stmt *ast.ReindexStmt) {
	st.appendString("REINDEX ")

	deparseUtilityOptionList(st, reindex_stmt.Params)

	switch reindex_stmt.Kind {
	case ast.ReindexObjectType_REINDEX_OBJECT_INDEX:
		st.appendString("INDEX ")
	case ast.ReindexObjectType_REINDEX_OBJECT_TABLE:
		st.appendString("TABLE ")
	case ast.ReindexObjectType_REINDEX_OBJECT_SCHEMA:
		st.appendString("SCHEMA ")
	case ast.ReindexObjectType_REINDEX_OBJECT_SYSTEM:
		st.appendString("SYSTEM ")
	case ast.ReindexObjectType_REINDEX_OBJECT_DATABASE:
		st.appendString("DATABASE ")
	}

	if reindex_stmt.Relation != nil {
		deparseRangeVar(st, reindex_stmt.Relation, contextNone)
	} else if reindex_stmt.Name != "" {
		st.appendString(quoteIdentifier(reindex_stmt.Name))
	}
}

func deparseRuleStmt(st *state, rule_stmt *ast.RuleStmt) {
	st.appendString("CREATE ")

	if rule_stmt.Replace {
		st.appendString("OR REPLACE ")
	}

	st.appendString("RULE ")
	st.appendString(quoteIdentifier(rule_stmt.Rulename))
	st.appendString(" AS ON ")

	switch rule_stmt.Event {
	case ast.CmdType_CMD_UNKNOWN, ast.CmdType_CMD_UTILITY, ast.CmdType_CMD_NOTHING:
		// Not supported here
	case ast.CmdType_CMD_SELECT:
		st.appendString("SELECT ")
	case ast.CmdType_CMD_UPDATE:
		st.appendString("UPDATE ")
	case ast.CmdType_CMD_INSERT:
		st.appendString("INSERT ")
	case ast.CmdType_CMD_DELETE:
		st.appendString("DELETE ")
	case ast.CmdType_CMD_MERGE:
		st.appendString("MERGE ")
	}

	st.appendString("TO ")
	deparseRangeVar(st, rule_stmt.Relation, contextNone)
	st.appendChar(' ')

	deparseWhereClause(st, rule_stmt.WhereClause)

	st.appendString("DO ")

	if rule_stmt.Instead {
		st.appendString("INSTEAD ")
	}

	if len(rule_stmt.Actions) == 0 {
		st.appendString("NOTHING")
	} else if len(rule_stmt.Actions) == 1 {
		deparseRuleActionStmt(st, rule_stmt.Actions[0])
	} else {
		st.appendChar('(')
		for i, item := range rule_stmt.Actions {
			deparseRuleActionStmt(st, item)
			if i < len(rule_stmt.Actions)-1 {
				st.appendString("; ")
			}
		}
		st.appendChar(')')
	}
}

func deparseNotifyStmt(st *state, notify_stmt *ast.NotifyStmt) {
	st.appendString("NOTIFY ")
	st.appendString(quoteIdentifier(notify_stmt.Conditionname))

	if notify_stmt.Payload != "" {
		st.appendString(", ")
		deparseStringLiteral(st, notify_stmt.Payload)
	}
}

func deparseListenStmt(st *state, listen_stmt *ast.ListenStmt) {
	st.appendString("LISTEN ")
	st.appendString(quoteIdentifier(listen_stmt.Conditionname))
}

func deparseUnlistenStmt(st *state, unlisten_stmt *ast.UnlistenStmt) {
	st.appendString("UNLISTEN ")
	if unlisten_stmt.Conditionname == "" {
		st.appendString("*")
	} else {
		st.appendString(quoteIdentifier(unlisten_stmt.Conditionname))
	}
}

func deparseCreateSeqStmt(st *state, create_seq_stmt *ast.CreateSeqStmt) {
	st.appendString("CREATE ")

	deparseOptTemp(st, create_seq_stmt.Sequence.Relpersistence)

	st.appendString("SEQUENCE ")

	if create_seq_stmt.IfNotExists {
		st.appendString("IF NOT EXISTS ")
	}

	deparseRangeVar(st, create_seq_stmt.Sequence, contextNone)
	st.appendChar(' ')

	deparseOptSeqOptList(st, create_seq_stmt.Options)

	st.removeTrailingSpace()
}

func deparseAlterFunctionStmt(st *state, alter_function_stmt *ast.AlterFunctionStmt) {
	st.appendString("ALTER ")

	switch alter_function_stmt.Objtype {
	case ast.ObjectType_OBJECT_FUNCTION:
		st.appendString("FUNCTION ")
	case ast.ObjectType_OBJECT_PROCEDURE:
		st.appendString("PROCEDURE ")
	case ast.ObjectType_OBJECT_ROUTINE:
		st.appendString("ROUTINE ")
	default:
		// Not supported here
	}

	deparseFunctionWithArgtypes(st, alter_function_stmt.Func)
	st.appendChar(' ')

	for i, item := range alter_function_stmt.Actions {
		deparseCommonFuncOptItem(st, item.GetDefElem())
		if i < len(alter_function_stmt.Actions)-1 {
			st.appendChar(' ')
		}
	}
}

func deparseTruncateStmt(st *state, truncate_stmt *ast.TruncateStmt) {
	st.appendString("TRUNCATE ")

	deparseRelationExprList(st, truncate_stmt.Relations)
	st.appendChar(' ')

	if truncate_stmt.RestartSeqs {
		st.appendString("RESTART IDENTITY ")
	}

	deparseOptDropBehavior(st, truncate_stmt.Behavior)

	st.removeTrailingSpace()
}

func deparseCreateEventTrigStmt(st *state, create_event_trig_stmt *ast.CreateEventTrigStmt) {
	st.appendString("CREATE EVENT TRIGGER ")
	st.appendString(quoteIdentifier(create_event_trig_stmt.Trigname))
	st.appendChar(' ')

	st.appendString("ON ")
	st.appendString(quoteIdentifier(create_event_trig_stmt.Eventname))
	st.appendChar(' ')

	if len(create_event_trig_stmt.Whenclause) > 0 {
		st.appendString("WHEN ")

		for i, item := range create_event_trig_stmt.Whenclause {
			def_elem := item.GetDefElem()
			l := def_elem.Arg.GetList().Items
			st.appendString(quoteIdentifier(def_elem.Defname))
			st.appendString(" IN (")
			for j, item2 := range l {
				deparseStringLiteral(st, strVal(item2))
				if j < len(l)-1 {
					st.appendString(", ")
				}
			}
			st.appendChar(')')
			if i < len(create_event_trig_stmt.Whenclause)-1 {
				st.appendString(" AND ")
			}
		}

		st.appendChar(' ')
	}

	st.appendString("EXECUTE FUNCTION ")
	deparseFuncName(st, create_event_trig_stmt.Funcname)
	st.appendString("()")
}

func deparseAlterEventTrigStmt(st *state, alter_event_trig_stmt *ast.AlterEventTrigStmt) {
	st.appendString("ALTER EVENT TRIGGER ")
	st.appendString(quoteIdentifier(alter_event_trig_stmt.Trigname))
	st.appendChar(' ')

	switch alter_event_trig_stmt.Tgenabled {
	case "O": // TRIGGER_FIRES_ON_ORIGIN
		st.appendString("ENABLE")
	case "R": // TRIGGER_FIRES_ON_REPLICA
		st.appendString("ENABLE REPLICA")
	case "A": // TRIGGER_FIRES_ALWAYS
		st.appendString("ENABLE ALWAYS")
	case "D": // TRIGGER_DISABLED
		st.appendString("DISABLE")
	}
}

func deparseRefreshMatViewStmt(st *state, refresh_mat_view_stmt *ast.RefreshMatViewStmt) {
	st.appendString("REFRESH MATERIALIZED VIEW ")

	if refresh_mat_view_stmt.Concurrent {
		st.appendString("CONCURRENTLY ")
	}

	deparseRangeVar(st, refresh_mat_view_stmt.Relation, contextNone)
	st.appendChar(' ')

	if refresh_mat_view_stmt.SkipData {
		st.appendString("WITH NO DATA ")
	}

	st.removeTrailingSpace()
}

func deparseReplicaIdentityStmt(st *state, replica_identity_stmt *ast.ReplicaIdentityStmt) {
	switch replica_identity_stmt.IdentityType {
	case "n": // REPLICA_IDENTITY_NOTHING
		st.appendString("NOTHING ")
	case "f": // REPLICA_IDENTITY_FULL
		st.appendString("FULL ")
	case "d": // REPLICA_IDENTITY_DEFAULT
		st.appendString("DEFAULT ")
	case "i": // REPLICA_IDENTITY_INDEX
		st.appendString("USING INDEX ")
		st.appendString(quoteIdentifier(replica_identity_stmt.Name))
	}
}

func deparseCreatePolicyStmt(st *state, create_policy_stmt *ast.CreatePolicyStmt) {
	st.appendString("CREATE POLICY ")
	deparseColId(st, create_policy_stmt.PolicyName)
	st.appendString(" ON ")
	deparseRangeVar(st, create_policy_stmt.Table, contextNone)
	st.appendChar(' ')

	if !create_policy_stmt.Permissive {
		st.appendString("AS RESTRICTIVE ")
	}

	if create_policy_stmt.CmdName == "all" {
		// Default
	} else if create_policy_stmt.CmdName == "select" {
		st.appendString("FOR SELECT ")
	} else if create_policy_stmt.CmdName == "insert" {
		st.appendString("FOR INSERT ")
	} else if create_policy_stmt.CmdName == "update" {
		st.appendString("FOR UPDATE ")
	} else if create_policy_stmt.CmdName == "delete" {
		st.appendString("FOR DELETE ")
	}

	st.appendString("TO ")
	deparseRoleList(st, create_policy_stmt.Roles)
	st.appendChar(' ')

	if create_policy_stmt.Qual != nil {
		st.appendString("USING (")
		deparseExpr(st, create_policy_stmt.Qual, contextAExpr)
		st.appendString(") ")
	}

	if create_policy_stmt.WithCheck != nil {
		st.appendString("WITH CHECK (")
		deparseExpr(st, create_policy_stmt.WithCheck, contextAExpr)
		st.appendString(") ")
	}
}
