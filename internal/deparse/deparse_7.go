// Ported from postgres_deparse.c (libpg_query 18.0.0).
// ALTER statement long tail, publications/subscriptions, triggers, JSON aggregates.
package deparse

import (
	"github.com/sqlc-dev/oliphant/ast"
)

// pg_trigger.h: TRIGGER_TYPE_* bits used by CreateTrigStmt.
const (
	triggerTypeBefore   = 1 << 1 // TRIGGER_TYPE_BEFORE
	triggerTypeInsert   = 1 << 2 // TRIGGER_TYPE_INSERT
	triggerTypeDelete   = 1 << 3 // TRIGGER_TYPE_DELETE
	triggerTypeUpdate   = 1 << 4 // TRIGGER_TYPE_UPDATE
	triggerTypeTruncate = 1 << 5 // TRIGGER_TYPE_TRUNCATE
	triggerTypeInstead  = 1 << 6 // TRIGGER_TYPE_INSTEAD
	triggerTypeAfter    = 0      // TRIGGER_TYPE_AFTER
)

// xml.h: XmlStandaloneType.
const (
	xmlStandaloneYes     = 0 // XML_STANDALONE_YES
	xmlStandaloneNo      = 1 // XML_STANDALONE_NO
	xmlStandaloneNoValue = 2 // XML_STANDALONE_NO_VALUE
)

func deparseAlterPolicyStmt(st *state, alter_policy_stmt *ast.AlterPolicyStmt) {
	st.appendString("ALTER POLICY ")
	st.appendString(quoteIdentifier(alter_policy_stmt.PolicyName))
	st.appendString(" ON ")
	deparseRangeVar(st, alter_policy_stmt.Table, contextNone)
	st.appendChar(' ')

	if len(alter_policy_stmt.Roles) > 0 {
		st.appendString("TO ")
		deparseRoleList(st, alter_policy_stmt.Roles)
		st.appendChar(' ')
	}

	if alter_policy_stmt.Qual != nil {
		st.appendString("USING (")
		deparseExpr(st, alter_policy_stmt.Qual, contextAExpr)
		st.appendString(") ")
	}

	if alter_policy_stmt.WithCheck != nil {
		st.appendString("WITH CHECK (")
		deparseExpr(st, alter_policy_stmt.WithCheck, contextAExpr)
		st.appendString(") ")
	}
}

func deparseCreateTableSpaceStmt(st *state, create_table_space_stmt *ast.CreateTableSpaceStmt) {
	st.appendString("CREATE TABLESPACE ")
	deparseColId(st, create_table_space_stmt.Tablespacename)
	st.appendChar(' ')

	if create_table_space_stmt.Owner != nil {
		st.appendString("OWNER ")
		deparseRoleSpec(st, create_table_space_stmt.Owner)
		st.appendChar(' ')
	}

	st.appendString("LOCATION ")

	if create_table_space_stmt.Location != "" {
		deparseStringLiteral(st, create_table_space_stmt.Location)
	} else {
		st.appendString("''")
	}

	st.appendChar(' ')

	deparseOptWith(st, create_table_space_stmt.Options)

	st.removeTrailingSpace()
}

func deparseCreateTransformStmt(st *state, create_transform_stmt *ast.CreateTransformStmt) {
	st.appendString("CREATE ")
	if create_transform_stmt.Replace {
		st.appendString("OR REPLACE ")
	}

	st.appendString("TRANSFORM FOR ")
	deparseTypeName(st, create_transform_stmt.TypeName)
	st.appendChar(' ')

	st.appendString("LANGUAGE ")
	st.appendString(quoteIdentifier(create_transform_stmt.Lang))
	st.appendChar(' ')

	st.appendChar('(')

	if create_transform_stmt.Fromsql != nil {
		st.appendString("FROM SQL WITH FUNCTION ")
		deparseFunctionWithArgtypes(st, create_transform_stmt.Fromsql)
	}

	if create_transform_stmt.Fromsql != nil && create_transform_stmt.Tosql != nil {
		st.appendString(", ")
	}

	if create_transform_stmt.Tosql != nil {
		st.appendString("TO SQL WITH FUNCTION ")
		deparseFunctionWithArgtypes(st, create_transform_stmt.Tosql)
	}

	st.appendChar(')')
}

func deparseCreateAmStmt(st *state, create_am_stmt *ast.CreateAmStmt) {
	st.appendString("CREATE ACCESS METHOD ")
	st.appendString(quoteIdentifier(create_am_stmt.Amname))
	st.appendChar(' ')

	st.appendString("TYPE ")
	switch create_am_stmt.Amtype {
	case "i": // AMTYPE_INDEX
		st.appendString("INDEX ")
	case "t": // AMTYPE_TABLE
		st.appendString("TABLE ")
	}

	st.appendString("HANDLER ")
	deparseHandlerName(st, create_am_stmt.HandlerName)
}

// "pub_obj_list" in gram.y
func deparsePublicationObjectList(st *state, pubobjects []*ast.Node) {
	for i, item := range pubobjects {
		obj := item.GetPublicationObjSpec()

		switch obj.Pubobjtype {
		case ast.PublicationObjSpecType_PUBLICATIONOBJ_TABLE:
			st.appendString("TABLE ")
			deparseRangeVar(st, obj.Pubtable.Relation, contextNone)

			if len(obj.Pubtable.Columns) > 0 {
				st.appendChar('(')
				deparseColumnList(st, obj.Pubtable.Columns)
				st.appendChar(')')
			}

			if obj.Pubtable.WhereClause != nil {
				st.appendString(" WHERE (")
				deparseExpr(st, obj.Pubtable.WhereClause, contextAExpr)
				st.appendString(")")
			}
		case ast.PublicationObjSpecType_PUBLICATIONOBJ_TABLES_IN_SCHEMA:
			st.appendString("TABLES IN SCHEMA ")
			st.appendString(quoteIdentifier(obj.Name))
		case ast.PublicationObjSpecType_PUBLICATIONOBJ_TABLES_IN_CUR_SCHEMA:
			st.appendString("TABLES IN SCHEMA CURRENT_SCHEMA")
		case ast.PublicationObjSpecType_PUBLICATIONOBJ_CONTINUATION:
			// This should be unreachable, the parser merges these before we can even get here.
		}

		if i < len(pubobjects)-1 {
			st.appendString(", ")
		}
	}
}

func deparseCreatePublicationStmt(st *state, create_publication_stmt *ast.CreatePublicationStmt) {
	st.appendString("CREATE PUBLICATION ")
	st.appendString(quoteIdentifier(create_publication_stmt.Pubname))
	st.appendChar(' ')

	if len(create_publication_stmt.Pubobjects) > 0 {
		st.appendString("FOR ")
		deparsePublicationObjectList(st, create_publication_stmt.Pubobjects)
		st.appendChar(' ')
	} else if create_publication_stmt.ForAllTables {
		st.appendString("FOR ALL TABLES ")
	}

	deparseOptDefinition(st, create_publication_stmt.Options)
	st.removeTrailingSpace()
}

func deparseAlterPublicationStmt(st *state, alter_publication_stmt *ast.AlterPublicationStmt) {
	st.appendString("ALTER PUBLICATION ")
	deparseColId(st, alter_publication_stmt.Pubname)
	st.appendChar(' ')

	if len(alter_publication_stmt.Pubobjects) > 0 {
		switch alter_publication_stmt.Action {
		case ast.AlterPublicationAction_AP_SetObjects:
			st.appendString("SET ")
		case ast.AlterPublicationAction_AP_AddObjects:
			st.appendString("ADD ")
		case ast.AlterPublicationAction_AP_DropObjects:
			st.appendString("DROP ")
		}

		deparsePublicationObjectList(st, alter_publication_stmt.Pubobjects)
	} else if len(alter_publication_stmt.Options) > 0 {
		st.appendString("SET ")
		deparseDefinition(st, alter_publication_stmt.Options)
	}
}

func deparseAlterSeqStmt(st *state, alter_seq_stmt *ast.AlterSeqStmt) {
	st.appendString("ALTER SEQUENCE ")

	if alter_seq_stmt.MissingOk {
		st.appendString("IF EXISTS ")
	}

	deparseRangeVar(st, alter_seq_stmt.Sequence, contextNone)
	st.appendChar(' ')

	deparseSeqOptList(st, alter_seq_stmt.Options)

	st.removeTrailingSpace()
}

func deparseAlterSystemStmt(st *state, alter_system_stmt *ast.AlterSystemStmt) {
	st.appendString("ALTER SYSTEM ")
	deparseVariableSetStmt(st, alter_system_stmt.Setstmt)
}

func deparseCommentStmt(st *state, comment_stmt *ast.CommentStmt) {
	var l []*ast.Node

	st.appendString("COMMENT ON ")

	switch comment_stmt.Objtype {
	case ast.ObjectType_OBJECT_COLUMN:
		st.appendString("COLUMN ")
	case ast.ObjectType_OBJECT_INDEX:
		st.appendString("INDEX ")
	case ast.ObjectType_OBJECT_SEQUENCE:
		st.appendString("SEQUENCE ")
	case ast.ObjectType_OBJECT_STATISTIC_EXT:
		st.appendString("STATISTICS ")
	case ast.ObjectType_OBJECT_TABLE:
		st.appendString("TABLE ")
	case ast.ObjectType_OBJECT_VIEW:
		st.appendString("VIEW ")
	case ast.ObjectType_OBJECT_MATVIEW:
		st.appendString("MATERIALIZED VIEW ")
	case ast.ObjectType_OBJECT_COLLATION:
		st.appendString("COLLATION ")
	case ast.ObjectType_OBJECT_CONVERSION:
		st.appendString("CONVERSION ")
	case ast.ObjectType_OBJECT_FOREIGN_TABLE:
		st.appendString("FOREIGN TABLE ")
	case ast.ObjectType_OBJECT_TSCONFIGURATION:
		st.appendString("TEXT SEARCH CONFIGURATION ")
	case ast.ObjectType_OBJECT_TSDICTIONARY:
		st.appendString("TEXT SEARCH DICTIONARY ")
	case ast.ObjectType_OBJECT_TSPARSER:
		st.appendString("TEXT SEARCH PARSER ")
	case ast.ObjectType_OBJECT_TSTEMPLATE:
		st.appendString("TEXT SEARCH TEMPLATE ")
	case ast.ObjectType_OBJECT_ACCESS_METHOD:
		st.appendString("ACCESS METHOD ")
	case ast.ObjectType_OBJECT_DATABASE:
		st.appendString("DATABASE ")
	case ast.ObjectType_OBJECT_EVENT_TRIGGER:
		st.appendString("EVENT TRIGGER ")
	case ast.ObjectType_OBJECT_EXTENSION:
		st.appendString("EXTENSION ")
	case ast.ObjectType_OBJECT_FDW:
		st.appendString("FOREIGN DATA WRAPPER ")
	case ast.ObjectType_OBJECT_LANGUAGE:
		st.appendString("LANGUAGE ")
	case ast.ObjectType_OBJECT_PUBLICATION:
		st.appendString("PUBLICATION ")
	case ast.ObjectType_OBJECT_ROLE:
		st.appendString("ROLE ")
	case ast.ObjectType_OBJECT_SCHEMA:
		st.appendString("SCHEMA ")
	case ast.ObjectType_OBJECT_FOREIGN_SERVER:
		st.appendString("SERVER ")
	case ast.ObjectType_OBJECT_SUBSCRIPTION:
		st.appendString("SUBSCRIPTION ")
	case ast.ObjectType_OBJECT_TABLESPACE:
		st.appendString("TABLESPACE ")
	case ast.ObjectType_OBJECT_TYPE:
		st.appendString("TYPE ")
	case ast.ObjectType_OBJECT_DOMAIN:
		st.appendString("DOMAIN ")
	case ast.ObjectType_OBJECT_AGGREGATE:
		st.appendString("AGGREGATE ")
	case ast.ObjectType_OBJECT_FUNCTION:
		st.appendString("FUNCTION ")
	case ast.ObjectType_OBJECT_OPERATOR:
		st.appendString("OPERATOR ")
	case ast.ObjectType_OBJECT_TABCONSTRAINT:
		st.appendString("CONSTRAINT ")
	case ast.ObjectType_OBJECT_DOMCONSTRAINT:
		st.appendString("CONSTRAINT ")
	case ast.ObjectType_OBJECT_POLICY:
		st.appendString("POLICY ")
	case ast.ObjectType_OBJECT_PROCEDURE:
		st.appendString("PROCEDURE ")
	case ast.ObjectType_OBJECT_ROUTINE:
		st.appendString("ROUTINE ")
	case ast.ObjectType_OBJECT_RULE:
		st.appendString("RULE ")
	case ast.ObjectType_OBJECT_TRANSFORM:
		st.appendString("TRANSFORM ")
	case ast.ObjectType_OBJECT_TRIGGER:
		st.appendString("TRIGGER ")
	case ast.ObjectType_OBJECT_OPCLASS:
		st.appendString("OPERATOR CLASS ")
	case ast.ObjectType_OBJECT_OPFAMILY:
		st.appendString("OPERATOR FAMILY ")
	case ast.ObjectType_OBJECT_LARGEOBJECT:
		st.appendString("LARGE OBJECT ")
	case ast.ObjectType_OBJECT_CAST:
		st.appendString("CAST ")
	default:
		// No other cases are supported in the parser
	}

	switch comment_stmt.Objtype {
	case ast.ObjectType_OBJECT_COLUMN,
		ast.ObjectType_OBJECT_INDEX,
		ast.ObjectType_OBJECT_SEQUENCE,
		ast.ObjectType_OBJECT_STATISTIC_EXT,
		ast.ObjectType_OBJECT_TABLE,
		ast.ObjectType_OBJECT_VIEW,
		ast.ObjectType_OBJECT_MATVIEW,
		ast.ObjectType_OBJECT_COLLATION,
		ast.ObjectType_OBJECT_CONVERSION,
		ast.ObjectType_OBJECT_FOREIGN_TABLE,
		ast.ObjectType_OBJECT_TSCONFIGURATION,
		ast.ObjectType_OBJECT_TSDICTIONARY,
		ast.ObjectType_OBJECT_TSPARSER,
		ast.ObjectType_OBJECT_TSTEMPLATE:
		deparseAnyName(st, comment_stmt.Object.GetList().Items)
	case ast.ObjectType_OBJECT_ACCESS_METHOD,
		ast.ObjectType_OBJECT_DATABASE,
		ast.ObjectType_OBJECT_EVENT_TRIGGER,
		ast.ObjectType_OBJECT_EXTENSION,
		ast.ObjectType_OBJECT_FDW,
		ast.ObjectType_OBJECT_LANGUAGE,
		ast.ObjectType_OBJECT_PUBLICATION,
		ast.ObjectType_OBJECT_ROLE,
		ast.ObjectType_OBJECT_SCHEMA,
		ast.ObjectType_OBJECT_FOREIGN_SERVER,
		ast.ObjectType_OBJECT_SUBSCRIPTION,
		ast.ObjectType_OBJECT_TABLESPACE:
		st.appendString(quoteIdentifier(strVal(comment_stmt.Object)))
	case ast.ObjectType_OBJECT_TYPE,
		ast.ObjectType_OBJECT_DOMAIN:
		deparseTypeName(st, comment_stmt.Object.GetTypeName())
	case ast.ObjectType_OBJECT_AGGREGATE:
		deparseAggregateWithArgtypes(st, comment_stmt.Object.GetObjectWithArgs())
	case ast.ObjectType_OBJECT_FUNCTION,
		ast.ObjectType_OBJECT_PROCEDURE,
		ast.ObjectType_OBJECT_ROUTINE:
		deparseFunctionWithArgtypes(st, comment_stmt.Object.GetObjectWithArgs())
	case ast.ObjectType_OBJECT_OPERATOR:
		deparseOperatorWithArgtypes(st, comment_stmt.Object.GetObjectWithArgs())
	case ast.ObjectType_OBJECT_TABCONSTRAINT,
		ast.ObjectType_OBJECT_POLICY,
		ast.ObjectType_OBJECT_RULE,
		ast.ObjectType_OBJECT_TRIGGER:
		l = comment_stmt.Object.GetList().Items
		st.appendString(quoteIdentifier(strVal(l[len(l)-1])))
		st.appendString(" ON ")
		deparseAnyNameSkipLast(st, l)
	case ast.ObjectType_OBJECT_DOMCONSTRAINT:
		l = comment_stmt.Object.GetList().Items
		st.appendString(quoteIdentifier(strVal(l[len(l)-1])))
		st.appendString(" ON DOMAIN ")
		deparseTypeName(st, l[0].GetTypeName())
	case ast.ObjectType_OBJECT_TRANSFORM:
		l = comment_stmt.Object.GetList().Items
		st.appendString("FOR ")
		deparseTypeName(st, l[0].GetTypeName())
		st.appendString(" LANGUAGE ")
		st.appendString(quoteIdentifier(strVal(l[1])))
	case ast.ObjectType_OBJECT_OPCLASS,
		ast.ObjectType_OBJECT_OPFAMILY:
		l = comment_stmt.Object.GetList().Items
		deparseAnyNameSkipFirst(st, l)
		st.appendString(" USING ")
		st.appendString(quoteIdentifier(strVal(l[0])))
	case ast.ObjectType_OBJECT_LARGEOBJECT:
		deparseValue(st, comment_stmt.Object, contextNone)
	case ast.ObjectType_OBJECT_CAST:
		l = comment_stmt.Object.GetList().Items
		st.appendChar('(')
		deparseTypeName(st, l[0].GetTypeName())
		st.appendString(" AS ")
		deparseTypeName(st, l[1].GetTypeName())
		st.appendChar(')')
	default:
		// No other cases are supported in the parser
	}

	st.appendString(" IS ")

	if comment_stmt.Comment != "" || st.empties[comment_stmt] {
		deparseStringLiteral(st, comment_stmt.Comment)
	} else {
		st.appendString("NULL")
	}
}

func deparseStatsElem(st *state, stats_elem *ast.StatsElem) {
	// only one of stats_elem->name or stats_elem->expr can be non-null
	if stats_elem.Name != "" {
		st.appendString(stats_elem.Name)
	} else if stats_elem.Expr != nil {
		st.appendChar('(')
		deparseExpr(st, stats_elem.Expr, contextAExpr)
		st.appendChar(')')
	}
}

func deparseCreateStatsStmt(st *state, create_stats_stmt *ast.CreateStatsStmt) {
	st.appendString("CREATE STATISTICS ")

	if create_stats_stmt.IfNotExists {
		st.appendString("IF NOT EXISTS ")
	}

	deparseAnyName(st, create_stats_stmt.Defnames)
	st.appendChar(' ')

	if len(create_stats_stmt.StatTypes) > 0 {
		st.appendChar('(')
		deparseNameList(st, create_stats_stmt.StatTypes)
		st.appendString(") ")
	}

	st.appendString("ON ")
	for i, item := range create_stats_stmt.Exprs {
		deparseStatsElem(st, item.GetStatsElem())
		if i < len(create_stats_stmt.Exprs)-1 {
			st.appendString(", ")
		}
	}

	st.appendString(" FROM ")
	deparseFromList(st, create_stats_stmt.Relations)
}

func deparseAlterCollationStmt(st *state, alter_collation_stmt *ast.AlterCollationStmt) {
	st.appendString("ALTER COLLATION ")
	deparseAnyName(st, alter_collation_stmt.Collname)
	st.appendString(" REFRESH VERSION")
}

func deparseAlterDatabaseStmt(st *state, alter_database_stmt *ast.AlterDatabaseStmt) {
	st.appendString("ALTER DATABASE ")
	deparseColId(st, alter_database_stmt.Dbname)
	st.appendChar(' ')
	deparseCreatedbOptList(st, alter_database_stmt.Options)
	st.removeTrailingSpace()
}

func deparseAlterDatabaseSetStmt(st *state, alter_database_set_stmt *ast.AlterDatabaseSetStmt) {
	st.appendString("ALTER DATABASE ")
	deparseColId(st, alter_database_set_stmt.Dbname)
	st.appendChar(' ')
	deparseVariableSetStmt(st, alter_database_set_stmt.Setstmt)
}

func deparseAlterStatsStmt(st *state, alter_stats_stmt *ast.AlterStatsStmt) {
	st.appendString("ALTER STATISTICS ")

	if alter_stats_stmt.MissingOk {
		st.appendString("IF EXISTS ")
	}

	deparseAnyName(st, alter_stats_stmt.Defnames)
	st.appendChar(' ')

	if alter_stats_stmt.Stxstattarget != nil {
		st.appendf("SET STATISTICS %d", alter_stats_stmt.Stxstattarget.GetInteger().Ival)
	} else {
		st.appendf("SET STATISTICS DEFAULT")
	}
}

func deparseAlterTSDictionaryStmt(st *state, alter_ts_dictionary_stmt *ast.AlterTSDictionaryStmt) {
	st.appendString("ALTER TEXT SEARCH DICTIONARY ")

	deparseAnyName(st, alter_ts_dictionary_stmt.Dictname)
	st.appendChar(' ')

	deparseDefinition(st, alter_ts_dictionary_stmt.Options)
}

func deparseAlterTSConfigurationStmt(st *state, alter_ts_configuration_stmt *ast.AlterTSConfigurationStmt) {
	st.appendString("ALTER TEXT SEARCH CONFIGURATION ")
	deparseAnyName(st, alter_ts_configuration_stmt.Cfgname)
	st.appendChar(' ')

	switch alter_ts_configuration_stmt.Kind {
	case ast.AlterTSConfigType_ALTER_TSCONFIG_ADD_MAPPING:
		st.appendString("ADD MAPPING FOR ")
		deparseNameList(st, alter_ts_configuration_stmt.Tokentype)
		st.appendString(" WITH ")
		deparseAnyNameList(st, alter_ts_configuration_stmt.Dicts)
	case ast.AlterTSConfigType_ALTER_TSCONFIG_ALTER_MAPPING_FOR_TOKEN:
		st.appendString("ALTER MAPPING FOR ")
		deparseNameList(st, alter_ts_configuration_stmt.Tokentype)
		st.appendString(" WITH ")
		deparseAnyNameList(st, alter_ts_configuration_stmt.Dicts)
	case ast.AlterTSConfigType_ALTER_TSCONFIG_REPLACE_DICT:
		st.appendString("ALTER MAPPING REPLACE ")
		deparseAnyName(st, alter_ts_configuration_stmt.Dicts[0].GetList().Items)
		st.appendString(" WITH ")
		deparseAnyName(st, alter_ts_configuration_stmt.Dicts[1].GetList().Items)
	case ast.AlterTSConfigType_ALTER_TSCONFIG_REPLACE_DICT_FOR_TOKEN:
		st.appendString("ALTER MAPPING FOR ")
		deparseNameList(st, alter_ts_configuration_stmt.Tokentype)
		st.appendString(" REPLACE ")
		deparseAnyName(st, alter_ts_configuration_stmt.Dicts[0].GetList().Items)
		st.appendString(" WITH ")
		deparseAnyName(st, alter_ts_configuration_stmt.Dicts[1].GetList().Items)
	case ast.AlterTSConfigType_ALTER_TSCONFIG_DROP_MAPPING:
		st.appendString("DROP MAPPING ")
		if alter_ts_configuration_stmt.MissingOk {
			st.appendString("IF EXISTS ")
		}
		st.appendString("FOR ")
		deparseNameList(st, alter_ts_configuration_stmt.Tokentype)
	}
}

func deparseVariableShowStmt(st *state, variable_show_stmt *ast.VariableShowStmt) {
	st.appendString("SHOW ")

	if variable_show_stmt.Name == "timezone" {
		st.appendString("TIME ZONE")
	} else if variable_show_stmt.Name == "transaction_isolation" {
		st.appendString("TRANSACTION ISOLATION LEVEL")
	} else if variable_show_stmt.Name == "session_authorization" {
		st.appendString("SESSION AUTHORIZATION")
	} else if variable_show_stmt.Name == "all" {
		st.appendString("ALL")
	} else {
		st.appendString(quoteIdentifier(variable_show_stmt.Name))
	}
}

func deparseRangeTableSample(st *state, range_table_sample *ast.RangeTableSample) {
	deparseRangeVar(st, range_table_sample.Relation.GetRangeVar(), contextNone)

	st.appendString(" TABLESAMPLE ")

	deparseFuncName(st, range_table_sample.Method)
	st.appendChar('(')
	deparseExprList(st, range_table_sample.Args)
	st.appendString(") ")

	if range_table_sample.Repeatable != nil {
		st.appendString("REPEATABLE (")
		deparseExpr(st, range_table_sample.Repeatable, contextAExpr)
		st.appendString(") ")
	}

	st.removeTrailingSpace()
}

func deparseCreateSubscriptionStmt(st *state, create_subscription_stmt *ast.CreateSubscriptionStmt) {
	st.appendString("CREATE SUBSCRIPTION ")
	st.appendString(quoteIdentifier(create_subscription_stmt.Subname))

	st.appendString(" CONNECTION ")
	if create_subscription_stmt.Conninfo != "" {
		deparseStringLiteral(st, create_subscription_stmt.Conninfo)
	} else {
		st.appendString("''")
	}

	st.appendString(" PUBLICATION ")

	for i, item := range create_subscription_stmt.Publication {
		deparseColLabel(st, strVal(item))
		if i < len(create_subscription_stmt.Publication)-1 {
			st.appendString(", ")
		}
	}
	st.appendChar(' ')

	deparseOptDefinition(st, create_subscription_stmt.Options)
	st.removeTrailingSpace()
}

func deparseAlterSubscriptionStmt(st *state, alter_subscription_stmt *ast.AlterSubscriptionStmt) {
	st.appendString("ALTER SUBSCRIPTION ")
	st.appendString(quoteIdentifier(alter_subscription_stmt.Subname))
	st.appendChar(' ')

	switch alter_subscription_stmt.Kind {
	case ast.AlterSubscriptionType_ALTER_SUBSCRIPTION_OPTIONS:
		st.appendString("SET ")
		deparseDefinition(st, alter_subscription_stmt.Options)
	case ast.AlterSubscriptionType_ALTER_SUBSCRIPTION_SKIP:
		st.appendString("SKIP ")
		deparseDefinition(st, alter_subscription_stmt.Options)
	case ast.AlterSubscriptionType_ALTER_SUBSCRIPTION_CONNECTION:
		st.appendString("CONNECTION ")
		deparseStringLiteral(st, alter_subscription_stmt.Conninfo)
		st.appendChar(' ')
	case ast.AlterSubscriptionType_ALTER_SUBSCRIPTION_REFRESH:
		st.appendString("REFRESH PUBLICATION ")
		deparseOptDefinition(st, alter_subscription_stmt.Options)
	case ast.AlterSubscriptionType_ALTER_SUBSCRIPTION_ADD_PUBLICATION:
		st.appendString("ADD PUBLICATION ")
		for i, item := range alter_subscription_stmt.Publication {
			deparseColLabel(st, strVal(item))
			if i < len(alter_subscription_stmt.Publication)-1 {
				st.appendString(", ")
			}
		}
		st.appendChar(' ')
		deparseOptDefinition(st, alter_subscription_stmt.Options)
	case ast.AlterSubscriptionType_ALTER_SUBSCRIPTION_DROP_PUBLICATION:
		st.appendString("DROP PUBLICATION ")
		for i, item := range alter_subscription_stmt.Publication {
			deparseColLabel(st, strVal(item))
			if i < len(alter_subscription_stmt.Publication)-1 {
				st.appendString(", ")
			}
		}
		st.appendChar(' ')
		deparseOptDefinition(st, alter_subscription_stmt.Options)
	case ast.AlterSubscriptionType_ALTER_SUBSCRIPTION_SET_PUBLICATION:
		st.appendString("SET PUBLICATION ")
		for i, item := range alter_subscription_stmt.Publication {
			deparseColLabel(st, strVal(item))
			if i < len(alter_subscription_stmt.Publication)-1 {
				st.appendString(", ")
			}
		}
		st.appendChar(' ')
		deparseOptDefinition(st, alter_subscription_stmt.Options)
	case ast.AlterSubscriptionType_ALTER_SUBSCRIPTION_ENABLED:
		defelem := alter_subscription_stmt.Options[0].GetDefElem()
		if optBooleanValue(defelem.Arg) {
			st.appendString(" ENABLE ")
		} else {
			st.appendString(" DISABLE ")
		}
	}

	st.removeTrailingSpace()
}

func deparseDropSubscriptionStmt(st *state, drop_subscription_stmt *ast.DropSubscriptionStmt) {
	st.appendString("DROP SUBSCRIPTION ")

	if drop_subscription_stmt.MissingOk {
		st.appendString("IF EXISTS ")
	}

	st.appendString(drop_subscription_stmt.Subname)
}

func deparseCallStmt(st *state, call_stmt *ast.CallStmt) {
	st.appendString("CALL ")
	deparseFuncCall(st, call_stmt.Funccall, contextNone)
}

func deparseAlterOwnerStmt(st *state, alter_owner_stmt *ast.AlterOwnerStmt) {
	var l []*ast.Node

	st.appendString("ALTER ")

	switch alter_owner_stmt.ObjectType {
	case ast.ObjectType_OBJECT_AGGREGATE:
		st.appendString("AGGREGATE ")
		deparseAggregateWithArgtypes(st, alter_owner_stmt.Object.GetObjectWithArgs())
	case ast.ObjectType_OBJECT_COLLATION:
		st.appendString("COLLATION ")
		deparseAnyName(st, alter_owner_stmt.Object.GetList().Items)
	case ast.ObjectType_OBJECT_CONVERSION:
		st.appendString("CONVERSION ")
		deparseAnyName(st, alter_owner_stmt.Object.GetList().Items)
	case ast.ObjectType_OBJECT_DATABASE:
		st.appendString("DATABASE ")
		deparseColId(st, strVal(alter_owner_stmt.Object))
	case ast.ObjectType_OBJECT_DOMAIN:
		st.appendString("DOMAIN ")
		deparseAnyName(st, alter_owner_stmt.Object.GetList().Items)
	case ast.ObjectType_OBJECT_FUNCTION:
		st.appendString("FUNCTION ")
		deparseFunctionWithArgtypes(st, alter_owner_stmt.Object.GetObjectWithArgs())
	case ast.ObjectType_OBJECT_LANGUAGE:
		st.appendString("LANGUAGE ")
		deparseColId(st, strVal(alter_owner_stmt.Object))
	case ast.ObjectType_OBJECT_LARGEOBJECT:
		st.appendString("LARGE OBJECT ")
		deparseNumericOnly(st, alter_owner_stmt.Object)
	case ast.ObjectType_OBJECT_OPERATOR:
		st.appendString("OPERATOR ")
		deparseOperatorWithArgtypes(st, alter_owner_stmt.Object.GetObjectWithArgs())
	case ast.ObjectType_OBJECT_OPCLASS:
		l = alter_owner_stmt.Object.GetList().Items
		st.appendString("OPERATOR CLASS ")
		deparseAnyNameSkipFirst(st, l)
		st.appendString(" USING ")
		deparseColId(st, strVal(l[0]))
	case ast.ObjectType_OBJECT_OPFAMILY:
		l = alter_owner_stmt.Object.GetList().Items
		st.appendString("OPERATOR FAMILY ")
		deparseAnyNameSkipFirst(st, l)
		st.appendString(" USING ")
		deparseColId(st, strVal(l[0]))
	case ast.ObjectType_OBJECT_PROCEDURE:
		st.appendString("PROCEDURE ")
		deparseFunctionWithArgtypes(st, alter_owner_stmt.Object.GetObjectWithArgs())
	case ast.ObjectType_OBJECT_ROUTINE:
		st.appendString("ROUTINE ")
		deparseFunctionWithArgtypes(st, alter_owner_stmt.Object.GetObjectWithArgs())
	case ast.ObjectType_OBJECT_SCHEMA:
		st.appendString("SCHEMA ")
		deparseColId(st, strVal(alter_owner_stmt.Object))
	case ast.ObjectType_OBJECT_TYPE:
		st.appendString("TYPE ")
		deparseAnyName(st, alter_owner_stmt.Object.GetList().Items)
	case ast.ObjectType_OBJECT_TABLESPACE:
		st.appendString("TABLESPACE ")
		deparseColId(st, strVal(alter_owner_stmt.Object))
	case ast.ObjectType_OBJECT_STATISTIC_EXT:
		st.appendString("STATISTICS ")
		deparseAnyName(st, alter_owner_stmt.Object.GetList().Items)
	case ast.ObjectType_OBJECT_TSDICTIONARY:
		st.appendString("TEXT SEARCH DICTIONARY ")
		deparseAnyName(st, alter_owner_stmt.Object.GetList().Items)
	case ast.ObjectType_OBJECT_TSCONFIGURATION:
		st.appendString("TEXT SEARCH CONFIGURATION ")
		deparseAnyName(st, alter_owner_stmt.Object.GetList().Items)
	case ast.ObjectType_OBJECT_FDW:
		st.appendString("FOREIGN DATA WRAPPER ")
		deparseColId(st, strVal(alter_owner_stmt.Object))
	case ast.ObjectType_OBJECT_FOREIGN_SERVER:
		st.appendString("SERVER ")
		deparseColId(st, strVal(alter_owner_stmt.Object))
	case ast.ObjectType_OBJECT_EVENT_TRIGGER:
		st.appendString("EVENT TRIGGER ")
		deparseColId(st, strVal(alter_owner_stmt.Object))
	case ast.ObjectType_OBJECT_PUBLICATION:
		st.appendString("PUBLICATION ")
		deparseColId(st, strVal(alter_owner_stmt.Object))
	case ast.ObjectType_OBJECT_SUBSCRIPTION:
		st.appendString("SUBSCRIPTION ")
		deparseColId(st, strVal(alter_owner_stmt.Object))
	}

	st.appendString(" OWNER TO ")
	deparseRoleSpec(st, alter_owner_stmt.Newowner)
}

func deparseOperatorDefList(st *state, defs []*ast.Node) {
	for i, item := range defs {
		def_elem := item.GetDefElem()
		st.appendString(quoteIdentifier(def_elem.Defname))
		st.appendString(" = ")
		if def_elem.Arg != nil {
			deparseDefArg(st, def_elem.Arg, true)
		} else {
			st.appendString("NONE")
		}

		if i < len(defs)-1 {
			st.appendString(", ")
		}
	}
}

func deparseAlterOperatorStmt(st *state, alter_operator_stmt *ast.AlterOperatorStmt) {
	st.appendString("ALTER OPERATOR ")
	deparseOperatorWithArgtypes(st, alter_operator_stmt.Opername)
	st.appendString(" SET (")
	deparseOperatorDefList(st, alter_operator_stmt.Options)
	st.appendChar(')')
}

func deparseAlterTypeStmt(st *state, alter_type_stmt *ast.AlterTypeStmt) {
	st.appendString("ALTER TYPE ")
	deparseAnyName(st, alter_type_stmt.TypeName)
	st.appendString(" SET (")
	deparseOperatorDefList(st, alter_type_stmt.Options)
	st.appendChar(')')
}

func deparseDropOwnedStmt(st *state, drop_owned_stmt *ast.DropOwnedStmt) {
	st.appendString("DROP OWNED BY ")
	deparseRoleList(st, drop_owned_stmt.Roles)
	st.appendChar(' ')
	deparseOptDropBehavior(st, drop_owned_stmt.Behavior)
	st.removeTrailingSpace()
}

func deparseReassignOwnedStmt(st *state, reassigned_owned_stmt *ast.ReassignOwnedStmt) {
	st.appendString("REASSIGN OWNED BY ")

	deparseRoleList(st, reassigned_owned_stmt.Roles)
	st.appendChar(' ')

	st.appendString("TO ")
	deparseRoleSpec(st, reassigned_owned_stmt.Newrole)
}

func deparseClosePortalStmt(st *state, close_portal_stmt *ast.ClosePortalStmt) {
	st.appendString("CLOSE ")
	if close_portal_stmt.Portalname != "" {
		st.appendString(quoteIdentifier(close_portal_stmt.Portalname))
	} else {
		st.appendString("ALL")
	}
}

func deparseCreateTrigStmt(st *state, create_trig_stmt *ast.CreateTrigStmt) {
	skip_events_or := true

	st.appendString("CREATE ")
	if create_trig_stmt.Replace {
		st.appendString("OR REPLACE ")
	}
	if create_trig_stmt.Isconstraint {
		st.appendString("CONSTRAINT ")
	}
	st.appendString("TRIGGER ")

	st.appendString(quoteIdentifier(create_trig_stmt.Trigname))
	st.appendChar(' ')

	switch create_trig_stmt.Timing {
	case triggerTypeBefore:
		st.appendString("BEFORE ")
	case triggerTypeAfter:
		st.appendString("AFTER ")
	case triggerTypeInstead:
		st.appendString("INSTEAD OF ")
	}

	if create_trig_stmt.Events&triggerTypeInsert != 0 {
		st.appendString("INSERT ")
		skip_events_or = false
	}
	if create_trig_stmt.Events&triggerTypeDelete != 0 {
		if !skip_events_or {
			st.appendString("OR ")
		}
		st.appendString("DELETE ")
		skip_events_or = false
	}
	if create_trig_stmt.Events&triggerTypeUpdate != 0 {
		if !skip_events_or {
			st.appendString("OR ")
		}
		st.appendString("UPDATE ")
		if len(create_trig_stmt.Columns) > 0 {
			st.appendString("OF ")
			deparseColumnList(st, create_trig_stmt.Columns)
			st.appendChar(' ')
		}
		skip_events_or = false
	}
	if create_trig_stmt.Events&triggerTypeTruncate != 0 {
		if !skip_events_or {
			st.appendString("OR ")
		}
		st.appendString("TRUNCATE ")
	}

	st.appendString("ON ")
	deparseRangeVar(st, create_trig_stmt.Relation, contextNone)
	st.appendChar(' ')

	if len(create_trig_stmt.TransitionRels) > 0 {
		st.appendString("REFERENCING ")
		for _, item := range create_trig_stmt.TransitionRels {
			deparseTriggerTransition(st, item.GetTriggerTransition())
			st.appendChar(' ')
		}
	}

	if create_trig_stmt.Constrrel != nil {
		st.appendString("FROM ")
		deparseRangeVar(st, create_trig_stmt.Constrrel, contextNone)
		st.appendChar(' ')
	}

	if create_trig_stmt.Deferrable {
		st.appendString("DEFERRABLE ")
	}

	if create_trig_stmt.Initdeferred {
		st.appendString("INITIALLY DEFERRED ")
	}

	if create_trig_stmt.Row {
		st.appendString("FOR EACH ROW ")
	}

	if create_trig_stmt.WhenClause != nil {
		st.appendString("WHEN (")
		deparseExpr(st, create_trig_stmt.WhenClause, contextAExpr)
		st.appendString(") ")
	}

	st.appendString("EXECUTE FUNCTION ")
	deparseFuncName(st, create_trig_stmt.Funcname)
	st.appendChar('(')
	for i, item := range create_trig_stmt.Args {
		deparseStringLiteral(st, strVal(item))
		if i < len(create_trig_stmt.Args)-1 {
			st.appendString(", ")
		}
	}
	st.appendChar(')')
}

func deparseTriggerTransition(st *state, trigger_transition *ast.TriggerTransition) {
	if trigger_transition.IsNew {
		st.appendString("NEW ")
	} else {
		st.appendString("OLD ")
	}

	if trigger_transition.IsTable {
		st.appendString("TABLE ")
	} else {
		st.appendString("ROW ")
	}

	st.appendString(quoteIdentifier(trigger_transition.Name))
}

func deparseXmlExpr(st *state, xml_expr *ast.XmlExpr, ctx nodeContext) {
	switch xml_expr.Op {
	case ast.XmlExprOp_IS_XMLCONCAT: /* XMLCONCAT(args) */
		st.appendString("xmlconcat(")
		deparseExprList(st, xml_expr.Args)
		st.appendChar(')')
	case ast.XmlExprOp_IS_XMLELEMENT: /* XMLELEMENT(name, xml_attributes, args) */
		st.appendString("xmlelement(name ")
		st.appendString(quoteIdentifier(xml_expr.Name))
		if len(xml_expr.NamedArgs) > 0 {
			st.appendString(", xmlattributes(")
			deparseXmlAttributeList(st, xml_expr.NamedArgs)
			st.appendString(")")
		}
		if len(xml_expr.Args) > 0 {
			st.appendString(", ")
			deparseExprList(st, xml_expr.Args)
		}
		st.appendString(")")
	case ast.XmlExprOp_IS_XMLFOREST: /* XMLFOREST(xml_attributes) */
		st.appendString("xmlforest(")
		deparseXmlAttributeList(st, xml_expr.NamedArgs)
		st.appendChar(')')
	case ast.XmlExprOp_IS_XMLPARSE: /* XMLPARSE(text, is_doc, preserve_ws) */
		st.appendString("xmlparse(")
		switch xml_expr.Xmloption {
		case ast.XmlOptionType_XMLOPTION_DOCUMENT:
			st.appendString("document ")
		case ast.XmlOptionType_XMLOPTION_CONTENT:
			st.appendString("content ")
		}
		deparseExpr(st, xml_expr.Args[0], contextAExpr)
		st.appendChar(')')
	case ast.XmlExprOp_IS_XMLPI: /* XMLPI(name [, args]) */
		st.appendString("xmlpi(name ")
		st.appendString(quoteIdentifier(xml_expr.Name))
		if len(xml_expr.Args) > 0 {
			st.appendString(", ")
			deparseExpr(st, xml_expr.Args[0], contextAExpr)
		}
		st.appendChar(')')
	case ast.XmlExprOp_IS_XMLROOT: /* XMLROOT(xml, version, standalone) */
		st.appendString("xmlroot(")
		deparseExpr(st, xml_expr.Args[0], contextAExpr)
		st.appendString(", version ")
		if xml_expr.Args[1].GetAConst().Isnull {
			st.appendString("no value")
		} else {
			deparseExpr(st, xml_expr.Args[1], contextAExpr)
		}
		if intVal(aConstVal(xml_expr.Args[2].GetAConst())) == xmlStandaloneYes {
			st.appendString(", standalone yes")
		} else if intVal(aConstVal(xml_expr.Args[2].GetAConst())) == xmlStandaloneNo {
			st.appendString(", standalone no")
		} else if intVal(aConstVal(xml_expr.Args[2].GetAConst())) == xmlStandaloneNoValue {
			st.appendString(", standalone no value")
		}
		st.appendChar(')')
	case ast.XmlExprOp_IS_XMLSERIALIZE: /* XMLSERIALIZE(is_document, xmlval) */
		// These are represented as XmlSerialize in raw parse trees
	case ast.XmlExprOp_IS_DOCUMENT: /* xmlval IS DOCUMENT */
		deparseExpr(st, xml_expr.Args[0], ctx)
		st.appendString(" IS DOCUMENT")
	}
}

func deparseRangeTableFuncCol(st *state, range_table_func_col *ast.RangeTableFuncCol) {
	st.appendString(quoteIdentifier(range_table_func_col.Colname))
	st.appendChar(' ')

	if range_table_func_col.ForOrdinality {
		st.appendString("FOR ORDINALITY ")
	} else {
		deparseTypeName(st, range_table_func_col.TypeName)
		st.appendChar(' ')

		if range_table_func_col.Colexpr != nil {
			st.appendString("PATH ")
			deparseExpr(st, range_table_func_col.Colexpr, contextNone /* b_expr */)
			st.appendChar(' ')
		}

		if range_table_func_col.Coldefexpr != nil {
			st.appendString("DEFAULT ")
			deparseExpr(st, range_table_func_col.Coldefexpr, contextNone /* b_expr */)
			st.appendChar(' ')
		}

		if range_table_func_col.IsNotNull {
			st.appendString("NOT NULL ")
		}
	}

	st.removeTrailingSpace()
}

func deparseRangeTableFunc(st *state, range_table_func *ast.RangeTableFunc) {
	if range_table_func.Lateral {
		st.appendString("LATERAL ")
	}

	st.appendString("xmltable(")
	if len(range_table_func.Namespaces) > 0 {
		st.appendString("xmlnamespaces(")
		deparseXmlNamespaceList(st, range_table_func.Namespaces)
		st.appendString("), ")
	}

	st.appendChar('(')
	deparseExpr(st, range_table_func.Rowexpr, contextNone /* c_expr */)
	st.appendChar(')')

	st.appendString(" PASSING ")
	deparseExpr(st, range_table_func.Docexpr, contextNone /* c_expr */)

	st.appendString(" COLUMNS ")
	for i, item := range range_table_func.Columns {
		deparseRangeTableFuncCol(st, item.GetRangeTableFuncCol())
		if i < len(range_table_func.Columns)-1 {
			st.appendString(", ")
		}
	}

	st.appendString(") ")

	if range_table_func.Alias != nil {
		st.appendString("AS ")
		deparseAlias(st, range_table_func.Alias)
	}

	st.removeTrailingSpace()
}

func deparseXmlSerialize(st *state, xml_serialize *ast.XmlSerialize) {
	st.appendString("xmlserialize(")
	switch xml_serialize.Xmloption {
	case ast.XmlOptionType_XMLOPTION_DOCUMENT:
		st.appendString("document ")
	case ast.XmlOptionType_XMLOPTION_CONTENT:
		st.appendString("content ")
	}
	deparseExpr(st, xml_serialize.Expr, contextAExpr)
	st.appendString(" AS ")
	deparseTypeName(st, xml_serialize.TypeName)

	if xml_serialize.Indent {
		st.appendString(" INDENT")
	}

	st.appendString(")")
}

func deparseJsonFormat(st *state, json_format *ast.JsonFormat) {
	if json_format == nil || json_format.FormatType == ast.JsonFormatType_JS_FORMAT_DEFAULT {
		return
	}

	st.appendString("FORMAT JSON ")

	switch json_format.Encoding {
	case ast.JsonEncoding_JS_ENC_UTF8:
		st.appendString("ENCODING utf8 ")
	case ast.JsonEncoding_JS_ENC_UTF16:
		st.appendString("ENCODING utf16 ")
	case ast.JsonEncoding_JS_ENC_UTF32:
		st.appendString("ENCODING utf32 ")
	case ast.JsonEncoding_JS_ENC_DEFAULT:
		// no encoding specified
	}
}

func deparseJsonIsPredicate(st *state, j *ast.JsonIsPredicate) {
	deparseExpr(st, j.Expr, contextAExpr)
	st.appendChar(' ')

	deparseJsonFormat(st, j.Format)

	st.appendString("IS ")

	switch j.ItemType {
	case ast.JsonValueType_JS_TYPE_ANY:
		st.appendString("JSON ")
	case ast.JsonValueType_JS_TYPE_ARRAY:
		st.appendString("JSON ARRAY ")
	case ast.JsonValueType_JS_TYPE_OBJECT:
		st.appendString("JSON OBJECT ")
	case ast.JsonValueType_JS_TYPE_SCALAR:
		st.appendString("JSON SCALAR ")
	}

	if j.UniqueKeys {
		st.appendString("WITH UNIQUE ")
	}

	st.removeTrailingSpace()
}

func deparseJsonValueExpr(st *state, json_value_expr *ast.JsonValueExpr) {
	deparseExpr(st, json_value_expr.RawExpr, contextAExpr)
	st.appendChar(' ')
	deparseJsonFormat(st, json_value_expr.Format)
}

func deparseJsonValueExprList(st *state, exprs []*ast.Node) {
	for i, item := range exprs {
		deparseJsonValueExpr(st, item.GetJsonValueExpr())
		st.removeTrailingSpace()
		if i < len(exprs)-1 {
			st.appendString(", ")
		}
	}
	st.appendChar(' ')
}

func deparseJsonKeyValue(st *state, json_key_value *ast.JsonKeyValue) {
	deparseExpr(st, json_key_value.Key, contextAExpr)
	st.appendString(": ")
	deparseJsonValueExpr(st, json_key_value.Value)
}

func deparseJsonKeyValueList(st *state, exprs []*ast.Node) {
	for i, item := range exprs {
		deparseJsonKeyValue(st, item.GetJsonKeyValue())
		st.removeTrailingSpace()
		if i < len(exprs)-1 {
			st.appendString(", ")
		}
	}
	st.appendChar(' ')
}

func deparseJsonOutput(st *state, json_output *ast.JsonOutput) {
	if json_output == nil {
		return
	}

	st.appendString("RETURNING ")
	deparseTypeName(st, json_output.TypeName)
	st.appendChar(' ')
	deparseJsonFormat(st, json_output.Returning.Format)
}

func deparseJsonObjectAgg(st *state, json_object_agg *ast.JsonObjectAgg) {
	st.appendString("JSON_OBJECTAGG(")
	deparseJsonKeyValue(st, json_object_agg.Arg)

	if json_object_agg.AbsentOnNull {
		st.appendString("ABSENT ON NULL ")
	}

	if json_object_agg.Unique {
		st.appendString("WITH UNIQUE ")
	}

	deparseJsonOutput(st, json_object_agg.Constructor.Output)

	st.removeTrailingSpace()
	st.appendString(") ")

	if json_object_agg.Constructor.AggFilter != nil {
		st.appendString("FILTER (WHERE ")
		deparseExpr(st, json_object_agg.Constructor.AggFilter, contextAExpr)
		st.appendString(") ")
	}

	if json_object_agg.Constructor.Over != nil {
		over := json_object_agg.Constructor.Over
		st.appendString("OVER ")
		if over.Name != "" {
			st.appendString(over.Name)
		} else {
			deparseWindowDef(st, over)
		}
	}

	st.removeTrailingSpace()
}

func deparseJsonArrayAgg(st *state, json_array_agg *ast.JsonArrayAgg) {
	st.appendString("JSON_ARRAYAGG(")
	deparseJsonValueExpr(st, json_array_agg.Arg)
	deparseOptSortClause(st, json_array_agg.Constructor.AggOrder, contextNone)

	if !json_array_agg.AbsentOnNull {
		st.appendString("NULL ON NULL ")
	}

	deparseJsonOutput(st, json_array_agg.Constructor.Output)

	st.removeTrailingSpace()
	st.appendString(") ")

	if json_array_agg.Constructor.AggFilter != nil {
		st.appendString("FILTER (WHERE ")
		deparseExpr(st, json_array_agg.Constructor.AggFilter, contextAExpr)
		st.appendString(") ")
	}

	if json_array_agg.Constructor.Over != nil {
		over := json_array_agg.Constructor.Over
		st.appendString("OVER ")
		if over.Name != "" {
			st.appendString(over.Name)
		} else {
			deparseWindowDef(st, over)
		}
	}

	st.removeTrailingSpace()
}
