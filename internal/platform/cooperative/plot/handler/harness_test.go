package handler_test

// Lắp ráp kiểm thử của phần thửa pf/cooperative (W3.3, chuyển từ geo/handler): plotMod gói Service thửa như các lối
// vào module mà kiểm thử cũ gọi trên geo.Module; adminGeometry làm phép tính địa giới xã như geo.Module.AdminGeometry
// (cùng SQL, giao dịch dưới r_geo) — kiểm thử không import pf/geo (R-02).

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cooperative/plot/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cooperative/plot/handler"
	coopport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cooperative/port"
	metricsport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/metrics/port"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	syncport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/sync/port"
	shareddb "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/db"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/meter"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

var (
	coopDB = shareddb.Module("pf/cooperative")
	geoDB  = shareddb.Module("pf/geo")
)

type plotMod struct{ svc *app.Service }

func (m plotMod) RegisterApp(rg *gin.RouterGroup)   { handler.Handler{Svc: m.svc}.RegisterApp(rg) }
func (m plotMod) RegisterAdmin(rg *gin.RouterGroup) { handler.Handler{Svc: m.svc}.RegisterAdmin(rg) }
func (m plotMod) SyncSource() syncport.Source       { return app.SyncSource{Svc: m.svc} }
func (m plotMod) SyncSink() syncport.Sink           { return app.SyncSink{Svc: m.svc} }
func (m plotMod) MetricsSource() metricsport.Source { return app.MetricsSource{Svc: m.svc} }
func (m plotMod) LayerGeometry() metricsport.GeometryResolver {
	return app.MetricsSource{Svc: m.svc}
}
func (m plotMod) PlotStats() *app.Service               { return m.svc }
func (m plotMod) PlotFacts() plotFacts                  { return plotFacts(m) }
func (m plotMod) PlotHouseholds() plotHH                { return plotHH(m) }
func (m plotMod) CoopCentroids() coopCentroids          { return coopCentroids(m) }
func (m plotMod) MapLayers() handler.MapLayers          { return handler.MapLayers{Svc: m.svc} }
func (m plotMod) UseSharing(pa partyport.PartnerAccess) { m.svc.Sharing = pa }
func (m plotMod) UseKN(v coopport.FarmerIdentity, names partyport.ActorNames) {
	m.svc.Vault, m.svc.Names = v, names
}
func (m plotMod) UseEgress(cp meter.Checkpoint, mode meter.Mode, log *slog.Logger) {
	m.svc.Egress, m.svc.EgressMode, m.svc.Log = cp, mode, log
}

type plotFacts struct{ svc *app.Service }

func (p plotFacts) PlotFacts(ctx context.Context, refs []uuid.UUID) (map[uuid.UUID]coopport.Plot, error) {
	if _, err := tenant.FromContext(ctx); err != nil {
		return nil, err
	}
	return p.svc.Facts(ctx, refs)
}

type plotHH struct{ svc *app.Service }

func (p plotHH) HouseholdsInCommunes(ctx context.Context, communes []string) ([]uuid.UUID, error) {
	return p.svc.InCommunes(ctx, communes)
}

type coopCentroids struct{ svc *app.Service }

func (c coopCentroids) CoopCentroids(ctx context.Context, coops []uuid.UUID) (map[uuid.UUID]coopport.CoopCentroid, error) {
	return c.svc.Centroids(ctx, coops)
}

// adminGeometry thay geo.Module.AdminGeometry: cùng truy vấn, giao dịch dưới r_geo (pf_geo.admin_boundary).
type adminGeometry struct{ db tenant.Beginner }

func (a adminGeometry) OutsideCommunePct(ctx context.Context, geojson, commune string) (*decimal.Decimal, error) {
	var s *string
	err := tenant.FromCtxTx(ctx, a.db, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `WITH n AS (SELECT ST_Multi(ST_SetSRID(ST_GeomFromGeoJSON($1::text), 4326)) AS g)
		  SELECT CASE WHEN ST_Area(n.g::geography) = 0 THEN 0
		              ELSE ST_Area(ST_Difference(n.g, b.geom)::geography) * 100 / ST_Area(n.g::geography) END::numeric(14,4)::text
		  FROM pf_geo.admin_boundary b, n
		  WHERE b.unit_code = $2::text AND b.superseded_at IS NULL`, geojson, commune).Scan(&s)
	})
	if errors.Is(err, pgx.ErrNoRows) || s == nil {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	d, err := decimal.NewFromString(*s)
	return &d, err
}

// seedCommune ghi ranh giới hành chính hiện hành của xã (như nhập ở pf/geo) dưới r_geo.
func (f *fixture) seedCommune(code, name string, geom map[string]any) {
	f.t.Helper()
	gj, _ := json.Marshal(geom)
	err := tenant.WithTenantTx(context.Background(), shareddb.ForModule(f.pool, geoDB), f.tenant, func(tx pgx.Tx) error {
		_, err := tx.Exec(context.Background(), `INSERT INTO pf_geo.admin_boundary (tenant_id, id, unit_code, level, name, geom, source, imported_by)
		  VALUES ($1::uuid, $2::uuid, $3::text, 'commune', $4::text, ST_Multi(ST_SetSRID(ST_GeomFromGeoJSON($5::text), 4326)), 'kiểm thử', $6::uuid)`,
			f.tenant, ids.New(), code, name, string(gj), ids.New())
		return err
	})
	if err != nil {
		f.t.Fatal(err)
	}
}

func hubCtx(f *fixture) context.Context { return tenant.WithID(context.Background(), f.tenant) }
