package parse

// CreateDomainStmt, CreatedbStmt, and the extension DDL:
// CreateExtensionStmt, AlterExtensionStmt, AlterExtensionContentsStmt.

import (
	"github.com/sqlc-dev/oliphant/ast"
)

// parseCreateDomainStmt is gram.y's CreateDomainStmt; CREATE DOMAIN_P
// consumed.
func (p *parser) parseCreateDomainStmt() *ast.Node {
	n := &ast.CreateDomainStmt{Domainname: p.anyName()}
	p.have(ast.Token_AS)
	n.TypeName = p.parseTypename()
	n.Constraints, n.CollClause = p.parseColQualList()
	return &ast.Node{Node: &ast.Node_CreateDomainStmt{CreateDomainStmt: n}}
}

// parseCreatedbStmt is gram.y's CreatedbStmt; CREATE DATABASE consumed.
func (p *parser) parseCreatedbStmt() *ast.Node {
	n := &ast.CreatedbStmt{Dbname: p.name()}
	p.parseOptWith()
	n.Options = p.parseCreatedbOptItems()
	return &ast.Node{Node: &ast.Node_CreatedbStmt{CreatedbStmt: n}}
}

// parseCreateExtensionStmt is gram.y's CreateExtensionStmt; CREATE
// EXTENSION consumed.
func (p *parser) parseCreateExtensionStmt() *ast.Node {
	n := &ast.CreateExtensionStmt{}
	if p.have(ast.Token_IF_P) {
		p.expect(ast.Token_NOT)
		p.expect(ast.Token_EXISTS)
		n.IfNotExists = true
	}
	n.Extname = p.name()
	p.parseOptWith()
	// create_extension_opt_list
	for {
		tok := p.peek()
		switch tok.Kind {
		case ast.Token_SCHEMA:
			p.next()
			n.Options = append(n.Options, makeDefElem("schema", nStr(p.name()), tok.Start))
		case ast.Token_VERSION_P:
			p.next()
			n.Options = append(n.Options, makeDefElem("new_version", nStr(p.nonReservedWordOrSconst()), tok.Start))
		case ast.Token_FROM:
			p.next()
			p.nonReservedWordOrSconst()
			p.ereport("base_yyparse", "CREATE EXTENSION ... FROM is no longer supported", tok.Start)
		case ast.Token_CASCADE:
			p.next()
			n.Options = append(n.Options, makeDefElem("cascade", nBoolean(true), tok.Start))
		default:
			return &ast.Node{Node: &ast.Node_CreateExtensionStmt{CreateExtensionStmt: n}}
		}
	}
}

// parseAlterExtensionStmt handles ALTER EXTENSION name ... : the UPDATE
// form (AlterExtensionStmt), ADD/DROP members (AlterExtensionContentsStmt),
// and SET SCHEMA (generic). ALTER EXTENSION is consumed.
func (p *parser) parseAlterExtensionStmt() *ast.Node {
	name := p.name()
	switch p.kind() {
	case ast.Token_UPDATE:
		p.next()
		n := &ast.AlterExtensionStmt{Extname: name}
		for p.kind() == ast.Token_TO {
			ttok := p.next()
			n.Options = append(n.Options, makeDefElem("new_version", nStr(p.nonReservedWordOrSconst()), ttok.Start))
		}
		return &ast.Node{Node: &ast.Node_AlterExtensionStmt{AlterExtensionStmt: n}}
	case ast.Token_ADD_P, ast.Token_DROP:
		n := &ast.AlterExtensionContentsStmt{Extname: name, Action: 1}
		if p.next().Kind == ast.Token_DROP {
			n.Action = -1
		}
		n.Objtype, n.Object = p.parseExtensionMemberObject()
		return &ast.Node{Node: &ast.Node_AlterExtensionContentsStmt{AlterExtensionContentsStmt: n}}
	}
	if n := p.parseAlterGenericTail(ast.ObjectType_OBJECT_EXTENSION, nStr(name), nil, false,
		tailSchema); n != nil {
		return n
	}
	p.syntaxErrorAt()
	return nil
}

// parseExtensionMemberObject parses the object reference of
// AlterExtensionContentsStmt.
func (p *parser) parseExtensionMemberObject() (ast.ObjectType, *ast.Node) {
	switch p.kind() {
	case ast.Token_AGGREGATE:
		p.next()
		return ast.ObjectType_OBJECT_AGGREGATE, p.parseAggregateWithArgtypes()
	case ast.Token_CAST:
		p.next()
		p.expect(ast.Token('('))
		from := p.parseTypename()
		p.expect(ast.Token_AS)
		to := p.parseTypename()
		p.expect(ast.Token(')'))
		return ast.ObjectType_OBJECT_CAST,
			nList([]*ast.Node{nTypeName(from), nTypeName(to)})
	case ast.Token_DOMAIN_P:
		p.next()
		return ast.ObjectType_OBJECT_DOMAIN, nTypeName(p.parseTypename())
	case ast.Token_FUNCTION:
		p.next()
		return ast.ObjectType_OBJECT_FUNCTION, p.parseFunctionWithArgtypes()
	case ast.Token_PROCEDURE:
		p.next()
		return ast.ObjectType_OBJECT_PROCEDURE, p.parseFunctionWithArgtypes()
	case ast.Token_ROUTINE:
		p.next()
		return ast.ObjectType_OBJECT_ROUTINE, p.parseFunctionWithArgtypes()
	case ast.Token_OPERATOR:
		p.next()
		switch p.kind() {
		case ast.Token_CLASS, ast.Token_FAMILY:
			objtype := ast.ObjectType_OBJECT_OPCLASS
			if p.next().Kind == ast.Token_FAMILY {
				objtype = ast.ObjectType_OBJECT_OPFAMILY
			}
			names := p.anyName()
			p.expect(ast.Token_USING)
			return objtype, nList(append([]*ast.Node{nStr(p.name())}, names...))
		}
		return ast.ObjectType_OBJECT_OPERATOR, p.parseOperatorWithArgtypes()
	case ast.Token_TRANSFORM:
		p.next()
		p.expect(ast.Token_FOR)
		t := p.parseTypename()
		p.expect(ast.Token_LANGUAGE)
		return ast.ObjectType_OBJECT_TRANSFORM,
			nList([]*ast.Node{nTypeName(t), nStr(p.name())})
	case ast.Token_TYPE_P:
		p.next()
		return ast.ObjectType_OBJECT_TYPE, nTypeName(p.parseTypename())
	}
	if t, ok := p.tryObjectTypeAnyName(); ok {
		return t, nList(p.anyName())
	}
	if t, ok := p.tryObjectTypeName(); ok {
		return t, nStr(p.name())
	}
	p.syntaxErrorAt()
	return 0, nil
}
