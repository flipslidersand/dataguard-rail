use anyhow::Result;
use clap::{Parser, Subcommand};

#[derive(Parser)]
#[command(name = "dataguard-engine", about = "DataGuard Rail Rust analysis engine")]
struct Args {
    #[command(subcommand)]
    command: Command,
}

#[derive(Subcommand)]
enum Command {
    /// SQL ファイルからカラムリネージュを生成する
    Analyze {
        /// 解析対象の SQL ファイル
        #[arg(long)]
        sql: String,
        /// 出力先 JSON ファイル
        #[arg(long, default_value = "lineage.json")]
        out: String,
    },
    /// CSV ファイルに品質ルールを適用して violations を出力する
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
            println!("analyze: sql={sql} out={out} — not yet implemented");
        }
        Command::Check { input, rules } => {
            println!("check: input={input} rules={rules} — not yet implemented");
        }
    }
    Ok(())
}
