package ingest

import (
	"context"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/measurement/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/mass"
)

// TareSource cho bì của cửa sổ cân (xe/ghe rỗng, hoặc 0 với cân sàn đã về 0).
// Hiện thực thuộc mkg/measurement (phiên cân ghi bì có phiên bản) — chờ mẫu cân C-6.
// Khóa theo Window để port.Integrator tính lại được tất định khi tranh chấp.
type TareSource interface {
	TareKg(ctx context.Context, tenantID uuid.UUID, w port.Window) (mass.Kg, error)
}

// ZeroTare là TareSource cho cân sàn đã về 0 (bì = 0).
type ZeroTare struct{}

// TareKg cài đặt TareSource.
func (ZeroTare) TareKg(context.Context, uuid.UUID, port.Window) (mass.Kg, error) {
	return mass.Kg{}, nil
}

// Integrator là hiện thực DUY NHẤT của port.Integrator (R-70): đọc cửa sổ từ cụm ts
// rồi chạy IntegrateV1.
type Integrator struct {
	Windows WindowReader
	Tare    TareSource // nil → ZeroTare
}

var _ port.Integrator = (*Integrator)(nil)

// RuleVersion cài đặt port.Integrator.
func (*Integrator) RuleVersion() string { return RuleV1 }

// Integrate cài đặt port.Integrator.
func (in *Integrator) Integrate(ctx context.Context, tenantID uuid.UUID, w port.Window) (port.Integration, error) {
	r, err := in.IntegrateDetailed(ctx, tenantID, w)
	return r.Integration, err
}

// IntegrateDetailed như Integrate, kèm received_at của số đo cuối (EventTime phiếu) và bì đã dùng.
func (in *Integrator) IntegrateDetailed(ctx context.Context, tenantID uuid.UUID, w port.Window) (Result, error) {
	tareSrc := in.Tare
	if tareSrc == nil {
		tareSrc = ZeroTare{}
	}
	tare, err := tareSrc.TareKg(ctx, tenantID, w)
	if err != nil {
		return Result{}, err
	}
	readings, err := in.Windows.ReadWindow(ctx, tenantID, w)
	if err != nil {
		return Result{}, err
	}
	return IntegrateV1(w, readings, tare)
}
