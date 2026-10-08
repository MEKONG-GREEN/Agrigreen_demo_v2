package main

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os/exec"
	"strings"
	"testing"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/cmd/integration-gateway/assembly"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/cmd/integration-gateway/gateway"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/apiclient/port"
)

// ADR-0022 mục 1, ADR-0033: cờ bật ≠ "1" → tiến trình thoát lỗi ngay, không mở cổng.
func TestDisabledFlagExits_ADR0033(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	for _, v := range []string{"", "0", "true"} {
		if err := run(context.Background(), log, func(k string) string {
			if k == gateway.EnvEnabled || k == port.EnvEnabled {
				return v
			}
			return ""
		}); !errors.Is(err, gateway.ErrDisabled) {
			t.Fatalf("cờ bật %q phải thoát với ErrDisabled, nhận %v", v, err)
		}
	}
}

// Đóng khi lỗi: bật nhưng thiếu tenant vận hành → không khởi động; lỗi mang tên đơn vị integration-gateway.
func TestEnabledRequiresOperatorTenant_ADR0033(t *testing.T) {
	t.Setenv("AG_ENV", "test")
	err := run(context.Background(), slog.New(slog.NewTextHandler(io.Discard, nil)), func(k string) string {
		if k == gateway.EnvEnabled {
			return "1"
		}
		return ""
	})
	if err == nil || !strings.HasPrefix(err.Error(), gateway.Unit+":") || !strings.Contains(err.Error(), gateway.EnvOperatorTenant) {
		t.Fatalf("muốn lỗi thiếu tenant vận hành của %s, nhận %v", gateway.Unit, err)
	}
}

// Đóng khi lỗi: thiếu sổ đo lường / hạn mức / cấp quyền → không khởi động.
func TestAssembleRequiresMeteringAndGrants_ADR0022(t *testing.T) {
	if _, err := assembly.Assemble(assembly.Config{Replicas: 1}, assembly.Ports{}, nil); !errors.Is(err, assembly.ErrNotWired) {
		t.Fatalf("muốn ErrNotWired, nhận %v", err)
	}
}

// RB-3, ADR-0022 mục 1 (X-03): nhị phân cổng không kéo kho định danh (kể cả client), vùng mkg, hay
// handler/app của pf/party — kể cả gián tiếp (archlint noVaultRoots chỉ kiểm import trực tiếp). Cấp quyền dp_*
// đi qua bộ điều hợp chỉ đọc party/port/grantlookup.
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
	for _, dep := range strings.Fields(string(out)) {
		rel, ok := strings.CutPrefix(dep, mod)
		if !ok {
			continue
		}
		if strings.HasPrefix(rel, "vault/") || strings.HasPrefix(rel, "mkg/") || rel == "platform/party" ||
			strings.HasPrefix(rel, "platform/party/app") || strings.HasPrefix(rel, "platform/party/handler") {
			t.Errorf("integration-gateway kéo gián tiếp %s", dep)
		}
	}
}
