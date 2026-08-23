//! CSV ファイルの各カラムを統計プロファイリングする。
//!
//! 出力フィールド（カラムごと）:
//! - `null_count` / `null_rate` … 空セル数とその割合
//! - `unique_count`             … ユニーク値の数
//! - `min` / `max` / `mean`     … 数値カラムのみ (非数値は null)

use anyhow::{Context, Result};
use serde::Serialize;
use std::collections::HashSet;
use std::path::Path;

/// CSV ファイル全体のプロファイル結果。
#[derive(Debug, Serialize)]
pub struct ProfileReport {
    pub table: String,
    pub profiled_at: String,
    pub row_count: usize,
    pub columns: Vec<ColumnProfile>,
}

/// 1 カラムの統計情報。
#[derive(Debug, Serialize)]
pub struct ColumnProfile {
    pub name: String,
    pub null_count: usize,
    pub null_rate: f64,
    pub unique_count: usize,
    /// 数値カラムのみ Some。
    pub min: Option<f64>,
    pub max: Option<f64>,
    pub mean: Option<f64>,
}

/// CSV ファイルを読み込んでプロファイルレポートを返す。
pub fn profile_file(input: &str, profiled_at: &str) -> Result<ProfileReport> {
    let table = Path::new(input)
        .file_stem()
        .and_then(|s| s.to_str())
        .unwrap_or("input")
        .to_string();

    let mut reader =
        csv::Reader::from_path(input).with_context(|| format!("failed to open CSV: {input}"))?;
    let headers: Vec<String> = reader.headers()?.iter().map(|s| s.to_string()).collect();
    let records: Vec<Vec<String>> = reader
        .records()
        .map(|r| r.map(|rec| rec.iter().map(|s| s.to_string()).collect()))
        .collect::<std::result::Result<_, _>>()
        .context("failed to read CSV records")?;

    Ok(build_report(headers, records, table, profiled_at))
}

fn build_report(
    headers: Vec<String>,
    records: Vec<Vec<String>>,
    table: String,
    profiled_at: &str,
) -> ProfileReport {
    let row_count = records.len();
    let columns = headers
        .iter()
        .enumerate()
        .map(|(col_idx, name)| profile_column(name, col_idx, &records, row_count))
        .collect();

    ProfileReport {
        table,
        profiled_at: profiled_at.to_string(),
        row_count,
        columns,
    }
}

fn profile_column(name: &str, col_idx: usize, records: &[Vec<String>], row_count: usize) -> ColumnProfile {
    let mut null_count = 0usize;
    let mut uniques: HashSet<&str> = HashSet::new();
    let mut nums: Vec<f64> = Vec::new();

    for rec in records {
        let cell = rec.get(col_idx).map(|s| s.as_str()).unwrap_or("");
        if cell.trim().is_empty() {
            null_count += 1;
        } else {
            uniques.insert(cell);
            if let Ok(v) = cell.trim().parse::<f64>() {
                nums.push(v);
            }
        }
    }

    let null_rate = if row_count > 0 {
        null_count as f64 / row_count as f64
    } else {
        0.0
    };
    let unique_count = uniques.len();

    // 全ての非 null セルが数値として解釈できた場合のみ数値統計を出力
    let non_null = row_count - null_count;
    let (min, max, mean) = if non_null > 0 && nums.len() == non_null {
        let mn = nums.iter().cloned().fold(f64::INFINITY, f64::min);
        let mx = nums.iter().cloned().fold(f64::NEG_INFINITY, f64::max);
        let avg = nums.iter().sum::<f64>() / nums.len() as f64;
        (Some(mn), Some(mx), Some(avg))
    } else {
        (None, None, None)
    };

    ColumnProfile {
        name: name.to_string(),
        null_count,
        null_rate,
        unique_count,
        min,
        max,
        mean,
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn make_report(headers: Vec<&str>, rows: Vec<Vec<&str>>) -> ProfileReport {
        let h: Vec<String> = headers.into_iter().map(|s| s.to_string()).collect();
        let r: Vec<Vec<String>> = rows
            .into_iter()
            .map(|row| row.into_iter().map(|s| s.to_string()).collect())
            .collect();
        build_report(h, r, "test".to_string(), "T")
    }

    #[test]
    fn numeric_column_stats() {
        let report = make_report(
            vec!["price"],
            vec![vec!["10"], vec!["20"], vec!["30"]],
        );
        let col = &report.columns[0];
        assert_eq!(col.null_count, 0);
        assert_eq!(col.unique_count, 3);
        assert_eq!(col.min, Some(10.0));
        assert_eq!(col.max, Some(30.0));
        assert_eq!(col.mean, Some(20.0));
    }

    #[test]
    fn null_rate_calculation() {
        let report = make_report(
            vec!["email"],
            vec![vec!["a@x.com"], vec![""], vec!["  "], vec!["b@x.com"]],
        );
        let col = &report.columns[0];
        assert_eq!(col.null_count, 2);
        assert!((col.null_rate - 0.5).abs() < f64::EPSILON);
        assert_eq!(col.unique_count, 2);
        assert!(col.min.is_none());
    }

    #[test]
    fn mixed_column_no_numeric_stats() {
        let report = make_report(
            vec!["code"],
            vec![vec!["A1"], vec!["42"], vec!["B2"]],
        );
        let col = &report.columns[0];
        // "A1" は数値でないので min/max/mean = None
        assert!(col.min.is_none());
        assert_eq!(col.unique_count, 3);
    }

    #[test]
    fn empty_csv_zero_rows() {
        let report = make_report(vec!["x"], vec![]);
        assert_eq!(report.row_count, 0);
        let col = &report.columns[0];
        assert_eq!(col.null_count, 0);
        assert!((col.null_rate).abs() < f64::EPSILON);
    }

    #[test]
    fn multiple_columns_profiled() {
        let report = make_report(
            vec!["id", "name", "score"],
            vec![
                vec!["1", "Alice", "90"],
                vec!["2", "", "80"],
                vec!["3", "Bob", ""],
            ],
        );
        assert_eq!(report.columns.len(), 3);
        assert_eq!(report.columns[0].null_count, 0); // id
        assert_eq!(report.columns[1].null_count, 1); // name
        assert_eq!(report.columns[2].null_count, 1); // score
        // score: 2 non-null but only 1 row has parseable num if "" counts as null
        // "90" and "80" are both numeric, "" is null → nums.len()==2 == non_null==2
        assert_eq!(report.columns[2].min, Some(80.0));
        assert_eq!(report.columns[2].max, Some(90.0));
    }
}
