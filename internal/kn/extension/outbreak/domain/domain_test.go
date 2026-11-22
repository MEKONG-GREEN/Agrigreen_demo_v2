package domain_test

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
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
		"household_ref":   func(o *domain.Outbreak) { r := uuid.New(); o.HouseholdRef = &r }, // v1 không ghi được nữa
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

// CNTS v4.0 (#252): địa bàn chỉ là ghi chú tùy chọn — DB-04 là hộ chọn trong danh sách.
func TestLocalityOptional(t *testing.T) {
	o := valid()
	o.Locality = ""
	if es := domain.NormalizeOutbreak(&o, now, today); len(es) != 0 {
		t.Fatalf("thiếu địa bàn vẫn hợp lệ: %v", es)
	}
}

// UR-XA-10 DB-16: số ha ≥ 0, tối đa 2 chữ số thập phân; kiểm cú pháp trước khi parse.
func TestParseHectares(t *testing.T) {
	for _, ok := range []string{"0", "12.5", "1234567890.99", " 3.10 "} {
		if d, good := domain.ParseHectares(ok); !good || d == nil {
			t.Errorf("%q phải hợp lệ", ok)
		}
	}
	if d, good := domain.ParseHectares(""); !good || d != nil {
		t.Errorf("rỗng → nil")
	}
	for _, bad := range []string{"-1", "1.234", "1e100000", "1e-100000", "12345678901", "1,5", "abc", ".5", strings.Repeat("9", 5000)} {
		if _, good := domain.ParseHectares(bad); good {
			t.Errorf("%q phải bị chặn", bad)
		}
	}
}

// FN-11 AC-05: bệnh nhóm thủy sản để trống DB-16 thì không gửi được; bệnh khác không ghi DB-16.
func TestCheckAqua(t *testing.T) {
	o := valid()
	if es := domain.CheckAqua(&o, true); !fields(es)["aqua_farmed_ha"] || !fields(es)["aqua_wild_ha"] {
		t.Fatalf("thiếu DB-16: %v", es)
	}
	z := decimal.Zero
	o.AquaFarmedHa, o.AquaWildHa = &z, &z
	if es := domain.CheckAqua(&o, true); len(es) != 0 {
		t.Fatalf("DB-16 = 0 hợp lệ: %v", es)
	}
	if es := domain.CheckAqua(&o, false); !fields(es)["aqua_farmed_ha"] {
		t.Fatalf("bệnh không thủy sản không ghi DB-16: %v", es)
	}
}

// UR-XA-10: DB-04 phải có ít nhất 1 hộ (DB-15 ≥ 1), không lặp, tối đa 200.
func TestCheckHouseholds(t *testing.T) {
	a := uuid.New()
	if es := domain.CheckHouseholds(nil, 0); !fields(es)["households"] {
		t.Fatal("không hộ nào phải bị chặn")
	}
	if es := domain.CheckHouseholds([]uuid.UUID{a}, 0); len(es) != 0 {
		t.Fatalf("một hộ hợp lệ: %v", es)
	}
	if es := domain.CheckHouseholds(nil, 1); len(es) != 0 {
		t.Fatalf("một hộ mới hợp lệ: %v", es)
	}
	if es := domain.CheckHouseholds([]uuid.UUID{a, a}, 0); !fields(es)["households"] {
		t.Fatal("hộ lặp phải bị chặn")
	}
	if es := domain.CheckHouseholds([]uuid.UUID{a}, domain.MaxHouseholds); !fields(es)["households"] {
		t.Fatal("quá 200 hộ phải bị chặn")
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
