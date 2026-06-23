# ADR-003: Go-Rust 統合は exec から始め Phase 5 で gRPC に移行する

- 日付: 2026-06-24
- ステータス: 承認済み

## 背景

Go (Ingestion) と Rust (Engine) の統合方法として FFI・gRPC・exec の 3 択がある。

## 決定

MVP では Go から Rust バイナリを `os/exec` で呼び出す。
Phase 5 で gRPC streaming に移行する。

## 理由

- exec は proto 定義・コード生成なしで即座に動作確認できる
- Rust engine 単体テストが CLI ベースで書きやすい
- gRPC への移行はインターフェースを安定させてから行う方がリスクが低い

## トレードオフ

- 行ごとのストリーミング処理は exec では非効率（ファイルベースの一括渡し）
- Phase 5 移行時に Go 側の呼び出しコードを全面書き直す必要がある
