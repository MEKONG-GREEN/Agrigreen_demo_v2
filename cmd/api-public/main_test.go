package main

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os/exec"
	"strings"
	"testing"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/apiclient/port"
)

// ADR-0022 mục 1 (A-4, P-01): AG_PUBLIC_API_ENABLED ≠ "1" → tiến trình thoát lỗi ngay, không mở cổng.
func TestDisabledFlagExits_ADR0022(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	for _, v := range []string{"", "0", "true", "yes"} {
		err := run(context.Background(), log, func(k string) string {
			if k == port.EnvEnabled {
				return v
			}
			return ""
		})
		if !errors.Is(err, ErrDisabled) {
			t.Fatalf("%s=%q phải thoát với ErrDisabled, nhận %v", port.EnvEnabled, v, err)
		}
	}
}

// Đóng khi lỗi: bật nhưng thiếu AG_PUBLIC_OPERATOR_TENANT → không khởi động.
func TestEnabledRequiresOperatorTenant_ADR0022(t *testing.T) {
	err := run(context.Background(), slog.New(slog.NewTextHandler(io.Discard, nil)), func(k string) string {
		if k == port.EnvEnabled {
			return "1"
		}
		return ""
	})
	if err == nil || errors.Is(err, ErrDisabled) || !strings.Contains(err.Error(), port.EnvOperatorTenant) {
		t.Fatalf("muốn lỗi thiếu %s, nhận %v", port.EnvOperatorTenant, err)
	}
}

// Đóng khi lỗi: thiếu sổ đo lường / hạn mức / cấp quyền → không khởi động.
func TestAssembleRequiresMeteringAndGrants_ADR0022(t *testing.T) {
	if _, err := Assemble(Config{Replicas: 1}, Ports{}, nil); !errors.Is(err, ErrNotWired) {
		t.Fatalf("muốn ErrNotWired, nhận %v", err)
	}
}

// RB-3, ADR-0022 mục 1 (X-03): nhị phân api-public không kéo kho định danh (kể cả client), vùng mkg,
// hay handler/app của pf/party — kể cả gián tiếp qua gốc module (archlint noVaultRoots chỉ kiểm import
// trực tiếp). Cấp quyền dp_* đi qua bộ điều hợp chỉ đọc party/port/grantlookup.
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
			t.Errorf("api-public kéo gián tiếp %s", dep)
		}
	}
}
