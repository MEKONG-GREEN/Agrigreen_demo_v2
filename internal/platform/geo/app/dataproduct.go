package app

// Sản phẩm dữ liệu hub_capacity v1 cho bên thứ ba (G5, ADR-0022; g5-conventions §6) và cổng
// PlotFacts cho collection_volume của pf/cropcycle.
//
// pf/apiclient (cmd/api-public) đã kiểm cấp quyền dp_hub_capacity của bên cấp TRƯỚC khi gọi; ở đây
// chỉ đọc mô hình đọc hub của tenant bên cấp (pool vai geo_app, tenant.WithTenantTx — R-05, không
// BYPASSRLS) và chỉ điền trường của DTO HubCapacityV1:
//   - bậc sử dụng (unknown | normal | warning | full) theo ngưỡng geo.hub_capacity_warn_pct /
//     geo.hub_capacity_full_pct của bên cấp (cùng quy tắc với widget GIS US-HUB-01, BR-52) — KHÔNG trả
//     tấn đã dùng, % lấp đầy, nguồn số, lô, giá;
//   - capacity_tons = sức chứa danh định MKG đã công bố qua van (hub_view.capacity) chỉ khi đơn vị là
//     tấn (đơn vị khác → null);
//   - loại: hub → hub, kho → warehouse (van ánh xạ warehouse/yard → kho, nên bãi hiện là warehouse);
//     cơ sở nhà máy và hub chưa có mã tỉnh không thuộc sản phẩm (hợp đồng bắt buộc province_code);
//   - trạng thái: active → active, retired → inactive.

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	apiclientport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/apiclient/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/geo/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/geo/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

var (
	_ apiclientport.HubCapacitySource = (*Service)(nil)
)

// Giới hạn trang của sản phẩm dữ liệu.
const (
	DefaultProductLimit = 100
	MaxProductLimit     = 500
)

// Bậc sử dụng của hợp đồng hub_capacity v1.
const (
	BandUnknown = "unknown"
	BandNormal  = "normal"
	BandWarning = "warning"
	BandFull    = "full"
)

// UtilizationBand ánh xạ lớp màu sức chứa của pf/geo sang bậc của hợp đồng hub_capacity v1.
func UtilizationBand(class string) string {
	switch class {
	case domain.CapacityAvailable:
		return BandNormal
	case domain.CapacityNearFull:
		return BandWarning
	case domain.CapacityFull:
		return BandFull
	}
	return BandUnknown
}

var productKind = map[string]string{domain.FacilityHub: "hub", domain.FacilityKho: "warehouse"}

func productLimit(limit int) int {
	if limit <= 0 {
		return DefaultProductLimit
	}
	return min(limit, MaxProductLimit)
}

func encodeRef(u uuid.UUID) string { return base64.RawURLEncoding.EncodeToString(u[:]) }

func decodeRef(s string) (uuid.UUID, error) {
	if s == "" {
		return uuid.Nil, nil
	}
	b, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil || len(b) != 16 {
		return uuid.Nil, problem.Validation.WithField("cursor", "con trỏ không hợp lệ")
	}
	return uuid.UUID(b), nil
}

func validProvince(p string) bool {
	if p == "" {
		return true
	}
	if len(p) != 2 {
		return false
	}
	return p[0] >= '0' && p[0] <= '9' && p[1] >= '0' && p[1] <= '9'
}

// HubCapacities cài đặt apiclientport.HubCapacitySource.
func (s *Service) HubCapacities(ctx context.Context, dataTenant uuid.UUID, provinceCode, after string, limit int) ([]apiclientport.HubCapacityV1, string, error) {
	if dataTenant == uuid.Nil {
		return nil, "", tenant.ErrMissing
	}
	if !validProvince(provinceCode) {
		return nil, "", problem.Validation.WithField("province_code", "mã tỉnh 2 chữ số")
	}
	cur, err := decodeRef(after)
	if err != nil {
		return nil, "", err
	}
	limit = productLimit(limit)
	warn, full, err := s.hubThresholds(tenant.WithID(ctx, dataTenant), s.now())
	if err != nil {
		return nil, "", err
	}
	var hubs []store.ProductHub
	var latest map[store.SubjectKey][]store.CapacityLatest
	err = tenant.WithTenantTx(ctx, s.DB, dataTenant, func(tx pgx.Tx) error {
		var e error
		if hubs, e = store.ProductHubs(ctx, tx, provinceCode, cur, limit+1); e != nil {
			return e
		}
		refs := make([]uuid.UUID, len(hubs))
		for i, h := range hubs {
			refs[i] = h.HubRef
		}
		latest, e = store.LatestFor(ctx, tx, domain.SubjectHub, refs)
		return e
	})
	if err != nil {
		return nil, "", err
	}
	next := ""
	if len(hubs) > limit {
		hubs = hubs[:limit]
		next = encodeRef(hubs[limit-1].HubRef)
	}
	out := make([]apiclientport.HubCapacityV1, 0, len(hubs))
	for _, h := range hubs {
		v := effectiveCapacity(latest[store.SubjectKey{Kind: domain.SubjectHub, Ref: h.HubRef}], warn, full)
		out = append(out, hubProduct(h, v))
	}
	return out, next, nil
}

// hubProduct dựng phần tử hub_capacity v1 — CHỈ trường của hợp đồng.
func hubProduct(h store.ProductHub, v CapacityView) apiclientport.HubCapacityV1 {
	asOf := h.ReceivedAt
	if v.AsOf != nil && v.AsOf.After(asOf) {
		asOf = *v.AsOf
	}
	status := "active"
	if h.Status == domain.StatusRetired {
		status = "inactive"
	}
	item := apiclientport.HubCapacityV1{
		HubRef: h.HubRef.String(), Code: h.Code, Name: h.Name, Kind: productKind[h.Kind], ProvinceCode: h.ProvinceCode,
		Location: apiclientport.LatLng{Lat: jsonNumber(h.Lat), Lng: jsonNumber(h.Lng)}, Status: status,
		UtilizationBand: UtilizationBand(v.Class), AsOf: asOf.UTC().Format(time.RFC3339),
	}
	if h.CommuneCode != "" {
		c := h.CommuneCode
		item.CommuneCode = &c
	}
	if h.Capacity != nil && h.Unit == domain.UnitTon && h.Capacity.IsPositive() {
		c := h.Capacity.StringFixed(2)
		item.CapacityTons = &c
	}
	return item
}

func jsonNumber(s string) json.Number { return json.Number(s) }
