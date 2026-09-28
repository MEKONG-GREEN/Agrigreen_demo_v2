package residency

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func envOf(m map[string]string) (func(string) string, []string) {
	var environ []string
	for k, v := range m {
		environ = append(environ, k+"="+v)
	}
	return func(k string) string { return m[k] }, environ
}

// Cấu hình production hợp lệ: mọi đích trong lãnh thổ hoặc tên dịch vụ nội cụm.
func prodEnv() map[string]string {
	return map[string]string{
		"AG_ENV":                 "production",
		"AG_RESIDENCY_ALLOWLIST": "agrigreen.vn, .viettelidc.com.vn, 10.20.0.0/16, 203.0.113.10, single-label",
		"AG_PF_DB_URL":           "postgres://pgbouncer:6432/?sslmode=require",
		"AG_DB_DIRECT_URL":       "postgres://ag_migrator:bí-mật@10.20.1.5:5432,10.20.1.6:5432/ag_pf?sslmode=require",
		"AG_S3_ENDPOINT":         "s3.hcm.viettelidc.com.vn:443",
		"AG_OTLP_ENDPOINT":       "otel-collector:4317",
		"AG_OIDC_ISSUER":         "https://id.agrigreen.vn/realms/agrigreen",
		"AG_OIDC_JWKS_URL":       "http://keycloak:8080/realms/agrigreen/protocol/openid-connect/certs",
		"AG_VAULT_URL":           "https://identity-vault.agrigreen.svc.agrigreen.vn:8090",
		"AG_AMQP_URL":            "amqps://agrigreen:mật-khẩu@203.0.113.10:5671/",
		"AG_SMTP_HOST":           "smtp.agrigreen.vn",
		"AG_HTTP_ADDR":           ":8080", // địa chỉ lắng nghe — không phải đích
		"AG_TEST_S3_ENDPOINT":    "s3.amazonaws.com",
	}
}

func TestProductionAllInTerritoryPasses(t *testing.T) {
	get, environ := envOf(prodEnv())
	if err := Check(get, environ); err != nil {
		t.Fatalf("cấu hình trong lãnh thổ phải qua: %v", err)
	}
}

func TestNonProductionSkipped(t *testing.T) {
	m := prodEnv()
	m["AG_ENV"] = "local"
	m["AG_S3_ENDPOINT"] = "s3.amazonaws.com"
	delete(m, "AG_RESIDENCY_ALLOWLIST")
	get, environ := envOf(m)
	if err := Check(get, environ); err != nil {
		t.Fatalf("Local/Dev không kiểm RB-7: %v", err)
	}
}

func TestProductionRequiresAllowlist(t *testing.T) {
	for _, v := range []string{"", "  ,  "} {
		m := prodEnv()
		m["AG_RESIDENCY_ALLOWLIST"] = v
		get, environ := envOf(m)
		if err := Check(get, environ); !errors.Is(err, ErrViolation) {
			t.Fatalf("thiếu danh mục (%q) phải dừng khởi động, nhận %v", v, err)
		}
	}
	m := prodEnv()
	m["AG_ENV"] = " PROD "
	m["AG_RESIDENCY_ALLOWLIST"] = "*"
	get, environ := envOf(m)
	if err := Check(get, environ); !errors.Is(err, ErrViolation) {
		t.Fatalf("ký tự đại diện phải bị từ chối, nhận %v", err)
	}
}

// Mỗi loại đích (DB, S3, OTLP, SMTP, FCM, Keycloak, vault) ra ngoài lãnh thổ đều bị chặn,
// và thông báo lỗi không lộ mật khẩu trong URL.
func TestEachEndpointOutsideTerritoryFails(t *testing.T) {
	cases := map[string]string{
		"AG_PF_DB_URL":     "postgres://platform_api:mật-khẩu-bí@db.us-east-1.rds.amazonaws.com:5432/ag_pf",
		"AG_DB_DIRECT_URL": "postgres://u:p@10.20.1.5:5432,54.1.2.3:5432/ag_pf",
		"AG_S3_ENDPOINT":   "s3.ap-southeast-1.amazonaws.com",
		"AG_OTLP_ENDPOINT": "otlp.eu01.nr-data.net:4317",
		"AG_SMTP_HOST":     "smtp.sendgrid.net",
		"AG_FCM_ENDPOINT":  "https://fcm.googleapis.com/v1/projects/x/messages:send",
		"AG_OIDC_ISSUER":   "https://agrigreen.eu.auth0.com/",
		"AG_VAULT_URL":     "https://vault.example.com:8090",
		"AG_AMQP_URL":      "amqps://u:p@b-1.mq.ap-southeast-1.amazonaws.com:5671",
		"AG_NEW_THING_URL": "https://api.example.org/hook", // biến mới theo quy ước tên cũng được kiểm
	}
	for k, v := range cases {
		m := prodEnv()
		m[k] = v
		get, environ := envOf(m)
		err := Check(get, environ)
		if !errors.Is(err, ErrViolation) || !strings.Contains(err.Error(), k) {
			t.Errorf("%s=%s phải bị chặn, nhận %v", k, v, err)
			continue
		}
		if strings.Contains(err.Error(), "mật-khẩu") || strings.Contains(err.Error(), "u:p@") {
			t.Errorf("thông báo lỗi lộ bí mật: %v", err)
		}
	}
}

// FCM data-only được phép ra ngoài qua AG_NONPII_ALLOWLIST; biến khác thì không.
func TestNonPIIAllowlistOnlyForPushChannels(t *testing.T) {
	m := prodEnv()
	m["AG_NONPII_ALLOWLIST"] = "fcm.googleapis.com"
	m["AG_FCM_ENDPOINT"] = "https://fcm.googleapis.com/v1/projects/agrigreen/messages:send"
	get, environ := envOf(m)
	if err := Check(get, environ); err != nil {
		t.Fatalf("FCM data-only trong AG_NONPII_ALLOWLIST phải qua: %v", err)
	}
	m["AG_OTLP_ENDPOINT"] = "fcm.googleapis.com:443"
	get, environ = envOf(m)
	if err := Check(get, environ); !errors.Is(err, ErrViolation) || !strings.Contains(err.Error(), "AG_OTLP_ENDPOINT") {
		t.Fatalf("AG_NONPII_ALLOWLIST không được mở cho OTLP (nhật ký có định danh): %v", err)
	}
}

func TestSingleLabelOnlyWhenListed(t *testing.T) {
	m := prodEnv()
	m["AG_RESIDENCY_ALLOWLIST"] = "agrigreen.vn,.viettelidc.com.vn,10.20.0.0/16,203.0.113.10"
	get, environ := envOf(m)
	err := Check(get, environ)
	if !errors.Is(err, ErrViolation) || !strings.Contains(err.Error(), "AG_PF_DB_URL → pgbouncer") {
		t.Fatalf("tên một nhãn chỉ qua khi danh mục có single-label: %v", err)
	}
}

func TestHosts(t *testing.T) {
	cases := map[string][]string{
		"seaweedfs:9000":                        {"seaweedfs"},
		"https://ID.AgriGreen.vn/realms/x":      {"id.agrigreen.vn"},
		"postgres://u:p@h1:5432,h2:5433/db?x=1": {"h1", "h2"},
		"postgres:///ag_pf?host=/var/run/pg":    nil,
		"[2001:db8::1]:443":                     {"2001:db8::1"},
		"a.vn:1, b.vn:2":                        {"a.vn", "b.vn"},
		"smtp.agrigreen.vn.":                    {"smtp.agrigreen.vn"},
	}
	for in, want := range cases {
		got, err := Hosts(in)
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Errorf("Hosts(%q) = %v, %v; muốn %v", in, got, err, want)
		}
	}
}

func TestListMatch(t *testing.T) {
	l, err := ParseList("agrigreen.vn, .gov.vn, 10.0.0.0/8, 2001:db8::/32, 198.51.100.7")
	if err != nil {
		t.Fatal(err)
	}
	for h, want := range map[string]bool{
		"agrigreen.vn": true, "s3.agrigreen.vn": true, "evilagrigreen.vn": false, "agrigreen.vn.evil.com": false,
		"mard.gov.vn": true, "gov.vn": true, "10.1.2.3": true, "11.0.0.1": false, "2001:db8::5": true,
		"198.51.100.7": true, "198.51.100.8": false, "::ffff:10.0.0.1": true, "pgbouncer": false,
	} {
		if got := l.Match(h); got != want {
			t.Errorf("Match(%q) = %v, muốn %v", h, got, want)
		}
	}
	for _, bad := range []string{"*.vn", "10.0.0.0/33", "user@host", "."} {
		if _, err := ParseList(bad); err == nil {
			t.Errorf("ParseList(%q) phải lỗi", bad)
		}
	}
}

// Mọi tiến trình dịch vụ gọi guard ngay khi khởi động — thiếu là hỏng build.
func TestServiceMainsCallGuard(t *testing.T) {
	for _, cmd := range []string{"api-platform", "identity-vault", "worker", "outbox-publisher"} {
		b, err := os.ReadFile(filepath.Join("..", "..", "..", "cmd", cmd, "main.go"))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(b), "residency.CheckEnv()") {
			t.Errorf("cmd/%s/main.go phải gọi residency.CheckEnv() lúc khởi động (RB-7)", cmd)
		}
	}
}
