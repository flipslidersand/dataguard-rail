# DataGuard Rail

Real-time data quality checking and column lineage tracking platform (Rust engine + Go ingestion layer).

リアルタイムデータ品質チェック・カラムリネージュ追跡プラットフォーム（Rust + Go）。

## Features / 主な機能

| Feature | Description / 説明 |
|---|---|
| SQL Lineage Analysis / SQL リネージュ解析 | Extracts column dependencies from `CREATE TABLE AS SELECT` / `CREATE VIEW` / カラム依存を抽出 |
| CSV Quality Rules / CSV 品質ルール評価 | `not_null` / comparison / unique / regex constraints via `rules.yaml` / `rules.yaml` で定義 |
| Data Profiling / データプロファイリング | Auto-computes min / max / mean / null_rate / unique_count per column / カラムごとに自動算出 |
| Data Ingestion / データ取込み | CSV / JSONL / PostgreSQL → violations stored in BadgerDB |
| REST API + Web UI | Dashboard + violations / lineage / schema-diff endpoints |
| gRPC Integration / gRPC 統合 | Go ↔ Rust via tonic gRPC (`--grpc-addr`) |
| OTel + Alerts / OTel + アラート | violations.total metric / Trace spans / Slack Webhook |

## Tech Stack / 使用技術

- **Rust:** sqlparser-rs, petgraph, tonic (gRPC server), regex, serde, clap, tokio
- **Go:** gin, pgx/v5, BadgerDB, cobra, zap, OpenTelemetry SDK

## Directory Structure / ディレクトリ構成

```
dataguard-rail/
├── proto/                       # gRPC proto definitions / gRPC proto 定義
│   └── dataguard.proto
├── rust-engine/                 # Rust analysis engine / Rust 解析エンジン
│   ├── src/
│   │   ├── main.rs             # CLI (analyze / check / profile / serve)
│   │   ├── lineage.rs          # SQL lineage analysis / SQL リネージュ解析
│   │   ├── check.rs            # CSV quality rule evaluation / CSV 品質ルール評価
│   │   ├── profile.rs          # CSV data profiling / データプロファイリング
│   │   └── grpc.rs             # tonic gRPC server
│   └── Cargo.toml
├── go-ingestion/                # Go ingestion layer + REST API / Go 取込み層
│   ├── cmd/dataguard/main.go
│   └── internal/
│       ├── config/             # YAML config / YAML 設定読み込み
│       ├── ingester/           # CSV / JSONL / PostgreSQL ingestion / 取込み
│       ├── engine/             # Rust engine invocation / Rust engine 呼び出し
│       ├── pipeline/           # ingest flow / ingest フロー
│       ├── store/              # BadgerDB persistence / BadgerDB 永続化
│       ├── server/             # gin REST API + Web UI dashboard
│       ├── telemetry/          # OTel init + metrics / OTel 初期化・メトリクス
│       ├── alert/              # Slack Webhook notifications / Slack Webhook 通知
│       └── pb/                 # protoc-generated files / protoc 生成ファイル
└── docs/
    ├── spec.md
    ├── data-model.md
    ├── implementation-guide.md
    ├── tech-stack.md
    └── adr/                    # Architecture Decision Records
```

## Prerequisites / 必要環境

- Rust 1.75+
- Go 1.24+
- protoc (gRPC code generation only / gRPC コード生成時のみ)

## Build / ビルド

```bash
# Rust engine / Rust エンジン
cd rust-engine && cargo build --release

# Go ingestion layer / Go 取込み層
cd go-ingestion && go build ./cmd/dataguard
```

## Running / 実行方法

### Ingest data (exec mode / exec モード)

```bash
dataguard ingest \
  --config examples/sources.yaml \
  --rules examples/rules.yaml \
  --db data/violations
```

### Ingest data (gRPC mode / gRPC モード)

```bash
# 1. Start Rust gRPC server / Rust gRPC サーバーを起動
dataguard-engine serve --addr [::1]:50051

# 2. Connect from Go / Go から gRPC で接続
dataguard ingest --grpc-addr localhost:50051 \
  --config examples/sources.yaml \
  --rules examples/rules.yaml
```

### Daemon mode / スケジューラモード (`--daemon`)

Add `schedule` to `sources.yaml` for cron-based execution.  
`sources.yaml` に `schedule` フィールドを追加すると cron 式でソースを定期実行できます。

```yaml
sources:
  - name: products
    type: csv
    path: ./data/products.csv
    schedule: "0 * * * *"      # every hour / 毎時0分に実行

  - name: orders
    type: postgres
    dsn: "postgres://user:pass@localhost/shop"
    query: "SELECT * FROM orders WHERE updated_at > now() - interval '1 day'"
    # no schedule → runs once on startup / schedule なし → 起動時に即時1回実行
```

```bash
dataguard ingest --daemon \
  --config examples/sources.yaml \
  --rules examples/rules.yaml \
  --db data/violations
```

### REST API + Web UI

```bash
dataguard serve --addr :8080 --db data/violations
# Open / ブラウザで http://localhost:8080/
```

### SQL Lineage Analysis / SQL リネージュ解析

```bash
dataguard-engine analyze --sql examples/sample.sql --out lineage.json
```

### CSV Quality Check / CSV 品質チェック

```bash
dataguard-engine check \
  --input data/products.csv \
  --rules examples/rules.yaml \
  --out violations.json
```

### CSV Data Profiling / CSV データプロファイリング

```bash
dataguard-engine profile --input data/products.csv --out profile.json
```

## Tests / テスト

```bash
# Rust (21 tests / 21 テスト)
cd rust-engine && cargo test

# Go (all packages / 全パッケージ)
cd go-ingestion && go test ./...
```

## API Endpoints / API エンドポイント

| Method | Path | Description / 説明 |
|---|---|---|
| GET | `/` | Dashboard (Web UI) / violations & schema-diff ダッシュボード |
| GET | `/health` | Health check / ヘルスチェック |
| GET | `/api/violations` | List violations (`?table=xxx`) / 違反一覧 |
| GET | `/api/lineage` | Get lineage (`?sql=path`) / リネージュ取得 |
| GET | `/api/schema-diff` | Schema diff (`?table=xxx`) / スキーマ差分 |

## Quality Rules / 品質ルール定義例 (`rules.yaml`)

```yaml
rules:
  - name: positive_price
    column: sale_price
    expression: value > 0

  - name: no_null_email
    column: email
    expression: not_null

  - name: no_duplicate_stock
    key: stock_id
    expression: count <= 1

  - name: valid_code
    column: code
    expression: "matches /^[A-Z]{2}[0-9]{4}$/"
```

## Architecture Decision Records

- [ADR-001](docs/adr/ADR-001-sqlparser-rs.md): sqlparser-rs + GenericDialect
- [ADR-002](docs/adr/ADR-002-petgraph-lineage.md): petgraph DiGraph for lineage / リネージュ
- [ADR-003](docs/adr/ADR-003-exec-first-grpc-later.md): exec-first → gRPC in Phase 5
- [ADR-004](docs/adr/ADR-004-go-ingestion-badgerdb.md): BadgerDB for violation persistence / violations 永続化
