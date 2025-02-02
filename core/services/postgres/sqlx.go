package postgres

import (
	"context"
	"database/sql"

	"github.com/pkg/errors"
	mapper "github.com/scylladb/go-reflectx"

	"github.com/smartcontractkit/chainlink/core/logger"
	"github.com/smartcontractkit/sqlx"
)

// AllowUnknownQueryerTypeInTransaction can be set by tests to allow a mock to be passed as a Queryer
var AllowUnknownQueryerTypeInTransaction bool

//go:generate mockery --name Queryer --output ./mocks/ --case=underscore
type Queryer interface {
	sqlx.Ext
	sqlx.ExtContext
	sqlx.Preparer
	sqlx.PreparerContext
	sqlx.Queryer
	Select(dest interface{}, query string, args ...interface{}) error
	SelectContext(ctx context.Context, dest interface{}, query string, args ...interface{}) error
	PrepareNamed(query string) (*sqlx.NamedStmt, error)
	QueryRow(query string, args ...interface{}) *sql.Row
	QueryRowContext(ctx context.Context, query string, args ...interface{}) *sql.Row
	Get(dest interface{}, query string, args ...interface{}) error
	GetContext(ctx context.Context, dest interface{}, query string, args ...interface{}) error
	NamedExec(query string, arg interface{}) (sql.Result, error)
	NamedQuery(query string, arg interface{}) (*sqlx.Rows, error)
}

func WrapDbWithSqlx(rdb *sql.DB) *sqlx.DB {
	db := sqlx.NewDb(rdb, "postgres")
	db.MapperFunc(mapper.CamelToSnakeASCII)
	return db
}

func SqlxTransactionWithDefaultCtx(q Queryer, lggr logger.Logger, fc func(q Queryer) error, txOpts ...TxOptions) (err error) {
	ctx, cancel := DefaultQueryCtx()
	defer cancel()
	return SqlxTransaction(ctx, q, lggr, fc, txOpts...)
}

func SqlxTransaction(ctx context.Context, q Queryer, lggr logger.Logger, fc func(q Queryer) error, txOpts ...TxOptions) (err error) {
	switch db := q.(type) {
	case *sqlx.Tx:
		// nested transaction: just use the outer transaction
		err = fc(db)
	case *sqlx.DB:
		err = sqlxTransactionQ(ctx, db, lggr, fc, txOpts...)
	default:
		if AllowUnknownQueryerTypeInTransaction {
			err = fc(q)
		} else {
			err = errors.Errorf("invalid db type")
		}
	}

	return
}
