package repository

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/RitikPandey1/Doorstep-Service-Booking/internal/domains"
)

type BookingRepo struct {
	db *pgxpool.Pool
}

func NewBookingRepo(db *pgxpool.Pool) *BookingRepo {
	return &BookingRepo{db: db}
}

// BookingFilter selects a customer's bookings. Nil fields are not filtered on.
type BookingFilter struct {
	Phone  string
	Status *string
	From   *time.Time // start_time >= From
	To     *time.Time // start_time < To
	Limit  int
	Offset int
}

// Insert stores b and returns it with generated and joined fields filled.
// Returns ErrOverlap or ErrDuplicateIdempotencyKey when a constraint rejects it.
func (r *BookingRepo) Insert(ctx context.Context, b domains.Booking) (domains.Booking, error) {
	row := r.db.QueryRow(ctx, QueryInsertBooking,
		b.CustomerPhone, b.ServicePK, b.Pincode, b.TechnicianPK,
		b.StartTime, b.EndTime, b.BlockedUntil, b.Status, b.IdempotencyKey, b.RequestHash)
	out, err := scanBooking(row)
	return out, mapError(err)
}

func (r *BookingRepo) GetByBookingID(ctx context.Context, id uuid.UUID) (domains.Booking, error) {
	b, err := scanBooking(r.db.QueryRow(ctx, QueryGetBookingByID, id))
	return b, mapError(err)
}

func (r *BookingRepo) GetByIdempotencyKey(ctx context.Context, key string) (domains.Booking, error) {
	b, err := scanBooking(r.db.QueryRow(ctx, QueryGetBookingByIdempotencyKey, key))
	return b, mapError(err)
}

// ListLiveForTechnicians returns non-cancelled bookings of the technicians starting in [from, to), by start time.
func (r *BookingRepo) ListLiveForTechnicians(ctx context.Context, technicianPKs []int64, from, to time.Time) ([]domains.Booking, error) {
	return r.list(ctx, QueryListLiveBookingsForTechnicians, technicianPKs, from, to)
}

// TransitionStatus moves the booking from status `from` to `to`.
// Returns ErrNotFound when the booking does not exist or is not in `from`.
func (r *BookingRepo) TransitionStatus(ctx context.Context, bookingPK int64, from, to string) (domains.Booking, error) {
	b, err := scanBooking(r.db.QueryRow(ctx, QueryTransitionBookingStatus, bookingPK, from, to))
	return b, mapError(err)
}

// ListByPhone returns one page of a customer's bookings (newest first) and the total match count.
func (r *BookingRepo) ListByPhone(ctx context.Context, f BookingFilter) ([]domains.Booking, int, error) {
	var total int
	if err := r.db.QueryRow(ctx, QueryCountBookingsByPhone, f.Phone, f.Status, f.From, f.To).Scan(&total); err != nil {
		return nil, 0, mapError(err)
	}
	items, err := r.list(ctx, QueryListBookingsByPhone, f.Phone, f.Status, f.From, f.To, f.Limit, f.Offset)
	return items, total, err
}

func (r *BookingRepo) list(ctx context.Context, query string, args ...any) ([]domains.Booking, error) {
	rows, err := r.db.Query(ctx, query, args...)
	if err != nil {
		return nil, mapError(err)
	}
	items, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (domains.Booking, error) {
		return scanBooking(row)
	})
	return items, mapError(err)
}

func scanBooking(row pgx.Row) (domains.Booking, error) {
	var b domains.Booking
	err := row.Scan(
		&b.PK, &b.BookingID, &b.CustomerPhone, &b.ServicePK, &b.Pincode, &b.TechnicianPK,
		&b.StartTime, &b.EndTime, &b.BlockedUntil, &b.Status, &b.IdempotencyKey, &b.RequestHash,
		&b.CreatedAt, &b.UpdatedAt, &b.ServiceID, &b.ServiceName, &b.TechnicianID, &b.TechnicianName,
	)
	return b, err
}
