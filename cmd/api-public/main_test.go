package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"os/exec"
	"strings"
	"testing"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/cmd/integration-gateway/gateway"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/apiclient/port"
)

// ADR-0022 mục 1 (A-4, P-01): AG_PUBLIC_API_ENABLED ≠ "1" → bí danh thoát lỗi ngay, không mở cổng.
func TestDisabledFlagExits_ADR0022(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	for _, v := range []string{"", "0", "true", "yes"} {
		err := run(context.Background(), log, func(k string) string {
			if k == port.EnvEnabled {
				return v
			}
			return ""
		})
		if !errors.Is(err, gateway.ErrDisabled) {
			t.Fatalf("%s=%q phải thoát với ErrDisabled, nhận %v", port.EnvEnabled, v, err)
		}
	}
}

// W5.2 (ADR-0033 §7.5): api-public là bí danh — CÙNG thân gateway.Run, tên đơn vị api-public, cảnh báo ngừng
// dùng; đọc cả biến cũ lẫn biến mới.
func TestAliasDelegatesToGateway_W52(t *testing.T) {
	t.Setenv("AG_ENV", "test") // AG_ENV bắt buộc (residency.CheckEnv) — kiểm trước tenant vận hành
	for _, enable := range []string{port.EnvEnabled, gateway.EnvEnabled} {
		var buf bytes.Buffer
		err := run(context.Background(), slog.New(slog.NewTextHandler(&buf, nil)), func(k string) string {
			if k == enable {
				return "1"
			}
			return ""
		})
		if err == nil || !strings.HasPrefix(err.Error(), gateway.AliasUnit+":") || !strings.Contains(err.Error(), port.EnvOperatorTenant) {
			t.Fatalf("%s=1: muốn lỗi thiếu tenant vận hành của bí danh, nhận %v", enable, err)
		}
		if !strings.Contains(buf.String(), "event=deprecated_alias") {
			t.Fatalf("bí danh phải log cảnh báo ngừng dùng: %s", buf.String())
		}
	}
}

// RB-3, ADR-0022 mục 1 (X-03): bí danh không kéo kho định danh, vùng mkg, hay handler/app của pf/party — kể cả
// gián tiếp qua gói gateway; và dùng đúng gốc lắp ráp của cmd/integration-gateway.
func TestNoVaultTransitiveDeps_RB3(t *testing.T) {
	goBin, err := exec.LookPath("go")
	if err != nil {
		t.Skip("không có lệnh go trên PATH")
	}
	out, err := exec.Command(goBin, "list", "-deps", ".").Output()
	if err != nil {
		t.Fatalf("go list -deps: %v", err)
	}
	const mod = "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/"
	sawAssembly := false
	for _, dep := range strings.Fields(string(out)) {
		if strings.HasSuffix(dep, "/cmd/integration-gateway/assembly") {
			sawAssembly = true
		}
		rel, ok := strings.CutPrefix(dep, mod)
		if !ok {
			continue
		}
		if strings.HasPrefix(rel, "vault/") || strings.HasPrefix(rel, "mkg/") || rel == "platform/party" ||
			strings.HasPrefix(rel, "platform/party/app") || strings.HasPrefix(rel, "platform/party/handler") {
			t.Errorf("api-public kéo gián tiếp %s", dep)
		}
	}
	if !sawAssembly {
		t.Error("bí danh phải dùng đúng gốc lắp ráp của cmd/integration-gateway")
	}
}
