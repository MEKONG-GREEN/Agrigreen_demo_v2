package migrations_test

import (
	"context"
	"io/fs"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

// PA1-HTX-SUPPORT-GPSLOG (#313): diễn tập chép một lần yêu cầu hỗ trợ App HTX từ bảng KN pf_extension.support_request
// sang bảng HTX pf_coop.support_request (pf 20261116020729–31). Database dừng ở migration pf cuối trước gói, nạp yêu cầu
// cũ (kèm nhiệm vụ KN) cho hai tenant, nâng lên đầu rồi kiểm: đếm trước/sau bằng scripts/pa1-313-support-copy/count.sql
// khớp; từng dòng giữ id, khóa idem, toạ độ, ảnh, trạng thái nhiệm vụ lúc chép, sync_seq; chuỗi số mới tiếp sau; bảng
// cũ còn đủ dòng nhưng vai KN hết quyền INSERT.

// seedSupportSQL nạp một yêu cầu hỗ trợ cũ (sổ task_event + task_current + support_request) cho tenant của giao dịch.
// $1 id yêu cầu, $2 trạng thái nhiệm vụ, $3 có GPS.
const seedSupportSQL = `WITH t AS (SELECT current_setting('app.tenant_id')::uuid AS tid, gen_random_uuid() AS task, gen_random_uuid() AS ev,
    gen_random_uuid() AS coop, gen_random_uuid() AS hh, gen_random_uuid() AS actor),
  e AS (INSERT INTO pf_extension.task_event (id, tenant_id, stream_key, task_id, action, status_after, due_at, event_time, actor_ref,
      idem_key, rule_version)
    SELECT ev, tid, 'task:' || task, task, 'created', 'new', now() + interval '24 hours', now(), actor, 'support:' || $1::text, 'extension/v1'
    FROM t RETURNING id),
  c AS (INSERT INTO pf_extension.task_current (tenant_id, task_id, commune_code, province_code, cooperative_ref, household_ref,
      category, description, priority, source, status, created_by, created_at, sla_hours, due_at, last_event_id, last_seq)
    SELECT tid, task, '31117', '31', coop, hh, 'sau_benh', 'Rầy nâu xuất hiện dày ở lô 3', 'urgent', 'htx_app', $2::text, actor,
      now(), 24, now() + interval '24 hours', ev, 1 FROM t RETURNING task_id)
INSERT INTO pf_extension.support_request (tenant_id, id, household_ref, cooperative_ref, commune_code, province_code, category,
    urgency, description, photo_refs, location, gps_accuracy_m, event_time, received_at, task_id, task_created_at, assignee_ref,
    assignment_rule, submitted_by, idem_key)
  SELECT tid, $1::uuid, hh, coop, '31117', '31', 'dich_hai', 'khan', 'Rầy nâu xuất hiện dày ở lô 3, lúa đang trổ',
    CASE WHEN $3::boolean THEN ARRAY[gen_random_uuid(), gen_random_uuid()] ELSE '{}'::uuid[] END,
    CASE WHEN $3::boolean THEN ST_SetSRID(ST_MakePoint(105.4352, 10.3861), 4326) END,
    CASE WHEN $3::boolean THEN 8 END, now() - interval '1 hour', now(), task, now(), NULL, 'admin_queue', actor, 'k-' || $1::text
  FROM t`

// lastVersionBefore trả phiên bản migration lớn nhất < v của thư mục cụm (tên dạng số hoặc mốc thời gian).
func lastVersionBefore(t *testing.T, dir string, v uint64) uint {
	t.Helper()
	entries, err := fs.ReadDir(migrations.FS, dir)
	if err != nil {
		t.Fatal(err)
	}
	var best uint64
	for _, e := range entries {
		head, _, _ := strings.Cut(e.Name(), "_")
		n, err := strconv.ParseUint(head, 10, 64)
		if err == nil && n < v && n > best {
			best = n
		}
	}
	return uint(best)
}

func TestSupportRequestCopyToCooperative_PA1_313(t *testing.T) {
	ctx := context.Background()
	pool, dsn := upgradeEnv(t, migrations.PF, lastVersionBefore(t, "pf", 20261116020729))
	env := pgtest.Get(t)
	type seed struct {
		tid    uuid.UUID
		id     uuid.UUID
		status string
		gps    bool
	}
	ta, tb := ids.New(), ids.New()
	seeds := []seed{{ta, ids.New(), "assigned", true}, {ta, ids.New(), "resolved", false}, {tb, ids.New(), "new", true}}
	for _, s := range seeds {
		if err := tenant.WithTenantTx(ctx, pool, s.tid, func(tx pgx.Tx) error {
			_, err := tx.Exec(ctx, seedSupportSQL, s.id, s.status, s.gps)
			return err
		}); err != nil {
			t.Fatalf("nạp yêu cầu cũ: %v", err)
		}
	}
	admin := adminPoolFor(t, env.AdminURL, dsn)
	script, err := os.ReadFile("../scripts/pa1-313-support-copy/count.sql")
	if err != nil {
		t.Fatal(err)
	}
	before, after, ok := strings.Cut(string(script), "-- 2. Sau")
	if !ok {
		t.Fatal("count.sql thiếu mục 2. Sau")
	}
	_, before, _ = strings.Cut(before, "-- 1. Trước")
	kn := map[uuid.UUID]int64{}
	rows, err := admin.Query(ctx, before)
	if err != nil {
		t.Fatalf("count.sql mục 1: %v", err)
	}
	for rows.Next() {
		var tid uuid.UUID
		var n int64
		if err := rows.Scan(&tid, &n); err != nil {
			t.Fatal(err)
		}
		kn[tid] = n
	}
	rows.Close()
	if kn[ta] != 2 || kn[tb] != 1 {
		t.Fatalf("đếm trước: %v", kn)
	}
	var taskSeqMax int64
	if err := admin.QueryRow(ctx, `SELECT max(sync_seq) FROM pf_extension.task_current`).Scan(&taskSeqMax); err != nil {
		t.Fatal(err)
	}

	migrateDir(t, dsn, "pf", 0)

	// Mục 2 của kịch bản đếm: mọi (tenant, HTX) khớp, dòng TONG = 3/3.
	rows, err = admin.Query(ctx, after)
	if err != nil {
		t.Fatalf("count.sql mục 2: %v", err)
	}
	var total struct{ kn, htx int64 }
	lines := 0
	for rows.Next() {
		var tid, coop string
		var knRows, htxRows, knNoGPS, htxNoGPS, knPhotos, htxPhotos int64
		var match bool
		if err := rows.Scan(&tid, &coop, &knRows, &htxRows, &knNoGPS, &htxNoGPS, &knPhotos, &htxPhotos, &match); err != nil {
			t.Fatal(err)
		}
		lines++
		if !match {
			t.Errorf("không khớp %s/%s: kn %d htx %d, thiếu GPS %d/%d, ảnh %d/%d", tid, coop, knRows, htxRows, knNoGPS, htxNoGPS, knPhotos, htxPhotos)
		}
		if tid == "TONG" {
			total.kn, total.htx = knRows, htxRows
		}
	}
	rows.Close()
	if lines != 4 || total.kn != 3 || total.htx != 3 {
		t.Fatalf("đếm sau: %d dòng, tổng kn %d htx %d", lines, total.kn, total.htx)
	}

	// Từng dòng giữ dữ liệu và trạng thái nhiệm vụ lúc chép; sync_seq = sync_seq nhiệm vụ cũ.
	for _, s := range seeds {
		var idem, status string
		var noGPS bool
		var photos int
		var seq, taskSeq int64
		var taskID, legacy uuid.UUID
		if err := admin.QueryRow(ctx, `SELECT c.idem_key, c.legacy_task_status, c.location IS NULL, cardinality(c.photo_refs), c.sync_seq,
		    t.sync_seq, c.legacy_task_id, s.task_id
		  FROM pf_coop.support_request c JOIN pf_extension.support_request s ON s.tenant_id = c.tenant_id AND s.id = c.id
		  JOIN pf_extension.task_current t ON t.tenant_id = s.tenant_id AND t.task_id = s.task_id
		  WHERE c.tenant_id = $1 AND c.id = $2`, s.tid, s.id).Scan(&idem, &status, &noGPS, &photos, &seq, &taskSeq, &legacy, &taskID); err != nil {
			t.Fatalf("dòng %s: %v", s.id, err)
		}
		if idem != "k-"+s.id.String() || status != s.status || noGPS == s.gps || (s.gps && photos != 2) || seq != taskSeq || legacy != taskID {
			t.Errorf("dòng %s: idem %s trạng thái %s thiếuGPS %v ảnh %d seq %d/%d", s.id, idem, status, noGPS, photos, seq, taskSeq)
		}
	}
	var next int64
	if err := admin.QueryRow(ctx, `SELECT nextval('pf_coop.support_request_sync_seq')`).Scan(&next); err != nil || next <= taskSeqMax {
		t.Fatalf("chuỗi số mới %d phải lớn hơn sync_seq nhiệm vụ cũ %d (%v)", next, taskSeqMax, err)
	}
	var oldRows int
	var knInsert, knSelect, coopInsert bool
	if err := admin.QueryRow(ctx, `SELECT (SELECT count(*) FROM pf_extension.support_request),
	    has_table_privilege('r_extension', 'pf_extension.support_request', 'INSERT'),
	    has_table_privilege('r_extension', 'pf_extension.support_request', 'SELECT'),
	    has_table_privilege('r_cooperative', 'pf_coop.support_request', 'INSERT')`).Scan(&oldRows, &knInsert, &knSelect, &coopInsert); err != nil {
		t.Fatal(err)
	}
	if oldRows != 3 || knInsert || !knSelect || !coopInsert {
		t.Fatalf("bảng cũ %d dòng, r_extension INSERT %v SELECT %v, r_cooperative INSERT %v", oldRows, knInsert, knSelect, coopInsert)
	}
}
