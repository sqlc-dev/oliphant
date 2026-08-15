package testfile

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestRoundTrip(t *testing.T) {
	cases := []Case{
		{Name: "001", Input: "SELECT 1", Expected: `{"version":170007}`},
		// The collision cases the escaping exists for: SQL comment lines that
		// look exactly like the format's markers.
		{Name: "002", Input: "--\n-- banner comment\n--\nSELECT 2;", Expected: "ERROR: nope"},
		{Name: "003", Input: "SELECT '|pipe'\n|literal-pipe-line", Expected: "== not a marker\n--"},
		{Name: "004", Input: "SELECT 3", Expected: "multi\nline\nexpectation"},
	}
	path := filepath.Join(t.TempDir(), "roundtrip.test")
	if err := Write(path, cases); err != nil {
		t.Fatal(err)
	}
	got, err := Read(path)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, cases) {
		t.Fatalf("round trip mismatch:\ngot  %#v\nwant %#v", got, cases)
	}

	// Writing twice must produce identical bytes (the reproducibility gate
	// depends on it).
	data1, _ := os.ReadFile(path)
	if err := Write(path, got); err != nil {
		t.Fatal(err)
	}
	data2, _ := os.ReadFile(path)
	if string(data1) != string(data2) {
		t.Fatal("Write is not deterministic")
	}
}

func TestErrorExpectationRoundTrip(t *testing.T) {
	e := ErrorExpectation{
		Message:   `syntax error at or near "$"`,
		Cursorpos: 8,
		Filename:  "scan.l",
		Funcname:  "scanner_yyerror",
	}
	rendered := RenderError(e)
	if !IsError(rendered) {
		t.Fatal("rendered error not detected as error expectation")
	}
	got, err := ParseError(rendered)
	if err != nil {
		t.Fatal(err)
	}
	if got != e {
		t.Fatalf("round trip mismatch: got %+v want %+v", got, e)
	}
}

func TestMetadata(t *testing.T) {
	path := filepath.Join(t.TempDir(), "x.test")
	m, err := ReadMetadata(path)
	if err != nil || len(m.Todo) != 0 {
		t.Fatalf("missing sidecar should read as empty, got %+v, %v", m, err)
	}
	if err := WriteMetadata(path, Metadata{Todo: []string{"002", "001"}}); err != nil {
		t.Fatal(err)
	}
	m, err = ReadMetadata(path)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(m.Todo, []string{"001", "002"}) {
		t.Fatalf("todo not sorted: %v", m.Todo)
	}
}
