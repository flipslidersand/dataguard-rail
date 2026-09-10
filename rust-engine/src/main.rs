mod check;
mod grpc;
mod lineage;
#[cfg(test)]
#[path = "lineage_test.rs"]
mod lineage_test;
mod profile;
#[cfg(test)]
#[path = "serve_tls_test.rs"]
mod serve_tls_test;

use anyhow::{Context, Result};
use clap::{Parser, Subcommand};
use std::fs;

#[derive(Parser)]
#[command(
    name = "dataguard-engine",
    about = "DataGuard Rail Rust analysis engine"
)]
struct Args {
    #[command(subcommand)]
    command: Command,
}

#[derive(Subcommand)]
enum Command {
    /// SQL ファイルからテーブルリネージュを生成する
    Analyze {
        #[arg(long)]
        sql: String,
        #[arg(long, default_value = "lineage.json")]
        out: String,
    },
    /// CSV ファイルに品質ルールを適用して violations を出力する
    Check {
        #[arg(long)]
        input: String,
        #[arg(long)]
        rules: String,
        #[arg(long, default_value = "violations.json")]
        out: String,
    },
    /// CSV ファイルの各カラムを統計プロファイリングする
    Profile {
        #[arg(long)]
        input: String,
        #[arg(long, default_value = "profile.json")]
        out: String,
    },
    /// gRPC サーバーを起動する (Phase 5)
    Serve {
        #[arg(long, default_value = "[::1]:50051")]
        addr: String,
        /// TLS サーバー証明書 (PEM)。--tls-key と併用。
        #[arg(long)]
        tls_cert: Option<String>,
        /// TLS サーバー秘密鍵 (PEM)。--tls-cert と併用。
        #[arg(long)]
        tls_key: Option<String>,
        /// クライアント証明書を検証する CA 証明書 (PEM)。指定時は mTLS を要求する。
        #[arg(long)]
        tls_client_ca: Option<String>,
        /// ループバック外でも TLS なし平文通信を明示的に許可する（非推奨）。
        #[arg(long)]
        insecure: bool,
    },
}

#[tokio::main]
async fn main() -> Result<()> {
    let args = Args::parse();
    match args.command {
        Command::Analyze { sql, out } => run_analyze(sql, out),
        Command::Check { input, rules, out } => run_check(input, rules, out),
        Command::Profile { input, out } => run_profile(input, out),
        Command::Serve {
            addr,
            tls_cert,
            tls_key,
            tls_client_ca,
            insecure,
        } => run_serve(addr, tls_cert, tls_key, tls_client_ca, insecure).await,
    }
}

/// --sql に .sql 拡張子付きの文字列が渡されたのにファイルが存在しない場合に true を返す。
/// 単なる SQL パースエラーに埋もれてパスのタイポに気づきにくくなるのを防ぐための判定。
fn should_warn_ambiguous_sql_path(sql: &str, exists: bool) -> bool {
    !exists && sql.to_lowercase().ends_with(".sql")
}

fn run_analyze(sql: String, out: String) -> Result<()> {
    let exists = std::path::Path::new(&sql).exists();
    if should_warn_ambiguous_sql_path(&sql, exists) {
        eprintln!(
            "WARNING: --sql {sql:?} はファイルとして見つかりません。\
             パスの誤りでなければ、この文字列自体を生の SQL として解析します。"
        );
    }
    let sql_text = if exists {
        fs::read_to_string(&sql).with_context(|| format!("Failed to read SQL file: {sql}"))?
    } else {
        sql
    };

    let report = lineage::analyze(&sql_text)?;
    let json = serde_json::to_string_pretty(&report)?;

    if out == "-" {
        println!("{json}");
    } else {
        fs::write(&out, &json).with_context(|| format!("Failed to write output: {out}"))?;
        eprintln!(
            "lineage: {} tables, {} edges → {out}",
            report.tables.len(),
            report.edges.len()
        );
    }
    Ok(())
}

fn run_profile(input: String, out: String) -> Result<()> {
    let profiled_at = chrono::Utc::now().to_rfc3339();
    let report = profile::profile_file(&input, &profiled_at)?;
    let json = serde_json::to_string_pretty(&report)?;

    if out == "-" {
        println!("{json}");
    } else {
        fs::write(&out, &json).with_context(|| format!("Failed to write output: {out}"))?;
        eprintln!(
            "profile: {} rows, {} columns → {out}",
            report.row_count,
            report.columns.len()
        );
    }
    Ok(())
}

fn run_check(input: String, rules: String, out: String) -> Result<()> {
    let detected_at = chrono::Utc::now().to_rfc3339();
    let violations = check::check_file(&input, &rules, &detected_at)?;
    let json = serde_json::to_string_pretty(&violations)?;

    if out == "-" {
        println!("{json}");
    } else {
        fs::write(&out, &json).with_context(|| format!("Failed to write output: {out}"))?;
        eprintln!("check: {} violation(s) → {out}", violations.len());
    }
    Ok(())
}

async fn run_serve(
    addr: String,
    tls_cert: Option<String>,
    tls_key: Option<String>,
    tls_client_ca: Option<String>,
    insecure: bool,
) -> Result<()> {
    use grpc::dataguard::data_guard_server::DataGuardServer;
    use grpc::DataGuardService;
    use tonic::transport::{Certificate, Identity, Server, ServerTlsConfig};

    let addr_parsed: std::net::SocketAddr = addr
        .parse()
        .with_context(|| format!("invalid addr: {addr}"))?;

    let tls_config = match (&tls_cert, &tls_key) {
        (Some(cert_path), Some(key_path)) => {
            let cert = fs::read_to_string(cert_path)
                .with_context(|| format!("failed to read --tls-cert: {cert_path}"))?;
            let key = fs::read_to_string(key_path)
                .with_context(|| format!("failed to read --tls-key: {key_path}"))?;
            let identity = Identity::from_pem(cert, key);
            let mut cfg = ServerTlsConfig::new().identity(identity);
            if let Some(ca_path) = &tls_client_ca {
                let ca_pem = fs::read_to_string(ca_path)
                    .with_context(|| format!("failed to read --tls-client-ca: {ca_path}"))?;
                cfg = cfg.client_ca_root(Certificate::from_pem(ca_pem));
            }
            Some(cfg)
        }
        (None, None) => {
            if !addr_parsed.ip().is_loopback() && !insecure {
                anyhow::bail!(
                    "refusing to bind {addr} without TLS: this addr is not loopback. \
                     Provide --tls-cert/--tls-key (optionally --tls-client-ca for mTLS), \
                     or pass --insecure to opt in to plaintext explicitly."
                );
            }
            if !addr_parsed.ip().is_loopback() {
                eprintln!(
                    "WARNING: --addr {addr} はループバック外にバインドされています。\
                     --insecure が指定されたため TLS 未対応の平文通信のまま起動します。\
                     信頼できないネットワークに公開しないでください（SSH トンネル等の利用を推奨）。"
                );
            }
            None
        }
        _ => anyhow::bail!("--tls-cert and --tls-key must be set together"),
    };

    eprintln!("gRPC server listening on {addr}");

    let mut builder = Server::builder();
    if let Some(cfg) = tls_config {
        builder = builder
            .tls_config(cfg)
            .context("invalid TLS configuration")?;
    }
    builder
        .add_service(DataGuardServer::new(DataGuardService))
        .serve(addr_parsed)
        .await
        .context("gRPC server error")?;
    Ok(())
}

#[cfg(test)]
mod analyze_sql_path_tests {
    use super::should_warn_ambiguous_sql_path;

    #[test]
    fn warns_when_sql_extension_but_missing_file() {
        assert!(should_warn_ambiguous_sql_path("queries/report.sql", false));
    }

    #[test]
    fn warns_case_insensitively() {
        assert!(should_warn_ambiguous_sql_path("REPORT.SQL", false));
    }

    #[test]
    fn no_warning_when_file_exists() {
        assert!(!should_warn_ambiguous_sql_path("queries/report.sql", true));
    }

    #[test]
    fn no_warning_for_raw_sql_without_sql_extension() {
        assert!(!should_warn_ambiguous_sql_path(
            "SELECT * FROM customers",
            false
        ));
    }
}
