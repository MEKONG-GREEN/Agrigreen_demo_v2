package domain

import (
	"github.com/google/uuid"
	"testing"
)

func TestPromotionV4RegularOnly(t *testing.T) {
	t.Run("ratings and discipline do not gate or postpone regular promotion", func(t *testing.T) {
		s := sub(5, "2023-11-01")
		s.Ratings = map[int]string{2024: "KHTNV"}
		to := d("2028-12-31")
		s.Disciplines = []Event{{Kind: EventDiscipline, ValidFrom: d("2025-01-01"), ValidTo: &to, Payload: map[string]string{"form": "canh_cao"}}}
		cs := Compute(s, testRules(), 2026)
		if len(cs) != 1 || cs[0].Cond != CondOK || !cs[0].Due.Equal(d("2026-11-01")) {
			t.Fatalf("FN09 BR01: want regular 2026-11-01 regardless KHTNV/discipline, got %+v", cs)
		}
	})
	t.Run("last step is excluded", func(t *testing.T) {
		if cs := Compute(sub(9, "2023-06-01"), testRules(), 2026); len(cs) != 0 {
			t.Fatalf("FN09 BR02/07: last step must not auto-create TNVK: %+v", cs)
		}
	})
	t.Run("awards never auto-create early promotion", func(t *testing.T) {
		s := sub(3, "2024-02-01")
		id := uuid.New()
		s.Awards = []PromoAward{{EventID: id, Year: 2024, Months: 12}}
		s.Proposals = []uuid.UUID{id}
		if cs := Compute(s, testRules(), 2026); len(cs) != 1 || cs[0].Kind != PromoNLTX {
			t.Fatalf("FN09 BR07: only regular: %+v", cs)
		}
	})
}
