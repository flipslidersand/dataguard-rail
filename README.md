# DataGuard Rail

Real-time data quality checking and column lineage tracking platform (Rust engine + Go ingestion layer).

## Features

| Feature | Description |
|---|---|
| SQL Lineage Analysis | Extracts column dependencies from `CREATE TABLE AS SELECT` / `CREATE VIEW` |
| CSV Quality Rules | Define `not_null` / comparison / unique / regex constraints in `rules.yaml` |
| Data Profiling | Auto-computes min / max / mean / null_rate / unique_count per column |
| Data Ingestion | CSV / JSONL / PostgreSQL sources → violations stored in BadgerDB |
| REST API + Web UI | Dashboard + violations / lineage / schema-diff endpoints |
| gRPC Integration | Go ↔ Rust connected via tonic gRPC (`--grpc-addr` flag) |
| OTel + Alerts | violations.total metric / Trace spans / Slack Webhook notifications |

## Tech Stack

- **Rust:** sqlparser-rs, petgraph, tonic (gRPC server), regex, serde, clap, tokio
- **Go:** gin, pgx/v5, BadgerDB, cobra, zap, OpenTelemetry SDK

## Directory Structure

```
dataguard-rail/
├── proto/                       # gRPC proto definitions
│   └── dataguard.proto
├── rust-engine/                 # Rust analysis engine
│   ├── src/
│   │   ├── main.rs             # CLI (analyze / check / profile / serve)
│   │   ├── lineage.rs          # SQL lineage analysis
│   │   ├── check.rs            # CSV quality rule evaluation
│   │   ├── profile.rs          # CSV data profiling
│   │   └── grpc.rs             # tonic gRPC server
│   └── Cargo.toml
├── go-ingestion/                # Go ingestion layer + REST API
│   ├── cmd/dataguard/main.go
│   └── internal/
│       ├── config/             # YAML config loading
│       ├── ingester/           # CSV / JSONL / PostgreSQL ingestion
│       ├── engine/             # Rust engine invocation (exec / gRPC)
│       ├── pipeline/           # ingest flow
│       ├── store/              # BadgerDB persistence
│       ├── server/             # gin REST API + Web UI dashboard
│       ├── telemetry/          # OTel init + metrics
│       ├── alert/              # Slack Webhook notifications
│       └── pb/                 # protoc-generated files
└── docs/
    ├── spec.md
    ├── data-model.md
    ├── implementation-guide.md
    ├── tech-stack.md
    └── adr/                    # Architecture Decision Records
```

## Prerequisites

- Rust 1.75+
- Go 1.24+
- protoc (only needed for gRPC code generation)

## Build

```bash
# Rust engine
cd rust-engine && cargo build --release

# Go ingestion layer
cd go-ingestion && go build ./cmd/dataguard
```

## Running

### Ingest data (exec mode)

```bash
dataguard ingest \
  --config examples/sources.yaml \
  --rules examples/rules.yaml \
  --db data/violations
```

### Ingest data (gRPC mode)

```bash
# 1. Start Rust gRPC server
dataguard-engine serve --addr [::1]:50051

# 2. Connect from Go via gRPC
dataguard ingest --grpc-addr localhost:50051 \
  --config examples/sources.yaml \
  --rules examples/rules.yaml
```

### Daemon mode (scheduled ingestion)

Add a `schedule` field to `sources.yaml` for cron-based execution:

```yaml
sources:
  - name: products
    type: csv
    path: ./data/products.csv
    schedule: "0 * * * *"      # every hour

  - name: orders
    type: postgres
    dsn: "postgres://user:pass@localhost/shop"
    query: "SELECT * FROM orders WHERE updated_at > now() - interval '1 day'"
    # no schedule → runs once on startup
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
# Open http://localhost:8080/ for the dashboard
```

### SQL Lineage Analysis

```bash
dataguard-engine analyze --sql examples/sample.sql --out lineage.json
```

### CSV Quality Check

```bash
dataguard-engine check \
  --input data/products.csv \
  --rules examples/rules.yaml \
  --out violations.json
```

### CSV Data Profiling

```bash
dataguard-engine profile \
  --input data/products.csv \
  --out profile.json
```

## Tests

```bash
# Rust (21 tests)
cd rust-engine && cargo test

# Go (all packages)
cd go-ingestion && go test ./...
```

## API Endpoints

| Method | Path | Description |
|---|---|---|
| GET | `/` | Violations + schema-diff dashboard (Web UI) |
| GET | `/health` | Health check |
| GET | `/api/violations` | List violations (`?table=xxx` to filter) |
| GET | `/api/lineage` | Get lineage (`?sql=path/to/file.sql`) |
| GET | `/api/schema-diff` | Schema diff (`?table=xxx`) |

## Quality Rules (`rules.yaml`)

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
- [ADR-002](docs/adr/ADR-002-petgraph-lineage.md): petgraph DiGraph for lineage
- [ADR-003](docs/adr/ADR-003-exec-first-grpc-later.md): exec-first → gRPC in Phase 5
- [ADR-004](docs/adr/ADR-004-go-ingestion-badgerdb.md): BadgerDB for violation persistence

