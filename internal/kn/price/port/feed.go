package port

// Tập dữ liệu bảng giá ngày công khai qua cổng tích hợp (W11.81 — BRD Báo giá nông sản v1.3 FN-TI-02.07 Đợt 2, UR-TI-07;
// doi-chieu D4 BG11; hợp đồng contracts/integration/feeds/price_board_daily.v1.json). Thay API công khai không xác thực
// /api/public/prices/latest của prototype: website và đối tác đọc qua cổng tích hợp (khóa API HMAC, đăng ký feed bốn mắt,
// sổ đo lường + egress_event). pf/price CHỈ trả bản ghi của phiên bản đã duyệt hiện hành (đã công bố) — không giá theo xã,
// không phiếu, không cán bộ, không ngày chưa duyệt; job dựng feed của pf/apiclient (cmd/worker) gọi qua gốc lắp ráp.

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// FeedPriceBoardDaily là tên tập dữ liệu (= tên hợp đồng tích hợp) của bảng giá ngày đã duyệt.
const FeedPriceBoardDaily = "price_board_daily"

// PublicBoardFeed trả MỌI bản ghi hiện hành (đúng hợp đồng tích hợp dataset) của tenant đơn vị công bố publisher, đọc bằng
// vai của pf/price (R-05). publisherCode là mã tỉnh của đơn vị (2 số) — đi vào bản ghi và entity_key. Kiểu trả về chỉ thư
// viện chuẩn để pf/apiclient không import pf/price (nối ở gốc lắp ráp). Tập không hỗ trợ → ErrUnknownPublicDataset.
type PublicBoardFeed interface {
	PublishedRecords(ctx context.Context, dataset string, publisher uuid.UUID, publisherCode string) ([]json.RawMessage, error)
}

// ErrUnknownPublicDataset: pf/price không có tập dữ liệu công khai tên này, hoặc mã tỉnh của đơn vị công bố sai dạng.
var ErrUnknownPublicDataset = problem.New(http.StatusNotFound, "price.unknown_public_dataset",
	"pf/price không có tập dữ liệu công khai này hoặc mã đơn vị công bố sai dạng")
