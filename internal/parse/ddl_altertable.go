package parse

// AlterTableStmt (ALTER TABLE/INDEX/SEQUENCE/VIEW/MATERIALIZED VIEW/FOREIGN
// TABLE), AlterTableMoveAllStmt, AlterCompositeTypeStmt, and the
// alter_table_cmd machinery.

import (
	"fmt"

	"github.com/sqlc-dev/oliphant/ast"
)

func nAlterTableCmd(n *ast.AlterTableCmd) *ast.Node {
	return &ast.Node{Node: &ast.Node_AlterTableCmd{AlterTableCmd: n}}
}

func newAlterTableCmd(subtype ast.AlterTableType) *ast.AlterTableCmd {
	// makeNode zeroes the C struct; behavior 0 is DROP_RESTRICT.
	return &ast.AlterTableCmd{Subtype: subtype, Behavior: ast.DropBehavior_DROP_RESTRICT}
}

// parseAlterTableStmt parses the AlterTableStmt family. ALTER and the
// object-kind keywords (TABLE / INDEX / SEQUENCE / VIEW / MATERIALIZED
// VIEW / FOREIGN TABLE) are consumed by the dispatcher, which passes the
// objtype; relExpr says whether the target uses relation_expr (TABLE,
// FOREIGN TABLE) or qualified_name.
func (p *parser) parseAlterTableStmt(objtype ast.ObjectType, relExpr, allowMoveAll bool) *ast.Node {
	// ALTER TABLE/INDEX/MATERIALIZED VIEW ALL IN TABLESPACE ...
	if allowMoveAll && p.kind() == ast.Token_ALL {
		p.next()
		p.expect(ast.Token_IN_P)
		p.expect(ast.Token_TABLESPACE)
		n := &ast.AlterTableMoveAllStmt{Objtype: objtype}
		n.OrigTablespacename = p.name()
		if p.have(ast.Token_OWNED) {
			p.expect(ast.Token_BY)
			n.Roles = p.parseRoleList()
		}
		p.expect(ast.Token_SET)
		p.expect(ast.Token_TABLESPACE)
		n.NewTablespacename = p.name()
		n.Nowait = p.have(ast.Token_NOWAIT)
		return &ast.Node{Node: &ast.Node_AlterTableMoveAllStmt{AlterTableMoveAllStmt: n}}
	}

	n := &ast.AlterTableStmt{Objtype: objtype}
	if p.have(ast.Token_IF_P) {
		p.expect(ast.Token_EXISTS)
		n.MissingOk = true
	}
	if relExpr {
		n.Relation = p.parseRelationExpr().GetRangeVar()
	} else {
		n.Relation = p.parseQualifiedName()
	}

	// The RENAME / SET SCHEMA / DEPENDS ON EXTENSION productions share this
	// prefix (RenameStmt, AlterObjectSchemaStmt, AlterObjectDependsStmt).
	switch p.kind() {
	case ast.Token_RENAME:
		p.next()
		r := newRenameStmt(objtype)
		r.Relation = n.Relation
		r.MissingOk = n.MissingOk
		switch {
		case p.have(ast.Token_TO):
			r.Newname = p.name()
		case objtype == ast.ObjectType_OBJECT_TABLE && p.kind() == ast.Token_CONSTRAINT:
			p.next()
			r.RenameType = ast.ObjectType_OBJECT_TABCONSTRAINT
			r.Subname = p.name()
			p.expect(ast.Token_TO)
			r.Newname = p.name()
		default:
			// RENAME [COLUMN] name TO name (not available for INDEX or
			// SEQUENCE).
			if objtype == ast.ObjectType_OBJECT_INDEX ||
				objtype == ast.ObjectType_OBJECT_SEQUENCE {
				p.syntaxErrorAt()
			}
			p.have(ast.Token_COLUMN)
			r.RenameType = ast.ObjectType_OBJECT_COLUMN
			r.RelationType = objtype
			r.Subname = p.name()
			p.expect(ast.Token_TO)
			r.Newname = p.name()
		}
		return nRenameStmt(r)
	case ast.Token_SET:
		if p.kindN(1) == ast.Token_SCHEMA && objtype != ast.ObjectType_OBJECT_INDEX {
			p.next()
			p.next()
			return nAlterObjectSchemaStmt(&ast.AlterObjectSchemaStmt{
				ObjectType: objtype,
				Relation:   n.Relation,
				MissingOk:  n.MissingOk,
				Newschema:  p.name(),
			})
		}
	case ast.Token_DEPENDS, ast.Token_NO:
		if objtype == ast.ObjectType_OBJECT_INDEX || objtype == ast.ObjectType_OBJECT_MATVIEW {
			if d := p.parseAlterGenericTail(objtype, nil, n.Relation, false, tailDepends); d != nil {
				return d
			}
		}
	}
	n.Cmds = p.parseAlterTableCmds()
	return &ast.Node{Node: &ast.Node_AlterTableStmt{AlterTableStmt: n}}
}

// parseAlterTableCmds is gram.y's alter_table_cmds, plus the partition_cmd
// and index_partition_cmd singletons (ATTACH/DETACH PARTITION).
func (p *parser) parseAlterTableCmds() []*ast.Node {
	list := []*ast.Node{p.parseAlterTableCmd()}
	for p.have(ast.Token(',')) {
		list = append(list, p.parseAlterTableCmd())
	}
	return list
}

// parseAlterTableCmd is gram.y's alter_table_cmd / partition_cmd /
// index_partition_cmd.
func (p *parser) parseAlterTableCmd() *ast.Node {
	tok := p.peek()
	switch tok.Kind {
	case ast.Token_ATTACH:
		// partition_cmd: ATTACH PARTITION qualified_name [PartitionBoundSpec]
		p.next()
		p.expect(ast.Token_PARTITION)
		n := newAlterTableCmd(ast.AlterTableType_AT_AttachPartition)
		cmd := &ast.PartitionCmd{Name: p.parseQualifiedName()}
		switch p.kind() {
		case ast.Token_FOR, ast.Token_DEFAULT:
			cmd.Bound = p.parsePartitionBoundSpec()
		}
		n.Def = &ast.Node{Node: &ast.Node_PartitionCmd{PartitionCmd: cmd}}
		return nAlterTableCmd(n)

	case ast.Token_DETACH:
		p.next()
		p.expect(ast.Token_PARTITION)
		cmd := &ast.PartitionCmd{Name: p.parseQualifiedName()}
		subtype := ast.AlterTableType_AT_DetachPartition
		if p.have(ast.Token_FINALIZE) {
			subtype = ast.AlterTableType_AT_DetachPartitionFinalize
		} else {
			cmd.Concurrent = p.have(ast.Token_CONCURRENTLY)
		}
		n := newAlterTableCmd(subtype)
		n.Def = &ast.Node{Node: &ast.Node_PartitionCmd{PartitionCmd: cmd}}
		return nAlterTableCmd(n)

	case ast.Token_ADD_P:
		p.next()
		switch p.kind() {
		case ast.Token_CONSTRAINT, ast.Token_CHECK, ast.Token_UNIQUE,
			ast.Token_PRIMARY, ast.Token_EXCLUDE, ast.Token_FOREIGN:
			n := newAlterTableCmd(ast.AlterTableType_AT_AddConstraint)
			n.Def = p.parseTableConstraint()
			return nAlterTableCmd(n)
		case ast.Token_COLUMN:
			p.next()
		}
		n := newAlterTableCmd(ast.AlterTableType_AT_AddColumn)
		if p.have(ast.Token_IF_P) {
			p.expect(ast.Token_NOT)
			p.expect(ast.Token_EXISTS)
			n.MissingOk = true
		}
		n.Def = p.parseColumnDef()
		return nAlterTableCmd(n)

	case ast.Token_ALTER:
		p.next()
		if p.kind() == ast.Token_CONSTRAINT {
			// ALTER CONSTRAINT name ConstraintAttributeSpec
			p.next()
			n := newAlterTableCmd(ast.AlterTableType_AT_AlterConstraint)
			c := &ast.Constraint{
				Contype: ast.ConstrType_CONSTR_FOREIGN,
				Conname: p.name(),
			}
			cas, casLoc := p.parseConstraintAttributeSpec()
			p.processCASbits(cas, casLoc, "FOREIGN KEY", &c.Deferrable, &c.Initdeferred, nil, nil)
			n.Def = nConstraint(c)
			return nAlterTableCmd(n)
		}
		p.have(ast.Token_COLUMN)
		if p.kind() == ast.Token_ICONST {
			// ALTER opt_column Iconst SET STATISTICS set_statistics_value
			itok := p.peek()
			num := p.iconst()
			p.expect(ast.Token_SET)
			p.expect(ast.Token_STATISTICS)
			if num <= 0 || num > 32767 {
				p.ereport("base_yyparse", "column number must be in range from 1 to 32767", itok.Start)
			}
			n := newAlterTableCmd(ast.AlterTableType_AT_SetStatistics)
			n.Num = num
			n.Def = p.parseSetStatisticsValue()
			return nAlterTableCmd(n)
		}
		return p.parseAlterColumnCmd()

	case ast.Token_DROP:
		p.next()
		switch p.kind() {
		case ast.Token_CONSTRAINT:
			p.next()
			n := newAlterTableCmd(ast.AlterTableType_AT_DropConstraint)
			if p.have(ast.Token_IF_P) {
				p.expect(ast.Token_EXISTS)
				n.MissingOk = true
			}
			n.Name = p.name()
			n.Behavior = p.parseOptDropBehavior()
			return nAlterTableCmd(n)
		case ast.Token_COLUMN:
			p.next()
		}
		n := newAlterTableCmd(ast.AlterTableType_AT_DropColumn)
		if p.have(ast.Token_IF_P) {
			p.expect(ast.Token_EXISTS)
			n.MissingOk = true
		}
		n.Name = p.colId()
		n.Behavior = p.parseOptDropBehavior()
		return nAlterTableCmd(n)

	case ast.Token_VALIDATE:
		p.next()
		p.expect(ast.Token_CONSTRAINT)
		n := newAlterTableCmd(ast.AlterTableType_AT_ValidateConstraint)
		n.Name = p.name()
		return nAlterTableCmd(n)

	case ast.Token_SET:
		p.next()
		switch p.kind() {
		case ast.Token_WITHOUT:
			p.next()
			switch {
			case p.have(ast.Token_OIDS):
				return nAlterTableCmd(newAlterTableCmd(ast.AlterTableType_AT_DropOids))
			case p.have(ast.Token_CLUSTER):
				return nAlterTableCmd(newAlterTableCmd(ast.AlterTableType_AT_DropCluster))
			}
			p.syntaxErrorAt()
		case ast.Token_LOGGED:
			p.next()
			return nAlterTableCmd(newAlterTableCmd(ast.AlterTableType_AT_SetLogged))
		case ast.Token_UNLOGGED:
			p.next()
			return nAlterTableCmd(newAlterTableCmd(ast.AlterTableType_AT_SetUnLogged))
		case ast.Token_ACCESS:
			p.next()
			p.expect(ast.Token_METHOD)
			n := newAlterTableCmd(ast.AlterTableType_AT_SetAccessMethod)
			if !p.have(ast.Token_DEFAULT) {
				n.Name = p.colId()
			}
			return nAlterTableCmd(n)
		case ast.Token_TABLESPACE:
			p.next()
			n := newAlterTableCmd(ast.AlterTableType_AT_SetTableSpace)
			n.Name = p.name()
			return nAlterTableCmd(n)
		case ast.Token('('):
			n := newAlterTableCmd(ast.AlterTableType_AT_SetRelOptions)
			n.Def = nList(p.parseReloptions())
			return nAlterTableCmd(n)
		}
		p.syntaxErrorAt()

	case ast.Token_RESET:
		p.next()
		n := newAlterTableCmd(ast.AlterTableType_AT_ResetRelOptions)
		n.Def = nList(p.parseReloptions())
		return nAlterTableCmd(n)

	case ast.Token_REPLICA:
		p.next()
		p.expect(ast.Token_IDENTITY_P)
		n := newAlterTableCmd(ast.AlterTableType_AT_ReplicaIdentity)
		r := &ast.ReplicaIdentityStmt{}
		switch {
		case p.have(ast.Token_NOTHING):
			r.IdentityType = "n"
		case p.have(ast.Token_FULL):
			r.IdentityType = "f"
		case p.have(ast.Token_DEFAULT):
			r.IdentityType = "d"
		case p.have(ast.Token_USING):
			p.expect(ast.Token_INDEX)
			r.IdentityType = "i"
			r.Name = p.name()
		default:
			p.syntaxErrorAt()
		}
		n.Def = &ast.Node{Node: &ast.Node_ReplicaIdentityStmt{ReplicaIdentityStmt: r}}
		return nAlterTableCmd(n)

	case ast.Token_ENABLE_P:
		p.next()
		switch {
		case p.have(ast.Token_TRIGGER):
			switch {
			case p.have(ast.Token_ALL):
				return nAlterTableCmd(newAlterTableCmd(ast.AlterTableType_AT_EnableTrigAll))
			case p.have(ast.Token_USER):
				return nAlterTableCmd(newAlterTableCmd(ast.AlterTableType_AT_EnableTrigUser))
			}
			n := newAlterTableCmd(ast.AlterTableType_AT_EnableTrig)
			n.Name = p.name()
			return nAlterTableCmd(n)
		case p.have(ast.Token_ALWAYS):
			switch {
			case p.have(ast.Token_TRIGGER):
				n := newAlterTableCmd(ast.AlterTableType_AT_EnableAlwaysTrig)
				n.Name = p.name()
				return nAlterTableCmd(n)
			case p.have(ast.Token_RULE):
				n := newAlterTableCmd(ast.AlterTableType_AT_EnableAlwaysRule)
				n.Name = p.name()
				return nAlterTableCmd(n)
			}
			p.syntaxErrorAt()
		case p.have(ast.Token_REPLICA):
			switch {
			case p.have(ast.Token_TRIGGER):
				n := newAlterTableCmd(ast.AlterTableType_AT_EnableReplicaTrig)
				n.Name = p.name()
				return nAlterTableCmd(n)
			case p.have(ast.Token_RULE):
				n := newAlterTableCmd(ast.AlterTableType_AT_EnableReplicaRule)
				n.Name = p.name()
				return nAlterTableCmd(n)
			}
			p.syntaxErrorAt()
		case p.have(ast.Token_RULE):
			n := newAlterTableCmd(ast.AlterTableType_AT_EnableRule)
			n.Name = p.name()
			return nAlterTableCmd(n)
		case p.have(ast.Token_ROW):
			p.expect(ast.Token_LEVEL)
			p.expect(ast.Token_SECURITY)
			return nAlterTableCmd(newAlterTableCmd(ast.AlterTableType_AT_EnableRowSecurity))
		}
		p.syntaxErrorAt()

	case ast.Token_DISABLE_P:
		p.next()
		switch {
		case p.have(ast.Token_TRIGGER):
			switch {
			case p.have(ast.Token_ALL):
				return nAlterTableCmd(newAlterTableCmd(ast.AlterTableType_AT_DisableTrigAll))
			case p.have(ast.Token_USER):
				return nAlterTableCmd(newAlterTableCmd(ast.AlterTableType_AT_DisableTrigUser))
			}
			n := newAlterTableCmd(ast.AlterTableType_AT_DisableTrig)
			n.Name = p.name()
			return nAlterTableCmd(n)
		case p.have(ast.Token_RULE):
			n := newAlterTableCmd(ast.AlterTableType_AT_DisableRule)
			n.Name = p.name()
			return nAlterTableCmd(n)
		case p.have(ast.Token_ROW):
			p.expect(ast.Token_LEVEL)
			p.expect(ast.Token_SECURITY)
			return nAlterTableCmd(newAlterTableCmd(ast.AlterTableType_AT_DisableRowSecurity))
		}
		p.syntaxErrorAt()

	case ast.Token_FORCE:
		p.next()
		p.expect(ast.Token_ROW)
		p.expect(ast.Token_LEVEL)
		p.expect(ast.Token_SECURITY)
		return nAlterTableCmd(newAlterTableCmd(ast.AlterTableType_AT_ForceRowSecurity))

	case ast.Token_NO:
		p.next()
		switch {
		case p.have(ast.Token_INHERIT):
			n := newAlterTableCmd(ast.AlterTableType_AT_DropInherit)
			n.Def = nRangeVar(p.parseQualifiedName())
			return nAlterTableCmd(n)
		case p.have(ast.Token_FORCE):
			p.expect(ast.Token_ROW)
			p.expect(ast.Token_LEVEL)
			p.expect(ast.Token_SECURITY)
			return nAlterTableCmd(newAlterTableCmd(ast.AlterTableType_AT_NoForceRowSecurity))
		}
		p.syntaxErrorAt()

	case ast.Token_INHERIT:
		p.next()
		n := newAlterTableCmd(ast.AlterTableType_AT_AddInherit)
		n.Def = nRangeVar(p.parseQualifiedName())
		return nAlterTableCmd(n)

	case ast.Token_OF:
		p.next()
		ntok := p.peek()
		def := makeTypeNameFromNameList(p.anyName())
		def.Location = ntok.Start
		n := newAlterTableCmd(ast.AlterTableType_AT_AddOf)
		n.Def = nTypeName(def)
		return nAlterTableCmd(n)

	case ast.Token_NOT:
		p.next()
		p.expect(ast.Token_OF)
		return nAlterTableCmd(newAlterTableCmd(ast.AlterTableType_AT_DropOf))

	case ast.Token_OWNER:
		p.next()
		p.expect(ast.Token_TO)
		n := newAlterTableCmd(ast.AlterTableType_AT_ChangeOwner)
		n.Newowner = p.parseRoleSpec()
		return nAlterTableCmd(n)

	case ast.Token_CLUSTER:
		p.next()
		p.expect(ast.Token_ON)
		n := newAlterTableCmd(ast.AlterTableType_AT_ClusterOn)
		n.Name = p.name()
		return nAlterTableCmd(n)

	case ast.Token_OPTIONS:
		n := newAlterTableCmd(ast.AlterTableType_AT_GenericOptions)
		n.Def = nList(p.parseAlterGenericOptions())
		return nAlterTableCmd(n)
	}
	p.syntaxErrorAt()
	return nil
}

// parseSetStatisticsValue is gram.y's set_statistics_value.
func (p *parser) parseSetStatisticsValue() *ast.Node {
	if p.have(ast.Token_DEFAULT) {
		return nil
	}
	return nInteger(p.parseSignedIconst())
}

// parseAlterColumnCmd handles the ALTER [COLUMN] ColId ... alternatives of
// alter_table_cmd; ALTER opt_column are consumed.
func (p *parser) parseAlterColumnCmd() *ast.Node {
	ctok := p.peek()
	colname := p.colId()
	switch p.kind() {
	case ast.Token_SET:
		p.next()
		switch p.kind() {
		case ast.Token_DEFAULT:
			p.next()
			n := newAlterTableCmd(ast.AlterTableType_AT_ColumnDefault)
			n.Name = colname
			n.Def = p.parseAExpr(0)
			return nAlterTableCmd(n)
		case ast.Token_NOT:
			p.next()
			p.expect(ast.Token_NULL_P)
			n := newAlterTableCmd(ast.AlterTableType_AT_SetNotNull)
			n.Name = colname
			return nAlterTableCmd(n)
		case ast.Token_EXPRESSION:
			p.next()
			p.expect(ast.Token_AS)
			p.expect(ast.Token('('))
			n := newAlterTableCmd(ast.AlterTableType_AT_SetExpression)
			n.Name = colname
			n.Def = p.parseAExpr(0)
			p.expect(ast.Token(')'))
			return nAlterTableCmd(n)
		case ast.Token_STATISTICS:
			p.next()
			n := newAlterTableCmd(ast.AlterTableType_AT_SetStatistics)
			n.Name = colname
			n.Def = p.parseSetStatisticsValue()
			return nAlterTableCmd(n)
		case ast.Token('('):
			n := newAlterTableCmd(ast.AlterTableType_AT_SetOptions)
			n.Name = colname
			n.Def = nList(p.parseReloptions())
			return nAlterTableCmd(n)
		case ast.Token_STORAGE:
			p.next()
			n := newAlterTableCmd(ast.AlterTableType_AT_SetStorage)
			n.Name = colname
			n.Def = nStr(p.parseColIdOrDefault())
			return nAlterTableCmd(n)
		case ast.Token_COMPRESSION:
			p.next()
			n := newAlterTableCmd(ast.AlterTableType_AT_SetCompression)
			n.Name = colname
			n.Def = nStr(p.parseColIdOrDefault())
			return nAlterTableCmd(n)
		case ast.Token_DATA_P:
			p.next()
			p.expect(ast.Token_TYPE_P)
			return p.parseAlterColumnType(colname, ctok.Start)
		case ast.Token_GENERATED:
			// alter_identity_column_option: SET GENERATED generated_when
			return p.parseAlterIdentityOptions(colname, true)
		default:
			// alter_identity_column_option: SET SeqOptElem
			if p.seqOptElemStarts() || p.kind() == ast.Token_AS {
				return p.parseAlterIdentityOptions(colname, true)
			}
		}
		p.syntaxErrorAt()
	case ast.Token_DROP:
		p.next()
		switch {
		case p.have(ast.Token_NOT):
			p.expect(ast.Token_NULL_P)
			n := newAlterTableCmd(ast.AlterTableType_AT_DropNotNull)
			n.Name = colname
			return nAlterTableCmd(n)
		case p.have(ast.Token_DEFAULT):
			n := newAlterTableCmd(ast.AlterTableType_AT_ColumnDefault)
			n.Name = colname
			return nAlterTableCmd(n)
		case p.have(ast.Token_EXPRESSION):
			n := newAlterTableCmd(ast.AlterTableType_AT_DropExpression)
			n.Name = colname
			if p.have(ast.Token_IF_P) {
				p.expect(ast.Token_EXISTS)
				n.MissingOk = true
			}
			return nAlterTableCmd(n)
		case p.have(ast.Token_IDENTITY_P):
			n := newAlterTableCmd(ast.AlterTableType_AT_DropIdentity)
			n.Name = colname
			if p.have(ast.Token_IF_P) {
				p.expect(ast.Token_EXISTS)
				n.MissingOk = true
			}
			return nAlterTableCmd(n)
		}
		p.syntaxErrorAt()
	case ast.Token_TYPE_P:
		p.next()
		return p.parseAlterColumnType(colname, ctok.Start)
	case ast.Token_ADD_P:
		// ALTER col ADD GENERATED generated_when AS IDENTITY [(seqopts)]
		p.next()
		gtok := p.expect(ast.Token_GENERATED)
		c := &ast.Constraint{
			Contype:  ast.ConstrType_CONSTR_IDENTITY,
			Location: gtok.Start,
		}
		c.GeneratedWhen = p.parseGeneratedWhen()
		p.expect(ast.Token_AS)
		p.expect(ast.Token_IDENTITY_P)
		if p.have(ast.Token('(')) {
			c.Options = p.parseSeqOptList()
			p.expect(ast.Token(')'))
		}
		n := newAlterTableCmd(ast.AlterTableType_AT_AddIdentity)
		n.Name = colname
		n.Def = nConstraint(c)
		return nAlterTableCmd(n)
	case ast.Token_RESTART:
		return p.parseAlterIdentityOptions(colname, false)
	case ast.Token_OPTIONS:
		n := newAlterTableCmd(ast.AlterTableType_AT_AlterColumnGenericOptions)
		n.Name = colname
		n.Def = nList(p.parseAlterGenericOptions())
		return nAlterTableCmd(n)
	}
	p.syntaxErrorAt()
	return nil
}

// parseAlterColumnType finishes ALTER [COLUMN] col [SET DATA] TYPE:
//
//	Typename opt_collate_clause alter_using
func (p *parser) parseAlterColumnType(colname string, colLoc int32) *ast.Node {
	n := newAlterTableCmd(ast.AlterTableType_AT_AlterColumnType)
	n.Name = colname
	def := &ast.ColumnDef{Location: colLoc}
	def.TypeName = p.parseTypename()
	if p.kind() == ast.Token_COLLATE {
		ctok := p.next()
		def.CollClause = &ast.CollateClause{
			Collname: p.anyName(),
			Location: ctok.Start,
		}
	}
	if p.have(ast.Token_USING) {
		def.RawDefault = p.parseAExpr(0)
	}
	n.Def = nColumnDef(def)
	return nAlterTableCmd(n)
}

// parseAlterIdentityOptions is gram.y's
// alter_identity_column_option_list; firstIsSet marks that the caller's
// lookahead sits after a consumed SET.
func (p *parser) parseAlterIdentityOptions(colname string, firstIsSet bool) *ast.Node {
	n := newAlterTableCmd(ast.AlterTableType_AT_SetIdentity)
	n.Name = colname
	var opts []*ast.Node
	first := true
	for {
		var setConsumed bool
		if first {
			setConsumed = firstIsSet
			first = false
		} else {
			switch p.kind() {
			case ast.Token_RESTART:
			case ast.Token_SET:
				p.next()
				setConsumed = true
			default:
				n.Def = nList(opts)
				return nAlterTableCmd(n)
			}
		}
		if !setConsumed {
			if p.kind() != ast.Token_RESTART {
				n.Def = nList(opts)
				return nAlterTableCmd(n)
			}
			rtok := p.next()
			switch p.kind() {
			case ast.Token_WITH, ast.Token_WITH_LA, ast.Token_ICONST,
				ast.Token_FCONST, ast.Token('+'), ast.Token('-'):
				p.parseOptWith()
				opts = append(opts, makeDefElem("restart", p.parseNumericOnly(), rtok.Start))
			default:
				opts = append(opts, makeDefElem("restart", nil, rtok.Start))
			}
			continue
		}
		// SET GENERATED generated_when | SET SeqOptElem
		if p.kind() == ast.Token_GENERATED {
			stok := p.toks[p.pos-1]
			p.next()
			when := p.parseGeneratedWhen()
			ival := int32('a')
			if when == attributeIdentityByDefault {
				ival = 'd'
			}
			opts = append(opts, makeDefElem("generated", nInteger(ival), stok.Start))
			continue
		}
		etok := p.peek()
		el := p.parseSeqOptElem()
		name := el.GetDefElem().GetDefname()
		if name == "as" || name == "restart" || name == "owned_by" {
			p.ereport("base_yyparse",
				fmt.Sprintf("sequence option %q not supported here", name), etok.Start)
		}
		opts = append(opts, el)
	}
}

// parseAlterGenericOptions is gram.y's alter_generic_options:
//
//	OPTIONS '(' alter_generic_option_list ')'
func (p *parser) parseAlterGenericOptions() []*ast.Node {
	p.expect(ast.Token_OPTIONS)
	p.expect(ast.Token('('))
	list := []*ast.Node{p.parseAlterGenericOptionElem()}
	for p.have(ast.Token(',')) {
		list = append(list, p.parseAlterGenericOptionElem())
	}
	p.expect(ast.Token(')'))
	return list
}

// parseAlterGenericOptionElem is gram.y's alter_generic_option_elem.
func (p *parser) parseAlterGenericOptionElem() *ast.Node {
	switch p.kind() {
	case ast.Token_SET:
		p.next()
		el := p.parseGenericOptionElem()
		el.GetDefElem().Defaction = ast.DefElemAction_DEFELEM_SET
		return el
	case ast.Token_ADD_P:
		p.next()
		el := p.parseGenericOptionElem()
		el.GetDefElem().Defaction = ast.DefElemAction_DEFELEM_ADD
		return el
	case ast.Token_DROP:
		p.next()
		ntok := p.peek()
		name := p.colLabel()
		return makeDefElemExtended("", name, nil, ast.DefElemAction_DEFELEM_DROP, ntok.Start)
	}
	return p.parseGenericOptionElem()
}

// parseAlterTypeDispatch routes ALTER TYPE_P any_name ...; the composite
// AlterCompositeTypeStmt lives here, with the rest of the ALTER TYPE
// family (enum, owner, rename, schema, generic) added in milestone 7.
func (p *parser) parseAlterTypeDispatch() *ast.Node {
	ntok := p.peek()
	names := p.anyName()
	switch p.kind() {
	case ast.Token_ADD_P, ast.Token_DROP, ast.Token_ALTER:
		if p.kindN(1) == ast.Token_ATTRIBUTE {
			// gram.y: AlterCompositeTypeStmt: ALTER TYPE_P any_name
			// alter_type_cmds
			n := &ast.AlterTableStmt{
				Objtype:  ast.ObjectType_OBJECT_TYPE,
				Relation: p.makeRangeVarFromAnyName(names, ntok.Start),
			}
			n.Cmds = []*ast.Node{p.parseAlterTypeCmd()}
			for p.have(ast.Token(',')) {
				n.Cmds = append(n.Cmds, p.parseAlterTypeCmd())
			}
			return &ast.Node{Node: &ast.Node_AlterTableStmt{AlterTableStmt: n}}
		}
		if p.kind() == ast.Token_ADD_P && p.kindN(1) == ast.Token_VALUE_P {
			return p.parseAlterEnumStmt(names)
		}
		if p.kind() == ast.Token_DROP && p.kindN(1) == ast.Token_VALUE_P {
			// gram.y: AlterEnumStmt's DROP VALUE arm always errors.
			dtok := p.next()
			p.next()
			p.sconst()
			p.ereport("base_yyparse", "dropping an enum value is not implemented", dtok.Start)
		}
	case ast.Token_RENAME:
		switch p.kindN(1) {
		case ast.Token_ATTRIBUTE:
			// RenameStmt: ALTER TYPE_P any_name RENAME ATTRIBUTE ...
			p.next()
			p.next()
			r := newRenameStmt(ast.ObjectType_OBJECT_ATTRIBUTE)
			r.RelationType = ast.ObjectType_OBJECT_TYPE
			r.Relation = p.makeRangeVarFromAnyName(names, ntok.Start)
			r.Subname = p.name()
			p.expect(ast.Token_TO)
			r.Newname = p.name()
			r.Behavior = p.parseOptDropBehavior()
			return nRenameStmt(r)
		case ast.Token_VALUE_P:
			return p.parseAlterEnumStmt(names)
		}
	case ast.Token_SET:
		if p.kindN(1) == ast.Token('(') {
			// gram.y: AlterTypeStmt: ALTER TYPE_P any_name SET '(' ... ')'
			p.next()
			p.next()
			n := &ast.AlterTypeStmt{TypeName: names}
			n.Options = p.parseOperatorDefList()
			p.expect(ast.Token(')'))
			return &ast.Node{Node: &ast.Node_AlterTypeStmt{AlterTypeStmt: n}}
		}
	}
	if n := p.parseAlterGenericTail(ast.ObjectType_OBJECT_TYPE, nList(names), nil, false,
		tailRename|tailSchema|tailOwner); n != nil {
		return n
	}
	p.syntaxErrorAt()
	return nil
}

// parseAlterEnumStmt is gram.y's AlterEnumStmt; ALTER TYPE_P any_name
// consumed, lookahead at ADD VALUE or RENAME VALUE.
func (p *parser) parseAlterEnumStmt(names []*ast.Node) *ast.Node {
	n := &ast.AlterEnumStmt{TypeName: names}
	if p.have(ast.Token_ADD_P) {
		p.expect(ast.Token_VALUE_P)
		if p.have(ast.Token_IF_P) {
			p.expect(ast.Token_NOT)
			p.expect(ast.Token_EXISTS)
			n.SkipIfNewValExists = true
		}
		n.NewVal = p.sconst()
		n.NewValIsAfter = true
		switch {
		case p.have(ast.Token_BEFORE):
			n.NewValNeighbor = p.sconst()
			n.NewValIsAfter = false
		case p.have(ast.Token_AFTER):
			n.NewValNeighbor = p.sconst()
		}
		return &ast.Node{Node: &ast.Node_AlterEnumStmt{AlterEnumStmt: n}}
	}
	p.expect(ast.Token_RENAME)
	p.expect(ast.Token_VALUE_P)
	n.OldVal = p.sconst()
	p.expect(ast.Token_TO)
	n.NewVal = p.sconst()
	return &ast.Node{Node: &ast.Node_AlterEnumStmt{AlterEnumStmt: n}}
}

// parseAlterTypeCmd is gram.y's alter_type_cmd.
func (p *parser) parseAlterTypeCmd() *ast.Node {
	switch p.kind() {
	case ast.Token_ADD_P:
		p.next()
		p.expect(ast.Token_ATTRIBUTE)
		n := newAlterTableCmd(ast.AlterTableType_AT_AddColumn)
		n.Def = p.parseTableFuncElement()
		n.Behavior = p.parseOptDropBehavior()
		return nAlterTableCmd(n)
	case ast.Token_DROP:
		p.next()
		p.expect(ast.Token_ATTRIBUTE)
		n := newAlterTableCmd(ast.AlterTableType_AT_DropColumn)
		if p.have(ast.Token_IF_P) {
			p.expect(ast.Token_EXISTS)
			n.MissingOk = true
		}
		n.Name = p.colId()
		n.Behavior = p.parseOptDropBehavior()
		return nAlterTableCmd(n)
	case ast.Token_ALTER:
		p.next()
		p.expect(ast.Token_ATTRIBUTE)
		ctok := p.peek()
		colname := p.colId()
		if p.have(ast.Token_SET) {
			p.expect(ast.Token_DATA_P)
		}
		p.expect(ast.Token_TYPE_P)
		cmd := p.parseAlterColumnType(colname, ctok.Start)
		cmd.GetAlterTableCmd().Behavior = p.parseOptDropBehavior()
		return cmd
	}
	p.syntaxErrorAt()
	return nil
}
