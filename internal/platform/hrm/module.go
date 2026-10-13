// Package hrm là module nghiệp vụ pf/hrm (`dom/hrm`, kind domain — ADR-0050): nhân sự của Phòng Hành chính – Tổng hợp
// (BRD HCTH v3.0). Chủ duy nhất schema pf_hrm.
//
// W11.21 (pf 000147) — khung module + DANH MỤC NHÂN SỰ (FN-19, UR-ADM-08), không dữ liệu cá nhân:
//   - Sổ pf_hrm.catalog_item: chức vụ, ngạch/CDNN, trình độ, nhóm chuyên ngành theo trình độ, chuyên ngành và các danh
//     sách khai báo sẵn; mỗi dòng là một phiên bản, ngừng sử dụng = phiên bản 'retired' (BR-01).
//   - Sổ pf_hrm.grade_scale: bảng hệ số theo bậc có ngày hiệu lực (BR-02, AC-02).
//   - Sổ pf_hrm.rule_param: quy tắc nâng bậc, bảng quy đổi thành tích — tham số có phiên bản, ngày hiệu lực.
//   - Dữ liệu mặc định từ BRD; phần BRD chưa có là MẪU (placeholder) chờ Q-070.
//   - Cấu hình quyền (port.Access) ở gốc lắp ráp; cấm float trong toàn module (agnofloat).
//   - HTTP: /v1/hrm/* (Application API).
//
// W11.22 (pf 000150, vault 000004) — HỒ SƠ NHÂN SỰ (FN-07, FN-08, FN-20): sổ hồ sơ (chủ thể vault, mã tự sinh), quá
// trình có hiệu lực, bậc = phép chiếu sự kiện, truy vấn as-of, nhật ký xem/xuất, ma trận quyền hồ sơ. PII chỉ ở kho định
// danh (định danh + ngăn hrm.profile; thẻ Thống kê BC Sở ở ngăn hrm.sensitive có khóa/mục đích riêng). Bật bằng
// UseRecords + cờ AG_HRM_RECORDS (mặc định tắt ở production — Q-005).
//
// Gói sau của cùng module: W11.24 kho tài liệu, W11.25 nâng bậc, W11.26 đánh giá, W11.28
// báo cáo Sở. Module khác chỉ import hrm/port (R-02). Tệp này là gốc lắp ráp cho cmd/api-platform.
package hrm

import (
	"github.com/gin-gonic/gin"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/hrm/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/hrm/handler"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/hrm/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/db"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/vault/identity/client"
)

// DBModule: giao dịch của module chạy SET LOCAL ROLE r_hrm (ADR-0028).
var DBModule = app.DBModule

// Module là pf/hrm đã nối.
type Module struct{ svc *app.Service }

// New tạo module trên pool b — vai đăng nhập của đơn vị (app_platform), là thành viên r_hrm.
func New(b tenant.Beginner, clk clock.Clock, acc port.Access) (*Module, error) {
	svc, err := app.NewService(db.ForModule(b, DBModule), clk, acc)
	if err != nil {
		return nil, err
	}
	return &Module{svc: svc}, nil
}

// UseRecords bật hồ sơ nhân sự (W11.22): kho định danh (cần phần mở rộng client.Profiles — vault 000004), danh mục đơn
// vị nội bộ (nil → chỉ kiểm dạng mã), cờ bật (port.EnvRecords, RecordsEnabled). Chưa gọi → route hồ sơ trả
// hrm.records_disabled.
func (m *Module) UseRecords(v client.Client, u port.Units, enabled bool) {
	m.svc.UseRecords(v, u, enabled)
}

// RecordsEnabled đọc cờ bật hồ sơ (port.EnvRecords): "1" bật, "0" tắt; vắng → tắt ở production, bật ở môi trường khác.
func RecordsEnabled(getenv func(string) string, production bool) bool {
	switch getenv(port.EnvRecords) {
	case "1":
		return true
	case "0":
		return false
	}
	return !production
}

// Catalog cho module khác: danh mục nhân sự, bảng hệ số và tham số có hiệu lực theo ngày.
func (m *Module) Catalog() port.Catalog { return m.svc }

// RegisterApp gắn route Application API /v1/hrm/*.
func (m *Module) RegisterApp(rg *gin.RouterGroup) { handler.Handler{Svc: m.svc}.RegisterApp(rg) }

// RegisterAdmin: không có route Admin API.
func (m *Module) RegisterAdmin(*gin.RouterGroup) {}

// Directory cho pf/contact (W11.23): cán bộ thuộc tỉnh — mã tham chiếu kho, đơn vị, chức vụ; tôn trọng AG_HRM_RECORDS.
func (m *Module) Directory() port.Directory { return m.svc }
