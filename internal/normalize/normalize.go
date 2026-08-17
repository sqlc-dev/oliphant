// Package normalize ports pg_query_normalize.c at the pinned 18.0.0:
// constants in the query text are replaced with $n parameter references,
// numbered in tree-walk order after the highest existing $n.
//
// The walk is const_record_walker: a hand-written switch over the raw parse
// tree with special cases for constant-bearing utility statements and for
// SELECT (GROUP BY entries are matched against target-list entries by
// fingerprint so both get the same parameter numbers), falling through to
// PostgreSQL 18's raw_expression_tree_walker for everything else. Node types
// that walker does not know abort the walk of that subtree — upstream's
// elog(ERROR) is caught per-node and swallowed — which this port reproduces
// by returning early for unsupported types (walker.go).
package normalize

import (
	"strconv"
	"strings"

	"github.com/sqlc-dev/oliphant/ast"
	"github.com/sqlc-dev/oliphant/internal/fingerprint"
	"github.com/sqlc-dev/oliphant/internal/lexer"
	"github.com/sqlc-dev/oliphant/internal/parse"
)

// constLoc is pgssLocationLen.
type constLoc struct {
	location int // start offset in query text
	length   int // length in bytes, or -1 to ignore
	// paramID is the Param id to use; a negative value means
	// abs(paramID) + highestExternParamID.
	paramID int
}

// state is pgssConstLocations.
type state struct {
	clocations []constLoc

	// highest Param id assigned so far, before accounting for external
	// param refs (starts at 1: the first constant gets -1 => extern+1).
	highestNormalizeParamID int
	// highest $n seen in the query, so numbering starts after it.
	highestExternParamID int

	query string

	// paramRefs records assigned or discovered param refs while walking one
	// target-list element; nil when inactive.
	paramRefs       []int
	paramRefsActive bool

	utilityOnly bool
}

// Normalize is pg_query_normalize_ext.
func Normalize(input string, utilityOnly bool) (string, *lexer.Error) {
	tree, perr := parse.Parse(input)
	if perr != nil {
		return "", perr
	}

	s := &state{
		highestNormalizeParamID: 1,
		query:                   input,
		utilityOnly:             utilityOnly,
	}
	for _, raw := range tree.Stmts {
		// const_record_walker over the RawStmt list: T_RawStmt walks ->stmt.
		s.walk(raw.Stmt)
	}
	return s.generateNormalizedQuery(), nil
}

// recordConstLocation is RecordConstLocation.
func (s *state) recordConstLocation(location int32) {
	if location < 0 {
		return // -1 indicates unknown or undefined location
	}
	s.clocations = append(s.clocations, constLoc{
		location: int(location),
		length:   -1,
		// by default we assume that we need a new param ref
		paramID: -s.highestNormalizeParamID,
	})
	s.highestNormalizeParamID++
	if s.paramRefsActive {
		s.paramRefs = append(s.paramRefs, s.clocations[len(s.clocations)-1].paramID)
	}
}

func isStringDelimiter(c byte) bool {
	return c == '\'' || c == '$'
}

func isSpecialStringStart(c byte) bool {
	return c == 'b' || c == 'B' || c == 'x' || c == 'X' || c == 'n' || c == 'N' || c == 'e' || c == 'E'
}

// recordDefElemArgLocation scans forward from a DefElem's location for the
// string argument's opening delimiter (record_defelem_arg_location).
func (s *state) recordDefElemArgLocation(location int32) {
	if location < 0 {
		return
	}
	for i := int(location); i < len(s.query); i++ {
		if isStringDelimiter(s.query[i]) ||
			(i+1 < len(s.query) && isSpecialStringStart(s.query[i]) && isStringDelimiter(s.query[i+1])) {
			s.recordConstLocation(int32(i))
			break
		}
	}
}

// recordMatchingString finds str in the query text and records the character
// before it — the opening quote (record_matching_string).
func (s *state) recordMatchingString(str string) {
	if str == "" {
		// A NULL conninfo upstream; "" can also be CONNECTION '', which
		// strstr would find at an arbitrary position — the C code passes the
		// empty string to strstr and gets position 0, recording -1, which
		// RecordConstLocation drops. Same net effect.
		return
	}
	if loc := strings.Index(s.query, str); loc >= 0 {
		s.recordConstLocation(int32(loc - 1))
	}
}

// fpAndParamRefs is FpAndParamRefs.
type fpAndParamRefs struct {
	fp        uint64
	paramRefs []int
}

// walkSelect is const_record_walker's T_SelectStmt case.
func (s *state) walkSelect(stmt *ast.SelectStmt) bool {
	if s.utilityOnly {
		return false
	}

	var fpList []*fpAndParamRefs

	if s.walkList(stmt.DistinctClause) {
		return true
	}
	if s.walkIntoClause(stmt.IntoClause) {
		return true
	}
	for _, lc := range stmt.TargetList {
		// Save all param refs we encounter or assign for this element.
		s.paramRefs = []int{}
		s.paramRefsActive = true

		if s.walk(lc) {
			return true
		}

		// Remember fingerprint and param refs for the GROUP BY matching.
		var val *ast.Node
		if rt := lc.GetResTarget(); rt != nil {
			val = rt.Val
		}
		fpList = append(fpList, &fpAndParamRefs{
			fp:        fingerprint.Node(val),
			paramRefs: s.paramRefs,
		})

		s.paramRefs = nil
		s.paramRefsActive = false
	}
	if s.walkList(stmt.FromClause) {
		return true
	}
	if s.walk(stmt.WhereClause) {
		return true
	}

	// Instead of walking all of groupClause, only walk certain items.
	for _, lc := range stmt.GroupClause {
		// Do not walk simple integer A_Consts: "GROUP BY 1" stays "GROUP BY
		// 1" (as in pg_stat_statements).
		if ac := lc.GetAConst(); ac != nil && ac.GetIval() != nil {
			continue
		}

		// Match GROUP BY clauses against the target list, to assign the same
		// param refs as used in the target list — this keeps the normalized
		// query valid.
		fp := fingerprint.Node(lc)
		var fppr *fpAndParamRefs
		for i, e := range fpList {
			if fp == e.fp {
				fppr = e
				fpList = append(fpList[:i], fpList[i+1:]...)
				break
			}
		}

		prevClocCount := len(s.clocations)
		if s.walk(lc) {
			return true
		}

		if fppr != nil && len(fppr.paramRefs) == len(s.clocations)-prevClocCount {
			for i := prevClocCount; i < len(s.clocations); i++ {
				s.clocations[i].paramID = fppr.paramRefs[i-prevClocCount]
			}
			s.highestNormalizeParamID -= len(fppr.paramRefs)
		}
	}
	for _, lc := range stmt.SortClause {
		// Similarly, don't turn "ORDER BY 1" into "ORDER BY $n".
		if sb := lc.GetSortBy(); sb != nil {
			if ac := sb.Node.GetAConst(); ac != nil && ac.GetIval() != nil {
				continue
			}
		}
		if s.walk(lc) {
			return true
		}
	}
	if s.walk(stmt.HavingClause) {
		return true
	}
	if s.walkList(stmt.WindowClause) {
		return true
	}
	if s.walkList(stmt.ValuesLists) {
		return true
	}
	if s.walk(stmt.LimitOffset) {
		return true
	}
	if s.walk(stmt.LimitCount) {
		return true
	}
	if s.walkList(stmt.LockingClause) {
		return true
	}
	if s.walkWithClause(stmt.WithClause) {
		return true
	}
	if stmt.Larg != nil && s.walkSelect(stmt.Larg) {
		return true
	}
	if stmt.Rarg != nil && s.walkSelect(stmt.Rarg) {
		return true
	}
	return false
}

// walk is const_record_walker: the hand-written cases, then the raw
// expression tree walker for everything else (walker.go).
func (s *state) walk(n *ast.Node) bool {
	if n == nil || n.Node == nil {
		return false
	}
	switch v := n.Node.(type) {
	case *ast.Node_AConst:
		s.recordConstLocation(v.AConst.Location)
		return false
	case *ast.Node_ParamRef:
		// Track the highest ParamRef number.
		if int(v.ParamRef.Number) > s.highestExternParamID {
			s.highestExternParamID = int(v.ParamRef.Number)
		}
		if s.paramRefsActive {
			s.paramRefs = append(s.paramRefs, int(v.ParamRef.Number))
		}
		return false
	case *ast.Node_DefElem:
		d := v.DefElem
		if d.Arg == nil || d.Arg.Node == nil {
			// No argument
		} else if d.Arg.GetString_() != nil {
			s.recordDefElemArgLocation(d.Location)
		} else if l := d.Arg.GetList(); l != nil && len(l.Items) == 1 && l.Items[0].GetString_() != nil {
			s.recordDefElemArgLocation(d.Location)
		}
		return s.walk(d.Arg)
	case *ast.Node_VariableSetStmt:
		if s.utilityOnly {
			return false
		}
		return s.walkList(v.VariableSetStmt.Args)
	case *ast.Node_CopyStmt:
		if s.utilityOnly {
			return false
		}
		return s.walk(v.CopyStmt.Query)
	case *ast.Node_CallStmt:
		if s.utilityOnly {
			return false
		}
		return s.walkFuncCall(v.CallStmt.Funccall)
	case *ast.Node_ExplainStmt:
		return s.walk(v.ExplainStmt.Query)
	case *ast.Node_CreateRoleStmt:
		return s.walkList(v.CreateRoleStmt.Options)
	case *ast.Node_AlterRoleStmt:
		return s.walkList(v.AlterRoleStmt.Options)
	case *ast.Node_DeclareCursorStmt:
		if s.utilityOnly {
			return false
		}
		return s.walk(v.DeclareCursorStmt.Query)
	case *ast.Node_CreateFunctionStmt:
		if s.utilityOnly {
			return false
		}
		return s.walkList(v.CreateFunctionStmt.Options)
	case *ast.Node_DoStmt:
		if s.utilityOnly {
			return false
		}
		return s.walkList(v.DoStmt.Args)
	case *ast.Node_CreateSubscriptionStmt:
		s.recordMatchingString(v.CreateSubscriptionStmt.Conninfo)
		return false
	case *ast.Node_AlterSubscriptionStmt:
		s.recordMatchingString(v.AlterSubscriptionStmt.Conninfo)
		return false
	case *ast.Node_CreateUserMappingStmt:
		return s.walkList(v.CreateUserMappingStmt.Options)
	case *ast.Node_AlterUserMappingStmt:
		return s.walkList(v.AlterUserMappingStmt.Options)
	case *ast.Node_TypeName:
		// Don't normalize constants in typmods or arrayBounds.
		return false
	case *ast.Node_SelectStmt:
		return s.walkSelect(v.SelectStmt)
	case *ast.Node_MergeStmt, *ast.Node_InsertStmt, *ast.Node_UpdateStmt, *ast.Node_DeleteStmt:
		if s.utilityOnly {
			return false
		}
		return s.rawWalk(n)
	default:
		return s.rawWalk(n)
	}
}

func (s *state) walkList(items []*ast.Node) bool {
	for _, item := range items {
		if s.walk(item) {
			return true
		}
	}
	return false
}

// generateNormalizedQuery is generate_normalized_query.
func (s *state) generateNormalizedQuery() string {
	s.fillInConstantLengths()

	var b strings.Builder
	b.Grow(len(s.query))
	querLoc := 0 // source query byte location
	for _, cl := range s.clocations {
		off := cl.location
		tokLen := cl.length
		if tokLen < 0 {
			continue // ignore any duplicates
		}

		// Copy what precedes the constant, then a param symbol in its place.
		b.WriteString(s.query[querLoc:off])
		paramID := cl.paramID
		if paramID < 0 {
			paramID = s.highestExternParamID + (-paramID)
		}
		b.WriteByte('$')
		b.WriteString(strconv.Itoa(paramID))

		querLoc = off + tokLen
	}
	b.WriteString(s.query[querLoc:])
	return b.String()
}

// fillInConstantLengths is fill_in_constant_lengths: scan the query with the
// core lexer to find the byte length of each recorded constant. Duplicate
// locations keep length -1 and are skipped later.
func (s *state) fillInConstantLengths() {
	// Sort by location with PostgreSQL's own qsort; for duplicate locations
	// only the entry pg_qsort leaves first is filled in (see qsort.go).
	if len(s.clocations) > 1 {
		pgQsort(s.clocations)
	}

	scanner := lexer.New(s.query)
	lastLoc := -1
	done := false
	for i := range s.clocations {
		if done {
			break
		}
		loc := s.clocations[i].location
		if loc <= lastLoc {
			continue // Duplicate constant, ignore
		}

		// Lex tokens until we find the desired constant.
		for {
			tok, lerr := scanner.Next()
			if lerr != nil || tok.Kind == 0 {
				// End of string (should not happen): leave lengths -1.
				done = true
				break
			}
			if int(tok.Start) >= loc {
				if s.query[loc] == '-' {
					// A negative value: the only case where more than a
					// single token is replaced, so that "bar = 1" and
					// "bar = -2" normalize identically.
					tok, lerr = scanner.Next()
					if lerr != nil || tok.Kind == 0 {
						done = true
						break
					}
				}
				// Flex leaves its hold-char NUL at the end of the current
				// match; the token End is the same position. (The upstream
				// U&'' trailing-whitespace trim is a no-op here: End already
				// excludes the UESCAPE lookahead whitespace.)
				s.clocations[i].length = int(tok.End) - loc
				break
			}
		}

		lastLoc = loc
	}
}
