package handler_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

// BF-ACC-REPLAY-SCOPE (#154, vòng 2 — Codex P2): phát lại theo sổ của các thao tác PUT/PATCH (không qua middleware
// cache HTTP, chỉ POST) và gán/sửa/thu hồi vai KN phải khớp CẢ người gọi lẫn nội dung yêu cầu; khác → 422
// idempotency_key_reused. Cùng người gọi, cùng nội dung → 200, không ghi thêm sổ.

const reused = "idempotency_key_reused"

func (f *knFix) withKey(c caller, method, path, idem string, body any) resp {
	return f.req(f.admin2, c, method, path, body, map[string]string{"Idempotency-Key": idem})
}

// expireKey cho dòng cache idempotency của khóa (nếu còn) hết hạn; sổ giữ nguyên.
func (f *knFix) expireKey(idem string) {
	f.t.Helper()
	if _, err := f.env.AdminPool(f.t, migrations.PF).Exec(context.Background(),
		"UPDATE shared.idempotency_key SET expires_at = now() - interval '1 hour' WHERE tenant_id=$1 AND idem_key=$2", f.tenant, idem); err != nil {
		f.t.Fatal(err)
	}
}

// countRows đếm dòng sổ (bỏ qua RLS) theo một cột tham chiếu.
func (f *knFix) countRows(table, col string, ref uuid.UUID) int {
	f.t.Helper()
	var n int
	if err := f.env.AdminPool(f.t, migrations.PF).QueryRow(context.Background(),
		"SELECT count(*) FROM pf_accounts."+table+" WHERE "+col+" = $1", ref).Scan(&n); err != nil {
		f.t.Fatal(err)
	}
	return n
}

// Đúng repro của Codex: PUT attributes với khóa K.
func TestBFAccReplayScope_AttrsReplayRequiresSameActorAndBody(t *testing.T) {
	f := newKNFixture(t)
	staffID, _, cs := f.createKN("Phạm Văn Bình", asg(port.KNTinh, f.p91, []string{deptTT}))
	aid := asgIDs(cs)[port.KNTinh]
	path := "/v1/accounts/users/" + staffID.String() + "/assignments/" + aid + "/attributes"
	body := map[string]any{"province_group": "tong_hop", "reason": "phân quyền ban đầu"}
	idem := key()
	if r := f.withKey(f.root, http.MethodPut, path, idem, body); r.status != http.StatusOK {
		t.Fatalf("đặt thuộc tính: %d %v", r.status, r.body)
	}
	events := f.countRows("assignment_attr_event", "assignment_id", uuid.MustParse(aid))
	// Đối chứng: account admin có quyền hiện hành trên tài khoản (khóa mới, cùng thân → 200, không đổi).
	if r := f.withKey(f.accAdmin, http.MethodPut, path, key(), body); r.status != http.StatusOK {
		t.Fatalf("đối chứng account admin khóa mới: %d %v", r.status, r.body)
	}
	wantCode(t, f.withKey(f.accAdmin, http.MethodPut, path, idem, body), http.StatusUnprocessableEntity, reused, "actor khác dùng lại khóa")
	changed := map[string]any{"province_group": "lanh_dao_phong", "reason": "phân quyền ban đầu"}
	wantCode(t, f.withKey(f.root, http.MethodPut, path, idem, changed), http.StatusUnprocessableEntity, reused, "cùng actor đổi nhóm")
	wantCode(t, f.withKey(f.root, http.MethodPut, path, idem, map[string]any{"province_group": "tong_hop", "reason": "lý do khác hẳn"}),
		http.StatusUnprocessableEntity, reused, "cùng actor đổi lý do")
	r := f.withKey(f.root, http.MethodPut, path, idem, body)
	if r.status != http.StatusOK || str(r.body, "attributes", "province_group") != "tong_hop" {
		t.Fatalf("phát lại hợp lệ: %d %v", r.status, r.body)
	}
	if n := f.countRows("assignment_attr_event", "assignment_id", uuid.MustParse(aid)); n != events {
		t.Fatalf("phát lại không được ghi thêm sổ: %d → %d", events, n)
	}
}

// PUT sửa vai trò (không qua cache HTTP).
func TestBFAccReplayScope_ChangeAssignmentReplayRequiresSameActorAndBody(t *testing.T) {
	f := newKNFixture(t)
	anID, _, cr := f.createKN("Nguyễn Văn An", asg(port.KNXa, f.p91, []string{deptTT}, "30292"))
	aid := asgIDs(cr)[port.KNXa]
	path := "/v1/accounts/users/" + anID.String() + "/assignments/" + aid
	ch := asg(port.KNXa, f.p91, []string{deptTT}, "30292", "30301")
	idem := key()
	if r := f.withKey(f.root, http.MethodPut, path, idem, ch); r.status != http.StatusOK {
		t.Fatalf("sửa: %d %v", r.status, r.body)
	}
	events := f.countRows("role_assignment_event", "assignment_id", uuid.MustParse(aid))
	wantCode(t, f.withKey(f.accAdmin, http.MethodPut, path, key(), ch), http.StatusConflict, "", "đối chứng account admin khóa mới (không đổi)")
	wantCode(t, f.withKey(f.accAdmin, http.MethodPut, path, idem, ch), http.StatusUnprocessableEntity, reused, "actor khác dùng lại khóa")
	other := asg(port.KNXa, f.p91, []string{deptTT}, "30292")
	wantCode(t, f.withKey(f.root, http.MethodPut, path, idem, other), http.StatusUnprocessableEntity, reused, "cùng actor đổi phạm vi")
	if r := f.withKey(f.root, http.MethodPut, path, idem, ch); r.status != http.StatusOK {
		t.Fatalf("phát lại hợp lệ: %d %v", r.status, r.body)
	}
	if n := f.countRows("role_assignment_event", "assignment_id", uuid.MustParse(aid)); n != events {
		t.Fatalf("phát lại không được ghi thêm sổ: %d → %d", events, n)
	}
}

// POST gán/thu hồi vai trò: sau khi cache HTTP hết hạn, phát lại theo sổ vẫn phải khớp nội dung.
func TestBFAccReplayScope_GrantRevokeReplayAfterCacheExpiryRequiresSameBody(t *testing.T) {
	f := newKNFixture(t)
	anID, _, _ := f.createKN("Nguyễn Văn An", asg(port.KNXa, f.p91, []string{deptTT}, "30292"))
	base := "/v1/accounts/users/" + anID.String() + "/assignments"
	g := asg(port.KNTinh, f.p91, []string{deptTT})
	idem := key()
	first := f.withKey(f.root, http.MethodPost, base, idem, g)
	if first.status != http.StatusCreated {
		t.Fatalf("gán: %d %v", first.status, first.body)
	}
	gid := str(first.body, "assignment", "assignment_id")
	f.expireKey(idem)
	g2 := asg(port.KNTinh, f.p91, []string{deptTT})
	g2["mgmt_level"] = "truong_phong"
	wantCode(t, f.withKey(f.root, http.MethodPost, base, idem, g2), http.StatusUnprocessableEntity, reused, "gán: cùng khóa, khác thân")
	f.expireKey(idem)
	if r := f.withKey(f.root, http.MethodPost, base, idem, g); r.status/100 != 2 || str(r.body, "assignment", "assignment_id") != gid {
		t.Fatalf("gán: phát lại hợp lệ: %d %v", r.status, r.body)
	}

	rpath := base + "/" + gid + "/revoke"
	rk := key()
	if r := f.withKey(f.root, http.MethodPost, rpath, rk, map[string]any{"reason": "Gán nhầm vai trò Cấp Tỉnh"}); r.status != http.StatusOK {
		t.Fatalf("thu hồi: %d %v", r.status, r.body)
	}
	f.expireKey(rk)
	wantCode(t, f.withKey(f.root, http.MethodPost, rpath, rk, map[string]any{"reason": "Một lý do thu hồi khác"}),
		http.StatusUnprocessableEntity, reused, "thu hồi: cùng khóa, khác lý do")
	f.expireKey(rk)
	wantCode(t, f.withKey(f.accAdmin, http.MethodPost, rpath, rk, map[string]any{"reason": "Gán nhầm vai trò Cấp Tỉnh"}),
		http.StatusUnprocessableEntity, reused, "thu hồi: actor khác dùng lại khóa")
	f.expireKey(rk)
	if r := f.withKey(f.root, http.MethodPost, rpath, rk, map[string]any{"reason": "Gán nhầm vai trò Cấp Tỉnh"}); r.status != http.StatusOK {
		t.Fatalf("thu hồi: phát lại hợp lệ: %d %v", r.status, r.body)
	}
}

// PATCH hồ sơ: trước đây phát lại trả ngay dòng sổ, không kiểm actor, tài khoản, phạm vi hay nội dung.
func TestBFAccReplayScope_ProfileReplayRequiresSameActorTargetAndBody(t *testing.T) {
	f := newKNFixture(t)
	anID, _, _ := f.createKN("Nguyễn Văn An", asg(port.KNTinh, f.p91, []string{deptTT}))
	otherID, _, _ := f.createKN("Lê Thị Bông", asg(port.KNTinh, f.p91, []string{deptTT}))
	path := "/v1/accounts/users/" + anID.String() + "/profile"
	body := map[string]any{"full_name": "Nguyễn Văn Ân", "reason": "sửa dấu tên"}
	idem := key()
	if r := f.withKey(f.root, http.MethodPatch, path, idem, body); r.status != http.StatusOK {
		t.Fatalf("sửa hồ sơ: %d %v", r.status, r.body)
	}
	wantCode(t, f.withKey(f.accAdmin, http.MethodPatch, path, key(), body), http.StatusConflict, "", "đối chứng account admin khóa mới (không đổi)")
	wantCode(t, f.withKey(f.accAdmin, http.MethodPatch, path, idem, body), http.StatusUnprocessableEntity, reused, "actor khác dùng lại khóa")
	wantCode(t, f.withKey(f.root, http.MethodPatch, path, idem, map[string]any{"full_name": "Nguyễn Văn Ấn", "reason": "sửa dấu tên"}),
		http.StatusUnprocessableEntity, reused, "cùng actor đổi tên")
	wantCode(t, f.withKey(f.root, http.MethodPatch, "/v1/accounts/users/"+otherID.String()+"/profile", idem, body),
		http.StatusUnprocessableEntity, reused, "cùng actor, tài khoản khác")
	before := f.countRows("account_profile_change", "user_ref", anID)
	if r := f.withKey(f.root, http.MethodPatch, path, idem, body); r.status != http.StatusOK {
		t.Fatalf("phát lại hợp lệ: %d %v", r.status, r.body)
	}
	if n := f.countRows("account_profile_change", "user_ref", anID); n != before {
		t.Fatalf("phát lại không được ghi thêm sổ: %d → %d", before, n)
	}
}

// Thu hồi ủy quyền (Admin API): khóa phải do chính người gọi ghi, cùng lý do.
func TestBFAccReplayScope_DelegationRevokeReplayRequiresSameActor(t *testing.T) {
	f := newKNFixture(t)
	_, lead, cr := f.createKN("Trần Thị Lan", asgLevel(port.KNTinh, f.p91, []string{deptTT}, port.MgmtTruongPhong))
	staffID, _, _ := f.createKN("Phạm Văn Bình", asg(port.KNTinh, f.p91, []string{deptTT}))
	leadCtx := f.ctxID(lead, asgIDs(cr)[port.KNTinh], deptTT)
	d := f.withCtx(lead, http.MethodPost, "/v1/me/delegations", leadCtx, map[string]any{"grantee_ref": staffID.String(),
		"power": authz.PowerReopenPeriod, "valid_to": time.Now().Add(72 * time.Hour).UTC().Format(time.RFC3339), "reason": "đi công tác Hà Nội một tuần"})
	if d.status != http.StatusCreated {
		t.Fatalf("ủy quyền: %d %v", d.status, d.body)
	}
	path := "/v1/accounts/delegations/" + str(d.body, "delegation_id") + "/revoke"
	rb := map[string]any{"reason": "Admin thu hồi theo đề nghị"}
	idem := key()
	if r := f.withKey(f.root, http.MethodPost, path, idem, rb); r.status != http.StatusOK {
		t.Fatalf("Admin thu hồi: %d %v", r.status, r.body)
	}
	f.expireKey(idem)
	wantCode(t, f.withKey(f.accAdmin, http.MethodPost, path, idem, rb), http.StatusUnprocessableEntity, reused, "actor khác dùng lại khóa")
	f.expireKey(idem)
	wantCode(t, f.withKey(f.root, http.MethodPost, path, idem, map[string]any{"reason": "Một lý do thu hồi khác"}),
		http.StatusUnprocessableEntity, reused, "cùng actor khác lý do")
	f.expireKey(idem)
	if r := f.withKey(f.root, http.MethodPost, path, idem, rb); r.status != http.StatusOK {
		t.Fatalf("phát lại hợp lệ: %d %v", r.status, r.body)
	}
}
