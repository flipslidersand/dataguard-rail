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

// txnBatchSize は 1 トランザクションあたりの最大 violation 件数。
// BadgerDB の ErrTxnTooBig を避けるために小さく保つ。
const txnBatchSize = 1000

// SaveViolations は violations をバッチトランザクションで保存する。
// txnBatchSize 件ごと、または ErrTxnTooBig 発生時に commit & 再開する。
func (s *Store) SaveViolations(violations []engine.Violation) error {
	txn := s.db.NewTransaction(true)
	committed := 0
	for i, v := range violations {
		data, err := json.Marshal(v)
		if err != nil {
			txn.Discard()
			return fmt.Errorf("marshal violation %s: %w", v.ID, err)
		}

	retry:
		if err := txn.Set(key(v), data); err != nil {
			if err == badger.ErrTxnTooBig {
				if commitErr := txn.Commit(); commitErr != nil {
					return fmt.Errorf("commit batch at index %d: %w", committed, commitErr)
				}
				committed = i
				txn = s.db.NewTransaction(true)
				goto retry
			}
			txn.Discard()
			return fmt.Errorf("set violation %s: %w", v.ID, err)
		}

		// バッチサイズに達したら commit して新しいトランザクションを開始する。
		if (i+1)%txnBatchSize == 0 {
			if commitErr := txn.Commit(); commitErr != nil {
				return fmt.Errorf("commit batch at index %d: %w", i, commitErr)
			}
			committed = i + 1
			txn = s.db.NewTransaction(true)
		}
	}
	return txn.Commit()
}

// CountViolations は保存済みの violation 件数をキーオンリースキャンで返す。
func (s *Store) CountViolations() (int, error) {
	count := 0
	err := s.db.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.PrefetchValues = false
		it := txn.NewIterator(opts)
		defer it.Close()
		prefix := []byte(keyPrefix)
		for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
			count++
		}
		return nil
	})
	if err != nil {
		return 0, fmt.Errorf("count violations: %w", err)
	}
	return count, nil
}

// ListViolationsPaged は offset から最大 limit 件の violation を返す。
// limit <= 0 は制限なし。
func (s *Store) ListViolationsPaged(limit, offset int) ([]engine.Violation, error) {
	var out []engine.Violation
	err := s.db.View(func(txn *badger.Txn) error {
		it := txn.NewIterator(badger.DefaultIteratorOptions)
		defer it.Close()
		prefix := []byte(keyPrefix)
		skipped := 0
		for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
			if skipped < offset {
				skipped++
				continue
			}
			if limit > 0 && len(out) >= limit {
				break
			}
			if err := it.Item().Value(func(val []byte) error {
				var v engine.Violation
				if err := json.Unmarshal(val, &v); err != nil {
					return err
				}
				out = append(out, v)
				return nil
			}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("list violations paged: %w", err)
	}
	return out, nil
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
