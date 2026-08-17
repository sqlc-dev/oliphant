// The cgo twin of the repo root's benchmark_test.go: identical benchmark
// names and inputs, run against the pinned pg_query_go v6.2.2 (the real
// libpg_query via cgo). Diff the two runs with benchstat:
//
//	go test -bench . -benchmem -run '^$' .. > pure.txt   (repo root)
//	go test -bench . -benchmem -run '^$' .  > cgo.txt    (this dir)
//	benchstat cgo.txt pure.txt
package main

import (
	"os"
	"strings"
	"testing"

	pg_query "github.com/pganalyze/pg_query_go/v6"
	"github.com/pganalyze/pg_query_go/v6/parser"
)

// Prevent compiler optimizations by assigning all results to global variables
// (same trick as upstream's benchmark file).
var (
	benchErr  error
	resultStr []byte
	resultS   string
	resultRes *pg_query.ParseResult
)

const stressCaseFile = "../parser/testdata/fingerprint/libpg_query.test"

// stressInput returns the largest input in the fingerprint suite — the
// 1.1 MB multi-VALUES INSERT (case 073). Minimal reimplementation of
// internal/testfile.Read (this module must not import the main module).
func stressInput(b *testing.B) string {
	b.Helper()
	data, err := os.ReadFile(stressCaseFile)
	if err != nil {
		b.Fatal(err)
	}
	var biggest, cur []string
	inInput := false
	flush := func() {
		if joinedLen(cur) > joinedLen(biggest) {
			biggest = cur
		}
		cur = nil
	}
	for _, line := range strings.Split(string(data), "\n") {
		switch {
		case strings.HasPrefix(line, "== "):
			flush()
			inInput = true
		case line == "--":
			inInput = false
		case inInput:
			cur = append(cur, strings.TrimPrefix(line, "|"))
		}
	}
	flush()
	return strings.Join(biggest, "\n")
}

func joinedLen(lines []string) int {
	n := 0
	for _, l := range lines {
		n += len(l) + 1
	}
	return n
}

func benchmarkParse(input string, b *testing.B) {
	for i := 0; i < b.N; i++ {
		resultRes, benchErr = pg_query.Parse(input)
		if benchErr != nil {
			b.Errorf("Benchmark produced error %s\n\n", benchErr)
		}
	}
}

func benchmarkParseParallel(input string, b *testing.B) {
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_, err := pg_query.Parse(input)
			if err != nil {
				b.Errorf("Benchmark produced error %s\n\n", err)
			}
		}
	})
}

func benchmarkRawParse(input string, b *testing.B) {
	for i := 0; i < b.N; i++ {
		resultStr, benchErr = parser.ParseToProtobuf(input)
		if benchErr != nil {
			b.Errorf("Benchmark produced error %s\n\n", benchErr)
		}
		if len(resultStr) == 0 {
			b.Errorf("Benchmark produced empty result\n\n")
		}
	}
}

func benchmarkRawParseParallel(input string, b *testing.B) {
	b.RunParallel(func(pb *testing.PB) {
		var str []byte
		var err error
		for pb.Next() {
			str, err = parser.ParseToProtobuf(input)
			if err != nil {
				b.Errorf("Benchmark produced error %s\n\n", err)
			}
			if len(str) == 0 {
				b.Errorf("Benchmark produced empty result\n\n")
			}
		}
	})
}

func benchmarkParseToJSON(input string, b *testing.B) {
	for i := 0; i < b.N; i++ {
		resultS, benchErr = pg_query.ParseToJSON(input)
		if benchErr != nil {
			b.Errorf("Benchmark produced error %s\n\n", benchErr)
		}
	}
}

func benchmarkScan(input string, b *testing.B) {
	for i := 0; i < b.N; i++ {
		_, benchErr = pg_query.Scan(input)
		if benchErr != nil {
			b.Errorf("Benchmark produced error %s\n\n", benchErr)
		}
	}
}

func benchmarkFingerprint(input string, b *testing.B) {
	var str string
	for i := 0; i < b.N; i++ {
		str, benchErr = pg_query.Fingerprint(input)
		if benchErr != nil {
			b.Errorf("Benchmark produced error %s\n\n", benchErr)
		}
		if str == "" {
			b.Errorf("Benchmark produced empty result\n\n")
		}
	}
}

func benchmarkNormalize(input string, b *testing.B) {
	for i := 0; i < b.N; i++ {
		resultS, benchErr = pg_query.Normalize(input)
		if benchErr != nil {
			b.Errorf("Benchmark produced error %s\n\n", benchErr)
		}
		if resultS == "" {
			b.Errorf("Benchmark produced empty result\n\n")
		}
	}
}

func BenchmarkParseSelect1(b *testing.B) {
	benchmarkParse("SELECT 1", b)
}
func BenchmarkParseSelect2(b *testing.B) {
	benchmarkParse("SELECT 1 FROM x WHERE y IN ('a', 'b', 'c')", b)
}
func BenchmarkParseCreateTable(b *testing.B) {
	benchmarkParse("CREATE TABLE types (a float(2), b float(49), c NUMERIC(2, 3), d character(4), e char(5), f varchar(6), g character varying(7))", b)
}

func BenchmarkParseSelect1Parallel(b *testing.B) {
	benchmarkParseParallel("SELECT 1", b)
}
func BenchmarkParseSelect2Parallel(b *testing.B) {
	benchmarkParseParallel("SELECT 1 FROM x WHERE y IN ('a', 'b', 'c')", b)
}
func BenchmarkParseCreateTableParallel(b *testing.B) {
	benchmarkParseParallel("CREATE TABLE types (a float(2), b float(49), c NUMERIC(2, 3), d character(4), e char(5), f varchar(6), g character varying(7))", b)
}

func BenchmarkRawParseSelect1(b *testing.B) {
	benchmarkRawParse("SELECT 1", b)
}
func BenchmarkRawParseSelect2(b *testing.B) {
	benchmarkRawParse("SELECT 1 FROM x WHERE y IN ('a', 'b', 'c')", b)
}
func BenchmarkRawParseCreateTable(b *testing.B) {
	benchmarkRawParse("CREATE TABLE types (a float(2), b float(49), c NUMERIC(2, 3), d character(4), e char(5), f varchar(6), g character varying(7))", b)
}

func BenchmarkRawParseSelect1Parallel(b *testing.B) {
	benchmarkRawParseParallel("SELECT 1", b)
}
func BenchmarkRawParseSelect2Parallel(b *testing.B) {
	benchmarkRawParseParallel("SELECT 1 FROM x WHERE y IN ('a', 'b', 'c')", b)
}
func BenchmarkRawParseCreateTableParallel(b *testing.B) {
	benchmarkRawParseParallel("CREATE TABLE types (a float(2), b float(49), c NUMERIC(2, 3), d character(4), e char(5), f varchar(6), g character varying(7))", b)
}

func BenchmarkFingerprintSelect1(b *testing.B) {
	benchmarkFingerprint("SELECT 1", b)
}
func BenchmarkFingerprintSelect2(b *testing.B) {
	benchmarkFingerprint("SELECT 1 FROM x WHERE y IN ('a', 'b', 'c')", b)
}
func BenchmarkFingerprintCreateTable(b *testing.B) {
	benchmarkFingerprint("CREATE TABLE types (a float(2), b float(49), c NUMERIC(2, 3), d character(4), e char(5), f varchar(6), g character varying(7))", b)
}

func BenchmarkNormalizeSelect1(b *testing.B) {
	benchmarkNormalize("SELECT 1", b)
}
func BenchmarkNormalizeSelect2(b *testing.B) {
	benchmarkNormalize("SELECT 1 FROM x WHERE y IN ('a', 'b', 'c')", b)
}
func BenchmarkNormalizeCreateTable(b *testing.B) {
	benchmarkNormalize("CREATE TABLE types (a float(2), b float(49), c NUMERIC(2, 3), d character(4), e char(5), f varchar(6), g character varying(7))", b)
}

// --- beyond the upstream set ---

func BenchmarkParseToJSONSelect2(b *testing.B) {
	benchmarkParseToJSON("SELECT 1 FROM x WHERE y IN ('a', 'b', 'c')", b)
}
func BenchmarkScanSelect2(b *testing.B) {
	benchmarkScan("SELECT 1 FROM x WHERE y IN ('a', 'b', 'c')", b)
}

func BenchmarkRawParseStress(b *testing.B) {
	input := stressInput(b)
	b.SetBytes(int64(len(input)))
	b.ResetTimer()
	benchmarkRawParse(input, b)
}
func BenchmarkRawParseStressParallel(b *testing.B) {
	input := stressInput(b)
	b.SetBytes(int64(len(input)))
	b.ResetTimer()
	benchmarkRawParseParallel(input, b)
}
func BenchmarkFingerprintStress(b *testing.B) {
	input := stressInput(b)
	b.SetBytes(int64(len(input)))
	b.ResetTimer()
	benchmarkFingerprint(input, b)
}
func BenchmarkNormalizeStress(b *testing.B) {
	input := stressInput(b)
	b.SetBytes(int64(len(input)))
	b.ResetTimer()
	benchmarkNormalize(input, b)
}
func BenchmarkScanStress(b *testing.B) {
	input := stressInput(b)
	b.SetBytes(int64(len(input)))
	b.ResetTimer()
	benchmarkScan(input, b)
}
