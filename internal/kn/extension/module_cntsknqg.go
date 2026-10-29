package extension

// Nối mẫu biểu dự án KNQG (W11.53 — D4 CN13, extension/cntsknqg): Mẫu 01/TĐ-DA, 03, 04/DA-NTTH, 05/DA-NTMH, 07/NTKQ-DA
// gắn dự án/điểm của W11.47, thực tế Mẫu 05 từ nhật ký W11.48. Không cần lời gọi nối mới ở gốc lắp ráp: tên xã, tên cán bộ
// dùng chung với vòng đời báo cáo; nhãn chủ hộ dùng kho định danh của hộ mô hình (UseCNTSModelVault); bản scan đã ký dùng
// kho đối tượng của kho văn bản phòng (UseDeptLibrary); Word đúng mẫu qua pf/export (ExportTemplates, UseExporter).

import (
	"context"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	knqgapp "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/cntsknqg/app"
	knqghandler "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/cntsknqg/handler"
	accport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/port"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/objstore"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/vault/identity/client"
)

// knqgService là use case mẫu biểu dự án KNQG (trường Module.knqg).
type knqgService = knqgapp.Service

// knqgSvc dựng use case (đọc cổng của module khác lúc gọi — các Use* nối sau New).
func (m *Module) knqgSvc() *knqgapp.Service {
	if m.knqg == nil {
		m.knqg = &knqgapp.Service{DB: m.svc.DB, Clock: m.svc.Clock, Units: m.model.Units,
			Names: func() partyport.ActorNames { return m.rc.Names },
			Vault: func() client.Client { return m.model.Vault },
			Store:      func() objstore.Store { return m.home.Store },
			Recipients: knqgRecipients{m: m}}
	}
	return m.knqg
}

// registerCNTSKNQG gắn route /v1/extension/cnts/knqg-specs, /knqg-docs*.
func (m *Module) registerCNTSKNQG(rg *gin.RouterGroup) {
	knqghandler.Handler{Svc: m.knqgSvc()}.RegisterApp(rg)
}

// knqgRecipients (W11.53b) tra người nhận thông báo biểu dự án KNQG từ danh bạ gán vai trò của pf/accounts (UseRoleHolders,
// UseKNContext — đọc lúc gọi). Chưa nối → rỗng (không thông báo).
type knqgRecipients struct{ m *Module }

// National: cán bộ Cấp TW (kn_tw) còn hiệu lực của phòng Chăn nuôi – Thủy sản.
func (r knqgRecipients) National(ctx context.Context, tid uuid.UUID) ([]uuid.UUID, error) {
	if r.m.svc.Holders == nil {
		return nil, nil
	}
	return r.m.svc.Holders.RoleHolders(ctx, tid, authz.KNTW, knqgapp.DeptCNTS)
}

// Managers: lãnh đạo phòng Chăn nuôi – Thủy sản Cấp Tỉnh của tỉnh province.
func (r knqgRecipients) Managers(ctx context.Context, tid uuid.UUID, province string) ([]uuid.UUID, error) {
	if r.m.svc.KNDirectory == nil || province == "" {
		return nil, nil
	}
	os, err := r.m.svc.KNDirectory.Officers(ctx, tid, knqgapp.DeptCNTS, province)
	if err != nil {
		return nil, err
	}
	var out []uuid.UUID
	for _, o := range os {
		if o.Level == accport.ScheduleLevelProvince && accport.IsManager(o.MgmtLevel, "") {
			out = append(out, o.ActorRef)
		}
	}
	return out, nil
}
