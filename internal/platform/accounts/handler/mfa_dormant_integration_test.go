package handler_test

import (
	"context"
	"net/http"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

func mfaOf(t *testing.T, r resp) map[string]any {
	t.Helper()
	m, ok := r.body["mfa"].(map[string]any)
	if !ok {
		t.Fatalf("thiếu mfa: %v", r.body)
	}
	return m
}

// D24 (CEO 13/10/2026, W10.03): màn quản trị thấy trạng thái TOTP; quản trị viên đặt lại TOTP có lý do ≥ 20 ký tự —
// Keycloak xóa otp, thêm CONFIGURE_TOTP (đăng ký lại ở lần đăng nhập sau), thu hồi phiên; sổ 'totp_reset'.
func TestResetTOTPWithReason_D24(t *testing.T) {
	f := newFixture(t)
	nat := f.nationalAdmin()
	ctx := context.Background()
	id := nat.create(person(f.team30292, "kn_officer"))
	path := "/v1/accounts/users/" + id.String()

	r := nat.do(http.MethodGet, path, nil)
	if m := mfaOf(t, r); r.status != http.StatusOK || m["required"] != true || m["configured"] != false || m["enrollment_pending"] != true {
		t.Fatalf("trạng thái sau tạo: %d %v", r.status, r.body)
	}
	// Người dùng kích hoạt và đăng ký TOTP.
	f.dir.CompleteActions(id, port.ActionVerifyEmail, port.ActionUpdatePassword, port.ActionConfigureTOTP)
	f.dir.SetOTP(id, true)
	if m := mfaOf(t, nat.do(http.MethodGet, path, nil)); m["configured"] != true || m["enrollment_pending"] != false {
		t.Fatalf("đã đăng ký: %v", m)
	}

	if r := nat.do(http.MethodPost, path+"/reset-totp", map[string]any{"reason": "mất máy"}); r.status != http.StatusUnprocessableEntity {
		t.Fatalf("lý do ngắn: %d %v", r.status, r.body)
	}
	if r := nat.raw(http.MethodPost, path+"/reset-totp", "", "application/json", mustJSON(map[string]any{"reason": goodReason})); r.status != http.StatusBadRequest && r.status != http.StatusPreconditionRequired && r.status != http.StatusUnprocessableEntity {
		t.Fatalf("thiếu Idempotency-Key: %d %v", r.status, r.body)
	}
	self := f.nationalAdmin()
	if r := self.do(http.MethodPost, "/v1/accounts/users/"+self.actor.String()+"/reset-totp", map[string]any{"reason": goodReason}); r.status != http.StatusForbidden || r.body["code"] != "accounts.self_action" {
		t.Fatalf("tự đặt lại: %d %v", r.status, r.body)
	}
	// Quản trị tỉnh 92 không thao tác được cán bộ thuộc tỉnh 91.
	other := f.as(f.tenant, f.p92, port.RoleAccountAdminProvince)
	if r := other.do(http.MethodPost, path+"/reset-totp", map[string]any{"reason": goodReason}); r.status != http.StatusForbidden {
		t.Fatalf("ngoài phạm vi: %d %v", r.status, r.body)
	}
	k := key()
	reason := "Cán bộ báo mất điện thoại cài ứng dụng xác thực"
	r = nat.raw(http.MethodPost, path+"/reset-totp", k, "application/json", mustJSON(map[string]any{"reason": reason}))
	if r.status != http.StatusOK || r.body["replayed"] != false {
		t.Fatalf("đặt lại: %d %v", r.status, r.body)
	}
	if has, _ := f.dir.HasOTP(ctx, id); has {
		t.Fatal("Keycloak còn TOTP")
	}
	u, _ := f.dir.GetUser(ctx, id)
	if !slices.Contains(u.RequiredActions, port.ActionConfigureTOTP) || f.dir.Logouts(id) != 1 || !u.Enabled {
		t.Fatalf("Keycloak: actions=%v logouts=%d enabled=%v", u.RequiredActions, f.dir.Logouts(id), u.Enabled)
	}
	// Gọi lặp cùng khóa → cùng kết quả (cùng seq), không thu hồi phiên lần hai.
	if r2 := nat.raw(http.MethodPost, path+"/reset-totp", k, "application/json", mustJSON(map[string]any{"reason": reason})); r2.status != http.StatusOK || r2.body["seq"] != r.body["seq"] {
		t.Fatalf("phát lại: %d %v", r2.status, r2.body)
	}
	if f.dir.Logouts(id) != 1 {
		t.Fatal("phát lại không được thu hồi phiên lần nữa")
	}
	if got := f.events(f.tenant, id); !slices.Equal(got, []string{"created:", "actions_sent:", "totp_reset:"}) {
		t.Fatalf("sổ: %v", got)
	}
	if m := mfaOf(t, nat.do(http.MethodGet, path, nil)); m["configured"] != false || m["enrollment_pending"] != true {
		t.Fatalf("sau đặt lại: %v", m)
	}
	// Quản lý HTX không bắt TOTP và chưa cấu hình → không có gì để đặt lại.
	coop := nat.create(person(f.coop30292, "coop_manager"))
	if r := nat.do(http.MethodPost, "/v1/accounts/users/"+coop.String()+"/reset-totp", map[string]any{"reason": goodReason}); r.status != http.StatusConflict || r.body["code"] != "accounts.state_conflict" {
		t.Fatalf("HTX chưa có TOTP: %d %v", r.status, r.body)
	}
	if m := mfaOf(t, nat.do(http.MethodGet, "/v1/accounts/users/"+coop.String(), nil)); m["required"] != false {
		t.Fatalf("HTX không thuộc D24: %v", m)
	}
}

func dormantRun(t *testing.T, f *fixture) accountsDormant {
	t.Helper()
	r, err := f.mod.RunDormant(context.Background(), func(context.Context) ([]uuid.UUID, error) { return []uuid.UUID{f.tenant}, nil })
	if err != nil {
		t.Fatal(err)
	}
	return accountsDormant{r.Warned, r.Disabled}
}

type accountsDormant struct{ warned, disabled int }

// D24 (W10.05, v4.3 P-4): job accounts_dormant báo trước ngày 38, khóa ngày 45 (mặc định khi chưa có tham số), ghi
// sổ có kiểm toán (actor = tác nhân hệ thống, cause dormant_45d), báo trước một lần cho mỗi mốc hoạt động; đăng
// nhập hoặc Admin mở khóa đặt lại mốc.
func TestDormantJobWarnsAt38AndDisablesAt45_D24(t *testing.T) {
	t0 := time.Now().UTC().Add(-90 * 24 * time.Hour).Truncate(time.Second)
	clk := clock.NewFixed(t0)
	f := newFixtureAt(t, clk)
	f.dir.SetNow(clk.Now)
	ctx := context.Background()
	nat := f.nationalAdmin()
	a := nat.create(person(f.team30292, "kn_officer"))
	b := nat.create(person(f.team30292, "kn_officer"))
	c := nat.create(person(f.coop30292, "coop_manager"))
	enabled := func(id uuid.UUID) bool { u, _ := f.dir.GetUser(ctx, id); return u.Enabled }

	clk.Advance(20 * 24 * time.Hour)
	f.dir.RecordLogin(b, "10.0.0.1", "agrigreen-web-ops", true)

	clk.Advance(18*24*time.Hour + time.Hour) // a, c: 38 ngày; b: 18 ngày
	if got := dormantRun(t, f); got != (accountsDormant{warned: 2}) {
		t.Fatalf("ngày 38: %+v", got)
	}
	if got := dormantRun(t, f); got != (accountsDormant{}) {
		t.Fatalf("chạy lại cùng ngày không báo lần hai: %+v", got)
	}
	clk.Advance(3 * 24 * time.Hour)
	if got := dormantRun(t, f); got != (accountsDormant{}) {
		t.Fatalf("ngày 41 không báo lại cùng mốc: %+v", got)
	}

	clk.Advance(4 * 24 * time.Hour) // a, c: 45 ngày; b: 25 ngày
	if got := dormantRun(t, f); got != (accountsDormant{disabled: 2}) {
		t.Fatalf("ngày 45: %+v", got)
	}
	if enabled(a) || enabled(c) || !enabled(b) || f.dir.Logouts(a) != 1 {
		t.Fatalf("Keycloak: a=%v b=%v c=%v logouts(a)=%d", enabled(a), enabled(b), enabled(c), f.dir.Logouts(a))
	}
	if got := f.events(f.tenant, a); !slices.Equal(got, []string{"created:", "actions_sent:", "dormant_warned:", "locked:" + string(port.LockDormant)}) {
		t.Fatalf("sổ a: %v", got)
	}
	r := nat.do(http.MethodGet, "/v1/accounts/users/"+a.String(), nil)
	if str(r.body, "lock_cause") != string(port.LockDormant) {
		t.Fatalf("lock_cause: %v", r.body)
	}
	// Kiểm toán: tác nhân hệ thống, lý do cố định, mốc hoạt động trong chi tiết.
	var actor uuid.UUID
	var reason, last string
	if err := tenant.WithTenantTx(ctx, f.pool, f.tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT actor_ref, reason, details->>'last_activity_at' FROM pf_accounts.account_event
		  WHERE user_ref = $1 AND action = 'locked'`, a).Scan(&actor, &reason, &last)
	}); err != nil {
		t.Fatal(err)
	}
	if actor != port.SystemActorRef || reason == "" || last != t0.Format(time.RFC3339) {
		t.Fatalf("kiểm toán khóa: actor=%s reason=%q last=%s", actor, reason, last)
	}
	var warnings, locks int
	_ = tenant.WithTenantTx(ctx, f.env.AdminPool(t, migrations.PF), f.tenant, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, "SELECT count(*) FROM shared.outbox WHERE tenant_id = $1 AND event_type = $2", f.tenant, port.EventUserDormantWarning).Scan(&warnings); err != nil {
			return err
		}
		return tx.QueryRow(ctx, "SELECT count(*) FROM shared.outbox WHERE tenant_id = $1 AND event_type = $2 AND payload->>'cause' = $3",
			f.tenant, port.EventUserLocked, string(port.LockDormant)).Scan(&locks)
	})
	if warnings != 2 || locks != 2 {
		t.Fatalf("outbox: dormant_warning=%d locked(dormant)=%d", warnings, locks)
	}

	// Admin mở khóa a → mốc mới; job không khóa lại ngay.
	if r := nat.do(http.MethodPost, "/v1/accounts/users/"+a.String()+"/unlock", map[string]any{"reason": "Cán bộ quay lại công tác sau nghỉ phép dài"}); r.status != http.StatusOK {
		t.Fatalf("mở khóa: %d %v", r.status, r.body)
	}
	if got := dormantRun(t, f); got != (accountsDormant{}) || !enabled(a) {
		t.Fatalf("sau mở khóa: %+v enabled=%v", got, enabled(a))
	}

	clk.Advance(13 * 24 * time.Hour) // b: 38 ngày từ lần đăng nhập; a: 13 ngày từ lúc mở khóa
	if got := dormantRun(t, f); got != (accountsDormant{warned: 1}) {
		t.Fatalf("b ngày 38: %+v", got)
	}
	f.dir.RecordLogin(b, "10.0.0.1", "agrigreen-web-ops", true) // b đăng nhập lại → không khóa
	clk.Advance(7 * 24 * time.Hour)
	if got := dormantRun(t, f); got != (accountsDormant{}) || !enabled(b) {
		t.Fatalf("b đã đăng nhập lại: %+v enabled=%v", got, enabled(b))
	}
}
