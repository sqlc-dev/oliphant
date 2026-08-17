package parse

// CreateTrigStmt (CREATE [CONSTRAINT] TRIGGER) and CreateEventTrigStmt.

import (
	"strconv"

	"github.com/sqlc-dev/oliphant/ast"
)

// Trigger type bits from trigger.h.
const (
	triggerTypeRow      = 1 << 0
	triggerTypeBefore   = 1 << 1
	triggerTypeInsert   = 1 << 2
	triggerTypeDelete   = 1 << 3
	triggerTypeUpdate   = 1 << 4
	triggerTypeTruncate = 1 << 5
	triggerTypeInstead  = 1 << 6
	triggerTypeAfter    = 0
)

// parseCreateTrigStmt is gram.y's CreateTrigStmt. CREATE [OR REPLACE]
// [CONSTRAINT] is consumed; TRIGGER is the lookahead.
func (p *parser) parseCreateTrigStmt(replace, isConstraint bool) *ast.Node {
	p.expect(ast.Token_TRIGGER)
	n := &ast.CreateTrigStmt{Replace: replace, Isconstraint: isConstraint}
	n.Trigname = p.name()

	if isConstraint {
		if replace {
			// gram.y: not supported, see CreateTrigger (no error position).
			p.ereport("base_yyparse", "CREATE OR REPLACE CONSTRAINT TRIGGER is not supported", -1)
		}
		p.expect(ast.Token_AFTER)
		n.Timing = triggerTypeAfter
		n.Events, n.Columns = p.parseTriggerEvents()
		p.expect(ast.Token_ON)
		n.Relation = p.parseQualifiedName()
		if p.have(ast.Token_FROM) {
			n.Constrrel = p.parseQualifiedName()
		}
		cas, casLoc := p.parseConstraintAttributeSpec()
		p.processCASbits(cas, casLoc, "TRIGGER", &n.Deferrable, &n.Initdeferred, nil, nil, nil)
		p.expect(ast.Token_FOR)
		p.have(ast.Token_EACH)
		p.expect(ast.Token_ROW)
		n.Row = true
		if p.have(ast.Token_WHEN) {
			p.expect(ast.Token('('))
			n.WhenClause = p.parseAExpr(0)
			p.expect(ast.Token(')'))
		}
	} else {
		// TriggerActionTime
		switch {
		case p.have(ast.Token_BEFORE):
			n.Timing = triggerTypeBefore
		case p.have(ast.Token_AFTER):
			n.Timing = triggerTypeAfter
		case p.have(ast.Token_INSTEAD):
			p.expect(ast.Token_OF)
			n.Timing = triggerTypeInstead
		default:
			p.syntaxErrorAt()
		}
		n.Events, n.Columns = p.parseTriggerEvents()
		p.expect(ast.Token_ON)
		n.Relation = p.parseQualifiedName()
		// TriggerReferencing
		if p.have(ast.Token_REFERENCING) {
			for {
				t := &ast.TriggerTransition{}
				switch {
				case p.have(ast.Token_NEW):
					t.IsNew = true
				case p.have(ast.Token_OLD):
				default:
					p.syntaxErrorAt()
				}
				switch {
				case p.have(ast.Token_TABLE):
					t.IsTable = true
				case p.have(ast.Token_ROW):
				default:
					p.syntaxErrorAt()
				}
				p.have(ast.Token_AS)
				t.Name = p.colId()
				n.TransitionRels = append(n.TransitionRels,
					&ast.Node{Node: &ast.Node_TriggerTransition{TriggerTransition: t}})
				if p.kind() != ast.Token_NEW && p.kind() != ast.Token_OLD {
					break
				}
			}
		}
		// TriggerForSpec
		if p.have(ast.Token_FOR) {
			p.have(ast.Token_EACH)
			switch {
			case p.have(ast.Token_ROW):
				n.Row = true
			case p.have(ast.Token_STATEMENT):
			default:
				p.syntaxErrorAt()
			}
		}
		if p.have(ast.Token_WHEN) {
			p.expect(ast.Token('('))
			n.WhenClause = p.parseAExpr(0)
			p.expect(ast.Token(')'))
		}
	}

	p.expect(ast.Token_EXECUTE)
	p.parseFunctionOrProcedure()
	n.Funcname = p.parseFuncName(p.peek())
	p.expect(ast.Token('('))
	// TriggerFuncArgs
	if p.kind() != ast.Token(')') {
		n.Args = append(n.Args, p.parseTriggerFuncArg())
		for p.have(ast.Token(',')) {
			n.Args = append(n.Args, p.parseTriggerFuncArg())
		}
	}
	p.expect(ast.Token(')'))
	return &ast.Node{Node: &ast.Node_CreateTrigStmt{CreateTrigStmt: n}}
}

// parseFunctionOrProcedure is gram.y's FUNCTION_or_PROCEDURE.
func (p *parser) parseFunctionOrProcedure() {
	if !p.have(ast.Token_FUNCTION) && !p.have(ast.Token_PROCEDURE) {
		p.syntaxErrorAt()
	}
}

// parseTriggerEvents is gram.y's TriggerEvents/TriggerOneEvent.
func (p *parser) parseTriggerEvents() (int32, []*ast.Node) {
	events, columns := p.parseTriggerOneEvent()
	for p.have(ast.Token_OR) {
		ev2, cols2 := p.parseTriggerOneEvent()
		if events&ev2 != 0 {
			p.parserYyerror("duplicate trigger events specified")
		}
		events |= ev2
		columns = append(columns, cols2...)
	}
	return events, columns
}

// parseTriggerOneEvent is gram.y's TriggerOneEvent.
func (p *parser) parseTriggerOneEvent() (int32, []*ast.Node) {
	switch {
	case p.have(ast.Token_INSERT):
		return triggerTypeInsert, nil
	case p.have(ast.Token_DELETE_P):
		return triggerTypeDelete, nil
	case p.have(ast.Token_UPDATE):
		if p.have(ast.Token_OF) {
			return triggerTypeUpdate, p.columnList()
		}
		return triggerTypeUpdate, nil
	case p.have(ast.Token_TRUNCATE):
		return triggerTypeTruncate, nil
	}
	p.syntaxErrorAt()
	return 0, nil
}

// parseTriggerFuncArg is gram.y's TriggerFuncArg.
func (p *parser) parseTriggerFuncArg() *ast.Node {
	tok := p.peek()
	switch tok.Kind {
	case ast.Token_ICONST:
		return nStr(strconv.Itoa(int(p.iconst())))
	case ast.Token_FCONST, ast.Token_SCONST:
		p.next()
		return nStr(tok.Str)
	}
	return nStr(p.colLabel())
}

// parseCreateEventTrigStmt is gram.y's CreateEventTrigStmt; CREATE EVENT
// TRIGGER consumed.
func (p *parser) parseCreateEventTrigStmt() *ast.Node {
	n := &ast.CreateEventTrigStmt{Trigname: p.name()}
	p.expect(ast.Token_ON)
	n.Eventname = p.colLabel()
	if p.have(ast.Token_WHEN) {
		// event_trigger_when_list
		for {
			etok := p.peek()
			name := p.colId()
			p.expect(ast.Token_IN_P)
			p.expect(ast.Token('('))
			var vals []*ast.Node
			vals = append(vals, nStr(p.sconst()))
			for p.have(ast.Token(',')) {
				vals = append(vals, nStr(p.sconst()))
			}
			p.expect(ast.Token(')'))
			n.Whenclause = append(n.Whenclause, makeDefElem(name, nList(vals), etok.Start))
			if !p.have(ast.Token_AND) {
				break
			}
		}
	}
	p.expect(ast.Token_EXECUTE)
	p.parseFunctionOrProcedure()
	n.Funcname = p.parseFuncName(p.peek())
	p.expect(ast.Token('('))
	p.expect(ast.Token(')'))
	return &ast.Node{Node: &ast.Node_CreateEventTrigStmt{CreateEventTrigStmt: n}}
}
