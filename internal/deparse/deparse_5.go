// Ported from postgres_deparse.c (libpg_query 17-6.2.2).
// Grouping sets, DML statements (INSERT/UPDATE/DELETE/MERGE), ALTER TABLE, COPY.
package deparse

import (
	"github.com/sqlc-dev/oliphant/ast"
)

func deparseGroupingSet(st *state, grouping_set *ast.GroupingSet) {
	switch grouping_set.Kind {
	case ast.GroupingSetKind_GROUPING_SET_EMPTY:
		st.appendString("()")
	case ast.GroupingSetKind_GROUPING_SET_SIMPLE:
		// Not present in raw parse trees
	case ast.GroupingSetKind_GROUPING_SET_ROLLUP:
		st.appendString("ROLLUP (")
		deparseExprList(st, grouping_set.Content)
		st.appendChar(')')
	case ast.GroupingSetKind_GROUPING_SET_CUBE:
		st.appendString("CUBE (")
		deparseExprList(st, grouping_set.Content)
		st.appendChar(')')
	case ast.GroupingSetKind_GROUPING_SET_SETS:
		st.appendString("GROUPING SETS (")
		deparseGroupByList(st, grouping_set.Content)
		st.appendChar(')')
	}
}

func deparseDropTableSpaceStmt(st *state, drop_table_space_stmt *ast.DropTableSpaceStmt) {
	st.appendString("DROP TABLESPACE ")

	if drop_table_space_stmt.MissingOk {
		st.appendString("IF EXISTS ")
	}

	st.appendString(drop_table_space_stmt.Tablespacename)
}

func deparseAlterObjectDependsStmt(st *state, alter_object_depends_stmt *ast.AlterObjectDependsStmt) {
	st.appendString("ALTER ")

	switch alter_object_depends_stmt.ObjectType {
	case ast.ObjectType_OBJECT_FUNCTION:
		st.appendString("FUNCTION ")
		deparseFunctionWithArgtypes(st, alter_object_depends_stmt.Object.GetObjectWithArgs())
	case ast.ObjectType_OBJECT_PROCEDURE:
		st.appendString("PROCEDURE ")
		deparseFunctionWithArgtypes(st, alter_object_depends_stmt.Object.GetObjectWithArgs())
	case ast.ObjectType_OBJECT_ROUTINE:
		st.appendString("ROUTINE ")
		deparseFunctionWithArgtypes(st, alter_object_depends_stmt.Object.GetObjectWithArgs())
	case ast.ObjectType_OBJECT_TRIGGER:
		st.appendString("TRIGGER ")
		deparseColId(st, strVal(alter_object_depends_stmt.Object.GetList().Items[0]))
		st.appendString(" ON ")
		deparseRangeVar(st, alter_object_depends_stmt.Relation, contextNone)
	case ast.ObjectType_OBJECT_MATVIEW:
		st.appendString("MATERIALIZED VIEW ")
		deparseRangeVar(st, alter_object_depends_stmt.Relation, contextNone)
	case ast.ObjectType_OBJECT_INDEX:
		st.appendString("INDEX ")
		deparseRangeVar(st, alter_object_depends_stmt.Relation, contextNone)
	default:
		// No other object types supported here
	}
	st.appendChar(' ')

	if alter_object_depends_stmt.Remove {
		st.appendString("NO ")
	}

	st.appendf("DEPENDS ON EXTENSION %s", alter_object_depends_stmt.Extname.Sval)
}

func deparseAlterObjectSchemaStmt(st *state, alter_object_schema_stmt *ast.AlterObjectSchemaStmt) {
	var l []*ast.Node

	st.appendString("ALTER ")

	switch alter_object_schema_stmt.ObjectType {
	case ast.ObjectType_OBJECT_AGGREGATE:
		st.appendString("AGGREGATE ")
		deparseAggregateWithArgtypes(st, alter_object_schema_stmt.Object.GetObjectWithArgs())
	case ast.ObjectType_OBJECT_COLLATION:
		st.appendString("COLLATION ")
		deparseAnyName(st, alter_object_schema_stmt.Object.GetList().Items)
	case ast.ObjectType_OBJECT_CONVERSION:
		st.appendString("CONVERSION ")
		deparseAnyName(st, alter_object_schema_stmt.Object.GetList().Items)
	case ast.ObjectType_OBJECT_DOMAIN:
		st.appendString("DOMAIN ")
		deparseAnyName(st, alter_object_schema_stmt.Object.GetList().Items)
	case ast.ObjectType_OBJECT_EXTENSION:
		st.appendString("EXTENSION ")
		st.appendString(quoteIdentifier(strVal(alter_object_schema_stmt.Object)))
	case ast.ObjectType_OBJECT_FUNCTION:
		st.appendString("FUNCTION ")
		deparseFunctionWithArgtypes(st, alter_object_schema_stmt.Object.GetObjectWithArgs())
	case ast.ObjectType_OBJECT_OPERATOR:
		st.appendString("OPERATOR ")
		deparseOperatorWithArgtypes(st, alter_object_schema_stmt.Object.GetObjectWithArgs())
	case ast.ObjectType_OBJECT_OPCLASS:
		l = alter_object_schema_stmt.Object.GetList().Items
		st.appendString("OPERATOR CLASS ")
		deparseAnyNameSkipFirst(st, l)
		st.appendString(" USING ")
		st.appendString(quoteIdentifier(strVal(l[0])))
	case ast.ObjectType_OBJECT_OPFAMILY:
		l = alter_object_schema_stmt.Object.GetList().Items
		st.appendString("OPERATOR FAMILY ")
		deparseAnyNameSkipFirst(st, l)
		st.appendString(" USING ")
		st.appendString(quoteIdentifier(strVal(l[0])))
	case ast.ObjectType_OBJECT_PROCEDURE:
		st.appendString("PROCEDURE ")
		deparseFunctionWithArgtypes(st, alter_object_schema_stmt.Object.GetObjectWithArgs())
	case ast.ObjectType_OBJECT_ROUTINE:
		st.appendString("ROUTINE ")
		deparseFunctionWithArgtypes(st, alter_object_schema_stmt.Object.GetObjectWithArgs())
	case ast.ObjectType_OBJECT_TABLE:
		st.appendString("TABLE ")
		if alter_object_schema_stmt.MissingOk {
			st.appendString("IF EXISTS ")
		}
		deparseRangeVar(st, alter_object_schema_stmt.Relation, contextNone)
	case ast.ObjectType_OBJECT_STATISTIC_EXT:
		st.appendString("STATISTICS ")
		deparseAnyName(st, alter_object_schema_stmt.Object.GetList().Items)
	case ast.ObjectType_OBJECT_TSPARSER:
		st.appendString("TEXT SEARCH PARSER ")
		deparseAnyName(st, alter_object_schema_stmt.Object.GetList().Items)
	case ast.ObjectType_OBJECT_TSDICTIONARY:
		st.appendString("TEXT SEARCH DICTIONARY ")
		deparseAnyName(st, alter_object_schema_stmt.Object.GetList().Items)
	case ast.ObjectType_OBJECT_TSTEMPLATE:
		st.appendString("TEXT SEARCH TEMPLATE ")
		deparseAnyName(st, alter_object_schema_stmt.Object.GetList().Items)
	case ast.ObjectType_OBJECT_TSCONFIGURATION:
		st.appendString("TEXT SEARCH CONFIGURATION ")
		deparseAnyName(st, alter_object_schema_stmt.Object.GetList().Items)
	case ast.ObjectType_OBJECT_SEQUENCE:
		st.appendString("SEQUENCE ")
		if alter_object_schema_stmt.MissingOk {
			st.appendString("IF EXISTS ")
		}
		deparseRangeVar(st, alter_object_schema_stmt.Relation, contextNone)
	case ast.ObjectType_OBJECT_VIEW:
		st.appendString("VIEW ")
		if alter_object_schema_stmt.MissingOk {
			st.appendString("IF EXISTS ")
		}
		deparseRangeVar(st, alter_object_schema_stmt.Relation, contextNone)
	case ast.ObjectType_OBJECT_MATVIEW:
		st.appendString("MATERIALIZED VIEW ")
		if alter_object_schema_stmt.MissingOk {
			st.appendString("IF EXISTS ")
		}
		deparseRangeVar(st, alter_object_schema_stmt.Relation, contextNone)
	case ast.ObjectType_OBJECT_FOREIGN_TABLE:
		st.appendString("FOREIGN TABLE ")
		if alter_object_schema_stmt.MissingOk {
			st.appendString("IF EXISTS ")
		}
		deparseRangeVar(st, alter_object_schema_stmt.Relation, contextNone)
	case ast.ObjectType_OBJECT_TYPE:
		st.appendString("TYPE ")
		deparseAnyName(st, alter_object_schema_stmt.Object.GetList().Items)
	default:
	}

	st.appendString(" SET SCHEMA ")
	st.appendString(quoteIdentifier(alter_object_schema_stmt.Newschema))
}

func deparseAlterTableCmd(st *state, alter_table_cmd *ast.AlterTableCmd, ctx nodeContext) {
	options := ""
	trailing_missing_ok := false

	switch alter_table_cmd.Subtype {
	case ast.AlterTableType_AT_AddColumn: /* add column */
		if ctx == contextAlterType {
			st.appendString("ADD ATTRIBUTE ")
		} else {
			st.appendString("ADD COLUMN ")
		}
	case ast.AlterTableType_AT_AddColumnToView: /* implicitly via CREATE OR REPLACE VIEW */
		// Not present in raw parser output
	case ast.AlterTableType_AT_ColumnDefault: /* alter column default */
		st.appendString("ALTER COLUMN ")
		if alter_table_cmd.Def != nil {
			options = "SET DEFAULT"
		} else {
			options = "DROP DEFAULT"
		}
	case ast.AlterTableType_AT_CookedColumnDefault: /* add a pre-cooked column default */
		// Not present in raw parser output
	case ast.AlterTableType_AT_DropNotNull: /* alter column drop not null */
		st.appendString("ALTER COLUMN ")
		options = "DROP NOT NULL"
	case ast.AlterTableType_AT_SetNotNull: /* alter column set not null */
		st.appendString("ALTER COLUMN ")
		options = "SET NOT NULL"
	case ast.AlterTableType_AT_DropExpression: /* alter column drop expression */
		st.appendString("ALTER COLUMN ")
		options = "DROP EXPRESSION"
		trailing_missing_ok = true
	case ast.AlterTableType_AT_CheckNotNull: /* check column is already marked not null */
		// Not present in raw parser output
	case ast.AlterTableType_AT_SetStatistics: /* alter column set statistics */
		st.appendString("ALTER COLUMN ")
		options = "SET STATISTICS"
	case ast.AlterTableType_AT_SetOptions: /* alter column set ( options ) */
		st.appendString("ALTER COLUMN ")
		options = "SET"
	case ast.AlterTableType_AT_ResetOptions: /* alter column reset ( options ) */
		st.appendString("ALTER COLUMN ")
		options = "RESET"
	case ast.AlterTableType_AT_SetStorage: /* alter column set storage */
		st.appendString("ALTER COLUMN ")
		options = "SET STORAGE"
	case ast.AlterTableType_AT_SetCompression: /* alter column set compression */
		st.appendString("ALTER COLUMN ")
		options = "SET COMPRESSION"
	case ast.AlterTableType_AT_DropColumn: /* drop column */
		if ctx == contextAlterType {
			st.appendString("DROP ATTRIBUTE ")
		} else {
			st.appendString("DROP ")
		}
	case ast.AlterTableType_AT_AddIndex: /* add index */
		st.appendString("ADD INDEX ")
	case ast.AlterTableType_AT_ReAddIndex: /* internal to commands/tablecmds.c */
	case ast.AlterTableType_AT_AddConstraint: /* add constraint */
		st.appendString("ADD ")
	case ast.AlterTableType_AT_ReAddConstraint: /* internal to commands/tablecmds.c */
	case ast.AlterTableType_AT_ReAddDomainConstraint: /* internal to commands/tablecmds.c */
	case ast.AlterTableType_AT_AlterConstraint: /* alter constraint */
		st.appendString("ALTER ") // CONSTRAINT keyword gets added by the Constraint itself (when deparsing def)
	case ast.AlterTableType_AT_ValidateConstraint: /* validate constraint */
		st.appendString("VALIDATE CONSTRAINT ")
	case ast.AlterTableType_AT_AddIndexConstraint: /* add constraint using existing index */
		// Not present in raw parser output
	case ast.AlterTableType_AT_DropConstraint: /* drop constraint */
		st.appendString("DROP CONSTRAINT ")
	case ast.AlterTableType_AT_ReAddComment, /* internal to commands/tablecmds.c */
		ast.AlterTableType_AT_ReAddStatistics: /* internal to commands/tablecmds.c */
	case ast.AlterTableType_AT_AlterColumnType: /* alter column type */
		if ctx == contextAlterType {
			st.appendString("ALTER ATTRIBUTE ")
		} else {
			st.appendString("ALTER COLUMN ")
		}
		options = "TYPE"
	case ast.AlterTableType_AT_AlterColumnGenericOptions: /* alter column OPTIONS (...) */
		st.appendString("ALTER COLUMN ")
		// Handled via special case in def handling
	case ast.AlterTableType_AT_ChangeOwner: /* change owner */
		st.appendString("OWNER TO ")
		deparseRoleSpec(st, alter_table_cmd.Newowner)
	case ast.AlterTableType_AT_ClusterOn: /* CLUSTER ON */
		st.appendString("CLUSTER ON ")
	case ast.AlterTableType_AT_DropCluster: /* SET WITHOUT CLUSTER */
		st.appendString("SET WITHOUT CLUSTER ")
	case ast.AlterTableType_AT_SetLogged: /* SET LOGGED */
		st.appendString("SET LOGGED ")
	case ast.AlterTableType_AT_SetUnLogged: /* SET UNLOGGED */
		st.appendString("SET UNLOGGED ")
	case ast.AlterTableType_AT_DropOids: /* SET WITHOUT OIDS */
		st.appendString("SET WITHOUT OIDS ")
	case ast.AlterTableType_AT_SetTableSpace: /* SET TABLESPACE */
		st.appendString("SET TABLESPACE ")
	case ast.AlterTableType_AT_SetRelOptions: /* SET (...) -- AM specific parameters */
		st.appendString("SET ")
	case ast.AlterTableType_AT_SetAccessMethod:
		st.appendf("SET ACCESS METHOD ")
	case ast.AlterTableType_AT_ResetRelOptions: /* RESET (...) -- AM specific parameters */
		st.appendString("RESET ")
	case ast.AlterTableType_AT_ReplaceRelOptions: /* replace reloption list in its entirety */
		// Not present in raw parser output
	case ast.AlterTableType_AT_EnableTrig: /* ENABLE TRIGGER name */
		st.appendString("ENABLE TRIGGER ")
	case ast.AlterTableType_AT_EnableAlwaysTrig: /* ENABLE ALWAYS TRIGGER name */
		st.appendString("ENABLE ALWAYS TRIGGER ")
	case ast.AlterTableType_AT_EnableReplicaTrig: /* ENABLE REPLICA TRIGGER name */
		st.appendString("ENABLE REPLICA TRIGGER ")
	case ast.AlterTableType_AT_DisableTrig: /* DISABLE TRIGGER name */
		st.appendString("DISABLE TRIGGER ")
	case ast.AlterTableType_AT_EnableTrigAll: /* ENABLE TRIGGER ALL */
		st.appendString("ENABLE TRIGGER ALL ")
	case ast.AlterTableType_AT_DisableTrigAll: /* DISABLE TRIGGER ALL */
		st.appendString("DISABLE TRIGGER ALL ")
	case ast.AlterTableType_AT_EnableTrigUser: /* ENABLE TRIGGER USER */
		st.appendString("ENABLE TRIGGER USER ")
	case ast.AlterTableType_AT_DisableTrigUser: /* DISABLE TRIGGER USER */
		st.appendString("DISABLE TRIGGER USER ")
	case ast.AlterTableType_AT_EnableRule: /* ENABLE RULE name */
		st.appendString("ENABLE RULE ")
	case ast.AlterTableType_AT_EnableAlwaysRule: /* ENABLE ALWAYS RULE name */
		st.appendString("ENABLE ALWAYS RULE ")
	case ast.AlterTableType_AT_EnableReplicaRule: /* ENABLE REPLICA RULE name */
		st.appendString("ENABLE REPLICA RULE ")
	case ast.AlterTableType_AT_DisableRule: /* DISABLE RULE name */
		st.appendString("DISABLE RULE ")
	case ast.AlterTableType_AT_AddInherit: /* INHERIT parent */
		st.appendString("INHERIT ")
	case ast.AlterTableType_AT_DropInherit: /* NO INHERIT parent */
		st.appendString("NO INHERIT ")
	case ast.AlterTableType_AT_AddOf: /* OF <type_name> */
		st.appendString("OF ")
	case ast.AlterTableType_AT_DropOf: /* NOT OF */
		st.appendString("NOT OF ")
	case ast.AlterTableType_AT_ReplicaIdentity: /* REPLICA IDENTITY */
		st.appendString("REPLICA IDENTITY ")
	case ast.AlterTableType_AT_EnableRowSecurity: /* ENABLE ROW SECURITY */
		st.appendString("ENABLE ROW LEVEL SECURITY ")
	case ast.AlterTableType_AT_DisableRowSecurity: /* DISABLE ROW SECURITY */
		st.appendString("DISABLE ROW LEVEL SECURITY ")
	case ast.AlterTableType_AT_ForceRowSecurity: /* FORCE ROW SECURITY */
		st.appendString("FORCE ROW LEVEL SECURITY ")
	case ast.AlterTableType_AT_NoForceRowSecurity: /* NO FORCE ROW SECURITY */
		st.appendString("NO FORCE ROW LEVEL SECURITY ")
	case ast.AlterTableType_AT_GenericOptions: /* OPTIONS (...) */
		// Handled in def field handling
	case ast.AlterTableType_AT_AttachPartition: /* ATTACH PARTITION */
		st.appendString("ATTACH PARTITION ")
	case ast.AlterTableType_AT_DetachPartition: /* DETACH PARTITION */
		st.appendString("DETACH PARTITION ")
	case ast.AlterTableType_AT_DetachPartitionFinalize: /* DETACH PARTITION FINALIZE */
		st.appendString("DETACH PARTITION ")
	case ast.AlterTableType_AT_AddIdentity: /* ADD IDENTITY */
		st.appendString("ALTER ")
		options = "ADD"
		// Other details are output via the constraint node (in def field)
	case ast.AlterTableType_AT_SetIdentity: /* SET identity column options */
		st.appendString("ALTER ")
	case ast.AlterTableType_AT_DropIdentity: /* DROP IDENTITY */
		st.appendString("ALTER COLUMN ")
		options = "DROP IDENTITY"
		trailing_missing_ok = true
	case ast.AlterTableType_AT_SetExpression:
		st.appendString("ALTER COLUMN ")
	}

	if alter_table_cmd.MissingOk && !trailing_missing_ok {
		if alter_table_cmd.Subtype == ast.AlterTableType_AT_AddColumn {
			st.appendString("IF NOT EXISTS ")
		} else {
			st.appendString("IF EXISTS ")
		}
	}

	if alter_table_cmd.Name != "" {
		st.appendString(quoteIdentifier(alter_table_cmd.Name))
		st.appendChar(' ')
	} else if alter_table_cmd.Subtype == ast.AlterTableType_AT_SetAccessMethod {
		st.appendString(" DEFAULT")
	}

	if alter_table_cmd.Num > 0 {
		st.appendf("%d ", alter_table_cmd.Num)
	}

	if options != "" {
		st.appendString(options)
		st.appendChar(' ')
	}

	if alter_table_cmd.MissingOk && trailing_missing_ok {
		st.appendString("IF EXISTS ")
	}

	switch alter_table_cmd.Subtype {
	case ast.AlterTableType_AT_AttachPartition,
		ast.AlterTableType_AT_DetachPartition:
		deparsePartitionCmd(st, alter_table_cmd.Def.GetPartitionCmd())
		st.appendChar(' ')
	case ast.AlterTableType_AT_DetachPartitionFinalize:
		deparsePartitionCmd(st, alter_table_cmd.Def.GetPartitionCmd())
		st.appendString("FINALIZE ")
	case ast.AlterTableType_AT_AddColumn,
		ast.AlterTableType_AT_AlterColumnType:
		deparseColumnDef(st, alter_table_cmd.Def.GetColumnDef())
		st.appendChar(' ')
	case ast.AlterTableType_AT_ColumnDefault:
		if alter_table_cmd.Def != nil {
			deparseExpr(st, alter_table_cmd.Def, contextAExpr)
			st.appendChar(' ')
		}
	case ast.AlterTableType_AT_SetStatistics:
		if alter_table_cmd.Def != nil {
			deparseSignedIconst(st, alter_table_cmd.Def)
		} else {
			st.appendString("DEFAULT")
		}
		st.appendChar(' ')
	case ast.AlterTableType_AT_SetOptions,
		ast.AlterTableType_AT_ResetOptions,
		ast.AlterTableType_AT_SetRelOptions,
		ast.AlterTableType_AT_ResetRelOptions:
		deparseRelOptions(st, alter_table_cmd.Def.GetList().Items)
		st.appendChar(' ')
	case ast.AlterTableType_AT_SetStorage:
		deparseColId(st, strVal(alter_table_cmd.Def))
		st.appendChar(' ')
	case ast.AlterTableType_AT_SetCompression:
		if strVal(alter_table_cmd.Def) == "default" {
			st.appendString("DEFAULT")
		} else {
			deparseColId(st, strVal(alter_table_cmd.Def))
		}
		st.appendChar(' ')
	case ast.AlterTableType_AT_AddIdentity,
		ast.AlterTableType_AT_AddConstraint,
		ast.AlterTableType_AT_AlterConstraint:
		deparseConstraint(st, alter_table_cmd.Def.GetConstraint())
		st.appendChar(' ')
	case ast.AlterTableType_AT_SetIdentity:
		deparseAlterIdentityColumnOptionList(st, alter_table_cmd.Def.GetList().Items)
		st.appendChar(' ')
	case ast.AlterTableType_AT_AlterColumnGenericOptions,
		ast.AlterTableType_AT_GenericOptions:
		deparseAlterGenericOptions(st, alter_table_cmd.Def.GetList().Items)
		st.appendChar(' ')
	case ast.AlterTableType_AT_AddInherit,
		ast.AlterTableType_AT_DropInherit:
		deparseRangeVar(st, alter_table_cmd.Def.GetRangeVar(), contextNone)
		st.appendChar(' ')
	case ast.AlterTableType_AT_AddOf:
		deparseTypeName(st, alter_table_cmd.Def.GetTypeName())
		st.appendChar(' ')
	case ast.AlterTableType_AT_ReplicaIdentity:
		deparseReplicaIdentityStmt(st, alter_table_cmd.Def.GetReplicaIdentityStmt())
		st.appendChar(' ')
	case ast.AlterTableType_AT_SetExpression:
		st.appendString("SET EXPRESSION AS (")
		deparseExpr(st, alter_table_cmd.Def, contextAExpr)
		st.appendChar(')')
	default:
	}

	deparseOptDropBehavior(st, alter_table_cmd.Behavior)

	st.removeTrailingSpace()
}

func deparseAlterTableObjType(st *state, typ ast.ObjectType) nodeContext {
	switch typ {
	case ast.ObjectType_OBJECT_TABLE:
		st.appendString("TABLE ")
	case ast.ObjectType_OBJECT_FOREIGN_TABLE:
		st.appendString("FOREIGN TABLE ")
	case ast.ObjectType_OBJECT_INDEX:
		st.appendString("INDEX ")
	case ast.ObjectType_OBJECT_SEQUENCE:
		st.appendString("SEQUENCE ")
	case ast.ObjectType_OBJECT_VIEW:
		st.appendString("VIEW ")
	case ast.ObjectType_OBJECT_MATVIEW:
		st.appendString("MATERIALIZED VIEW ")
	case ast.ObjectType_OBJECT_TYPE:
		st.appendString("TYPE ")
		return contextAlterType
	default:
	}

	return contextNone
}

func deparseAlterTableMoveAllStmt(st *state, move_all_stmt *ast.AlterTableMoveAllStmt) {
	st.appendString("ALTER ")
	deparseAlterTableObjType(st, move_all_stmt.Objtype)

	st.appendString("ALL IN TABLESPACE ")
	st.appendString(move_all_stmt.OrigTablespacename)
	st.appendChar(' ')

	if len(move_all_stmt.Roles) > 0 {
		st.appendString("OWNED BY ")
		deparseRoleList(st, move_all_stmt.Roles)
		st.appendChar(' ')
	}

	st.appendString("SET TABLESPACE ")
	st.appendString(move_all_stmt.NewTablespacename)
	st.appendChar(' ')

	if move_all_stmt.Nowait {
		st.appendString("NOWAIT")
	}
}

func deparseAlterTableStmt(st *state, alter_table_stmt *ast.AlterTableStmt) {
	st.appendString("ALTER ")
	context := deparseAlterTableObjType(st, alter_table_stmt.Objtype)

	if alter_table_stmt.MissingOk {
		st.appendString("IF EXISTS ")
	}

	deparseRangeVar(st, alter_table_stmt.Relation, context)
	st.appendChar(' ')

	parentLevel := st.increaseNestingLevel()
	for i, cmd := range alter_table_stmt.Cmds {
		deparseAlterTableCmd(st, cmd.GetAlterTableCmd(), context)
		if i < len(alter_table_stmt.Cmds)-1 {
			st.appendCommaAndPart()
		}
	}
	st.decreaseNestingLevel(parentLevel)
}

func deparseAlterTableSpaceOptionsStmt(st *state, alter_table_space_options_stmt *ast.AlterTableSpaceOptionsStmt) {
	st.appendString("ALTER TABLESPACE ")
	deparseColId(st, alter_table_space_options_stmt.Tablespacename)
	st.appendChar(' ')

	if alter_table_space_options_stmt.IsReset {
		st.appendString("RESET ")
	} else {
		st.appendString("SET ")
	}

	deparseRelOptions(st, alter_table_space_options_stmt.Options)
}

func deparseAlterDomainStmt(st *state, alter_domain_stmt *ast.AlterDomainStmt) {
	st.appendString("ALTER DOMAIN ")
	deparseAnyName(st, alter_domain_stmt.TypeName)
	st.appendChar(' ')

	switch alter_domain_stmt.Subtype {
	case "T":
		if alter_domain_stmt.Def != nil {
			st.appendString("SET DEFAULT ")
			deparseExpr(st, alter_domain_stmt.Def, contextAExpr)
		} else {
			st.appendString("DROP DEFAULT")
		}
	case "N":
		st.appendString("DROP NOT NULL")
	case "O":
		st.appendString("SET NOT NULL")
	case "C":
		st.appendString("ADD ")
		deparseConstraint(st, alter_domain_stmt.Def.GetConstraint())
	case "X":
		st.appendString("DROP CONSTRAINT ")
		if alter_domain_stmt.MissingOk {
			st.appendString("IF EXISTS ")
		}
		st.appendString(quoteIdentifier(alter_domain_stmt.Name))
		if alter_domain_stmt.Behavior == ast.DropBehavior_DROP_CASCADE {
			st.appendString(" CASCADE")
		}
	case "V":
		st.appendString("VALIDATE CONSTRAINT ")
		st.appendString(quoteIdentifier(alter_domain_stmt.Name))
	default:
		// No other subtypes supported by the parser
	}
}

func deparseRenameStmt(st *state, rename_stmt *ast.RenameStmt) {
	var l []*ast.Node

	st.appendString("ALTER ")

	switch rename_stmt.RenameType {
	case ast.ObjectType_OBJECT_AGGREGATE:
		st.appendString("AGGREGATE ")
	case ast.ObjectType_OBJECT_COLLATION:
		st.appendString("COLLATION ")
	case ast.ObjectType_OBJECT_CONVERSION:
		st.appendString("CONVERSION ")
	case ast.ObjectType_OBJECT_DATABASE:
		st.appendString("DATABASE ")
	case ast.ObjectType_OBJECT_DOMAIN,
		ast.ObjectType_OBJECT_DOMCONSTRAINT:
		st.appendString("DOMAIN ")
	case ast.ObjectType_OBJECT_FDW:
		st.appendString("FOREIGN DATA WRAPPER ")
	case ast.ObjectType_OBJECT_FUNCTION:
		st.appendString("FUNCTION ")
	case ast.ObjectType_OBJECT_ROLE:
		st.appendString("ROLE ")
	case ast.ObjectType_OBJECT_LANGUAGE:
		st.appendString("LANGUAGE ")
	case ast.ObjectType_OBJECT_OPCLASS:
		st.appendString("OPERATOR CLASS ")
	case ast.ObjectType_OBJECT_OPFAMILY:
		st.appendString("OPERATOR FAMILY ")
	case ast.ObjectType_OBJECT_POLICY:
		st.appendString("POLICY ")
	case ast.ObjectType_OBJECT_PROCEDURE:
		st.appendString("PROCEDURE ")
	case ast.ObjectType_OBJECT_PUBLICATION:
		st.appendString("PUBLICATION ")
	case ast.ObjectType_OBJECT_ROUTINE:
		st.appendString("ROUTINE ")
	case ast.ObjectType_OBJECT_SCHEMA:
		st.appendString("SCHEMA ")
	case ast.ObjectType_OBJECT_FOREIGN_SERVER:
		st.appendString("SERVER ")
	case ast.ObjectType_OBJECT_SUBSCRIPTION:
		st.appendString("SUBSCRIPTION ")
	case ast.ObjectType_OBJECT_TABLE,
		ast.ObjectType_OBJECT_TABCONSTRAINT:
		st.appendString("TABLE ")
	case ast.ObjectType_OBJECT_COLUMN:
		switch rename_stmt.RelationType {
		case ast.ObjectType_OBJECT_TABLE:
			st.appendString("TABLE ")
		case ast.ObjectType_OBJECT_FOREIGN_TABLE:
			st.appendString("FOREIGN TABLE ")
		case ast.ObjectType_OBJECT_VIEW:
			st.appendString("VIEW ")
		case ast.ObjectType_OBJECT_MATVIEW:
			st.appendString("MATERIALIZED VIEW ")
		default:
		}
	case ast.ObjectType_OBJECT_SEQUENCE:
		st.appendString("SEQUENCE ")
	case ast.ObjectType_OBJECT_VIEW:
		st.appendString("VIEW ")
	case ast.ObjectType_OBJECT_MATVIEW:
		st.appendString("MATERIALIZED VIEW ")
	case ast.ObjectType_OBJECT_INDEX:
		st.appendString("INDEX ")
	case ast.ObjectType_OBJECT_FOREIGN_TABLE:
		st.appendString("FOREIGN TABLE ")
	case ast.ObjectType_OBJECT_RULE:
		st.appendString("RULE ")
	case ast.ObjectType_OBJECT_TRIGGER:
		st.appendString("TRIGGER ")
	case ast.ObjectType_OBJECT_EVENT_TRIGGER:
		st.appendString("EVENT TRIGGER ")
	case ast.ObjectType_OBJECT_TABLESPACE:
		st.appendString("TABLESPACE ")
	case ast.ObjectType_OBJECT_STATISTIC_EXT:
		st.appendString("STATISTICS ")
	case ast.ObjectType_OBJECT_TSPARSER:
		st.appendString("TEXT SEARCH PARSER ")
	case ast.ObjectType_OBJECT_TSDICTIONARY:
		st.appendString("TEXT SEARCH DICTIONARY ")
	case ast.ObjectType_OBJECT_TSTEMPLATE:
		st.appendString("TEXT SEARCH TEMPLATE ")
	case ast.ObjectType_OBJECT_TSCONFIGURATION:
		st.appendString("TEXT SEARCH CONFIGURATION ")
	case ast.ObjectType_OBJECT_TYPE,
		ast.ObjectType_OBJECT_ATTRIBUTE:
		st.appendString("TYPE ")
	default:
	}

	if rename_stmt.MissingOk {
		st.appendString("IF EXISTS ")
	}

	switch rename_stmt.RenameType {
	case ast.ObjectType_OBJECT_AGGREGATE:
		deparseAggregateWithArgtypes(st, rename_stmt.Object.GetObjectWithArgs())
		st.appendString(" RENAME ")
	case ast.ObjectType_OBJECT_DOMCONSTRAINT:
		deparseAnyName(st, rename_stmt.Object.GetList().Items)
		st.appendString(" RENAME CONSTRAINT ")
		st.appendString(quoteIdentifier(rename_stmt.Subname))
		st.appendChar(' ')
	case ast.ObjectType_OBJECT_OPCLASS,
		ast.ObjectType_OBJECT_OPFAMILY:
		l = rename_stmt.Object.GetList().Items
		deparseAnyNameSkipFirst(st, l)
		st.appendString(" USING ")
		st.appendString(quoteIdentifier(strVal(l[0])))
		st.appendString(" RENAME ")
	case ast.ObjectType_OBJECT_POLICY:
		st.appendString(quoteIdentifier(rename_stmt.Subname))
		st.appendString(" ON ")
		deparseRangeVar(st, rename_stmt.Relation, contextNone)
		st.appendString(" RENAME ")
	case ast.ObjectType_OBJECT_FUNCTION,
		ast.ObjectType_OBJECT_PROCEDURE,
		ast.ObjectType_OBJECT_ROUTINE:
		deparseFunctionWithArgtypes(st, rename_stmt.Object.GetObjectWithArgs())
		st.appendString(" RENAME ")
	case ast.ObjectType_OBJECT_SUBSCRIPTION:
		deparseColId(st, strVal(rename_stmt.Object))
		st.appendString(" RENAME ")
	case ast.ObjectType_OBJECT_TABLE,
		ast.ObjectType_OBJECT_SEQUENCE,
		ast.ObjectType_OBJECT_VIEW,
		ast.ObjectType_OBJECT_MATVIEW,
		ast.ObjectType_OBJECT_INDEX,
		ast.ObjectType_OBJECT_FOREIGN_TABLE:
		deparseRangeVar(st, rename_stmt.Relation, contextNone)
		st.appendString(" RENAME ")
	case ast.ObjectType_OBJECT_COLUMN:
		deparseRangeVar(st, rename_stmt.Relation, contextNone)
		st.appendString(" RENAME COLUMN ")
		st.appendString(quoteIdentifier(rename_stmt.Subname))
		st.appendChar(' ')
	case ast.ObjectType_OBJECT_TABCONSTRAINT:
		deparseRangeVar(st, rename_stmt.Relation, contextNone)
		st.appendString(" RENAME CONSTRAINT ")
		st.appendString(quoteIdentifier(rename_stmt.Subname))
		st.appendChar(' ')
	case ast.ObjectType_OBJECT_RULE,
		ast.ObjectType_OBJECT_TRIGGER:
		st.appendString(quoteIdentifier(rename_stmt.Subname))
		st.appendString(" ON ")
		deparseRangeVar(st, rename_stmt.Relation, contextNone)
		st.appendString(" RENAME ")
	case ast.ObjectType_OBJECT_FDW,
		ast.ObjectType_OBJECT_LANGUAGE,
		ast.ObjectType_OBJECT_PUBLICATION,
		ast.ObjectType_OBJECT_FOREIGN_SERVER,
		ast.ObjectType_OBJECT_EVENT_TRIGGER:
		st.appendString(quoteIdentifier(strVal(rename_stmt.Object)))
		st.appendString(" RENAME ")
	case ast.ObjectType_OBJECT_DATABASE,
		ast.ObjectType_OBJECT_ROLE,
		ast.ObjectType_OBJECT_SCHEMA,
		ast.ObjectType_OBJECT_TABLESPACE:
		st.appendString(quoteIdentifier(rename_stmt.Subname))
		st.appendString(" RENAME ")
	case ast.ObjectType_OBJECT_COLLATION,
		ast.ObjectType_OBJECT_CONVERSION,
		ast.ObjectType_OBJECT_DOMAIN,
		ast.ObjectType_OBJECT_STATISTIC_EXT,
		ast.ObjectType_OBJECT_TSPARSER,
		ast.ObjectType_OBJECT_TSDICTIONARY,
		ast.ObjectType_OBJECT_TSTEMPLATE,
		ast.ObjectType_OBJECT_TSCONFIGURATION,
		ast.ObjectType_OBJECT_TYPE:
		deparseAnyName(st, rename_stmt.Object.GetList().Items)
		st.appendString(" RENAME ")
	case ast.ObjectType_OBJECT_ATTRIBUTE:
		deparseRangeVar(st, rename_stmt.Relation, contextAlterType)
		st.appendString(" RENAME ATTRIBUTE ")
		st.appendString(quoteIdentifier(rename_stmt.Subname))
		st.appendChar(' ')
	default:
	}

	st.appendString("TO ")
	st.appendString(quoteIdentifier(rename_stmt.Newname))
	st.appendChar(' ')

	deparseOptDropBehavior(st, rename_stmt.Behavior)

	st.removeTrailingSpace()
}

func deparseTransactionStmt(st *state, transaction_stmt *ast.TransactionStmt) {
	switch transaction_stmt.Kind {
	case ast.TransactionStmtKind_TRANS_STMT_BEGIN:
		st.appendString("BEGIN ")
		deparseTransactionModeList(st, transaction_stmt.Options)
	case ast.TransactionStmtKind_TRANS_STMT_START:
		st.appendString("START TRANSACTION ")
		deparseTransactionModeList(st, transaction_stmt.Options)
	case ast.TransactionStmtKind_TRANS_STMT_COMMIT:
		st.appendString("COMMIT ")
		if transaction_stmt.Chain {
			st.appendString("AND CHAIN ")
		}
	case ast.TransactionStmtKind_TRANS_STMT_ROLLBACK:
		st.appendString("ROLLBACK ")
		if transaction_stmt.Chain {
			st.appendString("AND CHAIN ")
		}
	case ast.TransactionStmtKind_TRANS_STMT_SAVEPOINT:
		st.appendString("SAVEPOINT ")
		st.appendString(quoteIdentifier(transaction_stmt.SavepointName))
	case ast.TransactionStmtKind_TRANS_STMT_RELEASE:
		st.appendString("RELEASE ")
		st.appendString(quoteIdentifier(transaction_stmt.SavepointName))
	case ast.TransactionStmtKind_TRANS_STMT_ROLLBACK_TO:
		st.appendString("ROLLBACK ")
		st.appendString("TO SAVEPOINT ")
		st.appendString(quoteIdentifier(transaction_stmt.SavepointName))
	case ast.TransactionStmtKind_TRANS_STMT_PREPARE:
		st.appendString("PREPARE TRANSACTION ")
		deparseStringLiteral(st, transaction_stmt.Gid)
	case ast.TransactionStmtKind_TRANS_STMT_COMMIT_PREPARED:
		st.appendString("COMMIT PREPARED ")
		deparseStringLiteral(st, transaction_stmt.Gid)
	case ast.TransactionStmtKind_TRANS_STMT_ROLLBACK_PREPARED:
		st.appendString("ROLLBACK PREPARED ")
		deparseStringLiteral(st, transaction_stmt.Gid)
	}

	st.removeTrailingSpace()
}

func isSetTimeZoneInterval(stmt *ast.VariableSetStmt) bool {
	if !(stmt.Name == "timezone" &&
		len(stmt.Args) == 1 &&
		stmt.Args[0].GetTypeCast() != nil) {
		return false
	}

	typeName := stmt.Args[0].GetTypeCast().TypeName

	return len(typeName.Names) == 2 &&
		strVal(typeName.Names[0]) == "pg_catalog" &&
		strVal(typeName.Names[len(typeName.Names)-1]) == "interval"
}

func deparseVariableSetStmt(st *state, variable_set_stmt *ast.VariableSetStmt) {
	switch variable_set_stmt.Kind {
	case ast.VariableSetKind_VAR_SET_VALUE: /* SET var = value */
		st.appendString("SET ")
		if variable_set_stmt.IsLocal {
			st.appendString("LOCAL ")
		}
		if isSetTimeZoneInterval(variable_set_stmt) {
			st.appendString("TIME ZONE ")
			deparseVarList(st, variable_set_stmt.Args)
		} else {
			deparseVarName(st, variable_set_stmt.Name)
			st.appendString(" TO ")
			deparseVarList(st, variable_set_stmt.Args)
		}
	case ast.VariableSetKind_VAR_SET_DEFAULT: /* SET var TO DEFAULT */
		st.appendString("SET ")
		if variable_set_stmt.IsLocal {
			st.appendString("LOCAL ")
		}
		deparseVarName(st, variable_set_stmt.Name)
		st.appendString(" TO DEFAULT")
	case ast.VariableSetKind_VAR_SET_CURRENT: /* SET var FROM CURRENT */
		st.appendString("SET ")
		if variable_set_stmt.IsLocal {
			st.appendString("LOCAL ")
		}
		deparseVarName(st, variable_set_stmt.Name)
		st.appendString(" FROM CURRENT")
	case ast.VariableSetKind_VAR_SET_MULTI: /* special case for SET TRANSACTION ... */
		st.appendString("SET ")
		if variable_set_stmt.IsLocal {
			st.appendString("LOCAL ")
		}
		if variable_set_stmt.Name == "TRANSACTION" {
			st.appendString("TRANSACTION ")
			deparseTransactionModeList(st, variable_set_stmt.Args)
		} else if variable_set_stmt.Name == "SESSION CHARACTERISTICS" {
			st.appendString("SESSION CHARACTERISTICS AS TRANSACTION ")
			deparseTransactionModeList(st, variable_set_stmt.Args)
		} else if variable_set_stmt.Name == "TRANSACTION SNAPSHOT" {
			st.appendString("TRANSACTION SNAPSHOT ")
			deparseStringLiteral(st, strVal(aConstVal(variable_set_stmt.Args[0].GetAConst())))
		}
	case ast.VariableSetKind_VAR_RESET: /* RESET var */
		st.appendString("RESET ")
		deparseVarName(st, variable_set_stmt.Name)
	case ast.VariableSetKind_VAR_RESET_ALL: /* RESET ALL */
		st.appendString("RESET ALL")
	}
}

func deparseDropdbStmt(st *state, dropdb_stmt *ast.DropdbStmt) {
	st.appendString("DROP DATABASE ")
	if dropdb_stmt.MissingOk {
		st.appendString("IF EXISTS ")
	}

	st.appendString(quoteIdentifier(dropdb_stmt.Dbname))
	st.appendChar(' ')

	if len(dropdb_stmt.Options) > 0 {
		st.appendChar('(')
		for i, option := range dropdb_stmt.Options {
			def_elem := option.GetDefElem()
			if def_elem.Defname == "force" {
				st.appendString("FORCE")
			}

			if i < len(dropdb_stmt.Options)-1 {
				st.appendString(", ")
			}
		}
		st.appendChar(')')
	}

	st.removeTrailingSpace()
}

func deparseVacuumStmt(st *state, vacuum_stmt *ast.VacuumStmt) {
	if vacuum_stmt.IsVacuumcmd {
		st.appendString("VACUUM ")
	} else {
		st.appendString("ANALYZE ")
	}

	deparseUtilityOptionList(st, vacuum_stmt.Options)

	for i, item := range vacuum_stmt.Rels {
		rel := item.GetVacuumRelation()

		deparseRangeVar(st, rel.Relation, contextNone)
		if len(rel.VaCols) > 0 {
			st.appendChar('(')
			for j, col := range rel.VaCols {
				st.appendString(quoteIdentifier(strVal(col)))
				if j < len(rel.VaCols)-1 {
					st.appendString(", ")
				}
			}
			st.appendChar(')')
		}

		if i < len(vacuum_stmt.Rels)-1 {
			st.appendString(", ")
		}
	}

	st.removeTrailingSpace()
}

func deparseLoadStmt(st *state, load_stmt *ast.LoadStmt) {
	st.appendString("LOAD ")
	deparseStringLiteral(st, load_stmt.Filename)
}

// Lock mode constants (lockdefs.h).
const (
	accessShareLock          = 1
	rowShareLock             = 2
	rowExclusiveLock         = 3
	shareUpdateExclusiveLock = 4
	shareLock                = 5
	shareRowExclusiveLock    = 6
	exclusiveLock            = 7
	accessExclusiveLock      = 8
)

func deparseLockStmt(st *state, lock_stmt *ast.LockStmt) {
	st.appendString("LOCK TABLE ")

	deparseRelationExprList(st, lock_stmt.Relations)
	st.appendChar(' ')

	if lock_stmt.Mode != accessExclusiveLock {
		st.appendString("IN ")
		switch lock_stmt.Mode {
		case accessShareLock:
			st.appendString("ACCESS SHARE ")
		case rowShareLock:
			st.appendString("ROW SHARE ")
		case rowExclusiveLock:
			st.appendString("ROW EXCLUSIVE ")
		case shareUpdateExclusiveLock:
			st.appendString("SHARE UPDATE EXCLUSIVE ")
		case shareLock:
			st.appendString("SHARE ")
		case shareRowExclusiveLock:
			st.appendString("SHARE ROW EXCLUSIVE ")
		case exclusiveLock:
			st.appendString("EXCLUSIVE ")
		case accessExclusiveLock:
			st.appendString("ACCESS EXCLUSIVE ")
		default:
		}
		st.appendString("MODE ")
	}

	if lock_stmt.Nowait {
		st.appendString("NOWAIT ")
	}

	st.removeTrailingSpace()
}

func deparseConstraintsSetStmt(st *state, constraints_set_stmt *ast.ConstraintsSetStmt) {
	st.appendString("SET CONSTRAINTS ")

	if len(constraints_set_stmt.Constraints) > 0 {
		deparseQualifiedNameList(st, constraints_set_stmt.Constraints)
		st.appendChar(' ')
	} else {
		st.appendString("ALL ")
	}

	if constraints_set_stmt.Deferred {
		st.appendString("DEFERRED")
	} else {
		st.appendString("IMMEDIATE")
	}
}

func deparseExplainStmt(st *state, explain_stmt *ast.ExplainStmt) {
	st.appendPartGroup("EXPLAIN", partNoIndent)

	deparseUtilityOptionList(st, explain_stmt.Options)

	deparseExplainableStmt(st, explain_stmt.Query)
}

func deparseCopyStmt(st *state, copy_stmt *ast.CopyStmt) {
	st.appendPartGroup("COPY", partIndent)

	if copy_stmt.Relation != nil {
		deparseRangeVar(st, copy_stmt.Relation, contextNone)
		if len(copy_stmt.Attlist) > 0 {
			st.appendChar('(')
			deparseColumnList(st, copy_stmt.Attlist)
			st.appendChar(')')
		}
		st.appendChar(' ')
	}

	if copy_stmt.Query != nil {
		st.appendChar('(')
		deparsePreparableStmt(st, copy_stmt.Query)
		st.appendString(") ")
	}

	if copy_stmt.IsFrom {
		st.appendString("FROM ")
	} else {
		st.appendString("TO ")
	}

	if copy_stmt.IsProgram {
		st.appendString("PROGRAM ")
	}

	if copy_stmt.Filename != "" {
		deparseStringLiteral(st, copy_stmt.Filename)
		st.appendChar(' ')
	} else {
		if copy_stmt.IsFrom {
			st.appendString("STDIN ")
		} else {
			st.appendString("STDOUT ")
		}
	}

	if len(copy_stmt.Options) > 0 {
		// In some cases, equivalent expressions may have slightly different parse trees for `COPY`
		// statements. For example the following two statements result in different (but equivalent) parse
		// trees:
		//
		//   - COPY foo FROM STDIN CSV FREEZE
		//   - COPY foo FROM STDIN WITH (FORMAT CSV, FREEZE)
		//
		// In order to make sure we deparse to the "correct" version, we always try to deparse to the older
		// compact syntax first.
		//
		// The old syntax can be seen here in the Postgres 8.4 Reference:
		//     https://www.postgresql.org/docs/8.4/sql-copy.html

		old_fmt := true

		// Loop over the options to see if any require the new `WITH (...)` syntax.
		for _, option := range copy_stmt.Options {
			def_elem := option.GetDefElem()

			if def_elem.Defname == "freeze" && optBooleanValue(def_elem.Arg) {
			} else if def_elem.Defname == "header" && def_elem.Arg != nil && optBooleanValue(def_elem.Arg) {
			} else if def_elem.Defname == "format" && strVal(def_elem.Arg) == "csv" {
			} else if def_elem.Defname == "force_quote" && def_elem.Arg != nil && def_elem.Arg.GetList() != nil {
			} else {
				old_fmt = false
				break
			}
		}

		// Branch to differing output modes, depending on if we can use the old syntax.
		if old_fmt {
			for _, option := range copy_stmt.Options {
				def_elem := option.GetDefElem()

				if def_elem.Defname == "freeze" && optBooleanValue(def_elem.Arg) {
					st.appendString("FREEZE ")
				} else if def_elem.Defname == "header" && def_elem.Arg != nil && optBooleanValue(def_elem.Arg) {
					st.appendString("HEADER ")
				} else if def_elem.Defname == "format" && strVal(def_elem.Arg) == "csv" {
					st.appendString("CSV ")
				} else if def_elem.Defname == "force_quote" && def_elem.Arg != nil && def_elem.Arg.GetList() != nil {
					st.appendString("FORCE QUOTE ")
					deparseColumnList(st, def_elem.Arg.GetList().Items)
				} else {
					// This isn't reachable, the conditions here are exactly the same as the first loop above.
				}
			}
		} else {
			st.appendString("WITH (")
			for i, option := range copy_stmt.Options {
				def_elem := option.GetDefElem()

				if def_elem.Defname == "format" {
					st.appendString("FORMAT ")

					format := strVal(def_elem.Arg)
					if format == "binary" {
						st.appendString("BINARY")
					} else if format == "csv" {
						st.appendString("CSV")
					} else if format == "text" {
						st.appendString("TEXT")
					}
				} else if def_elem.Defname == "freeze" {
					st.appendString("FREEZE")
					deparseOptBoolean(st, def_elem.Arg)
				} else if def_elem.Defname == "delimiter" {
					st.appendString("DELIMITER ")
					deparseStringLiteral(st, strVal(def_elem.Arg))
				} else if def_elem.Defname == "null" {
					st.appendString("NULL ")
					deparseStringLiteral(st, strVal(def_elem.Arg))
				} else if def_elem.Defname == "header" {
					st.appendString("HEADER")
					deparseOptBoolean(st, def_elem.Arg)
				} else if def_elem.Defname == "quote" {
					st.appendString("QUOTE ")
					deparseStringLiteral(st, strVal(def_elem.Arg))
				} else if def_elem.Defname == "escape" {
					st.appendString("ESCAPE ")
					deparseStringLiteral(st, strVal(def_elem.Arg))
				} else if def_elem.Defname == "force_quote" {
					st.appendString("FORCE_QUOTE ")
					if def_elem.Arg.GetAStar() != nil {
						st.appendChar('*')
					} else if def_elem.Arg.GetList() != nil {
						st.appendChar('(')
						deparseColumnList(st, def_elem.Arg.GetList().Items)
						st.appendChar(')')
					}
				} else if def_elem.Defname == "force_not_null" {
					st.appendString("FORCE_NOT_NULL ")

					if def_elem.Arg.GetAStar() != nil {
						deparseAStar(st, def_elem.Arg.GetAStar())
					} else {
						st.appendChar('(')
						deparseColumnList(st, def_elem.Arg.GetList().Items)
						st.appendChar(')')
					}
				} else if def_elem.Defname == "force_null" {
					st.appendString("FORCE_NULL ")

					if def_elem.Arg.GetAStar() != nil {
						deparseAStar(st, def_elem.Arg.GetAStar())
					} else {
						st.appendChar('(')
						deparseColumnList(st, def_elem.Arg.GetList().Items)
						st.appendChar(')')
					}
				} else if def_elem.Defname == "encoding" {
					st.appendString("ENCODING ")
					deparseStringLiteral(st, strVal(def_elem.Arg))
				} else {
					st.appendString(quoteIdentifier(def_elem.Defname))
					if def_elem.Arg != nil {
						st.appendChar(' ')
					}

					if def_elem.Arg == nil {
						// Nothing
					} else if def_elem.Arg.GetString_() != nil {
						deparseOptBooleanOrString(st, strVal(def_elem.Arg))
					} else if def_elem.Arg.GetInteger() != nil || def_elem.Arg.GetFloat() != nil {
						deparseNumericOnly(st, def_elem.Arg)
					} else if def_elem.Arg.GetAStar() != nil {
						deparseAStar(st, def_elem.Arg.GetAStar())
					} else if def_elem.Arg.GetList() != nil {
						l := def_elem.Arg.GetList().Items
						st.appendChar('(')
						for j, item := range l {
							deparseOptBooleanOrString(st, strVal(item))
							if j < len(l)-1 {
								st.appendString(", ")
							}
						}
						st.appendChar(')')
					}
				}

				if i < len(copy_stmt.Options)-1 {
					st.appendString(", ")
				}
			}
			st.appendString(") ")
		}
	}

	deparseWhereClause(st, copy_stmt.WhereClause)

	st.removeTrailingSpace()
}
