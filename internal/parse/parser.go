// Package parse is the hand-written recursive-descent port of the pinned
// grammar (internal/reference/gram.y, libpg_query 17-6.2.2). One parse*
// method per production, each with an attribution comment naming the gram.y
// rule it implements; the support functions at the bottom of gram.y live in
// gram_support.go with the same names.
package parse

import (
	"github.com/sqlc-dev/oliphant/ast"
	"github.com/sqlc-dev/oliphant/internal/lexer"
)

// pgVersionNum is the pinned PG_VERSION_NUM (PostgreSQL 18.4).
const pgVersionNum = 180004

// parser holds one parse's state: the filtered token stream (base_yylex
// semantics) buffered as the grammar pulls it, so lookahead never runs
// further ahead of the parse than bison's one (plus the filter's own) token.
type parser struct {
	src    string
	filter *lexer.Filter
	toks   []lexer.Token
	pos    int

	// emptyStrings records nodes whose string field was present in the
	// source but empty (COMMENT ... IS ” / SECURITY LABEL ... IS ”).
	// proto3 cannot represent C's NULL-vs-"" distinction, but the C
	// fingerprint sees the original tree and emits the field name for a
	// non-NULL empty string; the fingerprint walk consults this set.
	emptyStrings map[any]bool

	// inSubstrList marks that we are parsing substr_list, where SIMILAR
	// appears without TO (see parseAExprInfix).
	inSubstrList bool
}

// bail carries a parse error up to Parse via panic/recover: the reference
// aborts at the first error (bison + ereport longjmp), and threading error
// returns through several hundred productions would bury the grammar.
type bail struct{ err *lexer.Error }

// tokenCap sizes the token buffer up front: SQL averages a handful of bytes
// per token, and the dense extreme (large VALUES parameter lists, the corpus
// stress case) runs ~3 bytes/token, so len/3 keeps even pathological inputs
// from regrowing the buffer — append-regrowth otherwise dominates
// alloc_space on large statements — while short queries over-allocate at
// most a few hundred transient bytes.
func tokenCap(input string) int {
	return len(input)/3 + 8
}

// Parse is raw_parser for RAW_PARSE_DEFAULT: parse_toplevel/stmtmulti.
func Parse(input string) (res *ast.ParseResult, err *lexer.Error) {
	res, _, err = ParseTracked(input)
	return res, err
}

// ParseTracked is Parse plus the present-but-empty string set (see
// parser.emptyStrings) for the fingerprint walk.
func ParseTracked(input string) (res *ast.ParseResult, emptyStrings map[any]bool, err *lexer.Error) {
	s := lexer.New(input)
	p := &parser{src: s.Input(), filter: lexer.NewFilter(s), toks: make([]lexer.Token, 0, tokenCap(input))}
	defer func() {
		if r := recover(); r != nil {
			if b, ok := r.(bail); ok {
				res, emptyStrings, err = nil, nil, b.err
				return
			}
			panic(r)
		}
	}()
	return p.parseToplevel(), p.emptyStrings, nil
}

// markEmptyString records a node whose string field was written as ” in
// the source (non-NULL in the C tree).
func (p *parser) markEmptyString(node any) {
	if p.emptyStrings == nil {
		p.emptyStrings = map[any]bool{}
	}
	p.emptyStrings[node] = true
}

// fail aborts the parse with the given error.
func (p *parser) fail(err *lexer.Error) {
	panic(bail{err})
}

// syntaxError aborts with bison's yyerror report at the given token.
func (p *parser) syntaxError(tok lexer.Token) {
	p.fail(p.filter.SyntaxError(tok))
}

// syntaxErrorAt reports "syntax error" at the current lookahead token — the
// standard bison failure.
func (p *parser) syntaxErrorAt() {
	p.syntaxError(p.peek())
}

// ereport aborts with a gram.y action error (no "at or near" decoration).
// loc == -1 omits the cursor position, like parser_errposition(-1).
func (p *parser) ereport(funcname, message string, loc int32) {
	p.fail(p.filter.ParserError(funcname, message, int(loc)))
}

// fill materializes buffered tokens up to and including index i. A scanner
// error surfaces immediately when its token is materialized, matching the
// C behavior where the core_yylex call throws.
func (p *parser) fill(i int) {
	for len(p.toks) <= i {
		tok, err := p.filter.Next()
		if err != nil {
			p.fail(err)
		}
		p.toks = append(p.toks, tok)
		if tok.Kind == 0 {
			// EOF repeats forever if peeked past.
			return
		}
	}
}

// peek returns the current lookahead token without consuming it.
func (p *parser) peek() lexer.Token {
	return p.peekN(0)
}

// peekN returns the lookahead token n positions ahead (0 = current).
func (p *parser) peekN(n int) lexer.Token {
	p.fill(p.pos + n)
	if p.pos+n >= len(p.toks) {
		return p.toks[len(p.toks)-1] // EOF token
	}
	return p.toks[p.pos+n]
}

// next consumes and returns the current token.
func (p *parser) next() lexer.Token {
	tok := p.peek()
	if tok.Kind != 0 {
		p.pos++
	}
	return tok
}

// have consumes the current token if it has the given kind.
func (p *parser) have(kind ast.Token) bool {
	if p.peek().Kind == kind {
		p.pos++
		return true
	}
	return false
}

// expect consumes a token of the given kind or reports a syntax error.
func (p *parser) expect(kind ast.Token) lexer.Token {
	tok := p.peek()
	if tok.Kind != kind {
		p.syntaxError(tok)
	}
	p.pos++
	return tok
}

// mark/reset implement bounded backtracking for the few places where the
// LALR tables carry alternatives further than one token of lookahead can
// distinguish (e.g. ConstTypename vs ColId for type keywords).
func (p *parser) mark() int             { return p.pos }
func (p *parser) reset(mark int)        { p.pos = mark }
func (p *parser) loc() int32            { return p.peek().Start }
func (p *parser) kind() ast.Token       { return p.peek().Kind }
func (p *parser) kindN(n int) ast.Token { return p.peekN(n).Kind }

// parseToplevel is gram.y's parse_toplevel/stmtmulti for the default parse
// mode: a semicolon-separated statement list, each wrapped in a RawStmt whose
// location bookkeeping is driven entirely by semicolon positions.
func (p *parser) parseToplevel() *ast.ParseResult {
	res := &ast.ParseResult{Version: pgVersionNum}

	// gram.y: stmtmulti
	var last *ast.RawStmt
	for {
		// makeRawStmt($3, @3): the statement's location is its own first
		// token's (PG 18; previously the position after the prior ';').
		stmtStart := p.loc()
		if stmt := p.parseToplevelStmt(); stmt != nil {
			last = &ast.RawStmt{Stmt: stmt, StmtLocation: stmtStart}
			res.Stmts = append(res.Stmts, last)
		}
		tok := p.peek()
		if tok.Kind == 0 {
			break
		}
		if tok.Kind != ast.Token(';') {
			p.syntaxError(tok)
		}
		p.next()
		// gram.y: stmtmulti — updateRawStmtEnd(llast, @2); the nil-ing of
		// last reproduces the stmt_len>0 guard ("select foo ;; select bar").
		if last != nil {
			last.StmtLen = tok.Start - last.StmtLocation
			last = nil
		}
	}
	return res
}

// parseToplevelStmt is toplevel_stmt/stmt: dispatch on the leading token.
// Milestones 4-5 implement the SELECT family and DML; everything else
// reports the syntax error bison would only reach much later, and stays on
// the todo list until its milestone lands.
func (p *parser) parseToplevelStmt() *ast.Node {
	tok := p.peek()
	switch tok.Kind {
	case ast.Token(';'), 0:
		// Empty statement: stmt: /*EMPTY*/
		return nil
	case ast.Token_SELECT, ast.Token_TABLE, ast.Token_VALUES:
		// gram.y: SelectStmt: select_no_parens %prec UMINUS
		return p.parseSelectStmt()
	case ast.Token('('):
		// gram.y: SelectStmt: select_with_parens %prec UMINUS
		return p.parseSelectStmt()
	case ast.Token_WITH, ast.Token_WITH_LA:
		// opt_with_clause statements: SELECT/INSERT/UPDATE/DELETE/MERGE
		return p.parseWithPrefixedStmt()
	case ast.Token_INSERT:
		return p.parseInsertStmt(nil)
	case ast.Token_UPDATE:
		return p.parseUpdateStmt(nil)
	case ast.Token_DELETE_P:
		return p.parseDeleteStmt(nil)
	case ast.Token_MERGE:
		return p.parseMergeStmt(nil)
	case ast.Token_COPY:
		return p.parseCopyStmt()
	case ast.Token_PREPARE:
		// PREPARE TRANSACTION Sconst is a TransactionStmt; PREPARE name
		// [(types)] AS is a PrepareStmt ("transaction" can also be a plan
		// name, so the Sconst decides).
		if p.kindN(1) == ast.Token_TRANSACTION && p.kindN(2) == ast.Token_SCONST {
			p.next()
			return p.parsePrepareTransactionStmt()
		}
		return p.parsePrepareStmt()
	case ast.Token_ABORT_P, ast.Token_START, ast.Token_COMMIT,
		ast.Token_ROLLBACK, ast.Token_SAVEPOINT, ast.Token_RELEASE,
		ast.Token_BEGIN_P, ast.Token_END_P:
		return p.parseTransactionStmt()
	case ast.Token_NOTIFY:
		return p.parseNotifyStmt()
	case ast.Token_LISTEN:
		return p.parseListenStmt()
	case ast.Token_UNLISTEN:
		return p.parseUnlistenStmt()
	case ast.Token_LOAD:
		return p.parseLoadStmt()
	case ast.Token_LOCK_P:
		return p.parseLockStmt()
	case ast.Token_TRUNCATE:
		return p.parseTruncateStmt()
	case ast.Token_EXECUTE:
		return p.parseExecuteStmt()
	case ast.Token_DEALLOCATE:
		return p.parseDeallocateStmt()
	case ast.Token_DECLARE:
		return p.parseDeclareCursorStmt()
	case ast.Token_FETCH:
		return p.parseFetchStmt(false)
	case ast.Token_MOVE:
		return p.parseFetchStmt(true)
	case ast.Token_CLOSE:
		return p.parseClosePortalStmt()
	case ast.Token_CALL:
		return p.parseCallStmt()
	case ast.Token_CREATE:
		return p.parseCreateDispatch()
	case ast.Token_ALTER:
		return p.parseAlterDispatch()
	case ast.Token_DROP:
		return p.parseDropDispatch()
	case ast.Token_SET:
		// SET CONSTRAINTS is ConstraintsSetStmt unless "constraints" is
		// being used as a variable name (generic_set's var_name).
		if p.kindN(1) == ast.Token_CONSTRAINTS && !p.varNameContinues(2) {
			p.next()
			return p.parseConstraintsSetStmt()
		}
		return p.parseVariableSetStmt()
	case ast.Token_RESET:
		return p.parseVariableResetStmt()
	case ast.Token_SHOW:
		return p.parseVariableShowStmt()
	case ast.Token_CHECKPOINT:
		return p.parseCheckPointStmt()
	case ast.Token_DISCARD:
		return p.parseDiscardStmt()
	case ast.Token_REFRESH:
		return p.parseRefreshMatViewStmt()
	case ast.Token_COMMENT:
		return p.parseCommentStmt()
	case ast.Token_CLUSTER:
		return p.parseClusterStmt()
	case ast.Token_VACUUM:
		return p.parseVacuumStmt()
	case ast.Token_ANALYZE, ast.Token_ANALYSE:
		return p.parseAnalyzeStmt()
	case ast.Token_EXPLAIN:
		return p.parseExplainStmt()
	case ast.Token_REINDEX:
		return p.parseReindexStmt()
	case ast.Token_DO:
		return p.parseDoStmt()
	case ast.Token_GRANT:
		return p.parseGrantStmt()
	case ast.Token_REVOKE:
		return p.parseRevokeStmt()
	case ast.Token_IMPORT_P:
		return p.parseImportForeignSchemaStmt()
	case ast.Token_SECURITY:
		if p.kindN(1) == ast.Token_LABEL {
			p.next()
			p.next()
			return p.parseSecLabelStmt()
		}
	case ast.Token_REASSIGN:
		// gram.y: ReassignOwnedStmt
		p.next()
		p.expect(ast.Token_OWNED)
		p.expect(ast.Token_BY)
		roles := p.parseRoleList()
		p.expect(ast.Token_TO)
		return &ast.Node{Node: &ast.Node_ReassignOwnedStmt{ReassignOwnedStmt: &ast.ReassignOwnedStmt{
			Roles:   roles,
			Newrole: p.parseRoleSpec(),
		}}}
	}
	p.syntaxError(tok)
	return nil
}
