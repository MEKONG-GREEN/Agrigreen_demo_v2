package extension

// Nối "Số liệu nguồn ngoài" KHTC (W11.67 — extension/khtcext, BRD KHTC v2.2 FN-TI-02.11, BR-DL-19): danh mục chỉ tiêu,
// số liệu theo chỉ tiêu × kỳ có nguồn và minh chứng, "Nạp Excel tổng hợp" hai bước. Route /v1/extension/khtc/external/*
// luôn gắn; tệp minh chứng cần Deps.Store (kho đối tượng) — thiếu thì chỉ phần tệp trả 503. Màn soạn báo cáo (W11.66)
// đọc giá trị hiệu lực của kỳ qua khtcext/app.ReportFigures.

import (
	"github.com/gin-gonic/gin"

	extapp "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcext/app"
	exthandler "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcext/handler"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/objstore"
)

type khtcExternalService = extapp.Service

func (m *Module) khtcExtSvc() *extapp.Service {
	if m.khtcExt == nil {
		m.khtcExt = &extapp.Service{DB: m.svc.DB, Clock: m.svc.Clock, Store: m.objects, Names: func() partyport.ActorNames { return m.rc.Names }}
	}
	return m.khtcExt
}

// UseObjectStore thay kho đối tượng của tệp minh chứng (kiểm thử tích hợp dùng kho cục bộ).
func (m *Module) UseObjectStore(s objstore.Store) {
	m.objects = s
	if m.khtcExt != nil {
		m.khtcExt.Store = s
	}
}

func (m *Module) registerKHTCExternal(rg *gin.RouterGroup) {
	exthandler.Handler{Svc: m.khtcExtSvc()}.RegisterApp(rg)
}
