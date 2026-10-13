package extension

// Nối bộ đọc nguồn module của Kỳ KHTC (W11.64 — extension/khtcsource, BRD KHTC v2.2 BR-DL-02…04; ADR-0053 §8): "Cập nhật
// số liệu" chụp bất biến port KHTCSources (module_closedfig.go) vào sổ pf 20261013175705; số của hạng mục Kế hoạch năm
// (W11.61) có nguồn module; mốc dữ liệu; "Nguồn đã thay đổi". Route /v1/extension/khtc/periods/{id}/sources* luôn gắn.

import (
	"context"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	khtcsrcapp "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extension/khtcsource/app"
	khtcsrchandler "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extension/khtcsource/handler"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extension/port"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
)

// khtcSourceService là use case bộ đọc nguồn module (trường Module.khtcSrc).
type khtcSourceService = khtcsrcapp.Service

func (m *Module) khtcSourceSvc() *khtcsrcapp.Service {
	if m.khtcSrc == nil {
		m.khtcSrc = &khtcsrcapp.Service{DB: m.svc.DB, Clock: m.svc.Clock, Sources: liveKHTCSources{m}, Plans: m.KHTCPlans(),
			Names: func() partyport.ActorNames { return m.rc.Names }}
	}
	return m.khtcSrc
}

// registerKHTCSources gắn route /v1/extension/khtc/periods/{id}/sources*.
func (m *Module) registerKHTCSources(rg *gin.RouterGroup) {
	khtcsrchandler.Handler{Svc: m.khtcSourceSvc()}.RegisterApp(rg)
}

// UseKHTCSourceReader thay port KHTCSources của "Cập nhật số liệu" (kiểm thử tích hợp nạp bộ dữ liệu đối chứng KHTCQA).
func (m *Module) UseKHTCSourceReader(s port.KHTCSources) { m.khtcSourceSvc().Sources = s }

// liveKHTCSources đọc port KHTCSources của module lúc gọi (cổng xác nhận HTX nối trước hay sau đều được).
type liveKHTCSources struct{ m *Module }

func (l liveKHTCSources) Periods(ctx context.Context, tid uuid.UUID, q port.KHTCQuery) ([]port.KHTCPeriod, error) {
	return l.m.KHTCSources().Periods(ctx, tid, q)
}
