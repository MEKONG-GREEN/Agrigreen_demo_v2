package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/contracts/publication"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/geo/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/geo/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/geo/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/outbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// ---------------------------------------------------------------------------
// Cổng đọc cho gốc lắp ráp (G2 đợt 2)
// ---------------------------------------------------------------------------

var (
	_ port.PlotHouseholds = (*Service)(nil)
	_ port.CoopCentroids  = (*Service)(nil)
)

// HouseholdsInCommunes thực hiện port.PlotHouseholds (HTX US-NEWS-03). Tenant lấy từ context.
func (s *Service) HouseholdsInCommunes(ctx context.Context, communes []string) ([]uuid.UUID, error) {
	if communes != nil && len(communes) == 0 {
		return []uuid.UUID{}, nil
	}
	var out []uuid.UUID
	err := tenant.FromCtxTx(ctx, s.DB, func(tx pgx.Tx) error {
		var e error
		out, e = store.HouseholdsInCommunes(ctx, tx, communes == nil, communes)
		return e
	})
	return out, err
}

// CoopCentroids thực hiện port.CoopCentroids (tâm vùng nguyên liệu cho mô phỏng).
func (s *Service) CoopCentroids(ctx context.Context, coops []uuid.UUID) (map[uuid.UUID]port.CoopCentroid, error) {
	out := map[uuid.UUID]port.CoopCentroid{}
	if coops != nil && len(coops) == 0 {
		return out, nil
	}
	var rows []store.CoopCentroid
	err := tenant.FromCtxTx(ctx, s.DB, func(tx pgx.Tx) error {
		var e error
		rows, e = store.CoopCentroids(ctx, tx, coops == nil, coops)
		return e
	})
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		out[r.CooperativeRef] = port.CoopCentroid{CooperativeRef: r.CooperativeRef, Lon: r.Lon, Lat: r.Lat}
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// Consumer van công bố MKG (port.HubValveConsumer)
// ---------------------------------------------------------------------------

// hubFacilityV1 là payload hợp đồng hub_facility v1 (contracts/publication/hub_facility.json).
type hubFacilityV1 struct {
	HubRef   uuid.UUID `json:"hub_ref"`
	Code     string    `json:"code"`
	Name     string    `json:"name"`
	Kind     string    `json:"kind"` // hub | warehouse | yard
	Location struct {
		Lat json.Number `json:"lat"`
		Lng json.Number `json:"lng"`
	} `json:"location"`
	Boundary      json.RawMessage `json:"boundary"`
	ProvinceCode  string          `json:"province_code"`
	Status        string          `json:"status"` // active | inactive
	CapacityTons  string          `json:"capacity_tons"`
	Unit          string          `json:"unit"`
	EffectiveFrom string          `json:"effective_from"`
}

// hubCapacityV1 là payload hợp đồng hub_capacity v1.
type hubCapacityV1 struct {
	HubRef        uuid.UUID `json:"hub_ref"`
	AsOf          string    `json:"as_of"`
	CapacityTons  string    `json:"capacity_tons"`
	UsedTons      string    `json:"used_tons"`
	AvailableTons string    `json:"available_tons"`
	Unit          string    `json:"unit"`
}

// Ánh xạ loại cơ sở MKG → loại của pf_geo.hub_view (kho | hub | nha_may). Bãi (yard)
// là nơi chứa rơm ngoài trời — hiển thị như kho trên lớp bản đồ GIS.
var mkgKind = map[string]string{"hub": domain.FacilityHub, "warehouse": "kho", "yard": "kho"}

// errValve đánh dấu payload van không hợp lệ: không bao giờ tự lành → giao lại rồi DLQ (R-62).
var errValve = errors.New("geo: payload van công bố không hợp lệ")

// HandleHubPublication là inbox.Handler của consumer port.HubValveConsumer: kiểm payload
// bằng bộ kiểm hợp đồng lúc chạy (contracts/publication — mặc định từ chối trường lạ),
// rồi ghi mô hình đọc hub qua HubSink. Idempotent hai lớp: shared.inbox theo event_id và
// HubSink theo (hub_ref, phiên bản = effective_from) / (hub_ref, as_of).
func (s *Service) HandleHubPublication(ctx context.Context, _ pgx.Tx, env outbox.Envelope) error {
	var name string
	switch env.EventType {
	case port.EventMKGHubFacilityPublished:
		name = port.ContractHubFacility
	case port.EventMKGHubCapacityPublished:
		name = port.ContractHubCapacity
	default:
		return nil
	}
	c, err := publication.Get(name)
	if err != nil {
		return err
	}
	if err := c.Validate(env.Payload); err != nil {
		return fmt.Errorf("%w (%s): %w", errValve, env.EventType, err)
	}
	ctx = tenant.WithID(ctx, env.TenantID)
	if name == port.ContractHubCapacity {
		h, err := capacityFromValve(env.Payload)
		if err != nil {
			return err
		}
		if !h.CapacityTons.IsPositive() {
			return nil // cơ sở chưa khai sức chứa danh định: không có % lấp đầy để hiển thị
		}
		_, err = s.AppendHubCapacity(ctx, h)
		return err
	}
	h, err := facilityFromValve(env.Payload)
	if err != nil {
		return err
	}
	_, err = s.UpsertHubFacility(ctx, h)
	return err
}

func facilityFromValve(raw []byte) (port.HubFacility, error) {
	var p hubFacilityV1
	if err := json.Unmarshal(raw, &p); err != nil {
		return port.HubFacility{}, fmt.Errorf("%w: %w", errValve, err)
	}
	eff, err := time.Parse(time.RFC3339Nano, p.EffectiveFrom)
	if err != nil {
		return port.HubFacility{}, fmt.Errorf("%w: effective_from: %w", errValve, err)
	}
	lat, err1 := decimal.NewFromString(p.Location.Lat.String())
	lon, err2 := decimal.NewFromString(p.Location.Lng.String())
	if err1 != nil || err2 != nil {
		return port.HubFacility{}, fmt.Errorf("%w: location", errValve)
	}
	capacity, err := decimal.NewFromString(p.CapacityTons)
	if err != nil {
		return port.HubFacility{}, fmt.Errorf("%w: capacity_tons", errValve)
	}
	h := port.HubFacility{
		HubRef: p.HubRef, Version: eff.UTC().UnixMicro(), Code: strings.TrimSpace(p.Code), Name: strings.TrimSpace(p.Name),
		Kind: mkgKind[p.Kind], Lon: lon, Lat: lat, ProvinceCode: p.ProvinceCode, Status: domain.StatusActive, PublishedAt: eff.UTC(),
	}
	if p.Status == "inactive" {
		h.Status = domain.StatusRetired
	}
	if capacity.IsPositive() {
		h.CapacityTons = &capacity // 0 = chưa khai sức chứa danh định
	}
	if len(p.Boundary) > 0 && string(p.Boundary) != "null" {
		h.Polygon = p.Boundary
	}
	return h, nil
}

func capacityFromValve(raw []byte) (port.HubCapacity, error) {
	var p hubCapacityV1
	if err := json.Unmarshal(raw, &p); err != nil {
		return port.HubCapacity{}, fmt.Errorf("%w: %w", errValve, err)
	}
	at, err := time.Parse(time.RFC3339Nano, p.AsOf)
	if err != nil {
		return port.HubCapacity{}, fmt.Errorf("%w: as_of: %w", errValve, err)
	}
	capacity, err1 := decimal.NewFromString(p.CapacityTons)
	used, err2 := decimal.NewFromString(p.UsedTons)
	if err1 != nil || err2 != nil {
		return port.HubCapacity{}, fmt.Errorf("%w: capacity_tons/used_tons", errValve)
	}
	return port.HubCapacity{HubRef: p.HubRef, AsOf: at.UTC(), CapacityTons: capacity, UsedTons: used}, nil
}
