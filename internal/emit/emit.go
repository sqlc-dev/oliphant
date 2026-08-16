// Package emit renders parse trees as JSON with byte-parity to libpg_query's
// hand-rolled C emitter (pg_query_outfuncs_json.c + pg_query_json_helper.c at
// the pinned 17-6.2.2).
//
// The C emitter is generated from the same struct metadata the pinned
// pg_query.proto was generated from, so the proto descriptor carries
// everything the output format needs: message fields are declared in C struct
// order, each field's json_name is the C field name, and the field type
// selects the WRITE_*_FIELD macro. The walker below is therefore driven by
// protobuf reflection rather than by generated per-node code; byte-parity is
// enforced corpus-wide by the golden round-trip test in this package.
package emit

import (
	"fmt"
	"strconv"
	"strings"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"

	"github.com/sqlc-dev/oliphant/ast"
)

// ParseResult renders the full document: pg_query_nodes_to_json.
func ParseResult(tree *ast.ParseResult) string {
	var b strings.Builder
	// pg_query_outfuncs_json.c: pg_query_nodes_to_json emits the version and
	// the stmts array unconditionally (empty input => "stmts":[]).
	b.WriteString(`{"version":`)
	b.WriteString(strconv.FormatInt(int64(tree.Version), 10))
	b.WriteString(`,"stmts":[`)
	for i, raw := range tree.Stmts {
		if i > 0 {
			b.WriteByte(',')
		}
		// RawStmt fields are emitted inline, without a node-type wrapper.
		b.WriteByte('{')
		emitFields(&b, raw.ProtoReflect())
		b.WriteByte('}')
	}
	b.WriteString(`]}`)
	return b.String()
}

// Node renders one node with its type wrapper: _outNode.
func Node(msg proto.Message) string {
	var b strings.Builder
	emitNode(&b, msg.ProtoReflect())
	return b.String()
}

// emitNode is _outNode: {"<TypeName>":{...fields...}} for a Node message,
// dispatching on the oneof member. An unset Node (a NULL list element
// upstream) renders as {}.
func emitNode(b *strings.Builder, m protoreflect.Message) {
	d := m.Descriptor()
	if d.Name() != "Node" {
		// A specific node type reached through a typed field: the C emitter
		// prints its fields without a wrapper (WRITE_SPECIFIC_NODE_PTR_FIELD).
		b.WriteByte('{')
		emitFields(b, m)
		b.WriteByte('}')
		return
	}
	od := d.Oneofs().ByName("node")
	fd := m.WhichOneof(od)
	if fd == nil {
		b.WriteString("{}")
		return
	}
	b.WriteByte('{')
	b.WriteByte('"')
	b.WriteString(fd.JSONName())
	b.WriteString(`":{`)
	emitFields(b, m.Get(fd).Message())
	b.WriteString("}}")
}

// emitFields renders a message's fields in declaration (= C struct) order,
// applying the WRITE_*_FIELD zero-omission rules. Returns the number of
// fields written.
func emitFields(b *strings.Builder, m protoreflect.Message) int {
	d := m.Descriptor()

	// Special-cased node types, mirroring the hand-written emitters in
	// pg_query_outfuncs_json.c.
	switch d.Name() {
	case "A_Const":
		emitAConst(b, m)
		return 1
	case "Integer":
		// _outInteger: omit the default 0, to match protobuf's behavior.
		v := m.Get(d.Fields().ByName("ival")).Int()
		if v == 0 {
			return 0
		}
		fmt.Fprintf(b, `"ival":%d`, v)
		return 1
	case "Boolean":
		// _outBoolean: always emitted, true or false.
		fmt.Fprintf(b, `"boolval":%s`, boolStr(m.Get(d.Fields().ByName("boolval")).Bool()))
		return 1
	case "Float":
		b.WriteString(`"fval":`)
		emitToken(b, m.Get(d.Fields().ByName("fval")).String())
		return 1
	case "String":
		b.WriteString(`"sval":`)
		emitToken(b, m.Get(d.Fields().ByName("sval")).String())
		return 1
	case "BitString":
		b.WriteString(`"bsval":`)
		emitToken(b, m.Get(d.Fields().ByName("bsval")).String())
		return 1
	case "List", "IntList", "OidList":
		// _outList/_outIntList/_outOidList: "items" always present.
		b.WriteString(`"items":[`)
		items := m.Get(d.Fields().ByName("items")).List()
		for i := 0; i < items.Len(); i++ {
			if i > 0 {
				b.WriteByte(',')
			}
			emitNode(b, items.Get(i).Message())
		}
		b.WriteByte(']')
		return 1
	}

	fields := d.Fields()
	written := 0
	sep := func() {
		if written > 0 {
			b.WriteByte(',')
		}
		written++
	}
	for i := 0; i < fields.Len(); i++ {
		fd := fields.Get(i)
		v := m.Get(fd)
		switch {
		case fd.IsList():
			// WRITE_LIST_FIELD: omitted when NIL (empty).
			l := v.List()
			if l.Len() == 0 {
				continue
			}
			sep()
			b.WriteByte('"')
			b.WriteString(fd.JSONName())
			b.WriteString(`":[`)
			for j := 0; j < l.Len(); j++ {
				if j > 0 {
					b.WriteByte(',')
				}
				switch fd.Kind() {
				case protoreflect.MessageKind:
					emitNode(b, l.Get(j).Message())
				case protoreflect.Uint64Kind, protoreflect.Uint32Kind:
					fmt.Fprintf(b, "%d", l.Get(j).Uint())
				default:
					fmt.Fprintf(b, "%d", l.Get(j).Int())
				}
			}
			b.WriteByte(']')
		case fd.Kind() == protoreflect.MessageKind:
			// WRITE_NODE_PTR_FIELD / WRITE_SPECIFIC_NODE_PTR_FIELD: omitted
			// when NULL.
			if !m.Has(fd) {
				continue
			}
			sep()
			b.WriteByte('"')
			b.WriteString(fd.JSONName())
			b.WriteString(`":`)
			emitNode(b, v.Message())
		case fd.Kind() == protoreflect.EnumKind:
			// WRITE_ENUM_FIELD: always emitted, by C enum name (the proto
			// enum value names are the C names).
			sep()
			b.WriteByte('"')
			b.WriteString(fd.JSONName())
			b.WriteString(`":"`)
			b.WriteString(string(fd.Enum().Values().ByNumber(v.Enum()).Name()))
			b.WriteByte('"')
		case fd.Kind() == protoreflect.BoolKind:
			// WRITE_BOOL_FIELD: emitted only when true.
			if !v.Bool() {
				continue
			}
			sep()
			b.WriteByte('"')
			b.WriteString(fd.JSONName())
			b.WriteString(`":true`)
		case fd.Kind() == protoreflect.StringKind:
			// WRITE_STRING_FIELD / WRITE_CHAR_FIELD: omitted when NULL/0. A
			// non-NULL empty C string is indistinguishable in proto3; the
			// places upstream produces one (String.sval is handled above) are
			// fields the grammar always assigns, listed in alwaysEmitString.
			if v.String() == "" && !alwaysEmitString[string(d.Name())+"."+fd.JSONName()] {
				continue
			}
			sep()
			b.WriteByte('"')
			b.WriteString(fd.JSONName())
			b.WriteString(`":`)
			emitToken(b, v.String())
		case fd.Kind() == protoreflect.DoubleKind, fd.Kind() == protoreflect.FloatKind:
			// WRITE_FLOAT_FIELD: always emitted, %f.
			sep()
			fmt.Fprintf(b, `"%s":%f`, fd.JSONName(), v.Float())
		case fd.Kind() == protoreflect.Uint32Kind, fd.Kind() == protoreflect.Uint64Kind:
			// WRITE_UINT_FIELD: omitted when 0.
			if v.Uint() == 0 {
				continue
			}
			sep()
			fmt.Fprintf(b, `"%s":%d`, fd.JSONName(), v.Uint())
		default:
			// WRITE_INT_FIELD / WRITE_LONG_FIELD: omitted when 0.
			if v.Int() == 0 {
				continue
			}
			sep()
			fmt.Fprintf(b, `"%s":%d`, fd.JSONName(), v.Int())
		}
	}
	return written
}

// emitAConst is _outAConst: the value union member wrapped in an object under
// its member name, and the location emitted unconditionally.
func emitAConst(b *strings.Builder, m protoreflect.Message) {
	d := m.Descriptor()
	if m.Get(d.Fields().ByName("isnull")).Bool() {
		b.WriteString(`"isnull":true`)
	} else if fd := m.WhichOneof(d.Oneofs().ByName("val")); fd != nil {
		b.WriteByte('"')
		b.WriteString(fd.JSONName())
		b.WriteString(`":{`)
		inner := m.Get(fd).Message()
		switch fd.JSONName() {
		case "ival":
			// _outInteger: zero omitted => "ival":{}
			if v := inner.Get(inner.Descriptor().Fields().ByName("ival")).Int(); v != 0 {
				fmt.Fprintf(b, `"ival":%d`, v)
			}
		case "fval":
			b.WriteString(`"fval":`)
			emitToken(b, inner.Get(inner.Descriptor().Fields().ByName("fval")).String())
		case "boolval":
			// _outAConst emits "boolval":{} for false.
			if inner.Get(inner.Descriptor().Fields().ByName("boolval")).Bool() {
				b.WriteString(`"boolval":true`)
			}
		case "sval":
			b.WriteString(`"sval":`)
			emitToken(b, inner.Get(inner.Descriptor().Fields().ByName("sval")).String())
		case "bsval":
			b.WriteString(`"bsval":`)
			emitToken(b, inner.Get(inner.Descriptor().Fields().ByName("bsval")).String())
		}
		b.WriteByte('}')
	} else {
		// A_Const with no value member set cannot come from the parser; the C
		// union would have printed garbage. Render nothing but the location.
		b.WriteString(`"isnull":true`)
	}
	loc := m.Get(d.Fields().ByName("location")).Int()
	fmt.Fprintf(b, `,"location":%d`, loc)
}

// emitToken is _outToken: JSON string quoting with the reference's exact
// escape set — \b \f \n \r \t \" \\ as two-character escapes, and \u%04x for
// other control bytes plus '<' and '>'.
func emitToken(b *strings.Builder, s string) {
	b.WriteByte('"')
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == 0 {
			// The C emitter walks a NUL-terminated string.
			break
		}
		switch c {
		case '\b':
			b.WriteString(`\b`)
		case '\f':
			b.WriteString(`\f`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		default:
			if c < ' ' || c == '<' || c == '>' {
				fmt.Fprintf(b, `\u%04x`, c)
			} else {
				b.WriteByte(c)
			}
		}
	}
	b.WriteByte('"')
}

// alwaysEmitString lists string fields whose C value is never NULL — the
// grammar assigns them unconditionally, and ” is legal input — so the C
// emitter always writes them, even when empty. (proto3 cannot represent the
// NULL/"" distinction; for these fields "" means present.)
var alwaysEmitString = map[string]bool{
	"CreateSubscriptionStmt.conninfo": true, // CONNECTION Sconst is required syntax
	"CreateTableSpaceStmt.location":   true, // LOCATION Sconst is required syntax
}

func boolStr(v bool) string {
	if v {
		return "true"
	}
	return "false"
}
