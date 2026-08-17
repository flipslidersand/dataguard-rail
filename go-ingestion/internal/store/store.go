// Package store は violations を BadgerDB に永続化する (ADR-004)。
package store

import (
	"encoding/json"
	"fmt"

	badger "github.com/dgraph-io/badger/v4"
	"github.com/flipslidersand/dataguard-rail/internal/engine"
)

// keyPrefix は violation レコードの key プレフィックス (`violation:<table>:<id>`)。
const keyPrefix = "violation:"

// Store は BadgerDB のラッパ。
type Store struct {
	db *badger.DB
}

// Open は指定パスに BadgerDB を開く。ログは抑制する。
func Open(path string) (*Store, error) {
	opts := badger.DefaultOptions(path).WithLogger(nil)
	db, err := badger.Open(opts)
	if err != nil {
		return nil, fmt.Errorf("open badger %q: %w", path, err)
	}
	return &Store{db: db}, nil
}

// Close は DB を閉じる。
func (s *Store) Close() error {
	return s.db.Close()
}

// key は violation の一意キーを組み立てる。
func key(v engine.Violation) []byte {
	return []byte(fmt.Sprintf("%s%s:%s", keyPrefix, v.Table, v.ID))
}

// SaveViolations は violations を 1 トランザクションで保存する。
func (s *Store) SaveViolations(violations []engine.Violation) error {
	return s.db.Update(func(txn *badger.Txn) error {
		for _, v := range violations {
			data, err := json.Marshal(v)
			if err != nil {
				return fmt.Errorf("marshal violation %s: %w", v.ID, err)
			}
			if err := txn.Set(key(v), data); err != nil {
				return fmt.Errorf("set violation %s: %w", v.ID, err)
			}
		}
		return nil
	})
}

// ListViolations は保存済みの全 violation を prefix scan で返す。
func (s *Store) ListViolations() ([]engine.Violation, error) {
	var out []engine.Violation
	err := s.db.View(func(txn *badger.Txn) error {
		it := txn.NewIterator(badger.DefaultIteratorOptions)
		defer it.Close()
		prefix := []byte(keyPrefix)
		for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
			err := it.Item().Value(func(val []byte) error {
				var v engine.Violation
				if err := json.Unmarshal(val, &v); err != nil {
					return err
				}
				out = append(out, v)
				return nil
			})
			if err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("list violations: %w", err)
	}
	return out, nil
}
