package keycloak

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/port"
)

func secondsDur(n int) time.Duration { return time.Duration(n) * time.Second }

// runContract là hợp đồng chung của Directory: bản trong bộ nhớ và bản HTTP (qua
// emulator mô phỏng Keycloak) phải cư xử giống nhau. mem là kho quan sát được.
func runContract(t *testing.T, d port.Directory, mem *Memory) {
	ctx := context.Background()
	tid, org, org2 := uuid.New(), uuid.New(), uuid.New()
	nu := func(u string) port.NewUser {
		return port.NewUser{Username: u, Email: u + "@vi-du.vn", FirstName: "An", LastName: "Nguyễn Văn", TenantID: tid, OrgRef: org,
			RequiredActions: []string{port.ActionVerifyEmail, port.ActionUpdatePassword}}
	}
	a := nu("can-bo-a")
	a.Phone = "0999000001"
	idA, err := d.CreateUser(ctx, a)
	if err != nil {
		t.Fatal(err)
	}
	u, err := d.GetUser(ctx, idA)
	if err != nil || u.Username != "can-bo-a" || u.TenantID != tid || u.OrgRef != org || u.Phone != "0999000001" || !u.Enabled ||
		!slices.Equal(u.RequiredActions, a.RequiredActions) || u.LastName != "Nguyễn Văn" {
		t.Fatalf("GetUser: %+v %v", u.ID, err)
	}
	// Tên đăng nhập và email duy nhất trong realm (không phân biệt hoa thường).
	if _, err := d.CreateUser(ctx, nu("can-bo-a")); !errors.Is(err, port.ErrUserExists) {
		t.Fatalf("trùng username: %v", err)
	}
	dupMail := nu("khac")
	dupMail.Email = "CAN-BO-A@vi-du.vn"
	if _, err := d.CreateUser(ctx, dupMail); !errors.Is(err, port.ErrUserExists) {
		t.Fatalf("trùng email: %v", err)
	}
	idB, _ := d.CreateUser(ctx, nu("can-bo-b"))
	other := nu("can-bo-c")
	other.TenantID = uuid.New()
	if _, err := d.CreateUser(ctx, other); err != nil {
		t.Fatal(err)
	}

	attrs := map[string]string{port.AttrTenantID: tid.String(), port.AttrOrgRef: org.String()}
	found, err := d.FindUsers(ctx, port.Query{Attributes: attrs})
	if err != nil || len(found) != 2 {
		t.Fatalf("lọc theo tenant+org: %d %v", len(found), err)
	}
	page, _ := d.FindUsers(ctx, port.Query{Attributes: attrs, First: 1, Max: 1})
	if len(page) != 1 || page[0].ID != idB {
		t.Fatalf("phân trang first/max: %v", page)
	}
	byName, _ := d.FindUsers(ctx, port.Query{Username: "CAN-BO-A", Max: 1})
	if len(byName) != 1 || byName[0].ID != idA {
		t.Fatalf("tìm chính xác username: %d", len(byName))
	}
	byPhone, _ := d.FindUsers(ctx, port.Query{Attributes: map[string]string{port.AttrTenantID: tid.String(), port.AttrPhone: "0999000001"}})
	if len(byPhone) != 1 {
		t.Fatalf("tìm theo SĐT: %d", len(byPhone))
	}

	if err := d.SetEnabled(ctx, idA, false); err != nil {
		t.Fatal(err)
	}
	if err := d.SetOrgRef(ctx, idA, org2); err != nil {
		t.Fatal(err)
	}
	u, _ = d.GetUser(ctx, idA)
	if u.Enabled || u.OrgRef != org2 || u.TenantID != tid || u.Phone != "0999000001" {
		t.Fatalf("cập nhật phải giữ thuộc tính khác: enabled=%v org=%v tenant=%v", u.Enabled, u.OrgRef, u.TenantID)
	}
	if moved, _ := d.FindUsers(ctx, port.Query{Attributes: map[string]string{port.AttrOrgRef: org2.String()}}); len(moved) != 1 {
		t.Fatalf("đổi org_ref: %d", len(moved))
	}

	if err := d.AddRealmRoles(ctx, idA, []string{"kn_officer", "coop_manager"}); err != nil {
		t.Fatal(err)
	}
	if err := d.AddRealmRoles(ctx, idA, []string{"kn_officer"}); err != nil {
		t.Fatalf("gán lại vai đã có: %v", err)
	}
	if err := d.AddRealmRoles(ctx, idA, []string{"khong_co_vai"}); !errors.Is(err, port.ErrRoleNotFound) {
		t.Fatalf("vai không có: %v", err)
	}
	if err := d.RemoveRealmRoles(ctx, idA, []string{"coop_manager", "offline_access"}); err != nil {
		t.Fatal(err)
	}
	if roles, _ := d.RealmRoles(ctx, idA); !slices.Equal(roles, []string{"kn_officer"}) {
		t.Fatalf("vai: %v", roles)
	}
	if err := d.AddRequiredActions(ctx, idA, []string{port.ActionConfigureTOTP, port.ActionVerifyEmail}); err != nil {
		t.Fatal(err)
	}
	if u, _ = d.GetUser(ctx, idA); !slices.Contains(u.RequiredActions, port.ActionConfigureTOTP) || len(u.RequiredActions) != 3 {
		t.Fatalf("required actions: %v", u.RequiredActions)
	}
	if has, err := d.HasOTP(ctx, idA); err != nil || has {
		t.Fatalf("HasOTP: %v %v", has, err)
	}
	mem.SetOTP(idA, true)
	if has, _ := d.HasOTP(ctx, idA); !has {
		t.Fatal("HasOTP phải thấy otp")
	}

	// Tài khoản đang khóa không nhận được email hành động.
	if err := d.ExecuteActionsEmail(ctx, idA, []string{port.ActionUpdatePassword}, time.Hour); !errors.Is(err, port.ErrEmailFailed) {
		t.Fatalf("email khi đang khóa: %v", err)
	}
	_ = d.SetEnabled(ctx, idA, true)
	if err := d.ExecuteActionsEmail(ctx, idA, []string{port.ActionUpdatePassword}, 72*time.Hour); err != nil {
		t.Fatal(err)
	}
	sent := mem.Sent()
	if len(sent) != 1 || sent[0].UserID != idA || sent[0].Lifespan != 72*time.Hour || !slices.Equal(sent[0].Actions, []string{port.ActionUpdatePassword}) {
		t.Fatalf("email đã gửi: %+v", sent)
	}
	if err := d.Logout(ctx, idA); err != nil || mem.Logouts(idA) != 1 {
		t.Fatalf("logout: %v %d", err, mem.Logouts(idA))
	}
	// GIS US-LOG-01 AC-3: khóa do đăng nhập sai — đọc và xóa qua attack-detection.
	if locked, err := d.BruteForceLocked(ctx, idA); err != nil || locked {
		t.Fatalf("chưa bị khóa chống dò: %v %v", locked, err)
	}
	mem.LockByBruteForce(idA)
	if locked, err := d.BruteForceLocked(ctx, idA); err != nil || !locked {
		t.Fatalf("khóa chống dò: %v %v", locked, err)
	}
	if err := d.ClearBruteForce(ctx, idA); err != nil {
		t.Fatal(err)
	}
	if locked, _ := d.BruteForceLocked(ctx, idA); locked {
		t.Fatal("sau khi mở khóa")
	}
	// P0-VPS-OPS (#131): khóa tạm 15 phút — trạng thái kèm mốc tự hết, và tự hết khi qua mốc.
	if st, err := d.LoginLockStatus(ctx, idA); err != nil || st.Locked || st.Until != nil {
		t.Fatalf("chưa khóa: %+v %v", st, err)
	}
	before := time.Now()
	mem.LockByBruteForce(idA)
	st, err := d.LoginLockStatus(ctx, idA)
	if err != nil || !st.Locked || st.Until == nil {
		t.Fatalf("khóa tạm: %+v %v", st, err)
	}
	if want := before.Add(DefaultLockoutWait); st.Until.Before(want.Add(-time.Second)) || st.Until.After(want.Add(5*time.Second)) {
		t.Fatalf("mốc tự hết %v, muốn ~%v", st.Until, want)
	}
	mem.SetNow(func() time.Time { return time.Now().Add(DefaultLockoutWait + time.Minute) })
	if st, err := d.LoginLockStatus(ctx, idA); err != nil || st.Locked || st.Until != nil {
		t.Fatalf("khóa tạm phải tự hết: %+v %v", st, err)
	}
	if locked, _ := d.BruteForceLocked(ctx, idA); locked {
		t.Fatal("khóa tạm phải tự hết (BruteForceLocked)")
	}
	mem.SetNow(time.Now)

	if _, err := d.GetUser(ctx, uuid.New()); !errors.Is(err, port.ErrUserNotFound) {
		t.Fatalf("không có người dùng: %v", err)
	}
	if err := d.DeleteUser(ctx, idB); err != nil {
		t.Fatal(err)
	}
	if _, err := d.GetUser(ctx, idB); !errors.Is(err, port.ErrUserNotFound) {
		t.Fatalf("sau khi xóa bù: %v", err)
	}
}

func TestContractMemory(t *testing.T) {
	m := NewMemory()
	runContract(t, m, m)
}

func TestContractHTTPAgainstEmulator(t *testing.T) {
	e := newEmulator(t)
	runContract(t, e.client(t), e.mem)
	if n := e.tokens(); n != 1 {
		t.Fatalf("token phải được lưu và dùng lại, đã xin %d lần", n)
	}
}

// Token hết hiệu lực phía Keycloak (401) → xin token mới và thử lại một lần; token
// gần hạn → làm mới trước khi gọi.
func TestHTTPTokenRefresh(t *testing.T) {
	e := newEmulator(t)
	now := time.Date(2026, 9, 28, 8, 0, 0, 0, time.UTC)
	c, err := New(Config{BaseURL: e.srv.URL, Realm: emuRealm, ClientID: emuClient, ClientSecret: emuSecret, HTTP: e.srv.Client(),
		Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := c.FindUsers(ctx, port.Query{}); err != nil {
		t.Fatal(err)
	}
	e.revoke()
	if _, err := c.FindUsers(ctx, port.Query{}); err != nil || e.tokens() != 2 {
		t.Fatalf("401 → token mới: %v, %d", err, e.tokens())
	}
	now = now.Add(290 * time.Second) // còn 10 giây < biên 30 giây
	if _, err := c.FindUsers(ctx, port.Query{}); err != nil || e.tokens() != 3 {
		t.Fatalf("gần hạn → làm mới: %v, %d", err, e.tokens())
	}
}

// Sai bí mật → ErrUnavailable; thông báo lỗi không chứa bí mật hay thân phản hồi (R-14).
func TestHTTPErrorsCarryNoSecrets(t *testing.T) {
	e := newEmulator(t)
	c, _ := New(Config{BaseURL: e.srv.URL, Realm: emuRealm, ClientID: emuClient, ClientSecret: "sai-bi-mat", HTTP: e.srv.Client()})
	_, err := c.GetUser(context.Background(), uuid.New())
	if !errors.Is(err, port.ErrUnavailable) || strings.Contains(err.Error(), "sai-bi-mat") {
		t.Fatalf("lỗi token: %v", err)
	}
	ok := e.client(t)
	id, _ := ok.CreateUser(context.Background(), port.NewUser{Username: "trung-ten", Email: "trung@vi-du.vn", TenantID: uuid.New(), OrgRef: uuid.New()})
	_, err = ok.CreateUser(context.Background(), port.NewUser{Username: "trung-ten", Email: "khac@vi-du.vn", TenantID: uuid.New(), OrgRef: uuid.New()})
	if id == uuid.Nil || !errors.Is(err, port.ErrUserExists) || strings.Contains(err.Error(), "trung-ten") || strings.Contains(err.Error(), "User exists") {
		t.Fatalf("409 không được mang thân phản hồi: %v", err)
	}
}

func TestConfigFromEnv(t *testing.T) {
	t.Setenv("AG_KC_ADMIN_URL", "")
	if _, ok, err := ConfigFromEnv(); ok || err != nil {
		t.Fatalf("không đặt → tắt: %v %v", ok, err)
	}
	t.Setenv("AG_KC_ADMIN_URL", "http://keycloak:8080")
	t.Setenv("AG_KC_REALM", "agrigreen")
	t.Setenv("AG_KC_CLIENT_ID", "agrigreen-accounts")
	t.Setenv("AG_KC_CLIENT_SECRET", "")
	if _, _, err := ConfigFromEnv(); err == nil {
		t.Fatal("thiếu secret phải lỗi")
	}
	t.Setenv("AG_KC_CLIENT_SECRET", "x")
	t.Setenv("AG_KC_TIMEOUT", "5s")
	cfg, ok, err := ConfigFromEnv()
	if !ok || err != nil || cfg.Timeout != 5*time.Second {
		t.Fatalf("cấu hình: %v %v %v", ok, err, cfg.Timeout)
	}
}
