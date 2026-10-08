package port

// Bộ điều hợp FEED của danh bạ HTX/thửa (W5.5 — ADR-0033 §2, §5; kiến trúc v4.1 §41.4, R-INT-02): MKG đọc
// GET /v1/feeds/cooperative_directory, /plot_directory trên cổng tích hợp như mọi đối tác (khóa API, hạn mức, sổ đo
// lường, sổ đọc của bên cấp) thay /v1/partner/cooperatives, /plots (PlatformDirectory). Chạy song song một kỳ phát
// hành; W5.10 gỡ client mkg_sync.

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
)

// JobDirectoryFeed là tên job kéo feed danh bạ HTX/thửa và đối soát với bản sao cũ (AG_MKG_JOBS).
const JobDirectoryFeed = "fieldops_directory_feed"

// Tên feed (hợp đồng contracts/integration/feeds/<tên>.v1.json).
const (
	FeedCooperativeDirectory = "cooperative_directory"
	FeedPlotDirectory        = "plot_directory"
)

// DirectoryFeeds là các feed danh bạ, theo thứ tự kéo.
var DirectoryFeeds = []string{FeedCooperativeDirectory, FeedPlotDirectory}

// Nguồn đối chiếu danh bạ của lập/sửa lệnh gom (AG_FIELDOPS_DIRECTORY_SOURCE).
const (
	DirectorySourcePartner = "partner" // bản sao cũ qua /v1/partner/* (mặc định tới khi đối soát khớp)
	DirectorySourceFeed    = "feed"    // bản sao dựng từ feed của cổng tích hợp
)

// DirectoryFeedItem là một mục của trang feed (phong bì feed_page.v1).
type DirectoryFeedItem struct {
	Seq        int64
	EventID    uuid.UUID
	EntityKey  string
	Op         string          // upsert | retract
	Record     json.RawMessage // nil khi retract
	RecordedAt time.Time
}

// DirectoryFeedPage là một trang feed đã giải phong bì.
type DirectoryFeedPage struct {
	Feed            string
	ContractVersion int
	NextCursor      string
	HasMore         bool
	AsOf            time.Time
	MaxAge          time.Duration // freshness.max_age của hợp đồng (0 = không khai)
	Items           []DirectoryFeedItem
}

// DirectoryFeed đọc một trang feed sau cursor (rỗng = từ đầu phần feed còn giữ). Lỗi:
//   - ErrFeedGrantMissing: cổng trả 403 party.share_grant_missing (chưa cấp / đã thu hồi / hết hạn);
//   - ErrFeedCursorExpired: cursor quá hạn giữ 30 ngày (410) — đồng bộ lại từ đầu;
//   - ErrDirectoryUnavailable (bọc): mạng, 401, 403 khác, 429, 5xx, phong bì sai — thử lại lượt sau.
type DirectoryFeed interface {
	Page(ctx context.Context, feed, cursor string, limit int) (DirectoryFeedPage, error)
}

// Lỗi của DirectoryFeed.
var (
	ErrFeedGrantMissing  = errors.New("fieldops: bên cấp chưa cấp / đã thu hồi cấp quyền danh bạ (party.share_grant_missing)")
	ErrFeedCursorExpired = errors.New("fieldops: cursor feed đã quá hạn giữ (integration.cursor_expired)")
)
