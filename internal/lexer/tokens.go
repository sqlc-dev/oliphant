package lexer

import "github.com/sqlc-dev/oliphant/ast"

// The protobuf Token enum is generated from the pinned grammar's bison token
// list, so enum value names equal bison token names and enum numbers equal
// bison token numbers. That makes ast.Token_value the authoritative
// kwlist-token-name → token-number mapping (scan.l's ScanKeywordTokens).
type keywordToken struct {
	token ast.Token
	kind  ast.KeywordKind
}

var keywordTokens = buildKeywordTokens()

func buildKeywordTokens() map[string]keywordToken {
	m := make(map[string]keywordToken, len(Keywords))
	for name, kw := range Keywords {
		v, ok := ast.Token_value[kw.Token]
		if !ok {
			panic("lexer: keyword token " + kw.Token + " missing from ast.Token enum")
		}
		// KeywordCategory maps 1:1 onto KeywordKind minus NO_KEYWORD
		// (pg_query_scan.c: keyword_kind = category + 1).
		m[name] = keywordToken{token: ast.Token(v), kind: ast.KeywordKind(kw.Category) + 1}
	}
	return m
}
