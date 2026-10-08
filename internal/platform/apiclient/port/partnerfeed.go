package port

// Feed cấp phép lớp partner theo bên cấp (W5.5 — ADR-0033 §2, §5; ADR-0020): cooperative_directory, plot_directory
// thay GET /v1/partner/cooperatives, /plots. Tập dữ liệu dựng MỘT lần cho mỗi bên cấp (partition_key = tenant bên
// cấp) trong job pf/apiclient (cmd/worker) và phát cho mọi bên nhận đang có cấp quyền loại = grant_kind của hợp
// đồng; cổng kiểm cấp quyền (party/port.GrantLookup — hai phía, thu hồi có hiệu lực ở lời gọi kế tiếp) và ghi sổ
// đọc partner_read_log ở tenant bên cấp cho MỖI trang (K-05). Không lọc từng hàng theo bên nhận.

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// Tên feed lớp partner có nguồn ở W5.5 (hợp đồng contracts/integration/feeds/<tên>.v1.json, grant_kind = tên).
const (
	FeedCooperativeDirectory = "cooperative_directory"
	FeedPlotDirectory        = "plot_directory"
)

// PartnerFeedRefresh: mỗi bản ghi còn hiệu lực của feed lớp partner được phát lại (upsert cùng nội dung, event_id
// mới) khi lần phát gần nhất đã quá hạn này — để phần feed còn giữ (30 ngày, R-INT-06) luôn chứa trạng thái ĐẦY
// ĐỦ: bên nhận đọc từ đầu (hoặc đồng bộ lại sau 410) dựng được bản sao hoàn chỉnh mà không cần ảnh chụp. Phải nhỏ
// hơn hạn giữ trừ chu kỳ dựng.
const PartnerFeedRefresh = 7 * 24 * time.Hour

// PartnerSource: module chủ dữ liệu trả MỌI bản ghi hiện hành (đúng hợp đồng feed dataset) của tenant bên cấp
// grantor, đọc bằng pool vai của chính nó (R-05). Kiểu trả về chỉ thư viện chuẩn nên module kernel mà pf/apiclient
// phụ thuộc (pf/party — party/port/grantlookup) cài đặt được mà không import gói này. Tập không hỗ trợ → lỗi.
type PartnerSource interface {
	PartnerRecords(ctx context.Context, dataset string, grantor uuid.UUID) ([]json.RawMessage, error)
}

// PartnerGrantors liệt kê mọi bên cấp đang có cấp quyền loại kind cho bên nhận grantee tại at (party/port/grantlookup
// ActiveGrantors — hai phía, chỉ loại dp_* và party/port.FeedKinds).
type PartnerGrantors interface {
	ActiveGrantors(ctx context.Context, grantee uuid.UUID, kind string, at time.Time) ([]uuid.UUID, error)
}
