package main

import (
	"slices"
	"testing"
)

func TestPincodeBelongsToOneZone(t *testing.T) {
	seen := map[int]string{}
	for _, z := range zones {
		for _, p := range z.Pincodes {
			if other, ok := seen[p]; ok {
				t.Errorf("pincode %d is in zones %s and %s", p, other, z.Name)
			}
			seen[p] = z.Name
		}
	}
}

func TestEveryServiceHasQualifiedTechnicianInEveryZone(t *testing.T) {
	for _, z := range zones {
		for _, s := range services {
			found := slices.ContainsFunc(technicians, func(tech seedTechnician) bool {
				return tech.ZoneID == z.ID && slices.Contains(tech.Skills, s.Skill)
			})
			if !found {
				t.Errorf("zone %s has no technician for %s", z.Name, s.Name)
			}
		}
	}
}
