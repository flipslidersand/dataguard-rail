# DataGuard Rail — 仕様書

## 概要

Go でデータを収集・配送し、Rust で SQL・スキーマを解析して
品質チェックとカラムレベルのリネージュを生成するリアルタイム基盤。

## アーキテクチャ

```
DB / API / CSV
      ↓
Go Ingestion Layer
      ↓
Channel Queue
      ↓
Rust Analysis Engine
      ├─ Schema Validation
      ├─ SQL AST Analysis
      ├─ Column Lineage
      └─ Quality Rules
      ↓
Go Control API / Alert
```

## Go Ingestion の責務

- CSV / PostgreSQL / API からのデータ取込み
- 内部チャネルキューへのプッシュ
- Rust Engine への FFI または gRPC 呼び出し
- 品質チェック結果を受け取りアラート送信
- 管理 REST API
- Web UI 向け Backend (JSON)

## Rust Engine の責務

- SQL Parser (カスタム or sqlparser-rs)
- AST からカラム依存グラフを構築
- スキーマ差分の検出
- 品質ルールの高速評価 (YAML 定義)
- 影響範囲の算出
- Lineage JSON 生成

## MVP スコープ

- CSV または PostgreSQL からデータ取込み
- YAML で定義した品質ルールを評価
- 重複レコード検出
- SQL テーブル依存解析
- Lineage JSON 出力
- エラー通知 (ログ + `/api/violations`)

## 品質ルール例

```yaml
rules:
  - name: positive_price
    column: sale_price
    expression: value > 0

  - name: no_duplicate_stock
    key: stock_id
    window: 24h
    expression: count <= 1
```

## 出力例

```json
{
  "lineage": {
    "target": "monthly_sales",
    "sources": ["orders", "products"],
    "columns": {
      "total": ["orders.amount", "products.tax_rate"]
    }
  },
  "violations": [{ "rule": "positive_price", "row": 42, "value": -100 }]
}
```
