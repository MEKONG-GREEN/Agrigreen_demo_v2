package reports

import (
	"testing"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
)

func getenvOf(kv ...string) func(string) string {
	m := map[string]string{}
	for i := 0; i+1 < len(kv); i += 2 {
		m[kv[i]] = kv[i+1]
	}
	return func(k string) string { return m[k] }
}

// P0-NOTIFY-FAILCLOSED (#127): bộ gửi email báo cáo chưa cấu hình → nil (app ghi "Bỏ gửi"/"Gửi lỗi" kèm lý do, không
// "Đã gửi"); AG_MAILER=log ngoài máy phát triển và AG_MAILER=smtp thiếu host là lỗi khởi động.
func TestSMTPFromEnvWithPerDeployEnv(t *testing.T) {
	smtp := []string{"AG_SMTP_HOST", "smtp.agrigreen.vn", "AG_SMTP_FROM", "no-reply@agrigreen.vn"}
	cases := []struct {
		name       string
		env        []string
		configured bool
		fail       bool
	}{
		{name: "demo thiếu SMTP", env: []string{"AG_ENV", "staging", "AG_DEPLOY_ENV", "demo"}},
		{name: "production thiếu SMTP", env: []string{"AG_ENV", "production"}},
		{name: "production có SMTP", env: append([]string{"AG_ENV", "production"}, smtp...), configured: true},
		{name: "tắt có chủ đích", env: append([]string{"AG_ENV", "production", "AG_MAILER", "disabled"}, smtp...)},
		{name: "local log → không có bộ gửi", env: []string{"AG_ENV", "local", "AG_MAILER", "log"}},
		{name: "demo log bị từ chối", env: []string{"AG_ENV", "staging", "AG_DEPLOY_ENV", "demo", "AG_MAILER", "log"}, fail: true},
		{name: "smtp tường minh thiếu host", env: []string{"AG_ENV", "production", "AG_MAILER", "smtp"}, fail: true},
		{name: "giá trị lạ", env: []string{"AG_ENV", "local", "AG_MAILER", "x"}, fail: true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m, err := SMTPFromEnvWith(getenvOf(c.env...), clock.System{})
			if c.fail != (err != nil) || (m != nil) != c.configured {
				t.Fatalf("được %T %v", m, err)
			}
		})
	}
}

func TestLinkBasesFromEnvFailClosed(t *testing.T) {
	// Base compose đặt AG_REPORT_LINK_BASE=http://localhost:8080 — trên VPS (staging) phải bị loại.
	web, dl, errs := LinkBasesFromEnv(getenvOf("AG_ENV", "staging", "AG_REPORT_LINK_BASE", "http://localhost:8080"))
	if web != "" || dl != "" || len(errs) != 2 {
		t.Fatalf("staging: %q %q %v", web, dl, errs)
	}
	web, dl, errs = LinkBasesFromEnv(getenvOf("AG_ENV", "production", "AG_WEB_OPS_URL", "https://ops.agrigreen.vn", "AG_REPORT_LINK_BASE", "https://api.agrigreen.vn"))
	if web != "https://ops.agrigreen.vn" || dl != "https://api.agrigreen.vn" || len(errs) != 0 {
		t.Fatalf("production: %q %q %v", web, dl, errs)
	}
	web, dl, errs = LinkBasesFromEnv(getenvOf("AG_ENV", "local", "AG_REPORT_LINK_BASE", "http://localhost:8080"))
	if web != "" || dl != "http://localhost:8080" || len(errs) != 0 {
		t.Fatalf("local: %q %q %v", web, dl, errs)
	}
}
