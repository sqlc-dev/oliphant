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
			p.next()
			p.next()
			return p.parseCreateUserMappingStmt()
		}
		return p.parseCreateRoleStmt(ast.RoleStmtType_ROLESTMT_USER)
	case ast.Token_GROUP_P:
		return p.parseCreateRoleStmt(ast.RoleStmtType_ROLESTMT_GROUP)
	case ast.Token_SCHEMA:
		p.next()
		return p.parseCreateSchemaStmt()
	case ast.Token_CAST:
		p.next()
		return p.parseCreateCastStmt()
	case ast.Token_FUNCTION, ast.Token_PROCEDURE:
		return p.parseCreateFunctionStmt(false)
	case ast.Token_DOMAIN_P:
		p.next()
		return p.parseCreateDomainStmt()
	case ast.Token_DATABASE:
		p.next()
		return p.parseCreatedbStmt()
	case ast.Token_EXTENSION:
		p.next()
		return p.parseCreateExtensionStmt()
	case ast.Token_EVENT:
		if p.kindN(1) == ast.Token_TRIGGER {
			p.next()
			p.next()
			return p.parseCreateEventTrigStmt()
		}
	case ast.Token_POLICY:
		p.next()
		return p.parseCreatePolicyStmt()
	case ast.Token_PUBLICATION:
		p.next()
		return p.parseCreatePublicationStmt()
	case ast.Token_FOREIGN:
		switch p.kindN(1) {
		case ast.Token_DATA_P:
			p.next()
			p.next()
			p.expect(ast.Token_WRAPPER)
			return p.parseCreateFdwStmt()
		case ast.Token_TABLE:
			p.next()
			p.next()
			return p.parseCreateForeignTableStmt()
		}
	case ast.Token_SERVER:
		p.next()
		return p.parseCreateForeignServerStmt()
	case ast.Token_SUBSCRIPTION:
		p.next()
		return p.parseCreateSubscriptionStmt()
	case ast.Token_AGGREGATE:
		p.next()
		return p.parseCreateAggregateStmt(false)
	case ast.Token_OPERATOR:
		p.next()
		return p.parseCreateOperatorStmt()
	case ast.Token_TYPE_P:
		p.next()
		return p.parseCreateTypeStmt()
	case ast.Token_STATISTICS:
		p.next()
		return p.parseCreateStatsStmt()
	case ast.Token_COLLATION:
		// gram.y: DefineStmt COLLATION arms
		p.next()
		n := &ast.DefineStmt{Kind: ast.ObjectType_OBJECT_COLLATION}
		if p.have(ast.Token_IF_P) {
			p.expect(ast.Token_NOT)
			p.expect(ast.Token_EXISTS)
			n.IfNotExists = true
		}
		n.Defnames = p.anyName()
		if ftok := p.peek(); ftok.Kind == ast.Token_FROM {
			p.next()
			ntok := p.peek()
			n.Definition = []*ast.Node{makeDefElem("from", nList(p.anyName()), ntok.Start)}
		} else {
			n.Definition = p.parseDefinition()
		}
		return nDefineStmt(n)
	case ast.Token_TEXT_P:
		if p.kindN(1) == ast.Token_SEARCH {
			p.next()
			p.next()
			var kind ast.ObjectType
			switch p.kind() {
			case ast.Token_PARSER:
				kind = ast.ObjectType_OBJECT_TSPARSER
			case ast.Token_DICTIONARY:
				kind = ast.ObjectType_OBJECT_TSDICTIONARY
			case ast.Token_TEMPLATE:
				kind = ast.ObjectType_OBJECT_TSTEMPLATE
			case ast.Token_CONFIGURATION:
				kind = ast.ObjectType_OBJECT_TSCONFIGURATION
			default:
				p.syntaxErrorAt()
			}
			p.next()
			return nDefineStmt(&ast.DefineStmt{
				Kind:       kind,
				Defnames:   p.anyName(),
				Definition: p.parseDefinition(),
			})
		}
	case ast.Token_ACCESS:
		if p.kindN(1) == ast.Token_METHOD {
			p.next()
			p.next()
			return p.parseCreateAmStmt()
		}
	case ast.Token_RULE:
		p.next()
		return p.parseRuleStmt(false)
	case ast.Token_TRUSTED, ast.Token_PROCEDURAL, ast.Token_LANGUAGE:
		return p.parseCreatePLangStmt(false)
	case ast.Token_TABLESPACE:
		p.next()
		return p.parseCreateTableSpaceStmt()
	case ast.Token_CONVERSION_P:
		p.next()
		return p.parseCreateConversionStmt(false)
	case ast.Token_DEFAULT:
		if p.kindN(1) == ast.Token_CONVERSION_P {
			p.next()
			p.next()
			return p.parseCreateConversionStmt(true)
		}
	case ast.Token_TRANSFORM:
		p.next()
		return p.parseCreateTransformStmt(false)
	case ast.Token_ASSERTION:
		// gram.y: CreateAssertionStmt always errors (no position).
		p.next()
		p.anyName()
		p.expect(ast.Token_CHECK)
		p.expect(ast.Token('('))
		p.parseAExpr(0)
		p.expect(ast.Token(')'))
		p.parseConstraintAttributeSpec()
		p.ereport("base_yyparse", "CREATE ASSERTION is not yet implemented", -1)
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
	case ast.Token_FUNCTION, ast.Token_PROCEDURE:
		return p.parseCreateFunctionStmt(true)
	case ast.Token_AGGREGATE:
		p.next()
		return p.parseCreateAggregateStmt(true)
	case ast.Token_RULE:
		p.next()
		return p.parseRuleStmt(true)
	case ast.Token_TRUSTED, ast.Token_PROCEDURAL, ast.Token_LANGUAGE:
		return p.parseCreatePLangStmt(true)
	case ast.Token_TRANSFORM:
		p.next()
		return p.parseCreateTransformStmt(true)
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

// parseDropDispatch routes a statement beginning with DROP: DropStmt and
// the specialized Drop*/Remove* productions. The DROP token is not yet
// consumed.
func (p *parser) parseDropDispatch() *ast.Node {
	p.expect(ast.Token_DROP)
	switch p.kind() {
	case ast.Token_ROLE, ast.Token_GROUP_P:
		p.next()
		return p.parseDropRoleStmt()
	case ast.Token_USER:
		if p.kindN(1) == ast.Token_MAPPING {
			// gram.y: DropUserMappingStmt
			p.next()
			p.next()
			n := &ast.DropUserMappingStmt{MissingOk: p.parseDropIfExists()}
			p.expect(ast.Token_FOR)
			n.User = p.parseAuthIdent()
			p.expect(ast.Token_SERVER)
			n.Servername = p.name()
			return &ast.Node{Node: &ast.Node_DropUserMappingStmt{DropUserMappingStmt: n}}
		}
		p.next()
		return p.parseDropRoleStmt()

	case ast.Token_TYPE_P:
		// gram.y: DropStmt: DROP TYPE_P type_name_list
		p.next()
		n := newDropStmt(ast.ObjectType_OBJECT_TYPE)
		n.MissingOk = p.parseDropIfExists()
		n.Objects = p.parseTypeNameList()
		n.Behavior = p.parseOptDropBehavior()
		return nDropStmt(n)
	case ast.Token_DOMAIN_P:
		p.next()
		n := newDropStmt(ast.ObjectType_OBJECT_DOMAIN)
		n.MissingOk = p.parseDropIfExists()
		n.Objects = p.parseTypeNameList()
		n.Behavior = p.parseOptDropBehavior()
		return nDropStmt(n)

	case ast.Token_INDEX:
		if p.kindN(1) == ast.Token_CONCURRENTLY {
			p.next()
			p.next()
			n := newDropStmt(ast.ObjectType_OBJECT_INDEX)
			n.Concurrent = true
			n.MissingOk = p.parseDropIfExists()
			n.Objects = p.parseAnyNameList()
			n.Behavior = p.parseOptDropBehavior()
			return nDropStmt(n)
		}

	case ast.Token_FUNCTION, ast.Token_PROCEDURE, ast.Token_ROUTINE:
		// gram.y: RemoveFuncStmt
		var objtype ast.ObjectType
		switch p.next().Kind {
		case ast.Token_FUNCTION:
			objtype = ast.ObjectType_OBJECT_FUNCTION
		case ast.Token_PROCEDURE:
			objtype = ast.ObjectType_OBJECT_PROCEDURE
		default:
			objtype = ast.ObjectType_OBJECT_ROUTINE
		}
		n := newDropStmt(objtype)
		n.MissingOk = p.parseDropIfExists()
		n.Objects = p.parseFunctionWithArgtypesList()
		n.Behavior = p.parseOptDropBehavior()
		return nDropStmt(n)

	case ast.Token_AGGREGATE:
		// gram.y: RemoveAggrStmt
		p.next()
		n := newDropStmt(ast.ObjectType_OBJECT_AGGREGATE)
		n.MissingOk = p.parseDropIfExists()
		n.Objects = p.parseAggregateWithArgtypesList()
		n.Behavior = p.parseOptDropBehavior()
		return nDropStmt(n)

	case ast.Token_OPERATOR:
		p.next()
		switch p.kind() {
		case ast.Token_CLASS, ast.Token_FAMILY:
			// gram.y: DropOpClassStmt / DropOpFamilyStmt
			objtype := ast.ObjectType_OBJECT_OPCLASS
			if p.next().Kind == ast.Token_FAMILY {
				objtype = ast.ObjectType_OBJECT_OPFAMILY
			}
			n := newDropStmt(objtype)
			n.MissingOk = p.parseDropIfExists()
			names := p.anyName()
			p.expect(ast.Token_USING)
			n.Objects = []*ast.Node{nList(append([]*ast.Node{nStr(p.name())}, names...))}
			n.Behavior = p.parseOptDropBehavior()
			return nDropStmt(n)
		}
		// gram.y: RemoveOperStmt
		n := newDropStmt(ast.ObjectType_OBJECT_OPERATOR)
		n.MissingOk = p.parseDropIfExists()
		n.Objects = p.parseOperatorWithArgtypesList()
		n.Behavior = p.parseOptDropBehavior()
		return nDropStmt(n)

	case ast.Token_CAST:
		// gram.y: DropCastStmt
		p.next()
		n := newDropStmt(ast.ObjectType_OBJECT_CAST)
		n.MissingOk = p.parseDropIfExists()
		p.expect(ast.Token('('))
		from := p.parseTypename()
		p.expect(ast.Token_AS)
		to := p.parseTypename()
		p.expect(ast.Token(')'))
		n.Objects = []*ast.Node{nList([]*ast.Node{nTypeName(from), nTypeName(to)})}
		n.Behavior = p.parseOptDropBehavior()
		return nDropStmt(n)

	case ast.Token_TRANSFORM:
		// gram.y: DropTransformStmt
		p.next()
		n := newDropStmt(ast.ObjectType_OBJECT_TRANSFORM)
		n.MissingOk = p.parseDropIfExists()
		p.expect(ast.Token_FOR)
		t := p.parseTypename()
		p.expect(ast.Token_LANGUAGE)
		n.Objects = []*ast.Node{nList([]*ast.Node{nTypeName(t), nStr(p.name())})}
		n.Behavior = p.parseOptDropBehavior()
		return nDropStmt(n)

	case ast.Token_OWNED:
		// gram.y: DropOwnedStmt
		p.next()
		p.expect(ast.Token_BY)
		n := &ast.DropOwnedStmt{Roles: p.parseRoleList()}
		n.Behavior = p.parseOptDropBehavior()
		return &ast.Node{Node: &ast.Node_DropOwnedStmt{DropOwnedStmt: n}}

	case ast.Token_TABLESPACE:
		// gram.y: DropTableSpaceStmt
		p.next()
		n := &ast.DropTableSpaceStmt{MissingOk: p.parseDropIfExists()}
		n.Tablespacename = p.name()
		return &ast.Node{Node: &ast.Node_DropTableSpaceStmt{DropTableSpaceStmt: n}}

	case ast.Token_SUBSCRIPTION:
		// gram.y: DropSubscriptionStmt
		p.next()
		n := &ast.DropSubscriptionStmt{MissingOk: p.parseDropIfExists()}
		n.Subname = p.name()
		n.Behavior = p.parseOptDropBehavior()
		return &ast.Node{Node: &ast.Node_DropSubscriptionStmt{DropSubscriptionStmt: n}}

	case ast.Token_DATABASE:
		// gram.y: DropdbStmt
		p.next()
		n := &ast.DropdbStmt{MissingOk: p.parseDropIfExists()}
		n.Dbname = p.name()
		if p.kind() == ast.Token_WITH || p.kind() == ast.Token_WITH_LA || p.kind() == ast.Token('(') {
			p.parseOptWith()
			p.expect(ast.Token('('))
			for {
				ftok := p.expect(ast.Token_FORCE)
				n.Options = append(n.Options, makeDefElem("force", nil, ftok.Start))
				if !p.have(ast.Token(',')) {
					break
				}
			}
			p.expect(ast.Token(')'))
		}
		return &ast.Node{Node: &ast.Node_DropdbStmt{DropdbStmt: n}}
	}

	// gram.y: DropStmt: DROP object_type_name_on_any_name name ON any_name
	if t, ok := p.tryObjectTypeNameOnAnyName(); ok {
		n := newDropStmt(t)
		n.MissingOk = p.parseDropIfExists()
		name := p.name()
		p.expect(ast.Token_ON)
		names := p.anyName()
		n.Objects = []*ast.Node{nList(append(names, nStr(name)))}
		n.Behavior = p.parseOptDropBehavior()
		return nDropStmt(n)
	}
	// gram.y: DropStmt: DROP object_type_any_name any_name_list
	if t, ok := p.tryObjectTypeAnyName(); ok {
		n := newDropStmt(t)
		n.MissingOk = p.parseDropIfExists()
		n.Objects = p.parseAnyNameList()
		n.Behavior = p.parseOptDropBehavior()
		return nDropStmt(n)
	}
	// gram.y: DropStmt: DROP drop_type_name name_list
	if t, ok := p.tryDropTypeName(); ok {
		n := newDropStmt(t)
		n.MissingOk = p.parseDropIfExists()
		n.Objects = p.nameList()
		n.Behavior = p.parseOptDropBehavior()
		return nDropStmt(n)
	}
	p.syntaxErrorAt()
	return nil
}

// parseAuthIdent is gram.y's auth_ident: RoleSpec | USER.
func (p *parser) parseAuthIdent() *ast.RoleSpec {
	if p.kind() == ast.Token_USER {
		tok := p.next()
		return &ast.RoleSpec{
			Roletype: ast.RoleSpecType_ROLESPEC_CURRENT_USER,
			Location: tok.Start,
		}
	}
	return p.parseRoleSpec()
}

// RangeVar.relpersistence values (pg_class.h: RELPERSISTENCE_*).
const (
	relPersistPermanent = "p"
	relPersistTemp      = "t"
	relPersistUnlogged  = "u"
)
