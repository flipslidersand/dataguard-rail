// Package engine は Rust の dataguard-engine バイナリを exec で呼び出す薄いラッパ。
// ADR-003「exec-first, gRPC-later」に基づき、まずサブプロセス連携で MVP を通す。
package engine

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
)

// DefaultBin は bin 未指定時の既定値。絶対パスでないため New() に渡すとエラーになる。
// 呼び出し元はインストール先の絶対パス (例: "/usr/local/bin/dataguard-engine") を指定すること。
const DefaultBin = "dataguard-engine"

// Violation は check の出力 (Rust 側 data-model.md の Violation に一致)。
type Violation struct {
	ID         string `json:"id"`
	Rule       string `json:"rule"`
	Table      string `json:"table"`
	Row        int    `json:"row"`
	Column     string `json:"column"`
	Value      string `json:"value"`
	DetectedAt string `json:"detected_at"`
}

// Runner は dataguard-engine の実行者。
type Runner struct {
	Bin string
}

// New は Runner を返す。bin は絶対パスでなければならない。
// 空文字列を渡した場合は DefaultBin を使うが、絶対パスでないためエラーになる。
// 実行前に run() 内で filepath.IsAbs を検証する。
func New(bin string) *Runner {
	if bin == "" {
		bin = DefaultBin
	}
	return &Runner{Bin: bin}
}

// Check は `dataguard-engine check --input <csv> --rules <yaml> --out -` を実行し、
// stdout の violations JSON をパースして返す。
func (r *Runner) Check(inputCSV, rulesPath string) ([]Violation, error) {
	out, err := r.run("check", "--input", inputCSV, "--rules", rulesPath, "--out", "-")
	if err != nil {
		return nil, err
	}
	var violations []Violation
	if err := json.Unmarshal(out, &violations); err != nil {
		return nil, fmt.Errorf("parse violations json: %w", err)
	}
	return violations, nil
}

// Analyze は `dataguard-engine analyze --sql <file> --out -` を実行し、
// lineage JSON をそのまま返す (Go 側で構造を固定しないため RawMessage)。
func (r *Runner) Analyze(sqlPath string) (json.RawMessage, error) {
	out, err := r.run("analyze", "--sql", sqlPath, "--out", "-")
	if err != nil {
		return nil, err
	}
	return json.RawMessage(out), nil
}

// run はサブプロセスを実行し stdout を返す。非ゼロ終了時は stderr を含めてエラー化する。
// PATH ハイジャック防止のため、Bin は絶対パスでなければならない。
func (r *Runner) run(args ...string) ([]byte, error) {
	if !filepath.IsAbs(r.Bin) {
		return nil, fmt.Errorf("engine: Bin %q は絶対パスでなければなりません (PATH ハイジャック防止)", r.Bin)
	}
	cmd := exec.Command(r.Bin, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("exec %s %v: %w: %s", r.Bin, args, err, stderr.String())
	}
	return stdout.Bytes(), nil
}
