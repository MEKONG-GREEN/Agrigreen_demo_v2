package app

import (
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/hrm/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/hrm/store"
	"github.com/google/uuid"
	"testing"
	"time"
)

func TestPromotionFutureIndependentDecisionV4(t *testing.T) {
	staff, id := uuid.New(), uuid.New()
	due := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	c := domain.Candidate{StaffID: staff, Kind: domain.PromoNLTX, ToStep: 6, Due: due}
	c.Key = domain.CaseKey(staff, c.Kind, c.ToStep, due, uuid.Nil)
	for _, before := range []domain.PromoStatus{domain.PromoPending, domain.PromoExported} {
		w := &promoWorld{cases: map[string]*caseState{c.Key: {Status: before}}, decOrder: []uuid.UUID{id}, decisions: map[uuid.UUID]*decisionState{id: {Issue: store.DecisionRow{Kind: "nltx", People: []store.DecisionPerson{{StaffID: staff, ToStep: 6, EffectiveOn: "2026-11-01"}}}}}}
		if status, _ := w.status(c); status != domain.PromoDecided {
			t.Fatalf("FN09 AC05: future independent regular decision must override %s, got %s", before, status)
		}
		for _, other := range []domain.Candidate{{StaffID: staff, Kind: domain.PromoNLTX, ToStep: 6, Due: domain.AddMonths(due, 1)}, {StaffID: staff, Kind: domain.PromoNLTX, ToStep: 7, Due: due}} {
			other.Key = domain.CaseKey(staff, other.Kind, other.ToStep, other.Due, uuid.Nil)
			if status, _ := w.status(other); status != domain.PromoPending {
				t.Fatalf("decision applied to another cycle: %s", status)
			}
		}
		w.decisions[id].Cancel = &store.DecisionRow{}
		if status, _ := w.status(c); status != before {
			t.Fatalf("cancelled independent decision changed previous state: %s", status)
		}
	}
}
