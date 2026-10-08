// Package workflow là module năng lực pf/workflow (`cap/workflow`, kind capability — kiến trúc v4.1 §26,
// ADR-0027, W6.1): yêu cầu duyệt và quyết định dùng chung, quy tắc bốn mắt (R-56) — người duyệt khác người
// lập, khác người được hưởng — kiểm ở app/ và ở DB (pf 000118: CHECK decision_four_eyes, trigger gắn quyết
// định vào yêu cầu, một đối tượng một yêu cầu, một yêu cầu một quyết định).
//
//   - Sổ pf_workflow.request, pf_workflow.decision chỉ ghi thêm (shared.apply_ledger), RLS theo tenant.
//   - Approvals (Submit, Decide) ghi TRONG GIAO DỊCH CỦA BÊN GỌI, qua hàm DB SECURITY DEFINER
//     pf_workflow.submit/decide: thay đổi nghiệp vụ và quyết định cùng commit. Vai nhóm của bên gọi chỉ có
//     EXECUTE (pf 000118 cấp cho r_masterdata, r_party).
//   - Requests (Status) đọc trong giao dịch riêng, vai r_workflow (db.ForModule).
//   - Bên dùng: pf/masterdata (duyệt phiên bản tham số, kind masterdata.param_version) và pf/party (duyệt đề
//     xuất cấp quyền chia sẻ, kind party.share_grant — party là kernel nên khai interface riêng
//     party/port.FourEyes, gốc lắp ráp nối).
//
// Module khác chỉ import workflow/port (R-02). Tệp này là gốc lắp ráp cho cmd/api-platform.
package workflow

import (
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/workflow/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/workflow/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/db"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// DBModule: giao dịch riêng của module chạy SET LOCAL ROLE r_workflow (ADR-0028).
var DBModule = app.DBModule

// Module là pf/workflow đã nối.
type Module struct{ svc *app.Service }

// New tạo module trên pool b — vai đăng nhập của đơn vị (app_platform), là thành viên r_workflow. Mọi giao
// dịch riêng của module chạy dưới r_workflow (db.ForModule). b nil: chỉ dùng được Approvals (giao dịch bên gọi).
func New(b tenant.Beginner, clk clock.Clock) *Module {
	if clk == nil {
		clk = clock.System{}
	}
	svc := &app.Service{Clock: clk}
	if b != nil {
		svc.DB = db.ForModule(b, DBModule)
	}
	return &Module{svc: svc}
}

// Approvals cho module khác: lập yêu cầu, quyết định trong giao dịch của bên gọi.
func (m *Module) Approvals() port.Approvals { return m.svc }

// Requests cho module khác: trạng thái yêu cầu của một đối tượng.
func (m *Module) Requests() port.Requests { return m.svc }
