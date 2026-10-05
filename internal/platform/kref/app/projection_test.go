package app

import (
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/kref/port"
)

func org(ref, parent uuid.UUID) port.Org {
	return port.Org{Ref: ref, Kind: "extension_team", Code: ref.String()[:8], ParentRef: parent, Active: true, Seq: 1}
}

func TestClosure(t *testing.T) {
	a, b, c, d := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	got, err := Closure([]port.Org{org(c, b), org(a, uuid.Nil), org(b, a), org(d, a)})
	if err != nil {
		t.Fatal(err)
	}
	want := []ClosureRow{{a, a, 0}, {a, b, 1}, {a, c, 2}, {a, d, 1}, {b, b, 0}, {b, c, 1}, {c, c, 0}, {d, d, 0}}
	sortClosure(want)
	if !slices.Equal(got, want) {
		t.Fatalf("closure:\n%v\nmuốn\n%v", got, want)
	}
}

func TestClosureRejectsBrokenTrees(t *testing.T) {
	a, b, missing := uuid.New(), uuid.New(), uuid.New()
	if _, err := Closure([]port.Org{org(a, missing)}); err == nil {
		t.Fatal("cha không thuộc cây phải lỗi")
	}
	if _, err := Closure([]port.Org{org(a, b), org(b, a)}); err == nil {
		t.Fatal("vòng phải lỗi")
	}
}

// Băm theo quan hệ không phụ thuộc thứ tự, seq hay múi giờ; khác nội dung → khác băm.
func TestDigestsIgnoreOrderAndSeq(t *testing.T) {
	a, b := uuid.New(), uuid.New()
	to := time.Date(2025, 7, 1, 0, 0, 0, 0, time.UTC)
	units := []port.AdminUnit{
		{Code: "92", ValidFrom: time.Date(2025, 7, 1, 0, 0, 0, 0, time.UTC), Name: "Cần Thơ", Level: "province"},
		{Code: "31135", ValidFrom: time.Date(2020, 1, 1, 7, 0, 0, 0, time.FixedZone("ICT", 7*3600)), ValidTo: &to, Name: "Ninh Kiều",
			Level: "commune", ParentCode: "92", SuccessorCodes: []string{"b", "a"}},
	}
	g := port.ShareGrant{GrantID: uuid.New(), Grantee: uuid.New(), Kinds: []string{"plot_directory", "cooperative_directory"},
		ValidFrom: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), ValidTo: time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC), Seq: 2}
	p1, err := Project([]port.Org{org(a, uuid.Nil), org(b, a)}, units, []port.ShareGrant{g})
	if err != nil {
		t.Fatal(err)
	}
	ob := org(b, a)
	ob.Seq = 9
	g2 := g
	g2.Seq, g2.Kinds = 7, []string{"cooperative_directory", "plot_directory"}
	u2 := slices.Clone(units)
	slices.Reverse(u2)
	p2, err := Project([]port.Org{ob, org(a, uuid.Nil)}, u2, []port.ShareGrant{g2})
	if err != nil {
		t.Fatal(err)
	}
	if d := Diff(p1, p2); len(d) != 0 {
		t.Fatalf("cùng nội dung phải cùng băm, lệch %v", d)
	}
	if p1.Digests()[port.RelNodeClosure].Rows != 3 || p1.Digests()[port.RelShareScope].Rows != 0 {
		t.Fatalf("số dòng: %v", p1.Digests())
	}
	ob.Active = false
	p3, _ := Project([]port.Org{ob, org(a, uuid.Nil)}, u2, []port.ShareGrant{g2})
	if d := Diff(p1, p3); !slices.Equal(d, []string{port.RelOrg}) {
		t.Fatalf("đổi trạng thái chỉ lệch org: %v", d)
	}
	p4, _ := Project([]port.Org{org(b, uuid.Nil), org(a, uuid.Nil)}, units, nil)
	if d := Diff(p1, p4); !slices.Equal(d, []string{port.RelOrg, port.RelNodeClosure, port.RelShareGrantActive}) {
		t.Fatalf("đổi cha + bỏ cấp phép: %v", d)
	}
}

func TestAdminUnitPayload(t *testing.T) {
	vt := "2025-07-01"
	pc := "92"
	u, err := adminUnitChangedV1{Code: "31135", Name: "Ninh Kiều", Level: "commune", ParentCode: &pc, ValidFrom: "2020-01-01", ValidTo: &vt,
		SuccessorCodes: []string{"y", "x"}}.unit()
	if err != nil || u.ValidTo == nil || u.ValidTo.Format(time.DateOnly) != vt || u.ParentCode != "92" || !slices.Equal(u.SuccessorCodes, []string{"x", "y"}) {
		t.Fatalf("%+v %v", u, err)
	}
	if _, err := (adminUnitChangedV1{Code: "1", Level: "commune", ValidFrom: "01/01/2020"}).unit(); err == nil {
		t.Fatal("ngày sai dạng phải lỗi")
	}
}
