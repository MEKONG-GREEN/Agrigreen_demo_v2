package migrations_test

import (
	"context"
	"fmt"
	"io/fs"
	"maps"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/db"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

// W3.3 (docs/design/cooperative.md mục 8, docs/runbooks/cooperative-schema-move.md): diễn tập chuyển schema trên một
// database có dữ liệu đại diện. Database dừng ở migration pf cuối trước 000131 (trước W3.3), nạp dữ liệu vào 22 bảng (2 tenant, sổ có
// chuỗi nhiều dòng, dòng ghi TRƯỚC khi chuyển), rồi nâng lên đầu qua 000131–000133 và kiểm:
//   - đếm dòng và tổng kiểm row_hash từng bảng giữ nguyên; phân vùng chuyển hết sang pf_coop;
//   - opsjobs.VerifyAllChains sạch trước và sau, tên sổ (chain_head.ledger) không đổi;
//   - chuỗi băm và chuỗi số tiếp tục: ghi tiếp vào chuỗi cũ dưới vai r_cooperative → seq + 1, prev_hash nối;
//     sync_seq mới > giá trị trước khi chuyển;
//   - RLS bật + ép; quyền: chỉ r_cooperative, không còn quyền nào của r_party / r_geo / r_cropcycle.
var movedTables = []string{
	"cooperative", "household", "household_membership", "kn_profile_event",
	"plot", "plot_boundary", "plot_event", "boundary_conflict", "cooperative_boundary", "cooperative_area",
	"sop_template", "sop_version", "variety_sop_binding", "season_event", "farm_log_batch", "farm_log",
	"farm_log_review", "harvest_declaration", "season_current", "farm_log_current", "coop_season_area", "yield_summary",
}

var oldSchema = map[string]string{
	"cooperative": "pf_party", "household": "pf_party", "household_membership": "pf_party", "kn_profile_event": "pf_party",
	"plot": "pf_geo", "plot_boundary": "pf_geo", "plot_event": "pf_geo", "boundary_conflict": "pf_geo",
	"cooperative_boundary": "pf_geo", "cooperative_area": "pf_geo",
}

func schemaOf(t string) string {
	if s, ok := oldSchema[t]; ok {
		return s
	}
	return "pf_cropcycle"
}

// seedCoopSQL nạp dữ liệu đại diện cho tenant của giao dịch (app.tenant_id) ở lược đồ pf 000124. Mỗi lần gọi một
// "lô" (HTX, hộ, thửa, SOP, vụ, nhật ký, sản lượng); gọi hai lần cùng tenant → sổ có chuỗi nhiều dòng khác nhau.
const seedCoopSQL = `DO $$
DECLARE
  t       uuid := current_setting('app.tenant_id')::uuid;
  coop    uuid := gen_random_uuid();
  hh      uuid := gen_random_uuid();
  hh2     uuid := gen_random_uuid();
  plot    uuid := gen_random_uuid();
  bnd     uuid := gen_random_uuid();
  cbnd    uuid := gen_random_uuid();
  tmpl    uuid := gen_random_uuid();
  ver     uuid := gen_random_uuid();
  season  uuid := gen_random_uuid();
  sev     uuid := gen_random_uuid();
  lg      uuid := gen_random_uuid();
  batch   uuid := gen_random_uuid();
  harvest uuid := gen_random_uuid();
  actor   uuid := gen_random_uuid();
  rev     uuid := gen_random_uuid();
  code    text := 'TH' || substr(md5(plot::text), 1, 8);
  g       geometry := ST_Multi(ST_GeomFromText('POLYGON((105 10, 105.001 10, 105.001 10.001, 105 10.001, 105 10))', 4326));
BEGIN
  INSERT INTO pf_party.org (tenant_id, ref, kind, code, name, admin_unit_code, last_event_id, last_seq)
  VALUES (t, coop, 'cooperative', 'HTX' || substr(md5(coop::text), 1, 6), 'HTX diễn tập ' || coop, '31117', gen_random_uuid(), 1);
  INSERT INTO pf_party.cooperative (tenant_id, org_ref, tax_code, area_ha, established_on, location, head_office)
  VALUES (t, coop, lpad((abs(hashtext(coop::text)) % 10000000000)::text, 10, '0'), 120.5, '2015-01-01',
          ST_SetSRID(ST_MakePoint(105.6, 10.05), 4326), 'Ấp Tổng Hợp 1');
  INSERT INTO pf_party.household (tenant_id, household_ref, commune_code, cultivated_area_ha, created_by)
  VALUES (t, hh, '31117', 2.5, actor), (t, hh2, '31117', 1.0, actor);
  INSERT INTO pf_party.household_membership (tenant_id, id, household_ref, org_ref, since_date, until_date)
  VALUES (t, gen_random_uuid(), hh, coop, '2020-01-01', NULL), (t, gen_random_uuid(), hh2, coop, '2020-01-01', '2024-12-31');
  -- Sổ hồ sơ KN: hai dòng cùng chuỗi (HTX) + một dòng chuỗi hộ.
  INSERT INTO pf_party.kn_profile_event (id, tenant_id, stream_key, subject_kind, subject_ref, commune_code, action, before, after,
      event_time, actor_ref, idem_key, rule_version)
  VALUES (gen_random_uuid(), t, 'cooperative:' || coop, 'cooperative', coop, '31117', 'updated', '{}', '{"members": 10}', now(), actor, gen_random_uuid()::text, 'party/kn/v1'),
         (gen_random_uuid(), t, 'cooperative:' || coop, 'cooperative', coop, '31117', 'updated', '{"members": 10}', '{"members": 12}', now(), actor, gen_random_uuid()::text, 'party/kn/v1'),
         (gen_random_uuid(), t, 'household:' || hh, 'household', hh, '31117', 'updated', '{}', '{"area": "2.5"}', now(), actor, gen_random_uuid()::text, 'party/kn/v1');

  INSERT INTO pf_geo.plot (tenant_id, ref, code, commune_code, cooperative_ref, household_ref, status, current_version, current_boundary_id,
      geom, area_ha, source, created_by)
  VALUES (t, plot, code, '31117', coop, hh, 'active', 1, bnd, g, 1.2345, 'kn_web', actor);
  INSERT INTO pf_geo.plot_boundary (id, tenant_id, stream_key, plot_ref, geom, area_ha, point_count, source, commune_code, cooperative_ref,
      became_current, event_time, actor_ref, idem_key, rule_version)
  VALUES (bnd, t, 'plot:' || plot, plot, g, 1.2345, 4, 'kn_web', '31117', coop, true, now(), actor, gen_random_uuid()::text, 'geo/v1');
  INSERT INTO pf_geo.plot_event (id, tenant_id, stream_key, plot_ref, action, code, commune_code, cooperative_ref, household_ref, status,
      event_time, actor_ref, idem_key, rule_version)
  VALUES (gen_random_uuid(), t, 'plot:' || plot, plot, 'created', code, '31117', NULL, NULL, 'draft', now(), actor, gen_random_uuid()::text, 'geo/v1'),
         (gen_random_uuid(), t, 'plot:' || plot, plot, 'assigned', code, '31117', coop, hh, 'active', now(), actor, gen_random_uuid()::text, 'geo/v1');
  INSERT INTO pf_geo.boundary_conflict (tenant_id, id, kind, plot_ref, boundary_id, other_plot_refs, max_overlap_pct, commune_code, owner_ref,
      due_at, status, detected_at)
  VALUES (t, gen_random_uuid(), 'same_coop_overlap', plot, bnd, ARRAY[gen_random_uuid()], 3.5, '31117', coop, now() + interval '7 days', 'open', now());
  INSERT INTO pf_geo.cooperative_boundary (id, tenant_id, stream_key, cooperative_ref, action, geom, area_ha, point_count, source, commune_code,
      event_time, actor_ref, idem_key, rule_version)
  VALUES (cbnd, t, 'cooperative:' || coop, coop, 'recorded', g, 1.2345, 4, 'geo_web', '31117', now(), actor, gen_random_uuid()::text, 'geo/g2/v1');
  INSERT INTO pf_geo.cooperative_area (tenant_id, cooperative_ref, commune_code, status, current_version, current_boundary_id, geom, area_ha,
      source, created_by)
  VALUES (t, coop, '31117', 'active', 1, cbnd, g, 1.2345, 'geo_web', actor);

  INSERT INTO pf_cropcycle.sop_template (tenant_id, ref, code, name, name_key, crop, scope, latest_version, created_by)
  VALUES (t, tmpl, 'SOP-' || upper(substr(md5(tmpl::text), 1, 8)), 'SOP diễn tập ' || tmpl, 'sop diễn tập ' || tmpl, 'lua', 'system', 1, actor);
  INSERT INTO pf_cropcycle.sop_version (tenant_id, id, template_ref, version_no, status, steps, step_count, authored_by, authored_at)
  VALUES (t, ver, tmpl, 1, 'draft', '[]', 0, actor, now());
  INSERT INTO pf_cropcycle.variety_sop_binding (tenant_id, id, variety_code, template_ref, bound_by)
  VALUES (t, gen_random_uuid(), 'V' || substr(md5(tmpl::text), 1, 8), tmpl, actor);

  INSERT INTO pf_cropcycle.season_event (id, tenant_id, stream_key, season_ref, action, plot_ref, plot_code, household_ref, cooperative_ref,
      commune_code, season_code, season_year, variety_code, sop_template_ref, sop_version_id, sop_version_no, sowing_date,
      expected_harvest_date, harvest_open_date, area_ha, gps, gps_source, location_status, on_behalf, channel, event_time, actor_ref,
      idem_key, rule_version)
  VALUES (sev, t, 'season:' || season, season, 'opened', plot, code, hh, coop, '31117', 'DX', 2026, 'OM5451', tmpl, ver, 1,
          '2026-01-05', '2026-04-10', '2026-04-05', 1.2345, ST_SetSRID(ST_MakePoint(105.0005, 10.0005), 4326), 'device', 'match',
          false, 'app', now(), actor, gen_random_uuid()::text, 'cropcycle/v1');
  INSERT INTO pf_cropcycle.season_current (tenant_id, season_ref, plot_ref, plot_code, household_ref, cooperative_ref, commune_code,
      season_code, season_year, variety_code, sop_template_ref, sop_version_id, sop_version_no, sowing_date, expected_harvest_date,
      harvest_open_date, area_ha, status, current_event_id, version, location_status, gps_source, on_behalf, opened_by, opened_at)
  VALUES (t, season, plot, code, hh, coop, '31117', 'DX', 2026, 'OM5451', tmpl, ver, 1, '2026-01-05', '2026-04-10', '2026-04-05',
          1.2345, 'open', sev, 1, 'match', 'device', false, actor, now());
  INSERT INTO pf_cropcycle.farm_log_batch (id, tenant_id, stream_key, cooperative_ref, activity_code, requested, accepted, skipped, inputs,
      on_behalf, channel, event_time, actor_ref, idem_key, rule_version)
  VALUES (batch, t, 'farm_log_batch:' || batch, coop, 'bon_phan', 1, 1, '[]', '[]', true, 'web', now(), actor, gen_random_uuid()::text, 'cropcycle/v1');
  INSERT INTO pf_cropcycle.farm_log (id, tenant_id, stream_key, log_ref, season_ref, plot_ref, household_ref, cooperative_ref, commune_code,
      activity_code, gps_source, location_status, machine_codes, inputs, file_refs, batch_ref, became_current, on_behalf, channel, event_time,
      actor_ref, idem_key, rule_version)
  VALUES (lg, t, 'farm_log:' || lg, lg, season, plot, hh, coop, '31117', 'bon_phan', 'device', 'match', ARRAY['MPT'],
          '[{"name": "NPK", "qty": "50", "unit": "kg"}]', ARRAY[]::uuid[], batch, true, true, 'web', now(), actor, gen_random_uuid()::text, 'cropcycle/v1');
  INSERT INTO pf_cropcycle.farm_log_review (id, tenant_id, stream_key, log_ref, log_version_id, season_ref, cooperative_ref, decision,
      author_ref, event_time, actor_ref, idem_key, rule_version)
  VALUES (gen_random_uuid(), t, 'farm_log:' || lg || ':review', lg, lg, season, coop, 'approved', actor, now(), rev, gen_random_uuid()::text, 'cropcycle/v1');
  INSERT INTO pf_cropcycle.farm_log_current (tenant_id, id, log_ref, version, season_ref, plot_ref, household_ref, cooperative_ref, commune_code,
      activity_code, event_time, gps_source, location_status, author_ref, on_behalf, batch_ref, status, last_review_id, reviewed_by,
      reviewed_at, recorded_at)
  VALUES (t, lg, lg, 1, season, plot, hh, coop, '31117', 'bon_phan', now(), 'device', 'match', actor, true, batch, 'approved', NULL, rev, now(), now());
  INSERT INTO pf_cropcycle.harvest_declaration (id, tenant_id, stream_key, season_ref, plot_ref, household_ref, cooperative_ref, commune_code,
      season_code, season_year, variety_code, harvest_date, mass_kg, area_ha, yield_t_per_ha, anomaly_confirmed, source, gps_source,
      location_status, on_behalf, event_time, actor_ref, idem_key, rule_version)
  VALUES (harvest, t, 'harvest:' || season, season, plot, hh, coop, '31117', 'DX', 2026, 'OM5451', '2026-04-08', 8000.000, 1.2345, 6.4803,
          false, 'app', 'device', 'match', false, now(), actor, gen_random_uuid()::text, 'cropcycle/v1');
  INSERT INTO pf_cropcycle.coop_season_area (tenant_id, cooperative_ref, season_code, season_year, area_ha, seasons, open_seasons, completed_seasons)
  VALUES (t, coop, 'DX', 2026, 1.2345, 1, 1, 0);
  INSERT INTO pf_cropcycle.yield_summary (tenant_id, season_ref, plot_ref, household_ref, cooperative_ref, commune_code, season_code, season_year,
      variety_code, status, area_ha, sop_steps, sop_mandatory_steps, sop_steps_done, sop_mandatory_done, approved_logs, pending_logs)
  VALUES (t, season, plot, hh, coop, '31117', 'DX', 2026, 'OM5451', 'open', 1.2345, 0, 0, 0, 0, 1, 0);
END
$$`

type tableStat struct {
	rows int64
	sum  int64 // tổng hashtext của row_hash (sổ) hoặc của cả dòng (bảng khác)
}

func coopStats(ctx context.Context, t *testing.T, admin *pgxpool.Pool, schema func(string) string) map[string]tableStat {
	t.Helper()
	out := map[string]tableStat{}
	for _, tbl := range movedTables {
		var s tableStat
		// admin (superuser) không bị RLS lọc: đếm toàn bộ tenant. Bảng sổ: tổng kiểm theo row_hash; bảng khác: theo
		// nội dung dòng (to_jsonb), không phụ thuộc vị trí vật lý.
		q := fmt.Sprintf(`SELECT count(*), coalesce(sum(hashtext(%s)), 0) FROM %s.%s x`, sumExpr(tbl), schema(tbl), tbl)
		if err := admin.QueryRow(ctx, q).Scan(&s.rows, &s.sum); err != nil {
			t.Fatalf("%s: %v", tbl, err)
		}
		out[tbl] = s
	}
	return out
}

func sumExpr(tbl string) string {
	switch tbl {
	case "kn_profile_event", "plot_boundary", "plot_event", "cooperative_boundary", "season_event", "farm_log_batch", "farm_log",
		"farm_log_review", "harvest_declaration":
		return "x.row_hash::text"
	}
	return "to_jsonb(x)::text"
}

func partitionCount(ctx context.Context, t *testing.T, admin *pgxpool.Pool, where string) int {
	t.Helper()
	var n int
	if err := admin.QueryRow(ctx, `SELECT count(*) FROM pg_inherits i JOIN pg_class c ON c.oid = i.inhrelid
	    JOIN pg_class p ON p.oid = i.inhparent WHERE c.relkind IN ('r', 'p') AND `+where).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// pubViews là view công bố (W6.3) đọc bảng do W3.3 chuyển sang pf_coop.
var pubViews = []string{"pf_cropcycle_pub.v1_season_event", "pf_cropcycle_pub.v1_harvest_declaration", "pf_geo_pub.v1_plot_event",
	"pf_geo_pub.v1_plot_boundary", "pf_geo_pub.v1_layer_geometry"}

// pubCounts đếm dòng của từng view công bố theo tenant (RLS áp qua view: chủ view không vượt RLS).
func pubCounts(ctx context.Context, t *testing.T, pool *pgxpool.Pool, tenants []uuid.UUID) map[string]int64 {
	t.Helper()
	out := map[string]int64{}
	for _, tid := range tenants {
		for _, v := range pubViews {
			var n int64
			if err := tenant.WithTenantTx(ctx, pool, tid, func(tx pgx.Tx) error {
				return tx.QueryRow(ctx, "SELECT count(*) FROM "+v).Scan(&n)
			}); err != nil {
				t.Fatalf("%s: %v", v, err)
			}
			if n == 0 {
				t.Fatalf("%s: tenant %s không có dòng", v, tid)
			}
			out[tid.String()+" "+v] = n
		}
	}
	return out
}

func TestCooperativeSchemaMoveRehearsal_W33(t *testing.T) {
	ctx := context.Background()
	pool, dsn := upgradeEnv(t, migrations.PF, lastBefore(t, "pf", 131))
	env := pgtest.Get(t)
	tenants := []uuid.UUID{ids.New(), ids.New()}
	for _, tid := range tenants {
		for range 2 {
			if err := tenant.WithTenantTx(ctx, pool, tid, func(tx pgx.Tx) error {
				_, err := tx.Exec(ctx, seedCoopSQL)
				return err
			}); err != nil {
				t.Fatalf("nạp dữ liệu: %v", err)
			}
		}
	}
	// Admin (superuser) của database nâng cấp — không bị RLS lọc.
	admin := adminPoolFor(t, env.AdminURL, dsn)
	before := coopStats(ctx, t, admin, schemaOf)
	for _, tbl := range movedTables {
		if before[tbl].rows == 0 {
			t.Fatalf("dữ liệu đại diện thiếu bảng %s", tbl)
		}
	}
	partsBefore := partitionCount(ctx, t, admin, `p.relnamespace IN ('pf_party'::regnamespace, 'pf_geo'::regnamespace, 'pf_cropcycle'::regnamespace)
	    AND p.relname = ANY ('{kn_profile_event,plot_boundary,plot_event,cooperative_boundary,season_event,farm_log_batch,farm_log,farm_log_review,harvest_declaration}'::text[])`)
	var syncBefore int64
	if err := admin.QueryRow(ctx, `SELECT max(sync_seq) FROM pf_geo.plot`).Scan(&syncBefore); err != nil {
		t.Fatal(err)
	}
	ledgersBefore := verifyAll(t, pool)
	pubBefore := pubCounts(ctx, t, pool, tenants)

	start := time.Now()
	migrateDir(t, dsn, "pf", 0) // 000131–000133 (và mọi migration sau bản trước W3.3)
	t.Logf("pf 000131–000133 + phần còn lại: %s", time.Since(start))

	after := coopStats(ctx, t, admin, func(string) string { return "pf_coop" })
	for _, tbl := range movedTables {
		if after[tbl] != before[tbl] {
			t.Errorf("%s: trước %+v, sau %+v", tbl, before[tbl], after[tbl])
		}
		var leftover bool
		if err := admin.QueryRow(ctx, `SELECT to_regclass($1) IS NOT NULL`, schemaOf(tbl)+"."+tbl).Scan(&leftover); err != nil || leftover {
			t.Errorf("%s còn ở schema cũ (%v)", tbl, err)
		}
	}
	// Chỉ bảng đã chuyển — sổ mới tạo sau W3.3 ở pf_coop (vd W5.6 bilateral_*) không tính.
	if n := partitionCount(ctx, t, admin, `p.relnamespace = 'pf_coop'::regnamespace
	    AND p.relname = ANY ('{kn_profile_event,plot_boundary,plot_event,cooperative_boundary,season_event,farm_log_batch,farm_log,farm_log_review,harvest_declaration}'::text[])`); n != partsBefore || n == 0 {
		t.Errorf("phân vùng ở pf_coop: %d, trước %d", n, partsBefore)
	}
	if n := partitionCount(ctx, t, admin, `p.relnamespace = 'pf_coop'::regnamespace AND c.relnamespace <> 'pf_coop'::regnamespace`); n != 0 {
		t.Errorf("%d phân vùng còn ở schema cũ", n)
	}
	ledgersAfter := verifyAll(t, pool) // tên sổ cũ, chuỗi sạch
	// View công bố W6.3 (pf 000124) trên bảng đã chuyển bám theo OID: cùng số dòng theo tenant, vẫn đọc được.
	if pubAfter := pubCounts(ctx, t, pool, tenants); !maps.Equal(pubAfter, pubBefore) {
		t.Errorf("view _pub: trước %v, sau %v", pubBefore, pubAfter)
	}
	if len(ledgersAfter) != len(ledgersBefore) {
		t.Errorf("sổ trước %v, sau %v", ledgersBefore, ledgersAfter)
	}
	for l, n := range ledgersBefore {
		if ledgersAfter[l] != n {
			t.Errorf("sổ %s: trước %d dòng, sau %d", l, n, ledgersAfter[l])
		}
	}
	for _, l := range []string{"pf_geo.plot_boundary", "pf_cropcycle.farm_log", "pf_party.kn_profile_event"} {
		if ledgersAfter[l] == 0 {
			t.Errorf("tên sổ %s phải giữ nguyên (D-3)", l)
		}
	}

	// RLS bật + ép trên mọi bảng pf_coop (cả phân vùng).
	var noRLS []string
	rows, err := admin.Query(ctx, `SELECT c.oid::regclass::text FROM pg_class c WHERE c.relnamespace = 'pf_coop'::regnamespace
	    AND c.relkind IN ('r', 'p') AND NOT (c.relrowsecurity AND c.relforcerowsecurity)`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var s string
		_ = rows.Scan(&s)
		noRLS = append(noRLS, s)
	}
	rows.Close()
	if len(noRLS) > 0 {
		t.Errorf("thiếu FORCE RLS: %v", noRLS)
	}

	// Quyền: r_party, r_geo, r_cropcycle không chạm được pf_coop; r_cooperative không chạm được schema cũ.
	var leaks []string
	rows, err = admin.Query(ctx, `SELECT r.rolname || ' → ' || c.oid::regclass::text
	    FROM pg_class c CROSS JOIN pg_roles r
	   WHERE c.relkind IN ('r', 'p', 'S')
	     AND ((c.relnamespace = 'pf_coop'::regnamespace AND r.rolname IN ('r_party', 'r_geo', 'r_cropcycle', 'party_app', 'geo_app', 'cropcycle_app'))
	       OR (c.relnamespace IN ('pf_party'::regnamespace, 'pf_geo'::regnamespace, 'pf_cropcycle'::regnamespace) AND r.rolname = 'r_cooperative'))
	     AND ((c.relkind <> 'S' AND (has_table_privilege(r.oid, c.oid, 'SELECT') OR has_table_privilege(r.oid, c.oid, 'INSERT')
	            OR has_table_privilege(r.oid, c.oid, 'UPDATE') OR has_table_privilege(r.oid, c.oid, 'DELETE')))
	       OR (c.relkind = 'S' AND has_sequence_privilege(r.oid, c.oid, 'USAGE')))
	   LIMIT 20`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var s string
		_ = rows.Scan(&s)
		leaks = append(leaks, s)
	}
	rows.Close()
	if len(leaks) > 0 {
		t.Errorf("quyền chéo còn sót: %v", leaks)
	}

	// Ghi tiếp dưới vai r_cooperative (vai đăng nhập đơn vị app_platform → SET LOCAL ROLE): chuỗi băm nối tiếp, chuỗi
	// số tiếp tục; r_geo bị từ chối.
	appDSN, err := dbadmin.RoleURL(env.DirectURL, migrations.PF, suffixOf(t, dsn), dbadmin.AppPlatform)
	if err != nil {
		t.Fatal(err)
	}
	app, err := pgxpool.New(ctx, appDSN)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(app.Close)
	coop := db.ForModule(app, db.Module("pf/cooperative"))
	tid := tenants[0]
	var plotRef uuid.UUID
	var prevSeq int64
	if err := tenant.WithTenantTx(ctx, coop, tid, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `SELECT plot_ref, max(seq) FROM pf_coop.plot_boundary GROUP BY plot_ref ORDER BY plot_ref LIMIT 1`).
			Scan(&plotRef, &prevSeq); err != nil {
			return err
		}
		var seq int64
		if err := tx.QueryRow(ctx, `INSERT INTO pf_coop.plot_boundary (id, tenant_id, stream_key, plot_ref, geom, area_ha, point_count,
		    source, commune_code, became_current, event_time, actor_ref, idem_key, rule_version, supersedes_id, correction_reason)
		  SELECT $1::uuid, $2::uuid, 'plot:' || $3::text, $3::uuid, b.geom, b.area_ha, b.point_count, 'kn_web', '31117', true, now(), $4::uuid, $5::text, 'geo/v2',
		         b.id, 'sửa ranh sau khi chuyển schema'
		    FROM pf_coop.plot_boundary b WHERE b.plot_ref = $3::uuid ORDER BY b.seq DESC LIMIT 1
		  RETURNING seq`, ids.New(), tid, plotRef, ids.New(), ids.New().String()).Scan(&seq); err != nil {
			return fmt.Errorf("ghi tiếp sổ dưới r_cooperative: %w", err)
		}
		if seq != prevSeq+1 {
			return fmt.Errorf("seq = %d, muốn %d", seq, prevSeq+1)
		}
		var sync int64
		if err := tx.QueryRow(ctx, `UPDATE pf_coop.plot SET needs_review = true, sync_seq = nextval('pf_coop.plot_sync_seq')
		    WHERE ref = $1::uuid RETURNING sync_seq`, plotRef).Scan(&sync); err != nil {
			return err
		}
		if sync <= syncBefore {
			return fmt.Errorf("sync_seq %d không tiếp tục sau %d", sync, syncBefore)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	final := verifyAll(t, pool)
	if final["pf_geo.plot_boundary"] != ledgersAfter["pf_geo.plot_boundary"]+1 {
		t.Errorf("dòng ghi tiếp phải nối vào sổ 'pf_geo.plot_boundary': %d → %d", ledgersAfter["pf_geo.plot_boundary"], final["pf_geo.plot_boundary"])
	}
	geo := db.ForModule(app, db.Module("pf/geo"))
	err = tenant.WithTenantTx(ctx, geo, tid, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `SELECT 1 FROM pf_coop.plot LIMIT 1`)
		return err
	})
	if err == nil {
		t.Fatal("r_geo phải bị từ chối trên pf_coop")
	}
	if len(final) == 0 {
		t.Fatal("không có sổ nào được xác minh")
	}
}

// suffixOf trả hậu tố database kiểm thử từ DSN của upgradeEnv (ag_pf_<hậu tố>).
func suffixOf(t *testing.T, dsn string) string {
	t.Helper()
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	name := strings.TrimPrefix(u.Path, "/")
	_, suffix, ok := strings.Cut(name, "ag_"+string(migrations.PF)+"_")
	if !ok || suffix == "" {
		t.Fatalf("DSN không có hậu tố kiểm thử: %s", name)
	}
	return suffix
}

// adminPoolFor mở pool tài khoản quản trị cụm tới database của dsn (superuser — không bị RLS lọc).
func adminPoolFor(t *testing.T, adminURL, dsn string) *pgxpool.Pool {
	t.Helper()
	u, err := url.Parse(adminURL)
	if err != nil {
		t.Fatal(err)
	}
	d, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	u.Path = d.Path
	p, err := pgxpool.New(context.Background(), u.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(p.Close)
	return p
}

// lastBefore trả số migration lớn nhất < n của thư mục cụm (pf 000120–000124 do gói song song cấp — có thể chưa có).
func lastBefore(t *testing.T, dir string, n uint) uint {
	t.Helper()
	entries, err := fs.ReadDir(migrations.FS, dir)
	if err != nil {
		t.Fatal(err)
	}
	var best uint
	for _, e := range entries {
		var v uint
		if _, err := fmt.Sscanf(e.Name(), "%06d_", &v); err == nil && v < n && v > best {
			best = v
		}
	}
	if best == 0 {
		t.Fatalf("không có migration %s trước %d", dir, n)
	}
	return best
}
