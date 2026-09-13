// Package store は violations を BadgerDB に永続化する (ADR-004)。
package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	badger "github.com/dgraph-io/badger/v4"
	"github.com/flipslidersand/dataguard-rail/internal/engine"
	"go.uber.org/zap"
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

// gcInterval は RunGC が db.RunValueLogGC を試みる間隔 (var なのでテストで上書き可能)。
var gcInterval = 10 * time.Minute

// gcDiscardRatio は RunValueLogGC に渡す破棄率の閾値 (BadgerDB公式ドキュメント推奨値)。
const gcDiscardRatio = 0.5

// RunGC は ctx がキャンセルされるまで gcInterval ごとに BadgerDB の
// value log GC (RunValueLogGC) を実行し続けるブロッキングループ。
// SaveViolations による同一キー上書きで無効化された古い value log
// セグメントを回収しないとディスク使用量が単調増加し続けるため、
// 長時間稼働する daemon モードから goroutine として起動することを想定する
// (単発の `ingest` 実行では不要)。
//
// BadgerDB の一般的な運用パターンに従い、GC 1回で複数セグメントが
// 回収可能な場合に備えて ErrNoRewrite が返るまで RunValueLogGC を
// 連続実行する。ErrNoRewrite (回収対象なし) は正常終了として無視する。
func (s *Store) RunGC(ctx context.Context, log *zap.Logger) {
	if log == nil {
		log = zap.NewNop()
	}
	ticker := time.NewTicker(gcInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			for {
				err := s.db.RunValueLogGC(gcDiscardRatio)
				if err == nil {
					continue
				}
				if !errors.Is(err, badger.ErrNoRewrite) {
					log.Warn("badger value log gc failed", zap.Error(err))
				}
				break
			}
		}
	}
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

// tableKeyPrefix は table 単位の violation キープレフィックスを組み立てる
// (`violation:<table>:`)。key() が既に `violation:<table>:<id>` の形式で
// 保存しているため、table フィルタは追加インデックス無しで prefix scan だけで済む。
func tableKeyPrefix(table string) []byte {
	return []byte(fmt.Sprintf("%s%s:", keyPrefix, table))
}

// CountViolationsByTable は指定 table の violation 件数をキーオンリースキャンで返す。
func (s *Store) CountViolationsByTable(table string) (int, error) {
	count := 0
	err := s.db.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.PrefetchValues = false
		it := txn.NewIterator(opts)
		defer it.Close()
		prefix := tableKeyPrefix(table)
		for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
			count++
		}
		return nil
	})
	if err != nil {
		return 0, fmt.Errorf("count violations by table: %w", err)
	}
	return count, nil
}

// ListViolationsByTablePaged は指定 table に絞った violation を offset から
// 最大 limit 件返す。limit <= 0 は制限なし。ListViolationsPaged 同様、
// 全件ロードせず prefix scan + skip/limit だけで完結する。
func (s *Store) ListViolationsByTablePaged(table string, limit, offset int) ([]engine.Violation, error) {
	var out []engine.Violation
	err := s.db.View(func(txn *badger.Txn) error {
		it := txn.NewIterator(badger.DefaultIteratorOptions)
		defer it.Close()
		prefix := tableKeyPrefix(table)
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
		return nil, fmt.Errorf("list violations by table paged: %w", err)
	}
	return out, nil
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

// maxListViolations は ListViolations が一括でメモリに読み込むことを許容する上限件数。
// これを超えるデータセットでは ListViolationsPaged への切り替えが必要であることを
// 呼び出し元に知らせるため、超過時はエラーを返す (var なのでテストで上書き可能)。
var maxListViolations = 100_000

// ListViolations は保存済みの全 violation を prefix scan で返す。
// maxListViolations 件を超える場合はメモリ圧迫を避けるためエラーを返す
// (呼び出し元は ListViolationsPaged を使うこと)。
func (s *Store) ListViolations() ([]engine.Violation, error) {
	var out []engine.Violation
	err := s.db.View(func(txn *badger.Txn) error {
		it := txn.NewIterator(badger.DefaultIteratorOptions)
		defer it.Close()
		prefix := []byte(keyPrefix)
		for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
			if len(out) >= maxListViolations {
				return fmt.Errorf(
					"violation count exceeds limit (%d); use ListViolationsPaged instead",
					maxListViolations,
				)
			}
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
