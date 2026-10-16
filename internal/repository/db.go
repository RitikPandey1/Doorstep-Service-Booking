package repository

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
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
	// ErrUnavailable means Postgres could not be reached or did not answer in time.
	ErrUnavailable = errors.New("database unavailable")
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
	if isUnavailable(err) {
		return fmt.Errorf("%w: %v", ErrUnavailable, err)
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

// isUnavailable reports errors meaning "Postgres is down or unreachable", not "the query is wrong":
// connect failures, timeouts, dropped connections, and SQLSTATE classes 08 (connection exception)
// and 57P (server shutting down / not accepting connections).
func isUnavailable(err error) bool {
	var connErr *pgconn.ConnectError
	var netErr net.Error
	var pgErr *pgconn.PgError
	switch {
	case errors.As(err, &connErr), errors.As(err, &netErr), pgconn.Timeout(err), errors.Is(err, io.ErrUnexpectedEOF):
		return true
	case errors.As(err, &pgErr):
		return strings.HasPrefix(pgErr.Code, "08") || strings.HasPrefix(pgErr.Code, "57P")
	}
	return false
}
