package app

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/fieldops/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/fieldops/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/fieldops/store"
	mport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/measurement/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/mass"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/outbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// G3-2: tích hợp với mkg/measurement (docs/design/mkg.md §5.1, measurement/port ports_g3_2.go).

var _ port.KgPerRollEstimates = (*Service)(nil)

// KgPerRollAsOf cài đặt port.KgPerRollEstimates: chuỗi dự phòng HTX → hệ thống → cấu
// hình → 20, chỉ học từ lô gom cân TRƯỚC before và bỏ excludeLot. Tenant tường minh.
func (s *Service) KgPerRollAsOf(ctx context.Context, tid, htx uuid.UUID, before time.Time, exclude uuid.UUID) (port.KgPerRollEstimate, error) {
	var out port.KgPerRollEstimate
	err := tenant.WithTenantTx(ctx, s.DB, tid, func(tx pgx.Tx) error {
		own, err := store.SamplesBefore(ctx, tx, &htx, before, exclude, domain.LearnWindow)
		if err != nil {
			return err
		}
		all, err := store.SamplesBefore(ctx, tx, nil, before, exclude, domain.LearnWindow)
		if err != nil {
			return err
		}
		r := domain.ResolveKgPerRoll(own, all, s.ConfiguredKgPerRoll)
		k, err := mass.ParseKg(r.Kg.StringFixed(mass.Scale))
		if err != nil {
			return err
		}
		out = port.KgPerRollEstimate{Kg: k, Source: r.Source, Samples: r.Samples}
		return nil
	})
	return out, err
}

// HandleWeighTicket là consumer nội bộ mkg.fieldops.weigh_ticket
// (mkg.measurement.weigh_ticket.recorded, vai fieldops_app, giao dịch inbox): ghi khối
// lượng cân ĐẦU TIÊN của lô gom (HUB_IN; CUSTOMER_IN với tuyến DIRECT) vào bảng đệm học
// kg/cuộn — thay cho làm mới tay (RefreshWeighings vẫn giữ để chạy bù).
//
// Lô gom = collection_order_ref của phiếu (liên kết tường minh), không có thì lot_ref (quy
// ước đồng nhất). Bỏ qua: điểm cân khác, không có lệnh gom, chưa có cuộn xếp ghe/xe,
// phiếu gốc tới sau phiếu bù đã áp (stale). Giao lặp → cùng kết quả (upsert, R-61).
func (s *Service) HandleWeighTicket(ctx context.Context, tx pgx.Tx, env outbox.Envelope) error {
	if env.EventType != mport.EventWeighTicketRecorded {
		return nil
	}
	var t mport.WeighTicketRecordedV1
	if err := json.Unmarshal(env.Payload, &t); err != nil {
		return fmt.Errorf("fieldops: payload %s không hợp lệ: %w", env.EventType, err)
	}
	first := (t.Point == mport.PointHubIn && t.Route == mport.RouteHub) || (t.Point == mport.PointCustomerIn && t.Route == mport.RouteDirect)
	if !first || t.MassKg.Decimal().Sign() <= 0 {
		return nil
	}
	order := t.LotRef
	if t.CollectionOrderRef != nil {
		order = *t.CollectionOrderRef
	}
	o, err := store.GetOrder(ctx, tx, order, false)
	if err != nil || o == nil {
		return err
	}
	cur, err := store.WeighedOf(ctx, tx, order)
	if err != nil {
		return err
	}
	if cur != nil && cur.SupersedesTicketRef != nil && *cur.SupersedesTicketRef == t.TicketID {
		return nil // phiếu bù đã áp trước phiếu gốc
	}
	if cur != nil && cur.TicketRef != nil && *cur.TicketRef == t.TicketID {
		return nil // giao lặp
	}
	ts, err := store.Totals(ctx, tx, order)
	if err != nil {
		return err
	}
	rolls := 0
	for _, x := range ts {
		if x.Stage == port.StageBoatLoad {
			rolls += x.Rolls
		}
	}
	if rolls <= 0 {
		return nil // chưa xếp ghe/xe: RefreshWeighings chạy bù sau
	}
	return store.PutWeighedTicket(ctx, tx, env.TenantID, store.Unweighed{OrderID: order, HTXRef: o.HTXRef, Rolls: rolls}, t.MassKg,
		t.EventTime, t.TicketID, t.SupersedesID)
}
