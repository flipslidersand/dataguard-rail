# ADR-004: violations / schema snapshot の保存に BadgerDB を使う

- 日付: 2026-06-24
- ステータス: 承認済み

## 背景

品質チェック結果とスキーマスナップショットを永続化し、API から参照できるようにしたい。
外部 DB への依存は MVP では避けたい。

## 決定

Go Ingestion 層の BadgerDB (組み込み KV) に保存する。

## 理由

- Go バイナリに組み込めるため外部サービス不要
- StreamRail / SentinelMesh でも同じ判断をしており統一感がある
- キー設計: `violations:{timestamp}:{rule}` でレンジスキャンが可能

## トレードオフ

- 時系列集計クエリは BadgerDB には不向き → 将来的に ClickHouse へ移行
- BadgerDB の GC を定期実行しないとディスク肥大化
