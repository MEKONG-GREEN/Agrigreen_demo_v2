package handler_test

// #71 KN-GRANT-OWNER (BRD CNTS v3.7 FN-14 AC-01…AC-03, BR-02; W11.47): phiên chủ KN chiếu phạm vi quyền FN-14 hiện hành
// từ sổ cnts_grant trên PostgreSQL thật — cấp, sửa, thu hồi kèm người nhận việc, biên ngày (tính cả ngày cuối), cùng người
// nhiều lần gán, phòng/tenant khác, chỉ xem, hỗ trợ; closure của cap/forms (biểu, cơ sở, hộ, xã) đọc đúng payload đó.
// Phép chiếu chỉ đọc: không ghi sổ, không đổi người thao tác lịch sử.

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension"
	knport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	accport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/port"
	formsport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
)

type owned struct {
	p authz.Principal
	k knport.SessionContext
}

// project chiếu phiên chủ như middleware pf/accounts (ngữ cảnh đã giải ở server → registry của module).
func (f *fix) project(actor, tid uuid.UUID, kc authz.KNContext) owned {
	f.t.Helper()
	kc.ID, kc.OrgRef = ids.New(), ids.New()
	p := authz.Principal{ActorRef: actor, TenantID: tid, Roles: []string{"kn_user"}, MFA: true, KN: &kc}
	s, err := f.sessions.ResolveSelected(context.Background(), p)
	if err != nil {
		f.t.Fatalf("chiếu phiên chủ: %v", err)
	}
	p.SelectedSession = &s
	k, err := knport.ReadSessionContext(p)
	if err != nil {
		f.t.Fatalf("đọc payload chủ: %v", err)
	}
	return owned{p: p, k: k}
}

func (o owned) stamp(t *testing.T) []byte {
	t.Helper()
	s, err := o.p.SelectedSession.AuthorizationStamp()
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func (o owned) forms(t *testing.T) formsport.ResolvedActor {
	t.Helper()
	prof := extension.FormProfiles()[1]
	if prof.Name != "kn_cnts" {
		t.Fatalf("profile: %s", prof.Name)
	}
	a, used, err := prof.ResolveActor(context.Background(), o.p, prof)
	if err != nil || !used {
		t.Fatalf("forms actor: %v", err)
	}
	return a
}

func allowsTemplate(a formsport.ResolvedActor, code string) bool {
	return a.AllowsTemplate == nil || a.AllowsTemplate(code)
}

func TestCNTSGrantOwnerSession_71(t *testing.T) {
	f := newFix(t)
	tongHop := f.user(tinh("tong_hop"))
	chu := f.user(tinh("chu_nhiem"))
	officer := f.user(xa("30001"))
	heir := f.user(xa("30001"))
	f.xa.list = []accport.XaScope{{ActorRef: officer.ref, Communes: []string{"30001", "30002"}}, {ActorRef: heir.ref, Communes: []string{"30001"}}}
	fac2 := ids.New()

	// Chưa từng được cấp → nil (giữ W11.07), không có closure biểu.
	o := f.project(officer.ref, f.tenant, xa("30001"))
	if o.k.CNTSGrant != nil || o.forms(t).AllowsTemplate != nil {
		t.Fatalf("chưa có quyền mà có phạm vi tường minh: %+v", o.k.CNTSGrant)
	}

	// AC-01/AC-03, BR-02: quyền KSGM có hiệu lực ngay ở request kế tiếp; BM-01 chỉ cơ sở được cấp.
	g1 := want(t, tongHop.post(base+"grants", map[string]any{"grantee_ref": officer.ref.String(), "role": "ksgm",
		"commune_codes": []string{"30001"}, "facility_refs": []string{fac1.String()}, "forms": []string{"BM-01"}, "valid_to": "2027-10-13"}),
		201, "", "cấp KSGM")
	g1ref := str(g1, "ref")
	o = f.project(officer.ref, f.tenant, xa("30001"))
	if g := o.k.CNTSGrant; g == nil || len(g.Grants) != 1 || g.Grants[0].Ref.String() != g1ref || g.Grants[0].ValidTo != "2027-10-13" ||
		len(g.Grants[0].Facilities) != 1 || g.Grants[0].Facilities[0] != fac1 {
		t.Fatalf("phạm vi quyền: %+v", o.k.CNTSGrant)
	}
	a := o.forms(t)
	if !allowsTemplate(a, "CNTS-BM01") || allowsTemplate(a, "CNTS-BM04") || !a.AllowsTarget(formsport.UnitFacility, fac1) ||
		a.AllowsTarget(formsport.UnitFacility, fac2) || !a.Scope.Allows("30001") || a.Scope.Allows("30002") {
		t.Fatal("closure cap/forms không theo quyền FN-14")
	}
	before := o.stamp(t)
	rows := f.count(`SELECT count(*) FROM pf_extension.cnts_grant WHERE tenant_id = $1`, f.tenant)

	// Cùng người nhiều lần gán: phiên xã khác không nhận quyền của xã 30001; phòng khác, tenant khác không có phạm vi.
	if o2 := f.project(officer.ref, f.tenant, xa("30002")); o2.k.CNTSGrant != nil {
		t.Fatalf("lần gán khác nhận quyền: %+v", o2.k.CNTSGrant)
	}
	both := f.project(officer.ref, f.tenant, xa("30001", "30002"))
	if both.k.CNTSGrant == nil || both.forms(t).Scope.Allows("30002") {
		t.Fatal("quyền không thu hẹp xã của lần gán nhiều xã")
	}
	other := xa("30001")
	other.Department = "trong_trot"
	if o3 := f.project(officer.ref, f.tenant, other); o3.k.CNTSGrant != nil {
		t.Fatal("phòng khác nhận quyền CNTS")
	}
	if o4 := f.project(officer.ref, ids.New(), xa("30001")); o4.k.CNTSGrant != nil {
		t.Fatal("tenant khác thấy quyền")
	}
	view := xa("30001")
	view.ViewOnly = true
	if o5 := f.project(officer.ref, f.tenant, view); o5.k.CNTSGrant == nil || !o5.k.ReadOnly() || len(o5.k.CNTSGrant.Grants) != 1 {
		t.Fatal("ngữ cảnh chỉ xem phải vẫn thu hẹp theo quyền")
	}
	sup := xa("30001")
	sup.Support = &authz.KNSupport{SessionID: ids.New(), OnBehalfOf: officer.ref}
	if o6 := f.project(ids.New(), f.tenant, sup); o6.k.CNTSGrant == nil || len(o6.k.CNTSGrant.Grants) != 1 {
		t.Fatal("hỗ trợ tài khoản phải dùng quyền của tài khoản được hỗ trợ")
	}
	sup.Support = &authz.KNSupport{SessionID: ids.New()}
	if o7 := f.project(officer.ref, f.tenant, sup); o7.k.CNTSGrant != nil {
		t.Fatal("hỗ trợ không gắn tài khoản không có người được cấp")
	}

	// Biên ngày giờ Việt Nam, tính cả ngày cuối: 23:59 ngày 13 còn hiệu lực; 00:00 ngày 14 hết hạn → phạm vi rỗng, chặn.
	f.clk.Advance(13*time.Hour + 59*time.Minute)
	if o := f.project(officer.ref, f.tenant, xa("30001")); o.k.CNTSGrant == nil || len(o.k.CNTSGrant.Grants) != 1 {
		t.Fatal("ngày cuối hiệu lực phải còn quyền")
	}
	f.clk.Advance(time.Minute)
	o = f.project(officer.ref, f.tenant, xa("30001"))
	if o.k.CNTSGrant == nil || len(o.k.CNTSGrant.Grants) != 0 {
		t.Fatalf("hết hạn phải là phạm vi tường minh rỗng: %+v", o.k.CNTSGrant)
	}
	if a := o.forms(t); allowsTemplate(a, "CNTS-BM01") || a.AllowsTarget(formsport.UnitFacility, fac1) || !a.Scope.Empty() {
		t.Fatal("quyền hết hạn vẫn mở biểu/cơ sở")
	}
	f.clk.Advance(-14 * time.Hour)

	// Quyền chờ hiệu lực: tường minh nhưng chưa mở biểu; tới ngày hiệu lực thì mở.
	want(t, tongHop.post(base+"grants", map[string]any{"grantee_ref": heir.ref.String(), "role": "tang_cuong", "commune_codes": []string{"30001"},
		"forms": []string{"BM-05"}, "valid_from": "2027-10-14"}), 201, "", "cấp tăng cường từ ngày mai")
	if a := f.project(heir.ref, f.tenant, xa("30001")).forms(t); a.AllowsTemplate == nil || a.AllowsTemplate("CNTS-BM05-I") {
		t.Fatal("quyền chờ hiệu lực đã mở biểu")
	}
	f.clk.Advance(24 * time.Hour)
	if a := f.project(heir.ref, f.tenant, xa("30001")).forms(t); !allowsTemplate(a, "CNTS-BM05-I") || !allowsTemplate(a, "CNTS-BM05-II") {
		t.Fatal("tới ngày hiệu lực chưa mở biểu")
	}
	f.clk.Advance(-24 * time.Hour)

	// Sửa quyền đổi dấu quyền (idempotency không trả phản hồi theo quyền cũ) và mở thêm biểu.
	want(t, tongHop.post(base+"grants/"+g1ref, map[string]any{"base_version": 1, "commune_codes": []string{"30001"},
		"facility_refs": []string{fac1.String()}, "forms": []string{"BM-01", "BM-04"}, "valid_to": "2027-12-31"}), 200, "", "sửa quyền")
	o = f.project(officer.ref, f.tenant, xa("30001"))
	if bytes.Equal(before, o.stamp(t)) || !allowsTemplate(o.forms(t), "CNTS-BM04") {
		t.Fatal("sửa quyền không đổi dấu quyền/biểu")
	}
	same := f.project(officer.ref, f.tenant, xa("30001"))
	if len(same.k.CNTSGrant.Grants) != 1 || same.k.CNTSGrant.Grants[0].Seq != o.k.CNTSGrant.Grants[0].Seq {
		t.Fatal("phép chiếu không ổn định")
	}

	// AC-02: thu hồi kèm người nhận — người cũ mất ngay (tường minh rỗng), người nhận có cùng phạm vi từ hôm nay.
	heirBefore := f.project(heir.ref, f.tenant, xa("30001")).stamp(t)
	want(t, tongHop.post(base+"grants/"+g1ref+"/revoke", map[string]any{"base_version": 2, "reason": "Chuyển công tác",
		"successor_ref": heir.ref.String()}), 200, "", "thu hồi, chuyển việc")
	o = f.project(officer.ref, f.tenant, xa("30001"))
	if o.k.CNTSGrant == nil || len(o.k.CNTSGrant.Grants) != 0 || o.forms(t).AllowsTarget(formsport.UnitFacility, fac1) ||
		allowsTemplate(o.forms(t), "CNTS-BM01") {
		t.Fatal("người bị thu hồi còn phạm vi")
	}
	h := f.project(heir.ref, f.tenant, xa("30001"))
	if bytes.Equal(heirBefore, h.stamp(t)) || !h.forms(t).AllowsTarget(formsport.UnitFacility, fac1) || !allowsTemplate(h.forms(t), "CNTS-BM04") ||
		allowsTemplate(h.forms(t), "CNTS-BM05-I") {
		t.Fatalf("người nhận chưa có đúng phạm vi: %+v", h.k.CNTSGrant)
	}

	// Theo dõi mô hình: hộ giao qua điểm được bao; hộ đã rút không còn.
	prj := want(t, chu.post(base+"projects", map[string]any{"code": "da-71", "name": "Dự án 71", "kind": "knqg", "start_date": "2027-03-01"}),
		201, "", "dự án")
	site := want(t, chu.post(base+"demo-sites", map[string]any{"project_ref": str(prj, "ref"), "commune_code": "30001", "name": "Điểm 71",
		"model_type_code": "GA_TP", "officer_ref": officer.ref.String()}), 201, "", "điểm")
	hh := want(t, officer.post(base+"model-households", map[string]any{"site_ref": str(site, "ref"), "owner_name": "Lê Văn Tám",
		"owner_phone": "0911222333", "keeper_kind": "officer", "keeper_ref": officer.ref.String()}), 201, "", "hộ")
	hhRef := uuid.MustParse(str(hh, "ref"))
	want(t, chu.post(base+"grants", map[string]any{"grantee_ref": heir.ref.String(), "role": "theo_doi_mo_hinh", "commune_codes": []string{"30001"},
		"site_refs": []string{str(site, "ref")}, "forms": []string{"BM-08"}}), 201, "", "cấp theo dõi mô hình")
	h = f.project(heir.ref, f.tenant, xa("30001"))
	if a := h.forms(t); !a.AllowsTarget(formsport.UnitHousehold, hhRef) || a.AllowsTarget(formsport.UnitHousehold, ids.New()) ||
		!allowsTemplate(a, "CNTS-BM08") {
		t.Fatalf("hộ của điểm được giao: %+v", h.k.CNTSGrant)
	}
	want(t, officer.post(base+"model-households/"+hhRef.String()+"/withdraw", map[string]any{"base_version": 1, "reason": "Hộ ngừng nuôi"}),
		200, "", "rút hộ")
	if a := f.project(heir.ref, f.tenant, xa("30001")).forms(t); a.AllowsTarget(formsport.UnitHousehold, hhRef) {
		t.Fatal("hộ đã rút vẫn được bao")
	}

	// Phép chiếu chỉ đọc; người thao tác lịch sử của sổ giữ nguyên (không viết lại actor, không chuyển nháp).
	if n := f.count(`SELECT count(*) FROM pf_extension.cnts_grant WHERE tenant_id = $1`, f.tenant); n != rows+5 {
		t.Fatalf("số dòng sổ quyền: %d (trước %d)", n, rows)
	}
	if n := f.count(`SELECT count(*) FROM pf_extension.cnts_grant WHERE tenant_id = $1 AND actor_ref NOT IN ($2, $3)`, f.tenant,
		tongHop.ref, chu.ref); n != 0 {
		t.Fatalf("actor lịch sử bị đổi: %d", n)
	}
	if n := f.count(`SELECT count(*) FROM pf_extension.cnts_grant WHERE tenant_id = $1 AND grant_ref = $2 AND grantee_ref <> $3`, f.tenant,
		uuid.MustParse(g1ref), officer.ref); n != 0 {
		t.Fatal("thu hồi đổi người được cấp của quyền cũ")
	}
}
