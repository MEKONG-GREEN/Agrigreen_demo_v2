package app_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/contracts/integration"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/apiclient/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/apiclient/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/apiclient/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/objstore"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/params"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// fakeAggregates là nguồn ô thô giả: tenant → xã → số nông hộ (mã nông hộ "<xã>-<i>").
type fakeAggregates struct {
	mu    sync.Mutex
	hh    map[uuid.UUID]map[string]int
	extra []port.AggregateCell
}

func (f *fakeAggregates) set(t uuid.UUID, commune string, n int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.hh[t] == nil {
		f.hh[t] = map[string]int{}
	}
	f.hh[t][commune] = n
}

func (f *fakeAggregates) AggregateCells(_ context.Context, dataset string, t uuid.UUID, _ time.Time) ([]port.AggregateCell, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if dataset != port.FeedHarvestProgress {
		return nil, nil
	}
	var out []port.AggregateCell
	for commune, n := range f.hh[t] {
		c := port.AggregateCell{ProvinceCode: "89", ProvinceValidFrom: "2025-07-01", CommuneCode: commune, AdminUnitValidFrom: "2025-07-01",
			SeasonCode: "2026-HT", SeasonYear: 2026, DateFrom: "2026-10-10", DateTo: "2026-10-20",
			Measures: map[string]decimal.Decimal{port.MeasureAreaSown: decimal.NewFromInt(int64(10 * n)), port.MeasureAreaHarvested: decimal.NewFromInt(int64(4 * n))}}
		for i := range n {
			c.Units = append(c.Units, fmt.Sprintf("%s-%d", commune, i))
		}
		out = append(out, c)
	}
	return append(out, f.extra...), nil
}

type fixedK int

func (k fixedK) At(context.Context, string, time.Time) (params.ParamValue, error) {
	return params.ParamValue{Value: decimal.NewFromInt(int64(k))}, nil
}

type feedFixture struct {
	*fixture
	now      *clock.Fixed
	src      *fakeAggregates
	store    *objstore.Local
	storeNow time.Time
	srv      *httptest.Server
	t1, t2   uuid.UUID
}

func newFeedFixture(t *testing.T) *feedFixture {
	f := &feedFixture{fixture: newFixture(t), src: &fakeAggregates{hh: map[uuid.UUID]map[string]int{}}, t1: ids.New(), t2: ids.New()}
	f.storeNow = time.Now()
	var h http.Handler
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { h.ServeHTTP(w, r) }))
	t.Cleanup(f.srv.Close)
	st, err := objstore.NewLocal(t.TempDir(), objstore.LocalOptions{BaseURL: f.srv.URL, Now: func() time.Time { return f.storeNow }})
	if err != nil {
		t.Fatal(err)
	}
	f.store, h = st, st.Handler()
	f.now = clock.NewFixed(time.Now().Add(-2 * time.Hour))
	f.svc.Clock, f.svc.Store, f.svc.Aggregates, f.svc.Contributors = f.now, st, f.src, port.StaticContributors{f.t1, f.t2}
	return f
}

func (f *feedFixture) read(t *testing.T, after *int64, limit int) app.FeedPage {
	t.Helper()
	c, err := app.FeedContract(port.FeedHarvestProgress, 0)
	if err != nil {
		t.Fatal(err)
	}
	p, err := f.svc.ReadFeed(context.Background(), c, "", after, limit)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func ptr(n int64) *int64 { return &n }

// W5.3 (R-INT-05, R-INT-06, R-INT-10, ADR-0035): dựng tập tổng hợp qua ngưỡng k (gộp hai tenant đóng góp, đơn vị
// phân biệt), phiên bản + tổng kiểm = băm ảnh chụp ở kho đối tượng, feed chỉ nối thay đổi (upsert/retract) theo
// entity_key, cursor phân trang, trễ ẩn qua các phiên bản, không thay đổi → không phiên bản mới.
func TestFeedDatasetBuildAndPaging_W53(t *testing.T) {
	f := newFeedFixture(t)
	ctx := context.Background()
	f.src.set(f.t1, "89001", 8)
	f.src.set(f.t2, "89001", 4) // 8 + 4 = 12 nông hộ phân biệt xuyên tenant ≥ k
	f.src.set(f.t1, "89002", 6)
	f.src.set(f.t1, "89003", 5)

	r1, err := f.svc.BuildAggregate(ctx, port.FeedHarvestProgress)
	if err != nil {
		t.Fatal(err)
	}
	if r1.Version != 1 || !r1.Changed || r1.Upserts != 2 || r1.RecordCount != 2 || r1.Suppressed != 2 || r1.SnapshotKey == "" {
		t.Fatalf("phiên bản 1: 89001 + dòng withheld 89 (6 + 5 = 11 ≥ k), 2 ô ẩn: %+v", r1)
	}
	// Ảnh chụp ở kho đối tượng: sha256 của tệp = tổng kiểm.
	rc, info, err := f.store.Get(ctx, r1.SnapshotKey)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(rc)
	_ = rc.Close()
	sum := sha256.Sum256(body)
	if hex.EncodeToString(sum[:]) != r1.SHA256 || info.SHA256 != r1.SHA256 || strings.Count(string(body), "\n") != 2 ||
		!strings.Contains(string(body), `"area_level":"province_withheld"`) || strings.Contains(string(body), "89002") {
		t.Fatalf("ảnh chụp NDJSON + tổng kiểm: %s", body)
	}

	// Trang 1 (limit 1) → has_more; trang 2 → hết; cursor từ đầu đúng thứ tự entity_key.
	p := f.read(t, nil, 1)
	if len(p.Entries) != 1 || !p.HasMore || p.Entries[0].EntityKey != "89001:2026-HT" || p.Dataset == nil || p.Dataset.Version != 1 ||
		*p.Dataset.KUsed != 10 || *p.Dataset.Contributors != 2 || p.Dataset.SHA256 != r1.SHA256 {
		t.Fatalf("trang 1: %+v", p)
	}
	p2 := f.read(t, ptr(p.Next), 10)
	if len(p2.Entries) != 1 || p2.HasMore || p2.Entries[0].EntityKey != "89:2026-HT" || p2.Next != p.Next+1 {
		t.Fatalf("trang 2: %+v", p2)
	}
	if p3 := f.read(t, ptr(p2.Next), 10); len(p3.Entries) != 0 || p3.Next != p2.Next {
		t.Fatalf("hết feed: next_cursor giữ nguyên: %+v", p3)
	}
	if _, err := f.svc.ReadFeed(ctx, mustFeed(t), "", ptr(p2.Next+5), 10); err == nil || !strings.Contains(err.Error(), "invalid_cursor") {
		t.Fatalf("cursor vượt đầu feed → integration.invalid_cursor: %v", err)
	}

	// Không đổi → không phiên bản mới, không mục mới.
	if r, err := f.svc.BuildAggregate(ctx, port.FeedHarvestProgress); err != nil || r.Changed || r.Version != 1 {
		t.Fatalf("không đổi → không phiên bản: %+v %v", r, err)
	}

	// 89001 xuống 9 (< k) → retract; withheld đổi (gom 3 ô).
	f.src.set(f.t2, "89001", 1)
	r2, err := f.svc.BuildAggregate(ctx, port.FeedHarvestProgress)
	if err != nil || r2.Version != 2 || r2.Retracts != 1 || r2.Upserts != 1 || r2.RecordCount != 1 {
		t.Fatalf("phiên bản 2: retract 89001, cập nhật withheld: %+v %v", r2, err)
	}
	p4 := f.read(t, ptr(p2.Next), 10)
	if len(p4.Entries) != 2 || p4.Entries[0].EntityKey != "89001:2026-HT" || p4.Entries[0].Op != "retract" || p4.Entries[0].Record != nil ||
		p4.Entries[1].Op != "upsert" || *p4.Entries[1].DatasetVersion != 2 || p4.Dataset.Version != 2 {
		t.Fatalf("mục phiên bản 2 theo entity_key: %+v", p4.Entries)
	}

	// Trễ ẩn: 89001 lên 11 (≥ k nhưng < k + 2) → vẫn ẩn; 12 → phát lại.
	f.src.set(f.t2, "89001", 3)
	if _, err := f.svc.BuildAggregate(ctx, port.FeedHarvestProgress); err != nil {
		t.Fatal(err)
	}
	for _, e := range f.read(t, ptr(p4.Next), 50).Entries {
		if e.EntityKey == "89001:2026-HT" {
			t.Fatalf("11 < k + 2 → 89001 vẫn ẩn: %+v", e)
		}
	}
	f.src.set(f.t2, "89001", 4)
	if _, err := f.svc.BuildAggregate(ctx, port.FeedHarvestProgress); err != nil {
		t.Fatal(err)
	}
	last := f.read(t, ptr(p4.Next), 50)
	found := false
	for _, e := range last.Entries {
		found = found || (e.EntityKey == "89001:2026-HT" && e.Op == "upsert")
	}
	if !found {
		t.Fatalf("12 = k + 2 → 89001 phát lại: %+v", last.Entries)
	}

	// Trạng thái ẩn ghi ở dataset_cell_state, không lưu số đơn vị đếm.
	if err := tenant.WithTenantTx(ctx, f.svc.DB, f.op, func(tx pgx.Tx) error {
		st, err := store.CellStates(ctx, tx, port.FeedHarvestProgress)
		if err != nil {
			return err
		}
		if st["89002:2026-HT"] != true || st["89001:2026-HT"] != false {
			return fmt.Errorf("trạng thái ẩn: %v", st)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func mustFeed(t *testing.T) *integration.Contract {
	t.Helper()
	c, err := app.FeedContract(port.FeedHarvestProgress, 0)
	if err != nil {
		t.Fatal(err)
	}
	return c
}
