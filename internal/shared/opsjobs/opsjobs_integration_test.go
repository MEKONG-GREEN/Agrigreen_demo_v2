package opsjobs_test

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/objstore"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/opsjobs"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

func TestMain(m *testing.M) { os.Exit(pgtest.Run(m)) }

// writeRolls ghi n dòng vào sổ mẫu mkg_fieldops.roll_count, trả id theo seq.
func writeRolls(t *testing.T, tid uuid.UUID, stream string, n int) []uuid.UUID {
	t.Helper()
	ctx := context.Background()
	app := pgtest.Get(t).Pool(t, migrations.MKG, "fieldops_app")
	var out []uuid.UUID
	for i := 0; i < n; i++ {
		id := ids.New()
		if err := tenant.WithTenantTx(ctx, app, tid, func(tx pgx.Tx) error {
			_, err := tx.Exec(ctx, `INSERT INTO mkg_fieldops.roll_count
			  (id, tenant_id, stream_key, plot_ref, htx_ref, rolls, event_time, actor_ref, idem_key, rule_version)
			  VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, 'roll_count/v1')`,
				id, tid, stream, ids.New(), ids.New(), 10+i, time.Now().UTC().Add(-time.Hour), ids.New(), ids.New().String())
			return err
		}); err != nil {
			t.Fatal(err)
		}
		out = append(out, id)
	}
	return out
}

func testSigner(t *testing.T) *opsjobs.Signer {
	seed := make([]byte, ed25519.SeedSize)
	for i := range seed {
		seed[i] = byte(i)
	}
	s, err := opsjobs.NewSigner(seed, "")
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// Mục 8.2: neo hằng ngày ghi shared.chain_anchor và xuất manifest ký số kiểm được.
func TestAnchorDailyWritesRowsAndSignedManifest(t *testing.T) {
	ctx := context.Background()
	env := pgtest.Get(t)
	pub := env.Pool(t, migrations.MKG, dbadmin.OpsJobs)
	owner := env.Pool(t, migrations.MKG, dbadmin.Migrator)
	tid, stream := ids.New(), "lot:"+ids.New().String()
	writeRolls(t, tid, stream, 3)

	store, err := objstore.NewLocal(t.TempDir(), objstore.LocalOptions{})
	if err != nil {
		t.Fatal(err)
	}
	signer := testSigner(t)
	day := clock.Date{Year: 2026, Month: 9, Day: 26}
	res, err := opsjobs.AnchorDaily(ctx, pub, store, signer, "mkg", day)
	if err != nil {
		t.Fatal(err)
	}
	if res.Skipped || res.Inserted < 1 || res.Key != "anchors/mkg/2026-09-26.json" {
		t.Fatalf("kết quả neo: %+v", res)
	}

	var headHash []byte
	if err := owner.QueryRow(ctx, "SELECT last_hash FROM shared.chain_head WHERE tenant_id = $1 AND stream_key = $2", tid, stream).Scan(&headHash); err != nil {
		t.Fatal(err)
	}
	var anchored int
	if err := owner.QueryRow(ctx, "SELECT count(*) FROM shared.chain_anchor WHERE business_date = '2026-09-26' AND tenant_id = $1 AND stream_key = $2 AND last_seq = 3", tid, stream).Scan(&anchored); err != nil || anchored != 1 {
		t.Fatalf("chain_anchor phải có 1 dòng seq 3: %d %v", anchored, err)
	}

	rc, _, err := store.Get(ctx, res.Key)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(rc)
	_ = rc.Close()
	m, err := opsjobs.VerifyManifest(body, signer.Public())
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range m.Entries {
		if e.TenantID == tid && e.StreamKey == stream {
			found = e.LastSeq == 3 && e.LastHash == hex.EncodeToString(headHash)
		}
	}
	if !found || m.Cluster != "mkg" || m.BusinessDate != "2026-09-26" || m.KeyID != signer.KeyID {
		t.Fatalf("manifest thiếu/sai đầu chuỗi: %+v", m)
	}

	// Sửa một byte băm trong tệp → chữ ký hỏng.
	tampered := strings.Replace(string(body), hex.EncodeToString(headHash), strings.Repeat("0", 64), 1)
	if _, err := opsjobs.VerifyManifest([]byte(tampered), signer.Public()); err == nil {
		t.Fatal("manifest bị sửa phải không qua kiểm chữ ký")
	}
	other, _ := opsjobs.NewSigner(make([]byte, 32), "")
	if _, err := opsjobs.VerifyManifest(body, other.Public()); err == nil {
		t.Fatal("khóa công khai khác phải không qua kiểm chữ ký")
	}

	// Chạy lại: manifest đã có → bỏ qua, không đụng kho.
	again, err := opsjobs.AnchorDaily(ctx, pub, store, signer, "mkg", day)
	if err != nil || !again.Skipped {
		t.Fatalf("chạy lại phải bỏ qua: %+v %v", again, err)
	}
}

// Vai ứng dụng không được tự neo hay đọc neo.
func TestAnchorFunctionsNotGrantedToAppRoles(t *testing.T) {
	ctx := context.Background()
	app := pgtest.Get(t).Pool(t, migrations.MKG, "fieldops_app")
	for _, q := range []string{"SELECT shared.anchor_chains('2026-01-01')", "SELECT count(*) FROM shared.chain_anchors_of('2026-01-01')", "SELECT shared.purge_expired_idempotency(1)"} {
		if _, err := app.Exec(ctx, q); err == nil || !strings.Contains(err.Error(), "42501") {
			t.Errorf("%s bằng vai ứng dụng phải bị từ chối (42501), nhận %v", q, err)
		}
	}
}

func TestAnchorDateFor(t *testing.T) {
	cases := map[string]string{
		"2026-09-26T17:30:00Z": "2026-09-26", // 00:30 ngày 27 VN → neo ngày 26
		"2026-09-26T17:10:00Z": "2026-09-25", // 00:10 ngày 27 VN → ngày 25 (đã neo)
		"2026-09-27T08:00:00Z": "2026-09-26", // 15:00 ngày 27 VN, khởi động lại → bắt kịp ngày 26
		"2026-12-31T17:30:00Z": "2026-12-31",
	}
	for in, want := range cases {
		now, _ := time.Parse(time.RFC3339, in)
		if got := opsjobs.AnchorDateFor(now).String(); got != want {
			t.Errorf("AnchorDateFor(%s) = %s, mong %s", in, got, want)
		}
	}
}

func TestPurgeIdempotency(t *testing.T) {
	ctx := context.Background()
	env := pgtest.Get(t)
	api := env.Pool(t, migrations.PF, "platform_api")
	pub := env.Pool(t, migrations.PF, dbadmin.OpsJobs)
	tid := ids.New()
	if err := tenant.WithTenantTx(ctx, api, tid, func(tx pgx.Tx) error {
		for i, exp := range []string{"now() - interval '1 hour'", "now() - interval '1 minute'", "now() + interval '1 hour'"} {
			if _, err := tx.Exec(ctx, `INSERT INTO shared.idempotency_key (tenant_id, idem_key, actor_ref, method, route, request_hash, expires_at)
			  VALUES ($1, $2, $3, 'POST', '/v1/x', '\x00', `+exp+`)`, tid, "key-000"+string(rune('a'+i)), ids.New()); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	n, err := opsjobs.PurgeIdempotency(ctx, pub, 1)
	if err != nil || n < 2 {
		t.Fatalf("phải xóa ít nhất 2 khóa hết hạn: %d %v", n, err)
	}
	var left int
	if err := tenant.WithTenantTx(ctx, api, tid, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, "SELECT count(*) FROM shared.idempotency_key WHERE tenant_id = $1", tid).Scan(&left)
	}); err != nil || left != 1 {
		t.Fatalf("còn lại phải là 1 khóa chưa hết hạn: %d %v", left, err)
	}
}

// W2.1 (DB-11): dọn shared.ledger_idem ở MỌI cụm (shared/000006 áp cho mọi database),
// bằng vai job ag_ops_jobs (chỉ có EXECUTE — shared 000009), chỉ xóa dòng quá hạn.
func TestPurgeLedgerIdemEveryCluster(t *testing.T) {
	ctx := context.Background()
	env := pgtest.Get(t)
	for _, c := range migrations.Clusters {
		t.Run(string(c), func(t *testing.T) {
			owner := env.Pool(t, c, dbadmin.Migrator)
			pub := env.Pool(t, c, dbadmin.OpsJobs)
			ledgerName := "test.purge_" + ids.New().String()
			tid := ids.New()
			for i, exp := range []string{"now() - interval '1 day'", "now() - interval '1 second'", "now() + interval '1 hour'", "DEFAULT"} {
				if _, err := owner.Exec(ctx, `INSERT INTO shared.ledger_idem (ledger, tenant_id, idem_key, row_id, expires_at)
				  VALUES ($1, $2, $3, $4, `+exp+`)`, ledgerName, tid, "k"+string(rune('a'+i)), ids.New()); err != nil {
					t.Fatal(err)
				}
			}
			// Vai job không xóa thẳng được bảng (chỉ qua hàm SECURITY DEFINER).
			if _, err := pub.Exec(ctx, "DELETE FROM shared.ledger_idem WHERE ledger = $1", ledgerName); err == nil {
				t.Fatal("ag_ops_jobs không được có DELETE trực tiếp trên shared.ledger_idem")
			}
			r, err := opsjobs.PurgeAllIdempotency(ctx, pub, 1)
			if err != nil || r.LedgerIdem < 2 {
				t.Fatalf("phải xóa ít nhất 2 dòng quá hạn: %+v %v", r, err)
			}
			var keys []string
			rows, err := owner.Query(ctx, "SELECT idem_key FROM shared.ledger_idem WHERE ledger = $1 ORDER BY 1", ledgerName)
			if err != nil {
				t.Fatal(err)
			}
			for rows.Next() {
				var k string
				if err := rows.Scan(&k); err != nil {
					t.Fatal(err)
				}
				keys = append(keys, k)
			}
			rows.Close()
			if strings.Join(keys, ",") != "kc,kd" {
				t.Fatalf("chỉ được xóa dòng quá hạn, còn %v", keys)
			}
		})
	}
}

// W2.1: sau khi dòng đăng ký quá hạn bị dọn, ghi lại cùng idem_key được nhận như mới.
func TestLedgerReplayAcceptedAfterPurge(t *testing.T) {
	ctx := context.Background()
	env := pgtest.Get(t)
	owner := env.Pool(t, migrations.MKG, dbadmin.Migrator)
	pub := env.Pool(t, migrations.MKG, dbadmin.OpsJobs)
	app := env.Pool(t, migrations.MKG, "fieldops_app")
	tid := ids.New()
	idem := ids.New().String()
	write := func() error {
		return tenant.WithTenantTx(ctx, app, tid, func(tx pgx.Tx) error {
			_, err := tx.Exec(ctx, `INSERT INTO mkg_fieldops.roll_count
			  (id, tenant_id, stream_key, plot_ref, htx_ref, rolls, event_time, actor_ref, idem_key, rule_version)
			  VALUES ($1, $2, 'lot:replay', $3, $4, 1, $5, $6, $7, 'roll_count/v1')`,
				ids.New(), tid, ids.New(), ids.New(), time.Now().UTC().Add(-time.Hour), ids.New(), idem)
			return err
		})
	}
	if err := write(); err != nil {
		t.Fatal(err)
	}
	if err := write(); err == nil {
		t.Fatal("trong hạn phải bị chặn trùng")
	}
	if _, err := owner.Exec(ctx, "UPDATE shared.ledger_idem SET expires_at = now() - interval '1 second' WHERE tenant_id = $1 AND idem_key = $2", tid, idem); err != nil {
		t.Fatal(err)
	}
	if n, err := opsjobs.PurgeLedgerIdem(ctx, pub, 5000); err != nil || n < 1 {
		t.Fatalf("phải dọn dòng quá hạn: %d %v", n, err)
	}
	var left int
	if err := owner.QueryRow(ctx, "SELECT count(*) FROM shared.ledger_idem WHERE tenant_id = $1 AND idem_key = $2", tid, idem).Scan(&left); err != nil || left != 0 {
		t.Fatalf("dòng đăng ký phải bị xóa: %d %v", left, err)
	}
	if err := write(); err != nil {
		t.Fatalf("sau khi dọn, cùng idem_key phải được nhận như mới: %v", err)
	}
}

// migrate maintain chạy lặp an toàn và tạo đúng phần thiếu.
func TestEnsurePartitionsIdempotent(t *testing.T) {
	ctx := context.Background()
	owner := pgtest.Get(t).Pool(t, migrations.MKG, dbadmin.Migrator)
	tables, err := opsjobs.LedgerTables(ctx, owner)
	if err != nil || len(tables) == 0 {
		t.Fatalf("phải tìm thấy bảng sổ: %v %v", tables, err)
	}
	if tables[0].Ledger == "" {
		t.Fatalf("phải đọc được tên sổ từ trg_chain: %+v", tables[0])
	}
	sum := func(rs []opsjobs.PartitionReport) int {
		n := 0
		for _, r := range rs {
			n += r.Created
		}
		return n
	}
	r1, err := opsjobs.EnsurePartitions(ctx, owner, 24)
	if err != nil {
		t.Fatal(err)
	}
	if sum(r1) != 0 {
		t.Fatalf("apply_ledger đã tạo 24 tháng; lần đầu phải tạo 0, tạo %d", sum(r1))
	}
	r2, err := opsjobs.EnsurePartitions(ctx, owner, 27)
	if err != nil {
		t.Fatal(err)
	}
	if sum(r2) != 3*len(tables) {
		t.Fatalf("mở rộng 27 tháng phải tạo 3 phân vùng mỗi bảng, tạo %d", sum(r2))
	}
	r3, err := opsjobs.EnsurePartitions(ctx, owner, 27)
	if err != nil || sum(r3) != 0 {
		t.Fatalf("chạy lặp phải tạo 0: %d %v", sum(r3), err)
	}
	checkGates(t, owner)
}

func checkGates(t *testing.T, owner *pgxpool.Pool) {
	t.Helper()
	for _, view := range []string{"shared.v_missing_immutability", "shared.v_missing_rls"} {
		var n int
		if err := owner.QueryRow(context.Background(), "SELECT count(*) FROM "+view).Scan(&n); err != nil || n != 0 {
			t.Fatalf("%s phải rỗng sau khi tạo phân vùng: %d %v", view, n, err)
		}
	}
}

// migrate verify-chains phát hiện dòng bị sửa vượt trigger và dòng cuối bị xóa.
func TestVerifyAllChainsDetectsTampering(t *testing.T) {
	ctx := context.Background()
	env := pgtest.Get(t)
	owner := env.Pool(t, migrations.MKG, dbadmin.Migrator)
	good := ids.New()
	goodStream := "lot:" + ids.New().String()
	writeRolls(t, good, goodStream, 2)
	bad := ids.New()
	badStream := "lot:" + ids.New().String()
	badIDs := writeRolls(t, bad, badStream, 4)
	cut := ids.New()
	cutStream := "lot:" + ids.New().String()
	cutIDs := writeRolls(t, cut, cutStream, 3)

	find := func(cs []opsjobs.ChainCheck, tid uuid.UUID) opsjobs.ChainCheck {
		for _, c := range cs {
			if c.TenantID == tid {
				return c
			}
		}
		t.Fatalf("không có kết quả cho tenant %s", tid)
		return opsjobs.ChainCheck{}
	}
	cs, err := opsjobs.VerifyAllChains(ctx, owner)
	if err != nil {
		t.Fatal(err)
	}
	for _, tid := range []uuid.UUID{good, bad, cut} {
		if c := find(cs, tid); !c.OK || c.Table != "mkg_fieldops.roll_count" {
			t.Fatalf("trước khi sửa mọi chuỗi phải hợp lệ: %+v", c)
		}
	}

	admin := env.AdminPool(t, migrations.MKG)
	conn, err := admin.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Release()
	for _, q := range []struct {
		sql string
		id  uuid.UUID
	}{
		{"UPDATE mkg_fieldops.roll_count SET rolls = 999 WHERE id = $1", badIDs[1]},
		{"DELETE FROM mkg_fieldops.roll_count WHERE id = $1", cutIDs[2]},
	} {
		if _, err := conn.Exec(ctx, "SET session_replication_role = replica"); err != nil {
			t.Fatal(err)
		}
		if _, err := conn.Exec(ctx, q.sql, q.id); err != nil {
			t.Fatal(err)
		}
		if _, err := conn.Exec(ctx, "RESET session_replication_role"); err != nil {
			t.Fatal(err)
		}
	}

	cs, err = opsjobs.VerifyAllChains(ctx, owner)
	if err != nil {
		t.Fatal(err)
	}
	if c := find(cs, good); !c.OK {
		t.Fatalf("chuỗi không bị đụng phải còn hợp lệ: %+v", c)
	}
	if c := find(cs, bad); c.OK || c.BrokenAtSeq == nil || *c.BrokenAtSeq != 2 {
		t.Fatalf("phải báo đứt tại seq 2: %+v", c)
	}
	if c := find(cs, cut); c.OK || c.BrokenAtSeq == nil || *c.BrokenAtSeq != 3 {
		t.Fatalf("xóa dòng cuối phải bị phát hiện tại seq 3: %+v", c)
	}
}

// defaultLedger dựng một bảng sổ thử (mẫu §15) trong cụm pf rồi bỏ phân vùng tháng hiện tại, để dòng ghi
// mới rơi vào DEFAULT — tình huống job maintain trễ. Trả tên bảng và tên phân vùng tháng hiện tại.
func defaultLedger(t *testing.T, owner *pgxpool.Pool, name string) (table, part string) {
	t.Helper()
	ctx := context.Background()
	table = "pf_zm6." + name
	part = table + "_" + time.Now().UTC().Format("200601")
	for _, q := range []string{
		"CREATE SCHEMA IF NOT EXISTS pf_zm6",
		`CREATE TABLE ` + table + ` (
		  id uuid NOT NULL, tenant_id uuid NOT NULL, stream_key text NOT NULL, seq bigint NOT NULL,
		  note text NOT NULL CHECK (length(note) > 0), idem_key text NOT NULL,
		  recorded_at timestamptz NOT NULL DEFAULT now(), prev_hash bytea NOT NULL, row_hash bytea NOT NULL,
		  PRIMARY KEY (id, recorded_at)) PARTITION BY RANGE (recorded_at)`,
		"CREATE INDEX ON " + table + " (tenant_id, stream_key, seq)",
		"SELECT shared.apply_ledger('" + table + "', '" + table + "')",
		"ALTER TABLE " + table + " DETACH PARTITION " + part,
		"DROP TABLE " + part,
	} {
		if _, err := owner.Exec(ctx, q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	t.Cleanup(func() { _, _ = owner.Exec(context.Background(), "DROP TABLE IF EXISTS "+table) })
	return table, part
}

func writeEntries(t *testing.T, owner *pgxpool.Pool, table string, tid uuid.UUID, stream string, n int) {
	t.Helper()
	ctx := context.Background()
	for i := 0; i < n; i++ {
		if err := tenant.WithTenantTx(ctx, owner, tid, func(tx pgx.Tx) error {
			_, err := tx.Exec(ctx, "INSERT INTO "+table+" (id, tenant_id, stream_key, note, idem_key) VALUES ($1, $2, $3, 'ghi', $4)",
				ids.New(), tid, stream, ids.New().String())
			return err
		}); err != nil {
			t.Fatal(err)
		}
	}
}

func countIn(t *testing.T, owner *pgxpool.Pool, rel string, tid uuid.UUID) int {
	t.Helper()
	var n int
	if err := tenant.WithTenantTx(context.Background(), owner, tid, func(tx pgx.Tx) error {
		return tx.QueryRow(context.Background(), "SELECT count(*) FROM "+rel).Scan(&n)
	}); err != nil {
		t.Fatal(err)
	}
	return n
}

// waitDefault chờ chỉ số DEFAULT (thống kê n_live_tup — mỗi tiến trình xả số đếm khi rảnh, tới ~10 giây)
// bằng want; trả giá trị cuối.
func waitDefault(t *testing.T, pool *pgxpool.Pool, table string, want int64) int64 {
	t.Helper()
	var last int64 = -1
	for deadline := time.Now().Add(25 * time.Second); time.Now().Before(deadline); time.Sleep(250 * time.Millisecond) {
		rs, err := opsjobs.DefaultPartitionRows(context.Background(), pool)
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, r := range rs {
			if r.Table == table {
				last, found = r.Rows, true
			}
		}
		if !found {
			t.Fatalf("shared.ledger_default_rows() thiếu %s: %v", table, rs)
		}
		if last == want {
			break
		}
	}
	return last
}

// M6: dòng rơi vào DEFAULT không còn chặn tạo phân vùng tháng mãi mãi — create_monthly_partitions chuyển
// dòng sang phân vùng mới cùng giao dịch (chuỗi băm giữ nguyên, trigger + RLS đủ), chỉ số giám sát thấy
// dòng trong DEFAULT trước và về 0 sau.
func TestCreatePartitionMovesDefaultRows_M6(t *testing.T) {
	ctx := context.Background()
	env := pgtest.Get(t)
	owner := env.Pool(t, migrations.PF, dbadmin.Migrator)
	ops := env.Pool(t, migrations.PF, dbadmin.OpsJobs)
	table, part := defaultLedger(t, owner, "direct")
	tid, stream := ids.New(), "s:"+ids.New().String()
	writeEntries(t, owner, table, tid, stream, 3)
	if n := countIn(t, owner, table+"_default", tid); n != 3 {
		t.Fatalf("thiếu phân vùng tháng: 3 dòng phải nằm trong DEFAULT, có %d", n)
	}
	if est := waitDefault(t, ops, table, 3); est != 3 {
		t.Fatalf("chỉ số DEFAULT (vai ag_ops_jobs) phải thấy 3 dòng, thấy %d", est)
	}

	// Trước shared 000010 lệnh này lỗi check_violation mãi mãi.
	if _, err := owner.Exec(ctx, "SELECT shared.create_monthly_partitions($1::regclass, (now() AT TIME ZONE 'UTC')::date, 2)", table); err != nil {
		t.Fatalf("tạo phân vùng khi DEFAULT có dòng: %v", err)
	}
	var attached bool
	if err := owner.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM pg_inherits WHERE inhrelid = to_regclass($1) AND inhparent = $2::regclass)", part, table).Scan(&attached); err != nil || !attached {
		t.Fatalf("phân vùng %s phải được gắn: %v %v", part, attached, err)
	}
	if d, p, all := countIn(t, owner, table+"_default", tid), countIn(t, owner, part, tid), countIn(t, owner, table, tid); d != 0 || p != 3 || all != 3 {
		t.Fatalf("sau khi chuyển: DEFAULT %d, tháng %d, tổng %d (muốn 0, 3, 3)", d, p, all)
	}
	if est := waitDefault(t, ops, table, 0); est != 0 {
		t.Fatalf("chỉ số DEFAULT phải về 0, còn %d", est)
	}
	// Ghi tiếp nối chuỗi; chuỗi băm hợp lệ trên cả dòng đã chuyển.
	writeEntries(t, owner, table, tid, stream, 1)
	var ok bool
	var checked int64
	if err := tenant.WithTenantTx(ctx, owner, tid, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, "SELECT ok, rows_checked FROM shared.verify_chain($1::regclass, $2, $3)", table, tid, stream).Scan(&ok, &checked)
	}); err != nil || !ok || checked != 4 {
		t.Fatalf("chuỗi băm sau khi chuyển: ok=%v rows=%d %v", ok, checked, err)
	}
	// Bất biến còn nguyên: UPDATE dòng đã chuyển bị chặn; DEFAULT và phân vùng mới FORCE RLS.
	if err := tenant.WithTenantTx(ctx, owner, tid, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, "UPDATE "+part+" SET note = 'sửa'")
		return err
	}); err == nil || !strings.Contains(err.Error(), "append-only") {
		t.Fatalf("UPDATE dòng đã chuyển phải bị trg_immutable chặn: %v", err)
	}
	for _, rel := range []string{table + "_default", part} {
		var force bool
		if err := owner.QueryRow(ctx, "SELECT relforcerowsecurity FROM pg_class WHERE oid = $1::regclass", rel).Scan(&force); err != nil || !force {
			t.Fatalf("%s phải FORCE RLS: %v %v", rel, force, err)
		}
	}
	checkGates(t, owner)
}

// M6: migrate maintain (EnsurePartitions) thấy DEFAULT có dòng qua thống kê, chuyển trước rồi tạo phân vùng.
func TestEnsurePartitionsDrainsDefault_M6(t *testing.T) {
	ctx := context.Background()
	owner := pgtest.Get(t).Pool(t, migrations.PF, dbadmin.Migrator)
	table, part := defaultLedger(t, owner, "maintain")
	tid := ids.New()
	writeEntries(t, owner, table, tid, "s:"+ids.New().String(), 2)
	if est := waitDefault(t, owner, table, 2); est != 2 {
		t.Fatalf("thống kê DEFAULT phải thấy 2 dòng, thấy %d", est)
	}
	rs, err := opsjobs.EnsurePartitions(ctx, owner, 24)
	if err != nil {
		t.Fatal(err)
	}
	var got *opsjobs.PartitionReport
	for i := range rs {
		if rs[i].Table == table {
			got = &rs[i]
		}
	}
	if got == nil || got.Moved != 2 || got.DefaultRows != 0 || got.Created != 0 {
		t.Fatalf("báo cáo maintain: %+v", got)
	}
	if n := countIn(t, owner, part, tid); n != 2 {
		t.Fatalf("2 dòng phải sang %s, có %d", part, n)
	}
	checkGates(t, owner)
}

// M8: ag_outbox_publisher chỉ còn shared.outbox; job vận hành dùng ag_ops_jobs, chỉ đọc cột được cấp.
func TestOpsJobsRoleSplit_M8(t *testing.T) {
	ctx := context.Background()
	env := pgtest.Get(t)
	denied := func(t *testing.T, p *pgxpool.Pool, q string) {
		t.Helper()
		if _, err := p.Exec(ctx, q); err == nil || !strings.Contains(err.Error(), "42501") {
			t.Errorf("%s phải bị từ chối (42501), nhận %v", q, err)
		}
	}
	allowed := func(t *testing.T, p *pgxpool.Pool, q string) {
		t.Helper()
		if _, err := p.Exec(ctx, q); err != nil {
			t.Errorf("%s: %v", q, err)
		}
	}
	for _, c := range migrations.Clusters {
		t.Run(string(c), func(t *testing.T) {
			pub := env.Pool(t, c, dbadmin.OutboxPublisher)
			ops := env.Pool(t, c, dbadmin.OpsJobs)
			for _, q := range []string{
				"SELECT shared.anchor_chains('2026-01-01')",
				"SELECT count(*) FROM shared.chain_anchors_of('2026-01-01')",
				"SELECT shared.purge_expired_idempotency(1)",
				"SELECT shared.purge_expired_ledger_idem(1)",
				"SELECT tenant_id FROM shared.idempotency_key LIMIT 1",
			} {
				denied(t, pub, q)
			}
			allowed(t, pub, "SELECT id, payload FROM shared.outbox LIMIT 1")
			allowed(t, ops, "SELECT count(*) FROM shared.chain_anchors_of('2026-01-01')")
			allowed(t, ops, "SELECT shared.purge_expired_idempotency(1)")
			allowed(t, ops, "SELECT shared.purge_expired_ledger_idem(1)")
			allowed(t, ops, "SELECT DISTINCT tenant_id FROM shared.outbox")
			allowed(t, ops, "SELECT count(*) FROM shared.ledger_default_rows()")
			denied(t, ops, "SELECT payload FROM shared.outbox LIMIT 1")
			denied(t, ops, "UPDATE shared.outbox SET attempts = attempts WHERE false")
		})
	}
	pfPub, pfOps := env.Pool(t, migrations.PF, dbadmin.OutboxPublisher), env.Pool(t, migrations.PF, dbadmin.OpsJobs)
	for _, q := range []string{
		"SELECT tenant_id, status, escalated_at, due_at FROM pf_extension.task_current LIMIT 1",
		"SELECT tenant_id, status FROM pf_extension.report_period LIMIT 1",
		"SELECT tenant_id, week_start, locked_at FROM pf_extension.work_schedule_current LIMIT 1",
		"SELECT tenant_id, active FROM pf_extension.staff LIMIT 1",
		"SELECT tenant_id, sent_at FROM pf_notify.email_digest_item LIMIT 1",
		"SELECT tenant_id, active FROM pf_reports.schedule LIMIT 1",
		"SELECT tenant_id, source FROM pf_mechanization.area_sync_log LIMIT 1",
		"SELECT tenant_id, status FROM pf_sync.conflict LIMIT 1",
		"SELECT tenant_id, review_status FROM pf_mrv.evidence_current LIMIT 1",
	} {
		allowed(t, pfOps, q)
		denied(t, pfPub, q)
	}
	denied(t, pfOps, "SELECT * FROM pf_extension.task_current LIMIT 1")
	mkgPub, mkgOps := env.Pool(t, migrations.MKG, dbadmin.OutboxPublisher), env.Pool(t, migrations.MKG, dbadmin.OpsJobs)
	for _, q := range []string{
		"SELECT tenant_id, status FROM mkg_hub.facility_current LIMIT 1",
		"SELECT tenant_id, status, next_retry_at FROM mkg_acctbridge.journal_current LIMIT 1",
		"SELECT tenant_id, status FROM mkg_logistics.trip_current LIMIT 1",
		"SELECT tenant_id, status FROM mkg_logistics.vessel_current LIMIT 1",
	} {
		allowed(t, mkgOps, q)
		denied(t, mkgPub, q)
	}
	denied(t, mkgOps, "SELECT * FROM mkg_acctbridge.journal_current LIMIT 1")
}
