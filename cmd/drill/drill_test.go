package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/objstore"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/objsync"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/opsjobs"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

func TestMain(m *testing.M) { os.Exit(pgtest.Run(m)) }

// fixture là dữ liệu tối thiểu có đủ mọi đối tượng mà diễn tập kiểm.
type fixture struct {
	tenant    uuid.UUID
	rollIDs   []uuid.UUID
	paramID   uuid.UUID
	resultIDs []uuid.UUID
}

func seed(t *testing.T) fixture {
	t.Helper()
	env := pgtest.Get(t)
	f := fixture{tenant: ids.New()}

	// mkg: sổ mẫu roll_count, một stream 3 dòng.
	fo := env.Pool(t, migrations.MKG, "fieldops_app")
	stream := "lot:" + ids.New().String()
	for i := 0; i < 3; i++ {
		id := ids.New()
		exec(t, fo, f.tenant, `INSERT INTO mkg_fieldops.roll_count
		  (id, tenant_id, stream_key, plot_ref, htx_ref, rolls, event_time, actor_ref, idem_key, rule_version)
		  VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, 'roll_count/v1')`,
			id, f.tenant, stream, ids.New(), ids.New(), 10+i, time.Now().UTC().Add(-time.Hour), ids.New(), ids.New().String())
		f.rollIDs = append(f.rollIDs, id)
	}

	// pf: một phiên bản tham số đã duyệt (ngưỡng QT-02).
	md := env.Pool(t, migrations.PF, "masterdata_app")
	f.paramID = ids.New()
	exec(t, md, f.tenant, `INSERT INTO pf_masterdata.param_approved
	  (id, tenant_id, stream_key, param_key, version, param_version_id, value_num, unit, effective_from, source_doc,
	   setter_ref, event_time, actor_ref, idem_key, rule_version)
	  VALUES ($1, $2, 'param:cgh.coverage.sufficient_min_pct', 'cgh.coverage.sufficient_min_pct', 1, $3, 80, '%',
	          '2026-01-01T00:00:00Z', 'QĐ tổng hợp 01/2026', $4, now(), $5, $6, 'param/v1')`,
		f.paramID, f.tenant, ids.New(), ids.New(), ids.New(), "drill-"+ids.New().String())

	// pf: một lần cân đối đã lưu với 3 dòng — thiếu, đủ, chưa có diện tích.
	mech := env.Pool(t, migrations.PF, "mechanization_app")
	th := `{"sufficient":{"key":"cgh.coverage.sufficient_min_pct","value":"80","version":1,"source_doc":"x"},
	        "attention":{"key":"cgh.coverage.attention_min_pct","value":"60","version":1,"source_doc":"x"},
	        "surplus":{"key":"cgh.coverage.surplus_min_pct","value":"120","version":1,"source_doc":"x"}}`
	runID, actor, idem := ids.New(), ids.New(), "drill-run-"+ids.New().String()
	exec(t, mech, f.tenant, `INSERT INTO pf_mechanization.balance_run
	  (id, tenant_id, stream_key, season_code, season_year, reference_date, pilot_stages, thresholds, result_count, status_counts,
	   missing_norms, event_time, actor_ref, idem_key, rule_version)
	  VALUES ($1, $2, 'balance:DX-2026', 'DX', 2026, '2025-11-01', '{lam_dat}', $3::jsonb, 3,
	          '{"shortage":1,"sufficient":1,"no_data":1,"attention":0,"surplus":0,"no_norm":0,"unclassified":0}'::jsonb,
	          '{}', now(), $4, $5, 'cgh.balance/v1')`, runID, f.tenant, th, actor, idem)
	type row struct {
		area, norm, req, pct *string
		capacity, whole, gap *int
		status               string
	}
	s := func(v string) *string { return &v }
	n := func(v int) *int { return &v }
	for i, r := range []row{
		{area: s("10"), norm: s("5"), req: s("2"), pct: s("50"), capacity: n(1), whole: n(2), gap: n(-1), status: "shortage"},
		{area: s("10"), norm: s("5"), req: s("2"), pct: s("100"), capacity: n(2), whole: n(2), gap: n(0), status: "sufficient"},
		{capacity: n(0), norm: s("5"), status: "no_data"},
	} {
		id := ids.New()
		exec(t, mech, f.tenant, `INSERT INTO pf_mechanization.balance_result
		  (id, tenant_id, stream_key, run_id, season_code, season_year, reference_date, cooperative_ref, farm_stage, machine_type,
		   area_ha, norm_key, norm_value, norm_version, required, required_whole, machines_total, capacity, coverage_pct, gap, status,
		   event_time, actor_ref, idem_key, rule_version)
		  VALUES ($1, $2, $3, $4, 'DX', 2026, '2025-11-01', $5, 'lam_dat', 'MAY_CAY', $6::numeric, 'cgh.norm:MAY_CAY:lam_dat',
		          $7::numeric, 1, $8::numeric, $9, $10, $10, $11::numeric, $12, $13, now(), $14, $15, 'cgh.balance/v1')`,
			id, f.tenant, "result:"+runID.String(), runID, ids.New(), r.area, r.norm, r.req, r.whole, *r.capacity, r.pct, r.gap, r.status,
			actor, idem+":"+string(rune('a'+i)))
		f.resultIDs = append(f.resultIDs, id)
	}
	return f
}

func exec(t *testing.T, p tenant.Beginner, tid uuid.UUID, sql string, args ...any) {
	t.Helper()
	if err := tenant.WithTenantTx(context.Background(), p, tid, func(tx pgx.Tx) error {
		_, err := tx.Exec(context.Background(), sql, args...)
		return err
	}); err != nil {
		t.Fatal(err)
	}
}

// tamper sửa dữ liệu VƯỢT trigger (kẻ có quyền cao / hỏng đĩa) trên cụm c.
func tamper(t *testing.T, c migrations.Cluster, sql string, args ...any) {
	t.Helper()
	ctx := context.Background()
	conn, err := pgtest.Get(t).AdminPool(t, c).Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Release()
	for _, q := range []string{"SET session_replication_role = replica", sql, "RESET session_replication_role"} {
		a := args
		if q != sql {
			a = nil
		}
		if _, err := conn.Exec(ctx, q, a...); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
}

func mustStatus(t *testing.T, rep Report, name, cluster, want string) CheckResult {
	t.Helper()
	c, ok := rep.Find(name, cluster)
	if !ok {
		t.Fatalf("báo cáo thiếu kiểm %s/%s: %+v", name, cluster, rep.Checks)
	}
	if c.Status != want {
		t.Fatalf("%s/%s: trạng thái %s, muốn %s — %s %v", name, cluster, c.Status, want, c.Detail, c.Issues)
	}
	return c
}

// Diễn tập đầy đủ trên pgtest: dữ liệu → neo ký số → sao lưu kho đối tượng →
// chụp manifest → "phục hồi" kho đối tượng vào kho sạch → kiểm (mọi kiểm đạt,
// -strict) → rồi làm hỏng từng thứ một để chứng minh mỗi kiểm phát hiện được.
func TestDrillEndToEndAndDetectsCorruption(t *testing.T) {
	ctx := context.Background()
	env := pgtest.Get(t)
	f := seed(t)

	primaryDir, restoredDir := t.TempDir(), t.TempDir()
	primary, err := objstore.NewLocal(primaryDir, objstore.LocalOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range map[string]string{"evidence/hh1/anh1.enc": "bản mã ảnh 1", "evidence/hh1/anh2.enc": "bản mã ảnh 2"} {
		b := []byte(v)
		if _, err := primary.Put(ctx, k, bytes.NewReader(b), int64(len(b)), objstore.SHA256Hex(b), ""); err != nil {
			t.Fatal(err)
		}
	}
	seedKey := make([]byte, ed25519.SeedSize)
	seedKey[0] = 7
	signer, err := opsjobs.NewSigner(seedKey, "")
	if err != nil {
		t.Fatal(err)
	}
	day := clock.Date{Year: 2026, Month: 9, Day: 27}
	for _, c := range migrations.Clusters {
		if _, err := opsjobs.AnchorDaily(ctx, env.Pool(t, c, dbadmin.OutboxPublisher), primary, signer, string(c), day); err != nil {
			t.Fatal(err)
		}
	}

	backup, err := objstore.NewLocal(t.TempDir(), objstore.LocalOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := objsync.Backup(ctx, primary, backup, objsync.Options{Label: "drill-test"}); err != nil {
		t.Fatal(err)
	}
	cfg := Config{AdminURL: env.AdminURL, Suffix: env.Suffix, AnchorPub: signer.Public()}
	want, err := Capture(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if want.Clusters["pf"].ParamSetHash == "" || want.Clusters["pf"].LatestAnchorDate != "2026-09-27" || want.Clusters["mkg"].Chains == 0 {
		t.Fatalf("manifest chụp thiếu: %+v", want.Clusters)
	}
	// Manifest đi qua JSON như ngoài đời.
	raw, _ := json.Marshal(want)
	want = DBManifest{}
	if err := json.Unmarshal(raw, &want); err != nil {
		t.Fatal(err)
	}

	restored, err := objstore.NewLocal(restoredDir, objstore.LocalOptions{})
	if err != nil {
		t.Fatal(err)
	}
	om, _, err := objsync.ReadManifest(ctx, backup, "latest")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := objsync.Restore(ctx, backup, restored, om, ""); err != nil {
		t.Fatal(err)
	}
	cfg.Store, cfg.Objects = restored, &om

	check := func() Report {
		t.Helper()
		rep, err := Check(ctx, cfg, want, true)
		if err != nil {
			t.Fatal(err)
		}
		return rep
	}

	rep := check()
	if !rep.OK || rep.Fail != 0 || rep.Skip != 0 {
		b, _ := json.MarshalIndent(rep, "", "  ")
		t.Fatalf("môi trường vừa phục hồi phải đạt mọi kiểm:\n%s", b)
	}
	mustStatus(t, rep, CheckBalance, "pf", StatusPass)
	mustStatus(t, rep, CheckParamHash, "pf", StatusPass)
	if c := mustStatus(t, rep, CheckAnchors, "mkg", StatusPass); !strings.Contains(c.Detail, "chữ ký hợp lệ") {
		t.Fatalf("neo mkg: %s", c.Detail)
	}
	mustStatus(t, rep, CheckObjects, "", StatusPass)

	// 1. Một đối tượng hỏng trong kho đã phục hồi.
	evPath := filepath.Join(restoredDir, "objects", "evidence", "hh1", "anh1.enc")
	orig, err := os.ReadFile(evPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(evPath, []byte("hỏng"), 0o600); err != nil {
		t.Fatal(err)
	}
	rep = check()
	mustStatus(t, rep, CheckObjects, "", StatusFail)
	if rep.OK {
		t.Fatal("báo cáo phải không đạt")
	}
	if err := os.WriteFile(evPath, orig, 0o600); err != nil {
		t.Fatal(err)
	}

	// 2. Manifest neo bị thay bằng bản ký bởi khóa khác.
	anPath := filepath.Join(restoredDir, "objects", "anchors", "pf", "2026-09-27.json")
	origAnchor, err := os.ReadFile(anPath)
	if err != nil {
		t.Fatal(err)
	}
	other, _ := opsjobs.NewSigner(make([]byte, ed25519.SeedSize), "")
	m, err := opsjobs.VerifyManifest(origAnchor, signer.Public())
	if err != nil {
		t.Fatal(err)
	}
	forged, err := opsjobs.SignManifest(m, other)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(anPath, forged, 0o600); err != nil {
		t.Fatal(err)
	}
	rep = check()
	if c := mustStatus(t, rep, CheckAnchors, "pf", StatusFail); !strings.Contains(c.Detail, "chữ ký") {
		t.Fatalf("phải báo chữ ký: %s", c.Detail)
	}
	mustStatus(t, rep, CheckAnchors, "mkg", StatusPass)
	if err := os.WriteFile(anPath, origAnchor, 0o600); err != nil {
		t.Fatal(err)
	}
	if rep = check(); !rep.OK {
		t.Fatalf("hoàn nguyên xong phải đạt lại: %+v", rep.Checks)
	}

	// 3. Sửa giá trị tham số đã duyệt vượt trigger → băm bộ tham số và chuỗi pf hỏng.
	tamper(t, migrations.PF, "UPDATE pf_masterdata.param_approved SET value_num = 90 WHERE id = $1", f.paramID)
	rep = check()
	mustStatus(t, rep, CheckParamHash, "pf", StatusFail)
	mustStatus(t, rep, CheckChains, "pf", StatusFail)
	mustStatus(t, rep, CheckChains, "mkg", StatusPass)

	// 4. Sửa kết quả cân đối đã lưu → tính lại không khớp.
	tamper(t, migrations.PF, "UPDATE pf_mechanization.balance_result SET status = 'surplus' WHERE id = $1", f.resultIDs[0])
	rep = check()
	if c := mustStatus(t, rep, CheckBalance, "pf", StatusFail); !strings.Contains(strings.Join(c.Issues, "\n"), "status surplus ≠ shortage") {
		t.Fatalf("phải chỉ ra dòng lệch: %v", c.Issues)
	}

	// 5. Mất dòng cuối của sổ mkg → lệch số dòng và neo/chuỗi mkg hỏng.
	tamper(t, migrations.MKG, "DELETE FROM mkg_fieldops.roll_count WHERE id = $1", f.rollIDs[2])
	rep = check()
	if c := mustStatus(t, rep, CheckRowCounts, "mkg", StatusFail); !strings.Contains(strings.Join(c.Issues, "\n"), "mkg_fieldops.roll_count") {
		t.Fatalf("phải nêu bảng lệch: %v", c.Issues)
	}
	mustStatus(t, rep, CheckChains, "mkg", StatusFail)
	mustStatus(t, rep, CheckAnchors, "mkg", StatusFail)
	mustStatus(t, rep, CheckRowCounts, "vault", StatusPass)

	// Bảng được khai báo bỏ qua thì không tính lệch số dòng.
	cfg.IgnoreTables = map[string]bool{"mkg_fieldops.roll_count": true}
	rep = check()
	mustStatus(t, rep, CheckRowCounts, "mkg", StatusPass)
}

// Thiếu kho/khóa → kiểm bị bỏ qua; -strict coi bỏ qua là không đạt.
func TestStrictTreatsSkipAsFailure(t *testing.T) {
	r := Report{Checks: []CheckResult{{Name: CheckRowCounts, Status: StatusPass}, {Name: CheckObjects, Status: StatusSkip}}}
	r.finish(false)
	if !r.OK || r.Skip != 1 {
		t.Fatalf("không strict: %+v", r)
	}
	r.finish(true)
	if r.OK {
		t.Fatal("strict: bỏ qua phải là không đạt")
	}
}
