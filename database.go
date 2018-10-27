package cypress

import (
	"context"
	"database/sql"
	"time"

	"go.uber.org/zap"
)

// Queryable a queryable object that could be a Connection, DB or Tx
type Queryable interface {
	QueryContext(ctx context.Context, query string, args ...interface{}) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...interface{}) *sql.Row
}

// DataRow data row, which can be used to scan values or get column information
type DataRow interface {
	ColumnTypes() ([]*sql.ColumnType, error)
	Columns() ([]string, error)
	Scan(dest ...interface{}) error
}

// RowMapper maps a row to an object
type RowMapper interface {
	Map(row DataRow) (interface{}, error)
}

// RowMapperFunc a function that implements RowMapper
type RowMapperFunc func(row DataRow) (interface{}, error)

// Map implements the RowMapper interface
func (mapper RowMapperFunc) Map(row DataRow) (interface{}, error) {
	return mapper(row)
}

// LogExec log the sql Exec call result
func LogExec(activityID string, start time.Time, err error) {
	latency := time.Since(start)
	zap.L().Info("execSql", zap.Int("latency", int(latency.Seconds()*1000)), zap.Bool("success", err == nil), zap.String("activityId", activityID))
}

// QueryOne query one object
func QueryOne(ctx context.Context, queryable Queryable, mapper RowMapper, query string, args ...interface{}) (interface{}, error) {
	var err error
	start := time.Now()
	defer func(e error) {
		latency := time.Since(start)
		zap.L().Info("queryOne", zap.Int("latency", int(latency.Seconds()*1000)), zap.Bool("success", e == sql.ErrNoRows || e == nil), zap.String("activityId", GetTraceID(ctx)))
	}(err)
	rows, err := queryable.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}

	defer rows.Close()
	if !rows.Next() {
		return nil, nil
	}

	obj, err := mapper.Map(rows)
	if err != nil {
		return nil, err
	}

	return obj, nil
}

// QueryAll query all rows and map them to objects
func QueryAll(ctx context.Context, queryable Queryable, mapper RowMapper, query string, args ...interface{}) ([]interface{}, error) {
	var err error
	start := time.Now()
	defer func(e error) {
		latency := time.Since(start)
		zap.L().Info("queryAll", zap.Int("latency", int(latency.Seconds()*1000)), zap.Bool("success", e == sql.ErrNoRows || e == nil), zap.String("activityId", GetTraceID(ctx)))
	}(err)

	rows, err := queryable.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}

	defer rows.Close()
	results := make([]interface{}, 0, 10)
	for rows.Next() {
		obj, err := mapper.Map(rows)
		if err != nil {
			return nil, err
		}

		results = append(results, obj)
	}

	return results, nil
}
