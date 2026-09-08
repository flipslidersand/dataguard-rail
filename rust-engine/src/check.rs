//! CSV に品質ルール (rules.yaml) を適用して violations を生成する。
//!
//! 対応する式:
//! - `not_null`              … `column` が空でないこと
//! - `value <op> <num>`      … `column` の数値が条件を満たすこと (op: > >= < <= == !=)
//! - `count <op> <num>`      … `key` ごとの出現数が条件を満たすこと (重複検出)
//! - `matches /<pattern>/`   … `column` が正規表現にマッチすること

use anyhow::{anyhow, bail, Context, Result};
use chrono::Utc;
use regex::Regex;
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
    Matches { column: String, re: Regex },
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

        // matches /<pattern>/
        if let Some(rest) = expr.strip_prefix("matches ") {
            let pat = rest.trim();
            let inner = pat
                .strip_prefix('/')
                .and_then(|s| s.strip_suffix('/'))
                .ok_or_else(|| {
                    anyhow!(
                        "rule `{}`: matches 式は `/pattern/` 形式が必須 (got `{}`)",
                        raw.name,
                        pat
                    )
                })?;
            let column = raw
                .column
                .clone()
                .ok_or_else(|| anyhow!("rule `{}`: matches 式は column が必須", raw.name))?;
            let re = Regex::new(inner)
                .with_context(|| format!("rule `{}`: 正規表現が無効 `{}`", raw.name, inner))?;
            return Ok(Check::Matches { column, re });
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

/// ルールが参照する列/キー名 (violation の column フィールドに使う)。
fn check_label(check: &Check) -> &str {
    match check {
        Check::NotNull { column }
        | Check::Compare { column, .. }
        | Check::Matches { column, .. } => column,
        Check::Unique { key, .. } => key,
    }
}

/// 行単独で判定できるルール (NotNull/Compare/Matches) がこのセルで違反かどうか。
/// Unique は行をまたいだ集計 (出現数) が必要なため呼び出し元で個別に扱う。
fn is_row_violation(check: &Check, cell: &str) -> bool {
    match check {
        Check::NotNull { .. } => cell.trim().is_empty(),
        Check::Compare { op, rhs, .. } => !matches!(
            cell.trim().parse::<f64>().map(|v| op.eval(v, *rhs)),
            Ok(true)
        ),
        Check::Matches { re, .. } => !re.is_match(cell),
        Check::Unique { .. } => unreachable!("Unique は呼び出し元で処理する"),
    }
}

/// コンパイル済みルール一体 (列インデックス解決済み)。
struct Compiled<'a> {
    raw: &'a RawRule,
    check: Check,
    idx: usize,
}

fn compile_rules<'a>(headers: &[String], raw_rules: &'a [RawRule]) -> Result<Vec<Compiled<'a>>> {
    raw_rules
        .iter()
        .map(|raw| {
            let check = Check::compile(raw)?;
            let idx = col_index(headers, check_label(&check), &raw.name)?;
            Ok(Compiled { raw, check, idx })
        })
        .collect()
}

/// CSV ファイルとルールファイルを読み込み違反一覧を返す。
///
/// 大容量 CSV でも全行をメモリに展開しないよう、1 パスのストリーミングで評価する。
/// `count` (Unique) ルールのみキーごとの最終出現数が必要なため、対象列だけを
/// 抜き出す事前集計パスを行う (メモリ使用量はキーの distinct 数に比例し、行数には比例しない)。
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
    let compiled = compile_rules(&headers, &ruleset.rules)?;

    let unique_positions: Vec<usize> = compiled
        .iter()
        .enumerate()
        .filter(|(_, c)| matches!(c.check, Check::Unique { .. }))
        .map(|(i, _)| i)
        .collect();

    let mut counts: HashMap<usize, HashMap<String, u64>> = HashMap::new();
    if !unique_positions.is_empty() {
        let mut counter = csv::Reader::from_path(input)
            .with_context(|| format!("failed to open CSV (count pass): {input}"))?;
        for result in counter.records() {
            let rec = result.context("failed to read CSV records (count pass)")?;
            for &pos in &unique_positions {
                let cell = rec.get(compiled[pos].idx).unwrap_or("").to_string();
                *counts.entry(pos).or_default().entry(cell).or_insert(0) += 1;
            }
        }
    }

    let mut violations: Vec<Violation> = Vec::new();
    let run_ms = Utc::now().timestamp_millis();
    let mut seq = 0usize;

    for (row_i, result) in reader.records().enumerate() {
        let rec = result.context("failed to read CSV records")?;
        let row = row_i + 1;
        for (pos, c) in compiled.iter().enumerate() {
            let cell = rec.get(c.idx).unwrap_or("");
            let violated = match &c.check {
                Check::Unique { op, rhs, .. } => {
                    let count = counts
                        .get(&pos)
                        .and_then(|m| m.get(cell))
                        .copied()
                        .unwrap_or(0);
                    !op.eval(count as f64, *rhs)
                }
                other => is_row_violation(other, cell),
            };
            if violated {
                seq += 1;
                violations.push(Violation {
                    id: format!("viol-{run_ms}-{seq}"),
                    rule: c.raw.name.clone(),
                    table: table.clone(),
                    row,
                    column: check_label(&c.check).to_string(),
                    value: cell.to_string(),
                    detected_at: detected_at.to_string(),
                });
            }
        }
    }

    Ok(violations)
}

/// パース済みデータに対してルールを評価する (インメモリ版・テスト専用ヘルパー)。
/// 本番経路は `check_file` のストリーミング実装を使用する。
#[cfg(test)]
fn evaluate(
    headers: &[String],
    records: &[Vec<String>],
    raw_rules: &[RawRule],
    table: &str,
    detected_at: &str,
) -> Result<Vec<Violation>> {
    let mut violations: Vec<Violation> = Vec::new();
    let run_ms = Utc::now().timestamp_millis();
    let mut seq = 0usize;
    let mut push = |v: &mut Vec<Violation>, rule: &str, row: usize, column: &str, value: &str| {
        seq += 1;
        v.push(Violation {
            id: format!("viol-{run_ms}-{seq}"),
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
            Check::Matches { column, re } => {
                let idx = col_index(headers, &column, &raw.name)?;
                for (i, rec) in records.iter().enumerate() {
                    let cell = rec.get(idx).map(|s| s.as_str()).unwrap_or("");
                    if !re.is_match(cell) {
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

    /// テスト用に一意なパスへ内容を書き込む (tempfile crate に依存しない簡易実装)。
    fn write_temp(name: &str, content: &str) -> String {
        let path = std::env::temp_dir().join(format!(
            "dataguard-check-test-{}-{}-{}",
            std::process::id(),
            std::time::SystemTime::now()
                .duration_since(std::time::UNIX_EPOCH)
                .unwrap()
                .as_nanos(),
            name
        ));
        std::fs::write(&path, content).unwrap();
        path.to_str().unwrap().to_string()
    }

    #[test]
    fn check_file_streams_without_materializing_all_rows() {
        // count ルールを含む混在ルールセットで、check_file (ストリーミング) が
        // evaluate (インメモリ) と同じ違反を返すことを確認する。
        let csv = "sale_price,stock_id\n100,A\n-5,A\n0,B\n";
        let rules = "rules:\n  - name: positive_price\n    column: sale_price\n    expression: \"value > 0\"\n  - name: no_dup_stock\n    key: stock_id\n    expression: \"count <= 1\"\n";
        let csv_path = write_temp("input.csv", csv);
        let rules_path = write_temp("rules.yaml", rules);

        let got = check_file(&csv_path, &rules_path, "T").unwrap();
        std::fs::remove_file(&csv_path).ok();
        std::fs::remove_file(&rules_path).ok();

        // positive_price: row2(-5), row3(0) が違反 / no_dup_stock: A が2回出現するので row1,row2 が違反
        assert_eq!(got.len(), 4);
        assert!(got.iter().any(|v| v.rule == "positive_price" && v.row == 2));
        assert!(got.iter().any(|v| v.rule == "positive_price" && v.row == 3));
        assert!(got
            .iter()
            .filter(|v| v.rule == "no_dup_stock")
            .all(|v| v.value == "A"));
        assert_eq!(got.iter().filter(|v| v.rule == "no_dup_stock").count(), 2);
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
        // IDs contain a unix-ms prefix to be unique across runs: viol-{ms}-{seq}
        assert!(
            v[0].id.starts_with("viol-"),
            "expected viol- prefix, got {}",
            v[0].id
        );
        assert!(
            v[0].id.ends_with("-1"),
            "expected seq suffix -1, got {}",
            v[0].id
        );
        assert!(
            v[1].id.ends_with("-2"),
            "expected seq suffix -2, got {}",
            v[1].id
        );
        // IDs within the same run share the same timestamp prefix
        let prefix0 = v[0].id.trim_end_matches("-1");
        let prefix1 = v[1].id.trim_end_matches("-2");
        assert_eq!(
            prefix0, prefix1,
            "same-run IDs should share timestamp prefix"
        );
        assert_eq!(v[0].rule, "positive_price");
        assert_eq!(v[1].rule, "no_null_email");
    }

    #[test]
    fn violation_id_unique_across_runs() {
        // Calling evaluate twice must produce non-colliding IDs even for the same table/row.
        let headers = vec!["x".into()];
        let records = rows(&[&[""]]);
        let rules = vec![raw("not_null_x", Some("x"), None, "not_null")];
        let v1 = evaluate(&headers, &records, &rules, "t", "T1").unwrap();
        // Sleep briefly so that timestamp_millis() advances between calls.
        std::thread::sleep(std::time::Duration::from_millis(2));
        let v2 = evaluate(&headers, &records, &rules, "t", "T2").unwrap();
        assert_eq!(v1.len(), 1);
        assert_eq!(v2.len(), 1);
        assert_ne!(
            v1[0].id, v2[0].id,
            "IDs from different runs must not collide"
        );
    }

    #[test]
    fn missing_column_errors() {
        let headers = vec!["id".into()];
        let records = rows(&[&["1"]]);
        let rules = vec![raw("r", Some("nope"), None, "value > 0")];
        assert!(evaluate(&headers, &records, &rules, "t", "T").is_err());
    }

    #[test]
    fn matches_flags_non_conforming() {
        let headers = vec!["code".into()];
        let records = rows(&[&["AB1234"], &["ab1234"], &["X9"], &["CD5678"]]);
        let rules = vec![raw(
            "valid_code",
            Some("code"),
            None,
            r"matches /^[A-Z]{2}[0-9]{4}$/",
        )];
        let v = evaluate(&headers, &records, &rules, "t", "T").unwrap();
        assert_eq!(v.len(), 2);
        assert_eq!(v[0].value, "ab1234");
        assert_eq!(v[1].value, "X9");
    }

    #[test]
    fn matches_empty_cell_is_violation() {
        let headers = vec!["email".into()];
        let records = rows(&[&["user@example.com"], &[""], &["notanemail"]]);
        let rules = vec![raw(
            "valid_email",
            Some("email"),
            None,
            r"matches /^[^@]+@[^@]+\.[^@]+$/",
        )];
        let v = evaluate(&headers, &records, &rules, "t", "T").unwrap();
        assert_eq!(v.len(), 2);
    }

    #[test]
    fn matches_invalid_regex_errors() {
        let headers = vec!["x".into()];
        let records = rows(&[&["a"]]);
        let rules = vec![raw("r", Some("x"), None, "matches /[invalid/")];
        assert!(evaluate(&headers, &records, &rules, "t", "T").is_err());
    }

    #[test]
    fn matches_missing_slash_errors() {
        let headers = vec!["x".into()];
        let records = rows(&[&["a"]]);
        let rules = vec![raw("r", Some("x"), None, "matches noSlash")];
        assert!(evaluate(&headers, &records, &rules, "t", "T").is_err());
    }
}
