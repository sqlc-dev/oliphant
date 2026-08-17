package parse

// SET/RESET/SHOW and the small standalone utility statements:
// VariableSetStmt, VariableResetStmt, VariableShowStmt, ConstraintsSetStmt,
// CheckPointStmt, DiscardStmt, plus the transaction_mode machinery shared
// with TransactionStmt.

import (
	"github.com/sqlc-dev/oliphant/ast"
)

// parseVariableSetStmt is gram.y's VariableSetStmt:
//
//	SET set_rest | SET LOCAL set_rest | SET SESSION set_rest
func (p *parser) parseVariableSetStmt() *ast.Node {
	p.expect(ast.Token_SET)
	var n *ast.VariableSetStmt
	switch p.kind() {
	case ast.Token_LOCAL:
		// LOCAL is also a ColId: "SET local = ..." names a variable
		// "local". The LALR tables shift LOCAL and decide by the next
		// token: TO/'='/'.'  continue var_name, anything else means the
		// SET LOCAL prefix.
		if p.varNameContinues(1) {
			n = p.parseSetRest()
		} else {
			p.next()
			n = p.parseSetRest()
			n.IsLocal = true
		}
	case ast.Token_SESSION:
		// Same shape: SESSION AUTHORIZATION/CHARACTERISTICS belong to
		// set_rest; "SET session = x" names a variable; otherwise SESSION
		// is the scope prefix.
		switch {
		case p.varNameContinues(1),
			p.kindN(1) == ast.Token_CHARACTERISTICS,
			p.kindN(1) == ast.Token_AUTHORIZATION:
			n = p.parseSetRest()
		default:
			p.next()
			n = p.parseSetRest()
		}
	default:
		n = p.parseSetRest()
	}
	return &ast.Node{Node: &ast.Node_VariableSetStmt{VariableSetStmt: n}}
}

// varNameContinues reports whether the token after position n keeps the
// current keyword inside a var_name (generic_set's var_name TO/'='/FROM
// CURRENT paths, or a qualified name continuing with '.').
func (p *parser) varNameContinues(n int) bool {
	switch p.kindN(n) {
	case ast.Token_TO, ast.Token('='), ast.Token('.'):
		return true
	case ast.Token_FROM:
		// var_name FROM CURRENT_P
		return p.kindN(n+1) == ast.Token_CURRENT_P
	}
	return false
}

// parseSetRest is gram.y's set_rest.
func (p *parser) parseSetRest() *ast.VariableSetStmt {
	tok := p.peek()
	switch tok.Kind {
	case ast.Token_TRANSACTION:
		if !p.varNameContinues(1) {
			p.next()
			if p.have(ast.Token_SNAPSHOT) {
				// set_rest_more: TRANSACTION SNAPSHOT Sconst
				stok := p.peek()
				return &ast.VariableSetStmt{
					Kind:     ast.VariableSetKind_VAR_SET_MULTI,
					Name:     "TRANSACTION SNAPSHOT",
					Args:     []*ast.Node{makeStringConst(p.sconst(), stok.Start)},
					Location: stok.Start,
				}
			}
			// set_rest: TRANSACTION transaction_mode_list
			return &ast.VariableSetStmt{
				Kind:       ast.VariableSetKind_VAR_SET_MULTI,
				Name:       "TRANSACTION",
				Args:       p.parseTransactionModeList(),
				JumbleArgs: true,
				Location:   -1,
			}
		}
	case ast.Token_SESSION:
		if p.kindN(1) == ast.Token_CHARACTERISTICS {
			// set_rest: SESSION CHARACTERISTICS AS TRANSACTION
			// transaction_mode_list
			p.next()
			p.next()
			p.expect(ast.Token_AS)
			p.expect(ast.Token_TRANSACTION)
			return &ast.VariableSetStmt{
				Kind:       ast.VariableSetKind_VAR_SET_MULTI,
				Name:       "SESSION CHARACTERISTICS",
				Args:       p.parseTransactionModeList(),
				JumbleArgs: true,
				Location:   -1,
			}
		}
	}
	return p.parseSetRestMore()
}

// parseSetRestMore is gram.y's set_rest_more. The keyword-headed special
// syntaxes each also admit the keyword as a plain var_name; the lookahead
// checks reproduce how the LALR tables decide between them.
func (p *parser) parseSetRestMore() *ast.VariableSetStmt {
	tok := p.peek()
	switch tok.Kind {
	case ast.Token_TIME:
		// TIME ZONE zone_value
		if p.kindN(1) == ast.Token_ZONE {
			p.next()
			p.next()
			n := &ast.VariableSetStmt{
				Kind:       ast.VariableSetKind_VAR_SET_VALUE,
				Name:       "timezone",
				JumbleArgs: true,
				Location:   -1,
			}
			if zv := p.parseZoneValue(); zv != nil {
				n.Args = []*ast.Node{zv}
			} else {
				n.Kind = ast.VariableSetKind_VAR_SET_DEFAULT
			}
			return n
		}
	case ast.Token_CATALOG_P:
		if p.kindN(1) == ast.Token_SCONST {
			p.next()
			stok := p.peek()
			p.ereport("base_yyparse", "current database cannot be changed", stok.Start)
		}
	case ast.Token_SCHEMA:
		switch p.kindN(1) {
		case ast.Token_SCONST:
			p.next()
			stok := p.peek()
			return &ast.VariableSetStmt{
				Kind:     ast.VariableSetKind_VAR_SET_VALUE,
				Name:     "search_path",
				Args:     []*ast.Node{makeStringConst(p.sconst(), stok.Start)},
				Location: stok.Start,
			}
		case ast.Token_PARAM:
			p.next()
			prm := p.next()
			return &ast.VariableSetStmt{
				Kind: ast.VariableSetKind_VAR_SET_VALUE,
				Name: "search_path",
				Args: []*ast.Node{makeParamRef(prm.Ival, prm.Start)},
			}
		}
	case ast.Token_NAMES:
		if !p.varNameContinues(1) {
			// NAMES opt_encoding
			p.next()
			n := &ast.VariableSetStmt{
				Kind: ast.VariableSetKind_VAR_SET_VALUE,
				Name: "client_encoding",
			}
			etok := p.peek()
			switch etok.Kind {
			case ast.Token_SCONST:
				n.Args = []*ast.Node{makeStringConst(p.sconst(), etok.Start)}
				n.Location = etok.Start
			case ast.Token_DEFAULT:
				p.next()
				n.Kind = ast.VariableSetKind_VAR_SET_DEFAULT
				n.Location = etok.Start
			default:
				// empty opt_encoding: @2 is -1 (YYLLOC_DEFAULT of an empty
				// production).
				n.Kind = ast.VariableSetKind_VAR_SET_DEFAULT
				n.Location = -1
			}
			return n
		}
	case ast.Token_ROLE:
		if !p.varNameContinues(1) {
			p.next()
			vtok := p.peek()
			if vtok.Kind == ast.Token_PARAM {
				p.next()
				return &ast.VariableSetStmt{
					Kind: ast.VariableSetKind_VAR_SET_VALUE,
					Name: "role",
					Args: []*ast.Node{makeParamRef(vtok.Ival, vtok.Start)},
				}
			}
			return &ast.VariableSetStmt{
				Kind:     ast.VariableSetKind_VAR_SET_VALUE,
				Name:     "role",
				Args:     []*ast.Node{makeStringConst(p.nonReservedWordOrSconst(), vtok.Start)},
				Location: vtok.Start,
			}
		}
	case ast.Token_SESSION:
		if p.kindN(1) == ast.Token_AUTHORIZATION {
			p.next()
			p.next()
			vtok := p.peek()
			switch vtok.Kind {
			case ast.Token_DEFAULT:
				p.next()
				return &ast.VariableSetStmt{
					Kind:     ast.VariableSetKind_VAR_SET_DEFAULT,
					Name:     "session_authorization",
					Location: -1,
				}
			case ast.Token_PARAM:
				p.next()
				return &ast.VariableSetStmt{
					Kind: ast.VariableSetKind_VAR_SET_VALUE,
					Name: "session_authorization",
					Args: []*ast.Node{makeParamRef(vtok.Ival, vtok.Start)},
				}
			}
			return &ast.VariableSetStmt{
				Kind:     ast.VariableSetKind_VAR_SET_VALUE,
				Name:     "session_authorization",
				Args:     []*ast.Node{makeStringConst(p.nonReservedWordOrSconst(), vtok.Start)},
				Location: vtok.Start,
			}
		}
	case ast.Token_XML_P:
		if p.kindN(1) == ast.Token_OPTION {
			p.next()
			p.next()
			vtok := p.peek()
			opt := p.parseDocumentOrContent()
			s := "CONTENT"
			if opt == ast.XmlOptionType_XMLOPTION_DOCUMENT {
				s = "DOCUMENT"
			}
			return &ast.VariableSetStmt{
				Kind:       ast.VariableSetKind_VAR_SET_VALUE,
				Name:       "xmloption",
				Args:       []*ast.Node{makeStringConst(s, vtok.Start)},
				JumbleArgs: true,
				Location:   -1,
			}
		}
	}

	// generic_set / var_name FROM CURRENT_P
	name := p.parseVarName()
	switch {
	case p.have(ast.Token_TO), p.have(ast.Token('=')):
		if p.kind() == ast.Token_DEFAULT {
			p.next()
			return &ast.VariableSetStmt{
				Kind:     ast.VariableSetKind_VAR_SET_DEFAULT,
				Name:     name,
				Location: -1,
			}
		}
		vtok := p.peek()
		return &ast.VariableSetStmt{
			Kind:     ast.VariableSetKind_VAR_SET_VALUE,
			Name:     name,
			Args:     p.parseVarList(),
			Location: vtok.Start,
		}
	case p.kind() == ast.Token_FROM:
		p.next()
		p.expect(ast.Token_CURRENT_P)
		return &ast.VariableSetStmt{
			Kind:     ast.VariableSetKind_VAR_SET_CURRENT,
			Name:     name,
			Location: -1,
		}
	}
	p.syntaxErrorAt()
	return nil
}

// parseVarName is gram.y's var_name: ColId ('.' ColId)*.
func (p *parser) parseVarName() string {
	name := p.colId()
	for p.have(ast.Token('.')) {
		name += "." + p.colId()
	}
	return name
}

// parseVarList is gram.y's var_list.
func (p *parser) parseVarList() []*ast.Node {
	list := []*ast.Node{p.parseVarValue()}
	for p.have(ast.Token(',')) {
		list = append(list, p.parseVarValue())
	}
	return list
}

// parseVarValue is gram.y's var_value.
func (p *parser) parseVarValue() *ast.Node {
	tok := p.peek()
	switch tok.Kind {
	case ast.Token_PARAM:
		p.next()
		return makeParamRef(tok.Ival, tok.Start)
	case ast.Token_FCONST, ast.Token_ICONST, ast.Token('+'), ast.Token('-'):
		return makeAConst(p.parseNumericOnly(), tok.Start)
	}
	return makeStringConst(p.parseOptBooleanOrString(), tok.Start)
}

// parseZoneValue is gram.y's zone_value; nil means DEFAULT/LOCAL.
func (p *parser) parseZoneValue() *ast.Node {
	tok := p.peek()
	switch tok.Kind {
	case ast.Token_SCONST:
		p.next()
		return makeStringConst(tok.Str, tok.Start)
	case ast.Token_PARAM:
		p.next()
		return makeParamRef(tok.Ival, tok.Start)
	case ast.Token_IDENT:
		p.next()
		return makeStringConst(tok.Str, tok.Start)
	case ast.Token_INTERVAL:
		p.next()
		if p.have(ast.Token('(')) {
			// ConstInterval '(' Iconst ')' Sconst
			itok := p.peek()
			iv := p.iconst()
			p.expect(ast.Token(')'))
			t := SystemTypeName("interval")
			t.Location = tok.Start
			t.Typmods = []*ast.Node{
				makeIntConst(intervalFullRange, -1),
				makeIntConst(iv, itok.Start),
			}
			stok := p.peek()
			return makeStringConstCast(p.sconst(), stok.Start, t)
		}
		// ConstInterval Sconst opt_interval
		stok := p.peek()
		s := p.sconst()
		mtok := p.peek()
		typmods := p.parseOptInterval()
		if typmods != nil {
			c := asAConst(typmods[0])
			if ival := c.GetIval().GetIval(); ival&^(intervalMaskHour|intervalMaskMinute) != 0 {
				p.ereport("base_yyparse", "time zone interval must be HOUR or HOUR TO MINUTE", mtok.Start)
			}
		}
		t := SystemTypeName("interval")
		t.Location = tok.Start
		t.Typmods = typmods
		return makeStringConstCast(s, stok.Start, t)
	case ast.Token_FCONST, ast.Token_ICONST, ast.Token('+'), ast.Token('-'):
		return makeAConst(p.parseNumericOnly(), tok.Start)
	case ast.Token_DEFAULT, ast.Token_LOCAL:
		p.next()
		return nil
	}
	p.syntaxErrorAt()
	return nil
}

// nonReservedWordOrSconst is gram.y's NonReservedWord_or_Sconst.
func (p *parser) nonReservedWordOrSconst() string {
	if p.kind() == ast.Token_SCONST {
		return p.sconst()
	}
	return p.nonReservedWord()
}

// parseIsoLevel is gram.y's iso_level.
func (p *parser) parseIsoLevel() string {
	switch p.kind() {
	case ast.Token_READ:
		p.next()
		switch p.kind() {
		case ast.Token_UNCOMMITTED:
			p.next()
			return "read uncommitted"
		case ast.Token_COMMITTED:
			p.next()
			return "read committed"
		}
		p.syntaxErrorAt()
	case ast.Token_REPEATABLE:
		p.next()
		p.expect(ast.Token_READ)
		return "repeatable read"
	case ast.Token_SERIALIZABLE:
		p.next()
		return "serializable"
	}
	p.syntaxErrorAt()
	return ""
}

// transactionModeItemStarts reports whether the lookahead begins a
// transaction_mode_item.
func (p *parser) transactionModeItemStarts() bool {
	switch p.kind() {
	case ast.Token_ISOLATION, ast.Token_DEFERRABLE:
		return true
	case ast.Token_READ:
		k := p.kindN(1)
		return k == ast.Token_ONLY || k == ast.Token_WRITE
	case ast.Token_NOT:
		return p.kindN(1) == ast.Token_DEFERRABLE
	}
	return false
}

// parseTransactionModeItem is gram.y's transaction_mode_item.
func (p *parser) parseTransactionModeItem() *ast.Node {
	tok := p.peek()
	switch tok.Kind {
	case ast.Token_ISOLATION:
		p.next()
		p.expect(ast.Token_LEVEL)
		ltok := p.peek()
		return makeDefElem("transaction_isolation", makeStringConst(p.parseIsoLevel(), ltok.Start), tok.Start)
	case ast.Token_READ:
		p.next()
		switch p.kind() {
		case ast.Token_ONLY:
			p.next()
			return makeDefElem("transaction_read_only", makeIntConst(1, tok.Start), tok.Start)
		case ast.Token_WRITE:
			p.next()
			return makeDefElem("transaction_read_only", makeIntConst(0, tok.Start), tok.Start)
		}
		p.syntaxErrorAt()
	case ast.Token_DEFERRABLE:
		p.next()
		return makeDefElem("transaction_deferrable", makeIntConst(1, tok.Start), tok.Start)
	case ast.Token_NOT:
		p.next()
		p.expect(ast.Token_DEFERRABLE)
		return makeDefElem("transaction_deferrable", makeIntConst(0, tok.Start), tok.Start)
	}
	p.syntaxErrorAt()
	return nil
}

// parseTransactionModeList is gram.y's transaction_mode_list: items joined
// by commas or plain juxtaposition.
func (p *parser) parseTransactionModeList() []*ast.Node {
	list := []*ast.Node{p.parseTransactionModeItem()}
	for {
		if p.kind() == ast.Token(',') && p.transactionModeItemStartsAt(1) {
			p.next()
			list = append(list, p.parseTransactionModeItem())
			continue
		}
		if p.transactionModeItemStarts() {
			list = append(list, p.parseTransactionModeItem())
			continue
		}
		return list
	}
}

// transactionModeItemStartsAt is transactionModeItemStarts at offset n.
func (p *parser) transactionModeItemStartsAt(n int) bool {
	switch p.kindN(n) {
	case ast.Token_ISOLATION, ast.Token_DEFERRABLE:
		return true
	case ast.Token_READ:
		k := p.kindN(n + 1)
		return k == ast.Token_ONLY || k == ast.Token_WRITE
	case ast.Token_NOT:
		return p.kindN(n+1) == ast.Token_DEFERRABLE
	}
	return false
}

// parseVariableResetStmt is gram.y's VariableResetStmt: RESET reset_rest.
func (p *parser) parseVariableResetStmt() *ast.Node {
	p.expect(ast.Token_RESET)
	return &ast.Node{Node: &ast.Node_VariableSetStmt{VariableSetStmt: p.parseResetRest()}}
}

// parseResetRest is gram.y's reset_rest.
func (p *parser) parseResetRest() *ast.VariableSetStmt {
	switch p.kind() {
	case ast.Token_TIME:
		if p.kindN(1) == ast.Token_ZONE {
			p.next()
			p.next()
			return &ast.VariableSetStmt{Kind: ast.VariableSetKind_VAR_RESET, Name: "timezone", Location: -1}
		}
	case ast.Token_TRANSACTION:
		if p.kindN(1) == ast.Token_ISOLATION {
			p.next()
			p.next()
			p.expect(ast.Token_LEVEL)
			return &ast.VariableSetStmt{Kind: ast.VariableSetKind_VAR_RESET, Name: "transaction_isolation", Location: -1}
		}
	case ast.Token_SESSION:
		if p.kindN(1) == ast.Token_AUTHORIZATION {
			p.next()
			p.next()
			return &ast.VariableSetStmt{Kind: ast.VariableSetKind_VAR_RESET, Name: "session_authorization", Location: -1}
		}
	case ast.Token_ALL:
		p.next()
		return &ast.VariableSetStmt{Kind: ast.VariableSetKind_VAR_RESET_ALL, Location: -1}
	}
	// generic_reset: var_name
	return &ast.VariableSetStmt{Kind: ast.VariableSetKind_VAR_RESET, Name: p.parseVarName(), Location: -1}
}

// parseSetResetClause is gram.y's SetResetClause (SET or RESET without
// LOCAL, used by ALTER ... SET).
func (p *parser) parseSetResetClause() *ast.VariableSetStmt {
	if p.have(ast.Token_SET) {
		return p.parseSetRest()
	}
	if p.kind() == ast.Token_RESET {
		p.next()
		return p.parseResetRest()
	}
	p.syntaxErrorAt()
	return nil
}

// parseFunctionSetResetClause is gram.y's FunctionSetResetClause.
func (p *parser) parseFunctionSetResetClause() *ast.VariableSetStmt {
	if p.have(ast.Token_SET) {
		return p.parseSetRestMore()
	}
	if p.kind() == ast.Token_RESET {
		p.next()
		return p.parseResetRest()
	}
	p.syntaxErrorAt()
	return nil
}

// parseVariableShowStmt is gram.y's VariableShowStmt.
func (p *parser) parseVariableShowStmt() *ast.Node {
	p.expect(ast.Token_SHOW)
	n := &ast.VariableShowStmt{}
	switch p.kind() {
	case ast.Token_TIME:
		if p.kindN(1) == ast.Token_ZONE {
			p.next()
			p.next()
			n.Name = "timezone"
			return nVariableShowStmt(n)
		}
	case ast.Token_TRANSACTION:
		if p.kindN(1) == ast.Token_ISOLATION {
			p.next()
			p.next()
			p.expect(ast.Token_LEVEL)
			n.Name = "transaction_isolation"
			return nVariableShowStmt(n)
		}
	case ast.Token_SESSION:
		if p.kindN(1) == ast.Token_AUTHORIZATION {
			p.next()
			p.next()
			n.Name = "session_authorization"
			return nVariableShowStmt(n)
		}
	case ast.Token_ALL:
		p.next()
		n.Name = "all"
		return nVariableShowStmt(n)
	}
	n.Name = p.parseVarName()
	return nVariableShowStmt(n)
}

// parseConstraintsSetStmt is gram.y's ConstraintsSetStmt:
//
//	SET CONSTRAINTS constraints_set_list constraints_set_mode
//
// The caller has already consumed SET.
func (p *parser) parseConstraintsSetStmt() *ast.Node {
	p.expect(ast.Token_CONSTRAINTS)
	n := &ast.ConstraintsSetStmt{}
	// constraints_set_list: ALL | qualified_name_list
	if !p.have(ast.Token_ALL) {
		n.Constraints = p.parseQualifiedNameList()
	}
	// constraints_set_mode: DEFERRED | IMMEDIATE
	switch p.kind() {
	case ast.Token_DEFERRED:
		p.next()
		n.Deferred = true
	case ast.Token_IMMEDIATE:
		p.next()
	default:
		p.syntaxErrorAt()
	}
	return &ast.Node{Node: &ast.Node_ConstraintsSetStmt{ConstraintsSetStmt: n}}
}

// parseCheckPointStmt is gram.y's CheckPointStmt.
func (p *parser) parseCheckPointStmt() *ast.Node {
	p.expect(ast.Token_CHECKPOINT)
	return &ast.Node{Node: &ast.Node_CheckPointStmt{CheckPointStmt: &ast.CheckPointStmt{}}}
}

// parseDiscardStmt is gram.y's DiscardStmt.
func (p *parser) parseDiscardStmt() *ast.Node {
	p.expect(ast.Token_DISCARD)
	n := &ast.DiscardStmt{}
	switch p.kind() {
	case ast.Token_ALL:
		p.next()
		n.Target = ast.DiscardMode_DISCARD_ALL
	case ast.Token_TEMP, ast.Token_TEMPORARY:
		p.next()
		n.Target = ast.DiscardMode_DISCARD_TEMP
	case ast.Token_PLANS:
		p.next()
		n.Target = ast.DiscardMode_DISCARD_PLANS
	case ast.Token_SEQUENCES:
		p.next()
		n.Target = ast.DiscardMode_DISCARD_SEQUENCES
	default:
		p.syntaxErrorAt()
	}
	return &ast.Node{Node: &ast.Node_DiscardStmt{DiscardStmt: n}}
}

// makeAConst is gram.y's makeAConst: wrap a NumericOnly value node in an
// A_Const at the given location.
func makeAConst(v *ast.Node, location int32) *ast.Node {
	c := &ast.A_Const{Location: location}
	switch w := v.Node.(type) {
	case *ast.Node_Integer:
		c.Val = &ast.A_Const_Ival{Ival: w.Integer}
	case *ast.Node_Float:
		c.Val = &ast.A_Const_Fval{Fval: w.Float}
	}
	return nAConst(c)
}

// parseQualifiedNameList is gram.y's qualified_name_list.
func (p *parser) parseQualifiedNameList() []*ast.Node {
	list := []*ast.Node{nRangeVar(p.parseQualifiedName())}
	for p.have(ast.Token(',')) {
		list = append(list, nRangeVar(p.parseQualifiedName()))
	}
	return list
}

func nVariableShowStmt(n *ast.VariableShowStmt) *ast.Node {
	return &ast.Node{Node: &ast.Node_VariableShowStmt{VariableShowStmt: n}}
}
