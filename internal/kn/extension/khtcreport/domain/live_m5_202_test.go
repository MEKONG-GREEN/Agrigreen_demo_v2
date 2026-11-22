package domain_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	form "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcform/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcreport/domain"
	"github.com/google/uuid"
)

// Fixed identities/time make this a byte-level regression for existing frozen
// report captures, independent of the new no-period dashboard path.
func TestLive202ExistingComposeBytes(t *testing.T) {
	p, plan, sheets, _ := dataFixture()
	p.ID = uuid.MustParse("20200000-0000-4000-8000-000000000001")
	plan.VersionID = uuid.MustParse("20200000-0000-4000-8000-000000000002")
	refs := map[uuid.UUID]uuid.UUID{}
	for i := range plan.Entries {
		old := plan.Entries[i].Ref
		refs[old] = uuid.MustParse([]string{"20200000-0000-4000-8000-000000000003", "20200000-0000-4000-8000-000000000004"}[i])
	}
	master := uuid.MustParse("20200000-0000-4000-8000-000000000005")
	for i := range plan.Entries {
		e := &plan.Entries[i]
		e.Ref = refs[e.Ref]
		e.ProgramRef = &master
		if e.ParentRef != nil {
			x := refs[*e.ParentRef]
			e.ParentRef = &x
		}
	}
	for i := range sheets {
		s := &sheets[i]
		// The two attempts of the same weekly sheet retain the same period.
		s.PeriodID = uuid.MustParse("20200000-0000-4000-8000-0000000000" + []string{"10", "11", "11", "13", "14"}[i])
		if i == 4 {
			s.PeriodID = p.ID
		}
		s.SubmissionID = uuid.MustParse("20200000-0000-4000-8000-0000000000" + []string{"20", "21", "22", "23", "24"}[i])
		s.SheetID = uuid.MustParse("20200000-0000-4000-8000-0000000000" + []string{"30", "31", "32", "33", "34"}[i])
		cells := map[string]form.CellInput{}
		for ref, cell := range s.Sheet.Cells {
			cells[refs[uuid.MustParse(ref)].String()] = cell
		}
		s.Sheet.Cells = cells
		for j := range s.Sheet.Adjustments {
			a := &s.Sheet.Adjustments[j]
			a.EntryRef = refs[uuid.MustParse(a.EntryRef)].String()
		}
	}
	snapshot := &domain.SourceRef{ID: uuid.MustParse("20200000-0000-4000-8000-000000000040"), PlanVersionID: &plan.VersionID, Closed: true}
	d, err := domain.Compose(p, plan, sheets, snapshot, nil, day("2026-10-28"))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	h := sha256.Sum256(raw)
	const want = "6da7fca16ade4480a92a3138d6a4dae8c34328fffa619340db45a46474c55e12"
	if got := hex.EncodeToString(h[:]); got != want {
		t.Fatalf("existing Compose bytes %s input_hash %s", got, d.InputHash)
	}
}

func TestLive202WindowUsesActualVietnamCutoff(t *testing.T) {
	now := time.Date(2026, 10, 13, 17, 0, 0, 0, time.UTC) // 14/10 00:00 VN
	for _, tc := range []struct {
		name, raw, want, quantity, actual string
		year, quarter                     int
		empty, invalid                    bool
	}{
		{"default current", "", "2026-10-14", "2026-10-13", "2026-10-13T17:00:00Z", 2026, 4, false, false},
		{"past day inclusive", "2026-10-06", "2026-10-06", "2026-10-06", "2026-10-06T16:59:59.999999999Z", 2026, 4, false, false},
		{"future requested actual capped", "2026-12-31", "2026-12-31", "2026-10-13", "2026-10-13T17:00:00Z", 2026, 4, false, false},
		{"future default remains available endpoint", "", "", "", "", 2027, 1, true, false},
		{"future explicit has no reversed window", "2027-02-01", "", "", "", 2027, 1, true, false},
		{"out of selected quarter", "2026-09-30", "", "", "", 2026, 4, false, true},
		{"invalid calendar", "2026-11-31", "", "", "", 2026, 4, false, true},
		{"noncanonical", "2026-1-01", "", "", "", 2026, 4, false, true},
		{"hostile exponent", "1e100000", "", "", "", 2026, 4, false, true},
		{"hostile tiny exponent", "1e-100000", "", "", "", 2026, 4, false, true},
		{"hostile long", strings.Repeat("9", 100000), "", "", "", 2026, 4, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w, err := domain.NewLiveM5Window(tc.year, tc.quarter, tc.raw, now)
			if tc.invalid {
				if err == nil {
					t.Fatal("invalid through date admitted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if tc.empty {
				if w != nil {
					t.Fatalf("future query invented window %+v", w)
				}
				return
			}
			if w == nil || w.ThroughDate != tc.want || w.QuantityTo != tc.quantity || w.EffectiveCutoff != tc.actual || w.FromDate != "2025-12-31" || w.CalendarVersion != 2 || w.From.After(w.To) {
				t.Fatalf("actual window %+v", w)
			}
		})
	}
}

func TestLive202ArithmeticSharesM5WithoutCurrentPeriod(t *testing.T) {
	p, plan, sheets, _ := dataFixture()
	w, err := domain.NewLiveM5Window(2026, 4, "2026-10-13", day("2026-10-14"))
	if err != nil || w == nil {
		t.Fatal(err)
	}
	p.Type, p.From, p.To, p.Cutoff = "program", w.From, w.To, w.Cutoff
	sheets[len(sheets)-1].Type = "program"
	old, err := domain.Compose(p, plan, sheets, nil, nil, day("2026-10-14"))
	if err != nil {
		t.Fatal(err)
	}
	live, err := domain.ComposeLiveM5(*w, plan, sheets, nil, day("2026-10-14"))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(old.Rows, live.Rows) {
		t.Fatalf("live arithmetic diverges from actual M5 rows old%+v live%+v", old.Rows, live.Rows)
	}
	if live.Period.ID != uuid.Nil || live.Period.Name != "" || live.Period.Type != "" || live.Sources != nil {
		t.Fatal("live calculation invented lifecycle identity")
	}
	for i := range sheets {
		if sheets[i].To.Equal(day("2026-10-13")) {
			sheets[i].Decision = "returned"
		}
	}
	returned, err := domain.ComposeLiveM5(*w, plan, sheets, nil, day("2026-10-14"))
	if err != nil {
		t.Fatal(err)
	}
	if returned.Rows[1].YearToDate != "45" {
		t.Fatalf("returned input counted %+v", returned.Rows[1])
	}
	for _, raw := range []string{"1e100000", "1e-100000", strings.Repeat("9", 100000)} {
		sheets[0].Sheet.Cells[plan.Entries[1].Ref.String()] = form.CellInput{Done: raw}
		if _, err := domain.ComposeLiveM5(*w, plan, sheets, nil, day("2026-10-14")); err == nil {
			t.Fatal("hostile source sheet number accepted")
		}
	}
}
