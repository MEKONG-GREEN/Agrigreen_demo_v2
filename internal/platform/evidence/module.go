// Package evidence là module năng lực pf/evidence (`cap/evidence`, kind capability — kiến trúc v4.1 §18, §26,
// ADR-0027 mục 2, W6.5): tệp minh chứng đã mã hóa phong bì (schema pf_files, chuyển từ pf/sync), bộ ba chủ thể
// R-DB-07 và mở tệp có mục đích, có nhật ký.
//
//   - Attach: kiểm phong bì, băm bản mã; chủ thể (subject_module, subject_type, subject_id) — nếu có — phải
//     phân giải được qua Resolver của module chủ trong tenant người gọi (tồn tại, cùng đơn vị). Bộ ba là cột
//     NULL được của pf_files.file (pf 000123): tệp cũ và tệp tải trước khi biết thực thể không có bộ ba.
//   - Open(purpose) / OpenContent(purpose): vai xem minh chứng, mục đích bắt buộc (R-42), người gọi thấy chủ
//     thể (Resolver + phạm vi đơn vị hành chính, hoặc cấp phép chia sẻ), đồng ý của hộ. Mọi quyết định — cấp
//     hay từ chối — ghi sổ chỉ ghi thêm pf_files.access_log (shared.apply_ledger, chuỗi băm theo tệp).
//   - Module chủ hiện thực port.Resolver; module năng lực không import module domain — gốc lắp ráp nối
//     (cmd/api-platform/assembly: pf/cooperative → "pf/cooperative").
//   - pf/sync giữ route /v1/files (kiểm thiết bị, multipart) và gọi port.Files.
//
// Module khác chỉ import evidence/port (R-02). Tệp này là gốc lắp ráp cho cmd/api-platform.
package evidence

import (
	"log/slog"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/evidence/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/evidence/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/db"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/objstore"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// DBModule: giao dịch của module chạy SET LOCAL ROLE r_evidence (ADR-0028).
var DBModule = app.DBModule

// Deps là phụ thuộc của module.
type Deps struct {
	Store   objstore.Store     // kho đối tượng chỉ chứa bản mã (R-40); nil → /v1/files trả 503
	Vault   app.Unwrapper      // kho định danh mở DEK; nil → nội dung trả 503
	Scope   app.ScopeResolver  // phạm vi người gọi (pf/party); nil → tệp có chủ thể không mở được
	Consent app.ConsentChecker // tùy chọn: đồng ý của hộ (pf/party)
	Log     *slog.Logger
}

// Module là pf/evidence đã nối.
type Module struct{ svc *app.Service }

// New tạo module trên pool b — vai đăng nhập của đơn vị (app_platform), là thành viên r_evidence. Mọi giao dịch
// của module chạy dưới r_evidence (db.ForModule).
func New(b tenant.Beginner, clk clock.Clock, d Deps) *Module {
	if clk == nil {
		clk = clock.System{}
	}
	svc := &app.Service{Clock: clk, Deps: app.Deps{Store: d.Store, Vault: d.Vault, Scope: d.Scope, Consent: d.Consent,
		Log: d.Log, Resolvers: map[string]port.Resolver{}}}
	if b != nil {
		svc.DB = db.ForModule(b, DBModule)
	}
	return &Module{svc: svc}
}

// UseResolver nối Resolver của module chủ (subject_module = tên module.yaml). Gọi lúc lắp ráp, trước khi phục vụ.
func (m *Module) UseResolver(module string, r port.Resolver) { m.svc.Resolvers[module] = r }

// Files trả cổng tệp minh chứng.
func (m *Module) Files() port.Files { return m.svc }
