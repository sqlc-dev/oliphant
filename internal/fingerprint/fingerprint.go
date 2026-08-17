// Package fingerprint computes libpg_query's version-3 query fingerprints
// (pg_query_fingerprint.c + the generated pg_query_fingerprint_defs.c /
// pg_query_fingerprint_conds.c at the pinned 17-6.2.2).
//
// Like the JSON emitter (internal/emit), the per-node walk is driven by
// protobuf reflection rather than generated per-node code: the upstream defs
// are generated from the same struct metadata the pinned pg_query.proto was
// generated from, so the descriptor already carries the field names, the
// alphabetical field order (sorted here), and the field kinds that select the
// per-type emission rule. The hand-written parts below mirror the upstream
// generator's special-case tables (scripts/generate_fingerprint_outfuncs.rb)
// and pg_query_fingerprint.c's hand-written value-node/list handling.
//
// The C implementation streams tokens into an XXH3 state and snapshots the
// state to elide fields whose subtree contributed nothing. Appending zero
// bytes is the only way the digest stays unchanged, so this port streams the
// tokens into a byte buffer instead and elides by truncating; the digest is a
// one-shot XXH3 of the buffer. This keeps internal/xxh3 free of streaming
// state.
package fingerprint

import (
	"sort"
	"strconv"

	"google.golang.org/protobuf/reflect/protoreflect"

	"github.com/sqlc-dev/oliphant/ast"
	"github.com/sqlc-dev/oliphant/internal/xxh3"
)

// fingerprintVersion is PG_QUERY_FINGERPRINT_VERSION, the XXH3 seed.
const fingerprintVersion = 3

// maxDepth is _fingerprintNode's cutoff: "lets consistently cut them off at
// 100 nodes deep".
const maxDepth = 100

// Tree fingerprints a full parse result (pg_query_fingerprint's
// _fingerprintNode over the raw statement list).
func Tree(tree *ast.ParseResult) uint64 {
	ctx := newContext()
	// The C tree is a List of RawStmt; _fingerprintNode(tree, NULL, NULL, 0)
	// dispatches to _fingerprintList (no sorted field name), which visits
	// each element at depth 1.
	for _, raw := range tree.Stmts {
		ctx.rawStmt(raw, 1)
	}
	return ctx.digest()
}

// Node fingerprints a single node: pg_query_fingerprint_node (used by
// normalize's GROUP BY matching).
func Node(n *ast.Node) uint64 {
	ctx := newContext()
	ctx.node(n, "", "", 0)
	return ctx.digest()
}

// listKey identifies a List node for the listsort cache. Every *ast.Node
// belongs to exactly one list in a parse tree, so the first element plus the
// length is as unique as the C List pointer.
type listKey struct {
	first *ast.Node
	n     int
}

type sortItem struct {
	hash uint64
	pos  int
}

type context struct {
	buf []byte
	// listsortCache mirrors the C listsort_cache: without it, nested
	// sort-fields would be re-hashed once per enclosing sort level, going
	// exponential on deep trees (the 1.1 MB stress query).
	listsortCache map[listKey][]sortItem
}

func newContext() *context {
	return &context{listsortCache: make(map[listKey][]sortItem)}
}

// sub creates a scratch context sharing the listsort cache
// (_fingerprintInitContext with a parent).
func (ctx *context) sub() *context {
	return &context{listsortCache: ctx.listsortCache}
}

func (ctx *context) digest() uint64 {
	return xxh3.Hash64Seed(ctx.buf, fingerprintVersion)
}

// str is _fingerprintString.
func (ctx *context) str(s string) {
	ctx.buf = append(ctx.buf, s...)
}

// rawStmt fingerprints one RawStmt list element: the conds dispatch writes
// the type name, then the generated per-node walk (stmt_len and
// stmt_location are intentionally ignored).
func (ctx *context) rawStmt(raw *ast.RawStmt, depth uint) {
	if raw == nil {
		return
	}
	ctx.str("RawStmt")
	ctx.fields(raw.ProtoReflect(), "", "", depth)
}

// node is _fingerprintNode: depth cutoff, then dispatch on the node tag.
func (ctx *context) node(n *ast.Node, parentType, fieldName string, depth uint) {
	if depth >= maxDepth {
		return
	}
	if n == nil {
		return
	}
	m := n.ProtoReflect()
	od := m.Descriptor().Oneofs().ByName("node")
	fd := m.WhichOneof(od)
	if fd == nil {
		return // NIL list element
	}
	inner := m.Get(fd).Message()
	name := string(inner.Descriptor().Name())

	switch name {
	case "List":
		items := inner.Get(inner.Descriptor().Fields().ByName("items")).List()
		ctx.list(items, parentType, fieldName, depth)
	case "Integer":
		// _fingerprintInteger: only a non-zero ival contributes.
		if v := inner.Get(inner.Descriptor().Fields().ByName("ival")).Int(); v != 0 {
			ctx.str("Integer")
			ctx.str("ival")
			ctx.str(strconv.FormatInt(v, 10))
		}
	case "Float":
		// Emitted under the pre-PG15 field name "str" for stable fingerprints.
		if v := inner.Get(inner.Descriptor().Fields().ByName("fval")).String(); v != "" {
			ctx.str("Float")
			ctx.str("str")
			ctx.str(v)
		}
	case "Boolean":
		ctx.str("Boolean")
		ctx.str("boolval")
		if inner.Get(inner.Descriptor().Fields().ByName("boolval")).Bool() {
			ctx.str("true")
		} else {
			ctx.str("false")
		}
	case "String":
		// Emitted under the pre-PG15 field name "str", even when empty.
		ctx.str("String")
		ctx.str("str")
		ctx.str(inner.Get(inner.Descriptor().Fields().ByName("sval")).String())
	case "BitString":
		if v := inner.Get(inner.Descriptor().Fields().ByName("bsval")).String(); v != "" {
			ctx.str("BitString")
			ctx.str("str")
			ctx.str(v)
		}
	case "TypeCast":
		// conds: a cast of a constant or parameter is ignored entirely, so
		// $1::int and 1::int fingerprint like $1 and 1.
		arg := inner.Get(inner.Descriptor().Fields().ByName("arg")).Message()
		if argFd := arg.WhichOneof(arg.Descriptor().Oneofs().ByName("node")); argFd != nil {
			switch arg.Get(argFd).Message().Descriptor().Name() {
			case "A_Const", "ParamRef":
				return
			}
		}
		ctx.str("TypeCast")
		ctx.fields(inner, parentType, fieldName, depth)
	default:
		if skipNodes[name] {
			return // Intentionally ignoring for fingerprinting
		}
		ctx.str(name)
		ctx.fields(inner, parentType, fieldName, depth)
	}
}

// sortFields are the list field names whose elements are fingerprinted in
// sorted-by-hash order with duplicates removed (_fingerprintList).
var sortFields = map[string]bool{
	"fromClause":  true,
	"targetList":  true,
	"cols":        true,
	"rexpr":       true,
	"valuesLists": true,
	"args":        true,
}

// list is _fingerprintList. fieldName travels down through nested lists, so
// the inner lists of valuesLists sort too.
func (ctx *context) list(items protoreflect.List, parentType, fieldName string, depth uint) {
	if sortFields[fieldName] {
		key := listKey{n: items.Len()}
		if items.Len() > 0 {
			key.first = items.Get(0).Message().Interface().(*ast.Node)
		}
		sorted, ok := ctx.listsortCache[key]
		if !ok {
			sorted = make([]sortItem, items.Len())
			for i := 0; i < items.Len(); i++ {
				sub := ctx.sub()
				sub.node(items.Get(i).Message().Interface().(*ast.Node), parentType, fieldName, depth+1)
				sorted[i] = sortItem{hash: sub.digest(), pos: i}
			}
			sort.SliceStable(sorted, func(a, b int) bool { return sorted[a].hash < sorted[b].hash })
			ctx.listsortCache[key] = sorted
		}
		for i, it := range sorted {
			if i > 0 && sorted[i-1].hash == it.hash {
				continue // Ignore duplicates
			}
			ctx.node(items.Get(it.pos).Message().Interface().(*ast.Node), parentType, fieldName, depth+1)
		}
		return
	}
	for i := 0; i < items.Len(); i++ {
		ctx.node(items.Get(i).Message().Interface().(*ast.Node), parentType, fieldName, depth+1)
	}
}

// skipNodes mirrors FINGERPRINT_SKIP_NODES: every field intentionally
// ignored. Reached both through Node dispatch (no type name emitted either)
// and through typed fields (the walk contributes nothing, so the field name
// is elided).
var skipNodes = map[string]bool{
	"A_Const":      true,
	"Alias":        true,
	"ParamRef":     true,
	"SetToDefault": true,
	"IntList":      true,
	"OidList":      true,
}

// skipFields mirrors FINGERPRINT_OVERRIDE_FIELDS' :skip entries, plus the
// C-only fields the generator ignores by type (xpr). The "" key applies to
// every node type.
var skipFields = map[[2]string]bool{
	{"", "location"}:                       true,
	{"", "xpr"}:                            true,
	{"PrepareStmt", "name"}:                true,
	{"ExecuteStmt", "name"}:                true,
	{"DeallocateStmt", "name"}:             true,
	{"TransactionStmt", "options"}:         true,
	{"TransactionStmt", "gid"}:             true,
	{"TransactionStmt", "savepoint_name"}:  true,
	{"CreateFunctionStmt", "options"}:      true,
	{"FunctionParameter", "name"}:          true,
	{"DoStmt", "args"}:                     true,
	{"ListenStmt", "conditionname"}:        true,
	{"UnlistenStmt", "conditionname"}:      true,
	{"NotifyStmt", "conditionname"}:        true,
	{"DeclareCursorStmt", "portalname"}:    true,
	{"FetchStmt", "portalname"}:            true,
	{"ClosePortalStmt", "portalname"}:      true,
	{"RawStmt", "stmt_len"}:                true,
	{"RawStmt", "stmt_location"}:           true,
	{"JsonTablePathSpec", "name_location"}: true,
	// JsonTablePath.value and JsonTablePlan embeds exist only in the C
	// structs, not in the proto, so their skips need no entry here.
}

// alwaysEmitString mirrors internal/emit's list: string fields the grammar
// assigns unconditionally, where the C fingerprint emits the (possibly
// empty) value because the pointer is never NULL. (CreateTableSpaceStmt's
// "location" is already skipped by name.)
var alwaysEmitString = map[[2]string]bool{
	{"CreateSubscriptionStmt", "conninfo"}: true,
}

// fields walks one message's fields in alphabetical (C generator) order,
// applying the per-kind emission rules. parentType/fieldName describe how
// this node was reached (for the ResTarget.name override).
func (ctx *context) fields(m protoreflect.Message, parentType, fieldName string, depth uint) {
	d := m.Descriptor()
	msgName := string(d.Name())
	if skipNodes[msgName] {
		return // Intentionally ignoring all fields for fingerprinting
	}

	fds := d.Fields()
	order := make([]protoreflect.FieldDescriptor, fds.Len())
	for i := 0; i < fds.Len(); i++ {
		order[i] = fds.Get(i)
	}
	sort.Slice(order, func(a, b int) bool { return order[a].JSONName() < order[b].JSONName() })

	for _, fd := range order {
		name := fd.JSONName()
		if skipFields[[2]string{"", name}] || skipFields[[2]string{msgName, name}] {
			continue
		}
		v := m.Get(fd)

		// Hand-written overrides from the upstream generator.
		switch {
		case msgName == "ResTarget" && name == "name":
			// A SELECT target list alias does not change the fingerprint;
			// INSERT column names and UPDATE SET columns do.
			if s := v.String(); s != "" && !(parentType == "SelectStmt" && fieldName == "targetList") {
				ctx.str("name")
				ctx.str(s)
			}
			continue
		case msgName == "RangeVar" && name == "relname":
			// Runs of two or more digits are stripped (sharded table names
			// hash together), and temp table names are ignored entirely.
			relname := v.String()
			relpersistence := m.Get(fds.ByJSONName("relpersistence")).String()
			if relname != "" && relpersistence != "t" {
				r := make([]byte, 0, len(relname))
				for i := 0; i < len(relname); i++ {
					c := relname[i]
					if c >= '0' && c <= '9' &&
						((i+1 < len(relname) && relname[i+1] >= '0' && relname[i+1] <= '9') ||
							(i > 0 && relname[i-1] >= '0' && relname[i-1] <= '9')) {
						continue
					}
					r = append(r, c)
				}
				ctx.str("relname")
				ctx.str(string(r))
			}
			continue
		case msgName == "A_Expr" && name == "kind":
			// AEXPR_OP_ANY and AEXPR_IN fold into AEXPR_OP, so y = ANY($1)
			// and y IN ($1) fingerprint like y = $1.
			ctx.str("kind")
			kind := ast.A_Expr_Kind(v.Enum())
			if kind == ast.A_Expr_Kind_AEXPR_OP_ANY || kind == ast.A_Expr_Kind_AEXPR_IN {
				ctx.str("AEXPR_OP")
			} else {
				ctx.str(enumName(fd, v.Enum()))
			}
			continue
		case msgName == "AlterObjectDependsStmt" && name == "extname":
			// The one String*-typed C field: emitted as a plain string field.
			if m.Has(fd) {
				if s := v.Message().Get(v.Message().Descriptor().Fields().ByName("sval")).String(); s != "" {
					ctx.str("extname")
					ctx.str(s)
				}
			}
			continue
		case msgName == "CreateForeignTableStmt" && name == "base":
			// The embedded CreateStmt: no elision, same depth.
			ctx.str("base")
			ctx.fields(v.Message(), msgName, "base", depth)
			continue
		}

		switch {
		case fd.IsList() && fd.Kind() == protoreflect.MessageKind:
			// List* fields: skipped when empty; the field name is elided when
			// the walk contributes nothing, except for a single-NIL list.
			l := v.List()
			if l.Len() == 0 {
				continue
			}
			mark := len(ctx.buf)
			ctx.str(name)
			after := len(ctx.buf)
			if depth+1 < maxDepth {
				ctx.list(l, msgName, name, depth+1)
			}
			if len(ctx.buf) == after {
				singleNil := false
				if l.Len() == 1 {
					first := l.Get(0).Message()
					singleNil = first.WhichOneof(first.Descriptor().Oneofs().ByName("node")) == nil
				}
				if !singleNil {
					ctx.buf = ctx.buf[:mark]
				}
			}
		case fd.IsList():
			// Bitmapset fields (analyzed-only nodes): the field name is
			// written unconditionally, then each member.
			ctx.str(name)
			for i := 0; i < v.List().Len(); i++ {
				el := v.List().Get(i)
				switch fd.Kind() {
				case protoreflect.Uint64Kind, protoreflect.Uint32Kind:
					ctx.str(strconv.FormatUint(el.Uint(), 10))
				default:
					ctx.str(strconv.FormatInt(el.Int(), 10))
				}
			}
		case fd.Kind() == protoreflect.MessageKind:
			if !m.Has(fd) {
				continue
			}
			mark := len(ctx.buf)
			ctx.str(name)
			after := len(ctx.buf)
			sub := v.Message()
			if string(sub.Descriptor().Name()) == "Node" {
				ctx.node(sub.Interface().(*ast.Node), msgName, name, depth+1)
			} else {
				// Specific node pointers walk the fields directly, without
				// the type-name string the Node dispatch writes — and, as in
				// the generated C, without a depth cutoff (only
				// _fingerprintNode checks the depth).
				ctx.fields(sub, msgName, name, depth+1)
			}
			if len(ctx.buf) == after {
				ctx.buf = ctx.buf[:mark]
			}
		case fd.Kind() == protoreflect.EnumKind:
			// Always emitted, by C enum value name.
			ctx.str(name)
			ctx.str(enumName(fd, v.Enum()))
		case fd.Kind() == protoreflect.BoolKind:
			if v.Bool() {
				ctx.str(name)
				ctx.str("true")
			}
		case fd.Kind() == protoreflect.StringKind:
			if s := v.String(); s != "" || alwaysEmitString[[2]string{msgName, name}] {
				ctx.str(name)
				ctx.str(s)
			}
		case fd.Kind() == protoreflect.DoubleKind, fd.Kind() == protoreflect.FloatKind:
			if f := v.Float(); f != 0 {
				ctx.str(name)
				ctx.str(strconv.FormatFloat(f, 'f', 6, 64))
			}
		case fd.Kind() == protoreflect.Uint32Kind, fd.Kind() == protoreflect.Uint64Kind:
			if u := v.Uint(); u != 0 {
				ctx.str(name)
				ctx.str(strconv.FormatUint(u, 10))
			}
		default:
			if i := v.Int(); i != 0 {
				ctx.str(name)
				ctx.str(strconv.FormatInt(i, 10))
			}
		}
	}
}

func enumName(fd protoreflect.FieldDescriptor, n protoreflect.EnumNumber) string {
	return string(fd.Enum().Values().ByNumber(n).Name())
}
