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

> **Security / セキュリティ注意:** この gRPC チャネルは TLS 未対応の平文通信です。
> デフォルトはループバック (`[::1]`) バインドですが、`--addr`/`--grpc-addr` を
> ループバック外に向けると同一ネットワーク上の攻撃者による中間者攻撃
> （`csv_path`/`sql_path` の書き換え、`violations_json` の改ざん）が成立します。
> Go と Rust を別ホストで動かす場合は SSH トンネル等で経路を保護してください
> （ループバック外バインド時は起動時に警告ログが出力されます）。

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
dataguard serve --addr :8080 --db data/violations --api-key "$(gopass show -o infra/dataguard-rail/api-key)"
# Open / ブラウザで http://localhost:8080/ （Authorization: Bearer <api-key> が必要）
curl -H "Authorization: Bearer $API_KEY" http://localhost:8080/api/violations
```

`--api-key` を指定しない場合、`/` と `/api/*` は認証なしで公開される（ローカル開発専用。本番運用では必須）。
`/health` は監視用に常に認証不要。

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
