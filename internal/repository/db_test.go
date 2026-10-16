package repository

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestMapError(t *testing.T) {
	tests := []struct {
		name string
		in   error
		want error // nil means "returned unchanged"
	}{
		{"no rows", pgx.ErrNoRows, ErrNotFound},
		{"overlap constraint", &pgconn.PgError{Code: "23P01", ConstraintName: "bookings_no_overlap"}, ErrOverlap},
		{"idempotency key constraint", &pgconn.PgError{Code: "23505", ConstraintName: "bookings_idempotency_key_key"}, ErrDuplicateIdempotencyKey},
		{"server shutting down", &pgconn.PgError{Code: "57P01"}, ErrUnavailable},
		{"connection exception", &pgconn.PgError{Code: "08006"}, ErrUnavailable},
		{"network error", &net.OpError{Op: "dial", Err: errors.New("refused")}, ErrUnavailable},
		{"dropped connection", fmt.Errorf("read: %w", io.ErrUnexpectedEOF), ErrUnavailable},
		{"timeout", context.DeadlineExceeded, ErrUnavailable},
		{"other unique violation", &pgconn.PgError{Code: "23505", ConstraintName: "zones_name_key"}, nil},
		{"syntax error", &pgconn.PgError{Code: "42601"}, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := mapError(tc.in)
			if tc.want == nil {
				if got != tc.in {
					t.Fatalf("got %v, want the error unchanged", got)
				}
				return
			}
			if !errors.Is(got, tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}
