package parse

// Maintenance and inspection utilities: ClusterStmt, VacuumStmt,
// AnalyzeStmt, ExplainStmt, ReindexStmt, DoStmt, CreateCastStmt.

import (
	"github.com/sqlc-dev/oliphant/ast"
)

// parseUtilityOptionList is gram.y's utility_option_list (the caller has
// consumed the opening paren).
func (p *parser) parseUtilityOptionList() []*ast.Node {
	list := []*ast.Node{p.parseUtilityOptionElem()}
	for p.have(ast.Token(',')) {
		list = append(list, p.parseUtilityOptionElem())
	}
	p.expect(ast.Token(')'))
	return list
}

// parseUtilityOptionElem is gram.y's utility_option_elem.
func (p *parser) parseUtilityOptionElem() *ast.Node {
	tok := p.peek()
	var name string
	switch tok.Kind {
	case ast.Token_ANALYZE, ast.Token_ANALYSE:
		p.next()
		name = "analyze"
	case ast.Token_FORMAT_LA:
		p.next()
		name = "format"
	default:
		name = p.nonReservedWord()
	}
	// utility_option_arg
	var arg *ast.Node
	vtok := p.peek()
	switch vtok.Kind {
	case ast.Token(','), ast.Token(')'):
	case ast.Token_ICONST, ast.Token_FCONST, ast.Token('+'), ast.Token('-'):
		arg = p.parseNumericOnly()
	default:
		arg = nStr(p.parseOptBooleanOrString())
	}
	return makeDefElem(name, arg, tok.Start)
}

// parseClusterStmt is gram.y's ClusterStmt.
func (p *parser) parseClusterStmt() *ast.Node {
	p.expect(ast.Token_CLUSTER)
	n := &ast.ClusterStmt{}
	if p.have(ast.Token('(')) {
		n.Params = p.parseUtilityOptionList()
		if isColIdToken(p.peek()) {
			n.Relation = p.parseQualifiedName()
			if p.have(ast.Token_USING) {
				n.Indexname = p.name()
			}
		}
		return &ast.Node{Node: &ast.Node_ClusterStmt{ClusterStmt: n}}
	}
	if vtok := p.peek(); vtok.Kind == ast.Token_VERBOSE {
		p.next()
		n.Params = []*ast.Node{makeDefElem("verbose", nil, vtok.Start)}
	}
	if isColIdToken(p.peek()) {
		rel := p.parseQualifiedName()
		switch {
		case p.have(ast.Token_ON):
			// pre-8.3: CLUSTER indexname ON relation
			n.Indexname = rel.Relname
			n.Relation = p.parseQualifiedName()
		case p.have(ast.Token_USING):
			n.Relation = rel
			n.Indexname = p.name()
		default:
			n.Relation = rel
		}
	}
	return &ast.Node{Node: &ast.Node_ClusterStmt{ClusterStmt: n}}
}

// parseVacuumRelationList is gram.y's opt_vacuum_relation_list.
func (p *parser) parseOptVacuumRelationList() []*ast.Node {
	if !isColIdToken(p.peek()) {
		return nil
	}
	var list []*ast.Node
	for {
		v := &ast.VacuumRelation{Relation: p.parseQualifiedName()}
		if p.have(ast.Token('(')) {
			v.VaCols = p.nameList()
			p.expect(ast.Token(')'))
		}
		list = append(list, &ast.Node{Node: &ast.Node_VacuumRelation{VacuumRelation: v}})
		if !p.have(ast.Token(',')) {
			return list
		}
	}
}

// parseVacuumStmt is gram.y's VacuumStmt.
func (p *parser) parseVacuumStmt() *ast.Node {
	p.expect(ast.Token_VACUUM)
	n := &ast.VacuumStmt{IsVacuumcmd: true}
	if p.have(ast.Token('(')) {
		n.Options = p.parseUtilityOptionList()
	} else {
		if tok := p.peek(); tok.Kind == ast.Token_FULL {
			p.next()
			n.Options = append(n.Options, makeDefElem("full", nil, tok.Start))
		}
		if tok := p.peek(); tok.Kind == ast.Token_FREEZE {
			p.next()
			n.Options = append(n.Options, makeDefElem("freeze", nil, tok.Start))
		}
		if tok := p.peek(); tok.Kind == ast.Token_VERBOSE {
			p.next()
			n.Options = append(n.Options, makeDefElem("verbose", nil, tok.Start))
		}
		if tok := p.peek(); tok.Kind == ast.Token_ANALYZE || tok.Kind == ast.Token_ANALYSE {
			p.next()
			n.Options = append(n.Options, makeDefElem("analyze", nil, tok.Start))
		}
	}
	n.Rels = p.parseOptVacuumRelationList()
	return &ast.Node{Node: &ast.Node_VacuumStmt{VacuumStmt: n}}
}

// parseAnalyzeStmt is gram.y's AnalyzeStmt.
func (p *parser) parseAnalyzeStmt() *ast.Node {
	p.next() // ANALYZE or ANALYSE
	n := &ast.VacuumStmt{}
	if p.have(ast.Token('(')) {
		n.Options = p.parseUtilityOptionList()
	} else if tok := p.peek(); tok.Kind == ast.Token_VERBOSE {
		p.next()
		n.Options = []*ast.Node{makeDefElem("verbose", nil, tok.Start)}
	}
	n.Rels = p.parseOptVacuumRelationList()
	return &ast.Node{Node: &ast.Node_VacuumStmt{VacuumStmt: n}}
}

// parseExplainStmt is gram.y's ExplainStmt.
func (p *parser) parseExplainStmt() *ast.Node {
	p.expect(ast.Token_EXPLAIN)
	n := &ast.ExplainStmt{}
	switch tok := p.peek(); tok.Kind {
	case ast.Token_ANALYZE, ast.Token_ANALYSE:
		p.next()
		n.Options = []*ast.Node{makeDefElem("analyze", nil, tok.Start)}
		if vtok := p.peek(); vtok.Kind == ast.Token_VERBOSE {
			p.next()
			n.Options = append(n.Options, makeDefElem("verbose", nil, vtok.Start))
		}
	case ast.Token_VERBOSE:
		p.next()
		n.Options = []*ast.Node{makeDefElem("verbose", nil, tok.Start)}
	case ast.Token('('):
		// EXPLAIN '(' could also start a parenthesized SelectStmt.
		if !p.looksLikeSelectAhead() {
			p.next()
			n.Options = p.parseUtilityOptionList()
		}
	}
	n.Query = p.parseExplainableStmt()
	return &ast.Node{Node: &ast.Node_ExplainStmt{ExplainStmt: n}}
}

// parseExplainableStmt is gram.y's ExplainableStmt.
func (p *parser) parseExplainableStmt() *ast.Node {
	switch p.kind() {
	case ast.Token_SELECT, ast.Token_TABLE, ast.Token_VALUES, ast.Token('('):
		return p.parseSelectStmt()
	case ast.Token_WITH, ast.Token_WITH_LA:
		return p.parseWithPrefixedStmt()
	case ast.Token_INSERT:
		return p.parseInsertStmt(nil)
	case ast.Token_UPDATE:
		return p.parseUpdateStmt(nil)
	case ast.Token_DELETE_P:
		return p.parseDeleteStmt(nil)
	case ast.Token_MERGE:
		return p.parseMergeStmt(nil)
	case ast.Token_DECLARE:
		return p.parseDeclareCursorStmt()
	case ast.Token_EXECUTE:
		return p.parseExecuteStmt()
	case ast.Token_REFRESH:
		return p.parseRefreshMatViewStmt()
	case ast.Token_CREATE:
		// Only CreateAsStmt / CreateMatViewStmt (and the AS EXECUTE form)
		// are explainable.
		p.next()
		persistence := p.parseOptTempAfterCreate()
		if p.kind() == ast.Token_MATERIALIZED && persistence != relPersistTemp {
			return p.parseCreateMatViewStmt(persistence)
		}
		p.expect(ast.Token_TABLE)
		ifNotExists := false
		if p.have(ast.Token_IF_P) {
			p.expect(ast.Token_NOT)
			p.expect(ast.Token_EXISTS)
			ifNotExists = true
		}
		rel := p.parseQualifiedName()
		rel.Relpersistence = persistence
		into := p.parseCreateAsTarget(rel)
		p.expect(ast.Token_AS)
		return p.finishCreateTableAs(into, ifNotExists)
	}
	p.syntaxErrorAt()
	return nil
}

// parseReindexStmt is gram.y's ReindexStmt.
func (p *parser) parseReindexStmt() *ast.Node {
	p.expect(ast.Token_REINDEX)
	n := &ast.ReindexStmt{}
	if p.have(ast.Token('(')) {
		n.Params = p.parseUtilityOptionList()
	}
	switch p.kind() {
	case ast.Token_INDEX, ast.Token_TABLE:
		if p.next().Kind == ast.Token_INDEX {
			n.Kind = ast.ReindexObjectType_REINDEX_OBJECT_INDEX
		} else {
			n.Kind = ast.ReindexObjectType_REINDEX_OBJECT_TABLE
		}
		if tok := p.peek(); tok.Kind == ast.Token_CONCURRENTLY {
			p.next()
			n.Params = append(n.Params, makeDefElem("concurrently", nil, tok.Start))
		}
		n.Relation = p.parseQualifiedName()
	case ast.Token_SCHEMA:
		p.next()
		n.Kind = ast.ReindexObjectType_REINDEX_OBJECT_SCHEMA
		if tok := p.peek(); tok.Kind == ast.Token_CONCURRENTLY {
			p.next()
			n.Params = append(n.Params, makeDefElem("concurrently", nil, tok.Start))
		}
		n.Name = p.name()
	case ast.Token_SYSTEM_P, ast.Token_DATABASE:
		if p.next().Kind == ast.Token_SYSTEM_P {
			n.Kind = ast.ReindexObjectType_REINDEX_OBJECT_SYSTEM
		} else {
			n.Kind = ast.ReindexObjectType_REINDEX_OBJECT_DATABASE
		}
		if tok := p.peek(); tok.Kind == ast.Token_CONCURRENTLY {
			p.next()
			n.Params = append(n.Params, makeDefElem("concurrently", nil, tok.Start))
		}
		if isColIdToken(p.peek()) {
			n.Name = p.name()
		}
	default:
		p.syntaxErrorAt()
	}
	return &ast.Node{Node: &ast.Node_ReindexStmt{ReindexStmt: n}}
}

// parseDoStmt is gram.y's DoStmt: DO dostmt_opt_list.
func (p *parser) parseDoStmt() *ast.Node {
	p.expect(ast.Token_DO)
	n := &ast.DoStmt{}
	for {
		tok := p.peek()
		switch tok.Kind {
		case ast.Token_SCONST:
			p.next()
			n.Args = append(n.Args, makeDefElem("as", nStr(tok.Str), tok.Start))
			continue
		case ast.Token_LANGUAGE:
			p.next()
			n.Args = append(n.Args, makeDefElem("language", nStr(p.nonReservedWordOrSconst()), tok.Start))
			continue
		}
		if n.Args == nil {
			p.syntaxErrorAt()
		}
		return &ast.Node{Node: &ast.Node_DoStmt{DoStmt: n}}
	}
}

// parseCreateCastStmt is gram.y's CreateCastStmt; CREATE CAST consumed by
// the dispatcher.
func (p *parser) parseCreateCastStmt() *ast.Node {
	n := &ast.CreateCastStmt{}
	p.expect(ast.Token('('))
	n.Sourcetype = p.parseTypename()
	p.expect(ast.Token_AS)
	n.Targettype = p.parseTypename()
	p.expect(ast.Token(')'))
	switch {
	case p.have(ast.Token_WITHOUT):
		p.expect(ast.Token_FUNCTION)
	case p.have(ast.Token_WITH), p.have(ast.Token_WITH_LA):
		switch {
		case p.have(ast.Token_FUNCTION):
			n.Func = p.parseFunctionWithArgtypes().GetObjectWithArgs()
		case p.have(ast.Token_INOUT):
			n.Inout = true
		default:
			p.syntaxErrorAt()
		}
	default:
		p.syntaxErrorAt()
	}
	// cast_context
	n.Context = ast.CoercionContext_COERCION_EXPLICIT
	if p.have(ast.Token_AS) {
		switch {
		case p.have(ast.Token_IMPLICIT_P):
			n.Context = ast.CoercionContext_COERCION_IMPLICIT
		case p.have(ast.Token_ASSIGNMENT):
			n.Context = ast.CoercionContext_COERCION_ASSIGNMENT
		default:
			p.syntaxErrorAt()
		}
	}
	return &ast.Node{Node: &ast.Node_CreateCastStmt{CreateCastStmt: n}}
}
