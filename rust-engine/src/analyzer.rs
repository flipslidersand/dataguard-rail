//! sqlparser-rs の AST を走査してカラムリネージュを抽出する。
//!
//! - FROM / JOIN からテーブル依存とエイリアスを解決する
//! - SELECT の各投影を出力カラム → ソースカラム (`table.column`) に対応づける
//! - WITH (CTE) は基底テーブルへ展開して `sources` に含める

use crate::lineage::Lineage;
use anyhow::{anyhow, bail, Result};
use sqlparser::ast::{
    Expr, Function, FunctionArg, FunctionArgExpr, FunctionArguments, Query, Select, SelectItem,
    SetExpr, Statement, TableFactor, TableWithJoins,
};
use sqlparser::dialect::GenericDialect;
use sqlparser::parser::Parser;
use std::collections::{BTreeMap, BTreeSet, HashMap};

/// SQL 文字列を解析し、最初の解析可能なクエリからリネージュを構築する。
pub fn analyze(sql: &str) -> Result<Lineage> {
    let statements =
        Parser::parse_sql(&GenericDialect {}, sql).map_err(|e| anyhow!("SQL parse error: {e}"))?;

    let stmt = statements
        .into_iter()
        .next()
        .ok_or_else(|| anyhow!("no SQL statement found"))?;

    let (target, query) = match stmt {
        // CREATE TABLE ... AS SELECT / CREATE VIEW ... AS SELECT は出力先テーブル名を持つ
        Statement::CreateTable { name, query, .. } => {
            let q =
                query.ok_or_else(|| anyhow!("CREATE TABLE without AS SELECT is not analyzable"))?;
            (object_name(&name), *q)
        }
        Statement::CreateView { name, query, .. } => (object_name(&name), *query),
        Statement::Query(q) => ("result".to_string(), *q),
        other => bail!("unsupported statement for lineage: {other:?}"),
    };

    analyze_query(target, &query)
}

fn analyze_query(target: String, query: &Query) -> Result<Lineage> {
    // CTE 名 → その定義クエリ。sources 展開で基底テーブルへ解決するのに使う。
    let mut ctes: HashMap<String, &Query> = HashMap::new();
    if let Some(with) = &query.with {
        for cte in &with.cte_tables {
            ctes.insert(cte.alias.name.value.clone(), &cte.query);
        }
    }

    let select = extract_select(query)?;

    // FROM / JOIN からエイリアス → テーブル名を解決する
    let mut alias_map: HashMap<String, String> = HashMap::new();
    let mut from_tables: Vec<String> = Vec::new();
    for twj in &select.from {
        collect_tables(twj, &mut alias_map, &mut from_tables);
    }

    // sources: CTE は基底テーブルへ展開し重複を除く
    let mut sources: BTreeSet<String> = BTreeSet::new();
    for t in &from_tables {
        expand_source(t, &ctes, &mut sources, &mut BTreeSet::new());
    }

    let single_table = if from_tables.len() == 1 {
        Some(from_tables[0].clone())
    } else {
        None
    };

    // 投影ごとに出力カラム → ソースカラムを対応づける
    let mut columns: BTreeMap<String, Vec<String>> = BTreeMap::new();
    for (i, item) in select.projection.iter().enumerate() {
        let (out_name, exprs): (String, Vec<&Expr>) = match item {
            SelectItem::UnnamedExpr(e) => (default_col_name(e, i), vec![e]),
            SelectItem::ExprWithAlias { expr, alias } => (alias.value.clone(), vec![expr]),
            SelectItem::Wildcard(_) => ("*".to_string(), vec![]),
            SelectItem::QualifiedWildcard(name, _) => (format!("{name}.*"), vec![]),
        };

        let mut srcs: BTreeSet<String> = BTreeSet::new();
        for e in exprs {
            collect_columns(e, &alias_map, single_table.as_deref(), &mut srcs);
        }
        columns.insert(out_name, srcs.into_iter().collect());
    }

    Ok(Lineage::build(
        target,
        sources.into_iter().collect(),
        columns,
    ))
}

/// クエリ本体から最初の SELECT を取り出す (UNION などは左辺を採用)。
fn extract_select(query: &Query) -> Result<&Select> {
    fn walk(body: &SetExpr) -> Option<&Select> {
        match body {
            SetExpr::Select(s) => Some(s),
            SetExpr::Query(q) => walk(&q.body),
            SetExpr::SetOperation { left, .. } => walk(left),
            _ => None,
        }
    }
    walk(&query.body).ok_or_else(|| anyhow!("query has no SELECT body"))
}

/// TableWithJoins (FROM 節の 1 要素と、それに続く JOIN) を走査して
/// エイリアス表と参照テーブル一覧を埋める。
fn collect_tables(
    twj: &TableWithJoins,
    alias_map: &mut HashMap<String, String>,
    tables: &mut Vec<String>,
) {
    collect_factor(&twj.relation, alias_map, tables);
    for join in &twj.joins {
        collect_factor(&join.relation, alias_map, tables);
    }
}

fn collect_factor(
    factor: &TableFactor,
    alias_map: &mut HashMap<String, String>,
    tables: &mut Vec<String>,
) {
    match factor {
        TableFactor::Table { name, alias, .. } => {
            let table = object_name(name);
            if let Some(a) = alias {
                alias_map.insert(a.name.value.clone(), table.clone());
            }
            // テーブル名自身も (完全修飾されていない) 参照キーとして登録する
            alias_map
                .entry(table.clone())
                .or_insert_with(|| table.clone());
            if !tables.contains(&table) {
                tables.push(table);
            }
        }
        TableFactor::Derived {
            subquery, alias, ..
        } => {
            // サブクエリ: エイリアスがあればそのエイリアス配下の基底テーブルを引く。
            // MVP では内側 FROM のテーブルを sources として拾う。
            let mut inner_alias: HashMap<String, String> = HashMap::new();
            if let Ok(sel) = extract_select(subquery) {
                for t in &sel.from {
                    collect_tables(t, &mut inner_alias, tables);
                }
            }
            if let Some(a) = alias {
                // サブクエリ別名は基底テーブルへ解決できないため、単一なら代表テーブルへ寄せる
                if let Some(first) = tables.last().cloned() {
                    alias_map.insert(a.name.value.clone(), first);
                }
            }
            alias_map.extend(inner_alias);
        }
        TableFactor::NestedJoin {
            table_with_joins, ..
        } => {
            collect_tables(table_with_joins, alias_map, tables);
        }
        _ => {}
    }
}

/// CTE 名なら定義を辿って基底テーブルへ展開し、そうでなければそのまま追加する。
fn expand_source(
    name: &str,
    ctes: &HashMap<String, &Query>,
    out: &mut BTreeSet<String>,
    seen: &mut BTreeSet<String>,
) {
    if !seen.insert(name.to_string()) {
        return; // 循環 CTE 参照の保険
    }
    if let Some(q) = ctes.get(name) {
        if let Ok(sel) = extract_select(q) {
            let mut alias_map: HashMap<String, String> = HashMap::new();
            let mut inner: Vec<String> = Vec::new();
            for twj in &sel.from {
                collect_tables(twj, &mut alias_map, &mut inner);
            }
            for t in inner {
                expand_source(&t, ctes, out, seen);
            }
        }
    } else {
        out.insert(name.to_string());
    }
}

/// 式を再帰的に走査し、カラム参照を `table.column` に解決して集める。
fn collect_columns(
    expr: &Expr,
    alias_map: &HashMap<String, String>,
    single_table: Option<&str>,
    out: &mut BTreeSet<String>,
) {
    match expr {
        Expr::Identifier(ident) => {
            // 修飾なしカラム: テーブルが 1 つに定まるならそれで修飾する
            if let Some(t) = single_table {
                out.insert(format!("{t}.{}", ident.value));
            } else {
                out.insert(ident.value.clone());
            }
        }
        Expr::CompoundIdentifier(parts) => {
            if parts.len() >= 2 {
                let qualifier = &parts[parts.len() - 2].value;
                let col = &parts[parts.len() - 1].value;
                let table = alias_map
                    .get(qualifier)
                    .cloned()
                    .unwrap_or_else(|| qualifier.clone());
                out.insert(format!("{table}.{col}"));
            } else if let Some(one) = parts.first() {
                out.insert(one.value.clone());
            }
        }
        Expr::BinaryOp { left, right, .. } => {
            collect_columns(left, alias_map, single_table, out);
            collect_columns(right, alias_map, single_table, out);
        }
        Expr::UnaryOp { expr, .. }
        | Expr::Nested(expr)
        | Expr::Cast { expr, .. }
        | Expr::IsNull(expr)
        | Expr::IsNotNull(expr) => {
            collect_columns(expr, alias_map, single_table, out);
        }
        Expr::Function(func) => collect_function_columns(func, alias_map, single_table, out),
        Expr::Case {
            operand,
            conditions,
            results,
            else_result,
        } => {
            if let Some(op) = operand {
                collect_columns(op, alias_map, single_table, out);
            }
            for c in conditions {
                collect_columns(c, alias_map, single_table, out);
            }
            for r in results {
                collect_columns(r, alias_map, single_table, out);
            }
            if let Some(e) = else_result {
                collect_columns(e, alias_map, single_table, out);
            }
        }
        Expr::Between {
            expr, low, high, ..
        } => {
            collect_columns(expr, alias_map, single_table, out);
            collect_columns(low, alias_map, single_table, out);
            collect_columns(high, alias_map, single_table, out);
        }
        Expr::InList { expr, list, .. } => {
            collect_columns(expr, alias_map, single_table, out);
            for e in list {
                collect_columns(e, alias_map, single_table, out);
            }
        }
        _ => {}
    }
}

fn collect_function_columns(
    func: &Function,
    alias_map: &HashMap<String, String>,
    single_table: Option<&str>,
    out: &mut BTreeSet<String>,
) {
    if let FunctionArguments::List(list) = &func.args {
        for arg in &list.args {
            let expr = match arg {
                FunctionArg::Unnamed(FunctionArgExpr::Expr(e)) => Some(e),
                FunctionArg::Named {
                    arg: FunctionArgExpr::Expr(e),
                    ..
                } => Some(e),
                _ => None,
            };
            if let Some(e) = expr {
                collect_columns(e, alias_map, single_table, out);
            }
        }
    }
}

/// エイリアスの無い投影に付ける既定のカラム名。
fn default_col_name(expr: &Expr, index: usize) -> String {
    match expr {
        Expr::Identifier(i) => i.value.clone(),
        Expr::CompoundIdentifier(parts) => parts
            .last()
            .map(|p| p.value.clone())
            .unwrap_or_else(|| format!("col_{index}")),
        _ => format!("col_{index}"),
    }
}

/// ObjectName をドット区切り文字列にする。
fn object_name(name: &sqlparser::ast::ObjectName) -> String {
    name.0
        .iter()
        .map(|i| i.value.clone())
        .collect::<Vec<_>>()
        .join(".")
}
