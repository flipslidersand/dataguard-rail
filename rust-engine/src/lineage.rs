use anyhow::{Context, Result};
use petgraph::graph::DiGraph;
use serde::{Deserialize, Serialize};
use sqlparser::ast::{
    Expr, Query, Select, SelectItem, SetExpr, Statement, TableFactor, TableWithJoins,
};
use sqlparser::dialect::GenericDialect;
use sqlparser::parser::Parser;
use std::collections::HashMap;

/// 1 SQL ファイルのリネージュ解析結果。
#[derive(Debug, Serialize, Deserialize)]
pub struct LineageReport {
    pub tables: Vec<String>,
    pub edges: Vec<LineageEdge>,
    pub statements: Vec<StatementLineage>,
}

/// テーブル間の依存関係（to が from に依存する）。
#[derive(Debug, Serialize, Deserialize)]
pub struct LineageEdge {
    pub from: String,
    pub to: String,
}

/// 1 ステートメントのリネージュ。
#[derive(Debug, Serialize, Deserialize)]
pub struct StatementLineage {
    pub kind: String,
    pub target: Option<String>,
    pub sources: Vec<String>,
    pub columns: Vec<ColumnRef>,
    pub sql_ref: String,
}

/// カラムレベルのリネージュ（Phase 1 は best-effort）。
#[derive(Debug, Serialize, Deserialize)]
pub struct ColumnRef {
    pub source_table: Option<String>,
    pub source_column: String,
    pub alias: Option<String>,
}

/// SQL テキストを解析してリネージュレポートを生成する。
pub fn analyze(sql: &str) -> Result<LineageReport> {
    let dialect = GenericDialect {};
    let stmts = Parser::parse_sql(&dialect, sql)
        .with_context(|| format!("SQL parse failed: {}", &sql[..sql.len().min(120)]))?;

    let mut graph: DiGraph<String, ()> = DiGraph::new();
    let mut node_index: HashMap<String, petgraph::graph::NodeIndex> = HashMap::new();
    let mut statement_lineages = Vec::new();

    for stmt in &stmts {
        let sl = analyze_statement(stmt);
        // テーブルノードを graph に追加
        for tbl in sl.sources.iter().chain(sl.target.iter()) {
            let t = tbl.to_lowercase();
            node_index.entry(t.clone()).or_insert_with(|| graph.add_node(t));
        }
        // エッジ: source → target
        if let Some(ref target) = sl.target {
            let t_lower = target.to_lowercase();
            let tgt_idx = node_index[&t_lower];
            for src in &sl.sources {
                let s_lower = src.to_lowercase();
                let src_idx = node_index[&s_lower];
                if !graph.contains_edge(src_idx, tgt_idx) {
                    graph.add_edge(src_idx, tgt_idx, ());
                }
            }
        }
        statement_lineages.push(sl);
    }

    let tables: Vec<String> = graph
        .node_indices()
        .map(|i| graph[i].clone())
        .collect();

    let edges: Vec<LineageEdge> = graph
        .edge_indices()
        .map(|e| {
            let (src, dst) = graph.edge_endpoints(e).unwrap();
            LineageEdge {
                from: graph[src].clone(),
                to: graph[dst].clone(),
            }
        })
        .collect();

    Ok(LineageReport { tables, edges, statements: statement_lineages })
}

fn analyze_statement(stmt: &Statement) -> StatementLineage {
    let sql_ref = stmt.to_string();
    match stmt {
        Statement::Query(q) => {
            let sources = collect_query_sources(q);
            let columns = collect_select_columns(q);
            StatementLineage {
                kind: "SELECT".into(),
                target: None,
                sources,
                columns,
                sql_ref,
            }
        }
        Statement::CreateTable { name, query: Some(q), .. } => {
            let target = name.to_string();
            let sources = collect_query_sources(q);
            let columns = collect_select_columns(q);
            StatementLineage {
                kind: "CREATE_TABLE_AS".into(),
                target: Some(target),
                sources,
                columns,
                sql_ref,
            }
        }
        Statement::Insert(insert) => {
            let target = insert.table_name.to_string();
            let sources = insert
                .source
                .as_ref()
                .map(|q| collect_query_sources(q))
                .unwrap_or_default();
            StatementLineage {
                kind: "INSERT".into(),
                target: Some(target),
                sources,
                columns: vec![],
                sql_ref,
            }
        }
        _ => StatementLineage {
            kind: "OTHER".into(),
            target: None,
            sources: vec![],
            columns: vec![],
            sql_ref,
        },
    }
}

fn collect_query_sources(q: &Query) -> Vec<String> {
    let mut tables = Vec::new();
    collect_set_expr_sources(&q.body, &mut tables);
    tables.dedup();
    tables
}

fn collect_set_expr_sources(expr: &SetExpr, tables: &mut Vec<String>) {
    match expr {
        SetExpr::Select(s) => collect_select_sources(s, tables),
        SetExpr::SetOperation { left, right, .. } => {
            collect_set_expr_sources(left, tables);
            collect_set_expr_sources(right, tables);
        }
        SetExpr::Query(q) => collect_set_expr_sources(&q.body, tables),
        _ => {}
    }
}

fn collect_select_sources(select: &Select, tables: &mut Vec<String>) {
    for twj in &select.from {
        collect_table_with_joins(twj, tables);
    }
}

fn collect_table_with_joins(twj: &TableWithJoins, tables: &mut Vec<String>) {
    extract_table_factor(&twj.relation, tables);
    for join in &twj.joins {
        extract_table_factor(&join.relation, tables);
    }
}

fn extract_table_factor(factor: &TableFactor, tables: &mut Vec<String>) {
    match factor {
        TableFactor::Table { name, .. } => {
            tables.push(name.to_string());
        }
        TableFactor::Derived { subquery, .. } => {
            collect_set_expr_sources(&subquery.body, tables);
        }
        TableFactor::NestedJoin { table_with_joins, .. } => {
            collect_table_with_joins(table_with_joins, tables);
        }
        _ => {}
    }
}

fn collect_select_columns(q: &Query) -> Vec<ColumnRef> {
    let mut cols = Vec::new();
    if let SetExpr::Select(s) = q.body.as_ref() {
        for item in &s.projection {
            match item {
                SelectItem::UnnamedExpr(expr) => {
                    if let Some(cr) = expr_to_col_ref(expr, None) {
                        cols.push(cr);
                    }
                }
                SelectItem::ExprWithAlias { expr, alias } => {
                    if let Some(cr) = expr_to_col_ref(expr, Some(alias.value.clone())) {
                        cols.push(cr);
                    }
                }
                SelectItem::Wildcard(_) => {
                    cols.push(ColumnRef { source_table: None, source_column: "*".into(), alias: None });
                }
                _ => {}
            }
        }
    }
    cols
}

fn expr_to_col_ref(expr: &Expr, alias: Option<String>) -> Option<ColumnRef> {
    match expr {
        Expr::Identifier(id) => Some(ColumnRef {
            source_table: None,
            source_column: id.value.clone(),
            alias,
        }),
        Expr::CompoundIdentifier(parts) if parts.len() == 2 => Some(ColumnRef {
            source_table: Some(parts[0].value.clone()),
            source_column: parts[1].value.clone(),
            alias,
        }),
        _ => None,
    }
}
