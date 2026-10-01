package main

import (
	"context"
	"errors"
	"io"
	"log/slog"
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
