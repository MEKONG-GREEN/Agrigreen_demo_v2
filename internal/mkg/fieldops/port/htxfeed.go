package port

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// ---------------------------------------------------------------------------
// G3-2 — phản hồi của HTX quay về MKG (thêm vào, không đổi phần đã đóng băng).
//
// Nền tảng → MKG chỉ qua Application API công khai (g3-conventions §5): job leader của
// api-internal kéo luồng GET /v1/partner/htx-collection/responses (ADR-0020) bằng tài khoản dịch
// vụ tenant MKG (client agrigreen-mkg-sync, vai mkg_sync) rồi ghi htx_confirmation kênh
// platform. Không có hàng đợi pf → mkg, không import vùng nền tảng (R-03).
// ---------------------------------------------------------------------------

// JobHTXResponses là tên job kéo phản hồi của HTX (AG_MKG_JOBS).
const JobHTXResponses = "fieldops_htx_responses"

// HTXResponse là một phản hồi của HTX với bảng kê (htx_collection_statement) trên nền tảng.
type HTXResponse struct {
	ResponseID        uuid.UUID
	Cursor            int64 // con trỏ luồng (tăng dần)
	StatementRef      uuid.UUID
	StatementRevision int
	HTXRef            uuid.UUID
	PlotRef           uuid.UUID
	MKGRolls          int // số cuộn trên bảng kê HTX đã xem
	Outcome           string
	HTXRolls          int
	Reason            *string
	RespondedAt       time.Time
}

// HTXResponsePage là một trang luồng phản hồi; Next = con trỏ cho lần kéo sau.
type HTXResponsePage struct {
	Items []HTXResponse
	Next  int64
}

// HTXResponseFeed là nguồn phản hồi của HTX (Application API công khai của nền tảng).
// Lỗi mạng / 5xx / 401 → ErrDirectoryUnavailable (job thử lại lượt sau).
type HTXResponseFeed interface {
	HTXResponses(ctx context.Context, after int64, limit int) (HTXResponsePage, error)
}

// ---------------------------------------------------------------------------
// ADR-0020 (thêm vào, không đổi phần đã đóng băng): ngày của vụ trên thửa, đọc qua bề mặt đối tác
// /v1/partner/season-harvest-dates (cấp quyền season_harvest_dates) — nguồn cho quy tắc proto
// FM-01/02 (không cuộn trước ngày gặt; hạn cuộn sau gặt) — dùng ở app/harvest.go (mkg 000012).
// ---------------------------------------------------------------------------

// SeasonDates là ngày gieo / thu hoạch của một vụ trên thửa (không nông hộ, không sản lượng).
type SeasonDates struct {
	SeasonRef           uuid.UUID
	PlotRef             uuid.UUID
	SeasonCode          string
	SeasonYear          int
	SowingDate          string // YYYY-MM-DD
	ExpectedHarvestDate string
	HarvestOpenDate     string
	Status              string // open | completed | cancelled
}

// HarvestCalendar trả các vụ của một thửa. Lỗi như PlatformDirectory.
type HarvestCalendar interface {
	PlotSeasons(ctx context.Context, plotRef uuid.UUID) ([]SeasonDates, error)
}
