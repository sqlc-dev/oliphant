package parse

// Logical replication DDL: CreatePublicationStmt, AlterPublicationStmt,
// CreateSubscriptionStmt, AlterSubscriptionStmt.

import (
	"github.com/sqlc-dev/oliphant/ast"
)

// parseCreatePublicationStmt is gram.y's CreatePublicationStmt; CREATE
// PUBLICATION consumed.
func (p *parser) parseCreatePublicationStmt() *ast.Node {
	n := &ast.CreatePublicationStmt{Pubname: p.name()}
	if p.have(ast.Token_FOR) {
		if p.kind() == ast.Token_ALL && p.kindN(1) == ast.Token_TABLES {
			p.next()
			p.next()
			n.ForAllTables = true
		} else {
			n.Pubobjects = p.parsePubObjList()
		}
	}
	n.Options = p.parseOptDefinition()
	return &ast.Node{Node: &ast.Node_CreatePublicationStmt{CreatePublicationStmt: n}}
}

// parseAlterPublicationStmt handles ALTER PUBLICATION name ...; the name's
// tail (RENAME/OWNER) is resolved by the caller.
func (p *parser) parseAlterPublicationStmt(name string) *ast.Node {
	n := &ast.AlterPublicationStmt{Pubname: name}
	switch {
	case p.have(ast.Token_SET):
		if p.kind() == ast.Token('(') {
			n.Options = p.parseDefinition()
			return &ast.Node{Node: &ast.Node_AlterPublicationStmt{AlterPublicationStmt: n}}
		}
		n.Action = ast.AlterPublicationAction_AP_SetObjects
		n.Pubobjects = p.parsePubObjList()
	case p.have(ast.Token_ADD_P):
		n.Action = ast.AlterPublicationAction_AP_AddObjects
		n.Pubobjects = p.parsePubObjList()
	case p.have(ast.Token_DROP):
		n.Action = ast.AlterPublicationAction_AP_DropObjects
		n.Pubobjects = p.parsePubObjList()
	default:
		p.syntaxErrorAt()
	}
	return &ast.Node{Node: &ast.Node_AlterPublicationStmt{AlterPublicationStmt: n}}
}

// parsePubObjList is gram.y's pub_obj_list plus preprocess_pubobj_list.
func (p *parser) parsePubObjList() []*ast.Node {
	list := []*ast.Node{p.parsePublicationObjSpec()}
	for p.have(ast.Token(',')) {
		list = append(list, p.parsePublicationObjSpec())
	}
	p.preprocessPubobjList(list)
	return list
}

func nPublicationObjSpec(n *ast.PublicationObjSpec) *ast.Node {
	return &ast.Node{Node: &ast.Node_PublicationObjSpec{PublicationObjSpec: n}}
}

// parsePublicationObjSpec is gram.y's PublicationObjSpec.
func (p *parser) parsePublicationObjSpec() *ast.Node {
	tok := p.peek()
	switch tok.Kind {
	case ast.Token_TABLE:
		p.next()
		n := &ast.PublicationObjSpec{
			Pubobjtype: ast.PublicationObjSpecType_PUBLICATIONOBJ_TABLE,
			Pubtable:   &ast.PublicationTable{Relation: p.parseRelationExprVar()},
		}
		p.finishPublicationTable(n.Pubtable)
		return nPublicationObjSpec(n)
	case ast.Token_TABLES:
		p.next()
		p.expect(ast.Token_IN_P)
		p.expect(ast.Token_SCHEMA)
		stok := p.peek()
		if p.have(ast.Token_CURRENT_SCHEMA) {
			return nPublicationObjSpec(&ast.PublicationObjSpec{
				Pubobjtype: ast.PublicationObjSpecType_PUBLICATIONOBJ_TABLES_IN_CUR_SCHEMA,
				Location:   stok.Start,
			})
		}
		return nPublicationObjSpec(&ast.PublicationObjSpec{
			Pubobjtype: ast.PublicationObjSpecType_PUBLICATIONOBJ_TABLES_IN_SCHEMA,
			Name:       p.colId(),
			Location:   stok.Start,
		})
	case ast.Token_CURRENT_SCHEMA:
		p.next()
		return nPublicationObjSpec(&ast.PublicationObjSpec{
			Pubobjtype: ast.PublicationObjSpecType_PUBLICATIONOBJ_CONTINUATION,
			Location:   tok.Start,
		})
	case ast.Token_ONLY:
		// extended_relation_expr
		p.next()
		n := &ast.PublicationObjSpec{
			Pubobjtype: ast.PublicationObjSpecType_PUBLICATIONOBJ_CONTINUATION,
			Pubtable:   &ast.PublicationTable{},
		}
		var rel *ast.RangeVar
		if p.have(ast.Token('(')) {
			rel = p.parseQualifiedName()
			p.expect(ast.Token(')'))
		} else {
			rel = p.parseQualifiedName()
		}
		rel.Inh = false
		n.Pubtable.Relation = rel
		p.finishPublicationTable(n.Pubtable)
		return nPublicationObjSpec(n)
	}

	// ColId [indirection] forms and qualified_name '*'.
	name := p.colId()
	n := &ast.PublicationObjSpec{
		Pubobjtype: ast.PublicationObjSpecType_PUBLICATIONOBJ_CONTINUATION,
		Location:   tok.Start,
	}
	if p.kind() == ast.Token('.') {
		ind := p.parseOptIndirection()
		rel := p.makeRangeVarFromQualifiedName(name, ind, tok.Start)
		if p.have(ast.Token('*')) {
			rel.Inh = true
		}
		n.Pubtable = &ast.PublicationTable{Relation: rel}
		p.finishPublicationTable(n.Pubtable)
		return nPublicationObjSpec(n)
	}
	if p.have(ast.Token('*')) {
		// extended_relation_expr: qualified_name '*'
		rel := makeRangeVar("", name, tok.Start)
		rel.Inh = true
		n.Pubtable = &ast.PublicationTable{Relation: rel}
		n.Location = 0
		p.finishPublicationTable(n.Pubtable)
		return nPublicationObjSpec(n)
	}
	var cols []*ast.Node
	if p.have(ast.Token('(')) {
		cols = p.columnList()
		p.expect(ast.Token(')'))
	}
	var where *ast.Node
	if p.have(ast.Token_WHERE) {
		p.expect(ast.Token('('))
		where = p.parseAExpr(0)
		p.expect(ast.Token(')'))
	}
	if cols != nil || where != nil {
		n.Pubtable = &ast.PublicationTable{
			Relation:    makeRangeVar("", name, tok.Start),
			Columns:     cols,
			WhereClause: where,
		}
	} else {
		n.Name = name
	}
	return nPublicationObjSpec(n)
}

// finishPublicationTable parses opt_column_list OptWhereClause into a
// PublicationTable.
func (p *parser) finishPublicationTable(t *ast.PublicationTable) {
	if p.have(ast.Token('(')) {
		t.Columns = p.columnList()
		p.expect(ast.Token(')'))
	}
	if p.have(ast.Token_WHERE) {
		p.expect(ast.Token('('))
		t.WhereClause = p.parseAExpr(0)
		p.expect(ast.Token(')'))
	}
}

// preprocessPubobjList is gram.y's preprocess_pubobj_list.
func (p *parser) preprocessPubobjList(list []*ast.Node) {
	if len(list) == 0 {
		return
	}
	first := list[0].GetPublicationObjSpec()
	if first.Pubobjtype == ast.PublicationObjSpecType_PUBLICATIONOBJ_CONTINUATION {
		p.ereport("base_yyparse", "invalid publication object list", first.Location)
	}
	prev := ast.PublicationObjSpecType_PUBLICATIONOBJ_CONTINUATION
	for _, item := range list {
		obj := item.GetPublicationObjSpec()
		if obj.Pubobjtype == ast.PublicationObjSpecType_PUBLICATIONOBJ_CONTINUATION {
			obj.Pubobjtype = prev
		}
		switch obj.Pubobjtype {
		case ast.PublicationObjSpecType_PUBLICATIONOBJ_TABLE:
			if obj.Name == "" && obj.Pubtable == nil {
				p.ereport("base_yyparse", "invalid table name", obj.Location)
			}
			if obj.Name != "" {
				obj.Pubtable = &ast.PublicationTable{
					Relation: makeRangeVar("", obj.Name, obj.Location),
				}
				obj.Name = ""
			}
		case ast.PublicationObjSpecType_PUBLICATIONOBJ_TABLES_IN_SCHEMA,
			ast.PublicationObjSpecType_PUBLICATIONOBJ_TABLES_IN_CUR_SCHEMA:
			if obj.Pubtable != nil && obj.Pubtable.WhereClause != nil {
				p.ereport("base_yyparse", "WHERE clause not allowed for schema", obj.Location)
			}
			if obj.Pubtable != nil && obj.Pubtable.Columns != nil {
				p.ereport("base_yyparse", "column specification not allowed for schema", obj.Location)
			}
			switch {
			case obj.Name != "":
				obj.Pubobjtype = ast.PublicationObjSpecType_PUBLICATIONOBJ_TABLES_IN_SCHEMA
			case obj.Pubtable == nil:
				obj.Pubobjtype = ast.PublicationObjSpecType_PUBLICATIONOBJ_TABLES_IN_CUR_SCHEMA
			default:
				p.ereport("base_yyparse", "invalid schema name", obj.Location)
			}
		}
		prev = obj.Pubobjtype
	}
}

// parseCreateSubscriptionStmt is gram.y's CreateSubscriptionStmt; CREATE
// SUBSCRIPTION consumed.
func (p *parser) parseCreateSubscriptionStmt() *ast.Node {
	n := &ast.CreateSubscriptionStmt{Subname: p.name()}
	p.expect(ast.Token_CONNECTION)
	n.Conninfo = p.sconst()
	p.expect(ast.Token_PUBLICATION)
	n.Publication = p.nameList()
	n.Options = p.parseOptDefinition()
	return &ast.Node{Node: &ast.Node_CreateSubscriptionStmt{CreateSubscriptionStmt: n}}
}

// parseAlterSubscriptionStmt handles ALTER SUBSCRIPTION name ...; alterLoc
// is the ALTER token's location (used by the ENABLE/DISABLE DefElem).
func (p *parser) parseAlterSubscriptionStmt(name string, alterLoc int32) *ast.Node {
	n := &ast.AlterSubscriptionStmt{Subname: name}
	switch {
	case p.have(ast.Token_SET):
		if p.have(ast.Token_PUBLICATION) {
			n.Kind = ast.AlterSubscriptionType_ALTER_SUBSCRIPTION_SET_PUBLICATION
			n.Publication = p.nameList()
			n.Options = p.parseOptDefinition()
		} else {
			n.Kind = ast.AlterSubscriptionType_ALTER_SUBSCRIPTION_OPTIONS
			n.Options = p.parseDefinition()
		}
	case p.have(ast.Token_CONNECTION):
		n.Kind = ast.AlterSubscriptionType_ALTER_SUBSCRIPTION_CONNECTION
		n.Conninfo = p.sconst()
	case p.have(ast.Token_REFRESH):
		p.expect(ast.Token_PUBLICATION)
		n.Kind = ast.AlterSubscriptionType_ALTER_SUBSCRIPTION_REFRESH
		n.Options = p.parseOptDefinition()
	case p.have(ast.Token_ADD_P):
		p.expect(ast.Token_PUBLICATION)
		n.Kind = ast.AlterSubscriptionType_ALTER_SUBSCRIPTION_ADD_PUBLICATION
		n.Publication = p.nameList()
		n.Options = p.parseOptDefinition()
	case p.have(ast.Token_DROP):
		p.expect(ast.Token_PUBLICATION)
		n.Kind = ast.AlterSubscriptionType_ALTER_SUBSCRIPTION_DROP_PUBLICATION
		n.Publication = p.nameList()
		n.Options = p.parseOptDefinition()
	case p.have(ast.Token_ENABLE_P):
		n.Kind = ast.AlterSubscriptionType_ALTER_SUBSCRIPTION_ENABLED
		n.Options = []*ast.Node{makeDefElem("enabled", nBoolean(true), alterLoc)}
	case p.have(ast.Token_DISABLE_P):
		n.Kind = ast.AlterSubscriptionType_ALTER_SUBSCRIPTION_ENABLED
		n.Options = []*ast.Node{makeDefElem("enabled", nBoolean(false), alterLoc)}
	case p.have(ast.Token_SKIP):
		n.Kind = ast.AlterSubscriptionType_ALTER_SUBSCRIPTION_SKIP
		n.Options = p.parseDefinition()
	default:
		p.syntaxErrorAt()
	}
	return &ast.Node{Node: &ast.Node_AlterSubscriptionStmt{AlterSubscriptionStmt: n}}
}
