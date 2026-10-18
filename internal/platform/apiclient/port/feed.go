package port

// Feed và tập dữ liệu dựng sẵn của cổng tích hợp (W5.3 — R-INT-05, R-INT-06; ADR-0033 §2, ADR-0035).
//
// Kho feed chỉ ghi thêm thuộc pf/apiclient (schema pf_apiclient, pf 000126–000127): cursor = seq tăng đơn điệu
// mỗi feed, khóa thực thể, op upsert/retract, giữ 30 ngày. Tập tổng hợp dựng MỘT lần mỗi chu kỳ trong job của
// pf/apiclient (cmd/worker) từ ô THÔ do module chủ dữ liệu trả qua AggregateSource; quy tắc k (ẩn sơ cấp, dòng
// province_withheld, ẩn ô bù, trễ ẩn k + hysteresis) áp ở đây — một chỗ duy nhất, không ở module nguồn.
// Cổng (cmd/integration-gateway) phục vụ GET /v1/feeds/{feed}?cursor= và GET /v1/snapshots/{dataset}/{version}.

import (
	"context"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// Tên feed/tập dữ liệu có nguồn ở W5.3 (hợp đồng contracts/integration/feeds/<tên>.v1.json).
const (
	FeedHarvestProgress  = "harvest_progress_by_commune"
	FeedCollectionVolume = "collection_volume_by_commune"
	FeedAdminUnits       = "admin_units"
)

// FeedScope trả scope đọc feed (và tải ảnh chụp) của khách hàng API: "feed:<tên>:read". Đăng ký sản phẩm
// (subscription) dùng tên feed làm product.
func FeedScope(feed string) string { return "feed:" + feed + ":read" }

// Job của cmd/worker (-jobs, chạy ở leader).
const (
	// JobFeedBuild dựng một phiên bản mỗi tập dữ liệu có nguồn (chu kỳ EnvFeedBuildInterval, mặc định 15 phút —
	// §42 trong vụ). Không có thay đổi → không phiên bản mới.
	JobFeedBuild = "apiclient_feed_build"
	// JobFeedRetention dọn mục feed đã quá 30 ngày (pf_apiclient.purge_feed_entries), mỗi giờ.
	JobFeedRetention = "apiclient_feed_retention"
)

// Biến môi trường của feed.
const (
	// EnvAllowUnapproved: "1" cho cổng phục vụ hợp đồng tích hợp CHƯA approved (proposed/draft) — CHỈ ngoài
	// production (dev/test/UAT). Production: cổng từ chối khởi động nếu đặt, và mọi lời gọi tới hợp đồng chưa
	// approved trả 403 integration.contract_not_servable.
	EnvAllowUnapproved = "AG_GATEWAY_FEED_ALLOW_PROPOSED"
	// EnvContributors: mã tenant chủ dữ liệu đóng góp vào tập tổng hợp, phân tách bằng dấu phẩy (ADR-0035 §1 —
	// tạm, tới khi pf/party có cấp phép grant_class = aggregate; ĐTK 1: một tenant chương trình, H-1 (a)).
	EnvContributors = "AG_INTEGRATION_CONTRIBUTORS"
	// EnvFeedBuildInterval: chu kỳ dựng (time.ParseDuration, ≥ 1m), mặc định DefaultFeedBuildInterval.
	EnvFeedBuildInterval = "AG_INTEGRATION_BUILD_INTERVAL"
)

// Giới hạn feed.
const (
	FeedRetentionDays        = 30
	DefaultFeedPageLimit     = 500
	MaxFeedPageLimit         = 1000
	SnapshotURLTTL           = 15 * time.Minute // ≤ objstore.MaxPresignTTL (ADR-0033 §2)
	DefaultFeedBuildInterval = 15 * time.Minute
)

// AggregateCell là một ô THÔ (chưa ẩn) của tập tổng hợp — module chủ dữ liệu trả, trên tenant của chính nó.
// Units là mã đơn vị đếm (nông hộ) để đếm PHÂN BIỆT; chỉ dùng trong bộ nhớ của job dựng, không lưu, không ghi
// log, không bao giờ ra feed. Measures là chỉ số cộng được (decimal), theo tên trường hợp đồng.
type AggregateCell struct {
	ProvinceCode       string // 2 chữ số
	ProvinceValidFrom  string // YYYY-MM-DD: phiên bản tỉnh tại admin_unit_as_of (ADR-0032)
	CommuneCode        string // 5 chữ số
	AdminUnitValidFrom string // YYYY-MM-DD: phiên bản xã tại admin_unit_as_of
	SeasonCode         string // "<năm>-<mã vụ>", vd 2026-HT
	SeasonYear         int
	PeriodMonth        string // YYYY-MM (collection_volume_by_commune); rỗng với tiến độ gặt
	Units              []string
	Measures           map[string]decimal.Decimal
	// DateFrom / DateTo: khoảng ngày (YYYY-MM-DD) của ô — tiến độ gặt: ngày gặt dự kiến của phần CHƯA gặt.
	DateFrom, DateTo string
}

// Tên chỉ số của AggregateCell.Measures.
const (
	MeasureAreaSown      = "area_ha_sown"
	MeasureAreaHarvested = "area_ha_harvested"
	MeasureRolls         = "rolls_collected"
	MeasureAreaCollected = "area_ha_collected"
)

// AggregateSource: module chủ dữ liệu (pf/cropcycle) trả mọi ô thô của tập dataset trên tenant dataTenant tại
// asOf (đọc bằng pool vai của chính nó — R-05). Tập không hỗ trợ → ErrUnknownFeed.
type AggregateSource interface {
	AggregateCells(ctx context.Context, dataset string, dataTenant uuid.UUID, asOf time.Time) ([]AggregateCell, error)
}

// Contributors trả các tenant chủ dữ liệu có đóng góp đang hiệu lực vào tập dataset tại at (ADR-0035 §1).
type Contributors interface {
	Contributors(ctx context.Context, dataset string, at time.Time) ([]uuid.UUID, error)
}

// StaticContributors là Contributors cố định (EnvContributors) — tạm tới khi có cấp phép aggregate ở pf/party.
type StaticContributors []uuid.UUID

// Contributors cài đặt Contributors.
func (s StaticContributors) Contributors(context.Context, string, time.Time) ([]uuid.UUID, error) {
	return append([]uuid.UUID(nil), s...), nil
}

// Lỗi của feed (docs/errors.md, mục pf/apiclient — integration.*).
var (
	// ErrUnknownFeed: không có hợp đồng feed hợp lệ với tên/phiên bản này (mặc định từ chối).
	ErrUnknownFeed = problem.New(http.StatusNotFound, "integration.unknown_feed", "Không có feed này")
	// ErrContractNotServable: hợp đồng tích hợp (feed, nạp theo lô, song phương) không phục vụ ở môi trường này —
	// chưa approved (proposed/draft/retired), approved còn pending, hoặc loại chưa phục vụ ở cổng. Production không
	// phục vụ hợp đồng proposed; ngoài production cần cờ dev/test (AG_GATEWAY_FEED_ALLOW_PROPOSED /
	// AG_GATEWAY_INGEST_ALLOW_PROPOSED). ĐỊNH NGHĨA DUY NHẤT của mã integration.contract_not_servable:
	// ingest.ErrContractNotServable và ErrNotServable của pf/cooperative (bilateral) là bí danh của biến này.
	ErrContractNotServable = problem.New(http.StatusForbidden, "integration.contract_not_servable",
		"Hợp đồng tích hợp chưa được duyệt hoặc không phục vụ ở môi trường này")
	// ErrCursorExpired: cursor trỏ vào phần feed đã quá hạn giữ 30 ngày — đồng bộ lại từ ảnh chụp (trường
	// snapshot của phản hồi) rồi đọc tiếp.
	ErrCursorExpired = problem.New(http.StatusGone, "integration.cursor_expired", "Cursor đã quá hạn giữ của feed")
	// ErrInvalidCursor: cursor sai dạng hoặc vượt đầu feed.
	ErrInvalidCursor = problem.New(http.StatusBadRequest, "integration.invalid_cursor", "Cursor không hợp lệ")
	// ErrSnapshotNotFound: phiên bản không có ảnh chụp (hoặc không có phiên bản).
	ErrSnapshotNotFound = problem.New(http.StatusNotFound, "integration.snapshot_not_found", "Không có ảnh chụp cho phiên bản này")
	// ErrKBelowFloor: tham số k (publicdata.k_min) nhỏ hơn k của hợp đồng — job dựng từ chối chạy (ADR-0035 §3).
	ErrKBelowFloor = problem.New(http.StatusUnprocessableEntity, "integration.k_below_floor",
		"Ngưỡng k nhỏ hơn mức tối thiểu của hợp đồng — không dựng tập dữ liệu")
	// ErrRecordRejected: bản ghi không khớp hợp đồng tích hợp — job không ghi kho feed, cổng không trả trang.
	ErrRecordRejected = problem.New(http.StatusInternalServerError, "integration.record_rejected",
		"Bản ghi không khớp hợp đồng tích hợp — không ghi/không trả")
)
