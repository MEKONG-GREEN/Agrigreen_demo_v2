package app

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/contracts/publication"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/fieldops/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/fieldops/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/outbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/telemetry"
)

const dateLayout = "2006-01-02"

// export là cửa DUY NHẤT từ mkg/fieldops ra van công bố (mục 5): nạp hợp đồng
// đã duyệt nhúng trong nhị phân, kiểm payload (mặc định từ chối — khóa lạ, sai
// kiểu, thiếu trường), ghi sổ valve_export và shared.outbox (event_id = id bản
// ghi xuất) CÙNG giao dịch với thay đổi nghiệp vụ.
func (s *Service) export(ctx context.Context, tx pgx.Tx, tid, actor uuid.UUID, pub port.Publication, subject, orderID uuid.UUID,
	eventTime time.Time, idem string) (store.ValveExport, error) {
	c, err := publication.Get(pub.PublicationName())
	if err != nil {
		return store.ValveExport{}, ErrValveDenied.WithCause(err)
	}
	evType, evVer, ok := c.EventType()
	if !ok {
		return store.ValveExport{}, ErrValveDenied.WithDetail("hợp đồng không khai sự kiện")
	}
	raw, err := json.Marshal(pub)
	if err != nil {
		return store.ValveExport{}, err
	}
	if err := c.Validate(raw); err != nil {
		return store.ValveExport{}, ErrValveDenied.WithCause(err)
	}
	x := store.ValveExport{
		ID: ids.New(), TenantID: tid, Contract: c.Name, ContractVersion: c.Version, EventType: evType, SchemaVersion: evVer,
		SubjectRef: subject, OrderID: orderID, Payload: raw, EventTime: eventTime.UTC(), ActorRef: actor, IdemKey: idem,
	}
	if err := insert(store.InsertValveExport(ctx, tx, &x)); err != nil {
		return x, err
	}
	err = outbox.Enqueue(ctx, tx, outbox.Event{
		ID: x.ID, TenantID: tid, Type: evType, SchemaVersion: evVer, OccurredAt: x.EventTime,
		TraceID: telemetry.TraceID(ctx), Producer: Producer, Payload: pub, Kind: outbox.KindPlatform,
	})
	return x, err
}

// schedulePayload chiếu lệnh gom sang hợp đồng collection_schedule v1: chỉ khung
// thời gian và trạng thái — không tổ gom, máy, giá, khối lượng, khách hàng.
func schedulePayload(o store.Order, status string) port.CollectionSchedulePublishedV1 {
	return port.CollectionSchedulePublishedV1{
		ScheduleRef: o.OrderID, HTXRef: o.HTXRef, CommuneCode: o.CommuneCode,
		WindowFrom: o.WindowFrom.Format(dateLayout), WindowTo: o.WindowTo.Format(dateLayout),
		Status: status, Revision: int(o.LastSeq),
	}
}

// statementPayload chiếu số xếp ghe/xe hiệu lực của một thửa sang hợp đồng
// htx_collection_statement v1: chỉ số cuộn — không khối lượng cân, kg/cuộn, giá.
func statementPayload(r store.Recon, collectedAt time.Time) port.HTXCollectionStatementPublishedV1 {
	return port.HTXCollectionStatementPublishedV1{
		StatementRef: r.StatementRef, HTXRef: r.HTXRef, PlotRef: r.PlotRef,
		CollectedOn: clock.BusinessDate(collectedAt).String(), Rolls: r.MKGRolls, Revision: r.StatementRevision,
	}
}

// Publications liệt kê bản ghi đã xuất qua van của một lệnh gom (kiểm toán).
func (s *Service) Publications(ctx context.Context, orderID uuid.UUID, limit int) ([]store.ValveExport, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	var out []store.ValveExport
	err := s.read(ctx, func(tx pgx.Tx) error {
		var e error
		out, e = store.ValveExports(ctx, tx, orderID, limit)
		return e
	})
	return out, err
}
