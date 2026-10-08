package domain

import (
	"testing"
	"time"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/port"
)

var kn = port.Profile{Name: "kn", Hierarchy: []string{"TW", "TINH", "XA"}, Roles: map[string]string{"kn_tinh": "TINH", "kn_xa": "XA"},
	PublishBy: "TINH", CloseBy: "TINH", DefineBy: []string{"admin"}}

func TestValidateProfile(t *testing.T) {
	if err := ValidateProfile(kn); err != nil {
		t.Fatal(err)
	}
	if kn.Reviewer() != "TINH" || kn.Leaf() != "XA" {
		t.Fatalf("reviewer/leaf: %s %s", kn.Reviewer(), kn.Leaf())
	}
	bad := []func(p *port.Profile){
		func(p *port.Profile) { p.Name = "KN" },
		func(p *port.Profile) { p.Hierarchy = []string{"TW"} },
		func(p *port.Profile) { p.Hierarchy = []string{"TW", "TW"} },
		func(p *port.Profile) { p.PublishBy = "XA" },  // cấp lá không phát hành
		func(p *port.Profile) { p.CloseBy = "HUYEN" }, // ngoài cây
		func(p *port.Profile) { p.ReviewBy = "XA" },   // cấp lá không rà soát
		func(p *port.Profile) { p.Roles = map[string]string{"kn_xa": "HUYEN"} },
		func(p *port.Profile) { p.Roles = nil },
		func(p *port.Profile) { p.DefineBy = nil },
	}
	for i, mut := range bad {
		p := kn
		p.Roles = map[string]string{"kn_tinh": "TINH", "kn_xa": "XA"}
		mut(&p)
		if ValidateProfile(p) == nil {
			t.Errorf("cấu hình sai %d được chấp nhận: %+v", i, p)
		}
	}
}

func TestValidateTemplateAndValues(t *testing.T) {
	for _, f := range Fixtures() {
		if err := ValidateTemplate(f.Code, f.Title, f.Fields); err != nil {
			t.Fatalf("fixture %s: %v", f.Code, err)
		}
	}
	fields := Fixtures()[0].Fields
	cases := []struct {
		name string
		code string
		fs   []Field
		ok   bool
	}{
		{"mã thường", "fx-01", fields, false},
		{"không trường", "FX", nil, false},
		{"khóa trùng", "FX", []Field{{Key: "a", Label: "A", Type: TypeText}, {Key: "a", Label: "B", Type: TypeText}}, false},
		{"kiểu lạ", "FX", []Field{{Key: "a", Label: "A", Type: "float"}}, false},
		{"choice rỗng", "FX", []Field{{Key: "a", Label: "A", Type: TypeChoice}}, false},
		{"options cho text", "FX", []Field{{Key: "a", Label: "A", Type: TypeText, Options: []string{"x"}}}, false},
		{"hợp lệ", "FX.02_A", []Field{{Key: "a", Label: "A", Type: TypeInteger, Required: true}}, true},
	}
	for _, c := range cases {
		if err := ValidateTemplate(c.code, "Tiêu đề", c.fs); (err == nil) != c.ok {
			t.Errorf("%s: %v", c.name, err)
		}
	}
	good := map[string]string{"crop": "lua", "area_planned": "120.5", "area_sown": "-0.000001", "households": "42", "report_date": "2026-10-04"}
	if err := ValidateValues(fields, good); err != nil {
		t.Fatal(err)
	}
	for name, mut := range map[string]func(m map[string]string){
		"khóa lạ":           func(m map[string]string) { m["x"] = "1" },
		"thiếu bắt buộc":    func(m map[string]string) { delete(m, "crop") },
		"choice lạ":         func(m map[string]string) { m["crop"] = "mia" },
		"float số mũ":       func(m map[string]string) { m["area_sown"] = "1e3" },
		"quá 6 chữ số lẻ":   func(m map[string]string) { m["area_sown"] = "1.1234567" },
		"integer thập phân": func(m map[string]string) { m["households"] = "4.2" },
		"ngày sai":          func(m map[string]string) { m["report_date"] = "2026-13-01" },
	} {
		m := map[string]string{}
		for k, v := range good {
			m[k] = v
		}
		mut(m)
		if ValidateValues(fields, m) == nil {
			t.Errorf("%s: được chấp nhận", name)
		}
	}
	if err := ValidateUnits([]string{"U1", "U1"}); err == nil {
		t.Error("đơn vị trùng")
	}
	if _, ok := Reason("  ngắn  "); ok {
		t.Error("lý do ngắn")
	}
}

func TestPeriodStateAndResubmit(t *testing.T) {
	t0 := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	st := StateOf([]Event{{Action: "published"}})
	if st.Status != PeriodOpen || !st.AcceptsUnit("U1") {
		t.Fatalf("mở: %+v", st)
	}
	st = StateOf([]Event{{Action: "published"}, {Action: "closed"}})
	if st.Status != PeriodClosed || st.AcceptsUnit("U1") || st.Closes != 1 {
		t.Fatalf("chốt: %+v", st)
	}
	st = StateOf([]Event{{Action: "published"}, {Action: "closed"}, {Action: "reopened", Units: []string{"U2"}, At: t0.Add(time.Hour)}})
	if st.Status != PeriodReopened || st.AcceptsUnit("U1") || !st.AcceptsUnit("U2") {
		t.Fatalf("mở lại một phần: %+v", st)
	}
	acceptedBefore, acceptedAfter := t0, t0.Add(2*time.Hour)
	if !CanResubmit(SubAccepted, &acceptedBefore, st) || CanResubmit(SubAccepted, &acceptedAfter, st) {
		t.Fatal("nộp lại sau chấp nhận chỉ khi kỳ mở lại SAU lúc chấp nhận")
	}
	if CanResubmit(SubAccepted, &acceptedBefore, StateOf([]Event{{Action: "published"}})) || !CanResubmit(SubReturned, nil, st) {
		t.Fatal("trạng thái nộp lại")
	}
	if SubmissionStatus(false, nil) != SubNone || SubmissionStatus(true, nil) != SubSubmitted ||
		SubmissionStatus(true, &Review{Decision: SubReturned}) != SubReturned {
		t.Fatal("trạng thái bài nộp")
	}
	if p := PendingReview([]UnitSnapshot{{Unit: "U1", Status: SubSubmitted}, {Unit: "U2", Status: SubAccepted}}); len(p) != 1 || p[0] != "U1" {
		t.Fatalf("chờ rà soát: %v", p)
	}
}
