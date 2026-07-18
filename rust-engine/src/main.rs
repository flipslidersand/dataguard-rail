mod lineage;
#[cfg(test)]
#[path = "lineage_test.rs"]
mod lineage_test;

use anyhow::{Context, Result};
use clap::{Parser, Subcommand};
use std::fs;

#[derive(Parser)]
#[command(name = "dataguard-engine", about = "DataGuard Rail Rust analysis engine")]
struct Args {
    #[command(subcommand)]
    command: Command,
}

#[derive(Subcommand)]
enum Command {
    /// SQL ファイルからテーブルリネージュを生成する
    Analyze {
        /// 解析対象の SQL 文字列またはファイルパス
        #[arg(long)]
        sql: String,
        /// 出力先 JSON ファイル (- で stdout)
        #[arg(long, default_value = "lineage.json")]
        out: String,
    },
    /// CSV ファイルに品質ルールを適用して violations を出力する (Phase 2+)
    Check {
        /// 入力 CSV ファイル
        #[arg(long)]
        input: String,
        /// 品質ルール YAML ファイル
        #[arg(long)]
        rules: String,
    },
}

fn main() -> Result<()> {
    let args = Args::parse();
    match args.command {
        Command::Analyze { sql, out } => {
            let sql_text = if std::path::Path::new(&sql).exists() {
                fs::read_to_string(&sql)
                    .with_context(|| format!("Failed to read SQL file: {sql}"))?
            } else {
                sql
            };

            let report = lineage::analyze(&sql_text)?;
            let json = serde_json::to_string_pretty(&report)?;

            if out == "-" {
                println!("{json}");
            } else {
                fs::write(&out, &json)
                    .with_context(|| format!("Failed to write output: {out}"))?;
                eprintln!(
                    "lineage: {} tables, {} edges → {out}",
                    report.tables.len(),
                    report.edges.len()
                );
            }
        }
        Command::Check { .. } => {
            eprintln!("check — not yet implemented (Phase 2+)");
            std::process::exit(1);
        }
    }
    Ok(())
}
