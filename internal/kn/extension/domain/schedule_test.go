package domain

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
)

func day(s string) time.Time { d, _ := ParseDate(s); return d }

// KN v7.7 FN-XA-03.08: tuần Thứ Hai giờ VN, mặc định tuần kế tiếp, khóa khi tuần kết thúc, hạn theo giờ lệch.
func TestFN_XA_03_08_WeekRules(t *testing.T) {
	z := clock.Zone()
	// Chủ Nhật 20/09/2026 23:30 giờ VN = 16:30 UTC → vẫn tuần 14/09; tuần kế tiếp 21/09.
	now := time.Date(2026, 9, 20, 16, 30, 0, 0, time.UTC)
	if w := WeekStartOf(now, z); !w.Equal(day("2026-09-14")) {
		t.Fatalf("tuần hiện tại theo giờ VN: %v", w)
	}
	if w := NextWeekStart(now, z); !w.Equal(day("2026-09-21")) {
		t.Fatalf("tuần kế tiếp: %v", w)
	}
	if WeekLocked(day("2026-09-14"), now, z) || !WeekLocked(day("2026-09-14"), now.Add(time.Hour), z) {
		t.Fatal("khóa đúng 24:00 Chủ Nhật giờ VN")
	}
	if due := ScheduleDueAt(day("2026-09-21"), -55, z); !due.Equal(time.Date(2026, 9, 18, 10, 0, 0, 0, time.UTC)) {
		t.Fatalf("hạn −55 giờ = 17:00 Thứ Sáu giờ VN: %v", due)
	}
	if err := CheckWeek(day("2026-09-22"), now, z); err == nil {
		t.Fatal("không phải Thứ Hai")
	}
	if err := CheckWeek(day("2026-11-23"), now, z); !errors.Is(err, ErrWeekTooFar) {
		t.Fatalf("quá 8 tuần: %v", err)
	}
}

// KN v7.7 FN-XA-03.08 / FN-TI-02.07: trùng thời gian (cảnh báo), so bản đã duyệt, chuyển trạng thái, Log cũ → mới.
func TestFN_XA_03_08_ItemRules(t *testing.T) {
	a := ScheduleItem{ItemID: uuid.New(), Day: day("2026-09-21"), Session: SessionMorning, Start: "08:00", End: "10:00", Content: "Thăm đồng"}
	b := ScheduleItem{ItemID: uuid.New(), Day: day("2026-09-21"), Session: SessionMorning, Start: "09:30", Content: "Họp HTX"}
	c := ScheduleItem{ItemID: uuid.New(), Day: day("2026-09-21"), Session: SessionAfternoon, Content: "Tập huấn"}
	d := ScheduleItem{ItemID: uuid.New(), Day: day("2026-09-21"), Session: SessionAfternoon, Start: "16:30", Content: "Tổng hợp"}
	cf := Conflicts([]ScheduleItem{a, b, c, d})
	if !cf[a.ItemID] || !cf[b.ItemID] || !cf[c.ItemID] || !cf[d.ItemID] || len(Conflicts([]ScheduleItem{a, c})) != 0 {
		t.Fatalf("trùng giờ: 08–10 × 09:30 (1 giờ); chiều cả buổi × 16:30: %v", cf)
	}
	for _, it := range []*ScheduleItem{&a, &b, &c, &d} {
		it.CommuneCode, it.Location = "30001", "UBND xã"
	}
	norm, err := NormalizeItems(day("2026-09-21"), []ScheduleItem{c, a}, []string{"30001"}, uuid.New)
	if err != nil || norm[0].ItemID != a.ItemID || norm[1].Position != 1 {
		t.Fatalf("sắp theo ngày, buổi, giờ: %v %v", norm, err)
	}
	// Log bug TT-05/CN-04: cán bộ xã bắt buộc Xã/phường; mọi mục bắt buộc Địa điểm (chữ hoặc vị trí).
	noCommune, noPlace, gpsOnly := a, a, a
	noCommune.CommuneCode = " "
	noPlace.Location = " "
	gpsOnly.Location, gpsOnly.Place = "", &GeoPoint{Lat: 10, Lon: 105}
	var fe FieldError
	if _, err := NormalizeItems(day("2026-09-21"), []ScheduleItem{noCommune}, []string{"30001"}, uuid.New); !errors.Is(err, ErrItemCommuneReq) || !errors.As(err, &fe) || fe.Field != "items[0].commune_code" {
		t.Fatalf("cán bộ xã thiếu xã: %v", err)
	}
	// Quyết định PO 25/10/2026 (R-B1): cán bộ tỉnh cũng bắt buộc Xã/phường; xã phải thuộc tỉnh (allowed = xã của tỉnh).
	if _, err := NormalizeItems(day("2026-09-21"), []ScheduleItem{b, noCommune}, []string{"30001", "30002"}, uuid.New); !errors.Is(err, ErrItemCommuneReq) || !errors.As(err, &fe) || fe.Field != "items[1].commune_code" {
		t.Fatalf("cán bộ tỉnh thiếu xã: %v", err)
	}
	if _, err := NormalizeItems(day("2026-09-21"), []ScheduleItem{noCommune}, nil, uuid.New); !errors.Is(err, ErrItemCommuneReq) {
		t.Fatalf("không giới hạn xã (nil) vẫn bắt buộc xã: %v", err)
	}
	other := a
	other.CommuneCode = "31001"
	if _, err := NormalizeItems(day("2026-09-21"), []ScheduleItem{other}, []string{"30001", "30002"}, uuid.New); !errors.Is(err, ErrItemCommune) || !errors.As(err, &fe) || fe.Field != "items[0].commune_code" {
		t.Fatalf("cán bộ tỉnh chọn xã ngoài tỉnh: %v", err)
	}
	if _, err := NormalizeItems(day("2026-09-21"), []ScheduleItem{noPlace}, nil, uuid.New); !errors.Is(err, ErrItemLocationReq) || !errors.As(err, &fe) || fe.Field != "items[0].location" {
		t.Fatalf("thiếu địa điểm: %v", err)
	}
	if _, err := NormalizeItems(day("2026-09-21"), []ScheduleItem{gpsOnly}, []string{"30001"}, uuid.New); err != nil {
		t.Fatalf("chỉ có vị trí trên bản đồ vẫn đủ địa điểm: %v", err)
	}
	bad := a
	bad.Participants = "Chú Tư 0912345678"
	if _, err := NormalizeItems(day("2026-09-21"), []ScheduleItem{bad}, nil, uuid.New); !errors.Is(err, ErrPIIInText) {
		t.Fatalf("SĐT trong thành phần phối hợp: %v", err)
	}
	changed := a
	changed.Start = "07:30"
	if m := ChangedSince([]ScheduleItem{changed, c}, []ScheduleItem{a}); !m[a.ItemID] || !m[c.ItemID] {
		t.Fatalf("mục đổi giờ và mục mới là phần điều chỉnh: %v", m)
	}
	diff := DiffItems([]ScheduleItem{a, b}, []ScheduleItem{changed, c})
	if len(diff) != 3 || diff[0].Field != "T2 21/09 Sáng 08:00 · Giờ bắt đầu" || *diff[0].Old != "08:00" || *diff[0].New != "07:30" ||
		diff[1].Field != "Thêm mục" || diff[2].Field != "Bỏ mục" {
		t.Fatalf("Log cũ → mới: %+v", diff)
	}
	for _, c := range []struct {
		status, action string
		approved       int
		act, after     string
		err            bool
	}{
		{"", SaveDraft, 0, ScheduleActDraftSaved, ScheduleDraft, false},
		{ScheduleDraft, SaveSubmit, 0, ScheduleActSubmitted, SchedulePending, false},
		{SchedulePending, SaveDraft, 0, "", "", true},
		{ScheduleApproved, SaveSubmit, 3, ScheduleActAmended, SchedulePending, false},
		{ScheduleChangesRequested, SaveDraft, 0, ScheduleActDraftSaved, ScheduleDraft, false},
	} {
		act, after, err := NextOnSave(c.status, c.action, c.approved)
		if (err != nil) != c.err || act != c.act || after != c.after {
			t.Fatalf("%s + %s: %s %s %v", c.status, c.action, act, after, err)
		}
	}
	if err := ValidScheduleReason("ngắn"); err == nil || ValidScheduleReason("Bổ sung địa điểm buổi chiều") != nil {
		t.Fatal("lý do 10–1000 ký tự")
	}
	if !ApproverLevelAllowed("", []string{"truong_phong"}) || ApproverLevelAllowed("chuyen_vien", []string{"truong_phong"}) {
		t.Fatal("cấp quản lý duyệt (Q-05; rỗng = chưa có P1 → được)")
	}
}
