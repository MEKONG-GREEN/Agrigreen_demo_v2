package domain

import (
	"errors"
	"testing"
	"time"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/port"
)

func TestProfileUnitKinds_W1105(t *testing.T) {
	base := port.Profile{Name: "x", Hierarchy: []string{"TINH", "PHONG"}, Roles: map[string]string{"tinh@ke_hoach": "TINH", "tinh": "PHONG"},
		PublishBy: "TINH", CloseBy: "TINH", DefineBy: []string{"admin"}, UnitKinds: []string{port.UnitDepartment}, MaxSubmissions: 3}
	if err := ValidateProfile(base); err != nil {
		t.Fatal(err)
	}
	if base.LevelOf("tinh", "ke_hoach") != "TINH" || base.LevelOf("tinh", "trong_trot") != "PHONG" || base.LevelOf("tinh", "") != "PHONG" {
		t.Fatal("LevelOf theo phòng ban")
	}
	if k := (port.Profile{}).Kinds(); len(k) != 1 || k[0] != port.UnitCommune {
		t.Fatalf("mặc định xã: %v", k)
	}
	for name, mut := range map[string]func(*port.Profile){
		"person chưa hỗ trợ": func(p *port.Profile) { p.UnitKinds = []string{port.UnitPerson} },
		"trùng loại":         func(p *port.Profile) { p.UnitKinds = []string{port.UnitCommune, port.UnitCommune} },
		"giới hạn > 20":      func(p *port.Profile) { p.MaxSubmissions = 21 },
		"phòng ban sai":      func(p *port.Profile) { p.Departments = []string{"Trong-Trot"} },
	} {
		p := base
		mut(&p)
		if ValidateProfile(p) == nil {
			t.Errorf("%s: muốn lỗi", name)
		}
	}
}

func TestValidateSpec_W1105(t *testing.T) {
	kinds := []string{port.UnitCommune, port.UnitFacility, port.UnitDepartment}
	d := func(day int) *time.Time { x := time.Date(2026, 10, day, 0, 0, 0, 0, time.UTC); return &x }
	ok := []struct {
		units []string
		s     Spec
	}{
		{[]string{"X1"}, Spec{UnitKind: port.UnitCommune}},
		{[]string{"CS1", "CS2"}, Spec{UnitKind: port.UnitFacility, UnitParents: []string{"X1", "X1"}, MaxSubmissions: 3}},
		{[]string{"trong_trot"}, Spec{UnitKind: port.UnitDepartment, PeriodKind: "week", PeriodKey: "2026-W41", From: d(7), To: d(13)}},
	}
	for i, c := range ok {
		if err := ValidateSpec(kinds, c.units, c.s); err != nil {
			t.Errorf("ok %d: %v", i, err)
		}
	}
	bad := []struct {
		units []string
		s     Spec
		field string
	}{
		{[]string{"H1"}, Spec{UnitKind: port.UnitHousehold, UnitParents: []string{"X1"}}, "unit_kind"},
		{[]string{"CS1", "CS2"}, Spec{UnitKind: port.UnitFacility, UnitParents: []string{"X1"}}, "unit_parents"},
		{[]string{"X1"}, Spec{UnitKind: port.UnitCommune, UnitParents: []string{"X0"}}, "unit_parents"},
		{[]string{"TT"}, Spec{UnitKind: port.UnitDepartment}, "units"},
		{[]string{"X1"}, Spec{UnitKind: port.UnitCommune, MaxSubmissions: 21}, "max_submissions"},
		{[]string{"X1"}, Spec{UnitKind: port.UnitCommune, PeriodKey: "2026-W41"}, "period.key"},
		{[]string{"X1"}, Spec{UnitKind: port.UnitCommune, PeriodKind: "fortnight"}, "period.kind"},
		{[]string{"X1"}, Spec{UnitKind: port.UnitCommune, From: d(13), To: d(7)}, "period.to"},
	}
	for i, c := range bad {
		err := ValidateSpec(kinds, c.units, c.s)
		var te ErrTemplate
		if !errors.As(err, &te) || te.Field != c.field {
			t.Errorf("bad %d: muốn %s, có %v", i, c.field, err)
		}
	}
	s := Spec{UnitKind: port.UnitFacility, UnitParents: []string{"X1", "X2"}, MaxSubmissions: 3}
	if s.Parent([]string{"CS1", "CS2"}, "CS2") != "X2" || DefaultSpec().Parent([]string{"X1"}, "X1") != "X1" {
		t.Fatal("Parent")
	}
	if s.LimitReached(2) || !s.LimitReached(3) || DefaultSpec().LimitReached(100) {
		t.Fatal("LimitReached")
	}
}

func TestClosedStatuses_W1105(t *testing.T) {
	t0 := time.Date(2026, 10, 10, 0, 0, 0, 0, time.UTC)
	ev := func(action string, at time.Time, units ...string) Event {
		return Event{Action: action, At: at, Units: units}
	}
	open := StateOf([]Event{ev("published", t0)})
	closed := StateOf([]Event{ev("published", t0), ev("closed", t0.Add(time.Hour))})
	partial := StateOf([]Event{ev("published", t0), ev("closed", t0.Add(time.Hour)), ev("reopened", t0.Add(2*time.Hour), "U3")})
	cancelled := StateOf([]Event{ev("published", t0), ev("cancelled", t0.Add(time.Hour))})
	if cancelled.Status != PeriodCancelled || cancelled.AcceptsUnit("U1") || cancelled.ClosedFor("U1") {
		t.Fatalf("kỳ hủy: %+v", cancelled)
	}
	before, after := t0.Add(30*time.Minute), t0.Add(90*time.Minute)
	cases := []struct {
		st     PeriodState
		base   string
		unit   string
		heldAt *time.Time
		want   string
	}{
		{open, SubNone, "U1", nil, SubNone},
		{open, SubReturned, "U1", &after, SubReturned},
		{closed, SubNone, "U1", nil, SubMissed},
		{closed, SubReturned, "U1", nil, SubClosedUnadjusted},
		{closed, SubAccepted, "U1", nil, SubAccepted},
		{closed, SubNone, "U1", &after, SubAwaitingReopen},
		{closed, SubNone, "U1", &before, SubMissed},
		{partial, SubNone, "U3", nil, SubNone},
		{partial, SubReturned, "U2", nil, SubClosedUnadjusted},
		{cancelled, SubNone, "U1", nil, SubNone},
	}
	for i, c := range cases {
		if got := UnitStatus(c.base, c.st, c.unit, c.heldAt); got != c.want {
			t.Errorf("%d: %s, muốn %s", i, got, c.want)
		}
	}
}

func TestNilValuesOverdue_W1142(t *testing.T) {
	fs := []Field{{Key: "a", Type: TypeInteger, NonNegative: true}, {Key: "b", Type: TypeDecimal}, {Key: "c", Type: TypeText}}
	if err := ValidateValues(fs, map[string]string{"a": "-1"}); err == nil {
		t.Fatal("số âm phải bị chặn")
	}
	out, err := NilValues(fs, map[string]string{"b": "0.00", "c": "x"})
	if err != nil || out["a"] != "0" || out["b"] != "0" || out["c"] != "x" {
		t.Fatalf("không phát sinh: %v %v", out, err)
	}
	if _, err := NilValues(fs, map[string]string{"a": "2"}); err == nil {
		t.Fatal("không phát sinh mà có số")
	}
	due := time.Date(2026, 10, 5, 2, 0, 0, 0, time.UTC)
	st := StateOf([]Event{{Action: "published"}})
	if s := DisplayStatus(SubNone, st, "u", nil, due, due.Add(time.Minute)); s != SubOverdue {
		t.Fatalf("Trễ hạn: %s", s)
	}
	if s := DisplayStatus(SubNone, st, "u", nil, due, due); s != SubNone {
		t.Fatalf("chưa quá hạn: %s", s)
	}
	if ClosedStatus(SubOverdue) != SubMissed {
		t.Fatal("Trễ hạn lúc chốt → Không nộp")
	}
}
