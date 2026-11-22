package domain_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	form "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcform/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcreport/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	"github.com/google/uuid"
)

func TestW1168RangeAndLockedSelection(t *testing.T) {
	day := func(m time.Month, d int) time.Time { return time.Date(2026, m, d, 0, 0, 0, 0, time.UTC) }
	item := uuid.New()
	p := domain.Period{ID: uuid.New(), Type: "adhoc", Year: 2026, From: day(10, 1), To: day(10, 31), Cutoff: day(10, 31), Departments: []string{"trong_trot"}}
	plan := port.KHTCPlan{VersionID: uuid.New(), Entries: []port.KHTCPlanEntry{{Ref: item, Kind: "item", Name: "Diện tích mô hình", Department: "trong_trot"}}}
	sheets := []domain.Submitted{}
	for _, v := range []struct {
		month time.Month
		date  int
		done  string
	}{{9, 29, "9"}, {10, 6, "3"}, {11, 3, "100"}} {
		sheets = append(sheets, domain.Submitted{PeriodID: uuid.New(), SubmissionID: uuid.New(), Attempt: 1, Type: "week", Year: 2026, Department: "trong_trot", To: day(v.month, v.date), Sheet: form.Sheet{Cells: map[string]form.CellInput{item.String(): {Done: v.done}}}})
	}
	data, err := domain.Compose(p, plan, sheets, &domain.SourceRef{Closed: true}, nil, day(11, 1))
	if err != nil {
		t.Fatal(err)
	}
	if data.Template != "adhoc" || len(data.Rows) != 1 || data.Rows[0].InPeriod != "3" || data.Rows[0].YearToDate != "12" || data.Rows[0].Missing {
		t.Fatalf("range/source: %+v", data.Rows)
	}
	selected := &domain.AdhocSelection{Rows: []string{item.String()}, Metrics: []string{}}
	edited, err := domain.Edit(domain.Refresh(domain.Draft{}, data, nil), domain.Edits{Adhoc: selected, Texts: map[string]string{"noi_dung": "Số liệu từ phiếu đã gửi."}}, uuid.New(), day(11, 1))
	if err != nil {
		t.Fatal(err)
	}
	selected.Rows[0] = uuid.NewString()
	if edited.Adhoc.Rows[0] != item.String() || len(domain.AdhocData(edited).Rows) != 1 {
		t.Fatal("caller mutated frozen selection")
	}
}
func TestW1168HostileRawNumbersBeforeDecimal(t *testing.T) {
	item := uuid.New()
	now := time.Date(2026, 10, 6, 0, 0, 0, 0, time.UTC)
	p := domain.Period{ID: uuid.New(), Type: "adhoc", Year: 2026, From: now, To: now, Cutoff: now}
	plan := port.KHTCPlan{VersionID: uuid.New(), Entries: []port.KHTCPlanEntry{{Ref: item, Kind: "item", Department: "trong_trot"}}}
	for _, raw := range []string{"1e100000", "1e-100000", strings.Repeat("9", 100000)} {
		sub := domain.Submitted{PeriodID: uuid.New(), SubmissionID: uuid.New(), Attempt: 1, Type: "week", Year: 2026, Department: "trong_trot", To: now, Sheet: form.Sheet{Cells: map[string]form.CellInput{item.String(): {Done: raw}}}}
		if _, err := domain.Compose(p, plan, []domain.Submitted{sub}, nil, nil, now); !errors.Is(err, domain.ErrInput) {
			t.Fatalf("hostile len%d: %v", len(raw), err)
		}
	}
}
func TestW1168SelectionRejectsUnknownAndOversized(t *testing.T) {
	id := uuid.NewString()
	d := domain.Data{Template: "adhoc", Rows: []domain.ProgramRow{{Ref: uuid.MustParse(id), Kind: "item"}}}
	for _, s := range []domain.AdhocSelection{{}, {Rows: []string{id, id}}, {Rows: []string{uuid.NewString()}}, {Metrics: []string{strings.Repeat("x", 100000)}}, {Metrics: []string{"missing"}}} {
		if err := s.ValidateData(d); err == nil {
			t.Fatal("invalid selection accepted")
		}
	}
	if err := (domain.AdhocSelection{Rows: []string{id}}).ValidateData(d); err != nil {
		t.Fatal(err)
	}
}
