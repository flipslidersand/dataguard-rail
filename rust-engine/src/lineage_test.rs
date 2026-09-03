#[cfg(test)]
mod tests {
    use crate::lineage::analyze;

    #[test]
    fn simple_select_extracts_source_table() {
        let report = analyze("SELECT id, name FROM customers").unwrap();
        assert!(report.tables.contains(&"customers".to_string()));
        assert_eq!(report.statements.len(), 1);
        assert_eq!(report.statements[0].kind, "SELECT");
        assert!(report.statements[0]
            .sources
            .contains(&"customers".to_string()));
    }

    #[test]
    fn join_extracts_both_tables() {
        let sql = "SELECT o.id, c.name FROM orders o JOIN customers c ON o.customer_id = c.id";
        let report = analyze(sql).unwrap();
        let tables: Vec<&str> = report.tables.iter().map(|s| s.as_str()).collect();
        assert!(
            tables.contains(&"orders"),
            "expected orders in {:?}",
            tables
        );
        assert!(
            tables.contains(&"customers"),
            "expected customers in {:?}",
            tables
        );
    }

    #[test]
    fn create_table_as_select_has_target() {
        let sql = "CREATE TABLE monthly_sales AS SELECT SUM(amount) AS total FROM orders";
        let report = analyze(sql).unwrap();
        let stmt = &report.statements[0];
        assert_eq!(stmt.kind, "CREATE_TABLE_AS");
        assert_eq!(stmt.target.as_deref(), Some("monthly_sales"));
        assert!(stmt.sources.contains(&"orders".to_string()));
    }

    #[test]
    fn create_table_builds_edge() {
        let sql = "CREATE TABLE summary AS SELECT * FROM raw_data";
        let report = analyze(sql).unwrap();
        assert_eq!(report.edges.len(), 1);
        assert_eq!(report.edges[0].from, "raw_data");
        assert_eq!(report.edges[0].to, "summary");
    }

    #[test]
    fn column_references_extracted() {
        let sql = "SELECT o.amount, c.name AS customer_name FROM orders o JOIN customers c ON o.id = c.id";
        let report = analyze(sql).unwrap();
        let cols = &report.statements[0].columns;
        let col_names: Vec<&str> = cols.iter().map(|c| c.source_column.as_str()).collect();
        assert!(
            col_names.contains(&"amount"),
            "expected amount in {:?}",
            col_names
        );
        assert!(
            col_names.contains(&"name"),
            "expected name in {:?}",
            col_names
        );
        let name_col = cols.iter().find(|c| c.source_column == "name").unwrap();
        assert_eq!(name_col.alias.as_deref(), Some("customer_name"));
    }

    #[test]
    fn multiple_statements_all_analyzed() {
        let sql = "SELECT * FROM a; SELECT * FROM b;";
        let report = analyze(sql).unwrap();
        assert_eq!(report.statements.len(), 2);
        assert!(report.tables.contains(&"a".to_string()));
        assert!(report.tables.contains(&"b".to_string()));
    }

    #[test]
    fn invalid_sql_returns_error() {
        let result = analyze("NOT VALID SQL ;;;");
        assert!(result.is_err(), "expected parse error for invalid SQL");
    }
}
