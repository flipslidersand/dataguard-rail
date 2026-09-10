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

> **Security / セキュリティ注意:** デフォルトはループバック (`[::1]`) バインドで
> TLS 未対応の平文通信です。`--addr`/`--grpc-addr` を**ループバック外**に向ける場合、
> TLS/mTLS（下記）または `--insecure`/`--grpc-insecure` の明示的な opt-in が
> 無いと **起動/接続を拒否**します（中間者攻撃による `csv_path`/`sql_path` の
> 書き換え、`violations_json` の改ざんを防ぐため）。

#### TLS / mTLS でループバック外の Go↔Rust 通信を保護する

証明書は自己署名 CA で発行できます（社内 CA を使う場合も手順は同様）。

```bash
# CA
openssl req -x509 -newkey rsa:2048 -nodes -keyout ca.key -out ca.crt \
  -days 365 -subj "/CN=dataguard-ca"

# Rust engine のサーバー証明書（bind するホスト名/IPを SAN に含める）
openssl req -newkey rsa:2048 -nodes -keyout server.key -out server.csr \
  -subj "/CN=engine.internal" -addext "subjectAltName=DNS:engine.internal"
openssl x509 -req -in server.csr -CA ca.crt -CAkey ca.key -CAcreateserial \
  -out server.crt -days 365 -copy_extensions copy

# (mTLS を使う場合) Go 側のクライアント証明書
openssl req -newkey rsa:2048 -nodes -keyout client.key -out client.csr \
  -subj "/CN=go-ingestion"
openssl x509 -req -in client.csr -CA ca.crt -CAkey ca.key -CAcreateserial \
  -out client.crt -days 365
```

```bash
# 1. TLS（--tls-client-ca を付けると mTLS = クライアント証明書必須）
dataguard-engine serve --addr engine.internal:50051 \
  --tls-cert server.crt --tls-key server.key --tls-client-ca ca.crt

# 2. Go 側は --grpc-tls-ca で検証、mTLS の場合は --grpc-tls-cert/--grpc-tls-key も指定
dataguard ingest --grpc-addr engine.internal:50051 \
  --grpc-tls-ca ca.crt --grpc-tls-cert client.crt --grpc-tls-key client.key \
  --config examples/sources.yaml --rules examples/rules.yaml
```

ループバック限定運用で TLS が不要な場合は、既定のまま（`--tls-cert`/`--grpc-tls-ca`
未指定）で従来通り平文で動作します。ループバック外でどうしても TLS を使わない場合のみ、
`dataguard-engine serve --insecure` / `dataguard ingest --grpc-insecure` を明示的に指定してください。

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
# Rust (22 tests / 22 テスト)
cd rust-engine && cargo test

# Go (30 tests / 30 テスト, all packages / 全パッケージ)
cd go-ingestion && go test ./...
```

## Benchmark / ベンチマーク

Measured with a release build (`cargo build --release`) on a synthetic 1M-row CSV
(2 comparison rules: `sale_price > 0`, `qty > 0`).
リリースビルドで合成した100万行CSV（比較ルール2件: `sale_price > 0`, `qty > 0`）を検証した実測値。

| Metric / 指標 | Result / 結果 |
|---|---|
| Rows processed / 処理行数 | 1,000,000 |
| Wall time / 実行時間 | 1.21s |
| Violations detected / 検出違反数 | 666,966 |

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
