package domain

import (
	"errors"
	"testing"

	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/fieldops/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/mass"
)

func kg(s string) *mass.Kg { k := mass.MustKg(s); return &k }

// proto FM-03/FM-04: công đoạn sau bằng công đoạn trước, ít hơn thì phải có lý
// do chênh, nhiều hơn bị từ chối; công đoạn trước chưa có số thì không ghi được.
func TestStageEqualityFM04(t *testing.T) {
	prev := &Count{Rolls: 5800}
	cases := []struct {
		name  string
		stage string
		c     Count
		prev  *Count
		want  error
	}{
		{"bale không cần công đoạn trước", port.StageBale, Count{Rolls: 10}, nil, nil},
		{"gather thiếu bale (FM-03)", port.StageGather, Count{Rolls: 10}, nil, ErrPreviousStageMissing},
		{"bằng nhau", port.StageGather, Count{Rolls: 5800}, prev, nil},
		{"ít hơn không lý do", port.StageBoatLoad, Count{Rolls: 5700}, prev, ErrDiscrepancyNoReason},
		{"ít hơn có lý do", port.StageBoatLoad, Count{Rolls: 5700, DiscrepancyReason: "100 cuộn ướt để lại bờ"}, prev, nil},
		{"nhiều hơn", port.StageGather, Count{Rolls: 5801, DiscrepancyReason: "đếm lại được thêm"}, prev, ErrStageExceedsPrevious},
		{"rời sau cuộn", port.StageGather, Count{LooseKg: kg("100")}, prev, ErrUnitMismatch},
		{"rời bằng rời", port.StageGather, Count{LooseKg: kg("1500.5")}, &Count{LooseKg: kg("1500.5")}, nil},
		{"rời nhiều hơn rời", port.StageGather, Count{LooseKg: kg("1600")}, &Count{LooseKg: kg("1500")}, ErrStageExceedsPrevious},
		{"bản bù về 0", port.StageGather, Count{Rolls: 0, DiscrepancyReason: "nhập nhầm thửa khác"}, prev, nil},
	}
	for _, c := range cases {
		if err := CheckAgainstPrevious(c.stage, c.c, c.prev); !errors.Is(err, c.want) {
			t.Errorf("%s: %v, muốn %v", c.name, err, c.want)
		}
	}
	// Sửa công đoạn trước: công đoạn sau vẫn phải thỏa FM-04.
	if err := CheckAgainstNext(Count{Rolls: 90}, &Count{Rolls: 100}); !errors.Is(err, ErrNextStageExceeds) {
		t.Errorf("sửa bale nhỏ hơn gather: %v", err)
	}
	if err := CheckAgainstNext(Count{Rolls: 110}, &Count{Rolls: 100}); !errors.Is(err, ErrNextStageUnexplained) {
		t.Errorf("sửa bale lớn hơn gather không lý do: %v", err)
	}
	if err := CheckAgainstNext(Count{Rolls: 110}, &Count{Rolls: 100, DiscrepancyReason: "mười cuộn ướt bỏ lại"}); err != nil {
		t.Errorf("gather có lý do chênh: %v", err)
	}
}

// proto FM-09: bắt buộc số cuộn; tấn (kg rơm rời) chỉ khi không có cuộn.
func TestBaleCountMandatoryFM09(t *testing.T) {
	if err := ValidateCount(Count{}, false); err == nil {
		t.Error("không có cuộn, không có khối lượng phải bị từ chối")
	}
	if err := ValidateCount(Count{Rolls: 10, LooseKg: kg("200")}, false); err == nil {
		t.Error("có cuộn thì không được ghi khối lượng")
	}
	if err := ValidateCount(Count{LooseKg: kg("200")}, false); err != nil {
		t.Errorf("rơm rời: %v", err)
	}
	if err := ValidateCount(Count{}, true); err != nil {
		t.Errorf("bản bù về 0: %v", err)
	}
	if err := ValidateCount(Count{Rolls: 1, DiscrepancyReason: "ngắn"}, false); err == nil {
		t.Error("lý do chênh < 10 ký tự")
	}
}

func TestOrderTransitions(t *testing.T) {
	ok := [][2]string{
		{OrderPlanned, OrderPlanned}, {OrderPlanned, OrderApproved}, {OrderApproved, OrderAssigned},
		{OrderAssigned, OrderAssigned}, {OrderAssigned, OrderClosed}, {OrderPlanned, OrderCancelled}, {OrderAssigned, OrderCancelled},
	}
	for _, c := range ok {
		if err := Transition(c[0], c[1]); err != nil {
			t.Errorf("%s → %s: %v", c[0], c[1], err)
		}
	}
	bad := [][2]string{
		{OrderApproved, OrderApproved}, {OrderApproved, OrderPlanned}, {OrderPlanned, OrderAssigned},
		{OrderApproved, OrderClosed}, {OrderClosed, OrderCancelled}, {OrderCancelled, OrderApproved},
	}
	for _, c := range bad {
		if err := Transition(c[0], c[1]); !errors.Is(err, ErrOrderTransition) {
			t.Errorf("%s → %s phải bị chặn", c[0], c[1])
		}
	}
	if _, ok := ScheduleStatus(OrderPlanned, false); ok {
		t.Error("lệnh chưa duyệt không công bố")
	}
	if _, ok := ScheduleStatus(OrderCancelled, false); ok {
		t.Error("hủy lệnh chưa từng công bố thì không công bố")
	}
	if s, ok := ScheduleStatus(OrderCancelled, true); !ok || s != "cancelled" {
		t.Error("hủy lệnh đã công bố")
	}
}

func TestBuySideReconStatus(t *testing.T) {
	n := func(i int) *int { return &i }
	if ReconStatus(100, nil) != ReconPending || ReconStatus(100, n(100)) != ReconMatch || ReconStatus(95, n(100)) != ReconMismatch {
		t.Error("pending/match/mismatch")
	}
}

// proto baleKg: HTX (30 lô gần nhất) → hệ thống → cấu hình → 20.
func TestKgPerRollFallbackChain(t *testing.T) {
	s := func(rolls int, k string) WeighedSample {
		return WeighedSample{LotRef: ids.New(), Rolls: rolls, WeighedKg: mass.MustKg(k)}
	}
	cfg := decimal.RequireFromString("18.5")

	// Dữ liệu mẫu golden: 5700 cuộn cân 96,8 t → 16,98… → 17,0.
	r := ResolveKgPerRoll([]WeighedSample{s(5700, "96800")}, nil, &cfg)
	if r.Source != KgSourceHTX || r.Kg.String() != "17" || r.Samples != 1 {
		t.Fatalf("HTX: %+v", r)
	}
	r = ResolveKgPerRoll(nil, []WeighedSample{s(100, "2150"), s(100, "2050")}, &cfg)
	if r.Source != KgSourceSystem || r.Kg.String() != "21" || r.Samples != 2 {
		t.Fatalf("hệ thống: %+v", r)
	}
	r = ResolveKgPerRoll(nil, nil, &cfg)
	if r.Source != KgSourceConfig || r.Kg.String() != "18.5" {
		t.Fatalf("cấu hình: %+v", r)
	}
	zero := decimal.Zero
	r = ResolveKgPerRoll(nil, nil, &zero)
	if r.Source != KgSourceDefault || r.Kg.String() != "20" {
		t.Fatalf("mặc định: %+v", r)
	}
	// Chỉ 30 lô gần nhất (mới nhất trước) được tính.
	var many []WeighedSample
	for i := 0; i < 30; i++ {
		many = append(many, s(10, "200"))
	}
	many = append(many, s(10, "10000"))
	r = ResolveKgPerRoll(many, nil, nil)
	if r.Kg.String() != "20" || r.Samples != 30 {
		t.Fatalf("cửa sổ 30: %+v", r)
	}
}
