package parse

// Role and schema DDL: CreateRoleStmt/CreateUserStmt/CreateGroupStmt,
// AlterRoleStmt, AlterRoleSetStmt, AlterGroupStmt, DropRoleStmt,
// CreateSchemaStmt, plus RoleSpec/RoleId/role_list and CallStmt.

import (
	"fmt"

	"github.com/sqlc-dev/oliphant/ast"
)

// parseCallStmt is gram.y's CallStmt: CALL func_application.
func (p *parser) parseCallStmt() *ast.Node {
	p.expect(ast.Token_CALL)
	tok := p.peek()
	name := p.parseFuncName(tok)
	fn := p.parseFuncApplicationNoDecoration(name, tok)
	return &ast.Node{Node: &ast.Node_CallStmt{CallStmt: &ast.CallStmt{
		Funccall: asFuncCall(fn),
	}}}
}

// parseRoleSpec is gram.y's RoleSpec.
func (p *parser) parseRoleSpec() *ast.RoleSpec {
	tok := p.peek()
	switch tok.Kind {
	case ast.Token_CURRENT_ROLE:
		p.next()
		return &ast.RoleSpec{Roletype: ast.RoleSpecType_ROLESPEC_CURRENT_ROLE, Location: tok.Start}
	case ast.Token_CURRENT_USER:
		p.next()
		return &ast.RoleSpec{Roletype: ast.RoleSpecType_ROLESPEC_CURRENT_USER, Location: tok.Start}
	case ast.Token_SESSION_USER:
		p.next()
		return &ast.RoleSpec{Roletype: ast.RoleSpecType_ROLESPEC_SESSION_USER, Location: tok.Start}
	}
	name := p.nonReservedWord()
	// gram.y: "public" and "none" are not keywords, but are special here.
	switch name {
	case "public":
		return &ast.RoleSpec{Roletype: ast.RoleSpecType_ROLESPEC_PUBLIC, Location: tok.Start}
	case "none":
		p.ereport("base_yyparse", `role name "none" is reserved`, tok.Start)
	}
	return &ast.RoleSpec{
		Roletype: ast.RoleSpecType_ROLESPEC_CSTRING,
		Rolename: name,
		Location: tok.Start,
	}
}

// parseRoleId is gram.y's RoleId: a RoleSpec restricted to plain names.
func (p *parser) parseRoleId() string {
	tok := p.peek()
	spec := p.parseRoleSpec()
	switch spec.Roletype {
	case ast.RoleSpecType_ROLESPEC_CSTRING:
		return spec.Rolename
	case ast.RoleSpecType_ROLESPEC_PUBLIC:
		p.ereport("base_yyparse", `role name "public" is reserved`, tok.Start)
	case ast.RoleSpecType_ROLESPEC_SESSION_USER:
		p.ereport("base_yyparse", "SESSION_USER cannot be used as a role name here", tok.Start)
	case ast.RoleSpecType_ROLESPEC_CURRENT_USER:
		p.ereport("base_yyparse", "CURRENT_USER cannot be used as a role name here", tok.Start)
	case ast.RoleSpecType_ROLESPEC_CURRENT_ROLE:
		p.ereport("base_yyparse", "CURRENT_ROLE cannot be used as a role name here", tok.Start)
	}
	return ""
}

func nRoleSpec(r *ast.RoleSpec) *ast.Node {
	return &ast.Node{Node: &ast.Node_RoleSpec{RoleSpec: r}}
}

// parseRoleList is gram.y's role_list.
func (p *parser) parseRoleList() []*ast.Node {
	list := []*ast.Node{nRoleSpec(p.parseRoleSpec())}
	for p.have(ast.Token(',')) {
		list = append(list, nRoleSpec(p.parseRoleSpec()))
	}
	return list
}

// parseOptWith is gram.y's opt_with: WITH | WITH_LA | empty.
func (p *parser) parseOptWith() {
	if p.kind() == ast.Token_WITH || p.kind() == ast.Token_WITH_LA {
		p.next()
	}
}

// parseCreateRoleStmt is gram.y's CreateRoleStmt/CreateUserStmt/
// CreateGroupStmt; CREATE has been consumed and the ROLE/USER/GROUP_P
// keyword selects the stmt_type.
func (p *parser) parseCreateRoleStmt(stmtType ast.RoleStmtType) *ast.Node {
	p.next() // ROLE, USER, or GROUP_P
	n := &ast.CreateRoleStmt{StmtType: stmtType}
	n.Role = p.parseRoleId()
	p.parseOptWith()
	n.Options = p.parseOptRoleList(true)
	return &ast.Node{Node: &ast.Node_CreateRoleStmt{CreateRoleStmt: n}}
}

// parseOptRoleList is gram.y's OptRoleList (create=true) or
// AlterOptRoleList (create=false): a juxtaposed list of role options.
func (p *parser) parseOptRoleList(create bool) []*ast.Node {
	var list []*ast.Node
	for {
		el := p.parseRoleElem(create)
		if el == nil {
			return list
		}
		list = append(list, el)
	}
}

// parseRoleElem is gram.y's AlterOptRoleElem plus, when create is true, the
// CreateOptRoleElem extras. Returns nil when the lookahead does not start a
// role option.
func (p *parser) parseRoleElem(create bool) *ast.Node {
	tok := p.peek()
	switch tok.Kind {
	case ast.Token_PASSWORD:
		p.next()
		vtok := p.peek()
		switch vtok.Kind {
		case ast.Token_SCONST:
			return makeDefElem("password", nStr(p.sconst()), tok.Start)
		case ast.Token_PARAM:
			p.next()
			return makeDefElem("password", makeParamRef(vtok.Ival, vtok.Start), tok.Start)
		case ast.Token_NULL_P:
			p.next()
			return makeDefElem("password", nil, tok.Start)
		}
		p.syntaxErrorAt()
	case ast.Token_ENCRYPTED:
		p.next()
		p.expect(ast.Token_PASSWORD)
		vtok := p.peek()
		switch vtok.Kind {
		case ast.Token_SCONST:
			return makeDefElem("password", nStr(p.sconst()), tok.Start)
		case ast.Token_PARAM:
			p.next()
			return makeDefElem("password", makeParamRef(vtok.Ival, vtok.Start), tok.Start)
		}
		p.syntaxErrorAt()
	case ast.Token_UNENCRYPTED:
		p.next()
		p.expect(ast.Token_PASSWORD)
		p.ereport("base_yyparse", "UNENCRYPTED PASSWORD is no longer supported", tok.Start)
	case ast.Token_INHERIT:
		p.next()
		return makeDefElem("inherit", nBoolean(true), tok.Start)
	case ast.Token_CONNECTION:
		p.next()
		p.expect(ast.Token_LIMIT)
		return makeDefElem("connectionlimit", nInteger(p.parseSignedIconst()), tok.Start)
	case ast.Token_VALID:
		p.next()
		p.expect(ast.Token_UNTIL)
		return makeDefElem("validUntil", nStr(p.sconst()), tok.Start)
	case ast.Token_USER:
		p.next()
		return makeDefElem("rolemembers", nList(p.parseRoleList()), tok.Start)
	case ast.Token_IDENT:
		p.next()
		var name string
		var val bool
		switch tok.Str {
		case "superuser":
			name, val = "superuser", true
		case "nosuperuser":
			name, val = "superuser", false
		case "createrole":
			name, val = "createrole", true
		case "nocreaterole":
			name, val = "createrole", false
		case "replication":
			name, val = "isreplication", true
		case "noreplication":
			name, val = "isreplication", false
		case "createdb":
			name, val = "createdb", true
		case "nocreatedb":
			name, val = "createdb", false
		case "login":
			name, val = "canlogin", true
		case "nologin":
			name, val = "canlogin", false
		case "bypassrls":
			name, val = "bypassrls", true
		case "nobypassrls":
			name, val = "bypassrls", false
		case "noinherit":
			name, val = "inherit", false
		default:
			p.ereport("base_yyparse", fmt.Sprintf("unrecognized role option %q", tok.Str), tok.Start)
		}
		return makeDefElem(name, nBoolean(val), tok.Start)
	}
	if create {
		switch tok.Kind {
		case ast.Token_SYSID:
			p.next()
			return makeDefElem("sysid", nInteger(p.iconst()), tok.Start)
		case ast.Token_ADMIN:
			p.next()
			return makeDefElem("adminmembers", nList(p.parseRoleList()), tok.Start)
		case ast.Token_ROLE:
			p.next()
			return makeDefElem("rolemembers", nList(p.parseRoleList()), tok.Start)
		case ast.Token_IN_P:
			p.next()
			switch p.kind() {
			case ast.Token_ROLE, ast.Token_GROUP_P:
				p.next()
				return makeDefElem("addroleto", nList(p.parseRoleList()), tok.Start)
			}
			p.syntaxErrorAt()
		}
	}
	return nil
}

// parseAlterRoleStmtRest is gram.y's AlterRoleStmt and AlterRoleSetStmt
// with ALTER ROLE/USER RoleSpec already consumed.
func (p *parser) parseAlterRoleStmtRest(role *ast.RoleSpec) *ast.Node {
	// IN DATABASE / SET / RESET pick AlterRoleSetStmt.
	switch p.kind() {
	case ast.Token_IN_P:
		n := &ast.AlterRoleSetStmt{Role: role}
		p.next()
		p.expect(ast.Token_DATABASE)
		n.Database = p.name()
		n.Setstmt = p.parseSetResetClause()
		return &ast.Node{Node: &ast.Node_AlterRoleSetStmt{AlterRoleSetStmt: n}}
	case ast.Token_SET, ast.Token_RESET:
		n := &ast.AlterRoleSetStmt{Role: role, Setstmt: p.parseSetResetClause()}
		return &ast.Node{Node: &ast.Node_AlterRoleSetStmt{AlterRoleSetStmt: n}}
	}

	n := &ast.AlterRoleStmt{Role: role, Action: 1}
	p.parseOptWith()
	n.Options = p.parseOptRoleList(false)
	return &ast.Node{Node: &ast.Node_AlterRoleStmt{AlterRoleStmt: n}}
}

// parseAlterGroupStmtRest is gram.y's AlterGroupStmt with ALTER GROUP_P
// RoleSpec already consumed.
func (p *parser) parseAlterGroupStmtRest(role *ast.RoleSpec) *ast.Node {
	var action int32
	switch p.kind() {
	case ast.Token_ADD_P:
		p.next()
		action = 1
	case ast.Token_DROP:
		p.next()
		action = -1
	default:
		p.syntaxErrorAt()
	}
	utok := p.expect(ast.Token_USER)
	n := &ast.AlterRoleStmt{
		Role:   role,
		Action: action,
		Options: []*ast.Node{
			makeDefElem("rolemembers", nList(p.parseRoleList()), utok.End),
		},
	}
	// gram.y gives the DefElem @6 — the role_list's location.
	return &ast.Node{Node: &ast.Node_AlterRoleStmt{AlterRoleStmt: n}}
}

// parseDropRoleStmt is gram.y's DropRoleStmt; DROP ROLE/USER/GROUP_P
// consumed.
func (p *parser) parseDropRoleStmt() *ast.Node {
	n := &ast.DropRoleStmt{}
	if p.have(ast.Token_IF_P) {
		p.expect(ast.Token_EXISTS)
		n.MissingOk = true
	}
	n.Roles = p.parseRoleList()
	return &ast.Node{Node: &ast.Node_DropRoleStmt{DropRoleStmt: n}}
}

// parseCreateSchemaStmt is gram.y's CreateSchemaStmt; CREATE SCHEMA
// consumed.
func (p *parser) parseCreateSchemaStmt() *ast.Node {
	n := &ast.CreateSchemaStmt{}
	if p.have(ast.Token_IF_P) {
		p.expect(ast.Token_NOT)
		p.expect(ast.Token_EXISTS)
		n.IfNotExists = true
	}
	// opt_single_name AUTHORIZATION RoleSpec | ColId
	if p.kind() == ast.Token_AUTHORIZATION {
		p.next()
		n.Authrole = p.parseRoleSpec()
	} else {
		n.Schemaname = p.colId()
		if p.have(ast.Token_AUTHORIZATION) {
			n.Authrole = p.parseRoleSpec()
		}
	}
	// OptSchemaEltList
	eltTok := p.peek()
	elts := p.parseOptSchemaEltList()
	if n.IfNotExists && elts != nil {
		p.ereport("base_yyparse", "CREATE SCHEMA IF NOT EXISTS cannot include schema elements", eltTok.Start)
	}
	n.SchemaElts = elts
	return &ast.Node{Node: &ast.Node_CreateSchemaStmt{CreateSchemaStmt: n}}
}

// parseOptSchemaEltList is gram.y's OptSchemaEltList/schema_stmt: the
// statements that can appear inside CREATE SCHEMA.
func (p *parser) parseOptSchemaEltList() []*ast.Node {
	var list []*ast.Node
	for {
		var stmt *ast.Node
		switch p.kind() {
		case ast.Token_CREATE:
			stmt = p.parseCreateSchemaElt()
		case ast.Token_GRANT:
			stmt = p.parseGrantStmt()
		default:
			return list
		}
		list = append(list, stmt)
	}
}

// parseCreateSchemaElt dispatches the CREATE-headed schema_stmt
// alternatives: CreateStmt, IndexStmt, CreateSeqStmt, CreateTrigStmt,
// ViewStmt.
func (p *parser) parseCreateSchemaElt() *ast.Node {
	p.expect(ast.Token_CREATE)
	stmt := p.parseCreateStmtFamily(true)
	if stmt == nil {
		p.syntaxErrorAt()
	}
	return stmt
}
