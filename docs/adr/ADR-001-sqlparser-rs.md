# ADR-001: SQL 解析に sqlparser-rs を使う

- 日付: 2026-06-24
- ステータス: 承認済み

## 背景

カラムレベルのリネージュ生成には SQL の AST が必要。
自前パーサーは工数が大きく、pest/nom では SQL の全方言を網羅しにくい。

## 決定

`sqlparser` クレート (sqlparser-rs) を採用する。

## 理由

- MySQL / PostgreSQL / ANSI SQL など複数方言をサポート
- AST が Rust の enum で型安全に表現されている
- SELECT / FROM / JOIN / WITH (CTE) を網羅しており依存解析に十分

## トレードオフ

- ベンダー固有の拡張構文（PostgreSQL の `RETURNING` など）は方言設定が必要
- AST の形が sqlparser のバージョンに依存するため semver に注意
