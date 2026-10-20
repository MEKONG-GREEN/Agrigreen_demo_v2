package extension

// Nối Trang chủ phòng + kho văn bản phòng (W11.54 — D4 CN14, extension/depthome): số liệu đã chốt của phòng qua họ
// closed_figures/v1 (W11.10), sơ đồ tổ chức từ danh bạ gán vai trò của pf/accounts (KNDirectory), tên hiển thị từ
// Contact/danh bạ trực, tệp văn bản ở kho đối tượng (UseDeptLibrary).

import (
	"context"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/closedfig"
	dhapp "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/depthome/app"
	dhhandler "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/depthome/handler"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	accountsport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/port"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/objstore"
)

func (m *Module) newDeptHome() *dhapp.Service {
	return &dhapp.Service{DB: m.rc.DB, Clock: m.rc.Clock, Sources: dhapp.Sources{
		Closed:   deptClosed{m},
		Officers: m.deptOfficers,
		Names: func() partyport.ActorNames {
			if m.svc.ContactNames != nil {
				return m.svc.ContactNames
			}
			return m.rc.Names
		},
	}}
}

// UseDeptLibrary nối kho đối tượng cho tệp của kho văn bản phòng (W11.54). Chưa nối → tải lên/tải xuống trả 503
// extension.dependency_unavailable; trang chủ, danh sách vẫn đọc được.
func (m *Module) UseDeptLibrary(s objstore.Store) { m.home.Store = s }

// deptOfficers: cán bộ Cấp Tỉnh của phòng trong tỉnh của người gọi (P1 KNContext + KNDirectory). Thiếu nguồn → rỗng.
func (m *Module) deptOfficers(ctx context.Context, tid uuid.UUID, dept string) ([]dhapp.Officer, error) {
	if m.svc.KNContext == nil || m.svc.KNDirectory == nil {
		return nil, nil
	}
	kc, ok, err := m.svc.KNContext.KNContext(ctx)
	if err != nil || !ok || kc.ProvinceCode == "" {
		return nil, err
	}
	os, err := m.svc.KNDirectory.Officers(ctx, tid, dept, kc.ProvinceCode)
	if err != nil {
		return nil, err
	}
	out := []dhapp.Officer{}
	for _, o := range os {
		if o.Level != accountsport.ScheduleLevelProvince {
			continue
		}
		out = append(out, dhapp.Officer{ActorRef: o.ActorRef, MgmtLevel: o.MgmtLevel, Leader: accountsport.IsManager(o.MgmtLevel, "")})
	}
	return out, nil
}

// deptClosed đọc closed_figures/v1 lúc gọi (cổng xác nhận HTX nối sau New).
type deptClosed struct{ m *Module }

func (d deptClosed) ClosedFigures(ctx context.Context, tid uuid.UUID, q port.ClosedFiguresQuery) ([]port.ClosedFigure, error) {
	return closedfig.Reader{DB: d.m.rc.DB, HTX: d.m.htx}.ClosedFigures(ctx, tid, q)
}

func (m *Module) registerDeptHome(rg *gin.RouterGroup) {
	dhhandler.Handler{Svc: m.home}.RegisterApp(rg)
}
