use anyhow::{Context, Result};
use clap::{Parser, Subcommand};
use std::fs;

mod analyzer;
mod lineage;

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
            let sql_text = fs::read_to_string(&sql)
                .with_context(|| format!("failed to read SQL file: {sql}"))?;
            let lineage = analyzer::analyze(&sql_text)?;
            let json = serde_json::to_string_pretty(&lineage)?;
            fs::write(&out, &json).with_context(|| format!("failed to write output: {out}"))?;
            println!("wrote lineage to {out}");
            if lineage.has_cycle {
                eprintln!("warning: column lineage contains a cycle");
            }
        }
        Command::Check { input, rules } => {
            println!("check: input={input} rules={rules} — not yet implemented");
        }
    }
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::analyzer::analyze;

    #[test]
    fn simple_select_single_table() {
        let l = analyze("SELECT id, sale_price FROM products").unwrap();
        assert_eq!(l.target, "result");
        assert_eq!(l.sources, vec!["products"]);
        assert_eq!(l.columns["id"], vec!["products.id"]);
        assert_eq!(l.columns["sale_price"], vec!["products.sale_price"]);
        assert!(!l.has_cycle);
    }

    #[test]
    fn join_with_aliases() {
        let sql = "SELECT o.id AS order_id, p.name AS product \
                   FROM orders o JOIN products p ON o.product_id = p.id";
        let l = analyze(sql).unwrap();
        assert_eq!(l.sources, vec!["orders", "products"]);
        assert_eq!(l.columns["order_id"], vec!["orders.id"]);
        assert_eq!(l.columns["product"], vec!["products.name"]);
    }

    #[test]
    fn expression_collects_multiple_sources() {
        let sql = "SELECT SUM(o.amount * (1 + p.tax_rate)) AS total_revenue \
                   FROM orders o JOIN products p ON o.product_id = p.id";
        let l = analyze(sql).unwrap();
        assert_eq!(
            l.columns["total_revenue"],
            vec!["orders.amount", "products.tax_rate"]
        );
    }

    #[test]
    fn create_table_as_sets_target() {
        let sql = "CREATE TABLE monthly_sales AS \
                   SELECT SUM(amount) AS total FROM orders";
        let l = analyze(sql).unwrap();
        assert_eq!(l.target, "monthly_sales");
        assert_eq!(l.sources, vec!["orders"]);
        assert_eq!(l.columns["total"], vec!["orders.amount"]);
    }

    #[test]
    fn cte_expands_to_base_tables() {
        let sql = "WITH recent AS (SELECT id, amount FROM orders) \
                   SELECT id, amount FROM recent";
        let l = analyze(sql).unwrap();
        // CTE `recent` は基底テーブル `orders` に展開される
        assert_eq!(l.sources, vec!["orders"]);
    }
}
