# DataGuard Rail — 実装ガイド

## Phase 1: Rust Engine MVP (2週)

1. sqlparser-rs で SQL を解析し AST を取得
2. FROM / JOIN 節からテーブル依存を抽出
3. SELECT 列 → ソース列の対応を petgraph に記録
4. Lineage JSON を stdout 出力
5. `dataguard-engine analyze --sql "SELECT ..." --out lineage.json`

## Phase 2: 品質ルール評価 (1週)

1. `rules.yaml` を serde_yaml でロード
2. CSV レコードを行ごとに評価
3. violations.json を出力
4. `dataguard-engine check --input data.csv --rules rules.yaml`

## Phase 3: Go Ingestion + CLI 統合 (1週)

1. cobra で `ingest` サブコマンドを実装
2. CSV または PostgreSQL からデータ取込み
3. Rust バイナリを `exec` で呼び出し結果を受け取る
4. BadgerDB に violations 保存

## Phase 4: REST API (1週)

1. gin で以下を実装
   - `GET /api/violations` — 違反一覧
   - `GET /api/lineage?table=xxx` — リネージュ取得
   - `GET /api/schema-diff` — スキーマ差分履歴
2. スキーマスナップショットを定期取得して差分検出

## Phase 5: gRPC 統合 (1週)

1. Go ↔ Rust を exec から gRPC に切り替え
2. tonic サーバーを Rust 側に実装
3. Go から streaming で行データを送信、結果をストリーム受信

## Phase 6: OTel + アラート (3日)

1. violations/min を Metrics として公開
2. schema_diff_detected を Trace Span に記録
3. Slack Webhook 通知（設定任意）

## 注意点

- Rust engine は独立バイナリとして先にビルドし動作確認してから Go 統合
- sqlparser-rs の方言は `dialect::GenericDialect` で開始し必要に応じて変更
- petgraph の循環依存チェックは `is_cyclic_directed()` で確認
