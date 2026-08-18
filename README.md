# DataGuard Rail

リアルタイムデータ品質チェック・カラムリネージュ追跡プラットフォーム（Rust + Go）。

## 主な機能

| 機能 | 説明 |
|---|---|
| SQL リネージュ解析 | `CREATE TABLE AS SELECT` / `CREATE VIEW` からカラム依存を抽出 |
| CSV 品質ルール評価 | `rules.yaml` で not_null / 比較演算 / ユニーク制約を定義 |
| データ取込み | CSV / PostgreSQL ソースを定期取込み → violations を BadgerDB に保存 |
| REST API | `GET /api/violations` / `/api/lineage` / `/api/schema-diff` |
| gRPC 統合 | Go ↔ Rust を tonic gRPC で接続（`--grpc-addr` で切替） |
| OTel + アラート | violations.total メトリクス / Trace Span / Slack Webhook 通知 |

## 使用技術

- **Rust**: sqlparser-rs, petgraph, tonic (gRPC server), serde, clap, tokio
- **Go**: gin, pgx/v5, BadgerDB, cobra, zap, OpenTelemetry SDK

## ディレクトリ構成

```
dataguard-rail/
├── proto/                       # gRPC proto 定義
│   └── dataguard.proto
├── rust-engine/                 # Rust 解析エンジン
│   ├── src/
│   │   ├── main.rs             # CLI (analyze / check / serve)
│   │   ├── lineage.rs          # SQL リネージュ解析
│   │   ├── check.rs            # CSV 品質ルール評価
│   │   └── grpc.rs             # tonic gRPC サーバー
│   └── Cargo.toml
├── go-ingestion/                # Go 取込み層 + REST API
│   ├── cmd/dataguard/main.go   # CLI エントリポイント
│   └── internal/
│       ├── config/             # YAML 設定読み込み
│       ├── ingester/           # CSV / PostgreSQL 取込み
│       ├── engine/             # Rust engine 呼び出し (exec / gRPC)
│       ├── pipeline/           # ingest フロー
│       ├── store/              # BadgerDB 永続化
│       ├── server/             # gin REST API
│       ├── telemetry/          # OTel 初期化・メトリクス
│       ├── alert/              # Slack Webhook 通知
│       └── pb/                 # protoc 生成ファイル
└── docs/
    ├── spec.md
    ├── data-model.md
    ├── implementation-guide.md
    ├── tech-stack.md
    └── adr/                    # Architecture Decision Records
```

## セットアップ

### 必要環境

- Rust 1.75+
- Go 1.24+
- protoc（gRPC コード生成時のみ）

### ビルド

```bash
# Rust エンジン
cd rust-engine
cargo build --release

# Go 取込み層
cd go-ingestion
go build ./cmd/dataguard
```

## 実行方法

### データ取込み（exec モード）

```bash
dataguard ingest \
  --config examples/sources.yaml \
  --rules examples/rules.yaml \
  --db data/violations
```

### データ取込み（gRPC モード）

```bash
# 1. Rust gRPC サーバーを起動
dataguard-engine serve --addr [::1]:50051

# 2. Go から gRPC で接続
dataguard ingest --grpc-addr localhost:50051 \
  --config examples/sources.yaml \
  --rules examples/rules.yaml
```

### REST API サーバー起動

```bash
dataguard serve --addr :8080 --db data/violations
```

### OTel + Slack アラート付きで起動

```bash
dataguard serve \
  --otel-endpoint ""  \
  --slack-webhook https://hooks.slack.com/services/xxx
```

### SQL リネージュ解析

```bash
dataguard-engine analyze --sql examples/sample.sql --out lineage.json
```

### CSV 品質チェック

```bash
dataguard-engine check \
  --input data/products.csv \
  --rules examples/rules.yaml \
  --out violations.json
```

## テスト

```bash
# Rust
cd rust-engine && cargo test

# Go
cd go-ingestion && go test ./...
```

## API エンドポイント

| メソッド | パス | 説明 |
|---|---|---|
| GET | `/health` | ヘルスチェック |
| GET | `/api/violations` | 違反一覧（`?table=xxx` でフィルタ） |
| GET | `/api/lineage` | リネージュ取得（`?sql=path/to/file.sql`） |
| GET | `/api/schema-diff` | スキーマ差分（`?table=xxx`） |

## 品質ルール定義例（rules.yaml）

```yaml
rules:
  - name: positive_price
    column: sale_price
    expression: value > 0
  - name: no_null_email
    column: email
    expression: not_null
```

## データソース定義例（sources.yaml）

```yaml
sources:
  - name: products
    type: csv
    path: ./data/products.csv
  - name: orders
    type: postgres
    dsn: "postgres://user:pass@localhost/shop"
    query: "SELECT * FROM orders WHERE updated_at > now() - interval '1 day'"
```

## Architecture Decision Records

- [ADR-001](docs/adr/ADR-001-sqlparser-rs.md): sqlparser-rs + GenericDialect
- [ADR-002](docs/adr/ADR-002-petgraph-lineage.md): petgraph DiGraph によるリネージュ
- [ADR-003](docs/adr/ADR-003-exec-first-grpc-later.md): exec first → Phase 5 で gRPC 移行
- [ADR-004](docs/adr/ADR-004-go-ingestion-badgerdb.md): BadgerDB による violations 永続化
