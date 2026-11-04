package domain_test

import (
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtc/domain"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"strings"
	"testing"
)

func TestDraft225BoundsBeforeHash(t *testing.T) {
	for _, test := range []struct {
		name   string
		value  decimal.Decimal
		budget bool
	}{{"large exponent", decimal.New(1, 100000), false}, {"small exponent", decimal.New(1, -100000), false}, {"huge ordinary", decimal.New(1, 40), false}, {"negative", decimal.New(-1, 0), false}, {"quantity precision", decimal.New(100001, -5), false}, {"budget precision", decimal.New(1001, -3), true}} {
		t.Run(test.name, func(t *testing.T) {
			e := domain.Entry{}
			if test.budget {
				e.Budget = &test.value
			} else {
				e.PlannedQty = &test.value
			}
			if err := domain.CheckDraftResource225([]domain.Entry{e}); err == nil {
				t.Fatal("typed unsafe decimal accepted")
			}
			if hash, err := domain.DraftRequestHash225(2030, uuid.Nil, 0, "create", "", []domain.Entry{e}); err == nil || hash != nil {
				t.Fatal("unsafe decimal reached request hash")
			}
		})
	}
	qty := decimal.RequireFromString("9999999999999999.9999")
	budget := decimal.RequireFromString("999999999999999999.99")
	if err := domain.CheckDraftResource225([]domain.Entry{{PlannedQty: &qty, Budget: &budget}}); err != nil {
		t.Fatal("bounded business maxima rejected")
	}
	for name, es := range map[string][]domain.Entry{"too many rows": make([]domain.Entry, 5001), "long name": {{Name: strings.Repeat("9", 100000)}}, "too many communes": {{Communes: make([]string, 501)}}} {
		t.Run(name, func(t *testing.T) {
			if err := domain.CheckDraftResource225(es); err == nil {
				t.Fatal("resource bound not applied before hash")
			}
		})
	}
}
