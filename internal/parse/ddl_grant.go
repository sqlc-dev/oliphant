package parse

// GRANT/REVOKE (GrantStmt, GrantRoleStmt), ALTER DEFAULT PRIVILEGES,
// CREATE/ALTER POLICY, and CREATE ACCESS METHOD.

import (
	"fmt"

	"github.com/sqlc-dev/oliphant/ast"
)

func nAccessPriv(n *ast.AccessPriv) *ast.Node {
	return &ast.Node{Node: &ast.Node_AccessPriv{AccessPriv: n}}
}

func nGrantStmt(n *ast.GrantStmt) *ast.Node {
	return &ast.Node{Node: &ast.Node_GrantStmt{GrantStmt: n}}
}

// parsePrivilege is gram.y's privilege.
func (p *parser) parsePrivilege() *ast.Node {
	n := &ast.AccessPriv{}
	switch p.kind() {
	case ast.Token_SELECT:
		p.next()
		n.PrivName = "select"
	case ast.Token_REFERENCES:
		p.next()
		n.PrivName = "references"
	case ast.Token_CREATE:
		p.next()
		n.PrivName = "create"
	case ast.Token_ALTER:
		p.next()
		p.expect(ast.Token_SYSTEM_P)
		n.PrivName = "alter system"
		return nAccessPriv(n)
	default:
		n.PrivName = p.colId()
	}
	if p.have(ast.Token('(')) {
		n.Cols = p.columnList()
		p.expect(ast.Token(')'))
	}
	return nAccessPriv(n)
}

// parsePrivilegeList is gram.y's privilege_list.
func (p *parser) parsePrivilegeList() []*ast.Node {
	list := []*ast.Node{p.parsePrivilege()}
	for p.have(ast.Token(',')) {
		list = append(list, p.parsePrivilege())
	}
	return list
}

// parsePrivileges is gram.y's privileges: ALL [PRIVILEGES] [(cols)] or a
// privilege_list.
func (p *parser) parsePrivileges() []*ast.Node {
	if p.have(ast.Token_ALL) {
		p.have(ast.Token_PRIVILEGES)
		if p.have(ast.Token('(')) {
			n := &ast.AccessPriv{Cols: p.columnList()}
			p.expect(ast.Token(')'))
			return []*ast.Node{nAccessPriv(n)}
		}
		return nil
	}
	return p.parsePrivilegeList()
}

// parsePrivilegeTarget is gram.y's privilege_target.
func (p *parser) parsePrivilegeTarget() (ast.GrantTargetType, ast.ObjectType, []*ast.Node) {
	obj := ast.GrantTargetType_ACL_TARGET_OBJECT
	switch p.kind() {
	case ast.Token_TABLE:
		p.next()
		return obj, ast.ObjectType_OBJECT_TABLE, p.parseQualifiedNameList()
	case ast.Token_SEQUENCE:
		p.next()
		return obj, ast.ObjectType_OBJECT_SEQUENCE, p.parseQualifiedNameList()
	case ast.Token_FOREIGN:
		p.next()
		switch {
		case p.have(ast.Token_DATA_P):
			p.expect(ast.Token_WRAPPER)
			return obj, ast.ObjectType_OBJECT_FDW, p.nameList()
		case p.have(ast.Token_SERVER):
			return obj, ast.ObjectType_OBJECT_FOREIGN_SERVER, p.nameList()
		}
		p.syntaxErrorAt()
	case ast.Token_FUNCTION:
		p.next()
		return obj, ast.ObjectType_OBJECT_FUNCTION, p.parseFunctionWithArgtypesList()
	case ast.Token_PROCEDURE:
		p.next()
		return obj, ast.ObjectType_OBJECT_PROCEDURE, p.parseFunctionWithArgtypesList()
	case ast.Token_ROUTINE:
		p.next()
		return obj, ast.ObjectType_OBJECT_ROUTINE, p.parseFunctionWithArgtypesList()
	case ast.Token_DATABASE:
		p.next()
		return obj, ast.ObjectType_OBJECT_DATABASE, p.nameList()
	case ast.Token_DOMAIN_P:
		p.next()
		return obj, ast.ObjectType_OBJECT_DOMAIN, p.parseAnyNameList()
	case ast.Token_LANGUAGE:
		p.next()
		return obj, ast.ObjectType_OBJECT_LANGUAGE, p.nameList()
	case ast.Token_LARGE_P:
		p.next()
		p.expect(ast.Token_OBJECT_P)
		list := []*ast.Node{p.parseNumericOnly()}
		for p.have(ast.Token(',')) {
			list = append(list, p.parseNumericOnly())
		}
		return obj, ast.ObjectType_OBJECT_LARGEOBJECT, list
	case ast.Token_PARAMETER:
		p.next()
		list := []*ast.Node{nStr(p.parseVarName())}
		for p.have(ast.Token(',')) {
			list = append(list, nStr(p.parseVarName()))
		}
		return obj, ast.ObjectType_OBJECT_PARAMETER_ACL, list
	case ast.Token_SCHEMA:
		p.next()
		return obj, ast.ObjectType_OBJECT_SCHEMA, p.nameList()
	case ast.Token_TABLESPACE:
		p.next()
		return obj, ast.ObjectType_OBJECT_TABLESPACE, p.nameList()
	case ast.Token_TYPE_P:
		p.next()
		return obj, ast.ObjectType_OBJECT_TYPE, p.parseAnyNameList()
	case ast.Token_ALL:
		p.next()
		all := ast.GrantTargetType_ACL_TARGET_ALL_IN_SCHEMA
		var t ast.ObjectType
		switch p.kind() {
		case ast.Token_TABLES:
			t = ast.ObjectType_OBJECT_TABLE
		case ast.Token_SEQUENCES:
			t = ast.ObjectType_OBJECT_SEQUENCE
		case ast.Token_FUNCTIONS:
			t = ast.ObjectType_OBJECT_FUNCTION
		case ast.Token_PROCEDURES:
			t = ast.ObjectType_OBJECT_PROCEDURE
		case ast.Token_ROUTINES:
			t = ast.ObjectType_OBJECT_ROUTINE
		default:
			p.syntaxErrorAt()
		}
		p.next()
		p.expect(ast.Token_IN_P)
		p.expect(ast.Token_SCHEMA)
		return all, t, p.nameList()
	}
	return obj, ast.ObjectType_OBJECT_TABLE, p.parseQualifiedNameList()
}

// parseGranteeList is gram.y's grantee_list.
func (p *parser) parseGranteeList() []*ast.Node {
	parseGrantee := func() *ast.Node {
		p.have(ast.Token_GROUP_P)
		return nRoleSpec(p.parseRoleSpec())
	}
	list := []*ast.Node{parseGrantee()}
	for p.have(ast.Token(',')) {
		list = append(list, parseGrantee())
	}
	return list
}

// parseOptGrantedBy is gram.y's opt_granted_by.
func (p *parser) parseOptGrantedBy() *ast.RoleSpec {
	if p.kind() == ast.Token_GRANTED {
		p.next()
		p.expect(ast.Token_BY)
		return p.parseRoleSpec()
	}
	return nil
}

// parseGrantStmt is gram.y's GrantStmt / GrantRoleStmt: the privilege list
// is parsed first and ON/TO picks the statement.
func (p *parser) parseGrantStmt() *ast.Node {
	p.expect(ast.Token_GRANT)
	privs := p.parsePrivileges()
	if p.have(ast.Token_ON) {
		n := &ast.GrantStmt{
			IsGrant:    true,
			Privileges: privs,
			Behavior:   ast.DropBehavior_DROP_RESTRICT,
		}
		n.Targtype, n.Objtype, n.Objects = p.parsePrivilegeTarget()
		p.expect(ast.Token_TO)
		n.Grantees = p.parseGranteeList()
		if p.kind() == ast.Token_WITH && p.kindN(1) == ast.Token_GRANT {
			p.next()
			p.next()
			p.expect(ast.Token_OPTION)
			n.GrantOption = true
		}
		n.Grantor = p.parseOptGrantedBy()
		return nGrantStmt(n)
	}
	// gram.y: GrantRoleStmt
	p.expect(ast.Token_TO)
	n := &ast.GrantRoleStmt{
		IsGrant:      true,
		GrantedRoles: privs,
		GranteeRoles: p.parseRoleList(),
		Behavior:     ast.DropBehavior_DROP_RESTRICT,
	}
	if p.kind() == ast.Token_WITH || p.kind() == ast.Token_WITH_LA {
		p.next()
		// grant_role_opt_list
		for {
			otok := p.peek()
			name := p.colLabel()
			var val *ast.Node
			switch {
			case p.have(ast.Token_OPTION):
				val = nBoolean(true)
			case p.have(ast.Token_TRUE_P):
				val = nBoolean(true)
			case p.have(ast.Token_FALSE_P):
				val = nBoolean(false)
			default:
				p.syntaxErrorAt()
			}
			n.Opt = append(n.Opt, makeDefElem(name, val, otok.Start))
			if !p.have(ast.Token(',')) {
				break
			}
		}
	}
	n.Grantor = p.parseOptGrantedBy()
	return &ast.Node{Node: &ast.Node_GrantRoleStmt{GrantRoleStmt: n}}
}

// parseRevokeStmt is gram.y's RevokeStmt / RevokeRoleStmt.
func (p *parser) parseRevokeStmt() *ast.Node {
	p.expect(ast.Token_REVOKE)

	// REVOKE GRANT OPTION FOR ... is always the GrantStmt form; REVOKE
	// ColId OPTION FOR ... is the role form with an option DefElem.
	if p.kind() == ast.Token_GRANT && p.kindN(1) == ast.Token_OPTION {
		p.next()
		p.next()
		p.expect(ast.Token_FOR)
		n := &ast.GrantStmt{GrantOption: true, Privileges: p.parsePrivileges()}
		p.expect(ast.Token_ON)
		n.Targtype, n.Objtype, n.Objects = p.parsePrivilegeTarget()
		p.expect(ast.Token_FROM)
		n.Grantees = p.parseGranteeList()
		n.Grantor = p.parseOptGrantedBy()
		n.Behavior = p.parseOptDropBehavior()
		return nGrantStmt(n)
	}
	if isColIdToken(p.peek()) && p.kindN(1) == ast.Token_OPTION && p.kindN(2) == ast.Token_FOR {
		otok := p.peek()
		optName := p.colId()
		p.next()
		p.next()
		n := &ast.GrantRoleStmt{
			Opt:          []*ast.Node{makeDefElem(optName, nBoolean(false), otok.Start)},
			GrantedRoles: p.parsePrivilegeList(),
		}
		p.expect(ast.Token_FROM)
		n.GranteeRoles = p.parseRoleList()
		n.Grantor = p.parseOptGrantedBy()
		n.Behavior = p.parseOptDropBehavior()
		return &ast.Node{Node: &ast.Node_GrantRoleStmt{GrantRoleStmt: n}}
	}

	privs := p.parsePrivileges()
	if p.have(ast.Token_ON) {
		n := &ast.GrantStmt{Privileges: privs}
		n.Targtype, n.Objtype, n.Objects = p.parsePrivilegeTarget()
		p.expect(ast.Token_FROM)
		n.Grantees = p.parseGranteeList()
		n.Grantor = p.parseOptGrantedBy()
		n.Behavior = p.parseOptDropBehavior()
		return nGrantStmt(n)
	}
	p.expect(ast.Token_FROM)
	n := &ast.GrantRoleStmt{
		GrantedRoles: privs,
		GranteeRoles: p.parseRoleList(),
	}
	n.Grantor = p.parseOptGrantedBy()
	n.Behavior = p.parseOptDropBehavior()
	return &ast.Node{Node: &ast.Node_GrantRoleStmt{GrantRoleStmt: n}}
}

// parseAlterDefaultPrivilegesStmt is gram.y's AlterDefaultPrivilegesStmt;
// ALTER DEFAULT PRIVILEGES consumed.
func (p *parser) parseAlterDefaultPrivilegesStmt() *ast.Node {
	n := &ast.AlterDefaultPrivilegesStmt{}
	// DefACLOptionList
	for {
		tok := p.peek()
		switch tok.Kind {
		case ast.Token_IN_P:
			p.next()
			p.expect(ast.Token_SCHEMA)
			n.Options = append(n.Options, makeDefElem("schemas", nList(p.nameList()), tok.Start))
			continue
		case ast.Token_FOR:
			p.next()
			if !p.have(ast.Token_ROLE) && !p.have(ast.Token_USER) {
				p.syntaxErrorAt()
			}
			n.Options = append(n.Options, makeDefElem("roles", nList(p.parseRoleList()), tok.Start))
			continue
		}
		break
	}
	// DefACLAction
	action := &ast.GrantStmt{
		Targtype: ast.GrantTargetType_ACL_TARGET_DEFAULTS,
		Behavior: ast.DropBehavior_DROP_RESTRICT,
	}
	switch {
	case p.have(ast.Token_GRANT):
		action.IsGrant = true
		action.Privileges = p.parsePrivileges()
		p.expect(ast.Token_ON)
		action.Objtype = p.parseDefACLPrivilegeTarget()
		p.expect(ast.Token_TO)
		action.Grantees = p.parseGranteeList()
		if p.kind() == ast.Token_WITH && p.kindN(1) == ast.Token_GRANT {
			p.next()
			p.next()
			p.expect(ast.Token_OPTION)
			action.GrantOption = true
		}
	case p.have(ast.Token_REVOKE):
		if p.kind() == ast.Token_GRANT && p.kindN(1) == ast.Token_OPTION {
			p.next()
			p.next()
			p.expect(ast.Token_FOR)
			action.GrantOption = true
		}
		action.Privileges = p.parsePrivileges()
		p.expect(ast.Token_ON)
		action.Objtype = p.parseDefACLPrivilegeTarget()
		p.expect(ast.Token_FROM)
		action.Grantees = p.parseGranteeList()
		action.Behavior = p.parseOptDropBehavior()
	default:
		p.syntaxErrorAt()
	}
	n.Action = action
	return &ast.Node{Node: &ast.Node_AlterDefaultPrivilegesStmt{AlterDefaultPrivilegesStmt: n}}
}

// parseDefACLPrivilegeTarget is gram.y's defacl_privilege_target.
func (p *parser) parseDefACLPrivilegeTarget() ast.ObjectType {
	switch p.kind() {
	case ast.Token_TABLES:
		p.next()
		return ast.ObjectType_OBJECT_TABLE
	case ast.Token_FUNCTIONS, ast.Token_ROUTINES:
		p.next()
		return ast.ObjectType_OBJECT_FUNCTION
	case ast.Token_SEQUENCES:
		p.next()
		return ast.ObjectType_OBJECT_SEQUENCE
	case ast.Token_TYPES_P:
		p.next()
		return ast.ObjectType_OBJECT_TYPE
	case ast.Token_SCHEMAS:
		p.next()
		return ast.ObjectType_OBJECT_SCHEMA
	case ast.Token_LARGE_P:
		// LARGE_P OBJECTS_P (PG 18)
		if p.kindN(1) == ast.Token_OBJECTS_P {
			p.next()
			p.next()
			return ast.ObjectType_OBJECT_LARGEOBJECT
		}
	}
	p.syntaxErrorAt()
	return 0
}

// parseCreatePolicyStmt is gram.y's CreatePolicyStmt; CREATE POLICY
// consumed.
func (p *parser) parseCreatePolicyStmt() *ast.Node {
	n := &ast.CreatePolicyStmt{PolicyName: p.name(), Permissive: true, CmdName: "all"}
	p.expect(ast.Token_ON)
	n.Table = p.parseQualifiedName()
	if p.have(ast.Token_AS) {
		itok := p.peek()
		ident := p.expect(ast.Token_IDENT)
		switch ident.Str {
		case "permissive":
		case "restrictive":
			n.Permissive = false
		default:
			p.ereport("base_yyparse",
				fmt.Sprintf("unrecognized row security option %q", ident.Str), itok.Start)
		}
	}
	if p.have(ast.Token_FOR) {
		n.CmdName = p.parseRowSecurityCmd()
	}
	if p.have(ast.Token_TO) {
		n.Roles = p.parseRoleList()
	} else {
		n.Roles = []*ast.Node{nRoleSpec(&ast.RoleSpec{
			Roletype: ast.RoleSpecType_ROLESPEC_PUBLIC,
			Location: -1,
		})}
	}
	if p.have(ast.Token_USING) {
		p.expect(ast.Token('('))
		n.Qual = p.parseAExpr(0)
		p.expect(ast.Token(')'))
	}
	if p.kind() == ast.Token_WITH && p.kindN(1) == ast.Token_CHECK {
		p.next()
		p.next()
		p.expect(ast.Token('('))
		n.WithCheck = p.parseAExpr(0)
		p.expect(ast.Token(')'))
	}
	return &ast.Node{Node: &ast.Node_CreatePolicyStmt{CreatePolicyStmt: n}}
}

// parseAlterPolicyStmt is gram.y's AlterPolicyStmt; ALTER POLICY name ON
// qualified_name consumed.
func (p *parser) parseAlterPolicyStmt(name string, table *ast.RangeVar) *ast.Node {
	n := &ast.AlterPolicyStmt{PolicyName: name, Table: table}
	if p.have(ast.Token_TO) {
		n.Roles = p.parseRoleList()
	}
	if p.have(ast.Token_USING) {
		p.expect(ast.Token('('))
		n.Qual = p.parseAExpr(0)
		p.expect(ast.Token(')'))
	}
	if p.kind() == ast.Token_WITH && p.kindN(1) == ast.Token_CHECK {
		p.next()
		p.next()
		p.expect(ast.Token('('))
		n.WithCheck = p.parseAExpr(0)
		p.expect(ast.Token(')'))
	}
	return &ast.Node{Node: &ast.Node_AlterPolicyStmt{AlterPolicyStmt: n}}
}

// parseRowSecurityCmd is gram.y's row_security_cmd.
func (p *parser) parseRowSecurityCmd() string {
	switch {
	case p.have(ast.Token_ALL):
		return "all"
	case p.have(ast.Token_SELECT):
		return "select"
	case p.have(ast.Token_INSERT):
		return "insert"
	case p.have(ast.Token_UPDATE):
		return "update"
	case p.have(ast.Token_DELETE_P):
		return "delete"
	}
	p.syntaxErrorAt()
	return ""
}

// parseCreateAmStmt is gram.y's CreateAmStmt; CREATE ACCESS METHOD
// consumed.
func (p *parser) parseCreateAmStmt() *ast.Node {
	n := &ast.CreateAmStmt{Amname: p.name()}
	p.expect(ast.Token_TYPE_P)
	switch {
	case p.have(ast.Token_INDEX):
		n.Amtype = "i"
	case p.have(ast.Token_TABLE):
		n.Amtype = "t"
	default:
		p.syntaxErrorAt()
	}
	p.expect(ast.Token_HANDLER)
	n.HandlerName = p.parseHandlerName()
	return &ast.Node{Node: &ast.Node_CreateAmStmt{CreateAmStmt: n}}
}

// parseHandlerName is gram.y's handler_name: name | name attrs.
func (p *parser) parseHandlerName() []*ast.Node {
	names := []*ast.Node{nStr(p.name())}
	for p.have(ast.Token('.')) {
		names = append(names, nStr(p.attrName()))
	}
	return names
}
