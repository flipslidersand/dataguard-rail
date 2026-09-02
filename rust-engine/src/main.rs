mod check;
mod grpc;
mod lineage;
mod profile;
#[cfg(test)]
#[path = "lineage_test.rs"]
mod lineage_test;

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
    },
}

#[tokio::main]
async fn main() -> Result<()> {
    let args = Args::parse();
    match args.command {
        Command::Analyze { sql, out } => run_analyze(sql, out),
        Command::Check { input, rules, out } => run_check(input, rules, out),
        Command::Profile { input, out } => run_profile(input, out),
        Command::Serve { addr } => run_serve(addr).await,
    }
}

fn run_analyze(sql: String, out: String) -> Result<()> {
    let sql_text = if std::path::Path::new(&sql).exists() {
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

async fn run_serve(addr: String) -> Result<()> {
    use grpc::dataguard::data_guard_server::DataGuardServer;
    use grpc::DataGuardService;
    use tonic::transport::Server;

    let addr_parsed = addr.parse().with_context(|| format!("invalid addr: {addr}"))?;
    eprintln!("gRPC server listening on {addr}");

    Server::builder()
        .add_service(DataGuardServer::new(DataGuardService))
        .serve(addr_parsed)
        .await
        .context("gRPC server error")?;
    Ok(())
}
