// Package ingester はデータソースからレコードを読み込み、Rust engine に渡せる
// CSV 形式へ正規化する。Phase 3 では CSV ソースを実装し、PostgreSQL は #10 で追加する。
package ingester

import (
	"encoding/csv"
	"fmt"
	"os"
	"path/filepath"
)

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

// WriteTempCSV は Dataset を一時 CSV に書き出し、パスと後片付け関数を返す。
// PostgreSQL ソースでもこの経由で Rust engine に渡せるように共通化している。
func WriteTempCSV(ds *Dataset, dir string) (path string, cleanup func(), err error) {
	f, err := os.CreateTemp(dir, "dataguard-*.csv")
	if err != nil {
		return "", nil, fmt.Errorf("create temp csv: %w", err)
	}
	path = f.Name()
	cleanup = func() { _ = os.Remove(path) }

	w := csv.NewWriter(f)
	if err := w.Write(ds.Headers); err != nil {
		f.Close()
		cleanup()
		return "", nil, fmt.Errorf("write header: %w", err)
	}
	if err := w.WriteAll(ds.Rows); err != nil { // WriteAll は内部で Flush する
		f.Close()
		cleanup()
		return "", nil, fmt.Errorf("write rows: %w", err)
	}
	if err := f.Close(); err != nil {
		cleanup()
		return "", nil, fmt.Errorf("close temp csv: %w", err)
	}
	return path, cleanup, nil
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
