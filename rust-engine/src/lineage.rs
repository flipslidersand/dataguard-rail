//! カラムリネージュのデータモデルと petgraph によるグラフ表現。

use petgraph::algo::is_cyclic_directed;
use petgraph::graph::{DiGraph, NodeIndex};
use serde::Serialize;
use std::collections::{BTreeMap, HashMap};

/// `analyze` の最終出力 (spec.md / data-model.md 準拠)。
#[derive(Debug, Serialize, PartialEq, Eq)]
pub struct Lineage {
    /// 出力先テーブル名。CREATE TABLE/VIEW ... AS の場合はその名前、
    /// 素の SELECT の場合は "result"。
    pub target: String,
    /// 参照している元テーブル (CTE は基底テーブルへ展開済み)。
    pub sources: Vec<String>,
    /// 出力カラム名 → ソースカラム (`table.column`) のリスト。
    pub columns: BTreeMap<String, Vec<String>>,
    /// petgraph の `is_cyclic_directed()` による循環依存の有無。
    pub has_cycle: bool,
}

impl Lineage {
    /// 抽出済みの (target, sources, 出力カラム→ソースカラム) からリネージュを構築し、
    /// petgraph でグラフ化して循環依存を判定する。
    pub fn build(
        target: String,
        sources: Vec<String>,
        column_map: BTreeMap<String, Vec<String>>,
    ) -> Self {
        let mut graph: DiGraph<String, ()> = DiGraph::new();
        let mut index: HashMap<String, NodeIndex> = HashMap::new();

        let node = |g: &mut DiGraph<String, ()>,
                    idx: &mut HashMap<String, NodeIndex>,
                    label: &str|
         -> NodeIndex {
            *idx.entry(label.to_string())
                .or_insert_with(|| g.add_node(label.to_string()))
        };

        for (out_col, src_cols) in &column_map {
            let target_label = format!("{target}.{out_col}");
            let target_idx = node(&mut graph, &mut index, &target_label);
            for src in src_cols {
                let src_idx = node(&mut graph, &mut index, src);
                graph.add_edge(src_idx, target_idx, ());
            }
        }

        let has_cycle = is_cyclic_directed(&graph);

        Lineage {
            target,
            sources,
            columns: column_map,
            has_cycle,
        }
    }
}
