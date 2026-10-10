package assembly

import (
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/price"
	priceport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/price/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
)

// ---------------------------------------------------------------------------
// cap/price (W11.71, ADR-0049): khung module giá. Cấu hình theo bên dùng — tương đương mục `cap/price` của
// app/KN/app.yaml trong kiến trúc v4.1 §7.2 (app.yaml của repo chưa mang cấu hình module — W1.3). Pool đơn vị
// (app_platform → r_price) cho /v1/price/*.
// ---------------------------------------------------------------------------

// KNPriceProfile là cấu hình cap/price của App Khuyến nông (Báo giá nông sản v1.3): TW xem, Tỉnh (Phòng TTTT) quản lý,
// Xã (CTV báo giá) nhập. Khung: chỉ Admin KN ghi danh mục nền — cờ "Quản trị nghiệp vụ báo giá" của Cấp Tỉnh ở W11.72.
// Department là giả định (mã phòng ban của tenant chưa chốt — Q-02; docs/kn/gia-dinh-brd-2026-10.md).
var KNPriceProfile = priceport.Profile{
	Name:       "kn",
	Department: "thong_tin_truyen_thong",
	Roles:      map[string]string{authz.KNTW: "TW", authz.KNTinh: "TINH", authz.KNXa: "XA"},
	ManageBy:   []string{authz.KNAdmin},
}

// wirePrice dựng pf/price trên pool đơn vị.
func wirePrice(p *pgxpool.Pool, clk clock.Clock) (*price.Module, error) {
	return price.New(p, clk, []priceport.Profile{KNPriceProfile})
}
