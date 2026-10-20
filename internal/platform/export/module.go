// Package export là năng lực dùng chung pf/export (`cap/export`, kind capability — W10.17, ADR-0051; tách theo điều kiện
// W6.6 (i) của docs/dev/modules.md: mẫu văn bản có phiên bản cần cho ứng dụng thứ hai). Chủ duy nhất schema pf_export.
//
//   - Mẫu văn bản (internal/shared/doctpl): khóa + phiên bản, biến, khuôn câu, mục, lặp, bảng. Module bên dùng đăng ký mẫu
//     qua port.TemplateRegistration ở gốc lắp ráp; phiên bản dùng lần đầu trong tenant được ghi sổ template_version, đổi
//     nội dung mà không tăng phiên bản bị từ chối (export.template_drift).
//   - Bộ ghi .docx tất định (internal/shared/docx): cùng mẫu + dữ liệu → cùng byte.
//   - Lần xuất ghi sổ export_run chỉ ghi thêm: byte bất biến + SHA-256 (DB tính lại), băm mẫu, băm dữ liệu vào, số văn bản.
//   - Số văn bản tự cấp "{n}/<ký hiệu>" theo năm (sổ doc_number, n = seq của luồng — không trùng, không hở) và dòng
//     "<địa danh>, ngày dd tháng mm năm yyyy" theo giờ Việt Nam khi bên dùng để trống.
//
// Bên dùng: KHTC Mẫu 1–5 (pf/extension, W10.17); CNTS BM-11, HCTH MS05/06, Mẫu KNQG gọi cùng port ở gói sau. Các đường xuất
// bất đồng bộ cũ (pf/reports, pf/mrv, pf/apiclient) và tải đồng bộ chưa chuyển sang module này (ADR-0051 §Hệ quả).
//
// HTTP: /v1/exports/* (Application API, chỉ đọc, quyền do port.Access của gốc lắp ráp). Module khác chỉ import export/port.
package export

import (
	"github.com/gin-gonic/gin"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/export/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/export/handler"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/export/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/db"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// DBModule: giao dịch của module chạy SET LOCAL ROLE r_export (ADR-0028).
var DBModule = app.DBModule

// Deps là phụ thuộc do gốc lắp ráp nối.
type Deps struct {
	Access    port.Access
	Templates []port.TemplateRegistration
}

// Module là pf/export đã nối.
type Module struct{ svc *app.Service }

// New tạo module trên pool b (vai đăng nhập của đơn vị, thành viên r_export). Mẫu sai → lỗi khởi động.
func New(b tenant.Beginner, clk clock.Clock, deps Deps) (*Module, error) {
	svc, err := app.NewService(db.ForModule(b, DBModule), clk, deps.Access, deps.Templates)
	if err != nil {
		return nil, err
	}
	return &Module{svc: svc}, nil
}

// Exporter trả năng lực xuất cho module bên dùng.
func (m *Module) Exporter() port.Exporter { return m.svc }

// RegisterApp gắn /v1/exports/*.
func (m *Module) RegisterApp(rg *gin.RouterGroup) { handler.Handler{Svc: m.svc}.RegisterApp(rg) }

// RegisterAdmin: không có route Admin API.
func (m *Module) RegisterAdmin(*gin.RouterGroup) {}
