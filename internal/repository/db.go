package repository

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrNotFound = errors.New("not found")
	// ErrOverlap means the bookings_no_overlap exclusion constraint rejected the insert.
	ErrOverlap = errors.New("technician already booked in that interval")
	// ErrDuplicateIdempotencyKey means another booking already holds this Idempotency-Key.
	ErrDuplicateIdempotencyKey = errors.New("idempotency key already used")
)

// NewPool connects to Postgres and verifies the connection.
func NewPool(ctx context.Context, url string) (*pgxpool.Pool, error) {
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		return nil, err
	}
	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, err
	}
	return pool, nil
}

// mapError turns driver errors into repository sentinels.
func mapError(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch {
		case pgErr.Code == "23P01" && pgErr.ConstraintName == "bookings_no_overlap":
			return ErrOverlap
		case pgErr.Code == "23505" && pgErr.ConstraintName == "bookings_idempotency_key_key":
			return ErrDuplicateIdempotencyKey
		}
	}
	return err
}
