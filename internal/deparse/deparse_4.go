// Ported from postgres_deparse.c (libpg_query 17-6.2.2).
// DDL: operator families, CREATE TABLE, constraints, indexes, DROP.
package deparse

import (
	"github.com/sqlc-dev/oliphant/ast"
)

// parsenodes.h: OPCLASS_ITEM_*
const (
	opclassItemOperator    = 1
	opclassItemFunction    = 2
	opclassItemStoragetype = 3
)

// parsenodes.h: TableLikeOption
const (
	createTableLikeComments    = 1 << 0
	createTableLikeCompression = 1 << 1
	createTableLikeConstraints = 1 << 2
	createTableLikeDefaults    = 1 << 3
	createTableLikeGenerated   = 1 << 4
	createTableLikeIdentity    = 1 << 5
	createTableLikeIndexes     = 1 << 6
	createTableLikeStatistics  = 1 << 7
	createTableLikeStorage     = 1 << 8
	createTableLikeAll         = 0x7FFFFFFF // PG_INT32_MAX
)

// index.h: DEFAULT_INDEX_TYPE
const defaultIndexType = "btree"

func deparseCreateOpFamilyStmt(st *state, create_op_family_stmt *ast.CreateOpFamilyStmt) {
	st.appendString("CREATE OPERATOR FAMILY ")

	deparseAnyName(st, create_op_family_stmt.Opfamilyname)
	st.appendChar(' ')

	st.appendString("USING ")
	st.appendString(quoteIdentifier(create_op_family_stmt.Amname))
}

func deparseCreateOpClassItem(st *state, create_op_class_item *ast.CreateOpClassItem) {
	switch create_op_class_item.Itemtype {
	case opclassItemOperator:
		st.appendString("OPERATOR ")
		st.appendf("%d ", create_op_class_item.Number)

		if create_op_class_item.Name != nil {
			if len(create_op_class_item.Name.Objargs) > 0 {
				deparseOperatorWithArgtypes(st, create_op_class_item.Name)
			} else {
				deparseAnyOperator(st, create_op_class_item.Name.Objname)
			}
			st.appendChar(' ')
		}

		if len(create_op_class_item.OrderFamily) > 0 {
			st.appendString("FOR ORDER BY ")
			deparseAnyName(st, create_op_class_item.OrderFamily)
		}

		if len(create_op_class_item.ClassArgs) > 0 {
			st.appendChar('(')
			deparseTypeList(st, create_op_class_item.ClassArgs)
			st.appendChar(')')
		}
		st.removeTrailingSpace()
	case opclassItemFunction:
		st.appendString("FUNCTION ")
		st.appendf("%d ", create_op_class_item.Number)
		if len(create_op_class_item.ClassArgs) > 0 {
			st.appendChar('(')
			deparseTypeList(st, create_op_class_item.ClassArgs)
			st.appendString(") ")
		}
		if create_op_class_item.Name != nil {
			deparseFunctionWithArgtypes(st, create_op_class_item.Name)
		}
		st.removeTrailingSpace()
	case opclassItemStoragetype:
		st.appendString("STORAGE ")
		deparseTypeName(st, create_op_class_item.Storedtype)
	default:
	}
}

func deparseTableLikeClause(st *state, table_like_clause *ast.TableLikeClause) {
	st.appendString("LIKE ")
	deparseRangeVar(st, table_like_clause.Relation, contextNone)
	st.appendChar(' ')

	if table_like_clause.Options == createTableLikeAll {
		st.appendString("INCLUDING ALL ")
	} else {
		if table_like_clause.Options&createTableLikeComments != 0 {
			st.appendString("INCLUDING COMMENTS ")
		}
		if table_like_clause.Options&createTableLikeCompression != 0 {
			st.appendString("INCLUDING COMPRESSION ")
		}
		if table_like_clause.Options&createTableLikeConstraints != 0 {
			st.appendString("INCLUDING CONSTRAINTS ")
		}
		if table_like_clause.Options&createTableLikeDefaults != 0 {
			st.appendString("INCLUDING DEFAULTS ")
		}
		if table_like_clause.Options&createTableLikeIdentity != 0 {
			st.appendString("INCLUDING IDENTITY ")
		}
		if table_like_clause.Options&createTableLikeGenerated != 0 {
			st.appendString("INCLUDING GENERATED ")
		}
		if table_like_clause.Options&createTableLikeIndexes != 0 {
			st.appendString("INCLUDING INDEXES ")
		}
		if table_like_clause.Options&createTableLikeStatistics != 0 {
			st.appendString("INCLUDING STATISTICS ")
		}
		if table_like_clause.Options&createTableLikeStorage != 0 {
			st.appendString("INCLUDING STORAGE ")
		}
	}
	st.removeTrailingSpace()
}

func deparseCreateDomainStmt(st *state, create_domain_stmt *ast.CreateDomainStmt) {
	st.appendString("CREATE DOMAIN ")
	deparseAnyName(st, create_domain_stmt.Domainname)
	st.appendString(" AS ")

	deparseTypeName(st, create_domain_stmt.TypeName)
	st.appendChar(' ')

	if create_domain_stmt.CollClause != nil {
		deparseCollateClause(st, create_domain_stmt.CollClause)
		st.appendChar(' ')
	}

	for _, item := range create_domain_stmt.Constraints {
		deparseConstraint(st, item.GetConstraint(), contextNone)
		st.appendChar(' ')
	}

	st.removeTrailingSpace()
}

func deparseCreateExtensionStmt(st *state, create_extension_stmt *ast.CreateExtensionStmt) {
	st.appendString("CREATE EXTENSION ")

	if create_extension_stmt.IfNotExists {
		st.appendString("IF NOT EXISTS ")
	}

	deparseColId(st, create_extension_stmt.Extname)
	st.appendChar(' ')

	for _, item := range create_extension_stmt.Options {
		def_elem := item.GetDefElem()

		if def_elem.Defname == "schema" {
			st.appendString("SCHEMA ")
			deparseColId(st, strVal(def_elem.Arg))
		} else if def_elem.Defname == "new_version" {
			st.appendString("VERSION ")
			deparseNonReservedWordOrSconst(st, strVal(def_elem.Arg))
		} else if def_elem.Defname == "cascade" {
			st.appendString("CASCADE")
		}

		st.appendChar(' ')
	}

	st.removeTrailingSpace()
}

func deparseConstraint(st *state, constraint *ast.Constraint, context nodeContext) {
	if constraint.Conname != "" {
		st.appendString("CONSTRAINT ")
		st.appendString(quoteIdentifier(constraint.Conname))
		st.appendChar(' ')
	}

	switch constraint.Contype {
	case ast.ConstrType_CONSTR_NULL:
		st.appendString("NULL ")
	case ast.ConstrType_CONSTR_NOTNULL:
		st.appendString("NOT NULL ")
	case ast.ConstrType_CONSTR_DEFAULT:
		st.appendString("DEFAULT ")
		deparseBExpr(st, constraint.RawExpr)
	case ast.ConstrType_CONSTR_IDENTITY:
		st.appendString("GENERATED ")
		switch constraint.GeneratedWhen {
		case "a":
			st.appendString("ALWAYS ")
		case "d":
			st.appendString("BY DEFAULT ")
		default:
		}
		st.appendString("AS IDENTITY ")
		deparseOptParenthesizedSeqOptList(st, constraint.Options)
	case ast.ConstrType_CONSTR_GENERATED:
		st.appendString("GENERATED ALWAYS AS (")
		deparseExpr(st, constraint.RawExpr, contextAExpr)
		st.appendString(") ")
		if constraint.GeneratedKind == "s" {
			st.appendString("STORED ")
		} else {
			st.appendString("VIRTUAL ")
		}
	case ast.ConstrType_CONSTR_CHECK:
		st.appendString("CHECK (")
		deparseExpr(st, constraint.RawExpr, contextAExpr)
		st.appendString(") ")
	case ast.ConstrType_CONSTR_PRIMARY:
		st.appendString("PRIMARY KEY ")
	case ast.ConstrType_CONSTR_UNIQUE:
		st.appendString("UNIQUE ")
		if constraint.NullsNotDistinct {
			st.appendString("NULLS NOT DISTINCT ")
		}
	case ast.ConstrType_CONSTR_EXCLUSION:
		st.appendString("EXCLUDE ")
		if constraint.AccessMethod != defaultIndexType {
			st.appendString("USING ")
			st.appendString(quoteIdentifier(constraint.AccessMethod))
			st.appendChar(' ')
		}
		st.appendChar('(')
		for i, item := range constraint.Exclusions {
			exclusion := item.GetList().Items
			deparseIndexElem(st, exclusion[0].GetIndexElem())
			st.appendString(" WITH ")
			deparseAnyOperator(st, exclusion[1].GetList().Items)
			if i < len(constraint.Exclusions)-1 {
				st.appendString(", ")
			}
		}
		st.appendString(") ")
		if constraint.WhereClause != nil {
			st.appendString("WHERE (")
			deparseExpr(st, constraint.WhereClause, contextAExpr)
			st.appendString(") ")
		}
	case ast.ConstrType_CONSTR_FOREIGN:
		if len(constraint.FkAttrs) > 0 {
			st.appendString("FOREIGN KEY ")
		}
	case ast.ConstrType_CONSTR_ATTR_DEFERRABLE:
		st.appendString("DEFERRABLE ")
	case ast.ConstrType_CONSTR_ATTR_NOT_DEFERRABLE:
		st.appendString("NOT DEFERRABLE ")
	case ast.ConstrType_CONSTR_ATTR_DEFERRED:
		st.appendString("INITIALLY DEFERRED ")
	case ast.ConstrType_CONSTR_ATTR_IMMEDIATE:
		st.appendString("INITIALLY IMMEDIATE ")
	case ast.ConstrType_CONSTR_ATTR_ENFORCED:
		st.appendString("ENFORCED ")
	case ast.ConstrType_CONSTR_ATTR_NOT_ENFORCED:
		st.appendString("NOT ENFORCED ")
	}

	if len(constraint.Keys) > 0 {
		emitKeys := true
		needsParens := true

		if len(constraint.Keys) == 1 {
			key := constraint.Keys[0]
			if context == contextAlterDomain {
				emitKeys = !(key.GetString_() != nil && key.GetString_().Sval == "value")
			}
			needsParens = constraint.Contype != ast.ConstrType_CONSTR_NULL &&
				constraint.Contype != ast.ConstrType_CONSTR_NOTNULL
		}

		if needsParens {
			st.appendChar('(')
		}

		if emitKeys {
			deparseColumnList(st, constraint.Keys)
		}

		if constraint.WithoutOverlaps {
			st.appendString(" WITHOUT OVERLAPS")
		}

		if needsParens {
			st.appendChar(')')
		}

		st.appendChar(' ')
	}

	if len(constraint.FkAttrs) > 0 {
		st.appendChar('(')
		if constraint.FkWithPeriod {
			deparseColumnListWithPeriod(st, constraint.FkAttrs)
		} else {
			deparseColumnList(st, constraint.FkAttrs)
		}
		st.appendString(") ")
	}

	if constraint.Pktable != nil {
		st.appendString("REFERENCES ")
		deparseRangeVar(st, constraint.Pktable, contextNone)
		st.appendChar(' ')
		if len(constraint.PkAttrs) > 0 {
			st.appendChar('(')
			if constraint.PkWithPeriod {
				deparseColumnListWithPeriod(st, constraint.PkAttrs)
			} else {
				deparseColumnList(st, constraint.PkAttrs)
			}
			st.appendString(") ")
		}
	}

	switch constraint.FkMatchtype {
	case "s":
		// Default
	case "f":
		st.appendString("MATCH FULL ")
	case "p":
		// Not implemented in Postgres
	default:
		// Not specified
	}

	switch constraint.FkUpdAction {
	case "a":
		// Default
	case "r":
		st.appendString("ON UPDATE RESTRICT ")
	case "c":
		st.appendString("ON UPDATE CASCADE ")
	case "n":
		st.appendString("ON UPDATE SET NULL ")
	case "d":
		st.appendString("ON UPDATE SET DEFAULT ")
	default:
		// Not specified
	}

	switch constraint.FkDelAction {
	case "a":
		// Default
	case "r":
		st.appendString("ON DELETE RESTRICT ")
	case "c":
		st.appendString("ON DELETE CASCADE ")
	case "n", "d":
		st.appendString("ON DELETE SET ")

		switch constraint.FkDelAction {
		case "d":
			st.appendString("DEFAULT ")
		case "n":
			st.appendString("NULL ")
		}

		if len(constraint.FkDelSetCols) > 0 {
			st.appendString("(")
			for i, item := range constraint.FkDelSetCols {
				st.appendString(strVal(item))
				if i < len(constraint.FkDelSetCols)-1 {
					st.appendString(", ")
				}
			}
			st.appendString(")")
		}
	default:
		// Not specified
	}

	if len(constraint.Including) > 0 {
		st.appendString("INCLUDE (")
		deparseColumnList(st, constraint.Including)
		st.appendString(") ")
	}

	switch constraint.Contype {
	case ast.ConstrType_CONSTR_PRIMARY,
		ast.ConstrType_CONSTR_UNIQUE,
		ast.ConstrType_CONSTR_EXCLUSION:
		deparseOptWith(st, constraint.Options)
	default:
	}

	if constraint.Indexname != "" {
		st.appendf("USING INDEX %s ", quoteIdentifier(constraint.Indexname))
	}

	if constraint.Indexspace != "" {
		st.appendf("USING INDEX TABLESPACE %s ", quoteIdentifier(constraint.Indexspace))
	}

	if constraint.Deferrable {
		st.appendString("DEFERRABLE ")
	}

	if constraint.Initdeferred {
		st.appendString("INITIALLY DEFERRED ")
	}

	if constraint.IsNoInherit {
		st.appendString("NO INHERIT ")
	}

	if (constraint.Contype == ast.ConstrType_CONSTR_FOREIGN ||
		constraint.Contype == ast.ConstrType_CONSTR_CHECK) && !constraint.IsEnforced {
		st.appendString("NOT ENFORCED ")
	}

	if constraint.SkipValidation {
		st.appendString("NOT VALID ")
	}

	st.removeTrailingSpace()
}

func deparseReturnStmt(st *state, return_stmt *ast.ReturnStmt) {
	st.appendString("RETURN ")
	deparseExpr(st, return_stmt.Returnval, contextAExpr)
}

func deparseCreateFunctionStmt(st *state, create_function_stmt *ast.CreateFunctionStmt) {
	tableFunc := false

	st.appendString("CREATE ")
	if create_function_stmt.Replace {
		st.appendString("OR REPLACE ")
	}
	if create_function_stmt.IsProcedure {
		st.appendString("PROCEDURE ")
	} else {
		st.appendString("FUNCTION ")
	}

	deparseFuncName(st, create_function_stmt.Funcname)

	st.appendChar('(')
	for i, item := range create_function_stmt.Parameters {
		function_parameter := item.GetFunctionParameter()
		if function_parameter.Mode != ast.FunctionParameterMode_FUNC_PARAM_TABLE {
			deparseFunctionParameter(st, function_parameter)
			if i < len(create_function_stmt.Parameters)-1 && create_function_stmt.Parameters[i+1].GetFunctionParameter().Mode != ast.FunctionParameterMode_FUNC_PARAM_TABLE {
				st.appendString(", ")
			}
		} else {
			tableFunc = true
		}
	}
	st.appendString(") ")

	if tableFunc {
		st.appendString("RETURNS TABLE (")
		for i, item := range create_function_stmt.Parameters {
			function_parameter := item.GetFunctionParameter()
			if function_parameter.Mode == ast.FunctionParameterMode_FUNC_PARAM_TABLE {
				deparseFunctionParameter(st, function_parameter)
				if i < len(create_function_stmt.Parameters)-1 {
					st.appendString(", ")
				}
			}
		}
		st.appendString(") ")
	} else if create_function_stmt.ReturnType != nil {
		st.appendString("RETURNS ")
		deparseTypeName(st, create_function_stmt.ReturnType)
		st.appendChar(' ')
	}

	for _, item := range create_function_stmt.Options {
		deparseCreateFuncOptItem(st, item.GetDefElem())
		st.appendChar(' ')
	}

	if create_function_stmt.SqlBody != nil {
		/* RETURN or BEGIN ... END
		 */
		if create_function_stmt.SqlBody.GetReturnStmt() != nil {
			deparseReturnStmt(st, create_function_stmt.SqlBody.GetReturnStmt())
		} else {
			st.appendString("BEGIN ATOMIC ")
			if create_function_stmt.SqlBody.GetList().Items[0].Node != nil {
				body_stmt_list := create_function_stmt.SqlBody.GetList().Items[0].GetList().Items
				for _, item := range body_stmt_list {
					if item.GetReturnStmt() != nil {
						deparseReturnStmt(st, item.GetReturnStmt())
						st.appendString("; ")
					} else {
						deparseStmt(st, item)
						st.appendString("; ")
					}
				}
			}

			st.appendString("END ")
		}
	}

	st.removeTrailingSpace()
}

func deparseFunctionParameter(st *state, function_parameter *ast.FunctionParameter) {
	switch function_parameter.Mode {
	case ast.FunctionParameterMode_FUNC_PARAM_IN: /* input only */
		st.appendString("IN ")
	case ast.FunctionParameterMode_FUNC_PARAM_OUT: /* output only */
		st.appendString("OUT ")
	case ast.FunctionParameterMode_FUNC_PARAM_INOUT: /* both */
		st.appendString("INOUT ")
	case ast.FunctionParameterMode_FUNC_PARAM_VARIADIC: /* variadic (always input) */
		st.appendString("VARIADIC ")
	case ast.FunctionParameterMode_FUNC_PARAM_TABLE: /* table function output column */
		// No special annotation, the caller is expected to correctly put
		// this into the RETURNS part of the CREATE FUNCTION statement
	case ast.FunctionParameterMode_FUNC_PARAM_DEFAULT:
		// Default
	default:
	}

	if function_parameter.Name != "" {
		st.appendString(function_parameter.Name)
		st.appendChar(' ')
	}

	deparseTypeName(st, function_parameter.ArgType)
	st.appendChar(' ')

	if function_parameter.Defexpr != nil {
		st.appendString("= ")
		deparseExpr(st, function_parameter.Defexpr, contextAExpr)
	}

	st.removeTrailingSpace()
}

func deparseCheckPointStmt(st *state, check_point_stmt *ast.CheckPointStmt) {
	st.appendString("CHECKPOINT")
}

func deparseCreateSchemaStmt(st *state, create_schema_stmt *ast.CreateSchemaStmt) {
	st.appendString("CREATE SCHEMA ")

	if create_schema_stmt.IfNotExists {
		st.appendString("IF NOT EXISTS ")
	}

	if create_schema_stmt.Schemaname != "" {
		deparseColId(st, create_schema_stmt.Schemaname)
		st.appendChar(' ')
	}

	if create_schema_stmt.Authrole != nil {
		st.appendString("AUTHORIZATION ")
		deparseRoleSpec(st, create_schema_stmt.Authrole)
		st.appendChar(' ')
	}

	if len(create_schema_stmt.SchemaElts) > 0 {
		for i, item := range create_schema_stmt.SchemaElts {
			deparseSchemaStmt(st, item)
			if i < len(create_schema_stmt.SchemaElts)-1 {
				st.appendChar(' ')
			}
		}
	}

	st.removeTrailingSpace()
}

func deparseAlterRoleSetStmt(st *state, alter_role_set_stmt *ast.AlterRoleSetStmt) {
	st.appendString("ALTER ROLE ")

	if alter_role_set_stmt.Role == nil {
		st.appendString("ALL")
	} else {
		deparseRoleSpec(st, alter_role_set_stmt.Role)
	}

	st.appendChar(' ')

	if alter_role_set_stmt.Database != "" {
		st.appendString("IN DATABASE ")
		st.appendString(quoteIdentifier(alter_role_set_stmt.Database))
		st.appendChar(' ')
	}

	deparseVariableSetStmt(st, alter_role_set_stmt.Setstmt)
}

func deparseCreateConversionStmt(st *state, create_conversion_stmt *ast.CreateConversionStmt) {
	st.appendString("CREATE ")
	if create_conversion_stmt.Def {
		st.appendString("DEFAULT ")
	}

	st.appendString("CONVERSION ")
	deparseAnyName(st, create_conversion_stmt.ConversionName)
	st.appendChar(' ')

	st.appendString("FOR ")
	deparseStringLiteral(st, create_conversion_stmt.ForEncodingName)
	st.appendString(" TO ")
	deparseStringLiteral(st, create_conversion_stmt.ToEncodingName)

	st.appendString("FROM ")
	deparseAnyName(st, create_conversion_stmt.FuncName)
}

func deparseRoleSpec(st *state, role_spec *ast.RoleSpec) {
	switch role_spec.Roletype {
	case ast.RoleSpecType_ROLESPEC_CSTRING:
		st.appendString(quoteIdentifier(role_spec.Rolename))
	case ast.RoleSpecType_ROLESPEC_CURRENT_ROLE:
		st.appendString("CURRENT_ROLE")
	case ast.RoleSpecType_ROLESPEC_CURRENT_USER:
		st.appendString("CURRENT_USER")
	case ast.RoleSpecType_ROLESPEC_SESSION_USER:
		st.appendString("SESSION_USER")
	case ast.RoleSpecType_ROLESPEC_PUBLIC:
		st.appendString("public")
	}
}

func deparsePartitionElem(st *state, partition_elem *ast.PartitionElem) {
	if partition_elem.Name != "" {
		deparseColId(st, partition_elem.Name)
		st.appendChar(' ')
	} else if partition_elem.Expr != nil {
		st.appendChar('(')
		deparseExpr(st, partition_elem.Expr, contextAExpr)
		st.appendString(") ")
	}

	deparseOptCollate(st, partition_elem.Collation)
	deparseAnyName(st, partition_elem.Opclass)

	st.removeTrailingSpace()
}

func deparsePartitionSpec(st *state, partition_spec *ast.PartitionSpec) {
	st.appendPartGroup("PARTITION BY", partIndent)

	switch partition_spec.Strategy {
	case ast.PartitionStrategy_PARTITION_STRATEGY_LIST:
		st.appendString("LIST")
	case ast.PartitionStrategy_PARTITION_STRATEGY_HASH:
		st.appendString("HASH")
	case ast.PartitionStrategy_PARTITION_STRATEGY_RANGE:
		st.appendString("RANGE")
	}

	st.appendString(" (")
	for i, item := range partition_spec.PartParams {
		deparsePartitionElem(st, item.GetPartitionElem())
		if i < len(partition_spec.PartParams)-1 {
			st.appendString(", ")
		}
	}
	st.appendChar(')')
}

func deparsePartitionBoundSpec(st *state, partition_bound_spec *ast.PartitionBoundSpec) {
	if partition_bound_spec.IsDefault {
		st.appendString("DEFAULT")
		return
	}

	st.appendString("FOR VALUES ")

	switch partition_bound_spec.Strategy {
	case "h":
		st.appendf("WITH (MODULUS %d, REMAINDER %d)", partition_bound_spec.Modulus, partition_bound_spec.Remainder)
	case "l":
		st.appendString("IN (")
		deparseExprList(st, partition_bound_spec.Listdatums)
		st.appendChar(')')
	case "r":
		st.appendString("FROM (")
		deparseExprList(st, partition_bound_spec.Lowerdatums)
		st.appendString(") TO (")
		deparseExprList(st, partition_bound_spec.Upperdatums)
		st.appendChar(')')
	default:
	}
}

func deparsePartitionCmd(st *state, partition_cmd *ast.PartitionCmd) {
	deparseRangeVar(st, partition_cmd.Name, contextNone)

	if partition_cmd.Bound != nil {
		st.appendChar(' ')
		deparsePartitionBoundSpec(st, partition_cmd.Bound)
	}
	if partition_cmd.Concurrent {
		st.appendString(" CONCURRENTLY ")
	}
}

func deparseTableElement(st *state, node *ast.Node) {
	switch {
	case node.GetColumnDef() != nil:
		deparseColumnDef(st, node.GetColumnDef())
	case node.GetTableLikeClause() != nil:
		deparseTableLikeClause(st, node.GetTableLikeClause())
	case node.GetConstraint() != nil:
		deparseConstraint(st, node.GetConstraint(), contextNone)
	default:
	}
}

func deparseCreateStmt(st *state, create_stmt *ast.CreateStmt, is_foreign_table bool) {
	st.appendString("CREATE ")

	if is_foreign_table {
		st.appendString("FOREIGN ")
	}

	deparseOptTemp(st, create_stmt.Relation.Relpersistence)

	st.appendString("TABLE ")

	if create_stmt.IfNotExists {
		st.appendString("IF NOT EXISTS ")
	}

	deparseRangeVar(st, create_stmt.Relation, contextNone)
	st.appendChar(' ')

	if create_stmt.OfTypename != nil {
		st.appendString("OF ")
		deparseTypeName(st, create_stmt.OfTypename)
		st.appendChar(' ')
	}

	if create_stmt.Partbound != nil {
		st.appendString("PARTITION OF ")
		deparseRangeVar(st, create_stmt.InhRelations[0].GetRangeVar(), contextNone)
		st.appendChar(' ')
	}

	if len(create_stmt.TableElts) > 0 {
		var parent_level *nestingLevel
		// In raw parse output tableElts contains both columns and constraints
		// (and the constraints field is NIL)
		st.appendChar('(')
		parent_level = st.increaseNestingLevel()
		for i, item := range create_stmt.TableElts {
			deparseTableElement(st, item)
			if i < len(create_stmt.TableElts)-1 {
				st.appendCommaAndPart()
			}
		}
		st.decreaseNestingLevel(parent_level)
		st.appendString(") ")
	} else if create_stmt.Partbound == nil && create_stmt.OfTypename == nil {
		st.appendString("() ")
	}

	if create_stmt.Partbound != nil {
		deparsePartitionBoundSpec(st, create_stmt.Partbound)
		st.appendChar(' ')
	} else {
		deparseOptInherit(st, create_stmt.InhRelations)
	}

	if create_stmt.Partspec != nil {
		deparsePartitionSpec(st, create_stmt.Partspec)
		st.appendChar(' ')
	}

	if create_stmt.AccessMethod != "" {
		st.appendString("USING ")
		st.appendString(quoteIdentifier(create_stmt.AccessMethod))
	}

	deparseOptWith(st, create_stmt.Options)

	switch create_stmt.Oncommit {
	case ast.OnCommitAction_ONCOMMIT_NOOP:
		// No ON COMMIT clause
	case ast.OnCommitAction_ONCOMMIT_PRESERVE_ROWS:
		st.appendString("ON COMMIT PRESERVE ROWS ")
	case ast.OnCommitAction_ONCOMMIT_DELETE_ROWS:
		st.appendString("ON COMMIT DELETE ROWS ")
	case ast.OnCommitAction_ONCOMMIT_DROP:
		st.appendString("ON COMMIT DROP ")
	}

	if create_stmt.Tablespacename != "" {
		st.appendString("TABLESPACE ")
		st.appendString(quoteIdentifier(create_stmt.Tablespacename))
	}

	st.removeTrailingSpace()
}

func deparseCreateFdwStmt(st *state, create_fdw_stmt *ast.CreateFdwStmt) {
	st.appendString("CREATE FOREIGN DATA WRAPPER ")
	st.appendString(quoteIdentifier(create_fdw_stmt.Fdwname))
	st.appendChar(' ')

	if len(create_fdw_stmt.FuncOptions) > 0 {
		deparseFdwOptions(st, create_fdw_stmt.FuncOptions)
		st.appendChar(' ')
	}

	deparseCreateGenericOptions(st, create_fdw_stmt.Options)

	st.removeTrailingSpace()
}

func deparseAlterFdwStmt(st *state, alter_fdw_stmt *ast.AlterFdwStmt) {
	st.appendString("ALTER FOREIGN DATA WRAPPER ")
	st.appendString(quoteIdentifier(alter_fdw_stmt.Fdwname))
	st.appendChar(' ')

	if len(alter_fdw_stmt.FuncOptions) > 0 {
		deparseFdwOptions(st, alter_fdw_stmt.FuncOptions)
		st.appendChar(' ')
	}

	if len(alter_fdw_stmt.Options) > 0 {
		deparseAlterGenericOptions(st, alter_fdw_stmt.Options)
	}

	st.removeTrailingSpace()
}

func deparseCreateForeignServerStmt(st *state, create_foreign_server_stmt *ast.CreateForeignServerStmt) {
	st.appendString("CREATE SERVER ")
	if create_foreign_server_stmt.IfNotExists {
		st.appendString("IF NOT EXISTS ")
	}
	st.appendString(quoteIdentifier(create_foreign_server_stmt.Servername))
	st.appendChar(' ')

	if create_foreign_server_stmt.Servertype != "" {
		st.appendString("TYPE ")
		deparseStringLiteral(st, create_foreign_server_stmt.Servertype)
		st.appendChar(' ')
	}

	if create_foreign_server_stmt.Version != "" {
		st.appendString("VERSION ")
		deparseStringLiteral(st, create_foreign_server_stmt.Version)
		st.appendChar(' ')
	}

	st.appendString("FOREIGN DATA WRAPPER ")
	st.appendString(quoteIdentifier(create_foreign_server_stmt.Fdwname))
	st.appendChar(' ')

	deparseCreateGenericOptions(st, create_foreign_server_stmt.Options)

	st.removeTrailingSpace()
}

func deparseAlterForeignServerStmt(st *state, alter_foreign_server_stmt *ast.AlterForeignServerStmt) {
	st.appendString("ALTER SERVER ")

	st.appendString(quoteIdentifier(alter_foreign_server_stmt.Servername))
	st.appendChar(' ')

	if alter_foreign_server_stmt.HasVersion {
		st.appendString("VERSION ")
		if alter_foreign_server_stmt.Version != "" {
			deparseStringLiteral(st, alter_foreign_server_stmt.Version)
		} else {
			st.appendString("NULL")
		}
		st.appendChar(' ')
	}

	if len(alter_foreign_server_stmt.Options) > 0 {
		deparseAlterGenericOptions(st, alter_foreign_server_stmt.Options)
	}

	st.removeTrailingSpace()
}

func deparseCreateUserMappingStmt(st *state, create_user_mapping_stmt *ast.CreateUserMappingStmt) {
	st.appendString("CREATE USER MAPPING ")
	if create_user_mapping_stmt.IfNotExists {
		st.appendString("IF NOT EXISTS ")
	}

	st.appendString("FOR ")
	deparseRoleSpec(st, create_user_mapping_stmt.User)
	st.appendChar(' ')

	st.appendString("SERVER ")
	st.appendString(quoteIdentifier(create_user_mapping_stmt.Servername))
	st.appendChar(' ')

	deparseCreateGenericOptions(st, create_user_mapping_stmt.Options)

	st.removeTrailingSpace()
}

func deparseCreatedbStmt(st *state, createdb_stmt *ast.CreatedbStmt) {
	st.appendString("CREATE DATABASE ")
	deparseColId(st, createdb_stmt.Dbname)
	st.appendChar(' ')
	deparseCreatedbOptList(st, createdb_stmt.Options)
	st.removeTrailingSpace()
}

func deparseAlterUserMappingStmt(st *state, alter_user_mapping_stmt *ast.AlterUserMappingStmt) {
	st.appendString("ALTER USER MAPPING FOR ")
	deparseRoleSpec(st, alter_user_mapping_stmt.User)
	st.appendChar(' ')

	st.appendString("SERVER ")
	st.appendString(quoteIdentifier(alter_user_mapping_stmt.Servername))
	st.appendChar(' ')

	deparseAlterGenericOptions(st, alter_user_mapping_stmt.Options)

	st.removeTrailingSpace()
}

func deparseDropUserMappingStmt(st *state, drop_user_mapping_stmt *ast.DropUserMappingStmt) {
	st.appendString("DROP USER MAPPING ")

	if drop_user_mapping_stmt.MissingOk {
		st.appendString("IF EXISTS ")
	}

	st.appendString("FOR ")
	deparseRoleSpec(st, drop_user_mapping_stmt.User)
	st.appendChar(' ')

	st.appendString("SERVER ")
	st.appendString(quoteIdentifier(drop_user_mapping_stmt.Servername))
}

func deparseSecLabelStmt(st *state, sec_label_stmt *ast.SecLabelStmt) {
	st.appendString("SECURITY LABEL ")

	if sec_label_stmt.Provider != "" {
		st.appendString("FOR ")
		st.appendString(quoteIdentifier(sec_label_stmt.Provider))
		st.appendChar(' ')
	}

	st.appendString("ON ")

	switch sec_label_stmt.Objtype {
	case ast.ObjectType_OBJECT_COLUMN:
		st.appendString("COLUMN ")
		deparseAnyName(st, sec_label_stmt.Object.GetList().Items)
	case ast.ObjectType_OBJECT_FOREIGN_TABLE:
		st.appendString("FOREIGN TABLE ")
		deparseAnyName(st, sec_label_stmt.Object.GetList().Items)
	case ast.ObjectType_OBJECT_SEQUENCE:
		st.appendString("SEQUENCE ")
		deparseAnyName(st, sec_label_stmt.Object.GetList().Items)
	case ast.ObjectType_OBJECT_TABLE:
		st.appendString("TABLE ")
		deparseAnyName(st, sec_label_stmt.Object.GetList().Items)
	case ast.ObjectType_OBJECT_VIEW:
		st.appendString("VIEW ")
		deparseAnyName(st, sec_label_stmt.Object.GetList().Items)
	case ast.ObjectType_OBJECT_MATVIEW:
		st.appendString("MATERIALIZED VIEW ")
		deparseAnyName(st, sec_label_stmt.Object.GetList().Items)
	case ast.ObjectType_OBJECT_DATABASE:
		st.appendString("DATABASE ")
		st.appendString(quoteIdentifier(strVal(sec_label_stmt.Object)))
	case ast.ObjectType_OBJECT_EVENT_TRIGGER:
		st.appendString("EVENT TRIGGER ")
		st.appendString(quoteIdentifier(strVal(sec_label_stmt.Object)))
	case ast.ObjectType_OBJECT_LANGUAGE:
		st.appendString("LANGUAGE ")
		st.appendString(quoteIdentifier(strVal(sec_label_stmt.Object)))
	case ast.ObjectType_OBJECT_PUBLICATION:
		st.appendString("PUBLICATION ")
		st.appendString(quoteIdentifier(strVal(sec_label_stmt.Object)))
	case ast.ObjectType_OBJECT_ROLE:
		st.appendString("ROLE ")
		st.appendString(quoteIdentifier(strVal(sec_label_stmt.Object)))
	case ast.ObjectType_OBJECT_SCHEMA:
		st.appendString("SCHEMA ")
		st.appendString(quoteIdentifier(strVal(sec_label_stmt.Object)))
	case ast.ObjectType_OBJECT_SUBSCRIPTION:
		st.appendString("SUBSCRIPTION ")
		st.appendString(quoteIdentifier(strVal(sec_label_stmt.Object)))
	case ast.ObjectType_OBJECT_TABLESPACE:
		st.appendString("TABLESPACE ")
		st.appendString(quoteIdentifier(strVal(sec_label_stmt.Object)))
	case ast.ObjectType_OBJECT_TYPE:
		st.appendString("TYPE ")
		deparseTypeName(st, sec_label_stmt.Object.GetTypeName())
	case ast.ObjectType_OBJECT_DOMAIN:
		st.appendString("DOMAIN ")
		deparseTypeName(st, sec_label_stmt.Object.GetTypeName())
	case ast.ObjectType_OBJECT_AGGREGATE:
		st.appendString("AGGREGATE ")
		deparseAggregateWithArgtypes(st, sec_label_stmt.Object.GetObjectWithArgs())
	case ast.ObjectType_OBJECT_FUNCTION:
		st.appendString("FUNCTION ")
		deparseFunctionWithArgtypes(st, sec_label_stmt.Object.GetObjectWithArgs())
	case ast.ObjectType_OBJECT_LARGEOBJECT:
		st.appendString("LARGE OBJECT ")
		deparseValue(st, sec_label_stmt.Object, contextConstant)
	case ast.ObjectType_OBJECT_PROCEDURE:
		st.appendString("PROCEDURE ")
		deparseFunctionWithArgtypes(st, sec_label_stmt.Object.GetObjectWithArgs())
	case ast.ObjectType_OBJECT_ROUTINE:
		st.appendString("ROUTINE ")
		deparseFunctionWithArgtypes(st, sec_label_stmt.Object.GetObjectWithArgs())
	default:
		// Not supported in the parser
	}

	st.appendString(" IS ")

	if sec_label_stmt.Label != "" || st.empties[sec_label_stmt] {
		deparseStringLiteral(st, sec_label_stmt.Label)
	} else {
		st.appendString("NULL")
	}
}

func deparseCreateForeignTableStmt(st *state, create_foreign_table_stmt *ast.CreateForeignTableStmt) {
	deparseCreateStmt(st, create_foreign_table_stmt.BaseStmt, true)

	st.appendString(" SERVER ")
	st.appendString(quoteIdentifier(create_foreign_table_stmt.Servername))
	st.appendChar(' ')

	if len(create_foreign_table_stmt.Options) > 0 {
		deparseAlterGenericOptions(st, create_foreign_table_stmt.Options)
	}

	st.removeTrailingSpace()
}

func deparseImportForeignSchemaStmt(st *state, import_foreign_schema_stmt *ast.ImportForeignSchemaStmt) {
	st.appendString("IMPORT FOREIGN SCHEMA ")

	st.appendString(import_foreign_schema_stmt.RemoteSchema)
	st.appendChar(' ')

	switch import_foreign_schema_stmt.ListType {
	case ast.ImportForeignSchemaType_FDW_IMPORT_SCHEMA_ALL:
		// Default
	case ast.ImportForeignSchemaType_FDW_IMPORT_SCHEMA_LIMIT_TO:
		st.appendString("LIMIT TO (")
		deparseRelationExprList(st, import_foreign_schema_stmt.TableList)
		st.appendString(") ")
	case ast.ImportForeignSchemaType_FDW_IMPORT_SCHEMA_EXCEPT:
		st.appendString("EXCEPT (")
		deparseRelationExprList(st, import_foreign_schema_stmt.TableList)
		st.appendString(") ")
	}

	st.appendString("FROM SERVER ")
	st.appendString(quoteIdentifier(import_foreign_schema_stmt.ServerName))
	st.appendChar(' ')

	st.appendString("INTO ")
	st.appendString(quoteIdentifier(import_foreign_schema_stmt.LocalSchema))
	st.appendChar(' ')

	deparseCreateGenericOptions(st, import_foreign_schema_stmt.Options)

	st.removeTrailingSpace()
}

func deparseCreateTableAsStmt(st *state, create_table_as_stmt *ast.CreateTableAsStmt) {
	st.appendString("CREATE ")

	deparseOptTemp(st, create_table_as_stmt.Into.Rel.Relpersistence)

	switch create_table_as_stmt.Objtype {
	case ast.ObjectType_OBJECT_TABLE:
		st.appendString("TABLE ")
	case ast.ObjectType_OBJECT_MATVIEW:
		st.appendString("MATERIALIZED VIEW ")
	default:
		// Not supported here
	}

	if create_table_as_stmt.IfNotExists {
		st.appendString("IF NOT EXISTS ")
	}

	deparseIntoClause(st, create_table_as_stmt.Into)
	st.appendChar(' ')

	st.appendString("AS ")
	if create_table_as_stmt.Query.GetExecuteStmt() != nil {
		deparseExecuteStmt(st, create_table_as_stmt.Query.GetExecuteStmt())
	} else {
		deparseSelectStmt(st, create_table_as_stmt.Query.GetSelectStmt(), contextNone)
	}
	st.appendChar(' ')

	if create_table_as_stmt.Into.SkipData {
		st.appendString("WITH NO DATA ")
	}

	st.removeTrailingSpace()
}

func deparseViewStmt(st *state, view_stmt *ast.ViewStmt) {
	st.appendString("CREATE ")

	if view_stmt.Replace {
		st.appendString("OR REPLACE ")
	}

	deparseOptTemp(st, view_stmt.View.Relpersistence)

	st.appendString("VIEW ")
	deparseRangeVar(st, view_stmt.View, contextNone)
	st.appendChar(' ')

	if len(view_stmt.Aliases) > 0 {
		st.appendChar('(')
		deparseColumnList(st, view_stmt.Aliases)
		st.appendString(") ")
	}

	deparseOptWith(st, view_stmt.Options)

	st.appendString("AS ")
	deparseSelectStmt(st, view_stmt.Query.GetSelectStmt(), contextNone)

	switch view_stmt.WithCheckOption {
	case ast.ViewCheckOption_NO_CHECK_OPTION:
		// Default
	case ast.ViewCheckOption_LOCAL_CHECK_OPTION:
		st.appendString("WITH LOCAL CHECK OPTION ")
	case ast.ViewCheckOption_CASCADED_CHECK_OPTION:
		st.appendString("WITH CHECK OPTION ")
	}

	st.removeTrailingSpace()
}

func deparseDropStmt(st *state, drop_stmt *ast.DropStmt) {
	var l []*ast.Node

	st.appendString("DROP ")

	switch drop_stmt.RemoveType {
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
	case ast.ObjectType_OBJECT_EVENT_TRIGGER:
		st.appendString("EVENT TRIGGER ")
	case ast.ObjectType_OBJECT_EXTENSION:
		st.appendString("EXTENSION ")
	case ast.ObjectType_OBJECT_FDW:
		st.appendString("FOREIGN DATA WRAPPER ")
	case ast.ObjectType_OBJECT_FOREIGN_SERVER:
		st.appendString("SERVER ")
	case ast.ObjectType_OBJECT_FOREIGN_TABLE:
		st.appendString("FOREIGN TABLE ")
	case ast.ObjectType_OBJECT_FUNCTION:
		st.appendString("FUNCTION ")
	case ast.ObjectType_OBJECT_INDEX:
		st.appendString("INDEX ")
	case ast.ObjectType_OBJECT_LANGUAGE:
		st.appendString("LANGUAGE ")
	case ast.ObjectType_OBJECT_MATVIEW:
		st.appendString("MATERIALIZED VIEW ")
	case ast.ObjectType_OBJECT_OPCLASS:
		st.appendString("OPERATOR CLASS ")
	case ast.ObjectType_OBJECT_OPERATOR:
		st.appendString("OPERATOR ")
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
	case ast.ObjectType_OBJECT_RULE:
		st.appendString("RULE ")
	case ast.ObjectType_OBJECT_SCHEMA:
		st.appendString("SCHEMA ")
	case ast.ObjectType_OBJECT_SEQUENCE:
		st.appendString("SEQUENCE ")
	case ast.ObjectType_OBJECT_STATISTIC_EXT:
		st.appendString("STATISTICS ")
	case ast.ObjectType_OBJECT_TABLE:
		st.appendString("TABLE ")
	case ast.ObjectType_OBJECT_TRANSFORM:
		st.appendString("TRANSFORM ")
	case ast.ObjectType_OBJECT_TRIGGER:
		st.appendString("TRIGGER ")
	case ast.ObjectType_OBJECT_TSCONFIGURATION:
		st.appendString("TEXT SEARCH CONFIGURATION ")
	case ast.ObjectType_OBJECT_TSDICTIONARY:
		st.appendString("TEXT SEARCH DICTIONARY ")
	case ast.ObjectType_OBJECT_TSPARSER:
		st.appendString("TEXT SEARCH PARSER ")
	case ast.ObjectType_OBJECT_TSTEMPLATE:
		st.appendString("TEXT SEARCH TEMPLATE ")
	case ast.ObjectType_OBJECT_TYPE:
		st.appendString("TYPE ")
	case ast.ObjectType_OBJECT_VIEW:
		st.appendString("VIEW ")
	default:
		// Other object types are not supported here in the parser
	}

	if drop_stmt.Concurrent {
		st.appendString("CONCURRENTLY ")
	}

	if drop_stmt.MissingOk {
		st.appendString("IF EXISTS ")
	}

	switch drop_stmt.RemoveType {
	// drop_type_any_name
	case ast.ObjectType_OBJECT_TABLE,
		ast.ObjectType_OBJECT_SEQUENCE,
		ast.ObjectType_OBJECT_VIEW,
		ast.ObjectType_OBJECT_MATVIEW,
		ast.ObjectType_OBJECT_INDEX,
		ast.ObjectType_OBJECT_FOREIGN_TABLE,
		ast.ObjectType_OBJECT_COLLATION,
		ast.ObjectType_OBJECT_CONVERSION,
		ast.ObjectType_OBJECT_STATISTIC_EXT,
		ast.ObjectType_OBJECT_TSPARSER,
		ast.ObjectType_OBJECT_TSDICTIONARY,
		ast.ObjectType_OBJECT_TSTEMPLATE,
		ast.ObjectType_OBJECT_TSCONFIGURATION:
		deparseAnyNameList(st, drop_stmt.Objects)
		st.appendChar(' ')
	// drop_type_name
	case ast.ObjectType_OBJECT_ACCESS_METHOD,
		ast.ObjectType_OBJECT_EVENT_TRIGGER,
		ast.ObjectType_OBJECT_EXTENSION,
		ast.ObjectType_OBJECT_FDW,
		ast.ObjectType_OBJECT_PUBLICATION,
		ast.ObjectType_OBJECT_SCHEMA,
		ast.ObjectType_OBJECT_FOREIGN_SERVER:
		deparseNameList(st, drop_stmt.Objects)
		st.appendChar(' ')
	// drop_type_name_on_any_name
	case ast.ObjectType_OBJECT_POLICY,
		ast.ObjectType_OBJECT_RULE,
		ast.ObjectType_OBJECT_TRIGGER:
		l = drop_stmt.Objects[0].GetList().Items
		deparseColId(st, strVal(l[len(l)-1]))
		st.appendString(" ON ")
		deparseAnyNameSkipLast(st, l)
		st.appendChar(' ')
	case ast.ObjectType_OBJECT_CAST:
		l = drop_stmt.Objects[0].GetList().Items
		st.appendChar('(')
		deparseTypeName(st, l[0].GetTypeName())
		st.appendString(" AS ")
		deparseTypeName(st, l[1].GetTypeName())
		st.appendChar(')')
		st.appendChar(' ')
	case ast.ObjectType_OBJECT_OPFAMILY,
		ast.ObjectType_OBJECT_OPCLASS:
		l = drop_stmt.Objects[0].GetList().Items
		deparseAnyNameSkipFirst(st, l)
		st.appendString(" USING ")
		deparseColId(st, strVal(l[0]))
		st.appendChar(' ')
	case ast.ObjectType_OBJECT_TRANSFORM:
		l = drop_stmt.Objects[0].GetList().Items
		st.appendString("FOR ")
		deparseTypeName(st, l[0].GetTypeName())
		st.appendString(" LANGUAGE ")
		deparseColId(st, strVal(l[1]))
		st.appendChar(' ')
	case ast.ObjectType_OBJECT_LANGUAGE:
		deparseNameList(st, drop_stmt.Objects)
		st.appendChar(' ')
	case ast.ObjectType_OBJECT_TYPE,
		ast.ObjectType_OBJECT_DOMAIN:
		for i, item := range drop_stmt.Objects {
			deparseTypeName(st, item.GetTypeName())
			if i < len(drop_stmt.Objects)-1 {
				st.appendString(", ")
			}
		}
		st.appendChar(' ')
	case ast.ObjectType_OBJECT_AGGREGATE:
		for i, item := range drop_stmt.Objects {
			deparseAggregateWithArgtypes(st, item.GetObjectWithArgs())
			if i < len(drop_stmt.Objects)-1 {
				st.appendString(", ")
			}
		}
		st.appendChar(' ')
	case ast.ObjectType_OBJECT_FUNCTION,
		ast.ObjectType_OBJECT_PROCEDURE,
		ast.ObjectType_OBJECT_ROUTINE:
		for i, item := range drop_stmt.Objects {
			deparseFunctionWithArgtypes(st, item.GetObjectWithArgs())
			if i < len(drop_stmt.Objects)-1 {
				st.appendString(", ")
			}
		}
		st.appendChar(' ')
	case ast.ObjectType_OBJECT_OPERATOR:
		for i, item := range drop_stmt.Objects {
			deparseOperatorWithArgtypes(st, item.GetObjectWithArgs())
			if i < len(drop_stmt.Objects)-1 {
				st.appendString(", ")
			}
		}
		st.appendChar(' ')
	default:
	}

	deparseOptDropBehavior(st, drop_stmt.Behavior)

	st.removeTrailingSpace()
}
