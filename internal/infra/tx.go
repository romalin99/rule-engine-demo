package infra

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/jmoiron/sqlx"

	"tcg-rulex-engine/pkg/logs"
)

// WithTx runs fn inside a single Oracle transaction and returns its result
// (generic). Ported from tcg-rulex-engine to keep the DB layer identical.
//
// Contract:
//   - fn returns error  → auto Rollback (no commit)
//   - fn panics         → Rollback then re-panic
//   - txTimeout > 0      → derive a context.WithTimeout for the whole transaction
//   - txTimeout <= 0     → transaction lifetime governed by the caller ctx
//
// Inside fn, every repo call MUST use the ctx and tx fn receives — not the outer
// ctx or a bare *sqlx.DB — otherwise a statement escapes the transaction. Do not
// perform HTTP/Redis IO inside fn; keep external IO before/after the transaction.
func WithTx[T any](
	ctx context.Context,
	db *sqlx.DB,
	txTimeout time.Duration,
	opts *sql.TxOptions,
	fn func(ctx context.Context, tx *sqlx.Tx) (T, error),
) (result T, err error) {
	if db == nil {
		return result, fmt.Errorf("WithTx: nil *sqlx.DB")
	}
	if fn == nil {
		return result, fmt.Errorf("WithTx: nil fn")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	start := time.Now()

	if txTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, txTimeout)
		defer cancel()
	}

	tx, beginErr := db.BeginTxx(ctx, opts)
	if beginErr != nil {
		logs.Err(ctx, "WithTx begin failed: txTimeout=%v err=%v", txTimeout, beginErr)
		return result, fmt.Errorf("begin tx: %w", beginErr)
	}

	logs.Debug(ctx, "WithTx begin: txTimeout=%v", txTimeout)

	defer func() {
		if p := recover(); p != nil {
			logs.Err(ctx, "WithTx panic, rolling back: panic=%v elapsed=%v", p, time.Since(start))
			_ = rollbackQuietly(tx)
			panic(p)
		}
		if err != nil {
			var zero T
			result = zero
			logs.Warn(ctx, "WithTx rollback: err=%v elapsed=%v", err, time.Since(start))
			if rbErr := rollbackQuietly(tx); rbErr != nil {
				logs.Err(ctx, "WithTx rollback failed: err=%v rollbackErr=%v", err, rbErr)
				err = fmt.Errorf("%w; rollback failed: %v", err, rbErr)
			}
			return
		}
		if cErr := tx.Commit(); cErr != nil {
			var zero T
			result = zero
			err = fmt.Errorf("commit tx: %w", cErr)
			logs.Err(ctx, "WithTx commit failed: err=%v elapsed=%v", cErr, time.Since(start))
			return
		}
		logs.Info(ctx, "WithTx commit success: elapsed=%v", time.Since(start))
	}()

	return fn(ctx, tx)
}

// rollbackQuietly treats sql.ErrTxDone as success (already committed/rolled back).
func rollbackQuietly(tx *sqlx.Tx) error {
	if rbErr := tx.Rollback(); rbErr != nil && !errors.Is(rbErr, sql.ErrTxDone) {
		return rbErr
	}
	return nil
}

// Tx is the no-result convenience form of WithTx for pure-write transactions.
func Tx(
	ctx context.Context,
	db *sqlx.DB,
	txTimeout time.Duration,
	opts *sql.TxOptions,
	fn func(ctx context.Context, tx *sqlx.Tx) error,
) error {
	_, err := WithTx(ctx, db, txTimeout, opts, func(ctx context.Context, tx *sqlx.Tx) (struct{}, error) {
		return struct{}{}, fn(ctx, tx)
	})
	return err
}
