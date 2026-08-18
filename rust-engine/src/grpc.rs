use crate::{check, lineage};
use anyhow::Context;
use tonic::{Request, Response, Status};

// tonic-build が proto から生成するモジュール
pub mod dataguard {
    tonic::include_proto!("dataguard");
}

use dataguard::data_guard_server::DataGuard;
use dataguard::{AnalyzeRequest, AnalyzeResponse, CheckRequest, CheckResponse};

#[derive(Default)]
pub struct DataGuardService;

#[tonic::async_trait]
impl DataGuard for DataGuardService {
    async fn analyze(
        &self,
        request: Request<AnalyzeRequest>,
    ) -> Result<Response<AnalyzeResponse>, Status> {
        let sql_path = request.into_inner().sql_path;

        let sql_text = std::fs::read_to_string(&sql_path)
            .with_context(|| format!("cannot read {sql_path}"))
            .map_err(|e| Status::invalid_argument(e.to_string()))?;

        let report = lineage::analyze(&sql_text)
            .map_err(|e| Status::internal(e.to_string()))?;

        let lineage_json = serde_json::to_string(&report)
            .map_err(|e| Status::internal(e.to_string()))?;

        Ok(Response::new(AnalyzeResponse { lineage_json }))
    }

    async fn check(
        &self,
        request: Request<CheckRequest>,
    ) -> Result<Response<CheckResponse>, Status> {
        let req = request.into_inner();
        let detected_at = chrono::Utc::now().to_rfc3339();

        let violations = check::check_file(&req.csv_path, &req.rules_path, &detected_at)
            .map_err(|e| Status::internal(e.to_string()))?;

        let violations_json = serde_json::to_string(&violations)
            .map_err(|e| Status::internal(e.to_string()))?;

        Ok(Response::new(CheckResponse { violations_json }))
    }
}
