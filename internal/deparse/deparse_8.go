// Ported from postgres_deparse.c (libpg_query 17-6.2.2).
// JSON constructors, JSON_TABLE, value nodes, statement dispatch.
package deparse

import (
	"github.com/sqlc-dev/oliphant/ast"
)

func deparseJsonObjectConstructor(st *state, json_object_constructor *ast.JsonObjectConstructor) {
	st.appendString("JSON_OBJECT(")
	deparseJsonKeyValueList(st, json_object_constructor.Exprs)

	if json_object_constructor.AbsentOnNull {
		st.appendString("ABSENT ON NULL ")
	}

	if json_object_constructor.Unique {
		st.appendString("WITH UNIQUE ")
	}

	deparseJsonOutput(st, json_object_constructor.Output)

	st.removeTrailingSpace()
	st.appendChar(')')
}

func deparseJsonArrayConstructor(st *state, json_array_constructor *ast.JsonArrayConstructor) {
	st.appendString("JSON_ARRAY(")
	deparseJsonValueExprList(st, json_array_constructor.Exprs)

	if !json_array_constructor.AbsentOnNull {
		st.appendString("NULL ON NULL ")
	}

	deparseJsonOutput(st, json_array_constructor.Output)

	st.removeTrailingSpace()
	st.appendChar(')')
}

func deparseJsonArrayQueryConstructor(st *state, json_array_query_constructor *ast.JsonArrayQueryConstructor) {
	st.appendString("JSON_ARRAY(")

	deparseSelectStmt(st, json_array_query_constructor.Query.GetSelectStmt(), contextNone)
	deparseJsonFormat(st, json_array_query_constructor.Format)
	deparseJsonOutput(st, json_array_query_constructor.Output)

	st.removeTrailingSpace()
	st.appendChar(')')
}

func deparseJsonParseExpr(st *state, json_parse_expr *ast.JsonParseExpr) {
	st.appendString("JSON(")

	deparseJsonValueExpr(st, json_parse_expr.Expr)

	if json_parse_expr.UniqueKeys {
		st.appendString(" WITH UNIQUE KEYS")
	}

	st.appendString(")")
}

func deparseJsonScalarExpr(st *state, json_scalar_expr *ast.JsonScalarExpr) {
	st.appendString("JSON_SCALAR(")
	deparseExpr(st, json_scalar_expr.Expr, contextAExpr)
	st.appendString(")")
}

func deparseJsonSerializeExpr(st *state, json_serialize_expr *ast.JsonSerializeExpr) {
	st.appendString("JSON_SERIALIZE(")

	deparseJsonValueExpr(st, json_serialize_expr.Expr)

	if json_serialize_expr.Output != nil {
		deparseJsonOutput(st, json_serialize_expr.Output)
	}

	st.appendString(")")
}

func deparseJsonQuotesClauseOpt(st *state, quotes ast.JsonQuotes) {
	switch quotes {
	case ast.JsonQuotes_JS_QUOTES_UNSPEC:
	case ast.JsonQuotes_JS_QUOTES_KEEP:
		st.appendString(" KEEP QUOTES")
	case ast.JsonQuotes_JS_QUOTES_OMIT:
		st.appendString(" OMIT QUOTES")
	}
}

func deparseJsonOnErrorClauseOpt(st *state, behavior *ast.JsonBehavior) {
	if behavior == nil {
		return
	}

	st.appendChar(' ')
	deparseJsonBehavior(st, behavior)
	st.appendString(" ON ERROR")
}

func deparseJsonOnEmptyClauseOpt(st *state, behavior *ast.JsonBehavior) {
	if behavior != nil {
		st.appendChar(' ')
		deparseJsonBehavior(st, behavior)
		st.appendString(" ON EMPTY")
	}
}

func deparseJsonFuncExpr(st *state, json_func_expr *ast.JsonFuncExpr) {
	switch json_func_expr.Op {
	case ast.JsonExprOp_JSON_EXISTS_OP:
		st.appendString("JSON_EXISTS(")
	case ast.JsonExprOp_JSON_QUERY_OP:
		st.appendString("JSON_QUERY(")
	case ast.JsonExprOp_JSON_VALUE_OP:
		st.appendString("JSON_VALUE(")
	case ast.JsonExprOp_JSON_TABLE_OP:
		st.appendString("JSON_TABLE(")
	}

	deparseJsonValueExpr(st, json_func_expr.ContextItem)
	st.appendString(", ")
	deparseExpr(st, json_func_expr.Pathspec, contextAExpr)

	if len(json_func_expr.Passing) > 0 {
		st.appendString(" PASSING ")
	}

	for i, item := range json_func_expr.Passing {
		json_argument := item.GetJsonArgument()
		deparseJsonValueExpr(st, json_argument.Val)
		st.appendString(" AS ")
		deparseColLabel(st, json_argument.Name)

		if i < len(json_func_expr.Passing)-1 {
			st.appendString(", ")
		}
	}

	if json_func_expr.Output != nil {
		st.appendChar(' ')
		deparseJsonOutput(st, json_func_expr.Output)
	}

	switch json_func_expr.Wrapper {
	case ast.JsonWrapper_JSW_UNSPEC:
	case ast.JsonWrapper_JSW_NONE:
		st.appendString(" WITHOUT WRAPPER")
	case ast.JsonWrapper_JSW_CONDITIONAL:
		st.appendString(" WITH CONDITIONAL WRAPPER")
	case ast.JsonWrapper_JSW_UNCONDITIONAL:
		st.appendString(" WITH UNCONDITIONAL WRAPPER")
	}

	deparseJsonQuotesClauseOpt(st, json_func_expr.Quotes)
	deparseJsonOnEmptyClauseOpt(st, json_func_expr.OnEmpty)
	deparseJsonOnErrorClauseOpt(st, json_func_expr.OnError)

	st.appendChar(')')
}

func deparseJsonTablePathSpec(st *state, json_table_path_spec *ast.JsonTablePathSpec) {
	deparseStringLiteral(st, json_table_path_spec.String_.GetAConst().GetSval().GetSval())

	if json_table_path_spec.Name != "" {
		st.appendString(" AS ")
		deparseColLabel(st, json_table_path_spec.Name)
	}
}

func deparseJsonBehavior(st *state, json_behavior *ast.JsonBehavior) {
	switch json_behavior.Btype {
	case ast.JsonBehaviorType_JSON_BEHAVIOR_NULL:
		st.appendString("NULL")
	case ast.JsonBehaviorType_JSON_BEHAVIOR_ERROR:
		st.appendString("ERROR")
	case ast.JsonBehaviorType_JSON_BEHAVIOR_EMPTY:
		st.appendString("EMPTY")
	case ast.JsonBehaviorType_JSON_BEHAVIOR_TRUE:
		st.appendString("TRUE")
	case ast.JsonBehaviorType_JSON_BEHAVIOR_FALSE:
		st.appendString("FALSE")
	case ast.JsonBehaviorType_JSON_BEHAVIOR_EMPTY_ARRAY:
		st.appendString("EMPTY ARRAY")
	case ast.JsonBehaviorType_JSON_BEHAVIOR_EMPTY_OBJECT:
		st.appendString("EMPTY OBJECT")
	case ast.JsonBehaviorType_JSON_BEHAVIOR_DEFAULT:
		st.appendString("DEFAULT ")
		deparseExpr(st, json_behavior.Expr, contextAExpr)
	case ast.JsonBehaviorType_JSON_BEHAVIOR_UNKNOWN:
		st.appendString("UNKNOWN")
	}
}

func deparseJsonTableColumn(st *state, json_table_column *ast.JsonTableColumn) {
	if json_table_column.Coltype == ast.JsonTableColumnType_JTC_NESTED {
		st.appendString("NESTED PATH ")
		deparseJsonTablePathSpec(st, json_table_column.Pathspec)
		deparseJsonTableColumns(st, json_table_column.Columns)
		return
	}

	deparseColLabel(st, json_table_column.Name)
	st.appendChar(' ')

	switch json_table_column.Coltype {
	case ast.JsonTableColumnType_JTC_FOR_ORDINALITY:
		st.appendString(" FOR ORDINALITY")
	case ast.JsonTableColumnType_JTC_EXISTS,
		ast.JsonTableColumnType_JTC_FORMATTED,
		ast.JsonTableColumnType_JTC_REGULAR:
		deparseTypeName(st, json_table_column.TypeName)

		if json_table_column.Coltype == ast.JsonTableColumnType_JTC_EXISTS {
			st.appendString(" EXISTS ")
		} else {
			st.appendChar(' ')
		}

		if json_table_column.Format != nil {
			deparseJsonFormat(st, json_table_column.Format)
		}

		if json_table_column.Pathspec != nil {
			st.appendString("PATH ")
			deparseJsonTablePathSpec(st, json_table_column.Pathspec)
		}
	case ast.JsonTableColumnType_JTC_NESTED:
	}

	switch json_table_column.Wrapper {
	case ast.JsonWrapper_JSW_UNSPEC:
	case ast.JsonWrapper_JSW_NONE:
		if json_table_column.Coltype == ast.JsonTableColumnType_JTC_REGULAR || json_table_column.Coltype == ast.JsonTableColumnType_JTC_FORMATTED {
			st.appendString(" WITHOUT WRAPPER")
		}
	case ast.JsonWrapper_JSW_CONDITIONAL:
		st.appendString(" WITH CONDITIONAL WRAPPER")
	case ast.JsonWrapper_JSW_UNCONDITIONAL:
		st.appendString(" WITH UNCONDITIONAL WRAPPER")
	}

	deparseJsonQuotesClauseOpt(st, json_table_column.Quotes)
	deparseJsonOnEmptyClauseOpt(st, json_table_column.OnEmpty)
	deparseJsonOnErrorClauseOpt(st, json_table_column.OnError)
}

func deparseJsonTableColumns(st *state, json_table_columns []*ast.Node) {
	st.appendString(" COLUMNS (")

	for i, item := range json_table_columns {
		deparseJsonTableColumn(st, item.GetJsonTableColumn())

		if i < len(json_table_columns)-1 {
			st.appendString(", ")
		}
	}

	st.appendChar(')')
}

func deparseJsonTable(st *state, json_table *ast.JsonTable) {
	st.appendString("JSON_TABLE(")

	deparseJsonValueExpr(st, json_table.ContextItem)
	st.appendString(", ")
	deparseJsonTablePathSpec(st, json_table.Pathspec)

	if len(json_table.Passing) > 0 {
		st.appendString(" PASSING ")
	}

	for i, item := range json_table.Passing {
		json_argument := item.GetJsonArgument()
		deparseJsonValueExpr(st, json_argument.Val)
		st.appendString(" AS ")
		deparseColLabel(st, json_argument.Name)

		if i < len(json_table.Passing)-1 {
			st.appendString(", ")
		}
	}

	deparseJsonTableColumns(st, json_table.Columns)

	if json_table.OnError != nil {
		deparseJsonBehavior(st, json_table.OnError)
		st.appendString(" ON ERROR")
	}

	st.appendChar(')')

	if json_table.Alias != nil {
		st.appendChar(' ')
		deparseAlias(st, json_table.Alias)
	}
}

func deparseGroupingFunc(st *state, grouping_func *ast.GroupingFunc) {
	st.appendString("GROUPING(")
	deparseExprList(st, grouping_func.Args)
	st.appendChar(')')
}

func deparseClusterStmt(st *state, cluster_stmt *ast.ClusterStmt) {
	st.appendString("CLUSTER ")

	deparseUtilityOptionList(st, cluster_stmt.Params)

	if cluster_stmt.Relation != nil {
		deparseRangeVar(st, cluster_stmt.Relation, contextNone)
		st.appendChar(' ')
	}

	if cluster_stmt.Indexname != "" {
		st.appendString("USING ")
		st.appendString(quoteIdentifier(cluster_stmt.Indexname))
		st.appendChar(' ')
	}

	st.removeTrailingSpace()
}

func deparseValue(st *state, value *ast.Node, ctx nodeContext) {
	if value == nil {
		st.appendString("NULL")
		return
	}

	switch {
	case value.GetInteger() != nil, value.GetFloat() != nil:
		deparseNumericOnly(st, value)
	case value.GetBoolean() != nil:
		if value.GetBoolean().GetBoolval() {
			st.appendString("true")
		} else {
			st.appendString("false")
		}
	case value.GetString_() != nil:
		if ctx == contextIdentifier {
			st.appendString(quoteIdentifier(value.GetString_().GetSval()))
		} else if ctx == contextConstant {
			deparseStringLiteral(st, value.GetString_().GetSval())
		} else {
			st.appendString(value.GetString_().GetSval())
		}
	case value.GetBitString() != nil:
		if len(value.GetBitString().GetBsval()) >= 1 && value.GetBitString().GetBsval()[0] == 'x' {
			st.appendChar('x')
			deparseStringLiteral(st, value.GetBitString().GetBsval()[1:])
		} else if len(value.GetBitString().GetBsval()) >= 1 && value.GetBitString().GetBsval()[0] == 'b' {
			st.appendChar('b')
			deparseStringLiteral(st, value.GetBitString().GetBsval()[1:])
		}
	default:
		st.errorf("deparse: unrecognized value node type: %s",
			nodeTag(value))
	}
}

func deparsePreparableStmt(st *state, node *ast.Node) {
	switch {
	case node.GetSelectStmt() != nil:
		deparseSelectStmt(st, node.GetSelectStmt(), contextNone)
	case node.GetInsertStmt() != nil:
		deparseInsertStmt(st, node.GetInsertStmt())
	case node.GetUpdateStmt() != nil:
		deparseUpdateStmt(st, node.GetUpdateStmt())
	case node.GetDeleteStmt() != nil:
		deparseDeleteStmt(st, node.GetDeleteStmt())
	case node.GetMergeStmt() != nil:
		deparseMergeStmt(st, node.GetMergeStmt())
	}
}

func deparseRuleActionStmt(st *state, node *ast.Node) {
	switch {
	case node.GetSelectStmt() != nil:
		deparseSelectStmt(st, node.GetSelectStmt(), contextNone)
	case node.GetInsertStmt() != nil:
		deparseInsertStmt(st, node.GetInsertStmt())
	case node.GetUpdateStmt() != nil:
		deparseUpdateStmt(st, node.GetUpdateStmt())
	case node.GetDeleteStmt() != nil:
		deparseDeleteStmt(st, node.GetDeleteStmt())
	case node.GetNotifyStmt() != nil:
		deparseNotifyStmt(st, node.GetNotifyStmt())
	}
}

func deparseExplainableStmt(st *state, node *ast.Node) {
	switch {
	case node.GetSelectStmt() != nil:
		deparseSelectStmt(st, node.GetSelectStmt(), contextNone)
	case node.GetInsertStmt() != nil:
		deparseInsertStmt(st, node.GetInsertStmt())
	case node.GetUpdateStmt() != nil:
		deparseUpdateStmt(st, node.GetUpdateStmt())
	case node.GetDeleteStmt() != nil:
		deparseDeleteStmt(st, node.GetDeleteStmt())
	case node.GetDeclareCursorStmt() != nil:
		deparseDeclareCursorStmt(st, node.GetDeclareCursorStmt())
	case node.GetCreateTableAsStmt() != nil:
		deparseCreateTableAsStmt(st, node.GetCreateTableAsStmt())
	case node.GetRefreshMatViewStmt() != nil:
		deparseRefreshMatViewStmt(st, node.GetRefreshMatViewStmt())
	case node.GetExecuteStmt() != nil:
		deparseExecuteStmt(st, node.GetExecuteStmt())
	case node.GetMergeStmt() != nil:
		deparseMergeStmt(st, node.GetMergeStmt())
	}
}

func deparseSchemaStmt(st *state, node *ast.Node) {
	switch {
	case node.GetCreateStmt() != nil:
		deparseCreateStmt(st, node.GetCreateStmt(), false)
	case node.GetIndexStmt() != nil:
		deparseIndexStmt(st, node.GetIndexStmt())
	case node.GetCreateSeqStmt() != nil:
		deparseCreateSeqStmt(st, node.GetCreateSeqStmt())
	case node.GetCreateTrigStmt() != nil:
		deparseCreateTrigStmt(st, node.GetCreateTrigStmt())
	case node.GetGrantStmt() != nil:
		deparseGrantStmt(st, node.GetGrantStmt())
	case node.GetViewStmt() != nil:
		deparseViewStmt(st, node.GetViewStmt())
	}
}

func deparseStmt(st *state, node *ast.Node) {
	var parent_level *nestingLevel

	// For statements that can be nested, push/pop is handled directly in the
	// respective deparse...Stmt methods
	skip_push_pop := node.GetSelectStmt() != nil ||
		node.GetInsertStmt() != nil ||
		node.GetUpdateStmt() != nil ||
		node.GetDeleteStmt() != nil ||
		node.GetMergeStmt() != nil

	if !skip_push_pop {
		parent_level = st.increaseNestingLevel()
	}

	// Note the following grammar names are missing in the list, because they
	// get mapped to other node types:
	//
	// - AlterForeignTableStmt (=> AlterTableStmt)
	// - AlterGroupStmt (=> AlterRoleStmt)
	// - AlterCompositeTypeStmt (=> AlterTableStmt)
	// - AnalyzeStmt (=> VacuumStmt)
	// - CreateGroupStmt (=> CreateRoleStmt)
	// - CreateMatViewStmt (=> CreateTableAsStmt)
	// - CreateUserStmt (=> CreateRoleStmt)
	// - DropCastStmt (=> DropStmt)
	// - DropOpClassStmt (=> DropStmt)
	// - DropOpFamilyStmt (=> DropStmt)
	// - DropPLangStmt (=> DropPLangStmt)
	// - DropTransformStmt (=> DropStmt)
	// - RemoveAggrStmt (=> DropStmt)
	// - RemoveFuncStmt (=> DropStmt)
	// - RemoveOperStmt (=> DropStmt)
	// - RevokeStmt (=> GrantStmt)
	// - RevokeRoleStmt (=> GrantRoleStmt)
	// - VariableResetStmt (=> VariableSetStmt)
	//
	// And the following grammar names error out in the parser:
	// - CreateAssertionStmt (not supported yet)
	switch {
	case node.GetAlterEventTrigStmt() != nil:
		deparseAlterEventTrigStmt(st, node.GetAlterEventTrigStmt())
	case node.GetAlterCollationStmt() != nil:
		deparseAlterCollationStmt(st, node.GetAlterCollationStmt())
	case node.GetAlterDatabaseStmt() != nil:
		deparseAlterDatabaseStmt(st, node.GetAlterDatabaseStmt())
	case node.GetAlterDatabaseSetStmt() != nil:
		deparseAlterDatabaseSetStmt(st, node.GetAlterDatabaseSetStmt())
	case node.GetAlterDefaultPrivilegesStmt() != nil:
		deparseAlterDefaultPrivilegesStmt(st, node.GetAlterDefaultPrivilegesStmt())
	case node.GetAlterDomainStmt() != nil:
		deparseAlterDomainStmt(st, node.GetAlterDomainStmt())
	case node.GetAlterEnumStmt() != nil:
		deparseAlterEnumStmt(st, node.GetAlterEnumStmt())
	case node.GetAlterExtensionStmt() != nil:
		deparseAlterExtensionStmt(st, node.GetAlterExtensionStmt())
	case node.GetAlterExtensionContentsStmt() != nil:
		deparseAlterExtensionContentsStmt(st, node.GetAlterExtensionContentsStmt())
	case node.GetAlterFdwStmt() != nil:
		deparseAlterFdwStmt(st, node.GetAlterFdwStmt())
	case node.GetAlterForeignServerStmt() != nil:
		deparseAlterForeignServerStmt(st, node.GetAlterForeignServerStmt())
	case node.GetAlterFunctionStmt() != nil:
		deparseAlterFunctionStmt(st, node.GetAlterFunctionStmt())
	case node.GetAlterObjectDependsStmt() != nil:
		deparseAlterObjectDependsStmt(st, node.GetAlterObjectDependsStmt())
	case node.GetAlterObjectSchemaStmt() != nil:
		deparseAlterObjectSchemaStmt(st, node.GetAlterObjectSchemaStmt())
	case node.GetAlterOwnerStmt() != nil:
		deparseAlterOwnerStmt(st, node.GetAlterOwnerStmt())
	case node.GetAlterOperatorStmt() != nil:
		deparseAlterOperatorStmt(st, node.GetAlterOperatorStmt())
	case node.GetAlterTypeStmt() != nil:
		deparseAlterTypeStmt(st, node.GetAlterTypeStmt())
	case node.GetAlterPolicyStmt() != nil:
		deparseAlterPolicyStmt(st, node.GetAlterPolicyStmt())
	case node.GetAlterSeqStmt() != nil:
		deparseAlterSeqStmt(st, node.GetAlterSeqStmt())
	case node.GetAlterSystemStmt() != nil:
		deparseAlterSystemStmt(st, node.GetAlterSystemStmt())
	case node.GetAlterTableMoveAllStmt() != nil:
		deparseAlterTableMoveAllStmt(st, node.GetAlterTableMoveAllStmt())
	case node.GetAlterTableStmt() != nil:
		deparseAlterTableStmt(st, node.GetAlterTableStmt())
	case node.GetAlterTableSpaceOptionsStmt() != nil: // "AlterTblSpcStmt" in gram.y
		deparseAlterTableSpaceOptionsStmt(st, node.GetAlterTableSpaceOptionsStmt())
	case node.GetAlterPublicationStmt() != nil:
		deparseAlterPublicationStmt(st, node.GetAlterPublicationStmt())
	case node.GetAlterRoleSetStmt() != nil:
		deparseAlterRoleSetStmt(st, node.GetAlterRoleSetStmt())
	case node.GetAlterRoleStmt() != nil:
		deparseAlterRoleStmt(st, node.GetAlterRoleStmt())
	case node.GetAlterSubscriptionStmt() != nil:
		deparseAlterSubscriptionStmt(st, node.GetAlterSubscriptionStmt())
	case node.GetAlterStatsStmt() != nil:
		deparseAlterStatsStmt(st, node.GetAlterStatsStmt())
	case node.GetAlterTsconfigurationStmt() != nil:
		deparseAlterTSConfigurationStmt(st, node.GetAlterTsconfigurationStmt())
	case node.GetAlterTsdictionaryStmt() != nil:
		deparseAlterTSDictionaryStmt(st, node.GetAlterTsdictionaryStmt())
	case node.GetAlterUserMappingStmt() != nil:
		deparseAlterUserMappingStmt(st, node.GetAlterUserMappingStmt())
	case node.GetCallStmt() != nil:
		deparseCallStmt(st, node.GetCallStmt())
	case node.GetCheckPointStmt() != nil:
		deparseCheckPointStmt(st, node.GetCheckPointStmt())
	case node.GetClosePortalStmt() != nil:
		deparseClosePortalStmt(st, node.GetClosePortalStmt())
	case node.GetClusterStmt() != nil:
		deparseClusterStmt(st, node.GetClusterStmt())
	case node.GetCommentStmt() != nil:
		deparseCommentStmt(st, node.GetCommentStmt())
	case node.GetConstraintsSetStmt() != nil:
		deparseConstraintsSetStmt(st, node.GetConstraintsSetStmt())
	case node.GetCopyStmt() != nil:
		deparseCopyStmt(st, node.GetCopyStmt())
	case node.GetCreateAmStmt() != nil:
		deparseCreateAmStmt(st, node.GetCreateAmStmt())
	case node.GetCreateTableAsStmt() != nil: // "CreateAsStmt" in gram.y
		deparseCreateTableAsStmt(st, node.GetCreateTableAsStmt())
	case node.GetCreateCastStmt() != nil:
		deparseCreateCastStmt(st, node.GetCreateCastStmt())
	case node.GetCreateConversionStmt() != nil:
		deparseCreateConversionStmt(st, node.GetCreateConversionStmt())
	case node.GetCreateDomainStmt() != nil:
		deparseCreateDomainStmt(st, node.GetCreateDomainStmt())
	case node.GetCreateExtensionStmt() != nil:
		deparseCreateExtensionStmt(st, node.GetCreateExtensionStmt())
	case node.GetCreateFdwStmt() != nil:
		deparseCreateFdwStmt(st, node.GetCreateFdwStmt())
	case node.GetCreateForeignServerStmt() != nil:
		deparseCreateForeignServerStmt(st, node.GetCreateForeignServerStmt())
	case node.GetCreateForeignTableStmt() != nil:
		deparseCreateForeignTableStmt(st, node.GetCreateForeignTableStmt())
	case node.GetCreateFunctionStmt() != nil:
		deparseCreateFunctionStmt(st, node.GetCreateFunctionStmt())
	case node.GetCreateOpClassStmt() != nil:
		deparseCreateOpClassStmt(st, node.GetCreateOpClassStmt())
	case node.GetCreateOpFamilyStmt() != nil:
		deparseCreateOpFamilyStmt(st, node.GetCreateOpFamilyStmt())
	case node.GetCreatePublicationStmt() != nil:
		deparseCreatePublicationStmt(st, node.GetCreatePublicationStmt())
	case node.GetAlterOpFamilyStmt() != nil:
		deparseAlterOpFamilyStmt(st, node.GetAlterOpFamilyStmt())
	case node.GetCreatePolicyStmt() != nil:
		deparseCreatePolicyStmt(st, node.GetCreatePolicyStmt())
	case node.GetCreatePlangStmt() != nil:
		deparseCreatePLangStmt(st, node.GetCreatePlangStmt())
	case node.GetCreateSchemaStmt() != nil:
		deparseCreateSchemaStmt(st, node.GetCreateSchemaStmt())
	case node.GetCreateSeqStmt() != nil:
		deparseCreateSeqStmt(st, node.GetCreateSeqStmt())
	case node.GetCreateStmt() != nil:
		deparseCreateStmt(st, node.GetCreateStmt(), false)
	case node.GetCreateSubscriptionStmt() != nil:
		deparseCreateSubscriptionStmt(st, node.GetCreateSubscriptionStmt())
	case node.GetCreateStatsStmt() != nil:
		deparseCreateStatsStmt(st, node.GetCreateStatsStmt())
	case node.GetCreateTableSpaceStmt() != nil:
		deparseCreateTableSpaceStmt(st, node.GetCreateTableSpaceStmt())
	case node.GetCreateTransformStmt() != nil:
		deparseCreateTransformStmt(st, node.GetCreateTransformStmt())
	case node.GetCreateTrigStmt() != nil:
		deparseCreateTrigStmt(st, node.GetCreateTrigStmt())
	case node.GetCreateEventTrigStmt() != nil:
		deparseCreateEventTrigStmt(st, node.GetCreateEventTrigStmt())
	case node.GetCreateRoleStmt() != nil:
		deparseCreateRoleStmt(st, node.GetCreateRoleStmt())
	case node.GetCreateUserMappingStmt() != nil:
		deparseCreateUserMappingStmt(st, node.GetCreateUserMappingStmt())
	case node.GetCreatedbStmt() != nil:
		deparseCreatedbStmt(st, node.GetCreatedbStmt())
	case node.GetDeallocateStmt() != nil:
		deparseDeallocateStmt(st, node.GetDeallocateStmt())
	case node.GetDeclareCursorStmt() != nil:
		deparseDeclareCursorStmt(st, node.GetDeclareCursorStmt())
	case node.GetDefineStmt() != nil:
		deparseDefineStmt(st, node.GetDefineStmt())
	case node.GetDeleteStmt() != nil:
		deparseDeleteStmt(st, node.GetDeleteStmt())
	case node.GetDiscardStmt() != nil:
		deparseDiscardStmt(st, node.GetDiscardStmt())
	case node.GetDoStmt() != nil:
		deparseDoStmt(st, node.GetDoStmt())
	case node.GetDropOwnedStmt() != nil:
		deparseDropOwnedStmt(st, node.GetDropOwnedStmt())
	case node.GetDropStmt() != nil:
		deparseDropStmt(st, node.GetDropStmt())
	case node.GetDropSubscriptionStmt() != nil:
		deparseDropSubscriptionStmt(st, node.GetDropSubscriptionStmt())
	case node.GetDropTableSpaceStmt() != nil:
		deparseDropTableSpaceStmt(st, node.GetDropTableSpaceStmt())
	case node.GetDropRoleStmt() != nil:
		deparseDropRoleStmt(st, node.GetDropRoleStmt())
	case node.GetDropUserMappingStmt() != nil:
		deparseDropUserMappingStmt(st, node.GetDropUserMappingStmt())
	case node.GetDropdbStmt() != nil:
		deparseDropdbStmt(st, node.GetDropdbStmt())
	case node.GetExecuteStmt() != nil:
		deparseExecuteStmt(st, node.GetExecuteStmt())
	case node.GetExplainStmt() != nil:
		deparseExplainStmt(st, node.GetExplainStmt())
	case node.GetFetchStmt() != nil:
		deparseFetchStmt(st, node.GetFetchStmt())
	case node.GetGrantStmt() != nil:
		deparseGrantStmt(st, node.GetGrantStmt())
	case node.GetGrantRoleStmt() != nil:
		deparseGrantRoleStmt(st, node.GetGrantRoleStmt())
	case node.GetImportForeignSchemaStmt() != nil:
		deparseImportForeignSchemaStmt(st, node.GetImportForeignSchemaStmt())
	case node.GetIndexStmt() != nil:
		deparseIndexStmt(st, node.GetIndexStmt())
	case node.GetInsertStmt() != nil:
		deparseInsertStmt(st, node.GetInsertStmt())
	case node.GetListenStmt() != nil:
		deparseListenStmt(st, node.GetListenStmt())
	case node.GetRefreshMatViewStmt() != nil:
		deparseRefreshMatViewStmt(st, node.GetRefreshMatViewStmt())
	case node.GetLoadStmt() != nil:
		deparseLoadStmt(st, node.GetLoadStmt())
	case node.GetLockStmt() != nil:
		deparseLockStmt(st, node.GetLockStmt())
	case node.GetMergeStmt() != nil:
		deparseMergeStmt(st, node.GetMergeStmt())
	case node.GetNotifyStmt() != nil:
		deparseNotifyStmt(st, node.GetNotifyStmt())
	case node.GetPrepareStmt() != nil:
		deparsePrepareStmt(st, node.GetPrepareStmt())
	case node.GetReassignOwnedStmt() != nil:
		deparseReassignOwnedStmt(st, node.GetReassignOwnedStmt())
	case node.GetReindexStmt() != nil:
		deparseReindexStmt(st, node.GetReindexStmt())
	case node.GetRenameStmt() != nil:
		deparseRenameStmt(st, node.GetRenameStmt())
	case node.GetRuleStmt() != nil:
		deparseRuleStmt(st, node.GetRuleStmt())
	case node.GetSecLabelStmt() != nil:
		deparseSecLabelStmt(st, node.GetSecLabelStmt())
	case node.GetSelectStmt() != nil:
		deparseSelectStmt(st, node.GetSelectStmt(), contextNone)
	case node.GetTransactionStmt() != nil:
		deparseTransactionStmt(st, node.GetTransactionStmt())
	case node.GetTruncateStmt() != nil:
		deparseTruncateStmt(st, node.GetTruncateStmt())
	case node.GetUnlistenStmt() != nil:
		deparseUnlistenStmt(st, node.GetUnlistenStmt())
	case node.GetUpdateStmt() != nil:
		deparseUpdateStmt(st, node.GetUpdateStmt())
	case node.GetVacuumStmt() != nil:
		deparseVacuumStmt(st, node.GetVacuumStmt())
	case node.GetVariableSetStmt() != nil:
		deparseVariableSetStmt(st, node.GetVariableSetStmt())
	case node.GetVariableShowStmt() != nil:
		deparseVariableShowStmt(st, node.GetVariableShowStmt())
	case node.GetViewStmt() != nil:
		deparseViewStmt(st, node.GetViewStmt())
	// These node types are created by DefineStmt grammar for CREATE TYPE in some cases
	case node.GetCompositeTypeStmt() != nil:
		deparseCompositeTypeStmt(st, node.GetCompositeTypeStmt())
	case node.GetCreateEnumStmt() != nil:
		deparseCreateEnumStmt(st, node.GetCreateEnumStmt())
	case node.GetCreateRangeStmt() != nil:
		deparseCreateRangeStmt(st, node.GetCreateRangeStmt())
	default:
		st.errorf("deparse: unsupported top-level node type: %s", nodeTag(node))
	}

	if !skip_push_pop {
		st.decreaseNestingLevel(parent_level)
	}
}
