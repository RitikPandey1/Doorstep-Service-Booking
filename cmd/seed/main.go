// Command seed loads the reference data (services, zones, pincodes, technicians).
// UUIDs are fixed so README examples and tests can rely on them; re-running is a no-op.
package main

import (
	"context"
	"log/slog"
	"os"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/RitikPandey1/Doorstep-Service-Booking/internal/utils"
)

type seedService struct {
	ID, Name, Skill string
	DurationMin     int
}

type seedZone struct {
	ID, Name string
	Pincodes []int
}

type seedTechnician struct {
	ID, Name, ZoneID string
	Skills           []string
}

const (
	shiftStart = "08:00"
	shiftEnd   = "20:00"

	zoneNorth = "20000000-0000-0000-0000-000000000001"
	zoneSouth = "20000000-0000-0000-0000-000000000002"
)

var services = []seedService{
	{ID: "5e000000-0000-0000-0000-000000000001", Name: "Battery replacement", DurationMin: 60, Skill: "BATTERY_FIT"},
	{ID: "5e000000-0000-0000-0000-000000000002", Name: "Jump-start", DurationMin: 30, Skill: "JUMPSTART"},
	{ID: "5e000000-0000-0000-0000-000000000003", Name: "Battery health check", DurationMin: 30, Skill: "DIAGNOSTICS"},
}

var zones = []seedZone{
	{ID: zoneNorth, Name: "North", Pincodes: []int{110001, 110002, 110003}},
	{ID: zoneSouth, Name: "South", Pincodes: []int{560001, 560002, 560003}},
}

// All on 08:00–20:00. Per zone: one all-rounder, one BATTERY_FIT only, one JUMPSTART + DIAGNOSTICS.
var technicians = []seedTechnician{
	{ID: "7e000000-0000-0000-0000-000000000001", Name: "Ravi Kumar", ZoneID: zoneNorth, Skills: []string{"BATTERY_FIT", "JUMPSTART", "DIAGNOSTICS"}},
	{ID: "7e000000-0000-0000-0000-000000000002", Name: "Amit Sharma", ZoneID: zoneNorth, Skills: []string{"BATTERY_FIT"}},
	{ID: "7e000000-0000-0000-0000-000000000003", Name: "Suresh Yadav", ZoneID: zoneNorth, Skills: []string{"JUMPSTART", "DIAGNOSTICS"}},
	{ID: "7e000000-0000-0000-0000-000000000004", Name: "Karthik Reddy", ZoneID: zoneSouth, Skills: []string{"BATTERY_FIT", "JUMPSTART", "DIAGNOSTICS"}},
	{ID: "7e000000-0000-0000-0000-000000000005", Name: "Manoj Nair", ZoneID: zoneSouth, Skills: []string{"BATTERY_FIT"}},
	{ID: "7e000000-0000-0000-0000-000000000006", Name: "Arjun Rao", ZoneID: zoneSouth, Skills: []string{"JUMPSTART", "DIAGNOSTICS"}},
}

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	conn, err := pgx.Connect(ctx, utils.GetEnv("DATABASE_URL", "postgres://booking:booking@localhost:5432/booking?sslmode=disable"))
	if err != nil {
		slog.Error("connect failed", "error", err)
		os.Exit(1)
	}
	defer conn.Close(ctx)

	if err := pgx.BeginFunc(ctx, conn, seed); err != nil {
		slog.Error("seed failed", "error", err)
		os.Exit(1)
	}
	slog.Info("seed complete", "services", len(services), "zones", len(zones), "technicians", len(technicians))
}

func seed(tx pgx.Tx) error {
	ctx := context.Background()
	for _, s := range services {
		if _, err := tx.Exec(ctx, `
			INSERT INTO services (service_id, name, duration_min, required_skill)
			VALUES ($1, $2, $3, $4) ON CONFLICT (service_id) DO NOTHING`,
			s.ID, s.Name, s.DurationMin, s.Skill); err != nil {
			return err
		}
	}
	for _, z := range zones {
		if _, err := tx.Exec(ctx, `
			INSERT INTO zones (zone_id, name) VALUES ($1, $2) ON CONFLICT (zone_id) DO NOTHING`,
			z.ID, z.Name); err != nil {
			return err
		}
		for _, p := range z.Pincodes {
			if _, err := tx.Exec(ctx, `
				INSERT INTO zone_pincodes (pincode, zone_pk)
				SELECT $1, id FROM zones WHERE zone_id = $2
				ON CONFLICT (pincode) DO NOTHING`,
				p, z.ID); err != nil {
				return err
			}
		}
	}
	for _, t := range technicians {
		if _, err := tx.Exec(ctx, `
			INSERT INTO technicians (technician_id, name, zone_pk, skills, shift_start, shift_end)
			SELECT $1, $2, id, $4, $5, $6 FROM zones WHERE zone_id = $3
			ON CONFLICT (technician_id) DO NOTHING`,
			t.ID, t.Name, t.ZoneID, t.Skills, shiftStart, shiftEnd); err != nil {
			return err
		}
	}
	return nil
}
