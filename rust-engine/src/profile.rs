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

/// 1 カラムぶんの統計を 1 パスで積み上げるアキュムレータ。
/// 行データそのものは保持しないため、メモリ使用量はユニーク値の distinct 数に比例する
/// (全行を `Vec` に展開する旧実装は行数に比例し、大容量 CSV で OOM の原因になっていた)。
#[derive(Debug, Default, Clone)]
struct ColumnAcc {
    null_count: usize,
    uniques: HashSet<String>,
    numeric_count: usize,
    non_null_count: usize,
    min: f64,
    max: f64,
    sum: f64,
}

impl ColumnAcc {
    fn update(&mut self, cell: &str) {
        let trimmed = cell.trim();
        if trimmed.is_empty() {
            self.null_count += 1;
            return;
        }
        self.non_null_count += 1;
        self.uniques.insert(cell.to_string());
        if let Ok(v) = trimmed.parse::<f64>() {
            if self.numeric_count == 0 {
                self.min = v;
                self.max = v;
            } else {
                self.min = self.min.min(v);
                self.max = self.max.max(v);
            }
            self.sum += v;
            self.numeric_count += 1;
        }
    }

    fn finish(self, name: &str, row_count: usize) -> ColumnProfile {
        let null_rate = if row_count > 0 {
            self.null_count as f64 / row_count as f64
        } else {
            0.0
        };
        // 全ての非 null セルが数値として解釈できた場合のみ数値統計を出力
        let (min, max, mean) =
            if self.non_null_count > 0 && self.numeric_count == self.non_null_count {
                (
                    Some(self.min),
                    Some(self.max),
                    Some(self.sum / self.numeric_count as f64),
                )
            } else {
                (None, None, None)
            };

        ColumnProfile {
            name: name.to_string(),
            null_count: self.null_count,
            null_rate,
            unique_count: self.uniques.len(),
            min,
            max,
            mean,
        }
    }
}

/// CSV ファイルを読み込んでプロファイルレポートを返す (1 パスストリーミング)。
pub fn profile_file(input: &str, profiled_at: &str) -> Result<ProfileReport> {
    let table = Path::new(input)
        .file_stem()
        .and_then(|s| s.to_str())
        .unwrap_or("input")
        .to_string();

    let mut reader =
        csv::Reader::from_path(input).with_context(|| format!("failed to open CSV: {input}"))?;
    let headers: Vec<String> = reader.headers()?.iter().map(|s| s.to_string()).collect();

    let mut accs: Vec<ColumnAcc> = vec![ColumnAcc::default(); headers.len()];
    let mut row_count = 0usize;
    for result in reader.records() {
        let rec = result.context("failed to read CSV records")?;
        row_count += 1;
        for (col_idx, acc) in accs.iter_mut().enumerate() {
            let cell = rec.get(col_idx).unwrap_or("");
            acc.update(cell);
        }
    }

    let columns = headers
        .iter()
        .zip(accs)
        .map(|(name, acc)| acc.finish(name, row_count))
        .collect();

    Ok(ProfileReport {
        table,
        profiled_at: profiled_at.to_string(),
        row_count,
        columns,
    })
}

/// インメモリ版レポート構築 (テスト専用ヘルパー)。本番経路は `profile_file` を使用する。
#[cfg(test)]
fn build_report(
    headers: Vec<String>,
    records: Vec<Vec<String>>,
    table: String,
    profiled_at: &str,
) -> ProfileReport {
    let row_count = records.len();
    let mut accs: Vec<ColumnAcc> = vec![ColumnAcc::default(); headers.len()];
    for rec in &records {
        for (col_idx, acc) in accs.iter_mut().enumerate() {
            let cell = rec.get(col_idx).map(|s| s.as_str()).unwrap_or("");
            acc.update(cell);
        }
    }

    let columns = headers
        .iter()
        .zip(accs)
        .map(|(name, acc)| acc.finish(name, row_count))
        .collect();

    ProfileReport {
        table,
        profiled_at: profiled_at.to_string(),
        row_count,
        columns,
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

    /// テスト用に一意なパスへ内容を書き込む (tempfile crate に依存しない簡易実装)。
    fn write_temp(name: &str, content: &str) -> String {
        let path = std::env::temp_dir().join(format!(
            "dataguard-profile-test-{}-{}-{}",
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
    fn profile_file_streams_same_result_as_build_report() {
        let csv = "id,score\n1,90\n2,\n3,80\n";
        let path = write_temp("input.csv", csv);
        let got = profile_file(&path, "T").unwrap();
        std::fs::remove_file(&path).ok();

        assert_eq!(got.row_count, 3);
        let score = got.columns.iter().find(|c| c.name == "score").unwrap();
        assert_eq!(score.null_count, 1);
        assert_eq!(score.min, Some(80.0));
        assert_eq!(score.max, Some(90.0));
        assert_eq!(score.mean, Some(85.0));
    }

    #[test]
    fn numeric_column_stats() {
        let report = make_report(vec!["price"], vec![vec!["10"], vec!["20"], vec!["30"]]);
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
        let report = make_report(vec!["code"], vec![vec!["A1"], vec!["42"], vec!["B2"]]);
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
