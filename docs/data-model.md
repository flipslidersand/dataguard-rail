# DataGuard Rail — データモデル

## QualityRule (YAML)

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
    window: 24h
    expression: count <= 1
```

## Violation (JSON)

```json
{
  "id": "viol-uuid",
  "rule": "positive_price",
  "table": "products",
  "row": 42,
  "column": "sale_price",
  "value": "-100",
  "detected_at": "2026-01-01T00:00:00Z"
}
```

## ColumnLineage (JSON)

```json
{
  "target_table": "monthly_sales",
  "target_column": "total_revenue",
  "sources": [
    { "table": "orders", "column": "amount" },
    { "table": "products", "column": "tax_rate" }
  ],
  "sql_ref": "SELECT SUM(o.amount * (1 + p.tax_rate)) AS total_revenue ..."
}
```

## SchemaSnapshot (BadgerDB)

```json
{
  "table": "products",
  "captured_at": "2026-01-01T00:00:00Z",
  "columns": [
    { "name": "id", "type": "int4", "nullable": false },
    { "name": "sale_price", "type": "numeric", "nullable": true }
  ]
}
```

## SchemaDiff (生成物)

```json
{
  "table": "products",
  "detected_at": "2026-01-02T00:00:00Z",
  "added": [{ "name": "discount", "type": "numeric" }],
  "dropped": [],
  "changed": [{ "name": "sale_price", "from": "numeric", "to": "float8" }]
}
```

## DataSource (Go 設定)

```yaml
sources:
  - name: products_csv
    type: csv
    path: ./data/products.csv
    schedule: "0 * * * *"
  - name: orders_db
    type: postgres
    dsn: "postgres://user:pass@localhost/shop"
    query: "SELECT * FROM orders WHERE updated_at > $1"
    schedule: "*/5 * * * *"
```
