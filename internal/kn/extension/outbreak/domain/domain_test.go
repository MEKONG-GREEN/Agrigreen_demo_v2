package domain_test

import (
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/outbreak/domain"
)

var (
	now   = time.Date(2027, 10, 13, 3, 0, 0, 0, time.UTC)
	today = time.Date(2027, 10, 13, 0, 0, 0, 0, time.UTC)
)

func valid() domain.Outbreak {
	return domain.Outbreak{CommuneCode: "30001", DiseaseCode: "dtlcp", SpeciesCode: "heo", ReceivedAt: now.Add(-time.Hour), Source: "can_bo",
		Locality: "Ấp 1", Location: &domain.Point{Lat: decimal.RequireFromString("10.25"), Lon: decimal.RequireFromString("105.97")},
		LocationSource: "gps", HerdSize: 100, Dead: 3, Symptoms: "Sốt", AsOf: today}
}

func fields(es domain.Errors) map[string]bool {
	m := map[string]bool{}
	for _, e := range es {
		m[e.Field] = true
	}
	return m
}

// UR-XA-10 quy tắc chặn: thiếu vị trí; chết, tiêu hủy > quy mô đàn; văn bản không chứa số điện thoại/CCCD.
func TestUR_XA_10_Rules(t *testing.T) {
	o := valid()
	if es := domain.NormalizeOutbreak(&o, now, today); len(es) != 0 {
		t.Fatalf("hợp lệ: %v", es)
	}
	kg := decimal.RequireFromString("1.2345")
	culled := int64(101)
	cases := map[string]func(*domain.Outbreak){
		"location":        func(o *domain.Outbreak) { o.Location = nil },
		"dead":            func(o *domain.Outbreak) { o.Dead = 101 },
		"culled_head":     func(o *domain.Outbreak) { o.CulledHead = &culled },
		"culled_kg":       func(o *domain.Outbreak) { o.CulledKg = &kg },
		"symptoms":        func(o *domain.Outbreak) { o.Symptoms = "gọi 0912 345 678" },
		"as_of":           func(o *domain.Outbreak) { o.AsOf = today.AddDate(0, 0, 1) },
		"locality":        func(o *domain.Outbreak) { o.Locality = "" },
		"source":          func(o *domain.Outbreak) { o.Source = "khac" },
		"location_source": func(o *domain.Outbreak) { o.LocationSource = "" },
	}
	for field, mut := range cases {
		o := valid()
		mut(&o)
		if es := domain.NormalizeOutbreak(&o, now, today); !fields(es)[field] {
			t.Errorf("%s: muốn lỗi trường, nhận %v", field, es)
		}
	}
	o = valid()
	o.Location = &domain.Point{Lat: decimal.Zero, Lon: decimal.Zero}
	if es := domain.NormalizeOutbreak(&o, now, today); !fields(es)["location"] {
		t.Errorf("tọa độ 0,0 phải bị chặn: %v", es)
	}
}

func TestStatusOf(t *testing.T) {
	for ev, want := range map[string]string{domain.EventReported: domain.StatusReported, domain.EventUpdated: domain.StatusMonitoring,
		domain.EventReopened: domain.StatusMonitoring, domain.EventClosed: domain.StatusClosed} {
		if got := domain.StatusOf(ev); got != want {
			t.Errorf("%s → %s, muốn %s", ev, got, want)
		}
	}
}

func TestNormalizeSuspect(t *testing.T) {
	s := domain.Suspect{CommuneCode: "30001", ReporterKind: "nong_ho", ObservedAt: now, Locality: "Ấp 3", Description: "Heo bỏ ăn"}
	if es := domain.NormalizeSuspect(&s, now); len(es) != 0 {
		t.Fatalf("tin báo không vị trí vẫn hợp lệ: %v", es)
	}
	s.Description = ""
	if es := domain.NormalizeSuspect(&s, now); !fields(es)["description"] {
		t.Fatalf("thiếu mô tả: %v", es)
	}
}
