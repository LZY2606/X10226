package store

import (
	"context"
	"database/sql"
	"time"
)

const timeFmt = "2006-01-02T15:04:05Z07:00"

func parseTime(v string) time.Time {
	t, err := time.Parse(timeFmt, v)
	if err != nil {
		return time.Time{}
	}
	return t
}

type queryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}
