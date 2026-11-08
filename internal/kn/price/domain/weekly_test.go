package domain

import (
	"encoding/json"
	"github.com/shopspring/decimal"
	"os"
	"slices"
	"strings"
	"testing"
)

func TestWeeklyArithmetic284(t *testing.T) {
	i := WeeklyItem{Plan: decimal.NewFromInt(12), OpeningProgress: decimal.NewFromInt(10), Progress: decimal.NewFromInt(2), OpeningMoney: decimal.NewFromInt(803458000), Money: decimal.NewFromInt(10000000)}
	p, m, r := i.Cumulative()
	if !p.Equal(decimal.NewFromInt(12)) || !m.Equal(decimal.NewFromInt(813458000)) || r == nil || !r.Equal(decimal.NewFromInt(100)) {
		t.Fatalf("AC01/03: %s %s %v", p, m, r)
	}
	i.Plan = decimal.NewFromInt(6552)
	adjust := decimal.NewFromInt(3822)
	i.AdjustedPlan = &adjust
	i.OpeningProgress = decimal.Zero
	i.Progress = decimal.NewFromInt(1934)
	_, _, r = i.Cumulative()
	if r == nil || r.StringFixed(2) != "50.60" {
		t.Fatalf("AC02 adjusted plan: %v", r)
	}
	i.AdjustedPlan = nil
	i.Plan = decimal.Zero
	_, _, r = i.Cumulative()
	if r != nil {
		t.Fatal("zero plan must have no percentage")
	}
	i.Group = true
	p, m, r = i.Cumulative()
	if !p.IsZero() || !m.IsZero() || r != nil {
		t.Fatal("group must never contribute amounts")
	}
}

func TestWeeklyDecimalGuard284(t *testing.T) {
	for _, value := range []string{"1e100000", "1e-100000", strings.Repeat("9", 100000), "NaN", "Infinity", "-1", "1,5", "1.00000", " 1", "1.", ".1", ""} {
		if _, err := ParseWeeklyNumber(value, false); err == nil {
			t.Fatalf("accepted quantity %q", value[:min(len(value), 40)])
		}
		if _, err := ParseWeeklyNumber(value, true); err == nil {
			t.Fatalf("accepted money %q", value[:min(len(value), 40)])
		}
	}
	if _, err := ParseWeeklyNumber("1.5", true); err == nil {
		t.Fatal("money must be integer đồng")
	}
	for _, value := range []string{"0", "12.25", "999999999999999.9999"} {
		if _, err := ParseWeeklyNumber(value, false); err != nil {
			t.Fatal(value, err)
		}
	}
	if _, err := ParseWeeklyNumber("999999999999999999", true); err != nil {
		t.Fatal(err)
	}
}

func TestWeeklyOfficialCatalog284(t *testing.T) {
	info, training := WeeklyCatalog("information"), WeeklyCatalog("training")
	if len(info) != 19 || len(training) != 6 || len(WeeklyCatalog("outside")) != 0 {
		t.Fatalf("official catalog counts %d/%d", len(info), len(training))
	}
	if info[10].Plan != "6552" || training[3].Plan != "12" {
		t.Fatal("official scale differs from decision source")
	}
	groups := 0
	for _, i := range info {
		if i.Plan == "" {
			groups++
		}
	}
	if groups != 2 {
		t.Fatal("official two groups")
	}
}

func TestWeeklyOfficialCatalogWebParity284(t *testing.T) {
	raw, err := os.ReadFile("../../../../apps/web-ops/src/api/mock/fixtures/price-weekly-catalog.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture map[string][]WeeklyCatalogItem
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	for _, program := range []string{"information", "training"} {
		if !slices.Equal(fixture[program], WeeklyCatalog(program)) {
			t.Fatal("official catalog drift in web mock", program)
		}
	}
}
