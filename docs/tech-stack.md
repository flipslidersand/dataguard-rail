# DataGuard Rail — 技術スタック

## Rust Engine

| 用途         | クレート           | バージョン |
| ------------ | ------------------ | ---------- |
| SQL パーサー | sqlparser          | 0.46       |
| グラフ       | petgraph           | 0.6        |
| ルール評価   | serde_yaml         | 0.9        |
| シリアライズ | serde + serde_json | 1          |
| エラー       | anyhow             | 1          |
| CLI          | clap (derive)      | 4          |
| ベンチマーク | criterion          | 0.5        |

## Go Ingestion

| 用途          | パッケージ                     | バージョン |
| ------------- | ------------------------------ | ---------- |
| PostgreSQL    | github.com/jackc/pgx/v5        | 5.5.0      |
| HTTP ルーター | github.com/gin-gonic/gin       | 1.10.0     |
| KV ストア     | github.com/dgraph-io/badger/v4 | 4.2.0      |
| YAML          | gopkg.in/yaml.v3               | 3          |
| ログ          | go.uber.org/zap                | 1.27.0     |
| OTel          | go.opentelemetry.io/otel       | 1.24.0     |
| CLI           | github.com/spf13/cobra         | 1.8.0      |
| 設定          | github.com/spf13/viper         | 1.19.0     |

## 統合方式

- Go ↔ Rust: gRPC (Phase 1〜) または CLI サブプロセス (MVP)
- Phase 1 MVP は Rust を CLI バイナリとして Go から `exec` で呼び出し
- Phase 3 以降で tonic + grpc-go の gRPC に移行
