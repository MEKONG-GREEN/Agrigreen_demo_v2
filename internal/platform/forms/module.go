// Package forms là module năng lực pf/forms (`cap/forms`, kind capability — kiến trúc v4.1 §26, ADR-0027, W6.2):
// biểu thu thập theo kỳ dùng chung — định nghĩa biểu có phiên bản, phát hành kỳ, nộp bài, rà soát, chốt kỳ kèm
// ẢNH CHỤP ĐÓNG BĂNG (băm SHA-256 + rule_version), mở lại kỳ là bản ghi mới. Tách từ pf/reports (mẫu có phiên bản)
// và extension/reportcycle (vòng đời kỳ KN).
//
//   - Sổ pf_forms.* chỉ ghi thêm (pf 000121), máy trạng thái kỳ và ràng buộc bài nộp ở trigger; vai r_forms
//     (pf 000122, db.ForModule) chỉ SELECT, INSERT.
//   - Cấu hình theo bên dùng (port.Profile: hierarchy, publish_by, close_by, review_by, define_by) — KN dùng qua
//     cấu hình ở gốc lắp ráp (cmd/api-platform/assembly/forms.go).
//   - Rà soát bốn mắt qua cap/workflow (workflow/port.Approvals, kind forms.submission) trong giao dịch của module.
//   - Freezer: lối ghi song song chốt/mở lại kỳ + ảnh chụp cho bên dùng có vòng đời riêng (KN extension/reportcycle,
//     route v1 cũ giữ nguyên — nguyên tắc 3), trong giao dịch của bên dùng (hàm pf_forms.mirror).
//   - HTTP: /v1/forms/* (Application API).
//   - W11.05 (pf 000140): đơn vị lá tổng quát — loại đơn vị (xã, cơ sở GM, hộ/điểm, phòng ban; người và luồng nhiều
//     bước của HCTH CHỪA CHỖ), giới hạn số lần gửi, khóa kỳ theo lịch kỳ hệ thống (sổ phụ period_spec); hủy kỳ
//     (sự kiện cancelled); trạng thái lúc chốt Không nộp / Đã chốt – chưa điều chỉnh / Chờ mở kỳ (bài offline đến sau
//     chốt giữ ở held_submission). Profile KN: kn, kn_cnts, kn_khtc (cmd/api-platform/assembly/forms.go).
//
// 14 biểu thật của KN (câu hỏi Q-01) chưa có: chỉ có biểu mẫu thử (fixture); profile production không phát hành
// được kỳ trên biểu fixture — bật production chờ Q-01.
//
// Module khác chỉ import forms/port (R-02). Tệp này là gốc lắp ráp cho cmd/api-platform.
package forms

import (
	"github.com/gin-gonic/gin"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/handler"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/port"
	syncport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/sync/port"
	wfport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/workflow/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/db"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// DBModule: giao dịch riêng của module chạy SET LOCAL ROLE r_forms (ADR-0028).
var DBModule = app.DBModule

// Deps là phụ thuộc của module.
type Deps struct {
	// Approvals: cap/workflow cho rà soát bốn mắt (nil → chỉ CHECK review_four_eyes ở DB).
	Approvals wfport.Approvals
	// Profiles: cấu hình của từng bên dùng (KN: hierarchy [TW, TINH, XA], publish_by/close_by TINH).
	Profiles []port.Profile
	// W11.42: danh mục cơ sở giết mổ (đơn vị lá cơ sở — kiểm cơ sở thuộc xã; nil → không kiểm) và quy tắc nghiệp vụ
	// theo profile (vd kn_cnts: số liều tiêm ≤ tổng đàn).
	Facilities port.FacilityDirectory
	Validators map[string]port.Validator
}

// Module là pf/forms đã nối.
type Module struct{ svc *app.Service }

// New tạo module trên pool b — vai đăng nhập của đơn vị (app_platform), là thành viên r_forms. b nil: chỉ dùng được
// Freezer (giao dịch bên gọi). Cấu hình bên dùng sai → lỗi.
func New(b tenant.Beginner, clk clock.Clock, d Deps) (*Module, error) {
	var pool tenant.Beginner
	if b != nil {
		pool = db.ForModule(b, DBModule)
	}
	svc, err := app.NewService(pool, clk, d.Approvals, d.Profiles)
	if err != nil {
		return nil, err
	}
	svc.Facilities, svc.Validators = d.Facilities, d.Validators
	return &Module{svc: svc}, nil
}

// UseValidator nối quy tắc nghiệp vụ của bên dùng cho profile (gốc lắp ráp — bên cài quy tắc có thể dựng sau module).
func (m *Module) UseValidator(profile string, v port.Validator) {
	if m.svc.Validators == nil {
		m.svc.Validators = map[string]port.Validator{}
	}
	m.svc.Validators[profile] = v
}

// Publisher là cổng phát hành kỳ native cho module nghiệp vụ (W11.42 — KN CNTS).
func (m *Module) Publisher() port.Publisher { return app.Publisher{Svc: m.svc} }

// SyncSource/SyncSink: đồng bộ ngoại tuyến forms_task / forms_submission (W11.42).
func (m *Module) SyncSource() syncport.Source { return app.SyncSource{Svc: m.svc} }

// SyncSink trả Sink chiều lên forms_submission.
func (m *Module) SyncSink() syncport.Sink { return app.SyncSink{Svc: m.svc} }

// Control là cổng điều khiển kỳ native (tiến độ, chốt, hủy) cho module nghiệp vụ (W11.62 — KN Kỳ KHTC).
func (m *Module) Control() port.PeriodControl { return app.Control{Svc: m.svc} }

// Figures là cổng đọc số liệu kỳ native theo lô (bài nộp cuối + ảnh chụp mới nhất) cho tổng hợp của module nghiệp vụ
// (W11.44 — KN CNTS BM-02, BM-18).
func (m *Module) Figures() port.FiguresReader { return app.Figures{Svc: m.svc} }

// Entry là cổng nhập/rà soát bài nộp cho module nghiệp vụ có màn nhập riêng (W11.63 — KN phiếu Báo cáo cho KHTC).
func (m *Module) Entry() port.Entry { return app.Entry{Svc: m.svc} }

// Freezer cho bên dùng có vòng đời kỳ riêng: ghi chốt/mở lại + ảnh chụp trong giao dịch của bên dùng.
func (m *Module) Freezer() port.Freezer { return m.svc }

// RegisterApp gắn route Application API /v1/forms/*.
func (m *Module) RegisterApp(rg *gin.RouterGroup) { handler.Handler{Svc: m.svc}.RegisterApp(rg) }

// RegisterAdmin: không có route Admin API.
func (m *Module) RegisterAdmin(*gin.RouterGroup) {}
