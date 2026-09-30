package acctbridge

import (
	"context"
	"os"
	"strconv"
	"strings"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/acctbridge/adapter"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/acctbridge/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/objstore"
)

func unconfigured() port.LedgerAdapter {
	return adapter.Unconfigured{Reason: "không có bộ điều hợp"}
}

// AdapterFromEnv chọn bộ điều hợp phần mềm kế toán theo biến môi trường (docs/dev/acctbridge-adapter.md):
//
//	AG_ACCTBRIDGE_ADAPTER        file_export (mặc định) | fake (chỉ dev/kiểm thử — nhận giả, KHÔNG dùng ở môi trường thật)
//	AG_ACCTBRIDGE_EXPORT_PREFIX  tiền tố khóa đối tượng của bộ xuất tệp (mặc định "acctbridge")
//	AG_S3_* | AG_OBJSTORE_DIR    kho đối tượng trong lãnh thổ (shared/objstore.FromEnv, RB-7)
//
// Thiếu kho đối tượng → adapter.Unconfigured (luôn tạm không nhận: bút toán đã duyệt được giữ, thử lại rồi
// vào DLQ + cảnh báo). Trả thêm mô tả để ghi log khởi động (không có bí mật).
func AdapterFromEnv(ctx context.Context, clk clock.Clock) (port.LedgerAdapter, string) {
	switch strings.TrimSpace(os.Getenv("AG_ACCTBRIDGE_ADAPTER")) {
	case "fake":
		return adapter.NewFake(clk), adapter.NameFake
	case "", "file_export":
	default:
		return adapter.Unconfigured{Reason: "AG_ACCTBRIDGE_ADAPTER không hợp lệ"}, adapter.NameUnconfigured
	}
	st, err := objstore.FromEnv(ctx)
	if err != nil {
		return adapter.Unconfigured{Reason: "chưa cấu hình kho đối tượng"}, adapter.NameUnconfigured
	}
	return &adapter.FileExport{Store: st, Prefix: os.Getenv("AG_ACCTBRIDGE_EXPORT_PREFIX"), Clock: clk}, adapter.NameFileExport
}

// RetryMaxFromEnv đọc AG_ACCTBRIDGE_PUSH_RETRY_MAX (acctbridge.push.retry_max; 0 = mặc định 8 — tạm, chờ khách).
func RetryMaxFromEnv() int {
	n, err := strconv.Atoi(strings.TrimSpace(os.Getenv("AG_ACCTBRIDGE_PUSH_RETRY_MAX")))
	if err != nil || n < 1 || n > 50 {
		return 0
	}
	return n
}
