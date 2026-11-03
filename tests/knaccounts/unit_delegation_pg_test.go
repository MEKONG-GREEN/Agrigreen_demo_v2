package knaccounts_test

// KNSPLIT-2-ACCOUNTS-UNIT-DELEGATION-KN (#195): hợp đồng chủ KN cho đơn vị nội bộ, ủy quyền có hạn và phạm vi nhân sự
// (extension.UnitDelegationPolicy) so với hành vi accounts hiện hành trên PostgreSQL thật qua HTTP. accounts chưa dùng
// adapter (nối là #196), nên một middleware quan sát chạy NGAY SAU mod.Middleware() của Application API: nó gọi adapter
// với đúng context accounts sẽ nhận (phiên chủ đã giải ở server) rồi so quyết định với phản hồi HTTP của đường cũ —
// gồm phạm vi phòng/đơn vị, chỉ xem, thu hồi, hết hạn, phát lại trên ngữ cảnh tái dùng, mất lần gán và tenant khác.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension"
	knport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	accport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/port"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz/authztest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

const (
	probeHeader = "X-Test-Probe"
	probeDept   = "X-Test-Dept"
)

// probeRecord: quyết định của adapter chủ KN và phạm vi nhân sự của đường cũ, cùng một context request.
type probeRecord struct {
	actor                       uuid.UUID
	grant                       accport.DelegationGrantDecision
	grantHandled, revokeHandled bool
	peopleHandled               bool
	grantErr, revokeErr         error
	people                      accport.PeopleScopeDecision
	peopleErr, legacyPeopleErr  error
	legacyPeople                accport.KNPeopleScope
	can                         map[string]bool // SessionContext.Can của phiên chủ (nil = không có phiên)
}

type delegationProbe struct {
	mu     sync.Mutex
	policy accport.UnitDelegationPolicy
	f      *fixture
	recs   map[string]probeRecord
}

func (pr *delegationProbe) handler() gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.GetHeader(probeHeader)
		if id == "" {
			c.Next()
			return
		}
		ctx := c.Request.Context()
		var r probeRecord
		if p, ok := authz.FromContext(ctx); ok {
			r.actor = p.ActorRef
		}
		r.grant, r.grantHandled, r.grantErr = pr.policy.AuthorizeDelegationGrant(ctx, c.GetHeader(probeDept))
		r.revokeHandled, r.revokeErr = pr.policy.AuthorizeDelegationRevoke(ctx)
		r.people, r.peopleHandled, r.peopleErr = pr.policy.PeopleScope(ctx)
		r.legacyPeople, r.legacyPeopleErr = pr.f.mod.KNPeople().PeopleInScope(ctx)
		if _, k, err := knport.RequireSession(ctx); err == nil {
			r.can = map[string]bool{}
			for _, p := range pr.policy.DelegationDefinition().Powers {
				r.can[p.Code] = k.Can(p.Code)
			}
		}
		pr.mu.Lock()
		pr.recs[id] = r
		pr.mu.Unlock()
		c.Next()
	}
}

func (pr *delegationProbe) get(id string) (probeRecord, bool) {
	pr.mu.Lock()
	defer pr.mu.Unlock()
	r, ok := pr.recs[id]
	return r, ok
}

type delegFlow struct {
	t      *testing.T
	f      *fixture
	pr     *delegationProbe
	clk    *clock.Fixed
	root   caller
	policy accport.UnitDelegationPolicy
	phone  int
	checks int
	// idemReplay: lượt phát lại cùng khóa sau khi phiên đổi quyền.
	idemReplay bool
}

type knUser struct {
	id  uuid.UUID
	aid string
}

func (d *delegFlow) createUser(c asgCase) knUser {
	d.t.Helper()
	d.phone++
	r := d.root.do(http.MethodPost, "/v1/accounts/kn-users", map[string]any{"full_name": "Trần Văn Bình",
		"email": "deleg-" + ids.New().String()[24:] + "@vi-du.vn", "phone": fmt.Sprintf("0915%06d", d.phone), "assignments": []any{d.f.asgBody(c)}})
	if r.status != http.StatusCreated {
		d.t.Fatalf("create %v: %d %v", c, r.status, r.body)
	}
	list := r.body["assignments"].([]any)
	return knUser{id: uuid.MustParse(userID(r)), aid: list[0].(map[string]any)["assignment_id"].(string)}
}

func (d *delegFlow) setAttrs(u knUser, attrs map[string]any) {
	d.t.Helper()
	attrs["reason"] = "Phân công theo quyết định của Giám đốc"
	r := d.root.do(http.MethodPut, "/v1/accounts/users/"+u.id.String()+"/assignments/"+u.aid+"/attributes", attrs)
	if r.status != http.StatusOK {
		d.t.Fatalf("attrs %v: %d %v", attrs, r.status, r.body)
	}
}

// app gọi Application API: tenant tùy chọn (khác = tenant ngoài), ngữ cảnh, khóa idempotency và mã quan sát.
func (d *delegFlow) app(user, tid uuid.UUID, method, path, ctxID, idem, dept string, body any) (resp, string) {
	d.t.Helper()
	c := caller{f: d.f, token: d.f.iss.Sign(d.t, authztest.Token{Subject: user, TenantID: tid, Roles: []string{accport.RoleKNUser}})}
	saved := d.f.srv
	d.f.srv = d.f.app
	defer func() { d.f.srv = saved }()
	var b []byte
	if body != nil {
		b, _ = json.Marshal(body)
	}
	if idem == "" && method == http.MethodPost {
		idem = "k-" + ids.New().String()
	}
	probe := ids.New().String()
	hdr := map[string]string{probeHeader: probe, probeDept: dept}
	if ctxID != "" {
		hdr[authz.ContextHeader] = ctxID
	}
	return c.rawWith(method, path, idem, "application/json", b, hdr), probe
}

func (d *delegFlow) selectContext(u knUser, dept string) string {
	d.t.Helper()
	r, _ := d.app(u.id, d.f.tenant, http.MethodPost, "/v1/me/context", "", "", "", map[string]any{"assignment_id": u.aid, "department_code": dept})
	id, _ := r.body["context_id"].(string)
	if id == "" {
		d.t.Fatalf("select context %s: %d %v", dept, r.status, r.body)
	}
	return id
}

func problemOf(err error) (int, string) {
	var p *problem.Problem
	if errors.As(err, &p) {
		return p.Status, p.Code
	}
	return 0, ""
}

func codeOf(r resp) string { s, _ := r.body["code"].(string); return s }

// grant gửi POST /v1/me/delegations, so phản hồi cũ với quyết định adapter và trả phản hồi.
func (d *delegFlow) grant(name string, u knUser, ctxID, idem string, body map[string]any) resp {
	d.t.Helper()
	dept, _ := body["department_code"].(string)
	r, probe := d.app(u.id, d.f.tenant, http.MethodPost, "/v1/me/delegations", ctxID, idem, dept, body)
	rec, reached := d.pr.get(probe)
	d.checks++
	switch {
	case !reached:
		if r.status < 400 {
			d.t.Fatalf("%s: middleware passed without probe: %d", name, r.status)
		}
	case !rec.grantHandled:
		if rec.grantErr != nil || r.status != 428 {
			d.t.Fatalf("%s: unhandled (native) owner but legacy %d %v", name, r.status, r.body)
		}
	case rec.grantErr != nil:
		if st, code := problemOf(rec.grantErr); st != r.status || code != codeOf(r) {
			d.t.Fatalf("%s: owner error %d %s, legacy %d %v", name, st, code, r.status, r.body)
		}
	case !rec.grant.Allowed:
		// Phát lại sau khi phiên đổi quyền: lớp idempotency (khóa gắn phạm vi phiên) chặn trước service — vẫn là từ chối.
		if d.idemReplay && r.status == http.StatusUnprocessableEntity && codeOf(r) == "idempotency_key_reused" {
			break
		}
		if r.status != http.StatusForbidden || codeOf(r) != "accounts.delegation_forbidden" {
			d.t.Fatalf("%s: owner denied, legacy %d %v", name, r.status, r.body)
		}
	default:
		if codeOf(r) == "accounts.delegation_forbidden" || r.status == http.StatusForbidden || r.status == 428 {
			d.t.Fatalf("%s: owner allowed, legacy %d %v", name, r.status, r.body)
		}
		if r.status/100 == 2 && r.body["department_code"] != rec.grant.Department {
			d.t.Fatalf("%s: department %v != owner %s", name, r.body["department_code"], rec.grant.Department)
		}
		if rec.grant.UnitRef != d.f.p91 {
			d.t.Fatalf("%s: owner unit %s", name, rec.grant.UnitRef)
		}
	}
	return r
}

// revoke gửi POST /v1/me/delegations/{id}/revoke, so lỗi phiên của adapter với phản hồi cũ.
func (d *delegFlow) revoke(name string, u knUser, tid uuid.UUID, ctxID, id, idem string) resp {
	d.t.Helper()
	r, probe := d.app(u.id, tid, http.MethodPost, "/v1/me/delegations/"+id+"/revoke", ctxID, idem, "",
		map[string]any{"reason": "Thu hồi theo chỉ đạo của Giám đốc"})
	rec, reached := d.pr.get(probe)
	d.checks++
	switch {
	case !reached:
		if r.status < 400 {
			d.t.Fatalf("%s: middleware passed without probe: %d", name, r.status)
		}
	case !rec.revokeHandled:
		if rec.revokeErr != nil || r.status != 428 {
			d.t.Fatalf("%s: unhandled (native) owner but legacy %d %v", name, r.status, r.body)
		}
	case rec.revokeErr != nil:
		if st, code := problemOf(rec.revokeErr); st != r.status || code != codeOf(r) {
			d.t.Fatalf("%s: owner error %d %s, legacy %d %v", name, st, code, r.status, r.body)
		}
	default: // phiên hợp lệ: kết quả còn lại (200/404/409) là kiểm native của accounts
		if r.status == http.StatusForbidden || r.status == 428 {
			d.t.Fatalf("%s: owner accepted session, legacy %d %v", name, r.status, r.body)
		}
	}
	return r
}

// observe gọi GET /v1/me/context bằng ngữ cảnh (tái dùng), so phạm vi nhân sự và quyền ủy quyền của phiên chủ.
func (d *delegFlow) observe(name string, u knUser, ctxID, dept string) (probeRecord, resp) {
	d.t.Helper()
	r, probe := d.app(u.id, d.f.tenant, http.MethodGet, "/v1/me/context", ctxID, "", dept, nil)
	rec, ok := d.pr.get(probe)
	if !ok || r.status != http.StatusOK {
		d.t.Fatalf("%s: context %d %v", name, r.status, r.body)
	}
	d.checks++
	if (rec.peopleErr == nil) != (rec.legacyPeopleErr == nil) || !rec.peopleHandled {
		d.t.Fatalf("%s: people errors owner=%v legacy=%v", name, rec.peopleErr, rec.legacyPeopleErr)
	}
	lp, dec := rec.legacyPeople, rec.people
	switch dec.Mode {
	case accport.PeopleScopeAll:
		if !lp.All {
			d.t.Fatalf("%s: owner all, legacy %+v", name, lp)
		}
	case accport.PeopleScopeSelf:
		if lp.All || !slices.Equal(lp.Members, []uuid.UUID{rec.actor}) || len(lp.CommuneOfficers) != 0 {
			d.t.Fatalf("%s: owner self, legacy %+v", name, lp)
		}
	case accport.PeopleScopeInternalUnit:
		if lp.All || lp.InternalUnit != dec.InternalUnit || dec.UnitRef != d.f.p91 || dec.MemberRole != authz.KNTinh || dec.OfficerRole != authz.KNXa {
			d.t.Fatalf("%s: owner %+v, legacy %+v", name, dec, lp)
		}
	default:
		d.t.Fatalf("%s: owner mode %q", name, dec.Mode)
	}
	if lp.InternalUnit != dec.InternalUnit {
		d.t.Fatalf("%s: internal unit owner %q legacy %q", name, dec.InternalUnit, lp.InternalUnit)
	}
	// Quyền ủy quyền của phiên chủ = powers của ngữ cảnh cũ (lãnh đạo/Admin có sẵn mọi quyền).
	powers := sortedList(d.t, r.body["powers"])
	for code, can := range rec.can {
		if r.body["read_only"] == true && can {
			d.t.Fatalf("%s: read-only session can %s", name, code)
		}
		if !can && slices.Contains(powers, code) {
			d.t.Fatalf("%s: legacy power %s missing in owner session", name, code)
		}
	}
	return rec, r
}

func (d *delegFlow) delegBody(grantee knUser, dept, power string, validFor time.Duration) map[string]any {
	return map[string]any{"grantee_ref": grantee.id.String(), "department_code": dept, "power": power,
		"valid_to": d.clk.Now().Add(validFor).Format(time.RFC3339Nano), "reason": "Ủy quyền trong thời gian đi công tác"}
}

func runUnitDelegationFlow(t *testing.T, deps fixtureDeps) {
	policy, err := extension.UnitDelegationPolicy()
	if err != nil {
		t.Fatal(err)
	}
	clk := clock.NewFixed(time.Now())
	pr := &delegationProbe{policy: policy, recs: map[string]probeRecord{}}
	deps.Clock, deps.AppProbe = clk, pr.handler()
	f := newFixture(t, deps)
	pr.f = f
	d := &delegFlow{t: t, f: f, pr: pr, clk: clk, root: f.as(f.national, authz.RolePlatformAdmin), policy: policy}
	unitDef, delegDef := policy.InternalUnitDefinition(), policy.DelegationDefinition()

	// --- Định nghĩa: danh mục HTTP của đường cũ = dữ liệu chủ KN. ---
	cat := d.root.do(http.MethodGet, "/v1/accounts/mgmt-levels", nil)
	if cat.status != http.StatusOK || !sameCatalog(cat.body["powers"], delegDef.Powers) ||
		cat.body["delegation_max_days"] != float64(delegDef.MaxDuration/(24*time.Hour)) || delegDef.MaxDuration%(24*time.Hour) != 0 {
		t.Fatalf("delegation catalog %v vs %+v", cat.body, delegDef)
	}

	// --- Đơn vị nội bộ: seed, loại, phòng ban, loại Nút. ---
	// Nút quốc gia có đủ phòng ban của seed (so trọn mã phòng ban gắn kèm); tỉnh 91 thêm phòng HCTH (phạm vi BR-04).
	for _, code := range []string{partyport.DeptChanNuoiThuySan, partyport.DeptKeHoachTaiChinh, partyport.DeptThongTinTruyenThong, partyport.DeptHanhChinhTongHop} {
		f.depts.depts[f.national] = append(f.depts.depts[f.national], partyport.Department{ID: ids.New(), OrgRef: f.national, Code: code, Name: "Phòng " + code, Status: partyport.DeptActive})
	}
	f.depts.depts[f.p91] = append(f.depts.depts[f.p91], partyport.Department{ID: ids.New(), OrgRef: f.p91, Code: partyport.DeptHanhChinhTongHop, Name: "Phòng HCTH", Status: partyport.DeptActive})
	for _, org := range []uuid.UUID{f.p91, f.national} {
		r := d.root.do(http.MethodPost, "/v1/accounts/internal-units/seed", map[string]any{"org_ref": org.String()})
		created, _ := r.body["created"].([]any)
		if r.status != http.StatusOK || r.body["status"] != unitDef.SeedStatus || len(created) != len(unitDef.Seed) {
			t.Fatalf("seed %s: %d %v", org, r.status, r.body)
		}
		for i, item := range unitDef.Seed {
			got := created[i].(map[string]any)
			dept := item.DepartmentCode
			if _, err := f.depts.DepartmentByCode(context.Background(), org, dept); dept != "" && err != nil {
				dept = "" // phòng ban chưa khai ở đơn vị → không gắn, như cũ
			}
			if got["code"] != item.Code || got["kind"] != item.Kind || got["name"] != item.Name || got["sort_order"] != float64(item.SortOrder) ||
				(dept == "" && got["department_code"] != nil) || (dept != "" && got["department_code"] != dept) ||
				got["kind_label"] != accport.CatalogLabel(unitDef.Kinds, item.Kind) {
				t.Fatalf("seed item %s: %v", item.Code, got)
			}
		}
		again := d.root.do(http.MethodPost, "/v1/accounts/internal-units/seed", map[string]any{"org_ref": org.String()})
		if again.status != http.StatusOK || again.body["existing"] != float64(len(unitDef.Seed)) {
			t.Fatalf("reseed %s: %d %v", org, again.status, again.body)
		}
	}
	list := d.root.do(http.MethodGet, "/v1/accounts/internal-units?org_ref="+f.p91.String(), nil)
	if list.status != http.StatusOK || !sameCatalog(list.body["kinds"], unitDef.Kinds) {
		t.Fatalf("unit kinds %v", list.body["kinds"])
	}
	createUnit := func(org uuid.UUID, code, kind, dept string) resp {
		return d.root.do(http.MethodPost, "/v1/accounts/internal-units", map[string]any{"org_ref": org.String(), "code": code, "kind": kind,
			"name": "Đơn vị " + code, "department_code": dept, "sort_order": 70})
	}
	for i, k := range unitDef.Kinds {
		dept := ""
		if slices.Contains(unitDef.DepartmentKinds, k.Code) {
			dept = "bao_ve_thuc_vat"
		}
		if r := createUnit(f.p91, fmt.Sprintf("dv_%s_%d", k.Code, i), k.Code, dept); r.status != http.StatusCreated || r.body["kind_label"] != k.Label {
			t.Fatalf("owner kind %s rejected: %d %v", k.Code, r.status, r.body)
		}
		if !slices.Contains(unitDef.DepartmentKinds, k.Code) {
			if r := createUnit(f.p91, "dv_lech_"+k.Code, k.Code, "trong_trot"); r.status != http.StatusUnprocessableEntity {
				t.Fatalf("department on %s: %d %v", k.Code, r.status, r.body)
			}
		}
	}
	if r := createUnit(f.p91, "dv_kho", "kho", ""); r.status != http.StatusUnprocessableEntity || slices.ContainsFunc(unitDef.Kinds, func(c accport.CatalogItem) bool { return c.Code == "kho" }) {
		t.Fatalf("unknown kind: %d %v", r.status, r.body)
	}
	// Loại Nút: chỉ loại chủ khai báo mới có đơn vị nội bộ (HTX, tổ khuyến nông bị từ chối như cũ).
	for org, kind := range map[uuid.UUID]string{f.p91: "ttkn_province", f.national: "ttkn_national", f.coop30292: "cooperative", f.team30292: "extension_team"} {
		r := createUnit(org, "dv_nut_"+ids.New().String()[:6], "tram", "")
		if ok := slices.Contains(unitDef.OrgKinds, kind); ok != (r.status == http.StatusCreated) || (!ok && codeOf(r) != "accounts.internal_unit_org_invalid") {
			t.Fatalf("org kind %s (owner %v): %d %v", kind, ok, r.status, r.body)
		}
	}

	// --- Người dùng: tỉnh 91, phòng trồng trọt, đơn vị nội bộ trong_trot. ---
	mgr := d.createUser(asgCase{role: authz.KNTinh, org: "p91", depts: []string{"trong_trot"}, mgmt: "truong_phong"})
	staff := d.createUser(asgCase{role: authz.KNTinh, org: "p91", depts: []string{"trong_trot"}, mgmt: "chuyen_vien"})
	other := d.createUser(asgCase{role: authz.KNTinh, org: "p91", depts: []string{"bao_ve_thuc_vat"}, mgmt: "chuyen_vien"})
	lead := d.createUser(asgCase{role: authz.KNTinh, org: "p91", depts: []string{"trong_trot"}, mgmt: "lanh_dao_don_vi"})
	grp := d.createUser(asgCase{role: authz.KNTinh, org: "p91", depts: []string{"trong_trot"}, mgmt: "chuyen_vien"})
	tw := d.createUser(asgCase{role: authz.KNTW, org: "national", depts: []string{"trong_trot"}, mgmt: "giam_doc"})
	hcth := d.createUser(asgCase{role: authz.KNTinh, org: "p91", depts: []string{partyport.DeptHanhChinhTongHop}, mgmt: "chuyen_vien"})
	xa := d.createUser(asgCase{role: authz.KNXa, org: "p91", depts: []string{"trong_trot"}, communes: []string{"30292"}, mgmt: "chuyen_vien"})
	for _, u := range []knUser{mgr, staff, xa} {
		d.setAttrs(u, map[string]any{"internal_unit_code": "trong_trot"})
	}
	d.setAttrs(grp, map[string]any{"province_group": "lanh_dao_phong"})

	ctxMgr, ctxStaff, ctxOther := d.selectContext(mgr, "trong_trot"), d.selectContext(staff, "trong_trot"), d.selectContext(other, "bao_ve_thuc_vat")
	ctxLead, ctxLeadView := d.selectContext(lead, "trong_trot"), d.selectContext(lead, "bao_ve_thuc_vat")
	ctxHCTH := d.selectContext(hcth, partyport.DeptHanhChinhTongHop)
	ctxGrp, ctxTW, ctxXa := d.selectContext(grp, "trong_trot"), d.selectContext(tw, "trong_trot"), d.selectContext(xa, "trong_trot")

	// --- Phạm vi nhân sự (BR-04, HS-37) của từng phiên. ---
	if rec, _ := d.observe("people/mgr", mgr, ctxMgr, ""); rec.people.Mode != accport.PeopleScopeInternalUnit ||
		!sameRefs(rec.legacyPeople.Members, mgr.id, staff.id) || !sameRefs(rec.legacyPeople.CommuneOfficers, xa.id) {
		t.Fatalf("leader scope %+v / %+v", rec.people, rec.legacyPeople)
	}
	for name, c := range map[string]struct {
		u    knUser
		ctx  string
		mode accport.PeopleScopeMode
	}{
		"staff": {staff, ctxStaff, accport.PeopleScopeSelf}, "other": {other, ctxOther, accport.PeopleScopeSelf},
		"lead": {lead, ctxLead, accport.PeopleScopeAll}, "lead-view": {lead, ctxLeadView, accport.PeopleScopeAll},
		"hcth": {hcth, ctxHCTH, accport.PeopleScopeAll}, "grp-no-unit": {grp, ctxGrp, accport.PeopleScopeSelf}, "tw": {tw, ctxTW, accport.PeopleScopeSelf}, "xa": {xa, ctxXa, accport.PeopleScopeSelf},
	} {
		if rec, _ := d.observe("people/"+name, c.u, c.ctx, ""); rec.people.Mode != c.mode {
			t.Fatalf("people/%s: %q want %q", name, rec.people.Mode, c.mode)
		}
	}

	// --- Ủy quyền: phạm vi phòng/đơn vị, vai, chỉ xem, người nhận (native). ---
	approve, reopen := knport.SessionPowerApprove, knport.SessionPowerReopenPeriod
	idem1 := "k-" + ids.New().String()
	r := d.grant("grant/mgr", mgr, ctxMgr, idem1, d.delegBody(staff, "", approve, time.Hour))
	if r.status != http.StatusCreated {
		t.Fatalf("grant/mgr: %d %v", r.status, r.body)
	}
	d1 := r.body["delegation_id"].(string)
	if rr := d.grant("grant/mgr-replay", mgr, ctxMgr, idem1, d.delegBody(staff, "", approve, time.Hour)); rr.status/100 != 2 || rr.body["delegation_id"] != d1 {
		t.Fatalf("replay: %d %v", rr.status, rr.body)
	}
	d.grant("grant/mgr-explicit", mgr, ctxMgr, "", d.delegBody(staff, "trong_trot", reopen, time.Hour))
	for name, c := range map[string]struct {
		u      knUser
		ctx    string
		body   map[string]any
		status int
	}{
		"other-department": {mgr, ctxMgr, d.delegBody(other, "bao_ve_thuc_vat", approve, time.Hour), http.StatusForbidden},
		"grantee-other":    {mgr, ctxMgr, d.delegBody(other, "", approve, time.Hour), http.StatusUnprocessableEntity}, // người nhận: native
		"too-long":         {mgr, ctxMgr, d.delegBody(staff, "", approve, delegDef.MaxDuration+time.Minute), http.StatusUnprocessableEntity},
		"unknown-power":    {mgr, ctxMgr, d.delegBody(staff, "", "toan_quyen", time.Hour), http.StatusUnprocessableEntity},
		"staff":            {staff, ctxStaff, d.delegBody(mgr, "", approve, time.Hour), http.StatusForbidden},
		"lead":             {lead, ctxLead, d.delegBody(staff, "", reopen, time.Hour), http.StatusCreated},
		"lead-view":        {lead, ctxLeadView, d.delegBody(other, "", approve, time.Hour), http.StatusForbidden}, // chặn ở middleware
		"tw":               {tw, ctxTW, d.delegBody(staff, "", approve, time.Hour), http.StatusForbidden},
		"xa":               {xa, ctxXa, d.delegBody(staff, "", approve, time.Hour), http.StatusForbidden},
		"native":           {mgr, "", d.delegBody(staff, "", approve, time.Hour), 428},
	} {
		if rr := d.grant("grant/"+name, c.u, c.ctx, "", c.body); rr.status != c.status {
			t.Fatalf("grant/%s: %d %v", name, rr.status, rr.body)
		}
	}
	// Chỉ xem: adapter tự từ chối, không trông vào middleware.
	if rec, _ := d.observe("view/lead", lead, ctxLeadView, "bao_ve_thuc_vat"); rec.grant.Allowed || rec.grantErr != nil {
		t.Fatalf("read-only session allowed to grant: %+v %v", rec.grant, rec.grantErr)
	}
	if rec, _ := d.observe("view/lead-own", lead, ctxLeadView, ""); rec.grant.Allowed {
		t.Fatalf("read-only session allowed to grant in its department: %+v", rec.grant)
	}

	// --- Quyền nhận được trên ngữ cảnh tái dùng; thu hồi; phát lại thu hồi. ---
	if rec, ctxResp := d.observe("staff/powers", staff, ctxStaff, ""); !rec.can[approve] || !slices.Contains(sortedList(t, ctxResp.body["powers"]), approve) {
		t.Fatalf("delegated power missing: %v %v", rec.can, ctxResp.body["powers"])
	}
	if rr := d.revoke("revoke/staff", staff, f.tenant, ctxStaff, d1, ""); rr.status != http.StatusNotFound {
		t.Fatalf("grantee revoke: %d %v", rr.status, rr.body)
	}
	for name, c := range map[string]struct {
		u   knUser
		ctx string
		st  int
	}{"tw": {tw, ctxTW, http.StatusForbidden}, "xa": {xa, ctxXa, http.StatusForbidden}, "native": {mgr, "", 428}} {
		if rr := d.revoke("revoke/"+name, c.u, f.tenant, c.ctx, d1, ""); rr.status != c.st {
			t.Fatalf("revoke/%s: %d %v", name, rr.status, rr.body)
		}
	}
	idemR := "k-" + ids.New().String()
	if rr := d.revoke("revoke/mgr", mgr, f.tenant, ctxMgr, d1, idemR); rr.status != http.StatusOK || rr.body["status"] != "revoked" {
		t.Fatalf("revoke: %d %v", rr.status, rr.body)
	}
	if rr := d.revoke("revoke/mgr-replay", mgr, f.tenant, ctxMgr, d1, idemR); rr.status != http.StatusOK {
		t.Fatalf("revoke replay: %d %v", rr.status, rr.body)
	}
	if rr := d.revoke("revoke/mgr-again", mgr, f.tenant, ctxMgr, d1, ""); rr.status != http.StatusConflict {
		t.Fatalf("revoke twice: %d %v", rr.status, rr.body)
	}
	if rec, ctxResp := d.observe("staff/revoked", staff, ctxStaff, ""); rec.can[approve] || slices.Contains(sortedList(t, ctxResp.body["powers"]), approve) {
		t.Fatalf("revoked power still effective: %v %v", rec.can, ctxResp.body["powers"])
	}
	// Quyền reopen (mgr-explicit, lead) còn hiệu lực tới khi hết hạn.
	if rec, _ := d.observe("staff/reopen", staff, ctxStaff, ""); !rec.can[reopen] {
		t.Fatalf("reopen power missing: %v", rec.can)
	}
	clk.Advance(2 * time.Hour) // mọi ủy quyền 1 giờ hết hạn; ngữ cảnh (24 giờ) vẫn dùng lại được
	if rec, ctxResp := d.observe("staff/expired", staff, ctxStaff, ""); rec.can[reopen] || rec.can[approve] || len(sortedList(t, ctxResp.body["powers"])) != 0 {
		t.Fatalf("expired power still effective: %v %v", rec.can, ctxResp.body["powers"])
	}
	mine, _ := d.app(mgr.id, f.tenant, http.MethodGet, "/v1/me/delegations", ctxMgr, "", "", nil)
	for _, it := range mine.body["items"].([]any) {
		item := it.(map[string]any)
		if item["status"] == "expired" {
			if rr := d.revoke("revoke/expired", mgr, f.tenant, ctxMgr, item["delegation_id"].(string), ""); rr.status != http.StatusConflict {
				t.Fatalf("revoke expired: %d %v", rr.status, rr.body)
			}
			break
		}
	}

	// --- Phát lại sau khi mất quyền lãnh đạo, trên ngữ cảnh tái dùng. ---
	idemG := "k-" + ids.New().String()
	if rr := d.grant("grant/grp", grp, ctxGrp, idemG, d.delegBody(staff, "", approve, time.Hour)); rr.status != http.StatusCreated {
		t.Fatalf("group leader grant: %d %v", rr.status, rr.body)
	}
	d.setAttrs(grp, map[string]any{"province_group": "vien_chuc"})
	d.idemReplay = true
	if rr := d.grant("grant/grp-replay-demoted", grp, ctxGrp, idemG, d.delegBody(staff, "", approve, time.Hour)); rr.status/100 == 2 {
		t.Fatalf("replay after demotion succeeded: %d %v", rr.status, rr.body)
	}
	d.idemReplay = false
	if rr := d.grant("grant/grp-demoted", grp, ctxGrp, "", d.delegBody(staff, "", approve, time.Hour)); rr.status != http.StatusForbidden {
		t.Fatalf("demoted grant: %d %v", rr.status, rr.body)
	}
	if rec, _ := d.observe("grp/demoted", grp, ctxGrp, ""); rec.grant.Allowed {
		t.Fatalf("demoted session allowed: %+v", rec.grant)
	}

	// --- Tenant khác: ngữ cảnh không giải được, adapter không bao giờ thấy request. ---
	foreign := ids.New()
	if rr, probe := d.app(mgr.id, foreign, http.MethodPost, "/v1/me/delegations", ctxMgr, "", "", d.delegBody(staff, "", approve, time.Hour)); rr.status < 400 {
		t.Fatalf("foreign tenant: %d %v", rr.status, rr.body)
	} else if _, reached := pr.get(probe); reached {
		t.Fatal("foreign-tenant context reached the owner policy")
	}
	if rr := d.revoke("revoke/foreign-tenant", mgr, foreign, ctxMgr, d1, ""); rr.status < 400 {
		t.Fatalf("foreign tenant revoke: %d %v", rr.status, rr.body)
	}
	if rr := d.revoke("revoke/foreign-native", mgr, foreign, "", d1, ""); rr.status != 428 {
		t.Fatalf("foreign tenant native revoke: %d %v", rr.status, rr.body)
	}

	// --- Mất lần gán: ngữ cảnh tái dùng bị từ chối trước adapter, kể cả phát lại. ---
	rv := d.root.do(http.MethodPost, "/v1/accounts/users/"+mgr.id.String()+"/assignments/"+mgr.aid+"/revoke",
		map[string]any{"reason": "Điều chuyển công tác sang đơn vị khác"})
	if rv.status/100 != 2 {
		t.Fatalf("revoke assignment: %d %v", rv.status, rv.body)
	}
	for _, idem := range []string{idem1, ""} {
		if rr := d.grant("grant/mgr-lost-assignment", mgr, ctxMgr, idem, d.delegBody(staff, "", approve, time.Hour)); rr.status/100 == 2 {
			t.Fatalf("lost assignment (idem %q): %d %v", idem, rr.status, rr.body)
		}
	}
	if d.checks < 40 {
		t.Fatalf("matrix too small: %d checks", d.checks)
	}
}

func sameCatalog(v any, items []accport.CatalogItem) bool {
	list, ok := v.([]any)
	if !ok || len(list) != len(items) {
		return false
	}
	for i, it := range list {
		m := it.(map[string]any)
		if m["code"] != items[i].Code || m["label"] != items[i].Label {
			return false
		}
	}
	return true
}

func sameRefs(got []uuid.UUID, want ...uuid.UUID) bool {
	a, b := slices.Clone(got), slices.Clone(want)
	cmp := func(x, y uuid.UUID) int { return slices.Compare(x[:], y[:]) }
	slices.SortFunc(a, cmp)
	slices.SortFunc(b, cmp)
	return slices.Equal(a, b)
}

func TestKNUnitDelegationOwnerParityOnPostgres(t *testing.T) {
	t.Run("legacy-bridge", func(t *testing.T) { runUnitDelegationFlow(t, fixtureDeps{}) })
	t.Run("kn-owners", func(t *testing.T) {
		attrs, err := extension.AttributeCatalog()
		if err != nil {
			t.Fatal(err)
		}
		roles, err := extension.RoleCatalog(accport.NativeRoleCatalog())
		if err != nil {
			t.Fatal(err)
		}
		runUnitDelegationFlow(t, fixtureDeps{Attributes: attrs, Roles: roles, AssignmentRules: extension.AssignmentRules()})
	})
}
