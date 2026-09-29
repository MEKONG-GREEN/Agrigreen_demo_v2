package app

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/contracts/publication"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/hub/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/hub/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/hub/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/outbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/telemetry"
)

// unitTonnes là đơn vị khối lượng trên van (hợp đồng: "t").
const unitTonnes = "t"

// export là cửa DUY NHẤT từ mkg/hub ra van công bố (mục 5):
//
//  1. nạp hợp đồng đã phê duyệt nhúng trong nhị phân (không có → từ chối);
//  2. tuần tự hóa payload và kiểm theo hợp đồng — khóa lạ ở bất kỳ cấp nào, sai
//     kiểu, thiếu trường bắt buộc → từ chối, không ghi gì (mặc định từ chối);
//  3. ghi sổ mkg_hub.valve_export (đúng byte đã kiểm, kèm SHA-256) và
//     shared.outbox của cụm mkg với event_id = id bản ghi xuất, CÙNG giao dịch
//     với thay đổi nghiệp vụ. outbox-publisher phát lên RabbitMQ.
func (s *Service) export(ctx context.Context, tx pgx.Tx, p authz.Principal, pub port.Publication, subject uuid.UUID, eventTime time.Time, idem string) (store.ValveExport, error) {
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
	tid := p.TenantID
	x := store.ValveExport{
		ID: ids.New(), TenantID: tid, Contract: c.Name, ContractVersion: c.Version, EventType: evType, SchemaVersion: evVer,
		SubjectRef: subject, Payload: raw, EventTime: eventTime.UTC(), ActorRef: p.ActorRef, IdemKey: idem,
	}
	if err := insert(store.InsertValveExport(ctx, tx, &x)); err != nil {
		return x, err
	}
	err = outbox.Enqueue(ctx, tx, outbox.Event{
		ID: x.ID, TenantID: tid, Type: evType, SchemaVersion: evVer, OccurredAt: x.EventTime,
		TraceID: telemetry.TraceID(ctx), Producer: Producer, Payload: pub,
	})
	return x, err
}

// facilityPayload chiếu dữ liệu chủ hiện hành sang hợp đồng hub_facility v1.
func facilityPayload(f domain.Facility) port.HubFacilityPublishedV1 {
	return port.HubFacilityPublishedV1{
		HubRef: f.ID, Code: f.Spec.Code, Name: f.Spec.Name, Kind: string(f.Spec.Kind),
		Location:     port.GeoPoint{Lat: json.Number(f.Spec.Location.Lat.String()), Lng: json.Number(f.Spec.Location.Lng.String())},
		Boundary:     f.Spec.Boundary,
		ProvinceCode: f.Spec.ProvinceCode, Status: string(f.Spec.Status), CapacityTons: f.Spec.Capacity.Tonnes(), Unit: unitTonnes,
		EffectiveFrom: f.Spec.EffectiveFrom.UTC().Format(time.RFC3339Nano),
	}
}

// capacityPayload chiếu ảnh chụp sức chứa sang hợp đồng hub_capacity v1.
func capacityPayload(c domain.Capacity) port.HubCapacityPublishedV1 {
	return port.HubCapacityPublishedV1{
		HubRef: c.FacilityID, AsOf: c.AsOf.UTC().Format(time.RFC3339Nano), CapacityTons: c.Capacity.Tonnes(),
		UsedTons: c.Used.Tonnes(), AvailableTons: c.Available.Tonnes(), Unit: unitTonnes,
	}
}

func (s *Service) publishCapacity(ctx context.Context, tx pgx.Tx, p authz.Principal, f domain.Facility, asOf time.Time, idem string) (store.ValveExport, error) {
	used, err := store.UsedKg(ctx, tx, f.ID)
	if err != nil {
		return store.ValveExport{}, err
	}
	snap := domain.Snapshot(f.ID, asOf, f.Spec.Capacity, used)
	return s.export(ctx, tx, p, capacityPayload(snap), f.ID, asOf, idem)
}

// PublishCapacity chụp sức chứa của một cơ sở và xuất qua van hub_capacity.
func (s *Service) PublishCapacity(ctx context.Context, facility uuid.UUID, idem string) (x store.ValveExport, replayed bool, err error) {
	p, err := authz.Require(ctx, RoleHubOperator)
	if err != nil {
		return x, false, err
	}
	if err := requireIdem(idem); err != nil {
		return x, false, err
	}
	replayed, err = s.write(ctx, p.TenantID, func(tx pgx.Tx) error {
		f, err := store.Current(ctx, tx, facility, false)
		if err != nil {
			return err
		}
		if f == nil {
			return ErrFacilityNotFound
		}
		x, err = s.publishCapacity(ctx, tx, p, *f, s.Clock.Now(), idem)
		return err
	}, func(tx pgx.Tx) (bool, error) {
		prev, err := store.ValveByIdem(ctx, tx, idem)
		if prev != nil {
			x = *prev
		}
		return prev != nil, err
	})
	return x, replayed, err
}

// Publications liệt kê bản ghi đã xuất qua van của một cơ sở (kiểm toán).
func (s *Service) Publications(ctx context.Context, facility uuid.UUID, contract string, limit int) ([]store.ValveExport, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	var out []store.ValveExport
	err := s.read(ctx, func(tx pgx.Tx) error {
		var e error
		out, e = store.ValveExports(ctx, tx, contract, facility, limit)
		return e
	})
	return out, err
}
