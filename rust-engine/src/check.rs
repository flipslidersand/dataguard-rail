//! CSV に品質ルール (rules.yaml) を適用して violations を生成する。
//!
//! 対応する式:
//! - `not_null`           … `column` が空でないこと
//! - `value <op> <num>`   … `column` の数値が条件を満たすこと (op: > >= < <= == !=)
//! - `count <op> <num>`   … `key` ごとの出現数が条件を満たすこと (重複検出)

use anyhow::{anyhow, bail, Context, Result};
use serde::{Deserialize, Serialize};
use std::collections::HashMap;
use std::path::Path;

/// rules.yaml のトップレベル。
#[derive(Debug, Deserialize)]
struct RuleSet {
    rules: Vec<RawRule>,
}

/// YAML 上のルール定義 (data-model.md の QualityRule)。
#[derive(Debug, Deserialize)]
struct RawRule {
    name: String,
    column: Option<String>,
    key: Option<String>,
    #[allow(dead_code)]
    window: Option<String>,
    expression: String,
}

/// 出力する違反レコード (data-model.md の Violation)。
#[derive(Debug, Serialize, PartialEq, Eq)]
pub struct Violation {
    pub id: String,
    pub rule: String,
    pub table: String,
    pub row: usize,
    pub column: String,
    pub value: String,
    pub detected_at: String,
}

/// 比較演算子。
#[derive(Debug, Clone, Copy)]
enum Op {
    Gt,
    Ge,
    Lt,
    Le,
    Eq,
    Ne,
}

impl Op {
    fn parse(s: &str) -> Option<Op> {
        Some(match s {
            ">" => Op::Gt,
            ">=" => Op::Ge,
            "<" => Op::Lt,
            "<=" => Op::Le,
            "==" | "=" => Op::Eq,
            "!=" | "<>" => Op::Ne,
            _ => return None,
        })
    }

    fn eval(self, l: f64, r: f64) -> bool {
        match self {
            Op::Gt => l > r,
            Op::Ge => l >= r,
            Op::Lt => l < r,
            Op::Le => l <= r,
            Op::Eq => (l - r).abs() < f64::EPSILON,
            Op::Ne => (l - r).abs() >= f64::EPSILON,
        }
    }
}

/// コンパイル済みルール。
#[derive(Debug)]
enum Check {
    NotNull { column: String },
    Compare { column: String, op: Op, rhs: f64 },
    Unique { key: String, op: Op, rhs: f64 },
}

impl Check {
    /// YAML のルールを検証可能な形にコンパイルする。
    fn compile(raw: &RawRule) -> Result<Check> {
        let expr = raw.expression.trim();

        if expr == "not_null" {
            let column = raw
                .column
                .clone()
                .ok_or_else(|| anyhow!("rule `{}`: not_null は column が必須", raw.name))?;
            return Ok(Check::NotNull { column });
        }

        let tokens: Vec<&str> = expr.split_whitespace().collect();
        if tokens.len() == 3 {
            let op = Op::parse(tokens[1])
                .ok_or_else(|| anyhow!("rule `{}`: 未対応の演算子 `{}`", raw.name, tokens[1]))?;
            let rhs: f64 = tokens[2].parse().with_context(|| {
                format!("rule `{}`: 右辺が数値でない `{}`", raw.name, tokens[2])
            })?;

            match tokens[0] {
                "value" => {
                    let column = raw
                        .column
                        .clone()
                        .ok_or_else(|| anyhow!("rule `{}`: value 式は column が必須", raw.name))?;
                    return Ok(Check::Compare { column, op, rhs });
                }
                "count" => {
                    let key = raw
                        .key
                        .clone()
                        .ok_or_else(|| anyhow!("rule `{}`: count 式は key が必須", raw.name))?;
                    return Ok(Check::Unique { key, op, rhs });
                }
                _ => {}
            }
        }

        bail!("rule `{}`: 未対応の式 `{}`", raw.name, expr)
    }
}

/// ヘッダから列インデックスを引く。無ければエラー。
fn col_index(headers: &[String], name: &str, rule: &str) -> Result<usize> {
    headers
        .iter()
        .position(|h| h == name)
        .ok_or_else(|| anyhow!("rule `{}`: 列 `{}` が CSV に存在しない", rule, name))
}

/// CSV ファイルとルールファイルを読み込み違反一覧を返す。
pub fn check_file(input: &str, rules: &str, detected_at: &str) -> Result<Vec<Violation>> {
    let table = Path::new(input)
        .file_stem()
        .and_then(|s| s.to_str())
        .unwrap_or("input")
        .to_string();

    let yaml =
        std::fs::read_to_string(rules).with_context(|| format!("failed to read rules: {rules}"))?;
    let ruleset: RuleSet =
        serde_yaml::from_str(&yaml).with_context(|| format!("invalid rules yaml: {rules}"))?;

    let mut reader =
        csv::Reader::from_path(input).with_context(|| format!("failed to open CSV: {input}"))?;
    let headers: Vec<String> = reader.headers()?.iter().map(|s| s.to_string()).collect();
    let records: Vec<Vec<String>> = reader
        .records()
        .map(|r| r.map(|rec| rec.iter().map(|s| s.to_string()).collect()))
        .collect::<std::result::Result<_, _>>()
        .context("failed to read CSV records")?;

    evaluate(&headers, &records, &ruleset.rules, &table, detected_at)
}

/// パース済みデータに対してルールを評価する (テスト可能なコア)。
fn evaluate(
    headers: &[String],
    records: &[Vec<String>],
    raw_rules: &[RawRule],
    table: &str,
    detected_at: &str,
) -> Result<Vec<Violation>> {
    let mut violations: Vec<Violation> = Vec::new();
    let mut seq = 0usize;
    let mut push = |v: &mut Vec<Violation>, rule: &str, row: usize, column: &str, value: &str| {
        seq += 1;
        v.push(Violation {
            id: format!("viol-{seq}"),
            rule: rule.to_string(),
            table: table.to_string(),
            row,
            column: column.to_string(),
            value: value.to_string(),
            detected_at: detected_at.to_string(),
        });
    };

    for raw in raw_rules {
        match Check::compile(raw)? {
            Check::NotNull { column } => {
                let idx = col_index(headers, &column, &raw.name)?;
                for (i, rec) in records.iter().enumerate() {
                    let cell = rec.get(idx).map(|s| s.as_str()).unwrap_or("");
                    if cell.trim().is_empty() {
                        push(&mut violations, &raw.name, i + 1, &column, cell);
                    }
                }
            }
            Check::Compare { column, op, rhs } => {
                let idx = col_index(headers, &column, &raw.name)?;
                for (i, rec) in records.iter().enumerate() {
                    let cell = rec.get(idx).map(|s| s.as_str()).unwrap_or("");
                    let ok = cell.trim().parse::<f64>().map(|v| op.eval(v, rhs));
                    // 数値化できない or 条件を満たさない → 違反
                    if !matches!(ok, Ok(true)) {
                        push(&mut violations, &raw.name, i + 1, &column, cell);
                    }
                }
            }
            Check::Unique { key, op, rhs } => {
                let idx = col_index(headers, &key, &raw.name)?;
                // key の値ごとに出現数を数える
                let mut counts: HashMap<&str, u64> = HashMap::new();
                for rec in records {
                    let cell = rec.get(idx).map(|s| s.as_str()).unwrap_or("");
                    *counts.entry(cell).or_insert(0) += 1;
                }
                for (i, rec) in records.iter().enumerate() {
                    let cell = rec.get(idx).map(|s| s.as_str()).unwrap_or("");
                    let count = *counts.get(cell).unwrap_or(&0);
                    if !op.eval(count as f64, rhs) {
                        push(&mut violations, &raw.name, i + 1, &key, cell);
                    }
                }
            }
        }
    }

    Ok(violations)
}

#[cfg(test)]
mod tests {
    use super::*;

    fn raw(name: &str, column: Option<&str>, key: Option<&str>, expr: &str) -> RawRule {
        RawRule {
            name: name.to_string(),
            column: column.map(|s| s.to_string()),
            key: key.map(|s| s.to_string()),
            window: None,
            expression: expr.to_string(),
        }
    }

    fn rows(data: &[&[&str]]) -> Vec<Vec<String>> {
        data.iter()
            .map(|r| r.iter().map(|s| s.to_string()).collect())
            .collect()
    }

    #[test]
    fn value_comparison_flags_negative() {
        let headers = vec!["id".into(), "sale_price".into()];
        let records = rows(&[&["1", "100"], &["2", "-5"], &["3", "0"]]);
        let rules = vec![raw("positive_price", Some("sale_price"), None, "value > 0")];
        let v = evaluate(&headers, &records, &rules, "products", "T").unwrap();
        assert_eq!(v.len(), 2); // -5 と 0
        assert_eq!(v[0].row, 2);
        assert_eq!(v[0].value, "-5");
        assert_eq!(v[0].column, "sale_price");
        assert_eq!(v[1].row, 3);
    }

    #[test]
    fn not_null_flags_empty() {
        let headers = vec!["id".into(), "email".into()];
        let records = rows(&[&["1", "a@x.com"], &["2", ""], &["3", "  "]]);
        let rules = vec![raw("no_null_email", Some("email"), None, "not_null")];
        let v = evaluate(&headers, &records, &rules, "users", "T").unwrap();
        assert_eq!(v.len(), 2);
        assert_eq!(v[0].row, 2);
        assert_eq!(v[1].row, 3);
    }

    #[test]
    fn count_detects_duplicates() {
        let headers = vec!["stock_id".into()];
        let records = rows(&[&["A"], &["B"], &["A"], &["A"]]);
        let rules = vec![raw(
            "no_duplicate_stock",
            None,
            Some("stock_id"),
            "count <= 1",
        )];
        let v = evaluate(&headers, &records, &rules, "stock", "T").unwrap();
        // A が 3 回 → 該当 3 行が違反、B は 1 回で OK
        assert_eq!(v.len(), 3);
        assert!(v.iter().all(|x| x.value == "A"));
    }

    #[test]
    fn multiple_rules_accumulate_and_id_sequential() {
        let headers = vec!["sale_price".into(), "email".into()];
        let records = rows(&[&["-1", ""], &["5", "ok"]]);
        let rules = vec![
            raw("positive_price", Some("sale_price"), None, "value > 0"),
            raw("no_null_email", Some("email"), None, "not_null"),
        ];
        let v = evaluate(&headers, &records, &rules, "t", "T").unwrap();
        assert_eq!(v.len(), 2);
        assert_eq!(v[0].id, "viol-1");
        assert_eq!(v[1].id, "viol-2");
        assert_eq!(v[0].rule, "positive_price");
        assert_eq!(v[1].rule, "no_null_email");
    }

    #[test]
    fn missing_column_errors() {
        let headers = vec!["id".into()];
        let records = rows(&[&["1"]]);
        let rules = vec![raw("r", Some("nope"), None, "value > 0")];
        assert!(evaluate(&headers, &records, &rules, "t", "T").is_err());
    }
}
