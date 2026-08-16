package parse

// Statement dispatch for the CREATE/ALTER/DROP families. gram.y keeps every
// alternative alive in the LALR tables; the recursive-descent port picks the
// production from one or two tokens of lookahead past the head keyword.

import (
	"github.com/sqlc-dev/oliphant/ast"
)

// parseCreateDispatch routes a statement beginning with CREATE. The CREATE
// token is not yet consumed.
func (p *parser) parseCreateDispatch() *ast.Node {
	p.expect(ast.Token_CREATE)
	switch p.kind() {
	case ast.Token_ROLE:
		return p.parseCreateRoleStmt(ast.RoleStmtType_ROLESTMT_ROLE)
	case ast.Token_USER:
		if p.kindN(1) == ast.Token_MAPPING {
			break // CreateUserMappingStmt (milestone 7)
		}
		return p.parseCreateRoleStmt(ast.RoleStmtType_ROLESTMT_USER)
	case ast.Token_GROUP_P:
		return p.parseCreateRoleStmt(ast.RoleStmtType_ROLESTMT_GROUP)
	case ast.Token_SCHEMA:
		p.next()
		return p.parseCreateSchemaStmt()
	}
	if stmt := p.parseCreateStmtFamily(false); stmt != nil {
		return stmt
	}
	p.syntaxErrorAt()
	return nil
}

// parseCreateStmtFamily handles the CREATE statements that can also appear
// as CREATE SCHEMA elements (CreateStmt, IndexStmt, CreateSeqStmt,
// CreateTrigStmt, ViewStmt) plus, when schemaElt is false, the rest of the
// CREATE family. The CREATE token has been consumed. Returns nil when the
// lookahead matches nothing implemented.
func (p *parser) parseCreateStmtFamily(schemaElt bool) *ast.Node {
	switch p.kind() {
	case ast.Token_TEMPORARY, ast.Token_TEMP, ast.Token_LOCAL,
		ast.Token_GLOBAL, ast.Token_UNLOGGED:
		// OptTemp: heads CreateStmt, CreateAsStmt, ViewStmt, CreateSeqStmt.
		return p.parseOptTempHeadedCreate(schemaElt)
	case ast.Token_TABLE:
		return p.parseCreateTableOrAs(p.parseOptTempAfterCreate())
	case ast.Token_UNIQUE:
		if p.kindN(1) == ast.Token_INDEX {
			return p.parseIndexStmt()
		}
	case ast.Token_INDEX:
		return p.parseIndexStmt()
	case ast.Token_SEQUENCE:
		return p.parseCreateSeqStmt(relPersistPermanent)
	case ast.Token_VIEW:
		return p.parseViewStmt(false, relPersistPermanent)
	case ast.Token_RECURSIVE:
		if p.kindN(1) == ast.Token_VIEW {
			return p.parseViewStmt(false, relPersistPermanent)
		}
	case ast.Token_OR:
		if p.kindN(1) == ast.Token_REPLACE {
			return p.parseCreateOrReplace(schemaElt)
		}
	case ast.Token_TRIGGER:
		return p.parseCreateTrigStmt(false, false)
	case ast.Token_CONSTRAINT:
		if p.kindN(1) == ast.Token_TRIGGER {
			p.next()
			return p.parseCreateTrigStmt(false, true)
		}
	case ast.Token_MATERIALIZED:
		if !schemaElt {
			return p.parseCreateMatViewStmt(relPersistPermanent)
		}
	}
	return nil
}

// parseCreateOrReplace handles CREATE OR REPLACE ...; CREATE is consumed,
// OR REPLACE not yet.
func (p *parser) parseCreateOrReplace(schemaElt bool) *ast.Node {
	p.next() // OR
	p.next() // REPLACE
	switch p.kind() {
	case ast.Token_TEMPORARY, ast.Token_TEMP, ast.Token_LOCAL,
		ast.Token_GLOBAL, ast.Token_UNLOGGED:
		persistence := p.parseOptTempAfterCreate()
		if p.kind() == ast.Token_VIEW || (p.kind() == ast.Token_RECURSIVE && p.kindN(1) == ast.Token_VIEW) {
			return p.parseViewStmt(true, persistence)
		}
	case ast.Token_VIEW, ast.Token_RECURSIVE:
		if p.kind() == ast.Token_VIEW || p.kindN(1) == ast.Token_VIEW {
			return p.parseViewStmt(true, relPersistPermanent)
		}
	case ast.Token_TRIGGER:
		return p.parseCreateTrigStmt(true, false)
	case ast.Token_CONSTRAINT:
		if p.kindN(1) == ast.Token_TRIGGER {
			p.next()
			return p.parseCreateTrigStmt(true, true)
		}
	}
	p.syntaxErrorAt()
	return nil
}

// parseOptTempHeadedCreate parses a CREATE statement whose next token is an
// OptTemp keyword: CREATE [OptTemp] TABLE/SEQUENCE/VIEW.
func (p *parser) parseOptTempHeadedCreate(schemaElt bool) *ast.Node {
	persistence := p.parseOptTempAfterCreate()
	switch p.kind() {
	case ast.Token_TABLE:
		return p.parseCreateTableOrAs(persistence)
	case ast.Token_SEQUENCE:
		return p.parseCreateSeqStmt(persistence)
	case ast.Token_VIEW:
		return p.parseViewStmt(false, persistence)
	case ast.Token_RECURSIVE:
		if p.kindN(1) == ast.Token_VIEW {
			return p.parseViewStmt(false, persistence)
		}
	case ast.Token_MATERIALIZED:
		// CREATE UNLOGGED MATERIALIZED VIEW (OptNoLog)
		if persistence == relPersistUnlogged {
			return p.parseCreateMatViewStmt(persistence)
		}
	}
	p.syntaxErrorAt()
	return nil
}

// parseOptTempAfterCreate is gram.y's OptTemp, with TABLE (or SEQUENCE or
// VIEW) still ahead.
func (p *parser) parseOptTempAfterCreate() string {
	switch p.kind() {
	case ast.Token_TEMPORARY, ast.Token_TEMP:
		p.next()
		return relPersistTemp
	case ast.Token_LOCAL:
		p.next()
		switch p.kind() {
		case ast.Token_TEMPORARY, ast.Token_TEMP:
			p.next()
			return relPersistTemp
		}
		p.syntaxErrorAt()
	case ast.Token_GLOBAL:
		// GLOBAL TEMPORARY/TEMP: deprecated but accepted; gram.y warns and
		// treats it as TEMP (a warning is not part of the parse result).
		p.next()
		switch p.kind() {
		case ast.Token_TEMPORARY, ast.Token_TEMP:
			p.next()
			return relPersistTemp
		}
		p.syntaxErrorAt()
	case ast.Token_UNLOGGED:
		p.next()
		return relPersistUnlogged
	}
	return relPersistPermanent
}

// parseAlterDispatch routes a statement beginning with ALTER. The ALTER
// token is not yet consumed.
func (p *parser) parseAlterDispatch() *ast.Node {
	p.expect(ast.Token_ALTER)
	switch p.kind() {
	case ast.Token_ROLE, ast.Token_USER:
		if p.kindN(1) == ast.Token_MAPPING {
			break // AlterUserMappingStmt (milestone 7)
		}
		p.next()
		return p.parseAlterRoleStmt()
	case ast.Token_GROUP_P:
		p.next()
		return p.parseAlterGroupStmt()
	case ast.Token_TABLE:
		p.next()
		return p.parseAlterTableStmt(ast.ObjectType_OBJECT_TABLE, true, true)
	case ast.Token_INDEX:
		p.next()
		return p.parseAlterTableStmt(ast.ObjectType_OBJECT_INDEX, false, true)
	case ast.Token_VIEW:
		p.next()
		return p.parseAlterTableStmt(ast.ObjectType_OBJECT_VIEW, false, false)
	case ast.Token_MATERIALIZED:
		if p.kindN(1) == ast.Token_VIEW {
			p.next()
			p.next()
			return p.parseAlterTableStmt(ast.ObjectType_OBJECT_MATVIEW, false, true)
		}
	case ast.Token_FOREIGN:
		if p.kindN(1) == ast.Token_TABLE {
			p.next()
			p.next()
			return p.parseAlterTableStmt(ast.ObjectType_OBJECT_FOREIGN_TABLE, true, false)
		}
	case ast.Token_SEQUENCE:
		// AlterSeqStmt (SeqOptList) and AlterTableStmt (alter_table_cmds)
		// share the prefix through the sequence name; the next token picks
		// the production, exactly as the LALR tables do.
		p.next()
		missingOk := false
		if p.have(ast.Token_IF_P) {
			p.expect(ast.Token_EXISTS)
			missingOk = true
		}
		rel := p.parseQualifiedName()
		if p.seqOptElemStarts() {
			n := &ast.AlterSeqStmt{Sequence: rel, MissingOk: missingOk}
			n.Options = p.parseSeqOptList()
			return &ast.Node{Node: &ast.Node_AlterSeqStmt{AlterSeqStmt: n}}
		}
		n := &ast.AlterTableStmt{
			Objtype:   ast.ObjectType_OBJECT_SEQUENCE,
			Relation:  rel,
			MissingOk: missingOk,
		}
		n.Cmds = p.parseAlterTableCmds()
		return &ast.Node{Node: &ast.Node_AlterTableStmt{AlterTableStmt: n}}
	case ast.Token_TYPE_P:
		p.next()
		return p.parseAlterTypeDispatch()
	}
	p.syntaxErrorAt()
	return nil
}

// parseDropDispatch routes a statement beginning with DROP. The DROP token
// is not yet consumed.
func (p *parser) parseDropDispatch() *ast.Node {
	p.expect(ast.Token_DROP)
	switch p.kind() {
	case ast.Token_ROLE, ast.Token_GROUP_P:
		p.next()
		return p.parseDropRoleStmt()
	case ast.Token_USER:
		if p.kindN(1) == ast.Token_MAPPING {
			break // DropUserMappingStmt (milestone 7)
		}
		p.next()
		return p.parseDropRoleStmt()
	}
	p.syntaxErrorAt()
	return nil
}

// RangeVar.relpersistence values (pg_class.h: RELPERSISTENCE_*).
const (
	relPersistPermanent = "p"
	relPersistTemp      = "t"
	relPersistUnlogged  = "u"
)

// Stubs for productions landing later in milestones 6-7; each fails with
// the syntax error the dispatcher would otherwise have raised.
func (p *parser) parseCreateTrigStmt(orReplace, isConstraint bool) *ast.Node {
	p.syntaxErrorAt()
	return nil
}

func (p *parser) parseGrantStmt() *ast.Node {
	p.syntaxErrorAt()
	return nil
}
