use crate::{check, lineage};
use anyhow::Context;
use tonic::{Request, Response, Status};

// tonic-build が proto から生成するモジュール
pub mod dataguard {
    #![allow(clippy::result_large_err)]
    tonic::include_proto!("dataguard");
}

use dataguard::data_guard_server::DataGuard;
use dataguard::{AnalyzeRequest, AnalyzeResponse, CheckRequest, CheckResponse};

/// パストラバーサル防止: ".." コンポーネントおよび許可されていない拡張子を拒否する。
/// `allowed_exts` はいずれか1つに（大小文字を無視して）一致すれば許可する。
#[allow(clippy::result_large_err)]
fn validate_path(path: &str, allowed_exts: &[&str]) -> Result<(), Status> {
    use std::path::{Component, Path};
    let p = Path::new(path);
    let has_parent_dir = p.components().any(|c| c == Component::ParentDir);
    let has_allowed_ext = p
        .extension()
        .map(|e| allowed_exts.iter().any(|ext| e.eq_ignore_ascii_case(ext)))
        .unwrap_or(false);
    if has_parent_dir || !has_allowed_ext {
        return Err(Status::invalid_argument(format!("無効なパスです: {path}")));
    }
    Ok(())
}

pub struct DataGuardService;

#[tonic::async_trait]
impl DataGuard for DataGuardService {
    async fn analyze(
        &self,
        request: Request<AnalyzeRequest>,
    ) -> Result<Response<AnalyzeResponse>, Status> {
        let sql_path = request.into_inner().sql_path;
        validate_path(&sql_path, &["sql"])?;

        let sql_text = std::fs::read_to_string(&sql_path)
            .with_context(|| format!("cannot read {sql_path}"))
            .map_err(|e| Status::invalid_argument(e.to_string()))?;

        let report = lineage::analyze(&sql_text).map_err(|e| Status::internal(e.to_string()))?;

        let lineage_json =
            serde_json::to_string(&report).map_err(|e| Status::internal(e.to_string()))?;

        Ok(Response::new(AnalyzeResponse { lineage_json }))
    }

    async fn check(
        &self,
        request: Request<CheckRequest>,
    ) -> Result<Response<CheckResponse>, Status> {
        let req = request.into_inner();
        validate_path(&req.csv_path, &["csv"])?;
        validate_path(&req.rules_path, &["yaml", "yml"])?;
        let detected_at = chrono::Utc::now().to_rfc3339();

        let violations = check::check_file(&req.csv_path, &req.rules_path, &detected_at)
            .map_err(|e| Status::internal(e.to_string()))?;

        let violations_json =
            serde_json::to_string(&violations).map_err(|e| Status::internal(e.to_string()))?;

        Ok(Response::new(CheckResponse { violations_json }))
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn validate_path_accepts_matching_extension() {
        assert!(validate_path("data/input.csv", &["csv"]).is_ok());
        assert!(validate_path("rules.YAML", &["yaml", "yml"]).is_ok());
        assert!(validate_path("rules.yml", &["yaml", "yml"]).is_ok());
    }

    #[test]
    fn validate_path_rejects_parent_dir_traversal() {
        assert!(validate_path("../../etc/passwd.csv", &["csv"]).is_err());
        assert!(validate_path("data/../../secret.sql", &["sql"]).is_err());
    }

    #[test]
    fn validate_path_rejects_wrong_extension() {
        assert!(validate_path("data/input.txt", &["csv"]).is_err());
        assert!(validate_path("rules.json", &["yaml", "yml"]).is_err());
        assert!(validate_path("no_extension", &["csv"]).is_err());
    }
}
