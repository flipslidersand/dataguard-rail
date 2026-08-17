# DataGuard Rail

Go でデータを収集・配送し、Rust で SQL・スキーマを解析して
品質チェックとカラムレベルのリネージュを生成するリアルタイムデータ品質基盤。

## アーキテクチャ

```
DB / API / CSV
      ↓
Go Ingestion Layer   (取込み・キュー・REST API・アラート)
      ↓
Rust Analysis Engine (SQL AST 解析・カラムリネージュ・品質ルール評価)
      ↓
Go Control API / Alert
```

Go 側と Rust 側は当面 `exec` で連携する（ADR-003）。将来的に gRPC へ移行予定。

## コンポーネント

| ディレクトリ | 役割 |
|---|---|
| `rust-engine/` | SQL 解析・カラムリネージュ・品質ルール評価エンジン (`dataguard-engine`) |
| `go-ingestion/` | データ取込み・キュー・REST API・アラート (`dataguard`) |
| `docs/` | 仕様書・データモデル・技術選定・ADR |

## Rust Engine

```bash
cd rust-engine
cargo build --release

# SQL からカラムリネージュを生成
cargo run -- analyze --sql query.sql --out lineage.json

# CSV に品質ルールを適用（未実装 / Phase 2）
cargo run -- check --input data.csv --rules rules.yaml
```

### analyze 出力例 (`lineage.json`)

```json
{
  "target": "monthly_sales",
  "sources": ["orders", "products"],
  "columns": {
    "total_revenue": ["orders.amount", "products.tax_rate"]
  }
}
```

## Go Ingestion

```bash
cd go-ingestion
go build ./...

./dataguard ingest   # データ取込み + 品質チェック（未実装 / Phase 3）
./dataguard serve    # REST API サーバ（未実装 / Phase 4）
```

## 実装フェーズ

| Phase | 内容 | 状態 |
|---|---|---|
| 1 | Rust Engine: `analyze`（SQL→リネージュ JSON） | 実装中 |
| 2 | Rust Engine: `check`（品質ルール評価） | 未着手 |
| 3 | Go Ingestion + CLI 統合 (exec) | 未着手 |
| 4 | REST API (gin) | 未着手 |
| 5 | gRPC 統合 (tonic) | 未着手 |
| 6 | OTel + アラート | 未着手 |

詳細は [docs/implementation-guide.md](docs/implementation-guide.md) を参照。

## ドキュメント

- [仕様書](docs/spec.md)
- [データモデル](docs/data-model.md)
- [技術選定](docs/tech-stack.md)
- [ADR](docs/adr/)
