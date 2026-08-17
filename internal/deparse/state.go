// Package deparse ports libpg_query's postgres_deparse.c at the pinned
// 17-6.2.2, targeting the protobuf structs directly.
//
// This file is the deparser state machinery: nesting levels, part groups and
// parts (postgres_deparse.c's DeparseState). The machinery exists for the
// optional pretty-print mode, which pg_query_go's API never enables, but it
// still shapes the plain output — parts are joined with single spaces, with
// no space after "(" or before a part starting with ")" or ";", and part
// groups fold their major keyword into their first part — so it is ported
// exactly rather than approximated. Comment placement is dropped entirely:
// comments can only arrive through deparse options the Go API does not
// expose.
package deparse

import (
	"fmt"
	"strings"

	"github.com/sqlc-dev/oliphant/ast"
	"github.com/sqlc-dev/oliphant/internal/lexer"
)

// Zero-value PostgresDeparseOpts, after deparseRawStmtOpts applies defaults.
const (
	optIndentSize    = 4
	optMaxLineLength = 80
)

// nodeContext is DeparseNodeContext.
type nodeContext int

const (
	contextNone nodeContext = iota
	// Parent node type (and sometimes field)
	contextInsertRelation
	contextInsertSelect
	contextAExpr
	contextCreateType
	contextAlterType
	contextSetStatement
	contextFuncExpr
	contextSelectSetop
	contextSelectSortClause
	// Identifier vs constant context
	contextIdentifier
	contextConstant
)

// partIndentMode is DeparsePartIndentMode.
type partIndentMode int

const (
	partNoIndent partIndentMode = iota
	partIndent
	partIndentAndMerge
)

// part is DeparseStatePart.
type part struct {
	str       []byte
	indent    int
	mergeable bool
}

func (p *part) removeTrailingSpace() {
	if len(p.str) >= 1 && p.str[len(p.str)-1] == ' ' {
		p.str = p.str[:len(p.str)-1]
	}
}

// partGroup is DeparseStatePartGroup.
type partGroup struct {
	keyword    string
	hasKeyword bool
	parts      []*part
	indentMode partIndentMode
}

// nestingLevel is DeparseStateNestingLevel.
type nestingLevel struct {
	partGroups []*partGroup
	baseIndent int
}

// state is DeparseState.
type state struct {
	current     *nestingLevel
	resultParts []*part
}

// deparseError carries an elog/ereport message out of the recursion; the
// public entry point recovers it (the C code longjmps via PG_TRY).
type deparseError struct {
	message string
}

func (st *state) errorf(format string, args ...any) {
	panic(deparseError{message: fmt.Sprintf(format, args...)})
}

func makePart(st *state, level *nestingLevel, indentMode partIndentMode) *part {
	p := &part{indent: level.baseIndent}
	if indentMode != partNoIndent {
		p.indent += optIndentSize
	}
	p.mergeable = indentMode == partIndentAndMerge
	return p
}

// currentPartGroup is deparseGetCurrentPartGroup.
func (st *state) currentPartGroup() *partGroup {
	level := st.current
	if len(level.partGroups) > 0 {
		return level.partGroups[len(level.partGroups)-1]
	}
	pg := &partGroup{}
	pg.parts = append(pg.parts, makePart(st, level, pg.indentMode))
	level.partGroups = append(level.partGroups, pg)
	return pg
}

// currentPart is deparseGetCurrentPart.
func (st *state) currentPart() *part {
	pg := st.currentPartGroup()
	return pg.parts[len(pg.parts)-1]
}

// markCurrentPartNonMergeable is deparseMarkCurrentPartNonMergable.
func (st *state) markCurrentPartNonMergeable() {
	st.currentPart().mergeable = false
}

// appendf is deparseAppendStringInfo.
func (st *state) appendf(format string, args ...any) {
	p := st.currentPart()
	p.str = append(p.str, fmt.Sprintf(format, args...)...)
}

// appendString is deparseAppendStringInfoString.
func (st *state) appendString(s string) {
	p := st.currentPart()
	p.str = append(p.str, s...)
}

// appendChar is deparseAppendStringInfoChar.
func (st *state) appendChar(ch byte) {
	p := st.currentPart()
	p.str = append(p.str, ch)
}

// removeTrailingSpace is removeTrailingSpace.
func (st *state) removeTrailingSpace() {
	st.currentPart().removeTrailingSpace()
}

// removeTrailingEmptyPart is deparseRemoveTrailingEmptyPart.
func (st *state) removeTrailingEmptyPart() {
	pg := st.currentPartGroup()
	last := st.currentPart()
	if len(last.str) == 0 {
		pg.parts = pg.parts[:len(pg.parts)-1]
	}
}

// appendPart is deparseAppendPart.
func (st *state) appendPart(deduplicate bool) {
	pg := st.currentPartGroup()
	// Remove previous part if it's empty and we deduplicate. We don't keep
	// the existing part since it may have the wrong indent level or mode.
	if deduplicate {
		st.removeTrailingEmptyPart()
	}
	pg.parts = append(pg.parts, makePart(st, st.current, pg.indentMode))
}

// appendCommaAndPart is deparseAppendCommaAndPart (commas_start_of_line is
// always false).
func (st *state) appendCommaAndPart() {
	st.appendChar(',')
	st.appendPart(true)
}

// appendPartGroup is deparseAppendPartGroup.
func (st *state) appendPartGroup(keyword string, indentMode partIndentMode) {
	level := st.current
	if len(level.partGroups) > 0 {
		st.removeTrailingSpace()
	}
	pg := &partGroup{keyword: keyword, hasKeyword: true, indentMode: indentMode}
	pg.parts = append(pg.parts, makePart(st, level, indentMode))
	level.partGroups = append(level.partGroups, pg)
}

// increaseNestingLevel is deparseStateIncreaseNestingLevel; it returns the
// parent to be passed back to decreaseNestingLevel.
func (st *state) increaseNestingLevel() *nestingLevel {
	parent := st.current
	level := &nestingLevel{}
	if parent != nil {
		pg := st.currentPartGroup()
		level.baseIndent = parent.baseIndent + optIndentSize
		if pg.indentMode != partNoIndent { // Indent again if parts next to us are also indented
			level.baseIndent += optIndentSize
		}
		// Parts with nested elements don't get merged, even if otherwise permitted
		st.markCurrentPartNonMergeable()
	}
	st.current = level
	return parent
}

// decreaseNestingLevel is deparseStateDecreaseNestingLevel.
func (st *state) decreaseNestingLevel(parentLevel *nestingLevel) {
	level := st.current

	for _, pg := range level.partGroups {
		// Merge parts
		if pg.indentMode == partIndentAndMerge && len(pg.parts) > 1 {
			merged := pg.parts[:1]
			target := pg.parts[0]
			for _, p := range pg.parts[1:] {
				target.removeTrailingSpace()
				if target.mergeable && p.mergeable &&
					target.indent+len(target.str)+1+len(p.str) <= optMaxLineLength {
					if len(target.str) > 0 && target.str[len(target.str)-1] != '(' {
						target.str = append(target.str, ' ')
					}
					target.str = append(target.str, p.str...)
				} else {
					merged = append(merged, p)
					target = p
				}
			}
			pg.parts = merged
		}

		if pg.hasKeyword {
			target := makePart(st, level, partNoIndent)
			target.str = append(target.str, pg.keyword...)
			pg.parts = append([]*part{target}, pg.parts...)

			if len(pg.parts) == 2 || pg.indentMode == partNoIndent {
				p := pg.parts[1]
				if len(p.str) > 0 {
					target.str = append(target.str, ' ')
					target.str = append(target.str, p.str...)
				}
				pg.parts = append(pg.parts[:1], pg.parts[2:]...)
			}
		}

		if parentLevel != nil {
			parentPG := parentLevel.partGroups[len(parentLevel.partGroups)-1]
			parentPG.parts = append(parentPG.parts, pg.parts...)
		} else {
			// If it's the top level, save as results instead
			st.resultParts = append(st.resultParts, pg.parts...)
		}
	}

	st.current = parentLevel

	if parentLevel != nil {
		// Make sure parent statement writes that follow are on their own line
		st.appendPart(true)
		// Parts with nested elements don't get merged, even if otherwise permitted
		st.markCurrentPartNonMergeable()
	}
}

// emit is deparseEmit (pretty_print false), appending into the shared output
// buffer.
func (st *state) emit(out []byte) []byte {
	// If the last part is empty, drop it
	if n := len(st.resultParts); n > 0 && len(st.resultParts[n-1].str) == 0 {
		st.resultParts = st.resultParts[:n-1]
	}

	for i, p := range st.resultParts {
		lastPart := i == len(st.resultParts)-1

		if len(p.str) > 0 && (p.str[0] == ')' || p.str[0] == ';') {
			out = trimTrailingSpace(out)
		}

		out = append(out, p.str...)
		out = trimTrailingSpace(out)

		if !lastPart && (len(out) == 0 || out[len(out)-1] != '(') {
			out = append(out, ' ')
		}
	}
	st.resultParts = nil
	return out
}

func trimTrailingSpace(b []byte) []byte {
	if len(b) >= 1 && b[len(b)-1] == ' ' {
		return b[:len(b)-1]
	}
	return b
}

// deparseRawStmt is deparseRawStmtOpts with default options.
func deparseRawStmt(out []byte, raw *ast.RawStmt) []byte {
	if raw.Stmt == nil || raw.Stmt.Node == nil {
		panic(deparseError{message: "deparse error in deparseRawStmt: RawStmt with empty Stmt"})
	}
	st := &state{}
	deparseStmt(st, raw.Stmt)
	return st.emit(out)
}

// Deparse is pg_query_deparse_protobuf: statements joined with "; ".
func Deparse(tree *ast.ParseResult) (result string, err error) {
	defer func() {
		if r := recover(); r != nil {
			if de, ok := r.(deparseError); ok {
				err = fmt.Errorf("%s", de.message)
				return
			}
			panic(r)
		}
	}()
	var out []byte
	for i, raw := range tree.Stmts {
		out = deparseRawStmt(out, raw)
		if i < len(tree.Stmts)-1 {
			out = append(out, "; "...)
		}
	}
	return string(out), nil
}

// QuoteIdentifier exposes quoteIdentifier to the other internal packages
// that need ruleutils.c's quote_identifier (the summary walk renders table
// names with it).
func QuoteIdentifier(ident string) string { return quoteIdentifier(ident) }

// quoteIdentifier is ruleutils.c's quote_identifier (quote_all_identifiers
// is always false in libpg_query).
func quoteIdentifier(ident string) string {
	nquotes := 0
	safe := len(ident) > 0 && ((ident[0] >= 'a' && ident[0] <= 'z') || ident[0] == '_')
	for i := 0; i < len(ident); i++ {
		ch := ident[i]
		if (ch >= 'a' && ch <= 'z') || (ch >= '0' && ch <= '9') || ch == '_' {
			// okay
		} else {
			safe = false
			if ch == '"' {
				nquotes++
			}
		}
	}

	if safe {
		// Quote keywords except for unreserved ones.
		if kw, ok := lexer.Keywords[ident]; ok && kw.Category != lexer.Unreserved {
			safe = false
		}
	}
	if safe {
		return ident
	}

	var b strings.Builder
	b.Grow(len(ident) + nquotes + 2)
	b.WriteByte('"')
	for i := 0; i < len(ident); i++ {
		if ident[i] == '"' {
			b.WriteByte('"')
		}
		b.WriteByte(ident[i])
	}
	b.WriteByte('"')
	return b.String()
}

// isReservedKeyword: reserved keyword in all-lowercase spelling (the parser
// lowercases keywords, so mixed case never matches).
func isReservedKeyword(val string) bool {
	for i := 0; i < len(val); i++ {
		ch := val[i]
		if !((ch >= 'a' && ch <= 'z') || (ch >= '0' && ch <= '9') || ch == '_') {
			return false
		}
	}
	kw, ok := lexer.Keywords[strings.ToLower(val)]
	return ok && kw.Category == lexer.Reserved
}

// isOp: value consists only of operator characters.
func isOp(val string) bool {
	if len(val) == 0 {
		return false
	}
	for i := 0; i < len(val); i++ {
		switch val[i] {
		case '~', '!', '@', '#', '^', '&', '|', '`', '?', '+', '-', '*', '/', '%', '<', '>', '=':
		default:
			return false
		}
	}
	return true
}

// deparseStringLiteral appends a SQL string literal (postgres_fdw's
// deparseStringLiteral: E” syntax whenever a backslash is present).
func deparseStringLiteral(st *state, val string) {
	if strings.ContainsRune(val, '\\') {
		st.appendChar('E')
	}
	st.appendChar('\'')
	for i := 0; i < len(val); i++ {
		ch := val[i]
		if ch == '\'' || ch == '\\' {
			st.appendChar(ch)
		}
		st.appendChar(ch)
	}
	st.appendChar('\'')
}

// Interval typmod masks (datetime.h): INTERVAL_MASK(x) is 1 << x with the
// datetime token unit values.
const (
	intervalMaskYear      = 1 << 2  // INTERVAL_MASK(YEAR)
	intervalMaskMonth     = 1 << 1  // INTERVAL_MASK(MONTH)
	intervalMaskDay       = 1 << 3  // INTERVAL_MASK(DAY)
	intervalMaskHour      = 1 << 10 // INTERVAL_MASK(HOUR)
	intervalMaskMinute    = 1 << 11 // INTERVAL_MASK(MINUTE)
	intervalMaskSecond    = 1 << 12 // INTERVAL_MASK(SECOND)
	intervalFullRange     = 0x7FFF  // INTERVAL_FULL_RANGE
	intervalFullPrecision = 0xFFFF  // INTERVAL_FULL_PRECISION
)

// Value-node helpers (value.h accessors).
func strVal(n *ast.Node) string   { return n.GetString_().GetSval() }
func intVal(n *ast.Node) int      { return int(n.GetInteger().GetIval()) }
func floatVal(n *ast.Node) string { return n.GetFloat().GetFval() }
func boolVal(n *ast.Node) bool    { return n.GetBoolean().GetBoolval() }

// quoteQualifiedIdentifier is ruleutils.c's quote_qualified_identifier;
// qualifier "" stands for NULL.
func quoteQualifiedIdentifier(qualifier, ident string) string {
	if qualifier != "" {
		return quoteIdentifier(qualifier) + "." + quoteIdentifier(ident)
	}
	return quoteIdentifier(ident)
}

// nodeTag names a Node's type for error messages (C prints the numeric tag;
// deparse errors are not part of golden conformance).
func nodeTag(n *ast.Node) string {
	if n == nil || n.Node == nil {
		return "NULL"
	}
	m := n.ProtoReflect()
	fd := m.WhichOneof(m.Descriptor().Oneofs().ByName("node"))
	if fd == nil {
		return "NULL"
	}
	return string(m.Get(fd).Message().Descriptor().Name())
}

// aConstVal converts an A_Const's value union to the value Node
// deparseValue takes (&a_const->val, or NULL when isnull).
func aConstVal(a *ast.A_Const) *ast.Node {
	if a == nil || a.Isnull {
		return nil
	}
	switch v := a.Val.(type) {
	case *ast.A_Const_Ival:
		return &ast.Node{Node: &ast.Node_Integer{Integer: v.Ival}}
	case *ast.A_Const_Fval:
		return &ast.Node{Node: &ast.Node_Float{Float: v.Fval}}
	case *ast.A_Const_Boolval:
		return &ast.Node{Node: &ast.Node_Boolean{Boolean: v.Boolval}}
	case *ast.A_Const_Sval:
		return &ast.Node{Node: &ast.Node_String_{String_: v.Sval}}
	case *ast.A_Const_Bsval:
		return &ast.Node{Node: &ast.Node_BitString{BitString: v.Bsval}}
	}
	return nil
}
