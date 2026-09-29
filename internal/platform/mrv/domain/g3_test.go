package domain

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

func burning(src string, v int) ParamUse {
	return ParamUse{Key: "mrv.co2.open_burning_kg_per_t", Version: v, Value: "1460", Unit: "kgCO2e/t", Source: src}
}

func road(src string) ParamUse {
	return ParamUse{Key: "mrv.co2.transport_road_kg_per_tkm", Value: "0.062", Source: src}
}

func water(src string) ParamUse {
	return ParamUse{Key: "mrv.co2.transport_waterway_kg_per_tkm", Value: "0.021", Source: src}
}

// KN US-MRV-05: chỉ bằng chứng của thửa/vụ đã duyệt được tính; tấn = cận dưới bậc (bảo thủ).
func TestComputeEstimateOnlyApprovedCountsKNUSMRV05(t *testing.T) {
	in := []EstimateInput{{EvidenceRef: "b", Revision: 1, Bucket: "2000-5000"}, {EvidenceRef: "a", Revision: 2, Bucket: "500-1000"}}
	not := ComputeEstimate("mrv.co2.estimate.v1", in, false, nil, burning("default", 0), road("default"), water("default"))
	if !not.CountedTonnes.IsZero() || !not.AvoidedKg.IsZero() || not.Inputs[0].Exclusion != ExcludeNotApproved {
		t.Fatalf("chưa duyệt không được tính: %+v", not)
	}
	ok := ComputeEstimate("mrv.co2.estimate.v1", in, true, nil, burning("default", 0), road("default"), water("default"))
	if ok.CountedTonnes.String() != "2.5" || ok.AvoidedKg.String() != "3650" {
		t.Fatalf("2,5 t × 1460 = 3650 kgCO2e, nhận %s / %s", ok.CountedTonnes, ok.AvoidedKg)
	}
	if ok.Inputs[0].EvidenceRef != "a" || ok.TransportStatus != TransportNoLegData || ok.TransportKg != nil {
		t.Fatalf("thứ tự ổn định và không có chặng: %+v", ok)
	}
	if ok.InputSHA256 == not.InputSHA256 {
		t.Fatal("tư cách duyệt phải nằm trong băm đầu vào")
	}
	again := ComputeEstimate("mrv.co2.estimate.v1", []EstimateInput{in[1], in[0]}, true, nil, burning("default", 0), road("default"), water("default"))
	if again.InputSHA256 != ok.InputSHA256 {
		t.Fatal("tính tất định: cùng đầu vào → cùng băm")
	}
}

func TestComputeEstimateTransportLegs(t *testing.T) {
	in := []EstimateInput{{EvidenceRef: "a", Bucket: "10000+"}}
	r := ComputeEstimate("v", in, true, []TransportLeg{{Mode: "road", Km: decimal.NewFromInt(10)}, {Mode: "waterway", Km: decimal.NewFromInt(100)}},
		burning("default", 0), road("default"), water("default"))
	// 10 t × (10 km × 0,062 + 100 km × 0,021) = 10 × (0,62 + 2,1) = 27,2
	if r.TransportKg == nil || r.TransportKg.String() != "27.2" || r.TransportStatus != TransportComputed {
		t.Fatalf("vận chuyển: %+v", r.TransportKg)
	}
	bad := ComputeEstimate("v", []EstimateInput{{EvidenceRef: "x", Bucket: "?"}}, true, nil, burning("default", 0), road("default"), water("default"))
	if bad.Inputs[0].Exclusion != ExcludeBadBucket || !bad.CountedTonnes.IsZero() {
		t.Fatalf("bậc lạ: %+v", bad)
	}
}

// Chính thức chỉ khi mọi tham số là bản đã duyệt đúng phiên bản phương pháp ghim.
func TestMethodCoversProvisionalVsOfficial(t *testing.T) {
	used := []ParamUse{burning(ParamSourceApproved, 2), {Key: "r", Version: 1, Source: ParamSourceApproved}}
	pins := []ParamPin{{Key: "mrv.co2.open_burning_kg_per_t", Version: 2}, {Key: "r", Version: 1}}
	if !MethodCovers(pins, used) {
		t.Fatal("khớp phải chính thức")
	}
	if MethodCovers([]ParamPin{{Key: "mrv.co2.open_burning_kg_per_t", Version: 1}, {Key: "r", Version: 1}}, used) {
		t.Fatal("lệch phiên bản không được chính thức")
	}
	used[1].Source = ParamSourceDefault
	if MethodCovers(pins, used) {
		t.Fatal("tham số mặc định không bao giờ chính thức")
	}
	if MethodCovers(nil, nil) {
		t.Fatal("rỗng không chính thức")
	}
}

// KN US-MRV-02: gợi ý khớp theo thửa/vụ/cửa sổ hoạt động.
func TestSuggestMatchKNUSMRV02(t *testing.T) {
	t0 := time.Date(2026, 9, 1, 8, 0, 0, 0, time.UTC)
	log := LogItem{LogRef: uuid.New(), ActivityCode: "tuoi_nuoc", EventTime: t0}
	w := 3 * 24 * time.Hour
	if r, _ := SuggestMatch(log, nil, w); r != CompareUnverified {
		t.Fatalf("không có minh chứng: %s", r)
	}
	far := OfficerRecord{EvidenceType: "awd_log", EventTime: t0.Add(5 * 24 * time.Hour)}
	if r, _ := SuggestMatch(log, []OfficerRecord{far}, w); r != CompareUnverified {
		t.Fatalf("ngoài cửa sổ: %s", r)
	}
	other := OfficerRecord{EvidenceType: "straw_handling", EventTime: t0.Add(time.Hour)}
	if r, m := SuggestMatch(log, []OfficerRecord{other}, w); r != CompareMismatch || m == nil {
		t.Fatalf("trong cửa sổ khác hoạt động: %s", r)
	}
	awd := OfficerRecord{EvidenceType: "awd_log", EventTime: t0.Add(-2 * 24 * time.Hour)}
	if r, _ := SuggestMatch(log, []OfficerRecord{other, awd}, w); r != CompareMatch {
		t.Fatalf("awd ↔ tưới nước: %s", r)
	}
	explicit := OfficerRecord{EvidenceType: "field_checklist", ActivityCode: "bon_phan", EventTime: t0}
	if r, _ := SuggestMatch(log, []OfficerRecord{explicit}, w); r != CompareMismatch {
		t.Fatalf("activity_code ghi rõ khác: %s", r)
	}
}

// KN FR-08: không có bản ghi mới N ngày khi vụ đang mở.
func TestNoRecordAnomalyKNFR08(t *testing.T) {
	now := time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC)
	if f, d := NoRecordAnomaly("open", now.AddDate(0, 0, -15), now, 14); !f || d != 15 {
		t.Fatalf("15 ngày ≥ 14: %v %d", f, d)
	}
	if f, _ := NoRecordAnomaly("open", now.AddDate(0, 0, -13), now, 14); f {
		t.Fatal("13 ngày chưa bất thường")
	}
	if f, _ := NoRecordAnomaly("completed", now.AddDate(0, 0, -40), now, 14); f {
		t.Fatal("vụ đã đóng không cảnh báo")
	}
}

func TestBucketLowerKg(t *testing.T) {
	for b, want := range map[string]int64{"0-500": 0, "1000-2000": 1000, "10000+": 10000} {
		if v, ok := BucketLowerKg(b); !ok || v.IntPart() != want {
			t.Fatalf("%s: %v", b, v)
		}
	}
	if _, ok := BucketLowerKg("20000+"); ok {
		t.Fatal("bậc lạ")
	}
}

// Gói G1 không có dữ liệu G3 giữ nguyên băm (omitempty).
func TestSnapshotG3SectionsOmittedKeepHash(t *testing.T) {
	_, a, err := Snapshot{}.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	b, _, _ := Snapshot{}.Canonical()
	if string(b) != `{"catalogs":[],"evidence":[]}` || a == "" {
		t.Fatalf("dạng chuẩn G1 đổi: %s", b)
	}
}
