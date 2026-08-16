package parse

import (
	"github.com/sqlc-dev/oliphant/ast"
	"github.com/sqlc-dev/oliphant/internal/lexer"
)

// Identifier-class helpers: gram.y funnels every identifier-position
// consumption through a small set of productions encoding the four keyword
// reserved-ness categories plus the bare_label attribute. The lexer already
// classified each keyword token (Token.KeywordKind); a keyword token's Str is
// its canonical lower-case spelling, an IDENT's Str is the downcased,
// truncated identifier — in both cases exactly the C production's $$.

// isColIdToken reports whether tok can begin ColId:
// gram.y: ColId: IDENT | unreserved_keyword | col_name_keyword
func isColIdToken(tok lexer.Token) bool {
	switch tok.KeywordKind {
	case ast.KeywordKind_NO_KEYWORD:
		return tok.Kind == ast.Token_IDENT
	case ast.KeywordKind_UNRESERVED_KEYWORD, ast.KeywordKind_COL_NAME_KEYWORD:
		return true
	}
	return false
}

// isTypeFunctionNameToken:
// gram.y: type_function_name: IDENT | unreserved_keyword | type_func_name_keyword
func isTypeFunctionNameToken(tok lexer.Token) bool {
	switch tok.KeywordKind {
	case ast.KeywordKind_NO_KEYWORD:
		return tok.Kind == ast.Token_IDENT
	case ast.KeywordKind_UNRESERVED_KEYWORD, ast.KeywordKind_TYPE_FUNC_NAME_KEYWORD:
		return true
	}
	return false
}

// isNonReservedWordToken:
// gram.y: NonReservedWord: IDENT | unreserved_keyword | col_name_keyword |
// type_func_name_keyword
func isNonReservedWordToken(tok lexer.Token) bool {
	switch tok.KeywordKind {
	case ast.KeywordKind_NO_KEYWORD:
		return tok.Kind == ast.Token_IDENT
	case ast.KeywordKind_UNRESERVED_KEYWORD, ast.KeywordKind_COL_NAME_KEYWORD,
		ast.KeywordKind_TYPE_FUNC_NAME_KEYWORD:
		return true
	}
	return false
}

// isColLabelToken:
// gram.y: ColLabel: IDENT | unreserved_keyword | col_name_keyword |
// type_func_name_keyword | reserved_keyword
func isColLabelToken(tok lexer.Token) bool {
	if tok.KeywordKind != ast.KeywordKind_NO_KEYWORD {
		return true
	}
	return tok.Kind == ast.Token_IDENT
}

// isBareColLabelToken:
// gram.y: BareColLabel: IDENT | bare_label_keyword
func isBareColLabelToken(tok lexer.Token) bool {
	if tok.Kind == ast.Token_IDENT && tok.KeywordKind == ast.KeywordKind_NO_KEYWORD {
		return true
	}
	if tok.KeywordKind == ast.KeywordKind_NO_KEYWORD {
		return false
	}
	kw, ok := lexer.Keywords[tok.Str]
	return ok && kw.BareLabel
}

// colId consumes ColId or reports a syntax error.
func (p *parser) colId() string {
	tok := p.peek()
	if !isColIdToken(tok) {
		p.syntaxError(tok)
	}
	p.next()
	return tok.Str
}

// typeFunctionName consumes type_function_name.
func (p *parser) typeFunctionName() string {
	tok := p.peek()
	if !isTypeFunctionNameToken(tok) {
		p.syntaxError(tok)
	}
	p.next()
	return tok.Str
}

// nonReservedWord consumes NonReservedWord.
func (p *parser) nonReservedWord() string {
	tok := p.peek()
	if !isNonReservedWordToken(tok) {
		p.syntaxError(tok)
	}
	p.next()
	return tok.Str
}

// colLabel consumes ColLabel.
func (p *parser) colLabel() string {
	tok := p.peek()
	if !isColLabelToken(tok) {
		p.syntaxError(tok)
	}
	p.next()
	return tok.Str
}

// bareColLabel consumes BareColLabel.
func (p *parser) bareColLabel() string {
	tok := p.peek()
	if !isBareColLabelToken(tok) {
		p.syntaxError(tok)
	}
	p.next()
	return tok.Str
}

// sconst consumes Sconst.
func (p *parser) sconst() string {
	tok := p.expect(ast.Token_SCONST)
	return tok.Str
}

// iconst consumes Iconst.
func (p *parser) iconst() int32 {
	tok := p.expect(ast.Token_ICONST)
	return tok.Ival
}

// gram.y: attr_name: ColLabel
func (p *parser) attrName() string { return p.colLabel() }

// gram.y: name: ColId
func (p *parser) name() string { return p.colId() }

// gram.y: param_name: type_function_name
func (p *parser) paramName() string { return p.typeFunctionName() }

// gram.y: any_name: ColId | ColId attrs
// attrs: '.' attr_name | attrs '.' attr_name
func (p *parser) anyName() []*ast.Node {
	names := []*ast.Node{nStr(p.colId())}
	for p.have(ast.Token('.')) {
		names = append(names, nStr(p.attrName()))
	}
	return names
}

// gram.y: name_list: name | name_list ',' name
func (p *parser) nameList() []*ast.Node {
	names := []*ast.Node{nStr(p.name())}
	for p.have(ast.Token(',')) {
		names = append(names, nStr(p.name()))
	}
	return names
}

// gram.y: columnList: columnElem | columnList ',' columnElem
// columnElem: ColId
func (p *parser) columnList() []*ast.Node {
	cols := []*ast.Node{nStr(p.colId())}
	for p.have(ast.Token(',')) {
		cols = append(cols, nStr(p.colId()))
	}
	return cols
}
