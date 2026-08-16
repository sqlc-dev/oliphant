package parse

// CreateStmt (CREATE TABLE), CreateAsStmt (CREATE TABLE AS, including the
// AS EXECUTE variant that lives in gram.y's ExecuteStmt), CreateSeqStmt/
// AlterSeqStmt, ViewStmt, IndexStmt, CreateMatViewStmt, RefreshMatViewStmt,
// and their shared table-element/constraint machinery.

import (
	"fmt"

	"github.com/sqlc-dev/oliphant/ast"
)

func nCreateStmt(n *ast.CreateStmt) *ast.Node {
	return &ast.Node{Node: &ast.Node_CreateStmt{CreateStmt: n}}
}

func nConstraint(n *ast.Constraint) *ast.Node {
	return &ast.Node{Node: &ast.Node_Constraint{Constraint: n}}
}

func nColumnDef(n *ast.ColumnDef) *ast.Node {
	return &ast.Node{Node: &ast.Node_ColumnDef{ColumnDef: n}}
}

// parseCreateTableOrAs parses CREATE [OptTemp] TABLE ...: CreateStmt,
// CreateAsStmt, or the CREATE TABLE ... AS EXECUTE form of ExecuteStmt.
// CREATE and OptTemp are consumed, TABLE is not.
func (p *parser) parseCreateTableOrAs(persistence string) *ast.Node {
	p.expect(ast.Token_TABLE)
	ifNotExists := false
	if p.have(ast.Token_IF_P) {
		p.expect(ast.Token_NOT)
		p.expect(ast.Token_EXISTS)
		ifNotExists = true
	}
	rel := p.parseQualifiedName()
	rel.Relpersistence = persistence

	switch p.kind() {
	case ast.Token_OF:
		// gram.y: CreateStmt: ... qualified_name OF any_name ...
		p.next()
		ntok := p.peek()
		of := makeTypeNameFromNameList(p.anyName())
		of.Location = ntok.Start
		n := &ast.CreateStmt{
			Relation:    rel,
			OfTypename:  of,
			IfNotExists: ifNotExists,
		}
		if p.have(ast.Token('(')) {
			n.TableElts = p.parseTypedTableElementList()
			p.expect(ast.Token(')'))
		}
		p.finishCreateStmtTail(n)
		return nCreateStmt(n)

	case ast.Token_PARTITION:
		// gram.y: CreateStmt: ... qualified_name PARTITION OF ...
		p.next()
		p.expect(ast.Token_OF)
		parent := p.parseQualifiedName()
		n := &ast.CreateStmt{
			Relation:     rel,
			InhRelations: []*ast.Node{nRangeVar(parent)},
			IfNotExists:  ifNotExists,
		}
		if p.have(ast.Token('(')) {
			n.TableElts = p.parseTypedTableElementList()
			p.expect(ast.Token(')'))
		}
		n.Partbound = p.parsePartitionBoundSpec()
		p.finishCreateStmtTail(n)
		return nCreateStmt(n)

	case ast.Token('('):
		// Either CreateStmt's '(' OptTableElementList ')' or a
		// create_as_target column list. The LALR tables keep both alive
		// through the parens; commit to CREATE TABLE AS only once the AS
		// is seen.
		mark := p.mark()
		var into *ast.IntoClause
		err := p.try(func() {
			into = p.parseCreateAsTarget(rel)
			p.expect(ast.Token_AS)
		})
		if err == nil {
			return p.finishCreateTableAs(into, ifNotExists)
		}
		p.reset(mark)
		n := &ast.CreateStmt{Relation: rel, IfNotExists: ifNotExists}
		p.expect(ast.Token('('))
		if p.kind() != ast.Token(')') {
			n.TableElts = p.parseTableElementList()
		}
		p.expect(ast.Token(')'))
		if p.have(ast.Token_INHERITS) {
			p.expect(ast.Token('('))
			n.InhRelations = p.parseQualifiedNameList()
			p.expect(ast.Token(')'))
		}
		p.finishCreateStmtTail(n)
		return nCreateStmt(n)

	default:
		// create_as_target without a column list.
		into := p.parseCreateAsTarget(rel)
		p.expect(ast.Token_AS)
		return p.finishCreateTableAs(into, ifNotExists)
	}
}

// finishCreateStmtTail parses the common CreateStmt tail: OptPartitionSpec
// table_access_method_clause OptWith OnCommitOption OptTableSpace.
func (p *parser) finishCreateStmtTail(n *ast.CreateStmt) {
	n.Partspec = p.parseOptPartitionSpec()
	if p.have(ast.Token_USING) {
		n.AccessMethod = p.name()
	}
	n.Options = p.parseOptWithReloptions()
	n.Oncommit = p.parseOnCommitOption()
	if p.have(ast.Token_TABLESPACE) {
		n.Tablespacename = p.name()
	}
}

// parseOptWithReloptions is gram.y's OptWith:
//
//	WITH reloptions | WITHOUT OIDS | empty
func (p *parser) parseOptWithReloptions() []*ast.Node {
	switch p.kind() {
	case ast.Token_WITH:
		if p.kindN(1) == ast.Token('(') {
			p.next()
			return p.parseReloptions()
		}
	case ast.Token_WITHOUT:
		if p.kindN(1) == ast.Token_OIDS {
			p.next()
			p.next()
		}
	}
	return nil
}

// parseOnCommitOption is gram.y's OnCommitOption.
func (p *parser) parseOnCommitOption() ast.OnCommitAction {
	if p.kind() == ast.Token_ON && p.kindN(1) == ast.Token_COMMIT {
		p.next()
		p.next()
		switch p.kind() {
		case ast.Token_DROP:
			p.next()
			return ast.OnCommitAction_ONCOMMIT_DROP
		case ast.Token_DELETE_P:
			p.next()
			p.expect(ast.Token_ROWS)
			return ast.OnCommitAction_ONCOMMIT_DELETE_ROWS
		case ast.Token_PRESERVE:
			p.next()
			p.expect(ast.Token_ROWS)
			return ast.OnCommitAction_ONCOMMIT_PRESERVE_ROWS
		}
		p.syntaxErrorAt()
	}
	return ast.OnCommitAction_ONCOMMIT_NOOP
}

// parseCreateAsTarget is gram.y's create_as_target (minus the persistence,
// which the caller has already stored into rel):
//
//	qualified_name opt_column_list table_access_method_clause OptWith
//	OnCommitOption OptTableSpace
func (p *parser) parseCreateAsTarget(rel *ast.RangeVar) *ast.IntoClause {
	into := &ast.IntoClause{Rel: rel}
	if p.have(ast.Token('(')) {
		into.ColNames = p.columnList()
		p.expect(ast.Token(')'))
	}
	if p.have(ast.Token_USING) {
		into.AccessMethod = p.name()
	}
	into.Options = p.parseOptWithReloptions()
	into.OnCommit = p.parseOnCommitOption()
	if p.have(ast.Token_TABLESPACE) {
		into.TableSpaceName = p.name()
	}
	return into
}

// finishCreateTableAs parses the query and opt_with_data after AS,
// producing CreateTableAsStmt for both the SelectStmt and EXECUTE forms.
func (p *parser) finishCreateTableAs(into *ast.IntoClause, ifNotExists bool) *ast.Node {
	ctas := &ast.CreateTableAsStmt{
		Into:        into,
		Objtype:     ast.ObjectType_OBJECT_TABLE,
		IfNotExists: ifNotExists,
	}
	if p.have(ast.Token_EXECUTE) {
		// gram.y: ExecuteStmt: CREATE OptTemp TABLE create_as_target AS
		// EXECUTE name execute_param_clause opt_with_data
		e := &ast.ExecuteStmt{Name: p.name()}
		if p.have(ast.Token('(')) {
			e.Params = p.parseExprList()
			p.expect(ast.Token(')'))
		}
		ctas.Query = &ast.Node{Node: &ast.Node_ExecuteStmt{ExecuteStmt: e}}
	} else {
		ctas.Query = p.parseSelectStmt()
	}
	into.SkipData = !p.parseOptWithData()
	return &ast.Node{Node: &ast.Node_CreateTableAsStmt{CreateTableAsStmt: ctas}}
}

// parseOptWithData is gram.y's opt_with_data.
func (p *parser) parseOptWithData() bool {
	if p.kind() == ast.Token_WITH {
		switch {
		case p.kindN(1) == ast.Token_DATA_P:
			p.next()
			p.next()
			return true
		case p.kindN(1) == ast.Token_NO && p.kindN(2) == ast.Token_DATA_P:
			p.next()
			p.next()
			p.next()
			return false
		}
	}
	return true
}

// parseTableElementList is gram.y's TableElementList.
func (p *parser) parseTableElementList() []*ast.Node {
	list := []*ast.Node{p.parseTableElement()}
	for p.have(ast.Token(',')) {
		list = append(list, p.parseTableElement())
	}
	return list
}

// parseTableElement is gram.y's TableElement: columnDef | TableLikeClause |
// TableConstraint.
func (p *parser) parseTableElement() *ast.Node {
	switch p.kind() {
	case ast.Token_LIKE:
		return p.parseTableLikeClause()
	case ast.Token_CONSTRAINT, ast.Token_CHECK, ast.Token_UNIQUE,
		ast.Token_PRIMARY, ast.Token_EXCLUDE, ast.Token_FOREIGN:
		return p.parseTableConstraint()
	}
	return p.parseColumnDef()
}

// parseTypedTableElementList is gram.y's TypedTableElementList.
func (p *parser) parseTypedTableElementList() []*ast.Node {
	list := []*ast.Node{p.parseTypedTableElement()}
	for p.have(ast.Token(',')) {
		list = append(list, p.parseTypedTableElement())
	}
	return list
}

// parseTypedTableElement is gram.y's TypedTableElement: columnOptions |
// TableConstraint.
func (p *parser) parseTypedTableElement() *ast.Node {
	switch p.kind() {
	case ast.Token_CONSTRAINT, ast.Token_CHECK, ast.Token_UNIQUE,
		ast.Token_PRIMARY, ast.Token_EXCLUDE, ast.Token_FOREIGN:
		return p.parseTableConstraint()
	}
	// gram.y: columnOptions: ColId [WITH OPTIONS] ColQualList
	tok := p.peek()
	n := &ast.ColumnDef{
		Colname:  p.colId(),
		IsLocal:  true,
		Location: tok.Start,
	}
	if p.kind() == ast.Token_WITH && p.kindN(1) == ast.Token_OPTIONS {
		p.next()
		p.next()
	}
	n.Constraints, n.CollClause = p.parseColQualList()
	return nColumnDef(n)
}

// parseColumnDef is gram.y's columnDef:
//
//	ColId Typename opt_column_storage opt_column_compression
//	create_generic_options ColQualList
func (p *parser) parseColumnDef() *ast.Node {
	tok := p.peek()
	n := &ast.ColumnDef{
		Colname:  p.colId(),
		IsLocal:  true,
		Location: tok.Start,
	}
	n.TypeName = p.parseTypename()
	if p.kind() == ast.Token_STORAGE {
		p.next()
		n.StorageName = p.parseColIdOrDefault()
	}
	if p.kind() == ast.Token_COMPRESSION {
		p.next()
		n.Compression = p.parseColIdOrDefault()
	}
	n.Fdwoptions = p.parseCreateGenericOptions()
	n.Constraints, n.CollClause = p.parseColQualList()
	return nColumnDef(n)
}

// parseColIdOrDefault handles column_storage/column_compression's
// ColId-or-DEFAULT alternative.
func (p *parser) parseColIdOrDefault() string {
	if p.have(ast.Token_DEFAULT) {
		return "default"
	}
	return p.colId()
}

// parseColQualList is gram.y's ColQualList plus SplitColQualList: collect
// column constraints, splitting out a single CollateClause.
func (p *parser) parseColQualList() ([]*ast.Node, *ast.CollateClause) {
	var constraints []*ast.Node
	var coll *ast.CollateClause
	for {
		tok := p.peek()
		switch tok.Kind {
		case ast.Token_COLLATE:
			p.next()
			c := &ast.CollateClause{Collname: p.anyName(), Location: tok.Start}
			// gram.y: SplitColQualList
			if coll != nil {
				p.ereport("base_yyparse", "multiple COLLATE clauses not allowed", c.Location)
			}
			coll = c
		case ast.Token_CONSTRAINT:
			p.next()
			name := p.name()
			c := p.parseColConstraintElem()
			c.Conname = name
			c.Location = tok.Start
			constraints = append(constraints, nConstraint(c))
		case ast.Token_NOT:
			switch p.kindN(1) {
			case ast.Token_NULL_P:
				p.next()
				p.next()
				constraints = append(constraints, nConstraint(&ast.Constraint{
					Contype:  ast.ConstrType_CONSTR_NOTNULL,
					Location: tok.Start,
				}))
			case ast.Token_DEFERRABLE:
				p.next()
				p.next()
				constraints = append(constraints, nConstraint(&ast.Constraint{
					Contype:  ast.ConstrType_CONSTR_ATTR_NOT_DEFERRABLE,
					Location: tok.Start,
				}))
			default:
				return constraints, coll
			}
		case ast.Token_DEFERRABLE:
			p.next()
			constraints = append(constraints, nConstraint(&ast.Constraint{
				Contype:  ast.ConstrType_CONSTR_ATTR_DEFERRABLE,
				Location: tok.Start,
			}))
		case ast.Token_INITIALLY:
			p.next()
			switch {
			case p.have(ast.Token_DEFERRED):
				constraints = append(constraints, nConstraint(&ast.Constraint{
					Contype:  ast.ConstrType_CONSTR_ATTR_DEFERRED,
					Location: tok.Start,
				}))
			case p.have(ast.Token_IMMEDIATE):
				constraints = append(constraints, nConstraint(&ast.Constraint{
					Contype:  ast.ConstrType_CONSTR_ATTR_IMMEDIATE,
					Location: tok.Start,
				}))
			default:
				p.syntaxErrorAt()
			}
		case ast.Token_NULL_P, ast.Token_UNIQUE, ast.Token_PRIMARY,
			ast.Token_CHECK, ast.Token_DEFAULT, ast.Token_GENERATED,
			ast.Token_REFERENCES:
			c := p.parseColConstraintElem()
			constraints = append(constraints, nConstraint(c))
		default:
			return constraints, coll
		}
	}
}

// parseColConstraintElem is gram.y's ColConstraintElem. The leading
// CONSTRAINT name (if any) is handled by the caller.
func (p *parser) parseColConstraintElem() *ast.Constraint {
	tok := p.peek()
	n := &ast.Constraint{Location: tok.Start}
	switch tok.Kind {
	case ast.Token_NOT:
		p.next()
		p.expect(ast.Token_NULL_P)
		n.Contype = ast.ConstrType_CONSTR_NOTNULL
	case ast.Token_NULL_P:
		p.next()
		n.Contype = ast.ConstrType_CONSTR_NULL
	case ast.Token_UNIQUE:
		p.next()
		n.Contype = ast.ConstrType_CONSTR_UNIQUE
		n.NullsNotDistinct = !p.parseOptUniqueNullTreatment()
		n.Options = p.parseOptDefinition()
		n.Indexspace = p.parseOptConsTableSpace()
	case ast.Token_PRIMARY:
		p.next()
		p.expect(ast.Token_KEY)
		n.Contype = ast.ConstrType_CONSTR_PRIMARY
		n.Options = p.parseOptDefinition()
		n.Indexspace = p.parseOptConsTableSpace()
	case ast.Token_CHECK:
		p.next()
		n.Contype = ast.ConstrType_CONSTR_CHECK
		n.InitiallyValid = true
		p.expect(ast.Token('('))
		n.RawExpr = p.parseAExpr(0)
		p.expect(ast.Token(')'))
		if p.kind() == ast.Token_NO && p.kindN(1) == ast.Token_INHERIT {
			p.next()
			p.next()
			n.IsNoInherit = true
		}
	case ast.Token_DEFAULT:
		p.next()
		n.Contype = ast.ConstrType_CONSTR_DEFAULT
		n.RawExpr = p.parseBExpr(0)
	case ast.Token_GENERATED:
		p.next()
		wtok := p.peek()
		n.GeneratedWhen = p.parseGeneratedWhen()
		p.expect(ast.Token_AS)
		if p.have(ast.Token_IDENTITY_P) {
			n.Contype = ast.ConstrType_CONSTR_IDENTITY
			if p.have(ast.Token('(')) {
				n.Options = p.parseSeqOptList()
				p.expect(ast.Token(')'))
			}
		} else {
			n.Contype = ast.ConstrType_CONSTR_GENERATED
			p.expect(ast.Token('('))
			n.RawExpr = p.parseAExpr(0)
			p.expect(ast.Token(')'))
			p.expect(ast.Token_STORED)
			if n.GeneratedWhen != attributeIdentityAlways {
				p.ereport("base_yyparse", "for a generated column, GENERATED ALWAYS must be specified", wtok.Start)
			}
		}
	case ast.Token_REFERENCES:
		p.next()
		n.Contype = ast.ConstrType_CONSTR_FOREIGN
		n.InitiallyValid = true
		n.Pktable = p.parseQualifiedName()
		if p.have(ast.Token('(')) {
			n.PkAttrs = p.columnList()
			p.expect(ast.Token(')'))
		}
		n.FkMatchtype = p.parseKeyMatch()
		upd, del, delCols := p.parseKeyActions()
		n.FkUpdAction = upd
		n.FkDelAction = del
		n.FkDelSetCols = delCols
	default:
		p.syntaxErrorAt()
	}
	return n
}

// Identity attribute chars (pg_attribute.h).
const (
	attributeIdentityAlways    = "a"
	attributeIdentityByDefault = "d"
)

// parseGeneratedWhen is gram.y's generated_when.
func (p *parser) parseGeneratedWhen() string {
	switch {
	case p.have(ast.Token_ALWAYS):
		return attributeIdentityAlways
	case p.have(ast.Token_BY):
		p.expect(ast.Token_DEFAULT)
		return attributeIdentityByDefault
	}
	p.syntaxErrorAt()
	return ""
}

// parseOptUniqueNullTreatment is gram.y's opt_unique_null_treatment;
// returns true for NULLS DISTINCT (the default).
func (p *parser) parseOptUniqueNullTreatment() bool {
	if p.have(ast.Token_NULLS_P) {
		if p.have(ast.Token_NOT) {
			p.expect(ast.Token_DISTINCT)
			return false
		}
		p.expect(ast.Token_DISTINCT)
		return true
	}
	return true
}

// parseOptDefinition is gram.y's opt_definition: WITH definition | empty.
func (p *parser) parseOptDefinition() []*ast.Node {
	if p.kind() == ast.Token_WITH && p.kindN(1) == ast.Token('(') {
		p.next()
		return p.parseDefinition()
	}
	return nil
}

// parseDefinition is gram.y's definition: '(' def_list ')'.
func (p *parser) parseDefinition() []*ast.Node {
	p.expect(ast.Token('('))
	list := []*ast.Node{p.parseDefElem()}
	for p.have(ast.Token(',')) {
		list = append(list, p.parseDefElem())
	}
	p.expect(ast.Token(')'))
	return list
}

// parseDefElem is gram.y's def_elem: ColLabel ['=' def_arg].
func (p *parser) parseDefElem() *ast.Node {
	tok := p.peek()
	name := p.colLabel()
	var arg *ast.Node
	if p.have(ast.Token('=')) {
		arg = p.parseDefArg()
	}
	return makeDefElem(name, arg, tok.Start)
}

// parseOptConsTableSpace is gram.y's OptConsTableSpace:
//
//	USING INDEX TABLESPACE name | empty
func (p *parser) parseOptConsTableSpace() string {
	if p.kind() == ast.Token_USING && p.kindN(1) == ast.Token_INDEX &&
		p.kindN(2) == ast.Token_TABLESPACE {
		p.next()
		p.next()
		p.next()
		return p.name()
	}
	return ""
}

// parseKeyMatch is gram.y's key_match.
func (p *parser) parseKeyMatch() string {
	if p.kind() == ast.Token_MATCH {
		mtok := p.next()
		switch {
		case p.have(ast.Token_FULL):
			return "f" // FKCONSTR_MATCH_FULL
		case p.have(ast.Token_PARTIAL):
			p.ereport("base_yyparse", "MATCH PARTIAL not yet implemented", mtok.Start)
		case p.have(ast.Token_SIMPLE):
			return "s" // FKCONSTR_MATCH_SIMPLE
		default:
			p.syntaxErrorAt()
		}
	}
	return "s"
}

// parseKeyActions is gram.y's key_actions; returns the update action, the
// delete action, and the delete action's column list.
func (p *parser) parseKeyActions() (string, string, []*ast.Node) {
	upd, del := "a", "a" // FKCONSTR_ACTION_NOACTION
	var delCols []*ast.Node
	haveUpd, haveDel := false, false
	for p.kind() == ast.Token_ON &&
		(p.kindN(1) == ast.Token_UPDATE || p.kindN(1) == ast.Token_DELETE_P) {
		ontok := p.next()
		isUpd := p.next().Kind == ast.Token_UPDATE
		action, cols := p.parseKeyAction()
		if isUpd {
			if haveUpd {
				p.syntaxError(ontok)
			}
			haveUpd = true
			if cols != nil {
				// gram.y: key_update's column-list ereport
				what := "SET DEFAULT"
				if action == "n" {
					what = "SET NULL"
				}
				p.ereport("base_yyparse", fmt.Sprintf("a column list with %s is only supported for ON DELETE actions", what), ontok.Start)
			}
			upd = action
		} else {
			if haveDel {
				p.syntaxError(ontok)
			}
			haveDel = true
			del = action
			delCols = cols
		}
	}
	return upd, del, delCols
}

// parseKeyAction is gram.y's key_action.
func (p *parser) parseKeyAction() (string, []*ast.Node) {
	switch {
	case p.have(ast.Token_NO):
		p.expect(ast.Token_ACTION)
		return "a", nil
	case p.have(ast.Token_RESTRICT):
		return "r", nil
	case p.have(ast.Token_CASCADE):
		return "c", nil
	case p.have(ast.Token_SET):
		var action string
		switch {
		case p.have(ast.Token_NULL_P):
			action = "n"
		case p.have(ast.Token_DEFAULT):
			action = "d"
		default:
			p.syntaxErrorAt()
		}
		var cols []*ast.Node
		if p.have(ast.Token('(')) {
			cols = p.columnList()
			p.expect(ast.Token(')'))
		}
		return action, cols
	}
	p.syntaxErrorAt()
	return "", nil
}

// parseTableLikeClause is gram.y's TableLikeClause.
func (p *parser) parseTableLikeClause() *ast.Node {
	p.expect(ast.Token_LIKE)
	n := &ast.TableLikeClause{Relation: p.parseQualifiedName()}
	// gram.y: TableLikeOptionList
	for {
		switch {
		case p.have(ast.Token_INCLUDING):
			n.Options |= p.parseTableLikeOption()
		case p.have(ast.Token_EXCLUDING):
			n.Options &^= p.parseTableLikeOption()
		default:
			return &ast.Node{Node: &ast.Node_TableLikeClause{TableLikeClause: n}}
		}
	}
}

// CREATE_TABLE_LIKE_* bits (parsenodes.h).
const (
	createTableLikeComments    = 1 << 0
	createTableLikeCompression = 1 << 1
	createTableLikeConstraints = 1 << 2
	createTableLikeDefaults    = 1 << 3
	createTableLikeGenerated   = 1 << 4
	createTableLikeIdentity    = 1 << 5
	createTableLikeIndexes     = 1 << 6
	createTableLikeStatistics  = 1 << 7
	createTableLikeStorage     = 1 << 8
	createTableLikeAll         = 0x7FFFFFFF
)

// parseTableLikeOption is gram.y's TableLikeOption.
func (p *parser) parseTableLikeOption() uint32 {
	switch {
	case p.have(ast.Token_COMMENTS):
		return createTableLikeComments
	case p.have(ast.Token_COMPRESSION):
		return createTableLikeCompression
	case p.have(ast.Token_CONSTRAINTS):
		return createTableLikeConstraints
	case p.have(ast.Token_DEFAULTS):
		return createTableLikeDefaults
	case p.have(ast.Token_IDENTITY_P):
		return createTableLikeIdentity
	case p.have(ast.Token_GENERATED):
		return createTableLikeGenerated
	case p.have(ast.Token_INDEXES):
		return createTableLikeIndexes
	case p.have(ast.Token_STATISTICS):
		return createTableLikeStatistics
	case p.have(ast.Token_STORAGE):
		return createTableLikeStorage
	case p.have(ast.Token_ALL):
		return createTableLikeAll
	}
	p.syntaxErrorAt()
	return 0
}

// parseTableConstraint is gram.y's TableConstraint.
func (p *parser) parseTableConstraint() *ast.Node {
	tok := p.peek()
	if tok.Kind == ast.Token_CONSTRAINT {
		p.next()
		name := p.name()
		c := p.parseConstraintElem()
		c.Conname = name
		c.Location = tok.Start
		return nConstraint(c)
	}
	return nConstraint(p.parseConstraintElem())
}

// Constraint attribute bits (gram.y CAS_*).
const (
	casNotDeferrable      = 1 << 0
	casDeferrable         = 1 << 1
	casInitiallyImmediate = 1 << 2
	casInitiallyDeferred  = 1 << 3
	casNotValid           = 1 << 4
	casNoInherit          = 1 << 5
)

// parseConstraintAttributeSpec is gram.y's ConstraintAttributeSpec; returns
// the accumulated CAS bits and the location of the spec (its first token,
// or -1 when empty).
func (p *parser) parseConstraintAttributeSpec() (int, int32) {
	spec := 0
	loc := int32(-1)
	for {
		tok := p.peek()
		var bit int
		switch tok.Kind {
		case ast.Token_NOT:
			switch p.kindN(1) {
			case ast.Token_DEFERRABLE:
				p.next()
				p.next()
				bit = casNotDeferrable
			case ast.Token_VALID:
				p.next()
				p.next()
				bit = casNotValid
			default:
				return spec, loc
			}
		case ast.Token_DEFERRABLE:
			p.next()
			bit = casDeferrable
		case ast.Token_INITIALLY:
			switch p.kindN(1) {
			case ast.Token_IMMEDIATE:
				p.next()
				p.next()
				bit = casInitiallyImmediate
			case ast.Token_DEFERRED:
				p.next()
				p.next()
				bit = casInitiallyDeferred
			default:
				return spec, loc
			}
		case ast.Token_NO:
			if p.kindN(1) == ast.Token_INHERIT {
				p.next()
				p.next()
				bit = casNoInherit
			} else {
				return spec, loc
			}
		default:
			return spec, loc
		}
		newspec := spec | bit
		// gram.y: ConstraintAttributeSpec conflict checks, reported at @2.
		if newspec&(casNotDeferrable|casInitiallyDeferred) == casNotDeferrable|casInitiallyDeferred {
			p.ereport("base_yyparse", "constraint declared INITIALLY DEFERRED must be DEFERRABLE", tok.Start)
		}
		if newspec&(casNotDeferrable|casDeferrable) == casNotDeferrable|casDeferrable ||
			newspec&(casInitiallyImmediate|casInitiallyDeferred) == casInitiallyImmediate|casInitiallyDeferred {
			p.ereport("base_yyparse", "conflicting constraint properties", tok.Start)
		}
		spec = newspec
		if loc < 0 {
			loc = tok.Start
		}
	}
}

// processCASbits is gram.y's processCASbits: apply the attribute bits to
// the flags the constraint type supports, erroring on the rest.
func (p *parser) processCASbits(casBits int, loc int32, constrType string,
	deferrable, initdeferred, notValid, noInherit *bool) {
	if casBits&(casDeferrable|casInitiallyDeferred) != 0 {
		if deferrable != nil {
			*deferrable = true
		} else {
			p.ereport("base_yyparse",
				fmt.Sprintf("%s constraints cannot be marked DEFERRABLE", constrType), loc)
		}
	}
	if casBits&casInitiallyDeferred != 0 {
		if initdeferred != nil {
			*initdeferred = true
		} else {
			p.ereport("base_yyparse",
				fmt.Sprintf("%s constraints cannot be marked DEFERRABLE", constrType), loc)
		}
	}
	if casBits&casNotValid != 0 {
		if notValid != nil {
			*notValid = true
		} else {
			p.ereport("base_yyparse",
				fmt.Sprintf("%s constraints cannot be marked NOT VALID", constrType), loc)
		}
	}
	if casBits&casNoInherit != 0 {
		if noInherit != nil {
			*noInherit = true
		} else {
			p.ereport("base_yyparse",
				fmt.Sprintf("%s constraints cannot be marked NO INHERIT", constrType), loc)
		}
	}
}

// parseConstraintElem is gram.y's ConstraintElem. The leading CONSTRAINT
// name (if any) is handled by the caller.
func (p *parser) parseConstraintElem() *ast.Constraint {
	tok := p.peek()
	n := &ast.Constraint{Location: tok.Start}
	switch tok.Kind {
	case ast.Token_CHECK:
		p.next()
		n.Contype = ast.ConstrType_CONSTR_CHECK
		p.expect(ast.Token('('))
		n.RawExpr = p.parseAExpr(0)
		p.expect(ast.Token(')'))
		cas, casLoc := p.parseConstraintAttributeSpec()
		p.processCASbits(cas, casLoc, "CHECK", nil, nil, &n.SkipValidation, &n.IsNoInherit)
		n.InitiallyValid = !n.SkipValidation
	case ast.Token_UNIQUE:
		p.next()
		n.Contype = ast.ConstrType_CONSTR_UNIQUE
		if p.kind() == ast.Token_USING && p.kindN(1) == ast.Token_INDEX &&
			p.kindN(2) != ast.Token_TABLESPACE {
			// gram.y: UNIQUE ExistingIndex
			p.next()
			p.next()
			n.Indexname = p.name()
			cas, casLoc := p.parseConstraintAttributeSpec()
			p.processCASbits(cas, casLoc, "UNIQUE", &n.Deferrable, &n.Initdeferred, nil, nil)
			break
		}
		n.NullsNotDistinct = !p.parseOptUniqueNullTreatment()
		p.expect(ast.Token('('))
		n.Keys = p.columnList()
		p.expect(ast.Token(')'))
		n.Including = p.parseOptCInclude()
		n.Options = p.parseOptDefinition()
		n.Indexspace = p.parseOptConsTableSpace()
		cas, casLoc := p.parseConstraintAttributeSpec()
		p.processCASbits(cas, casLoc, "UNIQUE", &n.Deferrable, &n.Initdeferred, nil, nil)
	case ast.Token_PRIMARY:
		p.next()
		p.expect(ast.Token_KEY)
		n.Contype = ast.ConstrType_CONSTR_PRIMARY
		if p.kind() == ast.Token_USING && p.kindN(1) == ast.Token_INDEX &&
			p.kindN(2) != ast.Token_TABLESPACE {
			p.next()
			p.next()
			n.Indexname = p.name()
			cas, casLoc := p.parseConstraintAttributeSpec()
			p.processCASbits(cas, casLoc, "PRIMARY KEY", &n.Deferrable, &n.Initdeferred, nil, nil)
			break
		}
		p.expect(ast.Token('('))
		n.Keys = p.columnList()
		p.expect(ast.Token(')'))
		n.Including = p.parseOptCInclude()
		n.Options = p.parseOptDefinition()
		n.Indexspace = p.parseOptConsTableSpace()
		cas, casLoc := p.parseConstraintAttributeSpec()
		p.processCASbits(cas, casLoc, "PRIMARY KEY", &n.Deferrable, &n.Initdeferred, nil, nil)
	case ast.Token_EXCLUDE:
		p.next()
		n.Contype = ast.ConstrType_CONSTR_EXCLUSION
		// access_method_clause
		if p.have(ast.Token_USING) {
			n.AccessMethod = p.name()
		} else {
			n.AccessMethod = "btree" // DEFAULT_INDEX_TYPE
		}
		p.expect(ast.Token('('))
		n.Exclusions = p.parseExclusionConstraintList()
		p.expect(ast.Token(')'))
		n.Including = p.parseOptCInclude()
		n.Options = p.parseOptDefinition()
		n.Indexspace = p.parseOptConsTableSpace()
		if p.have(ast.Token_WHERE) {
			p.expect(ast.Token('('))
			n.WhereClause = p.parseAExpr(0)
			p.expect(ast.Token(')'))
		}
		cas, casLoc := p.parseConstraintAttributeSpec()
		p.processCASbits(cas, casLoc, "EXCLUDE", &n.Deferrable, &n.Initdeferred, nil, nil)
	case ast.Token_FOREIGN:
		p.next()
		p.expect(ast.Token_KEY)
		n.Contype = ast.ConstrType_CONSTR_FOREIGN
		p.expect(ast.Token('('))
		n.FkAttrs = p.columnList()
		p.expect(ast.Token(')'))
		p.expect(ast.Token_REFERENCES)
		n.Pktable = p.parseQualifiedName()
		if p.have(ast.Token('(')) {
			n.PkAttrs = p.columnList()
			p.expect(ast.Token(')'))
		}
		n.FkMatchtype = p.parseKeyMatch()
		upd, del, delCols := p.parseKeyActions()
		n.FkUpdAction = upd
		n.FkDelAction = del
		n.FkDelSetCols = delCols
		cas, casLoc := p.parseConstraintAttributeSpec()
		p.processCASbits(cas, casLoc, "FOREIGN KEY", &n.Deferrable, &n.Initdeferred, &n.SkipValidation, nil)
		n.InitiallyValid = !n.SkipValidation
	default:
		p.syntaxErrorAt()
	}
	return n
}

// parseOptCInclude is gram.y's opt_c_include.
func (p *parser) parseOptCInclude() []*ast.Node {
	if p.have(ast.Token_INCLUDE) {
		p.expect(ast.Token('('))
		cols := p.columnList()
		p.expect(ast.Token(')'))
		return cols
	}
	return nil
}

// parseExclusionConstraintList is gram.y's ExclusionConstraintList.
func (p *parser) parseExclusionConstraintList() []*ast.Node {
	list := []*ast.Node{p.parseExclusionConstraintElem()}
	for p.have(ast.Token(',')) {
		list = append(list, p.parseExclusionConstraintElem())
	}
	return list
}

// parseExclusionConstraintElem is gram.y's ExclusionConstraintElem:
//
//	index_elem WITH any_operator | index_elem WITH OPERATOR '(' any_operator ')'
func (p *parser) parseExclusionConstraintElem() *ast.Node {
	el := p.parseIndexElem()
	p.expect(ast.Token_WITH)
	var op []*ast.Node
	if p.have(ast.Token_OPERATOR) {
		p.expect(ast.Token('('))
		op = p.parseAnyOperator()
		p.expect(ast.Token(')'))
	} else {
		op = p.parseAnyOperator()
	}
	return nList([]*ast.Node{el, nList(op)})
}

// parseOptPartitionSpec is gram.y's OptPartitionSpec.
func (p *parser) parseOptPartitionSpec() *ast.PartitionSpec {
	if p.kind() != ast.Token_PARTITION {
		return nil
	}
	tok := p.next()
	p.expect(ast.Token_BY)
	stok := p.peek()
	strategy := p.colId()
	n := &ast.PartitionSpec{Location: tok.Start}
	// gram.y: parsePartitionStrategy
	switch {
	case strEqualsFold(strategy, "list"):
		n.Strategy = ast.PartitionStrategy_PARTITION_STRATEGY_LIST
	case strEqualsFold(strategy, "range"):
		n.Strategy = ast.PartitionStrategy_PARTITION_STRATEGY_RANGE
	case strEqualsFold(strategy, "hash"):
		n.Strategy = ast.PartitionStrategy_PARTITION_STRATEGY_HASH
	default:
		p.fail(p.filter.ParserError("base_yyparse",
			fmt.Sprintf("unrecognized partitioning strategy %q", strategy), -1))
		_ = stok
	}
	p.expect(ast.Token('('))
	n.PartParams = []*ast.Node{p.parsePartElem()}
	for p.have(ast.Token(',')) {
		n.PartParams = append(n.PartParams, p.parsePartElem())
	}
	p.expect(ast.Token(')'))
	return n
}

// parsePartElem is gram.y's part_elem.
func (p *parser) parsePartElem() *ast.Node {
	tok := p.peek()
	n := &ast.PartitionElem{Location: tok.Start}
	switch {
	case tok.Kind == ast.Token('('):
		p.next()
		n.Expr = p.parseAExpr(0)
		p.expect(ast.Token(')'))
	case p.startsFunctionCallAhead():
		n.Expr = p.parseFuncExprWindowless()
	default:
		n.Name = p.colId()
	}
	if p.have(ast.Token_COLLATE) {
		n.Collation = p.anyName()
	}
	if isColIdToken(p.peek()) {
		n.Opclass = p.anyName()
	}
	return &ast.Node{Node: &ast.Node_PartitionElem{PartitionElem: n}}
}

// parsePartitionBoundSpec is gram.y's PartitionBoundSpec.
func (p *parser) parsePartitionBoundSpec() *ast.PartitionBoundSpec {
	if p.have(ast.Token_DEFAULT) {
		return &ast.PartitionBoundSpec{IsDefault: true, Location: p.toks[p.pos-1].Start}
	}
	p.expect(ast.Token_FOR)
	p.expect(ast.Token_VALUES)
	tok := p.peek()
	switch tok.Kind {
	case ast.Token_WITH:
		// hash partition
		p.next()
		p.expect(ast.Token('('))
		n := &ast.PartitionBoundSpec{
			Strategy: "h", // PARTITION_STRATEGY_HASH
			Modulus:  -1, Remainder: -1,
			Location: tok.Start,
		}
		for {
			etok := p.peek()
			name := p.nonReservedWord()
			itok := p.peek()
			val := p.iconst()
			_ = itok
			switch name {
			case "modulus":
				if n.Modulus != -1 {
					p.ereport("base_yyparse", "modulus for hash partition provided more than once", etok.Start)
				}
				n.Modulus = val
			case "remainder":
				if n.Remainder != -1 {
					p.ereport("base_yyparse", "remainder for hash partition provided more than once", etok.Start)
				}
				n.Remainder = val
			default:
				p.ereport("base_yyparse",
					fmt.Sprintf("unrecognized hash partition bound specification %q", name), etok.Start)
			}
			if !p.have(ast.Token(',')) {
				break
			}
		}
		p.expect(ast.Token(')'))
		if n.Modulus == -1 {
			p.ereport("base_yyparse", "modulus for hash partition must be specified", -1)
		}
		if n.Remainder == -1 {
			p.ereport("base_yyparse", "remainder for hash partition must be specified", -1)
		}
		return n
	case ast.Token_IN_P:
		p.next()
		p.expect(ast.Token('('))
		n := &ast.PartitionBoundSpec{
			Strategy:   "l", // PARTITION_STRATEGY_LIST
			Listdatums: p.parseExprList(),
			Location:   tok.Start,
		}
		p.expect(ast.Token(')'))
		return n
	case ast.Token_FROM:
		p.next()
		p.expect(ast.Token('('))
		n := &ast.PartitionBoundSpec{
			Strategy:    "r", // PARTITION_STRATEGY_RANGE
			Lowerdatums: p.parseExprList(),
			Location:    tok.Start,
		}
		p.expect(ast.Token(')'))
		p.expect(ast.Token_TO)
		p.expect(ast.Token('('))
		n.Upperdatums = p.parseExprList()
		p.expect(ast.Token(')'))
		return n
	}
	p.syntaxErrorAt()
	return nil
}

// strEqualsFold is pg_strcasecmp(a, b) == 0 for ASCII.
func strEqualsFold(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := 0; i < len(a); i++ {
		ca, cb := a[i], b[i]
		if 'A' <= ca && ca <= 'Z' {
			ca += 'a' - 'A'
		}
		if 'A' <= cb && cb <= 'Z' {
			cb += 'a' - 'A'
		}
		if ca != cb {
			return false
		}
	}
	return true
}

// parseCreateGenericOptions is gram.y's create_generic_options:
//
//	OPTIONS '(' generic_option_list ')' | empty
func (p *parser) parseCreateGenericOptions() []*ast.Node {
	if p.kind() != ast.Token_OPTIONS || p.kindN(1) != ast.Token('(') {
		return nil
	}
	p.next()
	p.next()
	list := []*ast.Node{p.parseGenericOptionElem()}
	for p.have(ast.Token(',')) {
		list = append(list, p.parseGenericOptionElem())
	}
	p.expect(ast.Token(')'))
	return list
}

// parseGenericOptionElem is gram.y's generic_option_elem:
//
//	generic_option_name generic_option_arg
func (p *parser) parseGenericOptionElem() *ast.Node {
	tok := p.peek()
	name := p.colLabel()
	return makeDefElem(name, nStr(p.sconst()), tok.Start)
}

// parseCreateSeqStmt is gram.y's CreateSeqStmt; CREATE OptTemp consumed,
// SEQUENCE not.
func (p *parser) parseCreateSeqStmt(persistence string) *ast.Node {
	p.expect(ast.Token_SEQUENCE)
	n := &ast.CreateSeqStmt{}
	if p.have(ast.Token_IF_P) {
		p.expect(ast.Token_NOT)
		p.expect(ast.Token_EXISTS)
		n.IfNotExists = true
	}
	n.Sequence = p.parseQualifiedName()
	n.Sequence.Relpersistence = persistence
	if p.seqOptElemStarts() {
		n.Options = p.parseSeqOptList()
	}
	return &ast.Node{Node: &ast.Node_CreateSeqStmt{CreateSeqStmt: n}}
}

// parseAlterSeqStmt is gram.y's AlterSeqStmt; ALTER SEQUENCE consumed.
func (p *parser) parseAlterSeqStmt() *ast.Node {
	n := &ast.AlterSeqStmt{}
	if p.have(ast.Token_IF_P) {
		p.expect(ast.Token_EXISTS)
		n.MissingOk = true
	}
	n.Sequence = p.parseQualifiedName()
	n.Options = p.parseSeqOptList()
	return &ast.Node{Node: &ast.Node_AlterSeqStmt{AlterSeqStmt: n}}
}

// seqOptElemStarts reports whether the lookahead begins a SeqOptElem.
func (p *parser) seqOptElemStarts() bool {
	switch p.kind() {
	case ast.Token_AS, ast.Token_CACHE, ast.Token_CYCLE, ast.Token_INCREMENT,
		ast.Token_LOGGED, ast.Token_MAXVALUE, ast.Token_MINVALUE,
		ast.Token_OWNED, ast.Token_SEQUENCE, ast.Token_START,
		ast.Token_RESTART, ast.Token_UNLOGGED:
		return true
	case ast.Token_NO:
		switch p.kindN(1) {
		case ast.Token_CYCLE, ast.Token_MAXVALUE, ast.Token_MINVALUE:
			return true
		}
	}
	return false
}

// parseSeqOptList is gram.y's SeqOptList: one or more SeqOptElem.
func (p *parser) parseSeqOptList() []*ast.Node {
	list := []*ast.Node{p.parseSeqOptElem()}
	for p.seqOptElemStarts() {
		list = append(list, p.parseSeqOptElem())
	}
	return list
}

// parseSeqOptElem is gram.y's SeqOptElem.
func (p *parser) parseSeqOptElem() *ast.Node {
	tok := p.peek()
	switch tok.Kind {
	case ast.Token_AS:
		p.next()
		return makeDefElem("as", nTypeName(p.parseSimpleTypename()), tok.Start)
	case ast.Token_CACHE:
		p.next()
		return makeDefElem("cache", p.parseNumericOnly(), tok.Start)
	case ast.Token_CYCLE:
		p.next()
		return makeDefElem("cycle", nBoolean(true), tok.Start)
	case ast.Token_NO:
		p.next()
		switch {
		case p.have(ast.Token_CYCLE):
			return makeDefElem("cycle", nBoolean(false), tok.Start)
		case p.have(ast.Token_MAXVALUE):
			return makeDefElem("maxvalue", nil, tok.Start)
		case p.have(ast.Token_MINVALUE):
			return makeDefElem("minvalue", nil, tok.Start)
		}
		p.syntaxErrorAt()
	case ast.Token_INCREMENT:
		p.next()
		p.have(ast.Token_BY)
		return makeDefElem("increment", p.parseNumericOnly(), tok.Start)
	case ast.Token_LOGGED:
		p.next()
		return makeDefElem("logged", nil, tok.Start)
	case ast.Token_MAXVALUE:
		p.next()
		return makeDefElem("maxvalue", p.parseNumericOnly(), tok.Start)
	case ast.Token_MINVALUE:
		p.next()
		return makeDefElem("minvalue", p.parseNumericOnly(), tok.Start)
	case ast.Token_OWNED:
		p.next()
		p.expect(ast.Token_BY)
		return makeDefElem("owned_by", nList(p.anyName()), tok.Start)
	case ast.Token_SEQUENCE:
		p.next()
		p.expect(ast.Token_NAME_P)
		return makeDefElem("sequence_name", nList(p.anyName()), tok.Start)
	case ast.Token_START:
		p.next()
		p.parseOptWith()
		return makeDefElem("start", p.parseNumericOnly(), tok.Start)
	case ast.Token_RESTART:
		p.next()
		switch p.kind() {
		case ast.Token_WITH, ast.Token_WITH_LA, ast.Token_ICONST,
			ast.Token_FCONST, ast.Token('+'), ast.Token('-'):
			p.parseOptWith()
			return makeDefElem("restart", p.parseNumericOnly(), tok.Start)
		}
		return makeDefElem("restart", nil, tok.Start)
	case ast.Token_UNLOGGED:
		p.next()
		return makeDefElem("unlogged", nil, tok.Start)
	}
	p.syntaxErrorAt()
	return nil
}

// parseViewStmt is gram.y's ViewStmt. CREATE [OR REPLACE] [OptTemp] is
// consumed; [RECURSIVE] VIEW is not.
func (p *parser) parseViewStmt(replace bool, persistence string) *ast.Node {
	n := &ast.ViewStmt{Replace: replace}
	recursive := p.have(ast.Token_RECURSIVE)
	p.expect(ast.Token_VIEW)
	n.View = p.parseQualifiedName()
	n.View.Relpersistence = persistence
	if recursive {
		p.expect(ast.Token('('))
		n.Aliases = p.columnList()
		p.expect(ast.Token(')'))
	} else if p.have(ast.Token('(')) {
		n.Aliases = p.columnList()
		p.expect(ast.Token(')'))
	}
	if p.kind() == ast.Token_WITH && p.kindN(1) == ast.Token('(') {
		p.next()
		n.Options = p.parseReloptions()
	}
	p.expect(ast.Token_AS)
	query := p.parseSelectStmt()
	// opt_check_option
	n.WithCheckOption = ast.ViewCheckOption_NO_CHECK_OPTION
	if p.kind() == ast.Token_WITH || p.kind() == ast.Token_WITH_LA {
		ctok := p.peekN(1)
		switch ctok.Kind {
		case ast.Token_CHECK:
			p.next()
			p.next()
			p.expect(ast.Token_OPTION)
			n.WithCheckOption = ast.ViewCheckOption_CASCADED_CHECK_OPTION
		case ast.Token_CASCADED:
			p.next()
			p.next()
			p.expect(ast.Token_CHECK)
			p.expect(ast.Token_OPTION)
			n.WithCheckOption = ast.ViewCheckOption_CASCADED_CHECK_OPTION
		case ast.Token_LOCAL:
			p.next()
			p.next()
			p.expect(ast.Token_CHECK)
			p.expect(ast.Token_OPTION)
			n.WithCheckOption = ast.ViewCheckOption_LOCAL_CHECK_OPTION
		}
	}
	if recursive {
		if n.WithCheckOption != ast.ViewCheckOption_NO_CHECK_OPTION {
			p.ereport("base_yyparse", "WITH CHECK OPTION not supported on recursive views", -1)
		}
		n.Query = makeRecursiveViewSelect(n.View.Relname, n.Aliases, query)
	} else {
		n.Query = query
	}
	return &ast.Node{Node: &ast.Node_ViewStmt{ViewStmt: n}}
}

// makeRecursiveViewSelect wraps a recursive view's query in
// WITH RECURSIVE name (aliases) AS (query) SELECT aliases FROM name,
// mirroring gram.y's support function of the same name.
func makeRecursiveViewSelect(relname string, aliases []*ast.Node, query *ast.Node) *ast.Node {
	cte := &ast.CommonTableExpr{
		Ctename:         relname,
		Aliascolnames:   aliases,
		Ctematerialized: ast.CTEMaterialize_CTEMaterializeDefault,
		Ctequery:        query,
		Location:        -1,
	}
	w := &ast.WithClause{
		Recursive: true,
		Ctes:      []*ast.Node{nCommonTableExpr(cte)},
		Location:  -1,
	}
	var tl []*ast.Node
	for _, a := range aliases {
		tl = append(tl, nResTarget(&ast.ResTarget{
			Val: nColumnRef(&ast.ColumnRef{
				Fields:   []*ast.Node{nStr(asString(a).Sval)},
				Location: -1,
			}),
			Location: -1,
		}))
	}
	s := &ast.SelectStmt{
		WithClause:  w,
		TargetList:  tl,
		FromClause:  []*ast.Node{nRangeVar(makeRangeVar("", relname, -1))},
		LimitOption: ast.LimitOption_LIMIT_OPTION_DEFAULT,
		Op:          ast.SetOperation_SETOP_NONE,
	}
	return nSelectStmt(s)
}

// parseIndexStmt is gram.y's IndexStmt. CREATE is consumed; [UNIQUE] INDEX
// is not.
func (p *parser) parseIndexStmt() *ast.Node {
	n := &ast.IndexStmt{}
	n.Unique = p.have(ast.Token_UNIQUE)
	p.expect(ast.Token_INDEX)
	n.Concurrent = p.have(ast.Token_CONCURRENTLY)
	if p.have(ast.Token_IF_P) {
		p.expect(ast.Token_NOT)
		p.expect(ast.Token_EXISTS)
		n.IfNotExists = true
		n.Idxname = p.name()
	} else if p.kind() != ast.Token_ON {
		n.Idxname = p.name()
	}
	p.expect(ast.Token_ON)
	rel := p.parseRelationExpr()
	n.Relation = rel.GetRangeVar()
	if p.have(ast.Token_USING) {
		n.AccessMethod = p.name()
	} else {
		n.AccessMethod = "btree" // DEFAULT_INDEX_TYPE
	}
	p.expect(ast.Token('('))
	n.IndexParams = p.parseIndexParams()
	p.expect(ast.Token(')'))
	if p.have(ast.Token_INCLUDE) {
		p.expect(ast.Token('('))
		n.IndexIncludingParams = p.parseIndexParams()
		p.expect(ast.Token(')'))
	}
	n.NullsNotDistinct = !p.parseOptUniqueNullTreatment()
	if p.kind() == ast.Token_WITH && p.kindN(1) == ast.Token('(') {
		p.next()
		n.Options = p.parseReloptions()
	}
	if p.have(ast.Token_TABLESPACE) {
		n.TableSpace = p.name()
	}
	if p.have(ast.Token_WHERE) {
		n.WhereClause = p.parseAExpr(0)
	}
	return &ast.Node{Node: &ast.Node_IndexStmt{IndexStmt: n}}
}

// parseCreateMatViewStmt is gram.y's CreateMatViewStmt. CREATE [UNLOGGED]
// is consumed; MATERIALIZED VIEW is not.
func (p *parser) parseCreateMatViewStmt(persistence string) *ast.Node {
	p.expect(ast.Token_MATERIALIZED)
	p.expect(ast.Token_VIEW)
	ctas := &ast.CreateTableAsStmt{
		Objtype: ast.ObjectType_OBJECT_MATVIEW,
	}
	if p.have(ast.Token_IF_P) {
		p.expect(ast.Token_NOT)
		p.expect(ast.Token_EXISTS)
		ctas.IfNotExists = true
	}
	// create_mv_target: qualified_name opt_column_list
	// table_access_method_clause opt_reloptions OptTableSpace
	rel := p.parseQualifiedName()
	rel.Relpersistence = persistence
	into := &ast.IntoClause{Rel: rel, OnCommit: ast.OnCommitAction_ONCOMMIT_NOOP}
	if p.have(ast.Token('(')) {
		into.ColNames = p.columnList()
		p.expect(ast.Token(')'))
	}
	if p.have(ast.Token_USING) {
		into.AccessMethod = p.name()
	}
	if p.kind() == ast.Token_WITH && p.kindN(1) == ast.Token('(') {
		p.next()
		into.Options = p.parseReloptions()
	}
	if p.have(ast.Token_TABLESPACE) {
		into.TableSpaceName = p.name()
	}
	ctas.Into = into
	p.expect(ast.Token_AS)
	ctas.Query = p.parseSelectStmt()
	into.SkipData = !p.parseOptWithData()
	return &ast.Node{Node: &ast.Node_CreateTableAsStmt{CreateTableAsStmt: ctas}}
}

// parseRefreshMatViewStmt is gram.y's RefreshMatViewStmt.
func (p *parser) parseRefreshMatViewStmt() *ast.Node {
	p.expect(ast.Token_REFRESH)
	p.expect(ast.Token_MATERIALIZED)
	p.expect(ast.Token_VIEW)
	n := &ast.RefreshMatViewStmt{}
	n.Concurrent = p.have(ast.Token_CONCURRENTLY)
	n.Relation = p.parseQualifiedName()
	n.SkipData = !p.parseOptWithData()
	return &ast.Node{Node: &ast.Node_RefreshMatViewStmt{RefreshMatViewStmt: n}}
}
