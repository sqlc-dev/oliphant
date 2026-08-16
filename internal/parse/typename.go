package parse

import (
	"github.com/sqlc-dev/oliphant/ast"
)

// datetime.h: INTERVAL_MASK(b) = 1 << b, with the field codes below.
const (
	intervalMaskMonth  = 1 << 1
	intervalMaskYear   = 1 << 2
	intervalMaskDay    = 1 << 3
	intervalMaskHour   = 1 << 10
	intervalMaskMinute = 1 << 11
	intervalMaskSecond = 1 << 12
	intervalFullRange  = 0x7FFF // INTERVAL_FULL_RANGE
)

// parseTypename is gram.y's Typename: SimpleTypename with optional SETOF and
// array bounds.
func (p *parser) parseTypename() *ast.TypeName {
	setof := p.have(ast.Token_SETOF)
	t := p.parseSimpleTypename()
	t.Setof = setof

	// Typename: ... opt_array_bounds | ... ARRAY ['[' Iconst ']']
	if p.kind() == ast.Token_ARRAY {
		p.next()
		if p.have(ast.Token('[')) {
			n := p.iconst()
			p.expect(ast.Token(']'))
			t.ArrayBounds = []*ast.Node{nInteger(n)}
		} else {
			t.ArrayBounds = []*ast.Node{nInteger(-1)}
		}
		return t
	}
	for p.kind() == ast.Token('[') {
		p.next()
		if p.have(ast.Token(']')) {
			t.ArrayBounds = append(t.ArrayBounds, nInteger(-1))
			continue
		}
		n := p.iconst()
		p.expect(ast.Token(']'))
		t.ArrayBounds = append(t.ArrayBounds, nInteger(n))
	}
	return t
}

// parseSimpleTypename is gram.y's SimpleTypename.
func (p *parser) parseSimpleTypename() *ast.TypeName {
	tok := p.peek()
	if t := p.parseKeywordTypename(false); t != nil {
		return t
	}

	// GenericType: type_function_name [attrs] opt_type_modifiers
	if !isTypeFunctionNameToken(tok) {
		p.syntaxError(tok)
	}
	p.next()
	names := []*ast.Node{nStr(tok.Str)}
	for p.have(ast.Token('.')) {
		names = append(names, nStr(p.attrName()))
	}
	t := makeTypeNameFromNameList(names)
	t.Location = tok.Start
	if p.have(ast.Token('(')) {
		t.Typmods = p.parseExprList()
		p.expect(ast.Token(')'))
	}
	return t
}

// parseKeywordTypename parses the keyword-headed SimpleTypename families
// (Numeric, Bit, Character, ConstDatetime, ConstInterval opt_interval,
// JsonType), returning nil if the current token doesn't start one. When
// constForm is true, the ConstBit/ConstCharacter variants apply (unadorned
// BIT/CHAR default lengths are dropped) and INTERVAL is left to the caller
// (AexprConst handles its postfix options).
func (p *parser) parseKeywordTypename(constForm bool) *ast.TypeName {
	tok := p.peek()
	sysType := func(name string) *ast.TypeName {
		p.next()
		t := SystemTypeName(name)
		t.Location = tok.Start
		return t
	}
	switch tok.Kind {
	// Numeric
	case ast.Token_INT_P, ast.Token_INTEGER:
		return sysType("int4")
	case ast.Token_SMALLINT:
		return sysType("int2")
	case ast.Token_BIGINT:
		return sysType("int8")
	case ast.Token_REAL:
		return sysType("float4")
	case ast.Token_FLOAT_P:
		// FLOAT_P opt_float
		p.next()
		var t *ast.TypeName
		if p.have(ast.Token('(')) {
			ptok := p.peek()
			prec := p.iconst()
			switch {
			case prec < 1:
				p.ereport("base_yyparse", "precision for type float must be at least 1 bit", ptok.Start)
			case prec <= 24:
				t = SystemTypeName("float4")
			case prec <= 53:
				t = SystemTypeName("float8")
			default:
				p.ereport("base_yyparse", "precision for type float must be less than 54 bits", ptok.Start)
			}
			p.expect(ast.Token(')'))
		} else {
			t = SystemTypeName("float8")
		}
		t.Location = tok.Start
		return t
	case ast.Token_DOUBLE_P:
		// DOUBLE_P PRECISION; a bare "double" is a GenericType name (the
		// keyword is unreserved and PRECISION is what commits the Numeric
		// reading).
		if p.kindN(1) != ast.Token_PRECISION {
			return nil
		}
		p.next()
		p.next()
		t := SystemTypeName("float8")
		t.Location = tok.Start
		return t
	case ast.Token_DECIMAL_P, ast.Token_DEC, ast.Token_NUMERIC:
		p.next()
		t := SystemTypeName("numeric")
		t.Location = tok.Start
		if p.have(ast.Token('(')) {
			t.Typmods = p.parseExprList()
			p.expect(ast.Token(')'))
		}
		return t
	case ast.Token_BOOLEAN_P:
		return sysType("bool")

	// Bit / ConstBit
	case ast.Token_BIT:
		p.next()
		varying := p.have(ast.Token_VARYING)
		var t *ast.TypeName
		if p.have(ast.Token('(')) {
			// BitWithLength
			if varying {
				t = SystemTypeName("varbit")
			} else {
				t = SystemTypeName("bit")
			}
			t.Typmods = p.parseExprList()
			p.expect(ast.Token(')'))
		} else {
			// BitWithoutLength: bit defaults to bit(1), varbit to no limit
			if varying {
				t = SystemTypeName("varbit")
			} else {
				t = SystemTypeName("bit")
				if !constForm {
					t.Typmods = []*ast.Node{makeIntConst(1, -1)}
				}
			}
		}
		t.Location = tok.Start
		return t

	// Character / ConstCharacter
	case ast.Token_CHARACTER, ast.Token_CHAR_P, ast.Token_VARCHAR,
		ast.Token_NCHAR, ast.Token_NATIONAL:
		return p.parseCharacterTypename(constForm)

	// ConstDatetime
	case ast.Token_TIMESTAMP, ast.Token_TIME:
		p.next()
		var typmods []*ast.Node
		if p.have(ast.Token('(')) {
			itok := p.peek()
			n := p.iconst()
			p.expect(ast.Token(')'))
			typmods = []*ast.Node{makeIntConst(n, itok.Start)}
		}
		tz := p.parseOptTimezone()
		var name string
		if tok.Kind == ast.Token_TIMESTAMP {
			name = "timestamp"
			if tz {
				name = "timestamptz"
			}
		} else {
			name = "time"
			if tz {
				name = "timetz"
			}
		}
		t := SystemTypeName(name)
		t.Typmods = typmods
		t.Location = tok.Start
		return t

	// ConstInterval (SimpleTypename adds opt_interval / '(' Iconst ')')
	case ast.Token_INTERVAL:
		if constForm {
			// AexprConst deals with INTERVAL's postfix options itself.
			return nil
		}
		p.next()
		t := SystemTypeName("interval")
		t.Location = tok.Start
		if p.have(ast.Token('(')) {
			itok := p.peek()
			n := p.iconst()
			p.expect(ast.Token(')'))
			t.Typmods = []*ast.Node{
				makeIntConst(intervalFullRange, -1),
				makeIntConst(n, itok.Start),
			}
		} else {
			t.Typmods = p.parseOptInterval()
		}
		return t

	// JsonType
	case ast.Token_JSON:
		return sysType("json")
	}
	return nil
}

// parseCharacterTypename is gram.y's character/CharacterWithLength/
// CharacterWithoutLength.
func (p *parser) parseCharacterTypename(constForm bool) *ast.TypeName {
	tok := p.next()
	var base string
	switch tok.Kind {
	case ast.Token_VARCHAR:
		base = "varchar"
	case ast.Token_NATIONAL:
		// NATIONAL CHARACTER/CHAR_P opt_varying
		if p.kind() != ast.Token_CHARACTER && p.kind() != ast.Token_CHAR_P {
			p.syntaxErrorAt()
		}
		p.next()
		base = "bpchar"
	default: // CHARACTER, CHAR_P, NCHAR
		base = "bpchar"
	}
	if base == "bpchar" && p.have(ast.Token_VARYING) {
		base = "varchar"
	}
	var t *ast.TypeName
	if p.have(ast.Token('(')) {
		// CharacterWithLength
		itok := p.peek()
		n := p.iconst()
		p.expect(ast.Token(')'))
		t = SystemTypeName(base)
		t.Typmods = []*ast.Node{makeIntConst(n, itok.Start)}
	} else {
		// CharacterWithoutLength: char defaults to char(1)
		t = SystemTypeName(base)
		if base == "bpchar" && !constForm {
			t.Typmods = []*ast.Node{makeIntConst(1, -1)}
		}
	}
	t.Location = tok.Start
	return t
}

// parseOptTimezone is gram.y's opt_timezone.
func (p *parser) parseOptTimezone() bool {
	switch p.kind() {
	case ast.Token_WITH_LA:
		p.next()
		p.expect(ast.Token_TIME)
		p.expect(ast.Token_ZONE)
		return true
	case ast.Token_WITHOUT_LA:
		p.next()
		p.expect(ast.Token_TIME)
		p.expect(ast.Token_ZONE)
		return false
	}
	return false
}

// parseOptInterval is gram.y's opt_interval: the INTERVAL qualifier masks.
func (p *parser) parseOptInterval() []*ast.Node {
	tok := p.peek()
	one := func(mask int32) []*ast.Node {
		return []*ast.Node{makeIntConst(mask, tok.Start)}
	}
	switch tok.Kind {
	case ast.Token_YEAR_P:
		p.next()
		if p.have(ast.Token_TO) {
			p.expect(ast.Token_MONTH_P)
			return one(intervalMaskYear | intervalMaskMonth)
		}
		return one(intervalMaskYear)
	case ast.Token_MONTH_P:
		p.next()
		return one(intervalMaskMonth)
	case ast.Token_DAY_P:
		p.next()
		if p.have(ast.Token_TO) {
			switch p.kind() {
			case ast.Token_HOUR_P:
				p.next()
				return one(intervalMaskDay | intervalMaskHour)
			case ast.Token_MINUTE_P:
				p.next()
				return one(intervalMaskDay | intervalMaskHour | intervalMaskMinute)
			case ast.Token_SECOND_P:
				list := p.parseIntervalSecond()
				list[0] = makeIntConst(intervalMaskDay|intervalMaskHour|intervalMaskMinute|intervalMaskSecond, tok.Start)
				return list
			}
			p.syntaxErrorAt()
		}
		return one(intervalMaskDay)
	case ast.Token_HOUR_P:
		p.next()
		if p.have(ast.Token_TO) {
			switch p.kind() {
			case ast.Token_MINUTE_P:
				p.next()
				return one(intervalMaskHour | intervalMaskMinute)
			case ast.Token_SECOND_P:
				list := p.parseIntervalSecond()
				list[0] = makeIntConst(intervalMaskHour|intervalMaskMinute|intervalMaskSecond, tok.Start)
				return list
			}
			p.syntaxErrorAt()
		}
		return one(intervalMaskHour)
	case ast.Token_MINUTE_P:
		p.next()
		if p.have(ast.Token_TO) {
			if p.kind() != ast.Token_SECOND_P {
				p.syntaxErrorAt()
			}
			list := p.parseIntervalSecond()
			list[0] = makeIntConst(intervalMaskMinute|intervalMaskSecond, tok.Start)
			return list
		}
		return one(intervalMaskMinute)
	case ast.Token_SECOND_P:
		return p.parseIntervalSecond()
	}
	return nil
}

// parseIntervalSecond is gram.y's interval_second.
func (p *parser) parseIntervalSecond() []*ast.Node {
	tok := p.expect(ast.Token_SECOND_P)
	list := []*ast.Node{makeIntConst(intervalMaskSecond, tok.Start)}
	if p.have(ast.Token('(')) {
		itok := p.peek()
		n := p.iconst()
		p.expect(ast.Token(')'))
		list = append(list, makeIntConst(n, itok.Start))
	}
	return list
}

// parseConstTypenameExpr handles the AexprConst alternatives headed by a
// type keyword:
//
//	ConstTypename {Sconst,PARAM}
//	ConstInterval {Sconst opt_interval, '(' Iconst ')' Sconst, PARAM ...}
//
// It returns nil when the keyword cannot commit to a constant here (the
// caller falls back to columnref/function parsing).
func (p *parser) parseConstTypenameExpr() *ast.Node {
	tok := p.peek()
	switch tok.Kind {
	case ast.Token_INTERVAL:
		// Commits only when a Sconst/PARAM/'(' follows.
		switch p.kindN(1) {
		case ast.Token_SCONST:
			p.next()
			s := p.next()
			t := SystemTypeName("interval")
			t.Location = tok.Start
			t.Typmods = p.parseOptInterval()
			return makeStringConstCast(s.Str, s.Start, t)
		case ast.Token_PARAM:
			p.next()
			prm := p.next()
			t := SystemTypeName("interval")
			t.Location = tok.Start
			t.Typmods = p.parseOptInterval()
			return makeParamRefCast(prm.Ival, prm.Start, t)
		case ast.Token('('):
			p.next()
			p.next()
			itok := p.peek()
			n := p.iconst()
			p.expect(ast.Token(')'))
			t := SystemTypeName("interval")
			t.Location = tok.Start
			t.Typmods = []*ast.Node{
				makeIntConst(intervalFullRange, -1),
				makeIntConst(n, itok.Start),
			}
			switch p.kind() {
			case ast.Token_SCONST:
				s := p.next()
				return makeStringConstCast(s.Str, s.Start, t)
			case ast.Token_PARAM:
				prm := p.next()
				return makeParamRefCast(prm.Ival, prm.Start, t)
			}
			p.syntaxErrorAt()
		}
		return nil

	case ast.Token_INT_P, ast.Token_INTEGER, ast.Token_SMALLINT, ast.Token_BIGINT,
		ast.Token_REAL, ast.Token_BOOLEAN_P, ast.Token_JSON:
		// Bare type keywords: commit only on a following Sconst/PARAM.
		switch p.kindN(1) {
		case ast.Token_SCONST, ast.Token_PARAM:
			t := p.parseKeywordTypename(true)
			return p.finishConstCast(t)
		}
		return nil

	case ast.Token_DOUBLE_P:
		// DOUBLE PRECISION 'str'
		if p.kindN(1) == ast.Token_PRECISION {
			switch p.kindN(2) {
			case ast.Token_SCONST, ast.Token_PARAM:
				t := p.parseKeywordTypename(true)
				return p.finishConstCast(t)
			}
		}
		return nil

	case ast.Token_FLOAT_P, ast.Token_DECIMAL_P, ast.Token_DEC, ast.Token_NUMERIC,
		ast.Token_BIT, ast.Token_TIMESTAMP, ast.Token_TIME,
		ast.Token_CHARACTER, ast.Token_CHAR_P, ast.Token_VARCHAR, ast.Token_NCHAR:
		// These commit when followed by a modifier/decoration or a constant:
		// '(' begins typmods, VARYING/AS-IF decorations continue the type
		// syntax, and Sconst/PARAM ends it. A bare keyword followed by
		// anything else is a ColId.
		switch p.kindN(1) {
		case ast.Token_SCONST, ast.Token_PARAM, ast.Token('('):
			t := p.parseKeywordTypename(true)
			return p.finishConstCast(t)
		case ast.Token_VARYING:
			if tok.Kind == ast.Token_BIT || tok.Kind == ast.Token_CHARACTER ||
				tok.Kind == ast.Token_CHAR_P || tok.Kind == ast.Token_NCHAR {
				t := p.parseKeywordTypename(true)
				return p.finishConstCast(t)
			}
		case ast.Token_WITH_LA, ast.Token_WITHOUT_LA:
			if tok.Kind == ast.Token_TIMESTAMP || tok.Kind == ast.Token_TIME {
				t := p.parseKeywordTypename(true)
				return p.finishConstCast(t)
			}
		}
		return nil

	case ast.Token_NATIONAL:
		if p.kindN(1) == ast.Token_CHARACTER || p.kindN(1) == ast.Token_CHAR_P {
			t := p.parseKeywordTypename(true)
			return p.finishConstCast(t)
		}
		return nil
	}
	return nil
}

// finishConstCast expects the Sconst/PARAM ending a ConstTypename constant.
func (p *parser) finishConstCast(t *ast.TypeName) *ast.Node {
	switch p.kind() {
	case ast.Token_SCONST:
		s := p.next()
		return makeStringConstCast(s.Str, s.Start, t)
	case ast.Token_PARAM:
		prm := p.next()
		return makeParamRefCast(prm.Ival, prm.Start, t)
	}
	p.syntaxErrorAt()
	return nil
}
