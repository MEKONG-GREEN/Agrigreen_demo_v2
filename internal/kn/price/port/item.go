package port

// Mặt hàng theo thời kỳ (W11.73, pf 000141; BRD Báo giá v1.3 FN-AD-04.03). Một danh mục mặt hàng duy nhất ở cap/price;
// KHTC và module khác đọc qua Items (đề xuất Q-082). Phiếu báo giá (W11.75) đọc danh sách mặt hàng của xã theo ngày.

import (
	"context"
	"net/http"
	"time"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// Item là một mặt hàng có hiệu lực tại một ngày.
type Item struct {
	Code         string // MH + 5 chữ số, hệ thống sinh, không tái sử dụng
	Name         string
	Variety      string // giống / phẩm cấp
	GroupCode    string
	UnitCode     string
	Spec         string
	SalePlace    string
	Kind         string // basic (Cơ bản) | local (Địa phương)
	SortOrder    int
	Version      int64
	ValidFrom    time.Time
	ReplacesCode string // "Thay thế cho"
}

// Items là danh mục mặt hàng của tenant người gọi (tenant lấy từ ctx — tenant.WithID).
type Items interface {
	// ItemsAt trả mọi mặt hàng có hiệu lực tại ngày at (giờ VN), sắp theo nhóm, thứ tự, mã.
	ItemsAt(ctx context.Context, at time.Time) ([]Item, error)
	// PointItems trả mặt hàng xã báo giá point phải báo giá tại ngày at: Cơ bản ∪ Địa phương đã gán, lọc theo bộ theo vụ
	// (FN-AD-04.03 hậu điều kiện, AC-04). point là mã xã báo giá (mặc định = mã ĐVHC xã).
	PointItems(ctx context.Context, point string, at time.Time) ([]Item, error)
}

// Lỗi mặt hàng (docs/errors.md, mục pf/price).
var (
	ErrItemNotFound        = problem.New(http.StatusNotFound, "price.item_not_found", "Không có mặt hàng này")
	ErrItemDuplicate       = problem.New(http.StatusConflict, "price.item_duplicate", "Trùng tên, giống/phẩm cấp và quy cách trong cùng nhóm hàng")
	ErrItemLocked          = problem.New(http.StatusConflict, "price.item_locked", "Mặt hàng đã có giá: không sửa đơn vị tính, quy cách, nhóm hàng, loại — tạo mặt hàng thay thế")
	ErrItemRetired         = problem.New(http.StatusConflict, "price.item_retired", "Mặt hàng đã ngưng tại ngày hiệu lực")
	ErrItemDate            = problem.New(http.StatusUnprocessableEntity, "price.item_date_invalid", "Ngày hiệu lực không hợp lệ — thay đổi danh mục áp dụng từ ngày báo giá kế tiếp")
	ErrItemBasicAssign     = problem.New(http.StatusUnprocessableEntity, "price.item_basic_not_assignable", "Mặt hàng Cơ bản áp dụng mọi xã, không gán hay bỏ gán riêng từng xã")
	ErrGroupNotFound       = problem.New(http.StatusNotFound, "price.group_not_found", "Không có nhóm hàng này")
	ErrGroupNotEmpty       = problem.New(http.StatusConflict, "price.group_not_empty", "Nhóm hàng còn mặt hàng đang hiệu lực — không xóa được")
	ErrProposalNotFound    = problem.New(http.StatusNotFound, "price.proposal_not_found", "Không có đề xuất mặt hàng này")
	ErrProposalDecided     = problem.New(http.StatusConflict, "price.proposal_decided", "Đề xuất đã được xử lý")
	ErrItemSetNotFound     = problem.New(http.StatusNotFound, "price.item_set_not_found", "Không có bộ mặt hàng này")
	ErrItemSetCodeConflict = problem.New(http.StatusConflict, "price.item_set_exists", "Mã bộ mặt hàng đã có")
)
