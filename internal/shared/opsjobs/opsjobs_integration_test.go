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
	pub := env.Pool(t, migrations.MKG, dbadmin.OutboxPublisher)
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
	pub := env.Pool(t, migrations.PF, dbadmin.OutboxPublisher)
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
