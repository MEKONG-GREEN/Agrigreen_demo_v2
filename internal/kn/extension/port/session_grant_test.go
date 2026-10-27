package port

import (
	"bytes"
	"slices"
	"testing"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
)

// #71 KN-GRANT-OWNER: phạm vi quyền FN-14 trong payload phiên chủ KN.

func TestCNTSFormOfTemplate(t *testing.T) {
	cases := map[string]string{
		"CNTS-BM01": "BM-01", "CNTS-BM04": "BM-04", "CNTS-BM05-I": "BM-05", "CNTS-BM05-II": "BM-05", "CNTS-BM17": "BM-17",
		"cnts-bm01": "", "CNTS-BM1": "", "CNTS-BM011": "", "CNTS-BM20": "", "CNTS-BM00": "", "BM-01": "", "CNTS-BM01X": "", "": "",
	}
	for code, want := range cases {
		got, ok := CNTSFormOfTemplate(code)
		if got != want || ok != (want != "") {
			t.Fatalf("%q: muốn %q, nhận %q %v", code, want, got, ok)
		}
	}
}

func grantCtx(g *CNTSGrantScope) SessionContext {
	return SessionContext{ID: uuid.New(), Role: SessionRoleCommune, Department: "chan_nuoi_thuy_san", Communes: []string{"30001", "30002"},
		Scope: authz.Scope{Units: []string{"30001", "30002"}}, Facilities: []uuid.UUID{legacyFacility}, CNTSGrant: g}
}

var (
	legacyFacility = uuid.MustParse("0192a000-0000-7000-8000-0000000000aa")
	f1             = uuid.MustParse("0192a000-0000-7000-8000-000000000001")
	f2             = uuid.MustParse("0192a000-0000-7000-8000-000000000002")
	h1             = uuid.MustParse("0192a000-0000-7000-8000-000000000011")
	h2             = uuid.MustParse("0192a000-0000-7000-8000-000000000012")
)

func TestCNTSGrantNilKeepsLegacy(t *testing.T) {
	k := grantCtx(nil)
	if !k.AllowsCNTSForm("CNTS-BM04") || !k.AllowsCNTSForm("OTHER") {
		t.Fatal("chưa từng được cấp quyền: giữ biểu như cũ")
	}
	if !k.AllowsCNTSFacility(legacyFacility) || k.AllowsCNTSFacility(f1) {
		t.Fatal("chưa từng được cấp quyền: giữ giới hạn cơ sở W11.07")
	}
	if s := k.CNTSScope(); !slices.Equal(s.Units, k.Scope.Units) || s.All {
		t.Fatalf("phạm vi legacy đổi: %v", s)
	}
}

func TestCNTSGrantExplicitEmptyDenies(t *testing.T) {
	k := grantCtx(&CNTSGrantScope{})
	if k.AllowsCNTSForm("CNTS-BM01") || k.AllowsCNTSForm("OTHER") {
		t.Fatal("phạm vi tường minh rỗng phải chặn biểu")
	}
	if k.AllowsCNTSFacility(legacyFacility) || k.AllowsCNTSHousehold(h1) {
		t.Fatal("phạm vi tường minh rỗng không quay về thuộc tính W11.07")
	}
	if s := k.CNTSScope(); !s.Empty() {
		t.Fatalf("phạm vi tường minh rỗng phải rỗng: %v", s)
	}
}

func TestCNTSGrantScopeNarrowsOnly(t *testing.T) {
	k := grantCtx(&CNTSGrantScope{Grants: []CNTSGrant{
		{Ref: uuid.New(), Seq: 1, Role: "ksgm", Communes: []string{"30001"}, Facilities: []uuid.UUID{f1}, Forms: []string{"BM-01"}},
		{Ref: uuid.New(), Seq: 3, Role: "theo_doi_mo_hinh", Communes: []string{"30001", "99999"}, Households: []uuid.UUID{h1}, Forms: []string{"BM-08"}},
	}, Households: []uuid.UUID{h1}})
	if !k.AllowsCNTSForm("CNTS-BM01") || k.AllowsCNTSForm("CNTS-BM04") || k.AllowsCNTSForm("CNTS-BM09") || !k.AllowsCNTSForm("CNTS-BM08-A") {
		t.Fatal("biểu được giao sai")
	}
	if !k.AllowsCNTSFacility(f1) || k.AllowsCNTSFacility(f2) || k.AllowsCNTSFacility(legacyFacility) {
		t.Fatal("BR-02: cơ sở KSGM chỉ từ quyền FN-14")
	}
	if !k.AllowsCNTSHousehold(h1) || k.AllowsCNTSHousehold(h2) {
		t.Fatal("hộ được bao sai")
	}
	if s := k.CNTSScope(); s.All || !slices.Equal(s.Units, []string{"30001"}) {
		t.Fatalf("phạm vi chỉ thu hẹp trong xã của phiên: %v", s)
	}
	adm := k
	adm.Role = SessionRoleProvince
	if !adm.AllowsCNTSForm("CNTS-BM04") {
		t.Fatal("chỉ Cấp Xã mang phạm vi quyền FN-14")
	}
}

func TestCNTSGrantPayloadRoundTripAndStamp(t *testing.T) {
	g := &CNTSGrantScope{Grants: []CNTSGrant{{Ref: uuid.New(), Seq: 1, Role: "ksgm", Communes: []string{"30001"}, Facilities: []uuid.UUID{f1},
		Forms: []string{"BM-01"}, ValidFrom: "2027-10-13", ValidTo: "2027-10-13"}}}
	k := grantCtx(g)
	s, err := NewSessionContext(k)
	if err != nil {
		t.Fatal(err)
	}
	got, err := ReadSessionContext(authz.Principal{SelectedSession: &s})
	if err != nil || got.CNTSGrant == nil || len(got.CNTSGrant.Grants) != 1 || got.CNTSGrant.Grants[0].Facilities[0] != f1 {
		t.Fatalf("payload mất phạm vi quyền: %v %+v", err, got.CNTSGrant)
	}
	again, _ := NewSessionContext(k)
	a, _ := s.AuthorizationStamp()
	b, _ := again.AuthorizationStamp()
	if !bytes.Equal(a, b) {
		t.Fatal("dấu quyền phải ổn định khi quyền không đổi")
	}
	changed := k
	changed.CNTSGrant = &CNTSGrantScope{Grants: slices.Clone(g.Grants)}
	changed.CNTSGrant.Grants[0].Seq = 2
	c, _ := NewSessionContext(changed)
	stamp, _ := c.AuthorizationStamp()
	if bytes.Equal(a, stamp) {
		t.Fatal("sửa quyền phải đổi dấu quyền (idempotency, đồng bộ)")
	}
	legacy := k
	legacy.CNTSGrant = nil
	l, _ := NewSessionContext(legacy)
	ls, _ := l.AuthorizationStamp()
	if bytes.Equal(a, ls) {
		t.Fatal("có/không có phạm vi tường minh phải khác dấu quyền")
	}
}
