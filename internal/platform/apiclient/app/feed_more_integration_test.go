package app_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/apiclient/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/apiclient/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/apiclient/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/objstore"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

// R-INT-06: giữ 30 ngày — job dọn xóa mục quá hạn (trigger DB cấm sửa và cấm xóa mục chưa đủ 30 ngày), cursor cũ →
// hết hạn (cổng trả 410 kèm ảnh chụp), đọc từ đầu bắt đầu sau phần đã dọn; dataset_version không bị chạm.
func TestFeedRetentionPurge_W53(t *testing.T) {
	f := newFeedFixture(t)
	ctx := context.Background()
	f.now = clock.NewFixed(time.Now().Add(-40 * 24 * time.Hour))
	f.svc.Clock = f.now
	f.src.set(f.t1, "89001", 12)
	if _, err := f.svc.BuildAggregate(ctx, port.FeedHarvestProgress); err != nil {
		t.Fatal(err)
	}
	f.now.Advance(39 * 24 * time.Hour) // còn 1 ngày tuổi
	f.src.set(f.t1, "89002", 15)
	if _, err := f.svc.BuildAggregate(ctx, port.FeedHarvestProgress); err != nil {
		t.Fatal(err)
	}
	// Trigger áp cả chủ bảng: không xóa được mục mới, không sửa được mục nào.
	admin := pgtest.Get(t).AdminPool(t, migrations.PF)
	if _, err := admin.Exec(ctx, "DELETE FROM pf_apiclient.feed_entry WHERE tenant_id = $1::uuid AND recorded_at > now() - interval '2 days'", f.op); err == nil ||
		!strings.Contains(err.Error(), "chỉ ghi thêm") {
		t.Fatalf("xóa mục chưa đủ 30 ngày phải bị trigger chặn: %v", err)
	}
	if _, err := admin.Exec(ctx, "UPDATE pf_apiclient.feed_entry SET op = op WHERE tenant_id = $1::uuid", f.op); err == nil {
		t.Fatal("sửa kho feed phải bị chặn")
	}
	n, err := f.svc.PurgeFeeds(ctx, 1)
	if err != nil || n != 1 {
		t.Fatalf("dọn đúng 1 mục quá 30 ngày (lô 1): %d %v", n, err)
	}
	p := f.read(t, ptr(0), 10)
	if !p.Expired || p.Latest == nil || p.Latest.Version != 2 || p.Latest.SnapshotKey == nil {
		t.Fatalf("cursor 0 < purged_through → hết hạn kèm phiên bản mới nhất: %+v", p)
	}
	p = f.read(t, nil, 10)
	if p.Expired || len(p.Entries) != 1 || p.Entries[0].Seq != 2 || p.Entries[0].EntityKey != "89002:2026-HT" {
		t.Fatalf("đọc từ đầu phần còn giữ: %+v", p.Entries)
	}
	if err := tenant.WithTenantTx(ctx, f.svc.DB, f.op, func(tx pgx.Tx) error {
		v, err := store.DatasetVersionByNo(ctx, tx, port.FeedHarvestProgress, 1)
		if err == nil && v == nil {
			err = fmt.Errorf("dataset_version 1 phải còn")
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
}

// ADR-0033 §2: URL ký của ảnh chụp cấp lúc yêu cầu, hết hạn sau ≤ 15 phút; DB chỉ lưu khóa đối tượng.
func TestSnapshotSignedURLExpiry_W53(t *testing.T) {
	f := newFeedFixture(t)
	ctx := context.Background()
	f.src.set(f.t1, "89001", 12)
	r, err := f.svc.BuildAggregate(ctx, port.FeedHarvestProgress)
	if err != nil {
		t.Fatal(err)
	}
	link, err := f.svc.SnapshotURL(ctx, port.FeedHarvestProgress, r.Version)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(link.URL, f.srv.URL) || link.ExpiresAt.Sub(f.now.Now()) > objstore.MaxPresignTTL {
		t.Fatalf("URL ký ≤ 15 phút: %+v", link)
	}
	get := func() int {
		res, err := http.Get(link.URL) //nolint:noctx,gosec // kiểm thử, URL do kho cục bộ cấp
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(res.Body)
		_ = res.Body.Close()
		if res.StatusCode == http.StatusOK {
			s := sha256.Sum256(b)
			if hex.EncodeToString(s[:]) != r.SHA256 {
				t.Fatal("tệp tải về phải khớp tổng kiểm")
			}
		}
		return res.StatusCode
	}
	if c := get(); c != http.StatusOK {
		t.Fatalf("URL còn hạn → 200: %d", c)
	}
	f.storeNow = f.storeNow.Add(16 * time.Minute)
	if c := get(); c != http.StatusForbidden {
		t.Fatalf("URL quá hạn → 403: %d", c)
	}
	if _, err := f.svc.SnapshotURL(ctx, port.FeedHarvestProgress, 99); err == nil || !strings.Contains(err.Error(), port.ErrSnapshotNotFound.Code) {
		t.Fatalf("phiên bản không có → integration.snapshot_not_found: %v", err)
	}
	var leaked int
	if err := tenant.WithTenantTx(ctx, f.svc.DB, f.op, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM pf_apiclient.dataset_version WHERE snapshot_key LIKE '%sig=%' OR snapshot_key LIKE 'http%'`).Scan(&leaked)
	}); err != nil || leaked != 0 {
		t.Fatalf("không lưu URL ký: %d %v", leaked, err)
	}
}

// Mặc định từ chối: một bản ghi sai hợp đồng → cả phiên bản không ghi; k nhỏ hơn k_default → từ chối dựng.
func TestFeedBuildRejections_W53(t *testing.T) {
	f := newFeedFixture(t)
	ctx := context.Background()
	f.src.set(f.t1, "89001", 12)
	f.src.extra = []port.AggregateCell{{ProvinceCode: "89", ProvinceValidFrom: "2025-07-01", CommuneCode: "8900X", AdminUnitValidFrom: "2025-07-01",
		SeasonCode: "2026-HT", SeasonYear: 2026, Units: strings.Split("a,b,c,d,e,f,g,h,i,j,k", ","),
		Measures: map[string]decimal.Decimal{port.MeasureAreaSown: decimal.NewFromInt(1)}}}
	_, err := f.svc.BuildAggregate(ctx, port.FeedHarvestProgress)
	if err == nil || !strings.Contains(err.Error(), port.ErrRecordRejected.Code) {
		t.Fatalf("mã xã sai mẫu hợp đồng → integration.record_rejected: %v", err)
	}
	if p := f.read(t, nil, 10); len(p.Entries) != 0 || p.Latest != nil {
		t.Fatalf("không ghi gì khi từ chối: %+v", p)
	}
	f.src.extra = nil
	f.svc.Params = fixedK(5)
	if _, err := f.svc.BuildAggregate(ctx, port.FeedHarvestProgress); err == nil || !strings.Contains(err.Error(), port.ErrKBelowFloor.Code) {
		t.Fatalf("k = 5 < k_default 10 → từ chối: %v", err)
	}
	f.svc.Params = fixedK(12)
	if r, err := f.svc.BuildAggregate(ctx, port.FeedHarvestProgress); err != nil || r.RecordCount != 1 {
		t.Fatalf("k = 12 ≥ k_default → dựng: %+v %v", r, err)
	}
	if p := f.read(t, nil, 10); p.Dataset == nil || *p.Dataset.KUsed != 12 {
		t.Fatalf("k_used ghi vào phiên bản: %+v", p.Dataset)
	}
}

func strp(s string) *string { return &s }

// admin_units từ bảng chiếu lõi k_ref.admin_unit (W2.5): bản ghi khớp hợp đồng, kế thừa basis_status pending_c9.
func TestAdminUnitsFeed_W53(t *testing.T) {
	f := newFeedFixture(t)
	ctx := context.Background()
	admin := pgtest.Get(t).AdminPool(t, migrations.PF)
	for _, u := range []struct {
		code, from, level string
		to, parent        *string
		succ              []string
	}{
		{code: "89", from: "2025-07-01", level: "province", succ: []string{}},
		{code: "89001", from: "2020-01-01", level: "commune", to: strp("2025-07-01"), parent: strp("89"), succ: []string{"89005"}},
		{code: "89005", from: "2025-07-01", level: "commune", parent: strp("89"), succ: []string{}},
	} {
		if _, err := admin.Exec(ctx, `INSERT INTO k_ref.admin_unit (tenant_id, code, valid_from, valid_to, name, level, parent_code, successor_codes)
		  VALUES ($1::uuid, $2::text, $3::date, $4::date, $5::text, $6::text, $7::text, $8::text[])`,
			f.op, u.code, u.from, u.to, "Đơn vị "+u.code, u.level, u.parent, u.succ); err != nil {
			t.Fatal(err)
		}
	}
	r, err := f.svc.BuildAdminUnits(ctx)
	if err != nil || r.Version != 1 || r.RecordCount != 3 {
		t.Fatalf("admin_units phiên bản 1, 3 bản ghi: %+v %v", r, err)
	}
	c, err := app.FeedContract(port.FeedAdminUnits, 0)
	if err != nil {
		t.Fatal(err)
	}
	p, err := f.svc.ReadFeed(ctx, c, "", nil, 10)
	if err != nil || len(p.Entries) != 3 || p.Entries[2].EntityKey != "89:2025-07-01" {
		t.Fatalf("khóa code:valid_from theo thứ tự entity_key: %+v %v", p.Entries, err)
	}
	var rec map[string]any
	if err := json.Unmarshal(p.Entries[0].Record, &rec); err != nil {
		t.Fatal(err)
	}
	succ, _ := rec["successors"].([]any)
	if len(succ) != 1 {
		t.Fatalf("kế thừa của 89001: %v", rec)
	}
	s0 := succ[0].(map[string]any)
	if s0["code"] != "89005" || s0["basis_status"] != "pending_c9" || s0["valid_from"] != "2025-07-01" || s0["effective_on"] != "2025-07-01" {
		t.Fatalf("kế thừa của 89001: %v", s0)
	}
}
