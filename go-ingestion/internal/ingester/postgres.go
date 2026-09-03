package ingester

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// MaxPostgresRows は rowsToDataset が許容する最大行数。
// これを超えると OOM リスクがあるためエラーにする。
const MaxPostgresRows = 100_000

// queryRows は pgx.Rows の必要部分を抽象化したもの。DB 無しで rowsToDataset を
// 単体テストできるようにするための interface。
type queryRows interface {
	FieldDescriptions() []pgconn.FieldDescription
	Next() bool
	Values() ([]any, error)
	Err() error
}

// LoadPostgres は dsn へ接続し query を実行、結果を Dataset に変換する。
// timeout でクエリ全体にタイムアウトを設定する。
func LoadPostgres(ctx context.Context, dsn, query string, timeout time.Duration) (*Dataset, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("connect postgres: %w", err)
	}
	defer conn.Close(ctx)

	rows, err := conn.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("query postgres: %w", err)
	}
	defer rows.Close()

	return rowsToDataset(rows)
}

// rowsToDataset は結果セットをヘッダ + 文字列行の Dataset に変換する。
// 全セルは CSV に書けるよう文字列化する (Rust engine 側が型を解釈する)。
func rowsToDataset(rows queryRows) (*Dataset, error) {
	fields := rows.FieldDescriptions()
	headers := make([]string, len(fields))
	for i, f := range fields {
		headers[i] = string(f.Name)
	}

	var data [][]string
	for rows.Next() {
		if len(data) >= MaxPostgresRows {
			return nil, fmt.Errorf("postgres: 行数が上限 %d を超えました。クエリに LIMIT を追加してください", MaxPostgresRows)
		}
		vals, err := rows.Values()
		if err != nil {
			return nil, fmt.Errorf("read row: %w", err)
		}
		row := make([]string, len(vals))
		for i, v := range vals {
			row[i] = cellToString(v)
		}
		data = append(data, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate rows: %w", err)
	}

	return &Dataset{Headers: headers, Rows: data}, nil
}

// cellToString はセル値を CSV 用の文字列へ変換する。NULL は空文字。
func cellToString(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case []byte:
		return string(t)
	default:
		return fmt.Sprint(t)
	}
}
