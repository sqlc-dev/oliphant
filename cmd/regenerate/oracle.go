package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
)

// Oracle is a running instance of the pinned cgo oracle (oracle/ module).
type Oracle struct {
	cmd *exec.Cmd
	in  io.WriteCloser
	enc *json.Encoder
	dec *json.Decoder
}

type oracleReq struct {
	Op            string `json:"op"`
	SQL           string `json:"sql"`
	TrimSpace     bool   `json:"trim_space,omitempty"`
	TruncateLimit *int   `json:"truncate_limit,omitempty"`
}

type oracleError struct {
	Message   string `json:"message"`
	Cursorpos int    `json:"cursorpos,omitempty"`
	Filename  string `json:"filename,omitempty"`
	Funcname  string `json:"funcname,omitempty"`
	Context   string `json:"context,omitempty"`
}

type oracleToken struct {
	Start       int32  `json:"start"`
	End         int32  `json:"end"`
	Token       string `json:"token"`
	KeywordKind string `json:"keyword_kind"`
}

type oracleRes struct {
	Text    *string        `json:"text"`
	Stmts   []string       `json:"stmts"`
	Tokens  []oracleToken  `json:"tokens"`
	Summary *oracleSummary `json:"summary"`
	Err     *oracleError   `json:"error"`
}

type oracleSummaryTable struct {
	Name       string `json:"name"`
	SchemaName string `json:"schema_name"`
	TableName  string `json:"table_name"`
	Context    string `json:"context"`
}

type oracleSummaryFunction struct {
	Name         string `json:"name"`
	FunctionName string `json:"function_name"`
	SchemaName   string `json:"schema_name"`
	Context      string `json:"context"`
}

type oracleSummaryFilterColumn struct {
	SchemaName string `json:"schema_name"`
	TableName  string `json:"table_name"`
	Column     string `json:"column"`
}

type oracleSummary struct {
	Tables         []oracleSummaryTable        `json:"tables"`
	Aliases        map[string]string           `json:"aliases"`
	CteNames       []string                    `json:"cte_names"`
	Functions      []oracleSummaryFunction     `json:"functions"`
	FilterColumns  []oracleSummaryFilterColumn `json:"filter_columns"`
	StatementTypes []string                    `json:"statement_types"`
	TruncatedQuery string                      `json:"truncated_query"`
}

// StartOracle builds (if needed) and starts the oracle binary. bin may name a
// prebuilt binary; when empty, the oracle module is built with `go build`
// into a temp location — the only place cgo ever compiles.
func StartOracle(oracleDir, bin string) (*Oracle, error) {
	if bin == "" {
		bin = filepath.Join(os.TempDir(), "oliphant-oracle")
		build := exec.Command("go", "build", "-o", bin, ".")
		build.Dir = oracleDir
		build.Env = append(os.Environ(), "CGO_ENABLED=1")
		build.Stdout = os.Stderr
		build.Stderr = os.Stderr
		fmt.Fprintln(os.Stderr, "regenerate: building oracle (cgo, several minutes on a cold cache)...")
		if err := build.Run(); err != nil {
			return nil, fmt.Errorf("build oracle: %w", err)
		}
	}
	cmd := exec.Command(bin)
	cmd.Stderr = os.Stderr
	in, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return &Oracle{
		cmd: cmd,
		in:  in,
		enc: json.NewEncoder(in),
		dec: json.NewDecoder(bufio.NewReaderSize(out, 1<<20)),
	}, nil
}

func (o *Oracle) Close() error {
	o.in.Close()
	return o.cmd.Wait()
}

func (o *Oracle) call(r oracleReq) (oracleRes, error) {
	var res oracleRes
	if err := o.enc.Encode(r); err != nil {
		return res, fmt.Errorf("oracle request: %w", err)
	}
	if err := o.dec.Decode(&res); err != nil {
		return res, fmt.Errorf("oracle response (op=%s): %w", r.Op, err)
	}
	return res, nil
}

// Text runs a text-producing op (parse_json, normalize, normalize_utility,
// fingerprint, deparse) and returns the text or the error result.
func (o *Oracle) Text(op, sql string) (string, *oracleError, error) {
	res, err := o.call(oracleReq{Op: op, SQL: sql})
	if err != nil {
		return "", nil, err
	}
	if res.Err != nil {
		return "", res.Err, nil
	}
	if res.Text == nil {
		return "", nil, fmt.Errorf("oracle op %s returned neither text nor error", op)
	}
	return *res.Text, nil, nil
}

func (o *Oracle) Scan(sql string) ([]oracleToken, *oracleError, error) {
	res, err := o.call(oracleReq{Op: "scan", SQL: sql})
	if err != nil {
		return nil, nil, err
	}
	return res.Tokens, res.Err, nil
}

// Summary runs the summary op at the given truncation limit (-1 disables
// truncation, matching pg_query.Summary).
func (o *Oracle) Summary(sql string, truncateLimit int) (*oracleSummary, *oracleError, error) {
	res, err := o.call(oracleReq{Op: "summary", SQL: sql, TruncateLimit: &truncateLimit})
	if err != nil {
		return nil, nil, err
	}
	if res.Err != nil {
		return nil, res.Err, nil
	}
	if res.Summary == nil {
		return nil, nil, fmt.Errorf("oracle op summary returned neither summary nor error")
	}
	return res.Summary, nil, nil
}

func (o *Oracle) Split(op, sql string, trimSpace bool) ([]string, *oracleError, error) {
	res, err := o.call(oracleReq{Op: op, SQL: sql, TrimSpace: trimSpace})
	if err != nil {
		return nil, nil, err
	}
	return res.Stmts, res.Err, nil
}
