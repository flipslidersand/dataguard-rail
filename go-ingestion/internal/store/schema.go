package store

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	badger "github.com/dgraph-io/badger/v4"
)

const schemaPrefix = "schema:"

// ColumnDef はテーブルの列定義。
type ColumnDef struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	Nullable bool   `json:"nullable"`
}

// SchemaSnapshot は特定時刻のテーブルスキーマスナップショット。
type SchemaSnapshot struct {
	Table      string      `json:"table"`
	CapturedAt string      `json:"captured_at"` // RFC3339
	Columns    []ColumnDef `json:"columns"`
}

// ColumnChange は変更前後の列定義を保持する。
type ColumnChange struct {
	Name string `json:"name"`
	From string `json:"from"`
	To   string `json:"to"`
}

// SchemaDiff は2スナップショット間の差分。
type SchemaDiff struct {
	Table      string         `json:"table"`
	DetectedAt string         `json:"detected_at"` // RFC3339
	Added      []ColumnDef    `json:"added"`
	Dropped    []ColumnDef    `json:"dropped"`
	Changed    []ColumnChange `json:"changed"`
}

func schemaKey(table, capturedAt string) []byte {
	return []byte(fmt.Sprintf("%s%s:%s", schemaPrefix, table, capturedAt))
}

// SaveSnapshot はスキーマスナップショットを保存する。
func (s *Store) SaveSnapshot(snap SchemaSnapshot) error {
	data, err := json.Marshal(snap)
	if err != nil {
		return fmt.Errorf("marshal snapshot: %w", err)
	}
	return s.db.Update(func(txn *badger.Txn) error {
		return txn.Set(schemaKey(snap.Table, snap.CapturedAt), data)
	})
}

// ListSnapshots は指定テーブルのスナップショットを時系列順で返す。
func (s *Store) ListSnapshots(table string) ([]SchemaSnapshot, error) {
	prefix := []byte(fmt.Sprintf("%s%s:", schemaPrefix, table))
	var out []SchemaSnapshot
	err := s.db.View(func(txn *badger.Txn) error {
		it := txn.NewIterator(badger.DefaultIteratorOptions)
		defer it.Close()
		for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
			err := it.Item().Value(func(val []byte) error {
				var snap SchemaSnapshot
				if err := json.Unmarshal(val, &snap); err != nil {
					return err
				}
				out = append(out, snap)
				return nil
			})
			if err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("list snapshots %q: %w", table, err)
	}
	// BadgerDB はキー順なので captured_at の辞書順 = 時系列順
	sort.Slice(out, func(i, j int) bool { return out[i].CapturedAt < out[j].CapturedAt })
	return out, nil
}

// LatestDiff は最新2スナップショットを比較して差分を返す。スナップショットが1件以下なら nil。
func (s *Store) LatestDiff(table string) (*SchemaDiff, error) {
	snaps, err := s.ListSnapshots(table)
	if err != nil {
		return nil, err
	}
	if len(snaps) < 2 {
		return nil, nil
	}
	prev := snaps[len(snaps)-2]
	curr := snaps[len(snaps)-1]
	return diffSnapshots(prev, curr), nil
}

// ListDiffs は全テーブルの最新差分を返す。差分がないテーブルは含まない。
func (s *Store) ListDiffs() ([]SchemaDiff, error) {
	// schema: プレフィックス全件を走査してテーブル名を収集
	tables := map[string]struct{}{}
	prefix := []byte(schemaPrefix)
	err := s.db.View(func(txn *badger.Txn) error {
		it := txn.NewIterator(badger.DefaultIteratorOptions)
		defer it.Close()
		for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
			k := string(it.Item().Key())
			// "schema:<table>:<captured_at>" → table 抽出
			rest := strings.TrimPrefix(k, schemaPrefix)
			if idx := strings.Index(rest, ":"); idx >= 0 {
				tables[rest[:idx]] = struct{}{}
			}
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("list diffs scan: %w", err)
	}

	var diffs []SchemaDiff
	for t := range tables {
		d, err := s.LatestDiff(t)
		if err != nil {
			return nil, err
		}
		if d != nil {
			diffs = append(diffs, *d)
		}
	}
	return diffs, nil
}

// diffSnapshots は prev → curr の差分を計算する。
func diffSnapshots(prev, curr SchemaSnapshot) *SchemaDiff {
	prevMap := make(map[string]ColumnDef, len(prev.Columns))
	for _, c := range prev.Columns {
		prevMap[c.Name] = c
	}
	currMap := make(map[string]ColumnDef, len(curr.Columns))
	for _, c := range curr.Columns {
		currMap[c.Name] = c
	}

	diff := &SchemaDiff{
		Table:      curr.Table,
		DetectedAt: curr.CapturedAt,
		Added:      []ColumnDef{},
		Dropped:    []ColumnDef{},
		Changed:    []ColumnChange{},
	}
	for name, c := range currMap {
		if p, ok := prevMap[name]; !ok {
			diff.Added = append(diff.Added, c)
		} else if p.Type != c.Type {
			diff.Changed = append(diff.Changed, ColumnChange{Name: name, From: p.Type, To: c.Type})
		}
	}
	for name, c := range prevMap {
		if _, ok := currMap[name]; !ok {
			diff.Dropped = append(diff.Dropped, c)
		}
	}
	return diff
}
