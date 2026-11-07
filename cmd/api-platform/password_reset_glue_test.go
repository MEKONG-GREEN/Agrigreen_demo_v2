package main

import (
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz/authztest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
)

// #255 AUTH-V2-RESET-NOPIN (ADR-0057): route tự đặt lại mật khẩu nằm ở /v1/public (không cần token), còn đăng nhập bằng
// PIN Mobile đã bỏ theo BRD 2.0 (FN-CH-00.01, FN-05) — không bề mặt API nào có route PIN.
func TestPasswordResetPublicAndNoPinRoutes(t *testing.T) {
	app, admin, iss := glueFixture(t)
	for _, p := range []string{"/v1/public/password-reset", "/v1/public/password-reset/confirm"} {
		resp, b := send(t, http.MethodPost, app.URL+p, map[string]string{"Content-Type": "application/json"}, map[string]any{"email": "x@kn.example.vn"})
		if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusMethodNotAllowed {
			t.Fatalf("%s phải là route công khai: %d %v", p, resp.StatusCode, b)
		}
	}
	tok := iss.Sign(t, authztest.Token{Subject: ids.New(), TenantID: ids.New(), Roles: []string{"kn_user"}, MFA: true})
	hdr := map[string]string{"Authorization": "Bearer " + tok, "Content-Type": "application/json", "Idempotency-Key": "pin-" + ids.New().String()}
	for _, base := range []string{app.URL, admin.URL} {
		for _, p := range []string{"/v1/pin", "/v1/me/pin", "/v1/auth/pin", "/v1/public/pin", "/v1/devices/pin", "/v1/accounts/pin", "/v1/me/pin/verify"} {
			for _, m := range []string{http.MethodGet, http.MethodPost, http.MethodPut} {
				if resp, b := send(t, m, base+p, hdr, map[string]any{"pin": "123456"}); resp.StatusCode != http.StatusNotFound {
					t.Fatalf("%s %s phải 404 (PIN đã bỏ): %d %v", m, p, resp.StatusCode, b)
				}
			}
		}
	}
}

// Hợp đồng OpenAPI không còn đường dẫn hay operation PIN (gỡ hẳn, không tắt bằng cờ).
func TestOpenAPIHasNoPinPaths(t *testing.T) {
	pin := regexp.MustCompile(`(?im)^  /[^\n]*/pins?(/|:|\{)|operationId:\s*\w*Pin\b`)
	files, err := filepath.Glob(filepath.Join("..", "..", "contracts", "openapi", "*.yaml"))
	if err != nil || len(files) == 0 {
		t.Fatalf("không đọc được hợp đồng: %v", err)
	}
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		if m := pin.Find(b); m != nil {
			t.Fatalf("%s còn route PIN: %q", f, m)
		}
	}
}
