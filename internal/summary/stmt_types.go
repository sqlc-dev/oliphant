package summary

import (
	"github.com/sqlc-dev/oliphant/ast"
	"github.com/sqlc-dev/oliphant/internal/rawwalk"
)

// addStmtType is add_stmt_type_if_needed: an insertion-ordered set.
func (s *state) addStmtType(name string) {
	for _, t := range s.statementTypes {
		if t == name {
			return
		}
	}
	s.statementTypes = append(s.statementTypes, name)
}

// stmtWalk is pg_query_summary_stmt_walk_impl
// (pg_query_summary_statement_type.c): record the statement type of every
// node the raw walker can reach, with explicit sub-walks for the four
// utility statements that wrap a query but are not raw-walkable themselves.
func (s *state) stmtWalk(item any) bool {
	c := rawwalk.Concrete(item)
	if rawwalk.IsNil(c) {
		return false
	}

	switch v := c.(type) {
	case *ast.RawStmt:
		return s.stmtWalk(v.Stmt)
	case *ast.CreateTableAsStmt:
		s.addStmtType("CreateTableAsStmt")
		if v.Query != nil {
			s.stmtWalk(v.Query)
		}
	case *ast.ExplainStmt:
		s.addStmtType("ExplainStmt")
		if v.Query != nil {
			s.stmtWalk(v.Query)
		}
	case *ast.PrepareStmt:
		s.addStmtType("PrepareStmt")
		if v.Query != nil {
			s.stmtWalk(v.Query)
		}
	case *ast.ViewStmt:
		s.addStmtType("ViewStmt")
		if v.Query != nil {
			s.stmtWalk(v.Query)
		}
	case *ast.AlterCollationStmt:
		s.addStmtType("AlterCollationStmt")
	case *ast.AlterDatabaseSetStmt:
		s.addStmtType("AlterDatabaseSetStmt")
	case *ast.AlterDatabaseStmt:
		s.addStmtType("AlterDatabaseStmt")
	case *ast.AlterDefaultPrivilegesStmt:
		s.addStmtType("AlterDefaultPrivilegesStmt")
	case *ast.AlterDomainStmt:
		s.addStmtType("AlterDomainStmt")
	case *ast.AlterEnumStmt:
		s.addStmtType("AlterEnumStmt")
	case *ast.AlterEventTrigStmt:
		s.addStmtType("AlterEventTrigStmt")
	case *ast.AlterExtensionContentsStmt:
		s.addStmtType("AlterExtensionContentsStmt")
	case *ast.AlterExtensionStmt:
		s.addStmtType("AlterExtensionStmt")
	case *ast.AlterFdwStmt:
		s.addStmtType("AlterFdwStmt")
	case *ast.AlterForeignServerStmt:
		s.addStmtType("AlterForeignServerStmt")
	case *ast.AlterFunctionStmt:
		s.addStmtType("AlterFunctionStmt")
	case *ast.AlterObjectDependsStmt:
		s.addStmtType("AlterObjectDependsStmt")
	case *ast.AlterObjectSchemaStmt:
		s.addStmtType("AlterObjectSchemaStmt")
	case *ast.AlterOpFamilyStmt:
		s.addStmtType("AlterOpFamilyStmt")
	case *ast.AlterOperatorStmt:
		s.addStmtType("AlterOperatorStmt")
	case *ast.AlterOwnerStmt:
		s.addStmtType("AlterOwnerStmt")
	case *ast.AlterPolicyStmt:
		s.addStmtType("AlterPolicyStmt")
	case *ast.AlterPublicationStmt:
		s.addStmtType("AlterPublicationStmt")
	case *ast.AlterRoleSetStmt:
		s.addStmtType("AlterRoleSetStmt")
	case *ast.AlterRoleStmt:
		s.addStmtType("AlterRoleStmt")
	case *ast.AlterSeqStmt:
		s.addStmtType("AlterSeqStmt")
	case *ast.AlterStatsStmt:
		s.addStmtType("AlterStatsStmt")
	case *ast.AlterSubscriptionStmt:
		s.addStmtType("AlterSubscriptionStmt")
	case *ast.AlterSystemStmt:
		s.addStmtType("AlterSystemStmt")
	case *ast.AlterTableCmd:
		s.addStmtType("AlterTableCmd")
	case *ast.AlterTableMoveAllStmt:
		s.addStmtType("AlterTableMoveAllStmt")
	case *ast.AlterTableSpaceOptionsStmt:
		s.addStmtType("AlterTableSpaceOptionsStmt")
	case *ast.AlterTableStmt:
		s.addStmtType("AlterTableStmt")
	case *ast.AlterTSConfigurationStmt:
		s.addStmtType("AlterTSConfigurationStmt")
	case *ast.AlterTSDictionaryStmt:
		s.addStmtType("AlterTSDictionaryStmt")
	case *ast.AlterTypeStmt:
		s.addStmtType("AlterTypeStmt")
	case *ast.AlterUserMappingStmt:
		s.addStmtType("AlterUserMappingStmt")
	case *ast.CallStmt:
		s.addStmtType("CallStmt")
	case *ast.CheckPointStmt:
		s.addStmtType("CheckPointStmt")
	case *ast.ClosePortalStmt:
		s.addStmtType("ClosePortalStmt")
	case *ast.ClusterStmt:
		s.addStmtType("ClusterStmt")
	case *ast.CommentStmt:
		s.addStmtType("CommentStmt")
	case *ast.CompositeTypeStmt:
		s.addStmtType("CompositeTypeStmt")
	case *ast.ConstraintsSetStmt:
		s.addStmtType("ConstraintsSetStmt")
	case *ast.CopyStmt:
		s.addStmtType("CopyStmt")
	case *ast.CreateAmStmt:
		s.addStmtType("CreateAmStmt")
	case *ast.CreateCastStmt:
		s.addStmtType("CreateCastStmt")
	case *ast.CreateConversionStmt:
		s.addStmtType("CreateConversionStmt")
	case *ast.CreateDomainStmt:
		s.addStmtType("CreateDomainStmt")
	case *ast.CreateEnumStmt:
		s.addStmtType("CreateEnumStmt")
	case *ast.CreateEventTrigStmt:
		s.addStmtType("CreateEventTrigStmt")
	case *ast.CreateExtensionStmt:
		s.addStmtType("CreateExtensionStmt")
	case *ast.CreateFdwStmt:
		s.addStmtType("CreateFdwStmt")
	case *ast.CreateForeignServerStmt:
		s.addStmtType("CreateForeignServerStmt")
	case *ast.CreateForeignTableStmt:
		s.addStmtType("CreateForeignTableStmt")
	case *ast.CreateFunctionStmt:
		s.addStmtType("CreateFunctionStmt")
	case *ast.CreateOpClassStmt:
		s.addStmtType("CreateOpClassStmt")
	case *ast.CreateOpFamilyStmt:
		s.addStmtType("CreateOpFamilyStmt")
	case *ast.CreatePLangStmt:
		s.addStmtType("CreatePLangStmt")
	case *ast.CreatePolicyStmt:
		s.addStmtType("CreatePolicyStmt")
	case *ast.CreatePublicationStmt:
		s.addStmtType("CreatePublicationStmt")
	case *ast.CreateRangeStmt:
		s.addStmtType("CreateRangeStmt")
	case *ast.CreateRoleStmt:
		s.addStmtType("CreateRoleStmt")
	case *ast.CreateSchemaStmt:
		s.addStmtType("CreateSchemaStmt")
	case *ast.CreateSeqStmt:
		s.addStmtType("CreateSeqStmt")
	case *ast.CreateStatsStmt:
		s.addStmtType("CreateStatsStmt")
	case *ast.CreateStmt:
		s.addStmtType("CreateStmt")
	case *ast.CreateSubscriptionStmt:
		s.addStmtType("CreateSubscriptionStmt")
	case *ast.CreateTableSpaceStmt:
		s.addStmtType("CreateTableSpaceStmt")
	case *ast.CreateTransformStmt:
		s.addStmtType("CreateTransformStmt")
	case *ast.CreateTrigStmt:
		s.addStmtType("CreateTrigStmt")
	case *ast.CreateUserMappingStmt:
		s.addStmtType("CreateUserMappingStmt")
	case *ast.CreatedbStmt:
		s.addStmtType("CreatedbStmt")
	case *ast.DeallocateStmt:
		s.addStmtType("DeallocateStmt")
	case *ast.DeclareCursorStmt:
		s.addStmtType("DeclareCursorStmt")
	case *ast.DefineStmt:
		s.addStmtType("DefineStmt")
	case *ast.DeleteStmt:
		s.addStmtType("DeleteStmt")
	case *ast.DiscardStmt:
		s.addStmtType("DiscardStmt")
	case *ast.DoStmt:
		s.addStmtType("DoStmt")
	case *ast.DropOwnedStmt:
		s.addStmtType("DropOwnedStmt")
	case *ast.DropRoleStmt:
		s.addStmtType("DropRoleStmt")
	case *ast.DropStmt:
		s.addStmtType("DropStmt")
	case *ast.DropSubscriptionStmt:
		s.addStmtType("DropSubscriptionStmt")
	case *ast.DropTableSpaceStmt:
		s.addStmtType("DropTableSpaceStmt")
	case *ast.DropUserMappingStmt:
		s.addStmtType("DropUserMappingStmt")
	case *ast.DropdbStmt:
		s.addStmtType("DropdbStmt")
	case *ast.ExecuteStmt:
		s.addStmtType("ExecuteStmt")
	case *ast.FetchStmt:
		s.addStmtType("FetchStmt")
	case *ast.GrantRoleStmt:
		s.addStmtType("GrantRoleStmt")
	case *ast.GrantStmt:
		s.addStmtType("GrantStmt")
	case *ast.ImportForeignSchemaStmt:
		s.addStmtType("ImportForeignSchemaStmt")
	case *ast.IndexStmt:
		s.addStmtType("IndexStmt")
	case *ast.InsertStmt:
		s.addStmtType("InsertStmt")
	case *ast.ListenStmt:
		s.addStmtType("ListenStmt")
	case *ast.LoadStmt:
		s.addStmtType("LoadStmt")
	case *ast.LockStmt:
		s.addStmtType("LockStmt")
	case *ast.MergeStmt:
		s.addStmtType("MergeStmt")
	case *ast.NotifyStmt:
		s.addStmtType("NotifyStmt")
	case *ast.ReassignOwnedStmt:
		s.addStmtType("ReassignOwnedStmt")
	case *ast.RefreshMatViewStmt:
		s.addStmtType("RefreshMatViewStmt")
	case *ast.ReindexStmt:
		s.addStmtType("ReindexStmt")
	case *ast.RenameStmt:
		s.addStmtType("RenameStmt")
	case *ast.ReplicaIdentityStmt:
		s.addStmtType("ReplicaIdentityStmt")
	case *ast.RuleStmt:
		s.addStmtType("RuleStmt")
	case *ast.SecLabelStmt:
		s.addStmtType("SecLabelStmt")
	case *ast.SelectStmt:
		s.addStmtType("SelectStmt")
	case *ast.SetOperationStmt:
		s.addStmtType("SetOperationStmt")
	case *ast.TransactionStmt:
		s.addStmtType("TransactionStmt")
	case *ast.TruncateStmt:
		s.addStmtType("TruncateStmt")
	case *ast.UnlistenStmt:
		s.addStmtType("UnlistenStmt")
	case *ast.UpdateStmt:
		s.addStmtType("UpdateStmt")
	case *ast.VacuumStmt:
		s.addStmtType("VacuumStmt")
	case *ast.VariableSetStmt:
		s.addStmtType("VariableSetStmt")
	case *ast.VariableShowStmt:
		s.addStmtType("VariableShowStmt")
	}

	return rawwalk.WalkChildren(item, s.stmtWalk)
}
