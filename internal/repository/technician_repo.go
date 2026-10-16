package repository

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/RitikPandey1/Doorstep-Service-Booking/internal/domains"
)

type TechnicianRepo struct {
	db *pgxpool.Pool
}

func NewTechnicianRepo(db *pgxpool.Pool) *TechnicianRepo {
	return &TechnicianRepo{db: db}
}

func (r *TechnicianRepo) GetByTechnicianID(ctx context.Context, id uuid.UUID) (domains.Technician, error) {
	t, err := scanTechnician(r.db.QueryRow(ctx, QueryGetTechnicianByID, id))
	return t, mapError(err)
}

// ListByZoneAndSkill returns the technicians of a zone having the skill, ordered by internal id.
func (r *TechnicianRepo) ListByZoneAndSkill(ctx context.Context, zonePK int64, skill string) ([]domains.Technician, error) {
	rows, err := r.db.Query(ctx, QueryListTechniciansByZoneAndSkill, zonePK, skill)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (domains.Technician, error) {
		return scanTechnician(row)
	})
}

func scanTechnician(row pgx.Row) (domains.Technician, error) {
	var t domains.Technician
	var start, end pgtype.Time
	if err := row.Scan(&t.PK, &t.TechnicianID, &t.Name, &t.ZonePK, &t.Skills, &start, &end); err != nil {
		return t, err
	}
	t.ShiftStart = time.Duration(start.Microseconds) * time.Microsecond
	t.ShiftEnd = time.Duration(end.Microseconds) * time.Microsecond
	return t, nil
}
