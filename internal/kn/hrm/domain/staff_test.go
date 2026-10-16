package domain

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func day(s string) time.Time { t, _ := time.Parse(time.DateOnly, s); return t }

func mk(kind EventKind, from string, seq int64, p map[string]string) Event {
	t, prec, _ := ParseDate(from)
	return Event{ID: uuid.New(), Kind: kind, ValidFrom: t, Precision: prec, Payload: p, Seq: seq}
}

// W11.22: bậc = phép chiếu (ngạch đặt bậc, sự kiện bậc sau thay bậc), as-of, trạng thái FN-08, dòng bị thay/hủy.
func TestProjectAsOf_W1122(t *testing.T) {
	g := mk(EventGrade, "2020-01-01", 1, map[string]string{"grade": "01.003", "step": "5", "step_since": "2020-01-01", "reserved_coeff": "0.12"})
	st := mk(EventStep, "2024-01-01", 2, map[string]string{"step": "6", "step_since": "2024-01-01"})
	u1 := mk(EventUnit, "2020-01-01", 3, map[string]string{"unit": "trong_trot"})
	u2 := mk(EventUnit, "2026-11-01", 4, map[string]string{"unit": "chan_nuoi"})
	ws := mk(EventWorkStart, "2015-03-01", 5, nil)
	we := mk(EventWorkEnd, "2026-08-15", 6, map[string]string{"reason": "nghi_huu"})
	sec := mk(EventSecondment, "2025-01-01", 7, map[string]string{"to_org": "Chi cục"})
	to := day("2025-06-30")
	sec.ValidTo = &to
	evs := []Event{g, st, u1, u2, ws, we, sec}

	s := Project(evs, day("2023-06-01"), nil)
	if s.Step != 5 || s.Reserved != "0.12" || s.Unit != "trong_trot" || s.Status != StatusActive || len(s.Secondments) != 0 {
		t.Fatalf("2023: %+v", s)
	}
	if s := Project(evs, day("2025-03-01"), nil); s.Step != 6 || s.Reserved != "" || len(s.Secondments) != 1 {
		t.Fatalf("2025: %+v", s)
	}
	if s := Project(evs, day("2026-08-14"), nil); s.Status != StatusActive {
		t.Fatalf("trước nghỉ: %+v", s)
	}
	if s := Project(evs, day("2026-08-15"), nil); s.Status != StatusLeft || s.LeaveReason != "nghi_huu" {
		t.Fatalf("ngày nghỉ: %+v", s)
	}
	if s := Project(evs, day("2026-12-01"), nil); s.Unit != "chan_nuoi" || len(s.Secondments) != 0 {
		t.Fatalf("sau đổi đơn vị: %+v", s)
	}
	if s := Project(evs, day("2010-01-01"), nil); s.Status != StatusNone || s.Grade != "" {
		t.Fatalf("trước vào làm: %+v", s)
	}
	// Hủy sự kiện bậc → bậc quay về 5; thay ngày nghỉ.
	void := Event{ID: uuid.New(), Kind: EventStep, ValidFrom: st.ValidFrom, Void: true, Supersedes: &st.ID, Seq: 8}
	we2 := mk(EventWorkEnd, "2026-09-30", 9, map[string]string{"reason": "nghi_huu"})
	we2.Supersedes = &we.ID
	evs = append(evs, void, we2)
	if s := Project(evs, day("2026-09-01"), nil); s.Step != 5 || s.Status != StatusActive {
		t.Fatalf("sau hủy/thay: %+v", s)
	}
	if err := ValidateWork(evs); err != nil {
		t.Fatal(err)
	}
	bad := append(evs, mk(EventWorkEnd, "2027-01-01", 10, map[string]string{"reason": "khac"}))
	if err := ValidateWork(bad); err == nil {
		t.Fatal("hai ngày nghỉ liên tiếp phải bị chặn")
	}
}

func TestNormalizeEvent_W1122(t *testing.T) {
	if _, _, err := NormalizeEvent(StaffCanBoXa, EventInput{Kind: EventUnit, ValidFrom: "2022", Payload: map[string]string{"unit": "trong_trot"}}); err == nil {
		t.Fatal("cán bộ xã phải có xã + đơn vị quản lý (BR-07)")
	}
	e, _, err := NormalizeEvent(StaffCanBoXa, EventInput{Kind: EventUnit, ValidFrom: "2022-03", Payload: map[string]string{"commune": "30292", "mgmt_unit": "trong_trot"}})
	if err != nil || e.Precision != PrecisionMonth || e.ValidFrom != day("2022-03-01") {
		t.Fatalf("%+v %v", e, err)
	}
	e, refs, err := NormalizeEvent(StaffVienChuc, EventInput{Kind: EventGrade, ValidFrom: "2020-01-01", Payload: map[string]string{"grade": " 01.003 ", "step": "05", "reserved_coeff": "0.1"}})
	if err != nil || e.Payload["grade"] != "01.003" || e.Payload["step"] != "5" || e.Payload["step_since"] != "2020-01-01" || e.Payload["reserved_coeff"] != "0.10" || len(refs) != 1 {
		t.Fatalf("ngạch: %+v %v %v", e, refs, err)
	}
	for _, in := range []EventInput{
		{Kind: EventAward, ValidFrom: "2024", Payload: map[string]string{"form": "x", "amount": "1000000"}}, // không số tiền
		{Kind: EventUnit, ValidFrom: "2024", ValidTo: "2025", Payload: map[string]string{"unit": "a"}},
		{Kind: EventSecondment, ValidFrom: "2024-05-01", ValidTo: "2024-01-01", Payload: map[string]string{"to_org": "X"}},
		{Kind: EventTraining, ValidFrom: "2024", Payload: map[string]string{"type": "it_level", "level": "b1"}},
		{Kind: EventWorkEnd, ValidFrom: "2024-13-01", Payload: map[string]string{"reason": "khac"}},
	} {
		if _, _, err := NormalizeEvent(StaffVienChuc, in); err == nil {
			t.Errorf("phải bị chặn: %+v", in)
		}
	}
	if StaffCode(StaffHopDong, 7) != "HD-00007" || StaffCanBoXa.HasSensitiveTab() {
		t.Fatal("mã/thẻ")
	}
}

// FN-20: mặc định theo BR-04/BR-03, người dùng ưu tiên, Admin cố định, thẻ TK chỉ HCTH/người dùng.
func TestPermMatrix_W1122(t *testing.T) {
	if m := DefaultMatrix(GroupManager); m[PermView] != ScopeUnit || m[PermSensitive] != ScopeNone || m[PermEdit] != ScopeNone {
		t.Fatalf("lãnh đạo: %v", m)
	}
	for _, g := range []string{GroupTinh, GroupXa, GroupTW} {
		if DefaultMatrix(g)[PermView] != ScopeNone {
			t.Fatalf("%s mặc định không xem hồ sơ", g)
		}
	}
	role, _, err := ParseMatrix(GranteeRole, GroupHCTH, []byte(`{"sensitive.view":"none"}`))
	if err != nil {
		t.Fatal(err)
	}
	user, _, err := ParseMatrix(GranteeUser, "0190f1a2-0000-7000-8000-000000000001", []byte(`{"sensitive.view":"all"}`))
	if err != nil {
		t.Fatal(err)
	}
	if m := EffectiveMatrix(GroupHCTH, role, nil); m[PermSensitive] != ScopeNone || m[PermView] != ScopeAll {
		t.Fatalf("nhóm: %v", m)
	}
	if m := EffectiveMatrix(GroupHCTH, role, user); m[PermSensitive] != ScopeAll {
		t.Fatalf("người dùng ưu tiên: %v", m)
	}
	if m := EffectiveMatrix(GroupAdmin, Matrix{PermView: ScopeNone}, Matrix{PermView: ScopeNone}); m[PermView] != ScopeAll {
		t.Fatal("Admin cố định")
	}
	for _, c := range []struct{ kind, grantee, raw string }{
		{GranteeRole, GroupAdmin, `{"profile.view":"none"}`},
		{GranteeRole, GroupManager, `{"sensitive.view":"all"}`},
		{GranteeRole, GroupHCTH, `{"sensitive.view":"unit"}`},
		{GranteeRole, "khac", `{}`},
		{GranteeUser, "not-a-uuid", `{}`},
		{GranteeRole, GroupTinh, `{"profile.view":"world"}`},
		{GranteeRole, GroupTinh, `{"salary.view":"all"}`},
	} {
		if _, _, err := ParseMatrix(c.kind, c.grantee, []byte(c.raw)); err == nil {
			t.Errorf("phải bị chặn: %+v", c)
		}
	}
	if !ScopeUnit.Allows(false, true) || ScopeSelf.Allows(false, true) || !ScopeSelf.Allows(true, false) || ScopeNone.Allows(true, true) {
		t.Fatal("Allows")
	}
}
