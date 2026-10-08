package domain_test

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/metering/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/metering/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/meter"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/money"
)

var update = flag.Bool("update", false, "ghi lại tệp golden")

// ADR-0023 mục 3: ngày/tháng tính theo giờ Việt Nam.
func TestWindowsVietnamTime_ADR0023(t *testing.T) {
	at := time.Date(2026, 10, 31, 17, 30, 0, 0, time.UTC) // 00:30 ngày 01/11 giờ VN
	if got := domain.Day(at).Format(time.DateOnly); got != "2026-11-01" {
		t.Fatalf("ngày VN: %s", got)
	}
	if got := domain.Period(at); got != "2026-11" {
		t.Fatalf("kỳ VN: %s", got)
	}
	if got := domain.NextDayStart(at); !got.Equal(time.Date(2026, 11, 1, 17, 0, 0, 0, time.UTC)) {
		t.Fatalf("đầu ngày kế: %s", got)
	}
	if got := domain.NextMonthStart(at); !got.Equal(time.Date(2026, 11, 30, 17, 0, 0, 0, time.UTC)) {
		t.Fatalf("đầu tháng kế: %s", got)
	}
	s, e, err := domain.ParsePeriod("2026-12")
	if err != nil || !s.Equal(time.Date(2026, 11, 30, 17, 0, 0, 0, time.UTC)) || !e.Equal(time.Date(2026, 12, 31, 17, 0, 0, 0, time.UTC)) {
		t.Fatalf("kỳ 2026-12: %s %s %v", s, e, err)
	}
	if _, _, err := domain.ParsePeriod("2026-13"); err == nil {
		t.Fatal("kỳ sai phải lỗi")
	}
}

// ADR-0024 mục 1.3: × trung vị 28 ngày khi đủ 7 ngày mẫu; không đủ → ngưỡng tuyệt đối.
func TestThresholdMedianOrAbsolute_ADR0024(t *testing.T) {
	p := domain.DefaultEgressParams(meter.ChannelValve)
	if p.AnomalyRecords != 20_000 || !p.AnomalyFactor.Equal(decimal.NewFromInt(3)) || p.DailyQuota != 0 {
		t.Fatalf("mặc định van: %+v", p)
	}
	if th, basis := domain.Threshold([]int64{5, 5, 5}, p); th != 20_000 || basis != "absolute" {
		t.Fatalf("thiếu mẫu: %d %s", th, basis)
	}
	h := []int64{10, 12, 8, 10, 11, 9, 10} // trung vị 10
	if th, basis := domain.Threshold(h, p); th != 30 || basis != "median" {
		t.Fatalf("trung vị: %d %s", th, basis)
	}
	p.AnomalyFactor = decimal.RequireFromString("2.5")
	if th, _ := domain.Threshold(append(h, 13), p); th != 25 { // 8 mẫu: (10+10)/2 = 10 → 25
		t.Fatalf("hệ số thập phân: %d", th)
	}
	if domain.IsAnomaly(30, 30) || !domain.IsAnomaly(31, 30) || domain.IsAnomaly(1_000_000, 0) {
		t.Fatal("so ngưỡng sai")
	}
	if domain.DefaultEgressParams(meter.ChannelMRVPackage).DailyQuota != 50_000 {
		t.Fatal("hạn mức mặc định mrv_package = 50 000")
	}
}

func sampleStatement(t *testing.T, priced bool) port.Statement {
	t.Helper()
	st := port.Statement{
		ID: uuid.MustParse("019a0000-0000-7000-8000-000000000001"), ClientID: uuid.MustParse("019a0000-0000-7000-8000-000000000002"),
		ClientTenantID: uuid.MustParse("019a0000-0000-7000-8000-000000000003"), Period: "2026-10", Status: port.StatementApproved,
		FirstSeq: 1, LastSeq: 42, ChainHeadHash: "ab" + string(bytes.Repeat([]byte("0"), 62)),
		SnapshotSHA256: "cd" + string(bytes.Repeat([]byte("1"), 62)), RuleVersion: domain.RuleStatementV1,
		Lines: []port.StatementLine{
			{Product: "hub_capacity", StatusClass: "2xx", Calls: 3, Units: 3},
			{Product: "collection_volume", StatusClass: "4xx", Calls: 2},
			{Product: "collection_volume", StatusClass: "2xx", Calls: 37, Units: 37},
		},
	}
	if priced {
		p, err := money.ParsePrice("1500.5000")
		if err != nil {
			t.Fatal(err)
		}
		a := money.MustAmount("55518.50")
		st.Lines[2].UnitPrice, st.Lines[2].Amount, st.Lines[2].PriceVersion = &p, &a, "metering.price.default.collection_volume.per_unit_vnd@v1"
		z := money.MustAmount("0.00")
		st.Lines[1].UnitPrice, st.Lines[1].Amount, st.Lines[1].PriceVersion = &p, &z, "metering.price.default.collection_volume.per_unit_vnd@v1"
		sub := money.MustAmount("1000000.00")
		tot := money.MustAmount("1055518.50")
		st.Subscription, st.Total = &sub, &tot
	}
	return st
}

// ADR-0023 mục 4 (g5-conventions §9 "cùng CSV byte-cho-byte"): CSV bảng kê tất định — so tệp golden.
func TestStatementCSVGolden_ADR0023(t *testing.T) {
	for name, priced := range map[string]bool{"statement_units.golden.csv": false, "statement_priced.golden.csv": true} {
		st := sampleStatement(t, priced)
		got, err := domain.StatementCSV(st)
		if err != nil {
			t.Fatal(err)
		}
		// Thứ tự dòng đầu vào không ảnh hưởng.
		st.Lines[0], st.Lines[2] = st.Lines[2], st.Lines[0]
		again, _ := domain.StatementCSV(st)
		if !bytes.Equal(got, again) {
			t.Fatal("CSV phụ thuộc thứ tự dòng")
		}
		path := filepath.Join("testdata", name)
		if *update {
			if err := os.WriteFile(path, got, 0o644); err != nil {
				t.Fatal(err)
			}
		}
		want, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, bytes.ReplaceAll(want, []byte("\r\n"), []byte("\n"))) {
			t.Fatalf("%s lệch golden:\n%s", name, got)
		}
	}
}

// ADR-0023 mục 4: cùng ảnh chụp → cùng băm (không phụ thuộc thứ tự dòng); đổi một số → băm khác.
func TestSnapshotCanonicalHash_ADR0023(t *testing.T) {
	s := domain.Snapshot{RuleVersion: domain.RuleStatementV1, UnitsRules: []string{meter.RuleUnitsV1}, ClientID: "c", Period: "2026-10",
		StreamKey: "client:c:2026-10", FirstSeq: 1, LastSeq: 3, ChainHeadHash: "00",
		Lines: []domain.SnapshotLine{{Product: "b", StatusClass: "2xx", Calls: 1, Units: 1}, {Product: "a", StatusClass: "4xx", Calls: 2}}}
	b1, h1, err := s.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	s.Lines[0], s.Lines[1] = s.Lines[1], s.Lines[0]
	b2, h2, _ := s.Canonical()
	if h1 != h2 || !bytes.Equal(b1, b2) || len(h1) != 64 {
		t.Fatalf("băm phụ thuộc thứ tự: %s %s", h1, h2)
	}
	s.Lines[0].Units = 9
	if _, h3, _ := s.Canonical(); h3 == h1 {
		t.Fatal("đổi số liệu mà băm không đổi")
	}
}
