// Package testfile reads and writes the consolidated corpus format shared by
// the sqlc parser family: cases separated by `== <name>` lines, input and
// expectation separated by a `--` line, with a metadata.json sidecar per file
// tracking which cases are still todo.
//
// Layout of a .test file:
//
//	== 001
//	SELECT 1
//	--
//	{"version":170007,...}
//	== 002
//	...
//
// Because the inputs are SQL — where a line consisting of exactly `--` is a
// perfectly ordinary comment — content lines that would collide with the
// markers are escaped with a leading `|` (and lines starting with `|` are
// escaped too, making the encoding a bijection). Files are written only by
// cmd/regenerate; expectations are never edited by hand.
package testfile

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
)

// Case is one corpus entry: an input SQL string and its oracle-derived
// expectation (a protobuf-JSON tree, `ERROR: ...` block, token stream, …).
type Case struct {
	Name     string
	Input    string
	Expected string
}

// Read parses a .test file.
func Read(path string) ([]Case, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cases []Case
	var cur *Case
	var section []string
	inExpected := false

	flush := func() {
		if cur == nil {
			return
		}
		text := strings.Join(section, "\n")
		if inExpected {
			cur.Expected = text
		} else {
			cur.Input = text
		}
		cases = append(cases, *cur)
	}

	lines := strings.Split(string(data), "\n")
	// A trailing newline at EOF produces one empty trailing element; drop it.
	if n := len(lines); n > 0 && lines[n-1] == "" {
		lines = lines[:n-1]
	}
	for i, line := range lines {
		switch {
		case strings.HasPrefix(line, "== "):
			flush()
			cur = &Case{Name: strings.TrimPrefix(line, "== ")}
			section = nil
			inExpected = false
		case line == "--" && cur != nil && !inExpected:
			cur.Input = strings.Join(section, "\n")
			section = nil
			inExpected = true
		default:
			if cur == nil {
				return nil, fmt.Errorf("%s:%d: content before first `== ` marker", path, i+1)
			}
			section = append(section, unescapeLine(line))
		}
	}
	flush()
	return cases, nil
}

// Write renders cases into the .test format.
func Write(path string, cases []Case) error {
	var b strings.Builder
	for _, c := range cases {
		b.WriteString("== ")
		b.WriteString(c.Name)
		b.WriteByte('\n')
		writeSection(&b, c.Input)
		b.WriteString("--\n")
		writeSection(&b, c.Expected)
	}
	return os.WriteFile(path, []byte(b.String()), 0o644)
}

func writeSection(b *strings.Builder, text string) {
	for _, line := range strings.Split(text, "\n") {
		b.WriteString(escapeLine(line))
		b.WriteByte('\n')
	}
}

func escapeLine(line string) string {
	if line == "--" || strings.HasPrefix(line, "== ") || strings.HasPrefix(line, "|") {
		return "|" + line
	}
	return line
}

func unescapeLine(line string) string {
	if strings.HasPrefix(line, "|") {
		return line[1:]
	}
	return line
}

// Metadata is the per-file sidecar. Todo lists the case names not yet
// passing; `go test ./parser -check-parse` removes newly passing entries,
// and a case absent from Todo that fails is a regression, never a new todo.
type Metadata struct {
	Todo []string `json:"todo"`
}

func metadataPath(testPath string) string {
	return strings.TrimSuffix(testPath, ".test") + ".metadata.json"
}

// ReadMetadata loads the sidecar for a .test file; a missing sidecar means
// nothing is todo.
func ReadMetadata(testPath string) (Metadata, error) {
	var m Metadata
	data, err := os.ReadFile(metadataPath(testPath))
	if os.IsNotExist(err) {
		return m, nil
	}
	if err != nil {
		return m, err
	}
	err = json.Unmarshal(data, &m)
	return m, err
}

// WriteMetadata writes the sidecar (sorted, stable bytes).
func WriteMetadata(testPath string, m Metadata) error {
	sort.Strings(m.Todo)
	if m.Todo == nil {
		m.Todo = []string{}
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(metadataPath(testPath), append(data, '\n'), 0o644)
}
