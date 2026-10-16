package repository

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/RitikPandey1/Doorstep-Service-Booking/internal/domains"
)

type ZoneRepo struct {
	db *pgxpool.Pool
}

func NewZoneRepo(db *pgxpool.Pool) *ZoneRepo {
	return &ZoneRepo{db: db}
}

func (r *ZoneRepo) GetByPincode(ctx context.Context, pincode int) (domains.Zone, error) {
	var z domains.Zone
	err := r.db.QueryRow(ctx, QueryGetZoneByPincode, pincode).Scan(&z.PK, &z.ZoneID, &z.Name)
	return z, mapError(err)
}
