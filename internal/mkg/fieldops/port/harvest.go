package port

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// ---------------------------------------------------------------------------
// Nghiệm thu G3 — ngày gặt của thửa (proto FM-01, FM-02). Thêm vào, không đổi phần đã có.
//
// Nguồn: bề mặt chia sẻ dữ liệu xuyên tenant "partner" của nền tảng (ADR-0020), gọi
// bằng tài khoản dịch vụ tenant MKG qua Application API công khai (g3-conventions §5):
//
//	GET /v1/partner/season-harvest-dates?plot_ref=<uuid>
//	200 {"items": [{"plot_ref": "<uuid>", "season_ref": "<uuid>|null",
//	                "harvest_date": "YYYY-MM-DD", "kind": "expected|actual"}]}
//
// 404/403 (chưa có cấp quyền chia sẻ, RB-2) hoặc items rỗng → chưa biết ngày gặt:
// fieldops VẪN cho ghi nhưng gắn cờ (không bao giờ im lặng). Lỗi mạng/5xx/401 →
// ErrDirectoryUnavailable: fieldops dùng bảng đệm nếu có, không thì gắn cờ unavailable.
// ---------------------------------------------------------------------------

// JobHarvestDates là tên job làm mới bảng đệm ngày gặt của các lệnh gom đang mở (AG_MKG_JOBS).
const JobHarvestDates = "fieldops_harvest_dates"

// Loại ngày gặt.
const (
	HarvestExpected = "expected" // ngày gặt dự kiến (HTX khai trước vụ)
	HarvestActual   = "actual"   // ngày gặt thực tế (HTX khai sau khi gặt)
)

// SeasonHarvest là ngày gặt của một thửa trong một vụ.
type SeasonHarvest struct {
	PlotRef   uuid.UUID
	SeasonRef *uuid.UUID
	// HarvestDate là ngày nghiệp vụ (giờ Việt Nam) biểu diễn 00:00 UTC.
	HarvestDate time.Time
	Kind        string // expected | actual
}

// HarvestDates là nguồn ngày gặt theo vụ của một thửa (nền tảng, bề mặt partner).
// Trả danh sách rỗng hoặc ErrDirectoryNotFound khi nền tảng không có / chưa chia sẻ;
// ErrDirectoryUnavailable khi không gọi được.
type HarvestDates interface {
	SeasonHarvestDates(ctx context.Context, plotRef uuid.UUID) ([]SeasonHarvest, error)
}
