package parse

// The unified ALTER dispatcher: RenameStmt, AlterObjectSchemaStmt,
// AlterOwnerStmt, AlterObjectDependsStmt, and the object-specific ALTER
// statements that share their prefixes (AlterOperatorStmt, AlterTypeStmt,
// AlterCollationStmt, AlterSystemStmt, AlterTblSpcStmt, AlterStatsStmt,
// AlterDomainStmt, AlterDatabase*Stmt, AlterEventTrigStmt). Each ALTER
// <objtype> arm parses the object reference once and picks the statement
// from the following token, exactly as the LALR tables do.

import (
	"github.com/sqlc-dev/oliphant/ast"
)

// newRenameStmt builds a RenameStmt with C's zero-valued defaults
// (relationType OBJECT_ACCESS_METHOD and behavior DROP_RESTRICT are the
// proto spellings of C zero).
func newRenameStmt(t ast.ObjectType) *ast.RenameStmt {
	return &ast.RenameStmt{
		RenameType:   t,
		RelationType: ast.ObjectType_OBJECT_ACCESS_METHOD,
		Behavior:     ast.DropBehavior_DROP_RESTRICT,
	}
}

func nRenameStmt(n *ast.RenameStmt) *ast.Node {
	return &ast.Node{Node: &ast.Node_RenameStmt{RenameStmt: n}}
}

func nAlterObjectSchemaStmt(n *ast.AlterObjectSchemaStmt) *ast.Node {
	return &ast.Node{Node: &ast.Node_AlterObjectSchemaStmt{AlterObjectSchemaStmt: n}}
}

func nAlterOwnerStmt(n *ast.AlterOwnerStmt) *ast.Node {
	return &ast.Node{Node: &ast.Node_AlterOwnerStmt{AlterOwnerStmt: n}}
}

// alterTail flags which generic ALTER tails an object kind supports.
type alterTail int

const (
	tailRename alterTail = 1 << iota
	tailSchema
	tailOwner
	tailDepends
)

// parseAlterGenericTail parses RENAME TO / SET SCHEMA / OWNER TO /
// [NO] DEPENDS ON EXTENSION for an object addressed by a Node (object) or
// relation, returning nil if the lookahead is none of the supported tails.
func (p *parser) parseAlterGenericTail(t ast.ObjectType, object *ast.Node, rel *ast.RangeVar, missingOk bool, tails alterTail) *ast.Node {
	switch p.kind() {
	case ast.Token_RENAME:
		if tails&tailRename == 0 {
			return nil
		}
		p.next()
		p.expect(ast.Token_TO)
		n := newRenameStmt(t)
		n.Object = object
		n.Relation = rel
		n.MissingOk = missingOk
		n.Newname = p.name()
		return nRenameStmt(n)
	case ast.Token_SET:
		if tails&tailSchema == 0 || p.kindN(1) != ast.Token_SCHEMA {
			return nil
		}
		p.next()
		p.next()
		n := &ast.AlterObjectSchemaStmt{
			ObjectType: t,
			Object:     object,
			Relation:   rel,
			MissingOk:  missingOk,
			Newschema:  p.name(),
		}
		return nAlterObjectSchemaStmt(n)
	case ast.Token_OWNER:
		if tails&tailOwner == 0 {
			return nil
		}
		p.next()
		p.expect(ast.Token_TO)
		n := &ast.AlterOwnerStmt{
			ObjectType: t,
			Object:     object,
			Relation:   rel,
			Newowner:   p.parseRoleSpec(),
		}
		return nAlterOwnerStmt(n)
	case ast.Token_DEPENDS, ast.Token_NO:
		if tails&tailDepends == 0 {
			return nil
		}
		if p.kind() == ast.Token_NO && p.kindN(1) != ast.Token_DEPENDS {
			return nil
		}
		n := &ast.AlterObjectDependsStmt{
			ObjectType: t,
			Object:     object,
			Relation:   rel,
			Remove:     p.have(ast.Token_NO),
		}
		p.expect(ast.Token_DEPENDS)
		p.expect(ast.Token_ON)
		p.expect(ast.Token_EXTENSION)
		n.Extname = &ast.String{Sval: p.name()}
		return &ast.Node{Node: &ast.Node_AlterObjectDependsStmt{AlterObjectDependsStmt: n}}
	}
	return nil
}

// roleSpecToRoleId applies gram.y's RoleId restrictions to a parsed
// RoleSpec.
func (p *parser) roleSpecToRoleId(spec *ast.RoleSpec, loc int32) string {
	switch spec.Roletype {
	case ast.RoleSpecType_ROLESPEC_CSTRING:
		return spec.Rolename
	case ast.RoleSpecType_ROLESPEC_PUBLIC:
		p.ereport("base_yyparse", `role name "public" is reserved`, loc)
	case ast.RoleSpecType_ROLESPEC_SESSION_USER:
		p.ereport("base_yyparse", "SESSION_USER cannot be used as a role name here", loc)
	case ast.RoleSpecType_ROLESPEC_CURRENT_USER:
		p.ereport("base_yyparse", "CURRENT_USER cannot be used as a role name here", loc)
	case ast.RoleSpecType_ROLESPEC_CURRENT_ROLE:
		p.ereport("base_yyparse", "CURRENT_ROLE cannot be used as a role name here", loc)
	}
	return ""
}

// parseAlterDispatch routes a statement beginning with ALTER. The ALTER
// token is not yet consumed.
func (p *parser) parseAlterDispatch() *ast.Node {
	p.expect(ast.Token_ALTER)
	switch p.kind() {
	case ast.Token_AGGREGATE:
		p.next()
		obj := p.parseAggregateWithArgtypes()
		if n := p.parseAlterGenericTail(ast.ObjectType_OBJECT_AGGREGATE, obj, nil, false,
			tailRename|tailSchema|tailOwner); n != nil {
			return n
		}
		p.syntaxErrorAt()

	case ast.Token_COLLATION:
		p.next()
		names := p.anyName()
		if p.kind() == ast.Token_REFRESH {
			// gram.y: AlterCollationStmt
			p.next()
			p.expect(ast.Token_VERSION_P)
			return &ast.Node{Node: &ast.Node_AlterCollationStmt{AlterCollationStmt: &ast.AlterCollationStmt{
				Collname: names,
			}}}
		}
		if n := p.parseAlterGenericTail(ast.ObjectType_OBJECT_COLLATION, nList(names), nil, false,
			tailRename|tailSchema|tailOwner); n != nil {
			return n
		}
		p.syntaxErrorAt()

	case ast.Token_CONVERSION_P:
		p.next()
		obj := nList(p.anyName())
		if n := p.parseAlterGenericTail(ast.ObjectType_OBJECT_CONVERSION, obj, nil, false,
			tailRename|tailSchema|tailOwner); n != nil {
			return n
		}
		p.syntaxErrorAt()

	case ast.Token_DATABASE:
		p.next()
		dbname := p.name()
		switch p.kind() {
		case ast.Token_RENAME:
			p.next()
			p.expect(ast.Token_TO)
			n := newRenameStmt(ast.ObjectType_OBJECT_DATABASE)
			n.Subname = dbname
			n.Newname = p.name()
			return nRenameStmt(n)
		case ast.Token_OWNER:
			p.next()
			p.expect(ast.Token_TO)
			return nAlterOwnerStmt(&ast.AlterOwnerStmt{
				ObjectType: ast.ObjectType_OBJECT_DATABASE,
				Object:     nStr(dbname),
				Newowner:   p.parseRoleSpec(),
			})
		case ast.Token_REFRESH:
			p.next()
			p.expect(ast.Token_COLLATION)
			p.expect(ast.Token_VERSION_P)
			return &ast.Node{Node: &ast.Node_AlterDatabaseRefreshCollStmt{
				AlterDatabaseRefreshCollStmt: &ast.AlterDatabaseRefreshCollStmt{Dbname: dbname},
			}}
		case ast.Token_SET:
			if p.kindN(1) == ast.Token_TABLESPACE {
				p.next()
				p.next()
				ntok := p.peek()
				return &ast.Node{Node: &ast.Node_AlterDatabaseStmt{AlterDatabaseStmt: &ast.AlterDatabaseStmt{
					Dbname:  dbname,
					Options: []*ast.Node{makeDefElem("tablespace", nStr(p.name()), ntok.Start)},
				}}}
			}
			fallthrough
		case ast.Token_RESET:
			// gram.y: AlterDatabaseSetStmt
			return &ast.Node{Node: &ast.Node_AlterDatabaseSetStmt{AlterDatabaseSetStmt: &ast.AlterDatabaseSetStmt{
				Dbname:  dbname,
				Setstmt: p.parseSetResetClause(),
			}}}
		default:
			// gram.y: AlterDatabaseStmt: [WITH] createdb_opt_list
			n := &ast.AlterDatabaseStmt{Dbname: dbname}
			p.parseOptWith()
			n.Options = p.parseCreatedbOptItems()
			return &ast.Node{Node: &ast.Node_AlterDatabaseStmt{AlterDatabaseStmt: n}}
		}

	case ast.Token_DOMAIN_P:
		p.next()
		names := p.anyName()
		switch p.kind() {
		case ast.Token_RENAME:
			if p.kindN(1) == ast.Token_CONSTRAINT {
				p.next()
				p.next()
				n := newRenameStmt(ast.ObjectType_OBJECT_DOMCONSTRAINT)
				n.Object = nList(names)
				n.Subname = p.name()
				p.expect(ast.Token_TO)
				n.Newname = p.name()
				return nRenameStmt(n)
			}
		case ast.Token_SET:
			// SET SCHEMA is generic; SET DEFAULT / SET NOT NULL are
			// AlterDomainStmt.
			if p.kindN(1) != ast.Token_SCHEMA {
				return p.parseAlterDomainStmt(names)
			}
		case ast.Token_DROP, ast.Token_ADD_P, ast.Token_VALIDATE:
			return p.parseAlterDomainStmt(names)
		}
		if n := p.parseAlterGenericTail(ast.ObjectType_OBJECT_DOMAIN, nList(names), nil, false,
			tailRename|tailSchema|tailOwner); n != nil {
			return n
		}
		p.syntaxErrorAt()

	case ast.Token_EXTENSION:
		p.next()
		return p.parseAlterExtensionStmt()

	case ast.Token_FUNCTION, ast.Token_PROCEDURE, ast.Token_ROUTINE:
		var objtype ast.ObjectType
		switch p.next().Kind {
		case ast.Token_FUNCTION:
			objtype = ast.ObjectType_OBJECT_FUNCTION
		case ast.Token_PROCEDURE:
			objtype = ast.ObjectType_OBJECT_PROCEDURE
		default:
			objtype = ast.ObjectType_OBJECT_ROUTINE
		}
		obj := p.parseFunctionWithArgtypes()
		if n := p.parseAlterGenericTail(objtype, obj, nil, false,
			tailRename|tailSchema|tailOwner|tailDepends); n != nil {
			return n
		}
		return p.parseAlterFunctionStmt(objtype, obj.GetObjectWithArgs())

	case ast.Token_GROUP_P:
		p.next()
		stok := p.peek()
		spec := p.parseRoleSpec()
		if p.kind() == ast.Token_RENAME {
			p.next()
			p.expect(ast.Token_TO)
			n := newRenameStmt(ast.ObjectType_OBJECT_ROLE)
			n.Subname = p.roleSpecToRoleId(spec, stok.Start)
			rtok := p.peek()
			n.Newname = p.roleSpecToRoleId(p.parseRoleSpec(), rtok.Start)
			return nRenameStmt(n)
		}
		return p.parseAlterGroupStmtRest(spec)

	case ast.Token_PROCEDURAL:
		if p.kindN(1) != ast.Token_LANGUAGE {
			break
		}
		p.next()
		fallthrough
	case ast.Token_LANGUAGE:
		p.next()
		obj := nStr(p.name())
		if n := p.parseAlterGenericTail(ast.ObjectType_OBJECT_LANGUAGE, obj, nil, false,
			tailRename|tailOwner); n != nil {
			return n
		}
		p.syntaxErrorAt()

	case ast.Token_LARGE_P:
		p.next()
		p.expect(ast.Token_OBJECT_P)
		obj := p.parseNumericOnly()
		if n := p.parseAlterGenericTail(ast.ObjectType_OBJECT_LARGEOBJECT, obj, nil, false,
			tailOwner); n != nil {
			return n
		}
		p.syntaxErrorAt()

	case ast.Token_OPERATOR:
		p.next()
		switch p.kind() {
		case ast.Token_CLASS, ast.Token_FAMILY:
			isFamily := p.next().Kind == ast.Token_FAMILY
			objtype := ast.ObjectType_OBJECT_OPCLASS
			if isFamily {
				objtype = ast.ObjectType_OBJECT_OPFAMILY
			}
			names := p.anyName()
			p.expect(ast.Token_USING)
			obj := nList(append([]*ast.Node{nStr(p.name())}, names...))
			if isFamily {
				switch p.kind() {
				case ast.Token_ADD_P, ast.Token_DROP:
					return p.parseAlterOpFamilyStmt(obj)
				}
			}
			if n := p.parseAlterGenericTail(objtype, obj, nil, false,
				tailRename|tailSchema|tailOwner); n != nil {
				return n
			}
			p.syntaxErrorAt()
		}
		obj := p.parseOperatorWithArgtypes()
		if p.kind() == ast.Token_SET && p.kindN(1) == ast.Token('(') {
			// gram.y: AlterOperatorStmt
			p.next()
			p.next()
			n := &ast.AlterOperatorStmt{Opername: obj.GetObjectWithArgs()}
			n.Options = p.parseOperatorDefList()
			p.expect(ast.Token(')'))
			return &ast.Node{Node: &ast.Node_AlterOperatorStmt{AlterOperatorStmt: n}}
		}
		if n := p.parseAlterGenericTail(ast.ObjectType_OBJECT_OPERATOR, obj, nil, false,
			tailSchema|tailOwner); n != nil {
			return n
		}
		p.syntaxErrorAt()

	case ast.Token_ROLE, ast.Token_USER:
		if p.kindN(1) == ast.Token_MAPPING {
			break // AlterUserMappingStmt (FDW batch)
		}
		p.next()
		if p.kind() == ast.Token_ALL {
			p.next()
			n := &ast.AlterRoleSetStmt{}
			if p.have(ast.Token_IN_P) {
				p.expect(ast.Token_DATABASE)
				n.Database = p.name()
			}
			n.Setstmt = p.parseSetResetClause()
			return &ast.Node{Node: &ast.Node_AlterRoleSetStmt{AlterRoleSetStmt: n}}
		}
		stok := p.peek()
		spec := p.parseRoleSpec()
		if p.kind() == ast.Token_RENAME {
			p.next()
			p.expect(ast.Token_TO)
			n := newRenameStmt(ast.ObjectType_OBJECT_ROLE)
			n.Subname = p.roleSpecToRoleId(spec, stok.Start)
			rtok := p.peek()
			n.Newname = p.roleSpecToRoleId(p.parseRoleSpec(), rtok.Start)
			return nRenameStmt(n)
		}
		return p.parseAlterRoleStmtRest(spec)

	case ast.Token_RULE:
		p.next()
		subname := p.name()
		p.expect(ast.Token_ON)
		rel := p.parseQualifiedName()
		if n := p.parseAlterGenericTail(ast.ObjectType_OBJECT_RULE, nil, rel, false,
			tailRename); n != nil {
			n.GetRenameStmt().Subname = subname
			return n
		}
		p.syntaxErrorAt()

	case ast.Token_TRIGGER:
		p.next()
		subname := p.name()
		p.expect(ast.Token_ON)
		rel := p.parseQualifiedName()
		if n := p.parseAlterGenericTail(ast.ObjectType_OBJECT_TRIGGER, nil, rel, false,
			tailRename|tailDepends); n != nil {
			if r := n.GetRenameStmt(); r != nil {
				r.Subname = subname
			} else {
				n.GetAlterObjectDependsStmt().Object = nList([]*ast.Node{nStr(subname)})
			}
			return n
		}
		p.syntaxErrorAt()

	case ast.Token_EVENT:
		if p.kindN(1) != ast.Token_TRIGGER {
			break
		}
		p.next()
		p.next()
		name := p.name()
		switch p.kind() {
		case ast.Token_ENABLE_P:
			// gram.y: AlterEventTrigStmt / enable_trigger
			p.next()
			n := &ast.AlterEventTrigStmt{Trigname: name, Tgenabled: "O"}
			switch p.kind() {
			case ast.Token_REPLICA:
				p.next()
				n.Tgenabled = "R"
			case ast.Token_ALWAYS:
				p.next()
				n.Tgenabled = "A"
			}
			return &ast.Node{Node: &ast.Node_AlterEventTrigStmt{AlterEventTrigStmt: n}}
		case ast.Token_DISABLE_P:
			p.next()
			return &ast.Node{Node: &ast.Node_AlterEventTrigStmt{AlterEventTrigStmt: &ast.AlterEventTrigStmt{
				Trigname: name, Tgenabled: "D",
			}}}
		}
		if n := p.parseAlterGenericTail(ast.ObjectType_OBJECT_EVENT_TRIGGER, nStr(name), nil, false,
			tailRename|tailOwner); n != nil {
			return n
		}
		p.syntaxErrorAt()

	case ast.Token_SCHEMA:
		p.next()
		obj := nStr(p.name())
		if n := p.parseAlterGenericTail(ast.ObjectType_OBJECT_SCHEMA, nil, nil, false,
			tailRename|tailOwner); n != nil {
			// object_type_name statements carry the name as subname for
			// RENAME and as object for OWNER.
			if r := n.GetRenameStmt(); r != nil {
				r.Subname = asString(obj).Sval
			} else {
				n.GetAlterOwnerStmt().Object = obj
			}
			return n
		}
		p.syntaxErrorAt()

	case ast.Token_STATISTICS:
		p.next()
		missingOk := false
		if p.have(ast.Token_IF_P) {
			p.expect(ast.Token_EXISTS)
			missingOk = true
		}
		names := p.anyName()
		if p.kind() == ast.Token_SET && p.kindN(1) == ast.Token_STATISTICS {
			// gram.y: AlterStatsStmt
			p.next()
			p.next()
			return &ast.Node{Node: &ast.Node_AlterStatsStmt{AlterStatsStmt: &ast.AlterStatsStmt{
				Defnames:      names,
				MissingOk:     missingOk,
				Stxstattarget: p.parseSetStatisticsValue(),
			}}}
		}
		if !missingOk {
			if n := p.parseAlterGenericTail(ast.ObjectType_OBJECT_STATISTIC_EXT, nList(names), nil, false,
				tailRename|tailSchema|tailOwner); n != nil {
				return n
			}
		}
		p.syntaxErrorAt()

	case ast.Token_SYSTEM_P:
		// gram.y: AlterSystemStmt
		p.next()
		n := &ast.AlterSystemStmt{}
		switch {
		case p.have(ast.Token_SET):
			n.Setstmt = p.parseGenericSet()
		case p.have(ast.Token_RESET):
			n.Setstmt = &ast.VariableSetStmt{Kind: ast.VariableSetKind_VAR_RESET}
			if p.have(ast.Token_ALL) {
				n.Setstmt.Kind = ast.VariableSetKind_VAR_RESET_ALL
			} else {
				n.Setstmt.Name = p.parseVarName()
			}
		default:
			p.syntaxErrorAt()
		}
		return &ast.Node{Node: &ast.Node_AlterSystemStmt{AlterSystemStmt: n}}

	case ast.Token_TABLESPACE:
		p.next()
		name := p.name()
		switch p.kind() {
		case ast.Token_SET, ast.Token_RESET:
			// gram.y: AlterTblSpcStmt
			isReset := p.next().Kind == ast.Token_RESET
			n := &ast.AlterTableSpaceOptionsStmt{
				Tablespacename: name,
				IsReset:        isReset,
			}
			n.Options = p.parseReloptions()
			return &ast.Node{Node: &ast.Node_AlterTableSpaceOptionsStmt{AlterTableSpaceOptionsStmt: n}}
		case ast.Token_RENAME:
			p.next()
			p.expect(ast.Token_TO)
			n := newRenameStmt(ast.ObjectType_OBJECT_TABLESPACE)
			n.Subname = name
			n.Newname = p.name()
			return nRenameStmt(n)
		case ast.Token_OWNER:
			p.next()
			p.expect(ast.Token_TO)
			return nAlterOwnerStmt(&ast.AlterOwnerStmt{
				ObjectType: ast.ObjectType_OBJECT_TABLESPACE,
				Object:     nStr(name),
				Newowner:   p.parseRoleSpec(),
			})
		}
		p.syntaxErrorAt()

	case ast.Token_TEXT_P:
		if p.kindN(1) != ast.Token_SEARCH {
			break
		}
		p.next()
		p.next()
		var objtype ast.ObjectType
		var tails alterTail
		kindTok := p.next()
		switch kindTok.Kind {
		case ast.Token_PARSER:
			objtype, tails = ast.ObjectType_OBJECT_TSPARSER, tailRename|tailSchema
		case ast.Token_DICTIONARY:
			objtype, tails = ast.ObjectType_OBJECT_TSDICTIONARY, tailRename|tailSchema|tailOwner
		case ast.Token_TEMPLATE:
			objtype, tails = ast.ObjectType_OBJECT_TSTEMPLATE, tailRename|tailSchema
		case ast.Token_CONFIGURATION:
			objtype, tails = ast.ObjectType_OBJECT_TSCONFIGURATION, tailRename|tailSchema|tailOwner
		default:
			p.syntaxError(kindTok)
		}
		names := p.anyName()
		if objtype == ast.ObjectType_OBJECT_TSDICTIONARY && p.kind() == ast.Token('(') {
			// gram.y: AlterTSDictionaryStmt
			return &ast.Node{Node: &ast.Node_AlterTsdictionaryStmt{AlterTsdictionaryStmt: &ast.AlterTSDictionaryStmt{
				Dictname: names,
				Options:  p.parseDefinition(),
			}}}
		}
		if objtype == ast.ObjectType_OBJECT_TSCONFIGURATION {
			switch p.kind() {
			case ast.Token_ADD_P, ast.Token_ALTER, ast.Token_DROP:
				return p.parseAlterTSConfigurationStmt(names)
			}
		}
		if n := p.parseAlterGenericTail(objtype, nList(names), nil, false, tails); n != nil {
			return n
		}
		p.syntaxErrorAt()

	case ast.Token_TYPE_P:
		p.next()
		return p.parseAlterTypeDispatch()

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
		break // ALTER FOREIGN DATA WRAPPER (FDW batch)

	case ast.Token_SEQUENCE:
		p.next()
		missingOk := false
		if p.have(ast.Token_IF_P) {
			p.expect(ast.Token_EXISTS)
			missingOk = true
		}
		rel := p.parseQualifiedName()
		if n := p.parseAlterGenericTail(ast.ObjectType_OBJECT_SEQUENCE, nil, rel, missingOk,
			tailRename|tailSchema); n != nil {
			return n
		}
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
	}
	p.syntaxErrorAt()
	return nil
}

// parseAlterDomainStmt is gram.y's AlterDomainStmt; ALTER DOMAIN any_name
// consumed.
func (p *parser) parseAlterDomainStmt(names []*ast.Node) *ast.Node {
	n := &ast.AlterDomainStmt{
		TypeName: names,
		Behavior: ast.DropBehavior_DROP_RESTRICT,
	}
	switch {
	case p.have(ast.Token_SET):
		switch {
		case p.have(ast.Token_DEFAULT):
			n.Subtype = "T"
			n.Def = p.parseAExpr(0)
		case p.have(ast.Token_NOT):
			p.expect(ast.Token_NULL_P)
			n.Subtype = "O"
		default:
			p.syntaxErrorAt()
		}
	case p.have(ast.Token_DROP):
		switch {
		case p.have(ast.Token_DEFAULT):
			n.Subtype = "T"
		case p.have(ast.Token_NOT):
			p.expect(ast.Token_NULL_P)
			n.Subtype = "N"
		case p.have(ast.Token_CONSTRAINT):
			n.Subtype = "X"
			if p.have(ast.Token_IF_P) {
				p.expect(ast.Token_EXISTS)
				n.MissingOk = true
			}
			n.Name = p.name()
			n.Behavior = p.parseOptDropBehavior()
		default:
			p.syntaxErrorAt()
		}
	case p.have(ast.Token_ADD_P):
		n.Subtype = "C"
		n.Def = p.parseDomainConstraint()
	case p.have(ast.Token_VALIDATE):
		p.expect(ast.Token_CONSTRAINT)
		n.Subtype = "V"
		n.Name = p.name()
	default:
		p.syntaxErrorAt()
	}
	return &ast.Node{Node: &ast.Node_AlterDomainStmt{AlterDomainStmt: n}}
}

// parseDomainConstraint is gram.y's DomainConstraint.
func (p *parser) parseDomainConstraint() *ast.Node {
	tok := p.peek()
	if tok.Kind == ast.Token_CONSTRAINT {
		p.next()
		name := p.name()
		c := p.parseDomainConstraintElem()
		c.Conname = name
		c.Location = tok.Start
		return nConstraint(c)
	}
	return nConstraint(p.parseDomainConstraintElem())
}

// parseDomainConstraintElem is gram.y's DomainConstraintElem.
func (p *parser) parseDomainConstraintElem() *ast.Constraint {
	tok := p.peek()
	n := &ast.Constraint{Location: tok.Start}
	switch tok.Kind {
	case ast.Token_CHECK:
		p.next()
		n.Contype = ast.ConstrType_CONSTR_CHECK
		p.expect(ast.Token('('))
		n.RawExpr = p.parseAExpr(0)
		p.expect(ast.Token(')'))
		cas, casLoc := p.parseConstraintAttributeSpec()
		p.processCASbits(cas, casLoc, "CHECK", nil, nil, &n.SkipValidation, &n.IsNoInherit)
		n.InitiallyValid = !n.SkipValidation
	case ast.Token_NOT:
		p.next()
		p.expect(ast.Token_NULL_P)
		n.Contype = ast.ConstrType_CONSTR_NOTNULL
		n.Keys = []*ast.Node{nStr("value")}
		cas, casLoc := p.parseConstraintAttributeSpec()
		p.processCASbits(cas, casLoc, "NOT NULL", nil, nil, nil, &n.IsNoInherit)
		n.InitiallyValid = true
	default:
		p.syntaxErrorAt()
	}
	return n
}

// parseCreatedbOptItems is gram.y's createdb_opt_items.
func (p *parser) parseCreatedbOptItems() []*ast.Node {
	var list []*ast.Node
	for {
		tok := p.peek()
		var name string
		switch tok.Kind {
		case ast.Token_IDENT:
			p.next()
			name = tok.Str
		case ast.Token_CONNECTION:
			p.next()
			p.expect(ast.Token_LIMIT)
			name = "connection_limit"
		case ast.Token_ENCODING, ast.Token_LOCATION, ast.Token_OWNER,
			ast.Token_TABLESPACE, ast.Token_TEMPLATE:
			p.next()
			name = tok.Str
		default:
			return list
		}
		p.have(ast.Token('='))
		var arg *ast.Node
		switch p.kind() {
		case ast.Token_ICONST, ast.Token_FCONST, ast.Token('+'), ast.Token('-'):
			arg = p.parseNumericOnly()
		case ast.Token_DEFAULT:
			p.next()
		default:
			arg = nStr(p.parseOptBooleanOrString())
		}
		list = append(list, makeDefElem(name, arg, tok.Start))
	}
}

// parseGenericSet is gram.y's generic_set (used by ALTER SYSTEM SET).
func (p *parser) parseGenericSet() *ast.VariableSetStmt {
	name := p.parseVarName()
	if !p.have(ast.Token_TO) && !p.have(ast.Token('=')) {
		p.syntaxErrorAt()
	}
	if p.kind() == ast.Token_DEFAULT {
		p.next()
		return &ast.VariableSetStmt{
			Kind: ast.VariableSetKind_VAR_SET_DEFAULT,
			Name: name,
		}
	}
	return &ast.VariableSetStmt{
		Kind: ast.VariableSetKind_VAR_SET_VALUE,
		Name: name,
		Args: p.parseVarList(),
	}
}

// parseOperatorDefList is gram.y's operator_def_list.
func (p *parser) parseOperatorDefList() []*ast.Node {
	list := []*ast.Node{p.parseOperatorDefElem()}
	for p.have(ast.Token(',')) {
		list = append(list, p.parseOperatorDefElem())
	}
	return list
}

// parseOperatorDefElem is gram.y's operator_def_elem.
func (p *parser) parseOperatorDefElem() *ast.Node {
	tok := p.peek()
	name := p.colLabel()
	if !p.have(ast.Token('=')) {
		return makeDefElem(name, nil, tok.Start)
	}
	if p.have(ast.Token_NONE) {
		return makeDefElem(name, nil, tok.Start)
	}
	return makeDefElem(name, p.parseOperatorDefArg(), tok.Start)
}

// parseOperatorDefArg is gram.y's operator_def_arg.
func (p *parser) parseOperatorDefArg() *ast.Node {
	tok := p.peek()
	switch tok.Kind {
	case ast.Token_SCONST:
		p.next()
		return nStr(tok.Str)
	case ast.Token_ICONST, ast.Token_FCONST:
		return p.parseNumericOnly()
	case ast.Token('+'), ast.Token('-'):
		if p.kindN(1) == ast.Token_ICONST || p.kindN(1) == ast.Token_FCONST {
			return p.parseNumericOnly()
		}
		return nList(p.parseQualAllOp())
	case ast.Token_Op, ast.Token('*'), ast.Token('/'), ast.Token('%'),
		ast.Token('^'), ast.Token('<'), ast.Token('>'), ast.Token('='),
		ast.Token_LESS_EQUALS, ast.Token_GREATER_EQUALS, ast.Token_NOT_EQUALS,
		ast.Token_OPERATOR:
		return nList(p.parseQualAllOp())
	}
	if tok.KeywordKind == ast.KeywordKind_RESERVED_KEYWORD {
		p.next()
		return nStr(tok.Str)
	}
	return nTypeName(p.parseFuncType())
}

// Stubs for statements arriving in later milestone-7 batches.
func (p *parser) parseAlterOpFamilyStmt(obj *ast.Node) *ast.Node {
	p.syntaxErrorAt()
	return nil
}

func (p *parser) parseAlterTSConfigurationStmt(names []*ast.Node) *ast.Node {
	p.syntaxErrorAt()
	return nil
}
