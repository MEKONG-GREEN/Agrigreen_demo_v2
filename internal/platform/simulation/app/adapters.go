package app

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	mdport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/simulation/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// MasterdataParams hiện thực port.ParamSet trên pf/masterdata: masterdata chỉ trả
// phiên bản ĐÃ PHÊ DUYỆT có hiệu lực tại thời điểm tra (mục 16), nên found = true
// nghĩa là đã phê duyệt.
type MasterdataParams struct{ P mdport.Params }

// Approved tra khóa tại at trong tenant (tenant đặt vào context cho masterdata).
func (m MasterdataParams) Approved(ctx context.Context, tid uuid.UUID, key string, at time.Time) (port.ApprovedParam, bool, error) {
	if m.P == nil {
		return port.ApprovedParam{}, false, nil
	}
	v, err := m.P.At(tenant.WithID(ctx, tid), key, at)
	if errors.Is(err, mdport.ErrParamNotFound) {
		return port.ApprovedParam{}, false, nil
	}
	if err != nil {
		return port.ApprovedParam{}, false, err
	}
	return port.ApprovedParam{Value: v.Value, Version: v.Version}, true, nil
}
