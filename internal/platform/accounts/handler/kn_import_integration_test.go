package handler_test

// W11.02 (FN-AD-04.04, UR-AD-04): nạp Excel tài khoản KN nhiều vai trò; tab Phiên đang hoạt động / Lịch sử đăng nhập.

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/xuri/excelize/v2"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tabular"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

func (f *knFix) rawAdmin(c caller, path, idem, ctype string, body []byte) resp {
	f.t.Helper()
	hdr := map[string]string{"Content-Type": ctype, "Idempotency-Key": idem}
	r, _ := http.NewRequest(http.MethodPost, f.admin2.URL+path, bytes.NewReader(body))
	r.Header.Set("Authorization", "Bearer "+c.token)
	for k, v := range hdr {
		r.Header.Set(k, v)
	}
	res, err := http.DefaultClient.Do(r)
	if err != nil {
		f.t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()
	var m map[string]any
	_ = json.NewDecoder(res.Body).Decode(&m)
	return resp{status: res.StatusCode, hdr: res.Header, body: m}
}

func TestFN_AD_04_04_ImportMultiRoleAccounts(t *testing.T) {
	f := newKNFixture(t)
	ctx := context.Background()
	p91Code := ""
	{
		o, err := f.party.Get(tenant.WithID(ctx, f.tenant), f.p91)
		if err != nil {
			t.Fatal(err)
		}
		p91Code = o.Code
	}
	header := []string{"Họ tên", "Email", "SĐT", "Vai trò", "Mã Nút", "Phòng ban", "Cấp quản lý", "Xã phụ trách"}
	rows := [][]string{
		// Tài khoản A: Cấp Xã (1 xã) + Cấp Quốc gia — hai dòng cùng Email, Nút theo mã và theo UUID.
		{"Nguyễn Văn An", "an.nv@vi-du.vn", "0912345001", "Cấp Xã", p91Code, deptTT, "Chuyên viên", "30292"},
		{"Nguyễn Văn An", "AN.NV@vi-du.vn", "", "kn_tw", f.national.String(), deptTT, "", ""},
		// Tài khoản B: dòng 2 sai vai trò → cả tài khoản bị loại.
		{"Trần Thị Bình", "binh.tt@vi-du.vn", "", "Cấp Tỉnh", p91Code, deptTT, "truong_phong", ""},
		{"Trần Thị Bình", "binh.tt@vi-du.vn", "", "Thủ quỹ", p91Code, deptTT, "", ""},
		// Tài khoản C: phòng ban không thuộc đơn vị → loại (lỗi từ CreateKNUser).
		{"Lê Văn Cường", "cuong.lv@vi-du.vn", "", "Cấp Tỉnh", p91Code, "phong_khong_co", "", ""},
		// Dòng thiếu Email; Nút không có.
		{"Phạm Dũng", "", "", "Cấp Tỉnh", p91Code, deptTT, "", ""},
		{"Võ Em", "em.v@vi-du.vn", "", "Cấp Tỉnh", "KHONG-CO", deptTT, "", ""},
	}
	var buf bytes.Buffer
	if err := tabular.WriteXLSX(&buf, "Tai khoan", header, rows); err != nil {
		t.Fatal(err)
	}
	k := key()
	r := f.rawAdmin(f.root, "/v1/accounts/kn-users/import", k, tabular.ContentTypeXLSX, buf.Bytes())
	if r.status != http.StatusOK || r.body["accounts_created"] != float64(1) || r.body["created"] != float64(2) || r.body["rejected"] != float64(5) {
		t.Fatalf("nạp: %d %v", r.status, r.body)
	}
	out := r.body["rows"].([]any)
	reason := func(i int) string { s, _ := out[i].(map[string]any)["reason"].(string); return s }
	if !strings.Contains(reason(3), "Vai trò không hợp lệ") || !strings.Contains(reason(2), "dòng lỗi khác") || !strings.Contains(reason(4), "Phòng ban") ||
		!strings.Contains(reason(5), "Email") || !strings.Contains(reason(6), "Không có Nút") {
		t.Fatalf("lý do: %v", out)
	}
	for i := range out {
		if s := reason(i); strings.Contains(s, "@") || strings.Contains(s, "Bình") {
			t.Fatalf("lý do chứa dữ liệu nhập: %q", s)
		}
	}
	id := uuid.MustParse(out[0].(map[string]any)["user_id"].(string))
	if out[1].(map[string]any)["user_id"] != id.String() {
		t.Fatalf("hai dòng cùng Email phải là một tài khoản: %v", out)
	}
	asgs := f.adminDo(f.root, http.MethodGet, "/v1/accounts/users/"+id.String()+"/assignments", nil)
	var roles []string
	for _, a := range asgs.body["items"].([]any) {
		roles = append(roles, a.(map[string]any)["role"].(string))
	}
	slices.Sort(roles)
	if !slices.Equal(roles, []string{port.KNTW, port.KNXa}) {
		t.Fatalf("vai trò: %v", asgs.body)
	}
	if u, _ := f.dir.GetUser(ctx, id); u.Username != "0912345001" {
		t.Fatalf("tên đăng nhập = SĐT: %s", u.Username)
	}
	// Gửi lại cùng khóa → phát lại, không tạo thêm.
	again := f.rawAdmin(f.root, "/v1/accounts/kn-users/import", k, tabular.ContentTypeXLSX, buf.Bytes())
	if again.status != http.StatusOK || again.body["rows"].([]any)[0].(map[string]any)["user_id"] != id.String() {
		t.Fatalf("gọi lặp: %d %v", again.status, again.body)
	}
	// Sai mẫu, ô công thức, không phải quản trị KN.
	wantCode(t, f.rawAdmin(f.root, "/v1/accounts/kn-users/import", key(), "text/csv", []byte("ho_ten,email\nA,a@vi-du.vn\n")), http.StatusUnprocessableEntity,
		"accounts.import_invalid", "thiếu cột")
	fx := excelize.NewFile()
	_ = fx.SetSheetRow("Sheet1", "A1", &header)
	_ = fx.SetCellFormula("Sheet1", "B2", "=CONCAT(\"a\",\"@b.vn\")")
	var fb bytes.Buffer
	_ = fx.Write(&fb)
	wantCode(t, f.rawAdmin(f.root, "/v1/accounts/kn-users/import", key(), tabular.ContentTypeXLSX, fb.Bytes()), http.StatusUnprocessableEntity,
		"accounts.import_invalid", "ô công thức")
	wantCode(t, f.rawAdmin(f.provinceAdmin(), "/v1/accounts/kn-users/import", key(), "text/csv", []byte(strings.Join(header, ",")+"\n")), http.StatusForbidden, "",
		"quản trị tỉnh không quản lý tài khoản KN")

	// Tab Phiên đang hoạt động / Lịch sử đăng nhập.
	f.dir.RecordLogin(id, "10.0.0.7", "agrigreen-web-ops", false)
	f.dir.RecordLogin(id, "10.0.0.7", "agrigreen-web-ops", true)
	s := f.adminDo(f.root, http.MethodGet, "/v1/accounts/users/"+id.String()+"/sessions", nil)
	if s.status != http.StatusOK || s.body["available"] != true || len(s.body["active"].([]any)) != 1 || len(s.body["history"].([]any)) != 2 ||
		s.body["history"].([]any)[0].(map[string]any)["type"] != "LOGIN" || s.hdr.Get("Cache-Control") != "no-store" {
		t.Fatalf("phiên: %d %v", s.status, s.body)
	}
	other := f.tok(uuid.New(), uuid.New(), true, "platform_admin")
	wantCode(t, f.adminDo(other, http.MethodGet, "/v1/accounts/users/"+id.String()+"/sessions", nil), http.StatusNotFound, "accounts.user_not_found", "tenant khác")
}
