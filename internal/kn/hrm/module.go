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
// W11.28 (pf 20261014073846) — BÁO CÁO SỞ NS01–NS06 (FN-12) + DASHBOARD NHÂN SỰ (FN-06): biểu tại ngày chốt (as-of),
// quy tắc kiểm chéo, nhấn ô xem người, sổ xuất (lịch sử + trường nhạy cảm), tệp Excel dựng lại được (SHA-256).
//
// W11.24 (pf 20261015083926) — KHO TÀI LIỆU NHÂN SỰ (FN-14): cây thư mục, phân loại, quyền thư mục (ghi đè ma trận FN-20
// cho docs.view/docs.manage), tệp qua pf/evidence (mục đích hr_records, khóa của chủ thể "kho tài liệu" ở kho định danh),
// xóa = ẩn, nhật ký xem trước/tải xuống. Bật bằng UseDocs; tài liệu gắn nhân viên theo cờ AG_HRM_RECORDS.
//
// W11.25 (pf 20261021122821) — NÂNG BẬC LƯƠNG (FN-09, FN-10): kỳ NLTX/TNVK/NLTH tính từ mốc xếp bậc + bảng hệ số + tham
// số, xếp loại năm, trạng thái xử lý, xuất Mẫu số 1/2/4 (Excel, dựng lại + SHA-256), sổ quyết định (áp bậc cùng giao dịch,
// hủy = sự kiện bù), job hrm_promotion_scan (module_promotion.go).
//
// W11.27 (pf 20261024091030) — KẾ HOẠCH NHÂN LỰC NĂM (FN-11, D3 H7): profile "hrm_plan" của cap/forms (đơn vị lá = đơn vị
// nội bộ), số hiện có/dự kiến nghỉ hưu tự tính từ hồ sơ tại ngày chốt, nháp và điều chỉnh sau chốt ở sổ pf_hrm.
//
// Gói sau của cùng module: W11.26 đánh giá. Module khác chỉ import hrm/port (R-02). Tệp này là gốc lắp ráp cho cmd/api-platform.
package hrm

import (
	"log/slog"

	"github.com/gin-gonic/gin"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/hrm/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/hrm/handler"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/hrm/port"
	evport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/evidence/port"
	formsport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/db"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/meter"
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

// UseEgress nối điểm kiểm soát xuất pf/metering cho lần tải tệp báo cáo Sở (W11.28, ADR-0024: kênh report_export, xuất
// nội bộ → luôn observe). nil → không đo.
func (m *Module) UseEgress(cp meter.Checkpoint, mode meter.Mode, log *slog.Logger) {
	m.svc.Rep.Egress, m.svc.Rep.EgressMode, m.svc.Rep.Log = cp, mode, log
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

// UseDocs bật kho tài liệu nhân sự (W11.24, FN-14): tệp qua pf/evidence (mục đích hr_records). Gọi sau UseRecords (kho định
// danh giữ khóa kho tài liệu); gốc lắp ráp nối thêm FileResolver cho pf/evidence (chủ thể 'pf/hrm' / 'hr_folder').
func (m *Module) UseDocs(f evport.Files) { m.svc.UseDocs(f) }

// FileResolver là Resolver của pf/hrm cho pf/evidence (W11.24).
func (m *Module) FileResolver() evport.Resolver { return app.FileResolver{S: m.svc} }

// Catalog cho module khác: danh mục nhân sự, bảng hệ số và tham số có hiệu lực theo ngày.
func (m *Module) Catalog() port.Catalog { return m.svc }

// RegisterApp gắn route Application API /v1/hrm/*.
func (m *Module) RegisterApp(rg *gin.RouterGroup) { handler.Handler{Svc: m.svc}.RegisterApp(rg) }

// RegisterAdmin: không có route Admin API.
func (m *Module) RegisterAdmin(*gin.RouterGroup) {}

// FormProfiles trả cấu hình cap/forms do pf/hrm sở hữu (W11.27: "hrm_plan" — kế hoạch nhân lực năm, đơn vị lá = đơn vị nội
// bộ). Gốc lắp ráp nối vào forms.Deps.Profiles cùng PlanValidator.
func FormProfiles(acc port.Access) []formsport.Profile {
	return []formsport.Profile{app.PlanFormProfile(acc)}
}

// PlanValidator là quy tắc chặn bài nộp của profile hrm_plan (forms.Deps.Validators[port.PlanProfile]).
func PlanValidator() formsport.Validator { return app.PlanValidator{} }

// UsePlans bật kế hoạch nhân lực năm (W11.27, FN-11): cổng phát hành, điều khiển kỳ và nhập/rà soát của cap/forms.
func (m *Module) UsePlans(pub formsport.Publisher, ctl formsport.PeriodControl, entry formsport.Entry) {
	m.svc.UsePlans(app.PlanDeps{Forms: pub, Control: ctl, Entry: entry})
}

// Directory cho pf/contact (W11.23): cán bộ thuộc tỉnh — mã tham chiếu kho, đơn vị, chức vụ; tôn trọng AG_HRM_RECORDS.
func (m *Module) Directory() port.Directory { return m.svc }
