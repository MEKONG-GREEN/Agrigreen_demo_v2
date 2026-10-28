package port

// Feed tham chiếu của dữ liệu ĐÃ CÔNG BỐ do module chủ trả (W11.81 — price_board_daily; ADR-0033 §2, R-INT-05).
// Khác tập tổng hợp (không k, không ô thô) và lớp partner (không cấp quyền theo bên cấp): dữ liệu đã công bố công khai
// trong nghiệp vụ (bảng giá ngày đã duyệt — BRD Báo giá v1.3 FN-TI-02.07), phát cho mọi đối tác có khóa + đăng ký feed
// bốn mắt (grant_class reference). Job dựng hỏi module chủ cho từng ĐƠN VỊ CÔNG BỐ (tenant + mã tỉnh, cấu hình
// EnvPricePublishers), gộp thành MỘT tập theo phiên bản ở tenant đơn vị vận hành.

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

// FeedPriceBoardDaily là tập bảng giá ngày đã duyệt (hợp đồng contracts/integration/feeds/price_board_daily.v1.json).
const FeedPriceBoardDaily = "price_board_daily"

// EnvPricePublishers: đơn vị công bố bảng giá, dạng "<mã tỉnh 2 số>=<tenant UUID>[,…]" (vd "89=0199…"). Rỗng → không dựng
// price_board_daily.
const EnvPricePublishers = "AG_INTEGRATION_PRICE_PUBLISHERS"

// Publisher là một đơn vị công bố: tenant chủ dữ liệu và mã tỉnh (vào bản ghi, entity_key).
type Publisher struct {
	Tenant uuid.UUID
	Code   string
}

// PublishedSource: module chủ dữ liệu (pf/price — port.PublicBoardFeed) trả MỌI bản ghi hiện hành đã công bố (đúng hợp
// đồng dataset) của đơn vị công bố, đọc bằng vai của chính nó (R-05). Chỉ kiểu thư viện chuẩn + uuid để module chủ cài
// đặt mà không import gói này.
type PublishedSource interface {
	PublishedRecords(ctx context.Context, dataset string, publisher uuid.UUID, publisherCode string) ([]json.RawMessage, error)
}

// ParsePublishers đọc giá trị dạng EnvPricePublishers. Mã tỉnh hoặc tenant trùng → lỗi.
func ParsePublishers(v string) ([]Publisher, error) {
	var out []Publisher
	seen := map[string]bool{}
	for _, part := range strings.Split(v, ",") {
		if part = strings.TrimSpace(part); part == "" {
			continue
		}
		code, id, ok := strings.Cut(part, "=")
		code, id = strings.TrimSpace(code), strings.TrimSpace(id)
		if !ok || len(code) != 2 || code[0] < '0' || code[0] > '9' || code[1] < '0' || code[1] > '9' {
			return nil, fmt.Errorf("%s: %q phải dạng <mã tỉnh 2 số>=<tenant UUID>", EnvPricePublishers, part)
		}
		t, err := uuid.Parse(id)
		if err != nil || t == uuid.Nil {
			return nil, fmt.Errorf("%s: %q không phải UUID", EnvPricePublishers, id)
		}
		if seen[code] || seen[t.String()] {
			return nil, fmt.Errorf("%s: trùng mã tỉnh hoặc tenant ở %q", EnvPricePublishers, part)
		}
		seen[code], seen[t.String()] = true, true
		out = append(out, Publisher{Tenant: t, Code: code})
	}
	return out, nil
}
