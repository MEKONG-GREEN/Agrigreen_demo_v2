// Package contact là năng lực dùng chung pf/contact (`cap/directory`, kind capability — W11.23, BRD HCTH v3.0 FN-13
// Contact; Q-071, Q-072 theo đề xuất của dev). Chủ duy nhất schema pf_contact.
//
//   - Một người = một bản ghi = một chủ thể ở kho định danh (FN-13 BR-01). Cán bộ thuộc tỉnh (gồm cán bộ khuyến nông cấp
//     xã) CHIẾU từ hồ sơ nhân sự pf/hrm qua port (port.Staff — gốc lắp ráp nối), theo cổng dữ liệu thật AG_HRM_RECORDS;
//     sửa tại hồ sơ (FN-07, AC-02). Liên hệ ngoài tỉnh (Trung ương, tỉnh khác, đơn vị khác) ở sổ pf_contact.person.
//   - KHÔNG PII ở pf_contact: họ tên, SĐT ở vault.identity; email, năm sinh ở ngăn vault 'contact.card' (liên hệ ngoài
//     tỉnh) hoặc chiếu từ ngăn 'hrm.profile' (cán bộ thuộc tỉnh) — kho chỉ trả trường được hỏi (thẻ danh bạ, mục đích
//     contact.directory, vai realm vault_directory có ở mọi cán bộ KN qua kn_user).
//   - LỌC TRƯỜNG Ở API theo vai (BR-03): Cấp TW, Cấp Xã chỉ họ tên, năm sinh, SĐT (không hỏi email ở kho, DTO riêng không
//     có khóa khác, không xem chi tiết); Cấp Tỉnh, Admin thêm chức danh, đơn vị, cấp, email. Cập nhật: Cấp Tỉnh – HCTH,
//     Admin (port.Access).
//   - Thẻ Đơn vị (pf_contact.unit): liên hệ công vụ của tổ chức (không phải dữ liệu cá nhân).
//   - DisplayNames (cùng chữ ký party/port.ActorNames): tên cán bộ theo tài khoản — thay cột display_name của danh bạ trực
//     pf_extension.staff (Q-072).
//
// HTTP: /v1/contacts/* (Application API). Module khác chỉ import contact/port (R-02). Tệp này là gốc lắp ráp cho
// cmd/api-platform.
package contact

import (
	"github.com/gin-gonic/gin"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/contact/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/contact/handler"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/contact/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/db"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/vault/identity/client"
)

// DBModule: giao dịch của module chạy SET LOCAL ROLE r_contact (ADR-0028).
var DBModule = app.DBModule

// Module là pf/contact đã nối.
type Module struct{ svc *app.Service }

// New tạo module trên pool b — vai đăng nhập của đơn vị (app_platform), là thành viên r_contact.
func New(b tenant.Beginner, clk clock.Clock, acc port.Access, v client.Client) (*Module, error) {
	svc, err := app.NewService(db.ForModule(b, DBModule), clk, acc, v)
	if err != nil {
		return nil, err
	}
	return &Module{svc: svc}, nil
}

// UseStaff nối nguồn cán bộ thuộc tỉnh (KN: pf/hrm port.Directory) và nhãn đơn vị/xã (nil → hiện mã).
func (m *Module) UseStaff(s port.Staff, l port.Labels) { m.svc.Staff, m.svc.Labels = s, l }

// Names trả nguồn tên cán bộ theo tài khoản (cùng chữ ký party/port.ActorNames).
func (m *Module) Names() port.Names { return m.svc }

// RegisterApp gắn route Application API /v1/contacts/*.
func (m *Module) RegisterApp(rg *gin.RouterGroup) { handler.Handler{Svc: m.svc}.RegisterApp(rg) }

// RegisterAdmin: không có route Admin API.
func (m *Module) RegisterAdmin(*gin.RouterGroup) {}
