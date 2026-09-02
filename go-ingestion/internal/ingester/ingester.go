// Package ingester はデータソースからレコードを読み込み、Rust engine に渡せる
// CSV 形式へ正規化する。Phase 3 では CSV ソースを実装し、PostgreSQL は #10 で追加する。
package ingester

import (
	"bufio"
	"bytes"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// jsonlMaxLineBytes は LoadJSONL で許容する1行の最大バイト数 (10 MiB)。
// bufio.Scanner のデフォルト 64 KB では base64 埋め込み画像などで失敗するため拡張する。
const jsonlMaxLineBytes = 10 << 20 // 10 MiB

// Dataset は取込んだ表形式データの内部表現。
type Dataset struct {
	Headers []string
	Rows    [][]string
}

// LoadCSV は CSV ファイルを読み込み Dataset にする。
func LoadCSV(path string) (*Dataset, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open csv %q: %w", path, err)
	}
	defer f.Close()

	r := csv.NewReader(f)
	r.FieldsPerRecord = -1 // 行ごとの列数差を許容 (検証は engine 側に委ねる)

	records, err := r.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("read csv %q: %w", path, err)
	}
	if len(records) == 0 {
		return nil, fmt.Errorf("csv %q: ヘッダがありません", path)
	}

	return &Dataset{Headers: records[0], Rows: records[1:]}, nil
}

// LoadJSONL は JSON Lines ファイル (1行1オブジェクト) を読み込み Dataset にする。
// カラム順は最初の非空行のキー出現順で確定する。後続行に未知のキーがあれば無視する。
func LoadJSONL(path string) (*Dataset, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open jsonl %q: %w", path, err)
	}
	defer f.Close()

	var lines [][]byte
	scanner := bufio.NewScanner(f)
	// デフォルト 64 KB を 10 MiB に拡張し、base64 埋め込みなど大行のサイレント失敗を防ぐ。
	scanner.Buffer(make([]byte, 0, 64*1024), jsonlMaxLineBytes)
	for scanner.Scan() {
		b := scanner.Bytes()
		if len(b) == 0 {
			continue
		}
		cp := make([]byte, len(b))
		copy(cp, b)
		lines = append(lines, cp)
	}
	if err := scanner.Err(); err != nil {
		if errors.Is(err, bufio.ErrTooLong) {
			return nil, fmt.Errorf("scan jsonl %q: 行サイズが上限 (%d bytes) を超えています (base64 埋め込みや巨大フィールドが含まれている可能性があります): %w", path, jsonlMaxLineBytes, err)
		}
		return nil, fmt.Errorf("scan jsonl %q: %w", path, err)
	}
	if len(lines) == 0 {
		return nil, fmt.Errorf("jsonl %q: 有効な行がありません", path)
	}

	// 最初の行を token レベルで走査してキー挿入順を確定する。
	headers, headerIdx, err := jsonlHeaders(lines[0])
	if err != nil {
		return nil, fmt.Errorf("jsonl %q line 1: %w", path, err)
	}

	rows := make([][]string, 0, len(lines))
	for i, line := range lines {
		var obj map[string]json.RawMessage
		if err := json.Unmarshal(line, &obj); err != nil {
			return nil, fmt.Errorf("jsonl %q line %d: %w", path, i+1, err)
		}
		row := make([]string, len(headers))
		for k, raw := range obj {
			idx, ok := headerIdx[k]
			if !ok {
				continue
			}
			var s string
			if err := json.Unmarshal(raw, &s); err != nil {
				s = string(raw) // 数値・bool・null はそのまま文字列化
			}
			row[idx] = s
		}
		rows = append(rows, row)
	}
	return &Dataset{Headers: headers, Rows: rows}, nil
}

// jsonlHeaders は JSON オブジェクトの1行からキー出現順にヘッダを抽出する。
func jsonlHeaders(line []byte) ([]string, map[string]int, error) {
	dec := json.NewDecoder(bytes.NewReader(line))
	if _, err := dec.Token(); err != nil { // '{'
		return nil, nil, fmt.Errorf("expected '{': %w", err)
	}
	var headers []string
	idx := map[string]int{}
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return nil, nil, err
		}
		key, ok := tok.(string)
		if !ok {
			return nil, nil, fmt.Errorf("expected string key")
		}
		if _, dup := idx[key]; !dup {
			idx[key] = len(headers)
			headers = append(headers, key)
		}
		if err := dec.Decode(new(json.RawMessage)); err != nil { // skip value
			return nil, nil, err
		}
	}
	return headers, idx, nil
}

// WriteTempCSV は Dataset を一時 CSV に書き出し、パスと後片付け関数を返す。
// PostgreSQL ソースでもこの経由で Rust engine に渡せるように共通化している。
// エラー時は関数内で一時ファイルを削除し、cleanup=nil を返す。
// 成功時のみ cleanup クロージャを返すため、呼び出し元は常に defer cleanup() できる。
func WriteTempCSV(ds *Dataset, dir string) (path string, cleanup func(), err error) {
	f, err := os.CreateTemp(dir, "dataguard-*.csv")
	if err != nil {
		return "", nil, fmt.Errorf("create temp csv: %w", err)
	}
	path = f.Name()
	// errCleanup は失敗パスで一時ファイルを確実に削除するヘルパー。
	errCleanup := func() { f.Close(); _ = os.Remove(path) }

	w := csv.NewWriter(f)
	if err := w.Write(ds.Headers); err != nil {
		errCleanup()
		return "", nil, fmt.Errorf("write header: %w", err)
	}
	if err := w.WriteAll(ds.Rows); err != nil {
		errCleanup()
		return "", nil, fmt.Errorf("write rows: %w", err)
	}
	// WriteAll は内部で Flush を呼ぶが、OS レベルの write エラーは
	// Flush 後にしか検出できないケースがあるため w.Error() を明示チェックする。
	w.Flush()
	if err := w.Error(); err != nil {
		errCleanup()
		return "", nil, fmt.Errorf("flush temp csv: %w", err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(path)
		return "", nil, fmt.Errorf("close temp csv: %w", err)
	}
	return path, func() { _ = os.Remove(path) }, nil
}

// TempDir は一時 CSV の出力先ディレクトリを返す (存在しなければ作成)。
func TempDir(base string) (string, error) {
	if base == "" {
		return os.TempDir(), nil
	}
	if err := os.MkdirAll(base, 0o755); err != nil {
		return "", fmt.Errorf("mkdir %q: %w", base, err)
	}
	return filepath.Clean(base), nil
}
