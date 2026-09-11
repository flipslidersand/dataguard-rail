---
title: "rules.yaml は check/value ではなく expression 文字列を要求する"
tags: [rust, yaml, cli]
severity: low
date: "2026-09-07"
---

## 症状

READMEのルール定義例をそのまま真似て以下のようなYAMLを書くと失敗する:

```yaml
rules:
  - name: positive_price
    column: sale_price
    check: greater_than
    value: 0
```

```
Error: invalid rules yaml
Caused by:
    rules[0]: missing field `expression` at line 2 column 5
```

## 原因

`rust-engine/src/check.rs` の `RawRule` は `check`/`value` ではなく、`expression` という
1つの文字列フィールドで比較式全体を受け取る（`"value > 0"` のような形式）。README の
サンプルは読めば分かるが、直感的に `check`/`value` のような分離フィールドを書きたくなる。

## 対処

```yaml
rules:
  - name: positive_price
    column: sale_price
    expression: "value > 0"
```

`not_null` の場合は `expression: not_null`、重複検出は `expression: "count <= 1"` の形式。
