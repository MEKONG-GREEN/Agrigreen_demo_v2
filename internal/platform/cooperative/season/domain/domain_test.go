package domain

import (
	"errors"
	"testing"

	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/mass"
)

func step(seq int, act string, off, win int) Step {
	return Step{Seq: seq, Name: "Bước " + act, ActivityCode: act, DayOffset: off, WindowDays: win, Mandatory: true}
}

// HTX US-CAT-01 AC-6: thứ tự bước SOP phải liên tục 1..n.
func TestNormalizeStepsContiguousSeq(t *testing.T) {
	ok := []Step{step(1, ActLamDat, -5, 2), step(2, ActGieoSa, 0, 1), step(3, ActThuHoach, 95, 5)}
	if _, err := NormalizeSteps(ok); err != nil {
		t.Fatalf("bước hợp lệ bị từ chối: %v", err)
	}
	for name, steps := range map[string][]Step{
		"nhảy số":    {step(1, ActLamDat, 0, 1), step(3, ActThuHoach, 90, 3)},
		"bắt đầu 0":  {step(0, ActLamDat, 0, 1)},
		"đảo thứ tự": {step(2, ActLamDat, 0, 1), step(1, ActThuHoach, 90, 3)},
		"trùng số":   {step(1, ActLamDat, 0, 1), step(1, ActThuHoach, 90, 3)},
	} {
		_, err := NormalizeSteps(steps)
		var se StepOrderError
		if !errors.As(err, &se) {
			t.Errorf("%s: muốn StepOrderError, có %v", name, err)
		}
	}
	bad := []Step{{Seq: 1, Name: "x", ActivityCode: "nhay_mua", DayOffset: 0}}
	if _, err := NormalizeSteps(bad); err == nil {
		t.Error("hoạt động lạ phải bị từ chối")
	}
	ev := []Step{{Seq: 1, Name: "x", ActivityCode: ActBonPhan, EvidenceKinds: []string{"anh_hien_truong", "anh_hien_truong"}}}
	out, err := NormalizeSteps(ev)
	if err != nil || len(out[0].EvidenceKinds) != 1 {
		t.Errorf("gộp loại bằng chứng trùng: %v %v", out, err)
	}
}

// HTX US-YIELD-01 AC-1 (g2-conventions): cổng thu hoạch = sạ + offset − cửa sổ của bước thu hoạch.
func TestDatesForHarvestGate(t *testing.T) {
	sow := Day(2026, 9, 1)
	h, ok := HarvestStep([]Step{step(1, ActGieoSa, 0, 1), step(2, ActThuHoach, 95, 5), step(3, ActThuHoach, 100, 1)})
	if !ok || h.Seq != 2 {
		t.Fatalf("bước thu hoạch đầu tiên: %+v", h)
	}
	d := DatesFor(sow, h)
	if FormatDay(d.ExpectedHarvest) != "2026-12-05" || FormatDay(d.HarvestOpen) != "2026-11-30" {
		t.Errorf("mốc: %s %s", FormatDay(d.ExpectedHarvest), FormatDay(d.HarvestOpen))
	}
}

// HTX US-SEASON-01 AC-2: ngày sạ trong [hôm nay − 15, hôm nay + 30].
func TestCheckSowingDate(t *testing.T) {
	today := Day(2026, 9, 29)
	for off, want := range map[int]bool{-16: false, -15: true, 0: true, 30: true, 31: false} {
		err := CheckSowingDate(AddDays(today, off), today)
		if (err == nil) != want {
			t.Errorf("lệch %d ngày: err=%v, muốn hợp lệ=%v", off, err, want)
		}
	}
}

// HTX US-GPS-01/02: nhãn vị trí và nguồn xác thực.
func TestGPSNormalizeAndStatus(t *testing.T) {
	lon, lat := decimal.RequireFromString("105.6"), decimal.RequireFromString("10.05")
	g := GPS{Lon: &lon, Lat: &lat}
	if err := g.Normalize(); err != nil || g.Source != GPSDevice {
		t.Fatalf("mặc định device: %v %s", err, g.Source)
	}
	g = GPS{}
	if err := g.Normalize(); err != nil || g.Source != GPSDenied {
		t.Fatalf("không toạ độ → denied: %v %s", err, g.Source)
	}
	g = GPS{Source: GPSPhotoEXIF}
	if err := g.Normalize(); err == nil {
		t.Error("photo_exif thiếu toạ độ phải lỗi")
	}
	g = GPS{Source: GPSManualPick, Lon: &lon, Lat: &lat}
	if err := g.Normalize(); err == nil {
		t.Error("manual_pick kèm toạ độ phải lỗi")
	}
	tol := DefaultGPSToleranceM
	d50, d51 := decimal.NewFromInt(50), decimal.NewFromInt(51)
	if LocationStatus(&d50, tol) != LocMatch || LocationStatus(&d51, tol) != LocMismatch || LocationStatus(nil, tol) != LocMissing {
		t.Error("nhãn vị trí sai")
	}
}

// HTX US-YIELD-03: 0 → cần xác nhận; vượt ngưỡng → cần xác nhận.
func TestYieldAndAnomaly(t *testing.T) {
	area := decimal.RequireFromString("1.5")
	y := YieldTPerHa(mass.MustKg("9750"), area)
	if y.String() != "6.5" {
		t.Errorf("năng suất: %s", y)
	}
	if Anomaly(mass.MustKg("0"), decimal.Zero, DefaultYieldAnomalyMax) != AnomalyZero {
		t.Error("0 phải là bất thường")
	}
	if Anomaly(mass.MustKg("30000"), YieldTPerHa(mass.MustKg("30000"), area), DefaultYieldAnomalyMax) != AnomalyAbove {
		t.Error("20 t/ha phải vượt ngưỡng 12")
	}
	if Anomaly(mass.MustKg("9750"), y, DefaultYieldAnomalyMax) != "" {
		t.Error("6.5 t/ha không bất thường")
	}
	if ExpectedKg(area, decimal.RequireFromString("6.75")).String() != "10125" {
		t.Error("sản lượng dự kiến")
	}
}

func TestNotePII(t *testing.T) {
	if _, err := NormalizeNote("note", "Gọi anh Ba 0912345678"); err == nil {
		t.Error("ghi chú có SĐT phải bị từ chối")
	}
	if _, err := NormalizeNote("note", "Bón NPK 20-20-15, 50 kg"); err != nil {
		t.Errorf("ghi chú hợp lệ: %v", err)
	}
	if _, err := NormalizeInputs([]Input{{Name: "Urê", Qty: decimal.RequireFromString("1.12345"), Unit: "kg"}}); err == nil {
		t.Error("quá 4 chữ số lẻ phải lỗi")
	}
	if NameKey("  SOP   Lúa  OM5451 ") != "sop lúa om5451" {
		t.Error("NameKey")
	}
}
