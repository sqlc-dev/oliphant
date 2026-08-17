package plpgsql

import (
	"github.com/sqlc-dev/oliphant/internal/deparse"
)

// compileError carries an ereport out of the compile (message and the C
// error-data fields the golden format serializes). Context is attached at
// the catch site by the compile error callback logic.
type compileError struct {
	Message   string
	Filename  string
	Funcname  string
	Cursorpos int
	Context   string
}

func (e *compileError) Error() string { return e.Message }

// Filenames as __FILE__ expands in the vendored libpg_query build.
const (
	plGramFilename = "pl_gram.y"
	plCompFilename = "src_pl_plpgsql_src_pl_comp.c"
)

// pg_type.h OIDs (the ones the stripped pl_comp build distinguishes).
const (
	oidBool      = 16
	oidInt4      = 23
	oidText      = 25
	oidUnknown   = 705
	oidRefcursor = 1790
	oidRecord    = 2249
	oidVoid      = 2278

	oidDefaultCollation = 100
)

// compiler bundles the per-compile globals of pl_comp.c / pl_scanner.c.
type compiler struct {
	scan *plScanner

	fn *plFunction

	nsTop *nsItem

	datums     []datum // plpgsql_Datums
	datumsLast int     // datums_last

	checkSyntax bool   // plpgsql_check_syntax (always true here)
	errFuncname string // plpgsql_error_funcname
	nDatums     int    // plpgsql_nDatums (== len(datums))
}

// --- namespace (pl_funcs.c) --------------------------------------------------

type nsItem struct {
	itemtype nsItemType
	itemno   int
	prev     *nsItem
	name     string
}

// nsInit is plpgsql_ns_init.
func (c *compiler) nsInit() { c.nsTop = nil }

// nsPush is plpgsql_ns_push.
func (c *compiler) nsPush(label string, labelType labelType) {
	c.nsAdditem(nsTypeLabel, int(labelType), label)
}

// nsPop is plpgsql_ns_pop.
func (c *compiler) nsPop() {
	for c.nsTop.itemtype != nsTypeLabel {
		c.nsTop = c.nsTop.prev
	}
	c.nsTop = c.nsTop.prev
}

// nsAdditem is plpgsql_ns_additem.
func (c *compiler) nsAdditem(itemtype nsItemType, itemno int, name string) {
	c.nsTop = &nsItem{itemtype: itemtype, itemno: itemno, prev: c.nsTop, name: name}
}

// nsLookup is plpgsql_ns_lookup. name2/name3 empty mean NULL.
func (c *compiler) nsLookup(nsCur *nsItem, localmode bool, name1, name2, name3 string) (*nsItem, int) {
	for nsCur != nil {
		var nsitem *nsItem

		// Check this level for unqualified match to variable name
		for nsitem = nsCur; nsitem.itemtype != nsTypeLabel; nsitem = nsitem.prev {
			if nsitem.name == name1 {
				if name2 == "" || nsitem.itemtype != nsTypeVar {
					return nsitem, 1
				}
			}
		}

		// Check this level for qualified match to variable name
		if name2 != "" && nsitem.name == name1 {
			for inner := nsCur; inner.itemtype != nsTypeLabel; inner = inner.prev {
				if inner.name == name2 {
					if name3 == "" || inner.itemtype != nsTypeVar {
						return inner, 2
					}
				}
			}
		}

		if localmode {
			break // do not look into upper levels
		}
		nsCur = nsitem.prev
	}
	return nil, 0
}

// nsLookupLabel is plpgsql_ns_lookup_label.
func (c *compiler) nsLookupLabel(nsCur *nsItem, name string) *nsItem {
	for ; nsCur != nil; nsCur = nsCur.prev {
		if nsCur.itemtype == nsTypeLabel && nsCur.name == name {
			return nsCur
		}
	}
	return nil
}

// nsFindNearestLoop is plpgsql_ns_find_nearest_loop.
func (c *compiler) nsFindNearestLoop(nsCur *nsItem) *nsItem {
	for ; nsCur != nil; nsCur = nsCur.prev {
		if nsCur.itemtype == nsTypeLabel && nsCur.itemno == int(labelLoop) {
			return nsCur
		}
	}
	return nil
}

// --- datums ------------------------------------------------------------------

// startDatums is plpgsql_start_datums.
func (c *compiler) startDatums() {
	c.datums = nil
	c.datumsLast = 0
}

// addDatum is plpgsql_adddatum.
func (c *compiler) addDatum(d datum) {
	d.setDatumNo(len(c.datums))
	c.datums = append(c.datums, d)
}

// finishDatums is plpgsql_finish_datums.
func (c *compiler) finishDatums() {
	c.fn.datums = append([]datum(nil), c.datums...)
}

// addInitdatums is plpgsql_add_initdatums; varnos == nil forgets.
func (c *compiler) addInitdatums(collect bool) []int {
	var varnos []int
	for i := c.datumsLast; i < len(c.datums); i++ {
		switch c.datums[i].datumType() {
		case dtypeVar, dtypeRec:
			if collect {
				varnos = append(varnos, c.datums[i].datumNo())
			}
		}
	}
	c.datumsLast = len(c.datums)
	return varnos
}

// --- variable building -------------------------------------------------------

// buildVariable is plpgsql_build_variable.
func (c *compiler) buildVariable(refname string, lineno int, dtype *plType, add2namespace bool) plVariable {
	switch dtype.ttype {
	case ttypeScalar:
		v := &plVar{
			refname:              refname,
			lineno:               lineno,
			datatype:             dtype,
			cursorExplicitArgrow: 0,
		}
		c.addDatum(v)
		if add2namespace {
			c.nsAdditem(nsTypeVar, v.dno, refname)
		}
		return v
	case ttypeRec:
		return c.buildRecord(refname, lineno, dtype, dtype.typoid, add2namespace)
	case ttypePseudo:
		panic(&compileError{
			Message:  `variable "` + refname + `" has pseudo-type ` + formatTypeBe(dtype.typoid),
			Filename: plCompFilename,
			Funcname: "plpgsql_build_variable",
		})
	}
	return nil
}

// buildRecord is plpgsql_build_record.
func (c *compiler) buildRecord(refname string, lineno int, dtype *plType, rectypeid uint32, add2namespace bool) *plRec {
	rec := &plRec{
		refname:    refname,
		lineno:     lineno,
		datatype:   dtype,
		rectypeid:  rectypeid,
		firstfield: -1,
	}
	c.addDatum(rec)
	if add2namespace {
		c.nsAdditem(nsTypeRec, rec.dno, rec.refname)
	}
	return rec
}

// buildRecfield is plpgsql_build_recfield.
func (c *compiler) buildRecfield(rec *plRec, fldname string) *plRecfield {
	for i := rec.firstfield; i >= 0; {
		fld := c.datums[i].(*plRecfield)
		if fld.fieldname == fldname {
			return fld
		}
		i = fld.nextfield
	}
	recfield := &plRecfield{
		fieldname:   fldname,
		recparentno: rec.dno,
	}
	c.addDatum(recfield)
	recfield.nextfield = rec.firstfield
	rec.firstfield = recfield.dno
	return recfield
}

// buildDatatypeArrayof is plpgsql_build_datatype_arrayof (real at 18).
func (c *compiler) buildDatatypeArrayof(dtype *plType) *plType {
	// If it's already an array type, use it as-is: Postgres doesn't do
	// nested arrays.
	if dtype.typisarray {
		return dtype
	}
	arrayTypeid := getArrayType(dtype.typoid)
	if arrayTypeid == 0 {
		panic(compErr("plpgsql_build_datatype_arrayof",
			"could not find array type for data type %s", formatTypeBe(dtype.typoid)))
	}
	// Note we inherit typmod and collation, if any, from the element type.
	return c.plpgsqlBuildDatatype(arrayTypeid, dtype.atttypmod, dtype.collation, nil)
}

// quoteQualifiedIdentifier is ruleutils.c's quote_qualified_identifier.
func quoteQualifiedIdentifier(qualifier, ident string) string {
	if qualifier != "" {
		return deparse.QuoteIdentifier(qualifier) + "." + deparse.QuoteIdentifier(ident)
	}
	return deparse.QuoteIdentifier(ident)
}

// --- word lookup (pl_comp.c) -------------------------------------------------

// parseWord is plpgsql_parse_word.
func (c *compiler) parseWord(word1 string, quoted bool, lookup bool, wdatum *plWdatum, word *plWord) bool {
	if lookup && c.scan.identifierLookup == lookupNormal {
		if ns, _ := c.nsLookup(c.nsTop, false, word1, "", ""); ns != nil {
			switch ns.itemtype {
			case nsTypeVar, nsTypeRec:
				wdatum.datum = c.datums[ns.itemno]
				wdatum.ident = word1
				wdatum.quoted = quoted
				wdatum.idents = nil
				return true
			}
		}
	}
	word.ident = word1
	word.quoted = quoted
	return false
}

// parseDblword is plpgsql_parse_dblword.
func (c *compiler) parseDblword(word1, word2 string, wdatum *plWdatum, cword *[]string) bool {
	idents := []string{word1, word2}
	if c.scan.identifierLookup != lookupDeclare {
		ns, nnames := c.nsLookup(c.nsTop, false, word1, word2, "")
		if ns != nil {
			switch ns.itemtype {
			case nsTypeVar:
				// Block-qualified reference to scalar variable.
				wdatum.datum = c.datums[ns.itemno]
				wdatum.ident = ""
				wdatum.quoted = false
				wdatum.idents = idents
				return true
			case nsTypeRec:
				if nnames == 1 {
					// First word is a record name; second could be a field.
					rec := c.datums[ns.itemno].(*plRec)
					wdatum.datum = c.buildRecfield(rec, word2)
				} else {
					// Block-qualified reference to record variable.
					wdatum.datum = c.datums[ns.itemno]
				}
				wdatum.ident = ""
				wdatum.quoted = false
				wdatum.idents = idents
				return true
			}
		}
	}
	*cword = idents
	return false
}

// parseTripword is plpgsql_parse_tripword.
func (c *compiler) parseTripword(word1, word2, word3 string, wdatum *plWdatum, cword *[]string) bool {
	if c.scan.identifierLookup != lookupDeclare {
		ns, nnames := c.nsLookup(c.nsTop, false, word1, word2, word3)
		if ns != nil && ns.itemtype == nsTypeRec {
			rec := c.datums[ns.itemno].(*plRec)
			var idents []string
			var fld *plRecfield
			if nnames == 1 {
				fld = c.buildRecfield(rec, word2)
				idents = []string{word1, word2}
			} else {
				fld = c.buildRecfield(rec, word3)
				idents = []string{word1, word2, word3}
			}
			wdatum.datum = fld
			wdatum.ident = ""
			wdatum.quoted = false
			wdatum.idents = idents
			return true
		}
	}
	*cword = []string{word1, word2, word3}
	return false
}

// %TYPE / %ROWTYPE lookups are mocked in the vendored build to stubs that
// keep the reference text as the typname ("<ident>%TYPE" and friends).
func (c *compiler) parseWordType(ident string) *plType {
	return &plType{typname: ident + "%TYPE", ttype: ttypeScalar}
}

func (c *compiler) parseWordRowType(ident string) *plType {
	return &plType{typname: ident + "%rowtype", ttype: ttypeScalar}
}

func (c *compiler) parseCwordType(idents []string) *plType {
	return &plType{typname: nameListToString(idents) + "%TYPE", ttype: ttypeScalar}
}

func (c *compiler) parseCwordRowType(idents []string) *plType {
	return &plType{typname: nameListToString(idents) + "%rowtype", ttype: ttypeScalar}
}

// --- exception conditions ----------------------------------------------------

// recognizeErrCondition is plpgsql_recognize_err_condition (the returned
// SQLSTATE value is never serialized, so only validity matters).
func recognizeErrCondition(condname string, allowSqlstate bool) {
	if allowSqlstate && len(condname) == 5 && isSqlstate(condname) {
		return
	}
	if errConditions[condname] > 0 {
		return
	}
	panic(&compileError{
		Message:  `unrecognized exception condition "` + condname + `"`,
		Filename: plCompFilename,
		Funcname: "plpgsql_recognize_err_condition",
	})
}

// parseErrCondition is plpgsql_parse_err_condition: one chain node per
// exception_label_map entry with the name (duplicates produce chains).
func parseErrCondition(condname string) *plCondition {
	if condname == "others" {
		return &plCondition{condname: condname}
	}
	n := errConditions[condname]
	if n == 0 {
		panic(&compileError{
			Message:  `unrecognized exception condition "` + condname + `"`,
			Filename: plCompFilename,
			Funcname: "plpgsql_parse_err_condition",
		})
	}
	var prev *plCondition
	for i := 0; i < n; i++ {
		prev = &plCondition{condname: condname, next: prev}
	}
	return prev
}

func isSqlstate(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !(c >= '0' && c <= '9' || c >= 'A' && c <= 'Z') {
			return false
		}
	}
	return true
}

// nameListToString is NameListToString over plain identifier lists.
func nameListToString(names []string) string {
	out := ""
	for i, n := range names {
		if i > 0 {
			out += "."
		}
		out += n
	}
	return out
}
