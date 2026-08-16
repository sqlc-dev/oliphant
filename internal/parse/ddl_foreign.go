package parse

// Foreign-data DDL: CreateFdwStmt, AlterFdwStmt, CreateForeignServerStmt,
// AlterForeignServerStmt, CreateForeignTableStmt, ImportForeignSchemaStmt,
// CreateUserMappingStmt, AlterUserMappingStmt.

import (
	"github.com/sqlc-dev/oliphant/ast"
)

// parseOptFdwOptions is gram.y's opt_fdw_options.
func (p *parser) parseOptFdwOptions() []*ast.Node {
	var list []*ast.Node
	for {
		tok := p.peek()
		switch tok.Kind {
		case ast.Token_HANDLER:
			p.next()
			list = append(list, makeDefElem("handler", nList(p.parseHandlerName()), tok.Start))
		case ast.Token_VALIDATOR:
			p.next()
			list = append(list, makeDefElem("validator", nList(p.parseHandlerName()), tok.Start))
		case ast.Token_NO:
			switch p.kindN(1) {
			case ast.Token_HANDLER:
				p.next()
				p.next()
				list = append(list, makeDefElem("handler", nil, tok.Start))
			case ast.Token_VALIDATOR:
				p.next()
				p.next()
				list = append(list, makeDefElem("validator", nil, tok.Start))
			default:
				return list
			}
		default:
			return list
		}
	}
}

// parseCreateFdwStmt is gram.y's CreateFdwStmt; CREATE FOREIGN DATA
// WRAPPER consumed.
func (p *parser) parseCreateFdwStmt() *ast.Node {
	n := &ast.CreateFdwStmt{Fdwname: p.name()}
	n.FuncOptions = p.parseOptFdwOptions()
	n.Options = p.parseCreateGenericOptions()
	return &ast.Node{Node: &ast.Node_CreateFdwStmt{CreateFdwStmt: n}}
}

// parseAlterFdwStmt is gram.y's AlterFdwStmt; ALTER FOREIGN DATA WRAPPER
// consumed. RENAME TO/OWNER TO are resolved by the caller.
func (p *parser) parseAlterFdwStmt(name string) *ast.Node {
	n := &ast.AlterFdwStmt{Fdwname: name}
	n.FuncOptions = p.parseOptFdwOptions()
	if p.kind() == ast.Token_OPTIONS {
		n.Options = p.parseAlterGenericOptions()
	} else if n.FuncOptions == nil {
		p.syntaxErrorAt()
	}
	return &ast.Node{Node: &ast.Node_AlterFdwStmt{AlterFdwStmt: n}}
}

// parseCreateForeignServerStmt is gram.y's CreateForeignServerStmt; CREATE
// SERVER consumed.
func (p *parser) parseCreateForeignServerStmt() *ast.Node {
	n := &ast.CreateForeignServerStmt{}
	if p.have(ast.Token_IF_P) {
		p.expect(ast.Token_NOT)
		p.expect(ast.Token_EXISTS)
		n.IfNotExists = true
	}
	n.Servername = p.name()
	if p.have(ast.Token_TYPE_P) {
		n.Servertype = p.sconst()
	}
	if p.have(ast.Token_VERSION_P) {
		if !p.have(ast.Token_NULL_P) {
			n.Version = p.sconst()
		}
	}
	p.expect(ast.Token_FOREIGN)
	p.expect(ast.Token_DATA_P)
	p.expect(ast.Token_WRAPPER)
	n.Fdwname = p.name()
	n.Options = p.parseCreateGenericOptions()
	return &ast.Node{Node: &ast.Node_CreateForeignServerStmt{CreateForeignServerStmt: n}}
}

// parseAlterForeignServerStmt is gram.y's AlterForeignServerStmt; ALTER
// SERVER name consumed.
func (p *parser) parseAlterForeignServerStmt(name string) *ast.Node {
	n := &ast.AlterForeignServerStmt{Servername: name}
	if p.have(ast.Token_VERSION_P) {
		n.HasVersion = true
		if !p.have(ast.Token_NULL_P) {
			n.Version = p.sconst()
		}
		if p.kind() == ast.Token_OPTIONS {
			n.Options = p.parseAlterGenericOptions()
		}
	} else {
		n.Options = p.parseAlterGenericOptions()
	}
	return &ast.Node{Node: &ast.Node_AlterForeignServerStmt{AlterForeignServerStmt: n}}
}

// parseCreateForeignTableStmt is gram.y's CreateForeignTableStmt; CREATE
// FOREIGN TABLE consumed.
func (p *parser) parseCreateForeignTableStmt() *ast.Node {
	n := &ast.CreateForeignTableStmt{}
	base := &ast.CreateStmt{Oncommit: ast.OnCommitAction_ONCOMMIT_NOOP}
	if p.have(ast.Token_IF_P) {
		p.expect(ast.Token_NOT)
		p.expect(ast.Token_EXISTS)
		base.IfNotExists = true
	}
	base.Relation = p.parseQualifiedName()
	base.Relation.Relpersistence = relPersistPermanent
	if p.have(ast.Token_PARTITION) {
		p.expect(ast.Token_OF)
		base.InhRelations = []*ast.Node{nRangeVar(p.parseQualifiedName())}
		if p.have(ast.Token('(')) {
			base.TableElts = p.parseTypedTableElementList()
			p.expect(ast.Token(')'))
		}
		base.Partbound = p.parsePartitionBoundSpec()
	} else {
		p.expect(ast.Token('('))
		if p.kind() != ast.Token(')') {
			base.TableElts = p.parseTableElementList()
		}
		p.expect(ast.Token(')'))
		if p.have(ast.Token_INHERITS) {
			p.expect(ast.Token('('))
			base.InhRelations = p.parseQualifiedNameList()
			p.expect(ast.Token(')'))
		}
	}
	p.expect(ast.Token_SERVER)
	n.Servername = p.name()
	n.Options = p.parseCreateGenericOptions()
	n.BaseStmt = base
	return &ast.Node{Node: &ast.Node_CreateForeignTableStmt{CreateForeignTableStmt: n}}
}

// parseImportForeignSchemaStmt is gram.y's ImportForeignSchemaStmt.
func (p *parser) parseImportForeignSchemaStmt() *ast.Node {
	p.expect(ast.Token_IMPORT_P)
	p.expect(ast.Token_FOREIGN)
	p.expect(ast.Token_SCHEMA)
	n := &ast.ImportForeignSchemaStmt{RemoteSchema: p.name()}
	// import_qualification
	switch {
	case p.have(ast.Token_LIMIT):
		p.expect(ast.Token_TO)
		n.ListType = ast.ImportForeignSchemaType_FDW_IMPORT_SCHEMA_LIMIT_TO
		p.expect(ast.Token('('))
		n.TableList = p.parseRelationExprList()
		p.expect(ast.Token(')'))
	case p.have(ast.Token_EXCEPT):
		n.ListType = ast.ImportForeignSchemaType_FDW_IMPORT_SCHEMA_EXCEPT
		p.expect(ast.Token('('))
		n.TableList = p.parseRelationExprList()
		p.expect(ast.Token(')'))
	default:
		n.ListType = ast.ImportForeignSchemaType_FDW_IMPORT_SCHEMA_ALL
	}
	p.expect(ast.Token_FROM)
	p.expect(ast.Token_SERVER)
	n.ServerName = p.name()
	p.expect(ast.Token_INTO)
	n.LocalSchema = p.name()
	n.Options = p.parseCreateGenericOptions()
	return &ast.Node{Node: &ast.Node_ImportForeignSchemaStmt{ImportForeignSchemaStmt: n}}
}

// parseCreateUserMappingStmt is gram.y's CreateUserMappingStmt; CREATE
// USER MAPPING consumed.
func (p *parser) parseCreateUserMappingStmt() *ast.Node {
	n := &ast.CreateUserMappingStmt{}
	if p.have(ast.Token_IF_P) {
		p.expect(ast.Token_NOT)
		p.expect(ast.Token_EXISTS)
		n.IfNotExists = true
	}
	p.expect(ast.Token_FOR)
	n.User = p.parseAuthIdent()
	p.expect(ast.Token_SERVER)
	n.Servername = p.name()
	n.Options = p.parseCreateGenericOptions()
	return &ast.Node{Node: &ast.Node_CreateUserMappingStmt{CreateUserMappingStmt: n}}
}

// parseAlterUserMappingStmt is gram.y's AlterUserMappingStmt; ALTER USER
// MAPPING consumed.
func (p *parser) parseAlterUserMappingStmt() *ast.Node {
	p.expect(ast.Token_FOR)
	n := &ast.AlterUserMappingStmt{User: p.parseAuthIdent()}
	p.expect(ast.Token_SERVER)
	n.Servername = p.name()
	n.Options = p.parseAlterGenericOptions()
	return &ast.Node{Node: &ast.Node_AlterUserMappingStmt{AlterUserMappingStmt: n}}
}
