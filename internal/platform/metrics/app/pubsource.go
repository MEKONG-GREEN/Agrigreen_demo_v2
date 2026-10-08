package app

// Nguồn chỉ số trên view công bố _pub (R-DB-10, W6.3, ADR-0031 §2; pf 000124): pf/metrics (cap/analytics) đọc
// CHỈ view pf_<module>_pub.v<N>_* mà module chủ khai trong provides.publications — bằng vai của chính nó
// (r_metrics, thành viên r_<module>_pub), không qua port.Source chạy bằng vai của module chủ như trước W6.3.
// Truy vấn giữ nguyên ngữ nghĩa của nguồn cũ (internal/platform/<module>/app/metrics.go — kiểm thử đối chứng
// trong metrics_integration_test.go của từng module so từng giá trị, watermark, lỗi và phạm vi): giá trị ngày D
// chỉ dùng dòng có recorded_at < Cutoff(D). Đọc trên bản sao chỉ đọc khi đã nối (Service.analytics).

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/metrics/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// PubSourceModules là các module chủ dữ liệu có nguồn chỉ số trên view _pub (Definition.SourceModule).
var PubSourceModules = []string{"pf/cropcycle", "pf/extension", "pf/geo", "pf/mechanization"}

// PubSource là port.Source + port.ScopeLister trên view _pub của một module chủ dữ liệu; AsOfAt tính với mốc
// recorded_at tường minh (kiểm thử "bản ghi ghi nhận muộn bị loại").
type PubSource interface {
	port.Source
	port.ScopeLister
	AsOfAt(ctx context.Context, key port.Key, d clock.Date, cutoff time.Time, sc port.Scope) (port.Value, error)
}

// PubSources trả nguồn _pub theo module chủ dữ liệu.
func (s *Service) PubSources() map[string]port.Source {
	out := make(map[string]port.Source, len(PubSourceModules))
	for _, m := range PubSourceModules {
		out[m] = s.PubSource(m)
	}
	return out
}

// PubSource trả nguồn _pub của module chủ dữ liệu; nil nếu module không có.
func (s *Service) PubSource(module string) PubSource {
	switch module {
	case "pf/cropcycle":
		return ccPub{s}
	case "pf/extension":
		return knPub{s}
	case "pf/geo":
		return geoPub{s}
	case "pf/mechanization":
		return mechPub{s}
	}
	return nil
}

// pubRead mở giao dịch chỉ đọc của tenant trên pool phân tích (bản sao nếu đã phát lại tới cutoff).
func (s *Service) pubRead(ctx context.Context, tid uuid.UUID, cutoff time.Time, fn func(pgx.Tx) error) error {
	db := s.analytics(ctx, cutoff)
	if db == nil {
		return ErrDependency
	}
	return tenant.WithTenantTxOptions(ctx, db, tid, pgx.TxOptions{AccessMode: pgx.ReadOnly}, fn)
}

// asOfClosed: AsOf chung — ngày chưa hết (theo đồng hồ của module) → ErrNotClosed, không thì AsOfAt(Cutoff(d)).
func asOfClosed(s *Service, ctx context.Context, key port.Key, d clock.Date, sc port.Scope,
	at func(context.Context, port.Key, clock.Date, time.Time, port.Scope) (port.Value, error)) (port.Value, error) {
	if s.Clock.Now().Before(port.Cutoff(d)) {
		return port.Value{}, port.ErrNotClosed
	}
	return at(ctx, key, d, port.Cutoff(d), sc)
}

// newValue dựng giá trị rỗng như nguồn cũ: key, ngày, phạm vi, cutoff, phiên bản + đơn vị từ định nghĩa trong ctx.
func newValue(ctx context.Context, key port.Key, d clock.Date, cutoff time.Time, sc port.Scope) port.Value {
	v := port.Value{Key: key, BusinessDate: d, Scope: sc, Cutoff: cutoff}
	if def, ok := port.DefinitionFrom(ctx); ok {
		v.DefinitionVersion, v.Unit = def.Version, def.Unit
	}
	return v
}

func setWatermark(v *port.Value, id *uuid.UUID, at *time.Time) {
	if id != nil {
		v.Watermark.LastEventID = *id
	}
	if at != nil {
		v.Watermark.RecordedAt = at.UTC()
	}
}

func unitsOf(sc port.Scope) []string {
	if len(sc.Units) == 0 {
		return nil
	}
	return sc.Units
}

// ---------------------------------------------------------------------------
// pf/cropcycle — cc.season_area_ha, cc.yield_t_per_ha (theo vụ, Scope.Dimension "DX-2026")
// ---------------------------------------------------------------------------

const (
	keySeasonAreaHa port.Key = "cc.season_area_ha"
	keyYieldTPerHa  port.Key = "cc.yield_t_per_ha"
	// seasonHistory: job ghi các vụ của năm D và 2 năm trước.
	seasonHistory = 2
)

type ccPub struct{ s *Service }

func (p ccPub) AsOf(ctx context.Context, key port.Key, d clock.Date, sc port.Scope) (port.Value, error) {
	return asOfClosed(p.s, ctx, key, d, sc, p.AsOfAt)
}

func (p ccPub) AsOfAt(ctx context.Context, key port.Key, d clock.Date, cutoff time.Time, sc port.Scope) (port.Value, error) {
	v := newValue(ctx, key, d, cutoff, sc)
	if key != keySeasonAreaHa && key != keyYieldTPerHa {
		return v, port.ErrUnknownMetric
	}
	code, year, ok := port.ParseSeason(sc.Dimension)
	if !ok {
		return v, port.ErrNoData
	}
	err := p.s.pubRead(ctx, sc.TenantID, cutoff, func(tx pgx.Tx) error {
		var n int64
		var a, b string
		var wmID *uuid.UUID
		var wmAt *time.Time
		q := `WITH last AS (
		    SELECT DISTINCT ON (stream_key) id, recorded_at, action, area_ha, commune_code, season_code, season_year
		    FROM pf_cropcycle_pub.v1_season_event WHERE recorded_at < $1 ORDER BY stream_key, seq DESC)
		  SELECT count(*) FILTER (WHERE action <> 'cancelled'), coalesce(sum(area_ha) FILTER (WHERE action <> 'cancelled'), 0)::text, '0',
		         (array_agg(id ORDER BY recorded_at DESC, id DESC))[1], max(recorded_at)
		  FROM last WHERE upper(season_code) = $2 AND season_year = $3 AND ($4::text[] IS NULL OR commune_code = ANY($4))`
		if key == keyYieldTPerHa {
			q = `WITH last AS (
			    SELECT DISTINCT ON (stream_key) id, recorded_at, mass_kg, area_ha, commune_code, season_code, season_year
			    FROM pf_cropcycle_pub.v1_harvest_declaration WHERE recorded_at < $1 ORDER BY stream_key, seq DESC)
			  SELECT count(*), coalesce(sum(mass_kg), 0)::text, coalesce(sum(area_ha), 0)::text,
			         (array_agg(id ORDER BY recorded_at DESC, id DESC))[1], max(recorded_at)
			  FROM last WHERE upper(season_code) = $2 AND season_year = $3 AND ($4::text[] IS NULL OR commune_code = ANY($4))`
		}
		if err := tx.QueryRow(ctx, q, cutoff.UTC(), strings.ToUpper(code), year, unitsOf(sc)).Scan(&n, &a, &b, &wmID, &wmAt); err != nil {
			return err
		}
		if n == 0 {
			return port.ErrNoData
		}
		x, err := decimal.NewFromString(a)
		if err != nil {
			return err
		}
		y, err := decimal.NewFromString(b)
		if err != nil {
			return err
		}
		v.Value = x
		if key == keyYieldTPerHa {
			if !y.IsPositive() {
				return port.ErrNoData
			}
			v.Value = x.Div(decimal.NewFromInt(1000)).Div(y).Round(4)
		}
		setWatermark(&v, wmID, wmAt)
		return nil
	})
	return v, err
}

// Scopes: mỗi vụ (năm D và hai năm trước) có dữ liệu trước Cutoff(D), toàn tenant và từng xã.
func (p ccPub) Scopes(ctx context.Context, key port.Key, d clock.Date, tid uuid.UUID) ([]port.Scope, error) {
	view := "pf_cropcycle_pub.v1_season_event"
	if key == keyYieldTPerHa {
		view = "pf_cropcycle_pub.v1_harvest_declaration"
	}
	cutoff := port.Cutoff(d)
	var out []port.Scope
	err := p.s.pubRead(ctx, tid, cutoff, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT DISTINCT upper(season_code), season_year, commune_code FROM `+view+`
		  WHERE recorded_at < $1 AND season_year >= $2 ORDER BY 1, 2, 3`, cutoff.UTC(), d.Year-seasonHistory)
		if err != nil {
			return err
		}
		defer rows.Close()
		seen := map[string]bool{}
		for rows.Next() {
			var code, commune string
			var year int
			if err := rows.Scan(&code, &year, &commune); err != nil {
				return err
			}
			dim := port.SeasonDimension(code, year)
			if !seen[dim] {
				seen[dim] = true
				out = append(out, port.Scope{TenantID: tid, Dimension: dim})
			}
			out = append(out, port.Scope{TenantID: tid, Dimension: dim, Units: []string{commune}})
		}
		return rows.Err()
	})
	return out, err
}

// ---------------------------------------------------------------------------
// pf/extension — kn.* (KN US-DASH-01/02)
// ---------------------------------------------------------------------------

const (
	keySownAreaHa       port.Key = "kn.sown_area_ha"
	keyCoopsReported    port.Key = "kn.cooperatives_reported"
	keyOpenTasks        port.Key = "kn.open_tasks"
	keyDirectiveAckRate port.Key = "kn.directive_ack_rate_pct"
)

type knPub struct{ s *Service }

func (p knPub) AsOf(ctx context.Context, key port.Key, d clock.Date, sc port.Scope) (port.Value, error) {
	return asOfClosed(p.s, ctx, key, d, sc, p.AsOfAt)
}

// knPeriod trả kỳ tháng "YYYY-MM" chứa d và [đầu kỳ, đầu kỳ sau) giờ VN.
func knPeriod(d clock.Date) (string, time.Time, time.Time) {
	start := time.Date(d.Year, d.Month, 1, 0, 0, 0, 0, clock.Zone())
	return start.Format("2006-01"), start.UTC(), start.AddDate(0, 1, 0).UTC()
}

func (p knPub) AsOfAt(ctx context.Context, key port.Key, d clock.Date, cutoff time.Time, sc port.Scope) (port.Value, error) {
	v := newValue(ctx, key, d, cutoff, sc)
	period, pStart, pEnd := knPeriod(d)
	units := unitsOf(sc)
	err := p.s.pubRead(ctx, sc.TenantID, cutoff, func(tx pgx.Tx) error {
		var wmID *uuid.UUID
		var wmAt *time.Time
		switch key {
		case keySownAreaHa, keyCoopsReported:
			var sum string
			var coops int64
			if err := tx.QueryRow(ctx, `WITH cur AS (
			    SELECT DISTINCT ON (stream_key) id, recorded_at, report_kind, period, sown_area_ha, cooperative_ref, province_code, commune_code
			    FROM pf_extension_pub.v1_field_report WHERE recorded_at < $1 ORDER BY stream_key, seq DESC)
			  SELECT coalesce(sum(sown_area_ha), 0)::text, count(DISTINCT cooperative_ref),
			         (array_agg(id ORDER BY recorded_at DESC, id DESC))[1], max(recorded_at)
			  FROM cur WHERE report_kind = 'sowing_progress' AND period = $2
			    AND ($3::text[] IS NULL OR province_code = ANY($3) OR commune_code = ANY($3))`,
				cutoff.UTC(), period, units).Scan(&sum, &coops, &wmID, &wmAt); err != nil {
				return err
			}
			if key == keyCoopsReported {
				v.Value = decimal.NewFromInt(coops)
			} else {
				var err error
				if v.Value, err = decimal.NewFromString(sum); err != nil {
					return err
				}
			}
		case keyOpenTasks:
			var open int64
			if err := tx.QueryRow(ctx, `WITH last AS (
			    SELECT DISTINCT ON (task_id) task_id, id, recorded_at, status_after
			    FROM pf_extension_pub.v1_task_event WHERE recorded_at < $1 ORDER BY task_id, seq DESC)
			  SELECT count(*) FILTER (WHERE l.status_after <> 'resolved'),
			         (array_agg(l.id ORDER BY l.recorded_at DESC, l.id DESC))[1], max(l.recorded_at)
			  FROM last l JOIN pf_extension_pub.v1_task t ON t.task_id = l.task_id
			  WHERE ($2::text[] IS NULL OR t.province_code = ANY($2) OR t.commune_code = ANY($2))`,
				cutoff.UTC(), units).Scan(&open, &wmID, &wmAt); err != nil {
				return err
			}
			v.Value = decimal.NewFromInt(open)
		case keyDirectiveAckRate:
			var targets, acks int64
			if err := tx.QueryRow(ctx, `SELECT count(*), count(acked_at) FILTER (WHERE acked_at < $1),
			         greatest(max(directive_recorded_at), max(acked_at) FILTER (WHERE acked_at < $1))
			  FROM pf_extension_pub.v1_directive_target_ack
			  WHERE directive_recorded_at < $1 AND issued_at >= $2 AND issued_at < $3
			    AND ($4::text[] IS NULL OR province_code = ANY($4) OR unit_code = ANY($4))`,
				cutoff.UTC(), pStart, pEnd, units).Scan(&targets, &acks, &wmAt); err != nil {
				return err
			}
			v.Value = decimal.Zero
			if targets > 0 {
				v.Value = decimal.NewFromInt(acks * 100).Div(decimal.NewFromInt(targets)).Round(2)
			}
		default:
			return port.ErrUnknownMetric
		}
		setWatermark(&v, wmID, wmAt)
		return nil
	})
	return v, err
}

// Scopes: toàn tenant + từng tỉnh có dữ liệu.
func (p knPub) Scopes(ctx context.Context, _ port.Key, d clock.Date, tid uuid.UUID) ([]port.Scope, error) {
	out := []port.Scope{{TenantID: tid}}
	cutoff := port.Cutoff(d)
	err := p.s.pubRead(ctx, tid, cutoff, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT province_code FROM pf_extension_pub.v1_field_report WHERE recorded_at < $1
		  UNION SELECT province_code FROM pf_extension_pub.v1_task WHERE created_at < $1
		  UNION SELECT province_code FROM pf_extension_pub.v1_directive_target_ack WHERE target_issued_at < $1
		  ORDER BY 1`, cutoff.UTC())
		if err != nil {
			return err
		}
		codes, err := pgx.CollectRows(rows, pgx.RowTo[string])
		for _, c := range codes {
			out = append(out, port.Scope{TenantID: tid, Units: []string{c}})
		}
		return err
	})
	return out, err
}

// ---------------------------------------------------------------------------
// pf/geo — geo.plots_digitised; hình học as-of cho phát lại lớp GIS (port.GeometryResolver)
// ---------------------------------------------------------------------------

const keyPlotsDigitised port.Key = "geo.plots_digitised"

type geoPub struct{ s *Service }

func (p geoPub) AsOf(ctx context.Context, key port.Key, d clock.Date, sc port.Scope) (port.Value, error) {
	return asOfClosed(p.s, ctx, key, d, sc, p.AsOfAt)
}

func (p geoPub) AsOfAt(ctx context.Context, key port.Key, d clock.Date, cutoff time.Time, sc port.Scope) (port.Value, error) {
	v := newValue(ctx, key, d, cutoff, sc)
	if key != keyPlotsDigitised {
		return v, port.ErrUnknownMetric
	}
	err := p.s.pubRead(ctx, sc.TenantID, cutoff, func(tx pgx.Tx) error {
		var n int64
		var wmID *uuid.UUID
		var wmAt *time.Time
		if err := tx.QueryRow(ctx, `WITH ev AS (
		    SELECT DISTINCT ON (plot_ref) plot_ref, id, recorded_at, status, commune_code
		    FROM pf_geo_pub.v1_plot_event WHERE recorded_at < $1 ORDER BY plot_ref, seq DESC),
		  b AS (SELECT DISTINCT plot_ref FROM pf_geo_pub.v1_plot_boundary WHERE recorded_at < $1 AND became_current)
		  SELECT count(*), (array_agg(ev.id ORDER BY ev.recorded_at DESC, ev.id DESC))[1], max(ev.recorded_at)
		  FROM ev JOIN b ON b.plot_ref = ev.plot_ref
		  WHERE ev.status <> 'retired' AND ($2::text[] IS NULL OR ev.commune_code = ANY($2))`, cutoff.UTC(), unitsOf(sc)).Scan(&n, &wmID, &wmAt); err != nil {
			return err
		}
		v.Value = decimal.NewFromInt(n)
		setWatermark(&v, wmID, wmAt)
		return nil
	})
	return v, err
}

// Scopes: toàn tenant + từng xã có thửa.
func (p geoPub) Scopes(ctx context.Context, _ port.Key, d clock.Date, tid uuid.UUID) ([]port.Scope, error) {
	out := []port.Scope{{TenantID: tid}}
	cutoff := port.Cutoff(d)
	err := p.s.pubRead(ctx, tid, cutoff, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT DISTINCT commune_code FROM pf_geo_pub.v1_plot_event WHERE recorded_at < $1 ORDER BY 1`, cutoff.UTC())
		if err != nil {
			return err
		}
		codes, err := pgx.CollectRows(rows, pgx.RowTo[string])
		for _, c := range codes {
			out = append(out, port.Scope{TenantID: tid, Units: []string{c}})
		}
		return err
	})
	return out, err
}

// geometryKinds: loại dòng nguồn có hình học trong pf_geo_pub.v1_layer_geometry.
var geometryKinds = map[string]bool{"plot_boundary": true, "cooperative_boundary": true, "facility_event": true}

// PubGeometry là port.GeometryResolver trên pf_geo_pub.v1_layer_geometry (tenant lấy từ context). Dòng sổ bất
// biến nên đọc trên primary (không cần mốc cutoff).
type PubGeometry struct{ S *Service }

// Geometries thực hiện port.GeometryResolver.
func (g PubGeometry) Geometries(ctx context.Context, refs []port.GeometryRef) (map[port.GeometryRef]json.RawMessage, error) {
	byKind := map[string][]string{}
	for _, r := range refs {
		if geometryKinds[r.Kind] {
			byKind[r.Kind] = append(byKind[r.Kind], r.ID.String())
		}
	}
	out := map[port.GeometryRef]json.RawMessage{}
	if len(byKind) == 0 {
		return out, nil
	}
	if g.S.DB == nil {
		return nil, ErrDependency
	}
	err := tenant.FromCtxTx(ctx, g.S.DB, func(tx pgx.Tx) error {
		for kind, ids := range byKind {
			rows, err := tx.Query(ctx, `SELECT id, geojson FROM pf_geo_pub.v1_layer_geometry WHERE kind = $1 AND id = ANY($2::uuid[])`, kind, ids)
			if err != nil {
				return err
			}
			for rows.Next() {
				var id uuid.UUID
				var js string
				if err := rows.Scan(&id, &js); err != nil {
					rows.Close()
					return err
				}
				out[port.GeometryRef{Kind: kind, ID: id}] = json.RawMessage(js)
			}
			rows.Close()
			if err := rows.Err(); err != nil {
				return err
			}
		}
		return nil
	})
	return out, err
}

// ---------------------------------------------------------------------------
// pf/mechanization — cgh.coop_sufficiency_pct, cgh.coops_shortage (lần cân đối mới nhất của vụ trước Cutoff)
// ---------------------------------------------------------------------------

const (
	keyCoopSufficiencyPct port.Key = "cgh.coop_sufficiency_pct"
	keyCoopsShortage      port.Key = "cgh.coops_shortage"
)

type mechPub struct{ s *Service }

func (p mechPub) AsOf(ctx context.Context, key port.Key, d clock.Date, sc port.Scope) (port.Value, error) {
	return asOfClosed(p.s, ctx, key, d, sc, p.AsOfAt)
}

// AsOfAt: mức đáp ứng tổng thể mỗi HTX lấy từ view v1_coop_coverage (quy tắc QT-02 của module chủ, không tính
// lại cân đối) của lần cân đối mới nhất có recorded_at < cutoff.
func (p mechPub) AsOfAt(ctx context.Context, key port.Key, d clock.Date, cutoff time.Time, sc port.Scope) (port.Value, error) {
	v := newValue(ctx, key, d, cutoff, sc)
	if key != keyCoopSufficiencyPct && key != keyCoopsShortage {
		return v, port.ErrUnknownMetric
	}
	code, year, ok := port.ParseSeason(sc.Dimension)
	if !ok {
		return v, port.ErrNoData
	}
	err := p.s.pubRead(ctx, sc.TenantID, cutoff, func(tx pgx.Tx) error {
		var runID uuid.UUID
		var recorded time.Time
		var coops, sufficient, shortage int64
		err := tx.QueryRow(ctx, `WITH run AS (
		    SELECT id, recorded_at FROM pf_mechanization_pub.v1_balance_run
		    WHERE season_code = $1 AND season_year = $2 AND recorded_at < $3 ORDER BY seq DESC LIMIT 1)
		  SELECT run.id, run.recorded_at, count(DISTINCT c.cooperative_ref),
		         count(c.cooperative_ref) FILTER (WHERE c.status IN ('sufficient', 'surplus')),
		         count(c.cooperative_ref) FILTER (WHERE c.status = 'shortage')
		  FROM run LEFT JOIN pf_mechanization_pub.v1_coop_coverage c ON c.run_id = run.id
		       AND ($4::text[] IS NULL OR c.commune_code = ANY($4) OR c.province_code = ANY($4))
		  GROUP BY run.id, run.recorded_at`, code, year, cutoff.UTC(), unitsOf(sc)).Scan(&runID, &recorded, &coops, &sufficient, &shortage)
		if errors.Is(err, pgx.ErrNoRows) {
			return port.ErrNoData
		}
		if err != nil {
			return err
		}
		switch {
		case key == keyCoopsShortage:
			v.Value = decimal.NewFromInt(shortage)
		case coops == 0:
			v.Value = decimal.Zero
		default:
			v.Value = decimal.NewFromInt(sufficient * 100).Div(decimal.NewFromInt(coops)).Round(2)
		}
		v.Watermark = port.Watermark{LastEventID: runID, RecordedAt: recorded.UTC()}
		return nil
	})
	return v, err
}

// Scopes: mỗi vụ có lần cân đối trước Cutoff(D), toàn tenant và từng tỉnh có kết quả.
func (p mechPub) Scopes(ctx context.Context, _ port.Key, d clock.Date, tid uuid.UUID) ([]port.Scope, error) {
	cutoff := port.Cutoff(d)
	var out []port.Scope
	err := p.s.pubRead(ctx, tid, cutoff, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT DISTINCT r.season_code, r.season_year, coalesce(x.province_code, '')
		  FROM pf_mechanization_pub.v1_balance_run r
		  LEFT JOIN pf_mechanization_pub.v1_coop_coverage x ON x.run_id = r.id AND x.province_code IS NOT NULL
		  WHERE r.recorded_at < $1 ORDER BY 1, 2, 3`, cutoff.UTC())
		if err != nil {
			return err
		}
		defer rows.Close()
		seen := map[string]bool{}
		for rows.Next() {
			var code, prov string
			var year int
			if err := rows.Scan(&code, &year, &prov); err != nil {
				return err
			}
			dim := port.SeasonDimension(code, year)
			if !seen[dim] {
				seen[dim] = true
				out = append(out, port.Scope{TenantID: tid, Dimension: dim})
			}
			if prov != "" {
				out = append(out, port.Scope{TenantID: tid, Dimension: dim, Units: []string{prov}})
			}
		}
		return rows.Err()
	})
	return out, err
}

// ---------------------------------------------------------------------------
// Bản sao chỉ đọc (R-DB-10 §22.1)
// ---------------------------------------------------------------------------

// analytics chọn pool đọc phân tích: bản sao chỉ đọc (Service.Replica, AG_DB_REPLICA_URL) khi đã nối VÀ đã phát
// lại giao dịch tới mốc cutoff (pg_last_xact_replay_timestamp() ≥ cutoff) — giá trị ngày D đã ghi sổ không được
// thiếu dòng ghi nhận trước Cutoff(D) chỉ vì bản sao trễ; không thì primary. DSN trỏ vào primary (không ở chế độ
// phục hồi) → dùng luôn. Lỗi kiểm tra → primary (ghi cảnh báo).
func (s *Service) analytics(ctx context.Context, cutoff time.Time) tenant.Beginner {
	if s.Replica == nil {
		return s.DB
	}
	var recovery bool
	var replay *time.Time
	tx, err := s.Replica.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err == nil {
		err = tx.QueryRow(ctx, `SELECT pg_is_in_recovery(), pg_last_xact_replay_timestamp()`).Scan(&recovery, &replay)
		_ = tx.Rollback(ctx)
	}
	switch {
	case err != nil:
		if s.Log != nil {
			s.Log.Warn("bản sao chỉ đọc không dùng được — đọc primary", "event", "metrics.replica_fallback", "error", err.Error())
		}
		return s.DB
	case !recovery, replay != nil && !replay.Before(cutoff):
		return s.Replica
	}
	if s.Log != nil {
		s.Log.Info("bản sao chỉ đọc chưa phát lại tới cutoff — đọc primary", "event", "metrics.replica_lagging",
			"cutoff", cutoff.UTC().Format(time.RFC3339), "replay", replay)
	}
	return s.DB
}
