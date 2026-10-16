package repository

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/RitikPandey1/Doorstep-Service-Booking/internal/domains"
)

type ServiceRepo struct {
	db *pgxpool.Pool
}

func NewServiceRepo(db *pgxpool.Pool) *ServiceRepo {
	return &ServiceRepo{db: db}
}

func (r *ServiceRepo) GetByServiceID(ctx context.Context, id uuid.UUID) (domains.Service, error) {
	var s domains.Service
	err := r.db.QueryRow(ctx, QueryGetServiceByID, id).
		Scan(&s.PK, &s.ServiceID, &s.Name, &s.DurationMin, &s.RequiredSkill)
	return s, mapError(err)
}
