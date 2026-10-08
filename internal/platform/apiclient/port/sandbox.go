package port

import (
	"errors"
	"strings"
)

// Môi trường thử của đối tác (W5.8; R-INT-08, ADR-0033): một TRIỂN KHAI riêng (DB riêng, dữ liệu tổng hợp của
// cmd/seed-synthetic) chạy api-platform + integration-gateway với AG_PARTNER_SANDBOX=1. Khóa API phát hành ở đó
// có tiền tố riêng "agks_" (khóa thật "agk_"): cổng thử chỉ nhận khóa thử, cổng thật từ chối khóa thử (401) —
// khóa thử không bao giờ dùng được ở production, kể cả khi lộ. Production (residency.Production) từ chối khởi
// động khi đặt cờ.

const (
	// EnvSandbox: "1" bật chế độ thử của đối tác (api-platform phát hành khóa agks_, cổng chỉ nhận khóa agks_).
	EnvSandbox = "AG_PARTNER_SANDBOX"
	// SandboxKeyTokenPrefix: tiền tố khóa API của môi trường thử.
	SandboxKeyTokenPrefix = "agks_"
	// HeaderEnvironment: mọi phản hồi của cổng thử mang AG-Environment: sandbox.
	HeaderEnvironment  = "AG-Environment"
	EnvironmentSandbox = "sandbox"
)

// ErrSandboxInProduction: cờ thử đặt ở production — không khởi động (đóng khi lỗi).
var ErrSandboxInProduction = errors.New("apiclient: " + EnvSandbox + " chỉ cho môi trường thử của đối tác — production từ chối chế độ thử")

// SandboxFromEnv đọc cờ chế độ thử; production + cờ → ErrSandboxInProduction. Giá trị khác "", "0", "1" → lỗi.
func SandboxFromEnv(getenv func(string) string, production bool) (bool, error) {
	switch v := strings.TrimSpace(getenv(EnvSandbox)); v {
	case "", "0":
		return false, nil
	case "1":
		if production {
			return false, ErrSandboxInProduction
		}
		return true, nil
	default:
		return false, errors.New("apiclient: " + EnvSandbox + " phải là 0 hoặc 1")
	}
}

// IsSandboxKey: khóa trình ra có tiền tố khóa thử (không kiểm dạng đầy đủ — xem ParseKey).
func IsSandboxKey(presented string) bool { return strings.HasPrefix(presented, SandboxKeyTokenPrefix) }
