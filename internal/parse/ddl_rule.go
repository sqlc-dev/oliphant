package parse

// RuleStmt, CreatePLangStmt, CreateTableSpaceStmt, CreateConversionStmt,
// CreateTransformStmt, and AlterTSConfigurationStmt.

import (
	"github.com/sqlc-dev/oliphant/ast"
)

// parseRuleStmt is gram.y's RuleStmt; CREATE [OR REPLACE] RULE consumed.
func (p *parser) parseRuleStmt(replace bool) *ast.Node {
	n := &ast.RuleStmt{Replace: replace, Rulename: p.name()}
	p.expect(ast.Token_AS)
	p.expect(ast.Token_ON)
	// event
	switch {
	case p.have(ast.Token_SELECT):
		n.Event = ast.CmdType_CMD_SELECT
	case p.have(ast.Token_UPDATE):
		n.Event = ast.CmdType_CMD_UPDATE
	case p.have(ast.Token_DELETE_P):
		n.Event = ast.CmdType_CMD_DELETE
	case p.have(ast.Token_INSERT):
		n.Event = ast.CmdType_CMD_INSERT
	default:
		p.syntaxErrorAt()
	}
	p.expect(ast.Token_TO)
	n.Relation = p.parseQualifiedName()
	if p.have(ast.Token_WHERE) {
		n.WhereClause = p.parseAExpr(0)
	}
	p.expect(ast.Token_DO)
	// opt_instead
	switch {
	case p.have(ast.Token_INSTEAD):
		n.Instead = true
	case p.have(ast.Token_ALSO):
	}
	// RuleActionList
	switch {
	case p.have(ast.Token_NOTHING):
	case p.have(ast.Token('(')):
		// RuleActionMulti
		for {
			if stmt := p.parseRuleActionStmtOrEmpty(); stmt != nil {
				n.Actions = append(n.Actions, stmt)
			}
			if !p.have(ast.Token(';')) {
				break
			}
		}
		p.expect(ast.Token(')'))
	default:
		n.Actions = []*ast.Node{p.parseRuleActionStmt()}
	}
	return &ast.Node{Node: &ast.Node_RuleStmt{RuleStmt: n}}
}

// parseRuleActionStmt is gram.y's RuleActionStmt.
func (p *parser) parseRuleActionStmt() *ast.Node {
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
	case ast.Token_NOTIFY:
		return p.parseNotifyStmt()
	}
	p.syntaxErrorAt()
	return nil
}

// parseRuleActionStmtOrEmpty is gram.y's RuleActionStmtOrEmpty.
func (p *parser) parseRuleActionStmtOrEmpty() *ast.Node {
	switch p.kind() {
	case ast.Token(';'), ast.Token(')'):
		return nil
	}
	return p.parseRuleActionStmt()
}

// parseCreatePLangStmt is gram.y's CreatePLangStmt; CREATE [OR REPLACE]
// consumed, [TRUSTED] [PROCEDURAL] LANGUAGE ahead.
func (p *parser) parseCreatePLangStmt(replace bool) *ast.Node {
	trusted := p.have(ast.Token_TRUSTED)
	p.have(ast.Token_PROCEDURAL)
	p.expect(ast.Token_LANGUAGE)
	name := p.name()
	if p.kind() != ast.Token_HANDLER {
		// Parameterless CREATE LANGUAGE is interpreted as CREATE EXTENSION,
		// with OR REPLACE translated to IF NOT EXISTS.
		return &ast.Node{Node: &ast.Node_CreateExtensionStmt{CreateExtensionStmt: &ast.CreateExtensionStmt{
			IfNotExists: replace,
			Extname:     name,
		}}}
	}
	p.next()
	n := &ast.CreatePLangStmt{
		Replace:   replace,
		Plname:    name,
		Pltrusted: trusted,
		Plhandler: p.parseHandlerName(),
	}
	if p.have(ast.Token_INLINE_P) {
		n.Plinline = p.parseHandlerName()
	}
	// opt_validator
	switch {
	case p.have(ast.Token_VALIDATOR):
		n.Plvalidator = p.parseHandlerName()
	case p.kind() == ast.Token_NO && p.kindN(1) == ast.Token_VALIDATOR:
		p.next()
		p.next()
	}
	return &ast.Node{Node: &ast.Node_CreatePlangStmt{CreatePlangStmt: n}}
}

// parseCreateTableSpaceStmt is gram.y's CreateTableSpaceStmt; CREATE
// TABLESPACE consumed.
func (p *parser) parseCreateTableSpaceStmt() *ast.Node {
	n := &ast.CreateTableSpaceStmt{Tablespacename: p.name()}
	if p.have(ast.Token_OWNER) {
		n.Owner = p.parseRoleSpec()
	}
	p.expect(ast.Token_LOCATION)
	n.Location = p.sconst()
	if p.kind() == ast.Token_WITH && p.kindN(1) == ast.Token('(') {
		p.next()
		n.Options = p.parseReloptions()
	}
	return &ast.Node{Node: &ast.Node_CreateTableSpaceStmt{CreateTableSpaceStmt: n}}
}

// parseCreateConversionStmt is gram.y's CreateConversionStmt; CREATE
// [DEFAULT] CONVERSION consumed, def passed by the dispatcher.
func (p *parser) parseCreateConversionStmt(isDefault bool) *ast.Node {
	n := &ast.CreateConversionStmt{Def: isDefault, ConversionName: p.anyName()}
	p.expect(ast.Token_FOR)
	n.ForEncodingName = p.sconst()
	p.expect(ast.Token_TO)
	n.ToEncodingName = p.sconst()
	p.expect(ast.Token_FROM)
	n.FuncName = p.anyName()
	return &ast.Node{Node: &ast.Node_CreateConversionStmt{CreateConversionStmt: n}}
}

// parseCreateTransformStmt is gram.y's CreateTransformStmt; CREATE
// [OR REPLACE] TRANSFORM consumed.
func (p *parser) parseCreateTransformStmt(replace bool) *ast.Node {
	n := &ast.CreateTransformStmt{Replace: replace}
	p.expect(ast.Token_FOR)
	n.TypeName = p.parseTypename()
	p.expect(ast.Token_LANGUAGE)
	n.Lang = p.name()
	p.expect(ast.Token('('))
	// transform_element_list
	parseElem := func() (bool, *ast.ObjectWithArgs) {
		fromSQL := false
		switch {
		case p.have(ast.Token_FROM):
			fromSQL = true
		case p.have(ast.Token_TO):
		default:
			p.syntaxErrorAt()
		}
		p.expect(ast.Token_SQL_P)
		if !p.have(ast.Token_WITH) && !p.have(ast.Token_WITH_LA) {
			p.syntaxErrorAt()
		}
		p.expect(ast.Token_FUNCTION)
		return fromSQL, p.parseFunctionWithArgtypes().GetObjectWithArgs()
	}
	from1, fn1 := parseElem()
	if from1 {
		n.Fromsql = fn1
	} else {
		n.Tosql = fn1
	}
	if p.have(ast.Token(',')) {
		// The second element must go the other direction.
		dtok := p.peek()
		from2, fn2 := parseElem()
		if from2 == from1 {
			p.syntaxError(dtok)
		}
		if from2 {
			n.Fromsql = fn2
		} else {
			n.Tosql = fn2
		}
	}
	p.expect(ast.Token(')'))
	return &ast.Node{Node: &ast.Node_CreateTransformStmt{CreateTransformStmt: n}}
}
