package domain_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/reportcycle/domain"
	khtcgolden "github.com/MEKONG-GREEN/Agrigreen_demo_v2/tests/golden/khtc"
)

// KHTCQA (13/10/2026) — quy tắc quy đổi kỳ nguồn → tuần KHTC, kiểm bằng hàm thuần của lịch kỳ W11.03: một kỳ nguồn
// tính vào tuần KHTC chứa NGÀY CUỐI của kỳ nguồn (docs/uat/khtc-week-mapping.md). Dữ liệu: tests/golden/khtc.

func weekKey(w domain.Week) string { return fmt.Sprintf("%d-W%02d", w.Year, w.No) }

func day(t *testing.T, s string) time.Time {
	t.Helper()
	d, err := domain.ParseDate(s)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

// khtcWeekOf là quy tắc quy đổi: tuần KHTC chứa ngày cuối của kỳ nguồn.
func khtcWeekOf(khtc domain.PeriodRule, sourceTo time.Time) domain.Week { return khtc.WeekOf(sourceTo) }

// TestKHTCQA_WeekCrosswalkGolden: mọi dòng crosswalk.csv — kỳ nguồn là một tuần thật của quy tắc nguồn (hoặc một ngày
// báo giá) và tuần KHTC = tuần chứa ngày cuối của kỳ nguồn theo DefaultRule (Thứ Tư – Thứ Ba, tuần 1 chứa 01/01).
func TestKHTCQA_WeekCrosswalkGolden(t *testing.T) {
	ds, err := khtcgolden.Load()
	if err != nil {
		t.Fatal(err)
	}
	khtc, err := domain.ParseRule(ds.Rules["khtc"])
	if err != nil {
		t.Fatal(err)
	}
	if khtc != domain.DefaultRule || ds.Rules["trong_trot"] != domain.DefaultRule.Tag() {
		t.Fatalf("lịch KHTC và Trồng trọt phải là quy tắc mặc định %s", domain.DefaultRule.Tag())
	}
	for _, r := range ds.Crosswalk {
		name := r.CaseID + "/" + r.Source + "/" + r.SourceKey
		from, to := day(t, r.From), day(t, r.To)
		if tag := ds.Rules[r.Source]; tag == "day" {
			if r.From != r.To || r.SourceKey != r.From {
				t.Errorf("%s: kỳ ngày phải có khóa = ngày", name)
			}
		} else {
			src, err := domain.ParseRule(tag)
			if err != nil {
				t.Fatalf("%s: %v", name, err)
			}
			sw := src.WeekOf(from)
			if weekKey(sw) != r.SourceKey || !sw.From.Equal(from) || !sw.To.Equal(to) {
				t.Errorf("%s: tuần nguồn theo %s là %s %s–%s", name, tag, weekKey(sw), sw.From.Format(time.DateOnly), sw.To.Format(time.DateOnly))
			}
		}
		kw := khtcWeekOf(khtc, to)
		if weekKey(kw) != r.KHTCWeek || kw.From.Format(time.DateOnly) != r.KHTCFrom || kw.To.Format(time.DateOnly) != r.KHTCTo {
			t.Errorf("%s: tuần KHTC là %s %s–%s, golden %s %s–%s", name, weekKey(kw), kw.From.Format(time.DateOnly),
				kw.To.Format(time.DateOnly), r.KHTCWeek, r.KHTCFrom, r.KHTCTo)
		}
	}
	// Số tuần trong năm của các năm trong ca cuối năm (G05, G05C).
	for _, c := range []struct {
		rule string
		year int
		n    int
	}{{"khtc", 2026, 52}, {"khtc", 2030, 53}, {"cnts", 2029, 53}, {"trong_trot_v1", 2026, 53}} {
		r, _ := domain.ParseRule(ds.Rules[c.rule])
		if got := r.WeeksInYear(c.year); got != c.n {
			t.Errorf("%s năm %d có %d tuần, golden %d", c.rule, c.year, got, c.n)
		}
	}
}

// TestKHTCQA_WeeklySourcesMapOneToOne: với mọi quy tắc tuần của nguồn (Trồng trọt v2, CNTS, Trồng trọt v1), mỗi tuần
// KHTC 2026–2031 nhận đúng một tuần nguồn — tuần KHTC "trống" chỉ xảy ra khi nguồn không phát hành kỳ (G02).
func TestKHTCQA_WeeklySourcesMapOneToOne(t *testing.T) {
	ds, err := khtcgolden.Load()
	if err != nil {
		t.Fatal(err)
	}
	khtc := domain.DefaultRule
	for _, name := range []string{"trong_trot", "cnts", "trong_trot_v1"} {
		src, err := domain.ParseRule(ds.Rules[name])
		if err != nil {
			t.Fatal(err)
		}
		hits := map[string]int{}
		for d := domain.Date(2025, 12, 1); d.Before(domain.Date(2032, 2, 1)); d = d.AddDate(0, 0, 7) {
			hits[weekKey(khtcWeekOf(khtc, src.WeekOf(d).To))]++
		}
		for y := 2026; y <= 2031; y++ {
			for no := 1; no <= khtc.WeeksInYear(y); no++ {
				if k := fmt.Sprintf("%d-W%02d", y, no); hits[k] != 1 {
					t.Errorf("%s: tuần KHTC %s nhận %d tuần nguồn", name, k, hits[k])
				}
			}
		}
	}
}
