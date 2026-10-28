package handler

// Ca vàng hợp đồng menu Go ↔ web-ops (KN-fix, UR-KT-02): menu do bảng tĩnh Go dựng (cùng hàm của
// GET /v1/menu, cùng thân JSON KNMenu) cho 5 phòng ban mặc định An Giang (W11.06) được ghi ra
// apps/web-ops/src/api/mock/fixtures/kn-menu.json; Vitest (src/api/mock/kn-menu.contract.test.ts) kiểm
// bản giả lập web trả ĐÚNG như vậy, và accounts/domain (P1 default_route) kiểm cùng tệp.
// Sinh lại sau khi đổi domain/menu.go:
//   AG_UPDATE_WEB_FIXTURES=1 go test ./internal/platform/party/handler -run TestWebMenuFixture

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/seeddata"
)

const webMenuFixture = "../../../../apps/web-ops/src/api/mock/fixtures/kn-menu.json"

type adHocForms bool

func (f adHocForms) HasForms(context.Context, string, string, string) (bool, error) {
	return bool(f), nil
}

type webMenuCase struct {
	Name       string `json:"name"`
	Audience   string `json:"audience"`
	Department string `json:"department"`
	AdHoc      bool   `json:"ad_hoc"`
	Menu       any    `json:"menu"`
}

type webMenuDept struct {
	Code        string `json:"code"`
	Name        string `json:"name"`
	Status      string `json:"status"`
	MenuProfile string `json:"menu_profile"`
}

// UR-KT-02 / KN-fix: bản giả lập menu của web-ops = bảng menu Go (một hợp đồng).
func TestWebMenuFixture(t *testing.T) {
	navigation, err := domain.RegisteredNavigation()
	if err != nil {
		t.Fatal(err)
	}
	navigate := func(view app.MenuView) app.MenuView {
		out, err := app.ApplyNavigation(view, navigation)
		if err != nil {
			t.Fatal(err)
		}
		return out
	}

	var seed struct {
		Departments []webMenuDept `json:"departments"`
	}
	if err := json.Unmarshal(seeddata.Departments, &seed); err != nil {
		t.Fatal(err)
	}
	depts := make([]port.Department, 0, len(seed.Departments))
	for _, d := range seed.Departments {
		depts = append(depts, port.Department{Code: d.Code, Name: d.Name, Status: d.Status, MenuProfile: d.MenuProfile})
	}
	ctx := context.Background()
	cases := []webMenuCase{}
	for _, adHoc := range []bool{false, true} {
		for _, aud := range []string{port.AudienceTW, port.AudienceTinh, port.AudienceXa} {
			if adHoc && aud == port.AudienceTW {
				continue // Cấp Quốc gia không có Báo cáo đột xuất
			}
			v, err := app.DepartmentMenu(ctx, adHocForms(adHoc), depts[0], aud)
			if err != nil {
				t.Fatal(err)
			}
			name := aud + "/" + depts[0].Code
			if adHoc {
				name += "+dot_xuat"
			}
			cases = append(cases, webMenuCase{Name: name, Audience: aud, Department: depts[0].Code, AdHoc: adHoc, Menu: menuBody(navigate(v))})
		}
	}
	// kn.menu/v3 (W11.06): khuôn riêng của từng phòng ban còn lại (bảng menu không phụ thuộc trạng thái —
	// trạng thái chỉ chặn chọn phòng ban ở ngữ cảnh phiên).
	for _, d := range depts[1:] {
		for _, aud := range []string{port.AudienceTW, port.AudienceTinh, port.AudienceXa} {
			v, err := app.DepartmentMenu(ctx, nil, d, aud)
			if err != nil {
				t.Fatal(err)
			}
			cases = append(cases, webMenuCase{Name: aud + "/" + d.Code, Audience: aud, Department: d.Code, Menu: menuBody(navigate(v))})
		}
	}
	cases = append(cases, webMenuCase{Name: "admin", Audience: port.AudienceAdmin, Menu: menuBody(navigate(app.AdminMenu(uuid.Nil, depts)))})
	defaults := map[string]string{}
	for _, aud := range port.Audiences {
		defaults[aud] = domain.DefaultRoute(aud)
	}
	want, err := json.MarshalIndent(map[string]any{
		"_note":          "SINH TỪ GO (internal/platform/party/handler/webmenu_fixture_test.go) — không sửa tay. AG_UPDATE_WEB_FIXTURES=1 go test ./internal/platform/party/handler -run TestWebMenuFixture",
		"menu_version":   domain.MenuVersion,
		"departments":    seed.Departments,
		"default_routes": defaults,
		"cases":          cases,
	}, "", " ")
	if err != nil {
		t.Fatal(err)
	}
	want = append(want, '\n')
	if os.Getenv("AG_UPDATE_WEB_FIXTURES") == "1" {
		if err := os.MkdirAll(filepath.Dir(webMenuFixture), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(webMenuFixture, want, 0o644); err != nil { //nolint:gosec // tệp mẫu của repo
			t.Fatal(err)
		}
		return
	}
	got, err := os.ReadFile(webMenuFixture) //nolint:gosec // đường dẫn cố định trong repo
	if err != nil {
		t.Fatalf("%v (sinh bằng AG_UPDATE_WEB_FIXTURES=1)", err)
	}
	if !bytes.Equal(bytes.ReplaceAll(got, []byte("\r\n"), []byte("\n")), want) {
		t.Error("kn-menu.json lệch bảng menu Go — sinh lại bằng AG_UPDATE_WEB_FIXTURES=1 rồi chạy Vitest (kn-menu.contract.test.ts)")
	}
}
