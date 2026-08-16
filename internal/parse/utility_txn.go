package parse

// TransactionStmt (+ the legacy BEGIN/END forms), NOTIFY/LISTEN/UNLISTEN,
// LOAD, LOCK, and TRUNCATE.

import (
	"github.com/sqlc-dev/oliphant/ast"
)

// parseOptTransaction is gram.y's opt_transaction: WORK | TRANSACTION | ε.
func (p *parser) parseOptTransaction() {
	if p.kind() == ast.Token_WORK || p.kind() == ast.Token_TRANSACTION {
		p.next()
	}
}

// parseOptTransactionChain is gram.y's opt_transaction_chain.
func (p *parser) parseOptTransactionChain() bool {
	if p.have(ast.Token_AND) {
		if p.have(ast.Token_NO) {
			p.expect(ast.Token_CHAIN)
			return false
		}
		p.expect(ast.Token_CHAIN)
		return true
	}
	return false
}

func nTransactionStmt(n *ast.TransactionStmt) *ast.Node {
	return &ast.Node{Node: &ast.Node_TransactionStmt{TransactionStmt: n}}
}

// parseTransactionStmt is gram.y's TransactionStmt and
// TransactionStmtLegacy, dispatched on the leading keyword.
func (p *parser) parseTransactionStmt() *ast.Node {
	tok := p.next()
	n := &ast.TransactionStmt{Location: -1}
	switch tok.Kind {
	case ast.Token_ABORT_P:
		n.Kind = ast.TransactionStmtKind_TRANS_STMT_ROLLBACK
		p.parseOptTransaction()
		n.Chain = p.parseOptTransactionChain()
	case ast.Token_START:
		n.Kind = ast.TransactionStmtKind_TRANS_STMT_START
		p.expect(ast.Token_TRANSACTION)
		if p.transactionModeItemStarts() {
			n.Options = p.parseTransactionModeList()
		}
	case ast.Token_COMMIT:
		n.Kind = ast.TransactionStmtKind_TRANS_STMT_COMMIT
		if p.kind() == ast.Token_PREPARED {
			p.next()
			n.Kind = ast.TransactionStmtKind_TRANS_STMT_COMMIT_PREPARED
			stok := p.peek()
			n.Gid = p.sconst()
			n.Location = stok.Start
			break
		}
		p.parseOptTransaction()
		n.Chain = p.parseOptTransactionChain()
	case ast.Token_ROLLBACK:
		if p.kind() == ast.Token_PREPARED {
			p.next()
			n.Kind = ast.TransactionStmtKind_TRANS_STMT_ROLLBACK_PREPARED
			stok := p.peek()
			n.Gid = p.sconst()
			n.Location = stok.Start
			break
		}
		p.parseOptTransaction()
		if p.have(ast.Token_TO) {
			n.Kind = ast.TransactionStmtKind_TRANS_STMT_ROLLBACK_TO
			p.have(ast.Token_SAVEPOINT)
			stok := p.peek()
			n.SavepointName = p.colId()
			n.Location = stok.Start
			break
		}
		n.Kind = ast.TransactionStmtKind_TRANS_STMT_ROLLBACK
		n.Chain = p.parseOptTransactionChain()
	case ast.Token_SAVEPOINT:
		n.Kind = ast.TransactionStmtKind_TRANS_STMT_SAVEPOINT
		stok := p.peek()
		n.SavepointName = p.colId()
		n.Location = stok.Start
	case ast.Token_RELEASE:
		n.Kind = ast.TransactionStmtKind_TRANS_STMT_RELEASE
		p.have(ast.Token_SAVEPOINT)
		stok := p.peek()
		n.SavepointName = p.colId()
		n.Location = stok.Start
	case ast.Token_BEGIN_P:
		n.Kind = ast.TransactionStmtKind_TRANS_STMT_BEGIN
		p.parseOptTransaction()
		if p.transactionModeItemStarts() {
			n.Options = p.parseTransactionModeList()
		}
	case ast.Token_END_P:
		n.Kind = ast.TransactionStmtKind_TRANS_STMT_COMMIT
		p.parseOptTransaction()
		n.Chain = p.parseOptTransactionChain()
	}
	return nTransactionStmt(n)
}

// parsePrepareTransactionStmt is TransactionStmt's PREPARE TRANSACTION
// Sconst; PREPARE is consumed by the dispatcher.
func (p *parser) parsePrepareTransactionStmt() *ast.Node {
	p.expect(ast.Token_TRANSACTION)
	stok := p.peek()
	return nTransactionStmt(&ast.TransactionStmt{
		Kind:     ast.TransactionStmtKind_TRANS_STMT_PREPARE,
		Gid:      p.sconst(),
		Location: stok.Start,
	})
}

// parseNotifyStmt is gram.y's NotifyStmt.
func (p *parser) parseNotifyStmt() *ast.Node {
	p.expect(ast.Token_NOTIFY)
	n := &ast.NotifyStmt{Conditionname: p.colId()}
	if p.have(ast.Token(',')) {
		n.Payload = p.sconst()
	}
	return &ast.Node{Node: &ast.Node_NotifyStmt{NotifyStmt: n}}
}

// parseListenStmt is gram.y's ListenStmt.
func (p *parser) parseListenStmt() *ast.Node {
	p.expect(ast.Token_LISTEN)
	return &ast.Node{Node: &ast.Node_ListenStmt{ListenStmt: &ast.ListenStmt{
		Conditionname: p.colId(),
	}}}
}

// parseUnlistenStmt is gram.y's UnlistenStmt.
func (p *parser) parseUnlistenStmt() *ast.Node {
	p.expect(ast.Token_UNLISTEN)
	n := &ast.UnlistenStmt{}
	if p.have(ast.Token('*')) {
		// conditionname stays empty
	} else {
		n.Conditionname = p.colId()
	}
	return &ast.Node{Node: &ast.Node_UnlistenStmt{UnlistenStmt: n}}
}

// parseLoadStmt is gram.y's LoadStmt: LOAD file_name.
func (p *parser) parseLoadStmt() *ast.Node {
	p.expect(ast.Token_LOAD)
	return &ast.Node{Node: &ast.Node_LoadStmt{LoadStmt: &ast.LoadStmt{
		Filename: p.sconst(),
	}}}
}

// Lock mode numbers from lockdefs.h.
const (
	accessShareLock          = 1
	rowShareLock             = 2
	rowExclusiveLock         = 3
	shareUpdateExclusiveLock = 4
	shareLock                = 5
	shareRowExclusiveLock    = 6
	exclusiveLock            = 7
	accessExclusiveLock      = 8
)

// parseLockStmt is gram.y's LockStmt:
//
//	LOCK_P opt_table relation_expr_list opt_lock opt_nowait
func (p *parser) parseLockStmt() *ast.Node {
	p.expect(ast.Token_LOCK_P)
	p.have(ast.Token_TABLE)
	n := &ast.LockStmt{Mode: accessExclusiveLock}
	n.Relations = p.parseRelationExprList()
	if p.have(ast.Token_IN_P) {
		// lock_type MODE
		switch {
		case p.have(ast.Token_ACCESS):
			switch {
			case p.have(ast.Token_SHARE):
				n.Mode = accessShareLock
			case p.have(ast.Token_EXCLUSIVE):
				n.Mode = accessExclusiveLock
			default:
				p.syntaxErrorAt()
			}
		case p.have(ast.Token_ROW):
			switch {
			case p.have(ast.Token_SHARE):
				n.Mode = rowShareLock
			case p.have(ast.Token_EXCLUSIVE):
				n.Mode = rowExclusiveLock
			default:
				p.syntaxErrorAt()
			}
		case p.have(ast.Token_SHARE):
			switch {
			case p.have(ast.Token_UPDATE):
				p.expect(ast.Token_EXCLUSIVE)
				n.Mode = shareUpdateExclusiveLock
			case p.have(ast.Token_ROW):
				p.expect(ast.Token_EXCLUSIVE)
				n.Mode = shareRowExclusiveLock
			default:
				n.Mode = shareLock
			}
		case p.have(ast.Token_EXCLUSIVE):
			n.Mode = exclusiveLock
		default:
			p.syntaxErrorAt()
		}
		p.expect(ast.Token_MODE)
	}
	n.Nowait = p.have(ast.Token_NOWAIT)
	return &ast.Node{Node: &ast.Node_LockStmt{LockStmt: n}}
}

// parseRelationExprList is gram.y's relation_expr_list.
func (p *parser) parseRelationExprList() []*ast.Node {
	list := []*ast.Node{p.parseRelationExpr()}
	for p.have(ast.Token(',')) {
		list = append(list, p.parseRelationExpr())
	}
	return list
}

// parseOptDropBehavior is gram.y's opt_drop_behavior.
func (p *parser) parseOptDropBehavior() ast.DropBehavior {
	switch p.kind() {
	case ast.Token_CASCADE:
		p.next()
		return ast.DropBehavior_DROP_CASCADE
	case ast.Token_RESTRICT:
		p.next()
		return ast.DropBehavior_DROP_RESTRICT
	}
	return ast.DropBehavior_DROP_RESTRICT
}

// parseTruncateStmt is gram.y's TruncateStmt:
//
//	TRUNCATE opt_table relation_expr_list opt_restart_seqs opt_drop_behavior
func (p *parser) parseTruncateStmt() *ast.Node {
	p.expect(ast.Token_TRUNCATE)
	p.have(ast.Token_TABLE)
	n := &ast.TruncateStmt{}
	n.Relations = p.parseRelationExprList()
	switch p.kind() {
	case ast.Token_CONTINUE_P:
		p.next()
		p.expect(ast.Token_IDENTITY_P)
	case ast.Token_RESTART:
		p.next()
		p.expect(ast.Token_IDENTITY_P)
		n.RestartSeqs = true
	}
	n.Behavior = p.parseOptDropBehavior()
	return &ast.Node{Node: &ast.Node_TruncateStmt{TruncateStmt: n}}
}
