package gateway

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/cmd/integration-gateway/internal/testpki"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/apiclient/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
)

func envOf(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

// Cờ bật: tên mới AG_GATEWAY_ENABLED hoặc tên cũ AG_PUBLIC_API_ENABLED (một kỳ); khác "1" → thoát lỗi.
func TestEnabledFlagNewAndLegacy_W52(t *testing.T) {
	for _, v := range []string{"", "0", "true", "yes"} {
		for _, k := range []string{EnvEnabled, port.EnvEnabled} {
			if err := Run(context.Background(), quiet(), envOf(map[string]string{k: v}), Unit); !errors.Is(err, ErrDisabled) {
				t.Fatalf("%s=%q phải thoát với ErrDisabled, nhận %v", k, v, err)
			}
		}
	}
	for _, k := range []string{EnvEnabled, port.EnvEnabled} {
		if !Enabled(envOf(map[string]string{k: "1"})) {
			t.Fatalf("%s=1 phải bật", k)
		}
	}
}

// Tên mới thắng tên cũ; tên cũ vẫn đọc được (kỳ chuyển).
func TestEnvAnyPrefersNewName_W52(t *testing.T) {
	get := envOf(map[string]string{EnvOperatorTenant: "moi", port.EnvOperatorTenant: "cu"})
	if envAny(get, EnvOperatorTenant, port.EnvOperatorTenant) != "moi" {
		t.Fatal("tên mới phải thắng")
	}
	if envAny(envOf(map[string]string{port.EnvOperatorTenant: "cu"}), EnvOperatorTenant, port.EnvOperatorTenant) != "cu" {
		t.Fatal("tên cũ phải đọc được")
	}
}

// Đóng khi lỗi: bật nhưng thiếu tenant vận hành → không khởi động; lỗi mang tên đơn vị (bí danh hay cổng).
func TestEnabledRequiresOperatorTenant_W52(t *testing.T) {
	t.Setenv("AG_ENV", "test")
	for _, unit := range []string{Unit, AliasUnit} {
		err := Run(context.Background(), quiet(), envOf(map[string]string{EnvEnabled: "1"}), unit)
		if err == nil || errors.Is(err, ErrDisabled) || !strings.Contains(err.Error(), port.EnvOperatorTenant) || !strings.HasPrefix(err.Error(), unit+":") {
			t.Fatalf("%s: muốn lỗi thiếu tenant vận hành, nhận %v", unit, err)
		}
	}
}

// Bật listener mTLS mà thiếu tệp gắn / chứng thư → không khởi động (trước khi mở DB).
func TestMTLSConfigFailsClosed_W52(t *testing.T) {
	dir := t.TempDir()
	if _, err := mtlsFromEnv(envOf(map[string]string{EnvMTLSAddr: ":8443"})); err == nil || !strings.Contains(err.Error(), EnvMTLSBindings) {
		t.Fatalf("thiếu tệp gắn phải lỗi: %v", err)
	}
	bindings := testpki.Write(t, dir, "b.json", []byte(`{"version":1,"bindings":[{"client_id":"`+ids.New().String()+`","cert_sha256":[]}]}`))
	if _, err := mtlsFromEnv(envOf(map[string]string{EnvMTLSAddr: ":8443", EnvMTLSBindings: bindings})); err == nil || !strings.Contains(err.Error(), EnvTLSCert) {
		t.Fatalf("thiếu chứng thư máy chủ phải lỗi: %v", err)
	}
	if _, err := mtlsFromEnv(envOf(map[string]string{EnvMTLSBindings: testpki.Write(t, dir, "x.json", []byte(`{"version":9}`))})); err == nil {
		t.Fatal("tệp gắn sai phải lỗi kể cả khi listener mTLS tắt")
	}
	ca := testpki.NewCA(t, "ca")
	cert, key, clientCA := testpki.ServerFiles(t, dir, ca, ca)
	cfg, err := mtlsFromEnv(envOf(map[string]string{EnvMTLSAddr: ":8443", EnvMTLSBindings: bindings, EnvTLSCert: cert, EnvTLSKey: key, EnvClientCA: clientCA}))
	if err != nil || cfg.TLS == nil || cfg.Identity == nil {
		t.Fatalf("cấu hình mTLS đủ phải nạp được: %v", err)
	}
	if cfg, err := mtlsFromEnv(envOf(nil)); err != nil || cfg.Addr != "" || cfg.Identity != nil {
		t.Fatalf("mặc định: mTLS tắt, %v", err)
	}
}
