package app_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/engine"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/engine/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/engine/port"
	foport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/fieldops/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	shareddb "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/db"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ledger"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

func TestMain(m *testing.M) { os.Exit(pgtest.Run(m)) }

// fakeFeed là cổng tích hợp giả ở mức port.DirectoryFeed (phong bì đã giải — bộ điều hợp HTTP của W5.5 có kiểm thử
// riêng): mỗi feed một dãy mục, cursor = "s<số mục đã trả>", trang `page` mục. errs: lỗi trả MỘT lần cho lời gọi kế.
type fakeFeed struct {
	mu     sync.Mutex
	items  map[string][]foport.DirectoryFeedItem
	asOf   time.Time
	maxAge time.Duration
	page   int
	errs   map[string]error
	calls  int
}

func newFakeFeed(asOf time.Time) *fakeFeed {
	return &fakeFeed{items: map[string][]foport.DirectoryFeedItem{}, asOf: asOf, maxAge: 15 * time.Minute, page: 2, errs: map[string]error{}}
}

func (f *fakeFeed) add(feed, key string, rec any) {
	f.mu.Lock()
	defer f.mu.Unlock()
	it := foport.DirectoryFeedItem{Seq: int64(len(f.items[feed]) + 1), EventID: ids.New(), EntityKey: key, Op: "retract", RecordedAt: f.asOf}
	if rec != nil {
		raw, _ := json.Marshal(rec)
		it.Op, it.Record = "upsert", raw
	}
	f.items[feed] = append(f.items[feed], it)
}

func (f *fakeFeed) Page(_ context.Context, feed, cursor string, _ int) (foport.DirectoryFeedPage, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if err := f.errs[feed]; err != nil {
		delete(f.errs, feed)
		return foport.DirectoryFeedPage{}, err
	}
	after := 0
	if cursor != "" {
		after, _ = strconv.Atoi(strings.TrimPrefix(cursor, "s"))
	}
	all := f.items[feed]
	end := min(after+f.page, len(all))
	return foport.DirectoryFeedPage{Feed: feed, ContractVersion: 1, NextCursor: fmt.Sprintf("s%d", end), HasMore: end < len(all),
		AsOf: f.asOf, MaxAge: f.maxAge, Items: slices.Clone(all[after:end])}, nil
}

func harvestRec(area, level, sown, harvested, share string) map[string]any {
	return map[string]any{"area_code": area, "area_level": level, "province_code": area[:2], "admin_unit_valid_from": "2025-07-01",
		"season_code": "2026-HT", "season_year": 2026, "area_ha_sown": sown, "area_ha_harvested": harvested, "harvested_share": share,
		"expected_harvest_from": nil, "expected_harvest_to": nil}
}

func plotRec(ref uuid.UUID, area string) map[string]any {
	return map[string]any{"plot_ref": ref.String(), "code": "T-" + ref.String()[:4], "commune_code": "89123", "cooperative_ref": nil,
		"status": "active", "area_ha": area, "revision": 1, "updated_at": "2026-10-08T00:00:00Z"}
}

type fixture struct {
	t    *testing.T
	env  *pgtest.Env
	clk  *clock.Fixed
	feed *fakeFeed
	mod  *engine.Module
	tid  uuid.UUID
}

func newFixture(t *testing.T, feeds ...string) *fixture {
	env := pgtest.Get(t)
	now := time.Date(2026, 10, 9, 1, 0, 0, 0, time.UTC)
	f := &fixture{t: t, env: env, clk: clock.NewFixed(now), feed: newFakeFeed(now), tid: ids.New()}
	f.mod = engine.New(env.Pool(t, migrations.MKG, dbadmin.AppInternal), f.clk, engine.Options{Feed: f.feed, Feeds: feeds, AllowProposed: true})
	return f
}

func (f *fixture) sync() map[string]engine.FeedSync {
	f.t.Helper()
	rs, err := f.mod.SyncObservationFeeds(context.Background(), f.tid)
	if err != nil {
		f.t.Fatalf("sync: %v", err)
	}
	out := map[string]engine.FeedSync{}
	for _, r := range rs {
		out[r.Feed] = r
	}
	return out
}

func (f *fixture) latest(kind string, keys ...string) port.Snapshot {
	f.t.Helper()
	s, err := f.mod.Observations().Latest(context.Background(), f.tid, port.Query{Kind: kind, EntityKeys: keys})
	if err != nil {
		f.t.Fatalf("latest %s: %v", kind, err)
	}
	return s
}

func (f *fixture) count() int {
	f.t.Helper()
	var n int
	owner := f.env.Pool(f.t, migrations.MKG, dbadmin.Migrator)
	if err := owner.QueryRow(context.Background(), `SELECT count(*) FROM mkg_engine.observation WHERE tenant_id = $1`, f.tid).Scan(&n); err != nil {
		f.t.Fatal(err)
	}
	return n
}

// asEngine: pool vai đăng nhập app_internal, giao dịch SET LOCAL ROLE r_engine (như module).
func asEngine(p tenant.Beginner) tenant.Beginner { return shareddb.ForModule(p, engine.DBModule) }

func one(t *testing.T, s port.Snapshot, key string) port.Observation {
	t.Helper()
	for _, o := range s.Fresh {
		if o.EntityKey == key {
			return o
		}
	}
	t.Fatalf("%s: %q không có trong Fresh (stale %d, missing %v)", s.Kind, key, len(s.Stale), s.Missing)
	return port.Observation{}
}

// Trang feed → quan sát: giá trị decimal giữ số lẻ, cận bất định lan truyền, nguồn gốc feed, phát lại idempotent.
func TestFeedPagesBecomeObservations(t *testing.T) {
	f := newFixture(t, "harvest_progress_by_commune", "plot_directory")
	plot := ids.New()
	f.feed.add("harvest_progress_by_commune", "89123:2026-HT", harvestRec("89123", "commune", "812.40", "530.15", "0.6526"))
	f.feed.add("harvest_progress_by_commune", "89:2026-HT", harvestRec("89", "province_withheld", "143.20", "61.00", "0.4260"))
	f.feed.add("harvest_progress_by_commune", "89140:2026-HT", harvestRec("89140", "commune", "10.00", "1.00", "0.1000"))
	f.feed.add("plot_directory", plot.String(), plotRec(plot, "1.2500"))

	rs := f.sync()
	h := rs["harvest_progress_by_commune"]
	if h.Pages != 2 || h.Items != 3 || h.Observed != 6 || !h.CaughtUp || h.Stale {
		t.Fatalf("harvest sync: %+v", h)
	}
	if p := rs["plot_directory"]; p.Observed != 1 || !p.CaughtUp {
		t.Fatalf("plot sync: %+v", p)
	}

	rem := one(t, f.latest(port.KindHarvestAreaRemaining, "89123:2026-HT"), "89123:2026-HT")
	if rem.Value.String() != "282.25" || rem.Unit != port.UnitHectare || !rem.Uncertainty.Equal(decimal.RequireFromString("0.01")) {
		t.Errorf("remaining: %s %s ± %s", rem.Value, rem.Unit, rem.Uncertainty)
	}
	if rem.Source != port.SourceAPI || rem.Feed != "harvest_progress_by_commune" || rem.ContractVersion != 1 || rem.FeedSeq != 1 ||
		rem.EventID == uuid.Nil || rem.FeedCursor != "" || rem.MaxAge != 15*time.Minute || rem.MapperVersion != domain.MapperVersion {
		t.Errorf("nguồn gốc: %+v", rem)
	}
	var rec map[string]any
	if err := json.Unmarshal(rem.Payload, &rec); err != nil || rec["area_code"] != "89123" {
		t.Errorf("payload: %s", rem.Payload)
	}
	share := f.latest(port.KindHarvestShare)
	if len(share.Fresh) != 3 {
		t.Fatalf("share: %+v", share)
	}
	residual := one(t, share, "89:2026-HT")
	if residual.FeedCursor != "" || !slices.Contains(residual.UncertaintyBasis, domain.BasisKSuppressedResidual) {
		t.Errorf("dòng province_withheld: cursor %q basis %v", residual.FeedCursor, residual.UncertaintyBasis)
	}
	if c := one(t, share, "89140:2026-HT"); c.FeedCursor != "s2" || c.FeedSeq != 3 { // trang thứ hai
		t.Errorf("cursor nguồn gốc trang 2: %q seq %d", c.FeedCursor, c.FeedSeq)
	}
	pl := one(t, f.latest(port.KindPlot, plot.String()), plot.String())
	if pl.Value.StringFixed(4) != "1.2500" || pl.Value.Exponent() != -4 {
		t.Errorf("area_ha mất số lẻ: %s", pl.Value)
	}

	// Phát lại: cursor quá hạn (410) → đọc lại từ đầu, mục đã áp bỏ qua theo idem_key.
	n := f.count()
	f.feed.errs["harvest_progress_by_commune"] = foport.ErrFeedCursorExpired
	h = f.sync()["harvest_progress_by_commune"]
	if !h.Resynced || h.Observed != 0 || h.Duplicates != 6 || f.count() != n {
		t.Fatalf("phát lại không idempotent: %+v, %d → %d dòng", h, n, f.count())
	}
	// Lượt kế không có mục mới: không ghi gì.
	if h = f.sync()["harvest_progress_by_commune"]; h.Observed != 0 || h.Items != 0 || f.count() != n {
		t.Fatalf("lượt rỗng: %+v", h)
	}

	// Rút ô (ẩn dưới k) → Missing; giá trị cũ vẫn ở sổ.
	f.feed.add("harvest_progress_by_commune", "89140:2026-HT", nil)
	if h = f.sync()["harvest_progress_by_commune"]; h.Observed != 2 || h.Retracted != 2 {
		t.Fatalf("retract: %+v", h)
	}
	s := f.latest(port.KindHarvestShare, "89140:2026-HT", "89123:2026-HT")
	if !slices.Equal(s.Missing, []string{"89140:2026-HT"}) || len(s.Fresh) != 1 {
		t.Errorf("sau retract: fresh %d missing %v", len(s.Fresh), s.Missing)
	}

	// Chuỗi băm của sổ nguyên vẹn (vai r_engine).
	if err := tenant.WithTenantTx(context.Background(), asEngine(f.env.Pool(t, migrations.MKG, dbadmin.AppInternal)), f.tid,
		func(tx pgx.Tx) error {
			r, err := ledger.Verify(context.Background(), tx, "mkg_engine.observation", f.tid, "api:harvest_progress_by_commune")
			if err != nil {
				return err
			}
			if !r.OK || r.RowsChecked != 8 {
				return fmt.Errorf("verify: %+v", r)
			}
			return nil
		}); err != nil {
		t.Fatal(err)
	}
}

// Quá max_age → Stale, không bao giờ trong Fresh; feed đọc tới đầu với as_of mới (không đổi bản ghi) → tươi lại.
func TestStaleObservationsFlagged(t *testing.T) {
	f := newFixture(t, "plot_directory")
	plot := ids.New()
	f.feed.maxAge = time.Hour
	f.feed.add("plot_directory", plot.String(), plotRec(plot, "2.0000"))
	f.sync()
	if s := f.latest(port.KindPlot); len(s.Fresh) != 1 || len(s.Stale) != 0 {
		t.Fatalf("ban đầu: %+v", s)
	}
	f.clk.Advance(time.Hour + time.Second)
	s := f.latest(port.KindPlot)
	if len(s.Fresh) != 0 || len(s.Stale) != 1 || s.Stale[0].EntityKey != plot.String() {
		t.Fatalf("quá max_age phải Stale: fresh %d stale %d", len(s.Fresh), len(s.Stale))
	}
	// Nền tảng ngừng: lượt kéo lỗi → giữ quan sát cũ (Stale), không chặn.
	f.feed.errs["plot_directory"] = errors.New("cổng không trả lời")
	if _, err := f.mod.SyncObservationFeeds(context.Background(), f.tid); err == nil {
		t.Fatal("muốn lỗi khi cổng lỗi")
	}
	if s = f.latest(port.KindPlot); len(s.Stale) != 1 {
		t.Fatalf("sau lỗi cổng: %+v", s)
	}
	// Cổng trở lại, as_of tiến, bản ghi không đổi → as_of hiệu lực tiến theo feed.
	f.feed.asOf = f.clk.Now()
	if r := f.sync()["plot_directory"]; r.Observed != 0 || r.Stale {
		t.Fatalf("đọc lại: %+v", r)
	}
	s = f.latest(port.KindPlot)
	if len(s.Fresh) != 1 || !s.Fresh[0].EffectiveAsOf.Equal(f.clk.Now()) || s.Fresh[0].AsOf.Equal(f.clk.Now()) {
		t.Fatalf("as_of hiệu lực: %+v", s)
	}
}

// Thu hồi cấp quyền (403 party.share_grant_missing) → quan sát rút grant_revoked cho mọi entity của feed.
func TestGrantRevokedRetractsFeedObservations(t *testing.T) {
	f := newFixture(t, "cooperative_directory")
	a, b := ids.New(), ids.New()
	for _, ref := range []uuid.UUID{a, b} {
		f.feed.add("cooperative_directory", ref.String(), map[string]any{"cooperative_ref": ref.String(), "code": "HTX", "name": "HTX thử",
			"commune_code": "89123", "active": true, "lon": nil, "lat": nil, "updated_at": "2026-10-08T00:00:00Z"})
	}
	f.sync()
	if s := f.latest(port.KindCooperative); len(s.Fresh) != 2 || !slices.Equal(s.Fresh[0].UncertaintyBasis, []string{domain.BasisLocationMissing}) {
		t.Fatalf("trước thu hồi: %+v", s)
	}
	f.feed.errs["cooperative_directory"] = foport.ErrFeedGrantMissing
	r := f.sync()["cooperative_directory"]
	if !r.Revoked || r.Retracted != 2 {
		t.Fatalf("thu hồi: %+v", r)
	}
	s := f.latest(port.KindCooperative)
	if len(s.Fresh)+len(s.Stale) != 0 || len(s.Missing) != 2 {
		t.Fatalf("sau thu hồi: %+v", s)
	}
	// Thu hồi lặp: không ghi thêm.
	n := f.count()
	f.feed.errs["cooperative_directory"] = foport.ErrFeedGrantMissing
	if r = f.sync()["cooperative_directory"]; r.Retracted != 0 || f.count() != n {
		t.Fatalf("thu hồi lặp: %+v", r)
	}
}

// Hợp đồng proposed không kéo khi thiếu cờ dev/test; feed lệch hợp đồng → không áp trang, cursor giữ nguyên.
func TestContractGate(t *testing.T) {
	env := pgtest.Get(t)
	now := time.Date(2026, 10, 9, 1, 0, 0, 0, time.UTC)
	fk := newFakeFeed(now)
	tid := ids.New()
	m := engine.New(env.Pool(t, migrations.MKG, dbadmin.AppInternal), clock.NewFixed(now), engine.Options{Feed: fk, Feeds: []string{"plot_directory"}})
	rs, err := m.SyncObservationFeeds(context.Background(), tid)
	if err != nil || len(rs) != 1 || rs[0].Skipped != "contract_not_approved" || fk.calls != 0 {
		t.Fatalf("không cờ: %+v %v (calls %d)", rs, err, fk.calls)
	}
	f := newFixture(t, "plot_directory")
	plot := ids.New()
	bad := plotRec(plot, "1.0000")
	bad["owner_name"] = "lạ" // khóa ngoài hợp đồng (mặc định từ chối)
	f.feed.add("plot_directory", plot.String(), bad)
	if _, err := f.mod.SyncObservationFeeds(context.Background(), f.tid); err == nil {
		t.Fatal("bản ghi lệch hợp đồng phải lỗi")
	}
	other := ids.New()
	f.feed.items["plot_directory"] = nil
	f.feed.add("plot_directory", plot.String(), plotRec(other, "1.0000")) // entity_key ≠ plot_ref
	if _, err := f.mod.SyncObservationFeeds(context.Background(), f.tid); err == nil {
		t.Fatal("entity_key lệch phải lỗi")
	}
	if f.count() != 0 {
		t.Fatalf("trang lỗi không được ghi: %d dòng", f.count())
	}
	if _, err := f.mod.Observations().Latest(context.Background(), f.tid, port.Query{Kind: "no_such_kind"}); !errors.Is(err, port.ErrUnknownKind) {
		t.Errorf("kind lạ: %v", err)
	}
}

// RLS + sổ chỉ ghi thêm: tenant khác không thấy; r_engine không UPDATE/DELETE được; trigger chặn cả chủ sở hữu.
func TestObservationIsolationAndImmutability(t *testing.T) {
	f := newFixture(t, "plot_directory")
	plot := ids.New()
	f.feed.add("plot_directory", plot.String(), plotRec(plot, "3.0000"))
	f.sync()
	other := ids.New()
	s, err := f.mod.Observations().Latest(context.Background(), other, port.Query{Kind: port.KindPlot, EntityKeys: []string{plot.String()}})
	if err != nil || len(s.Fresh)+len(s.Stale) != 0 || !slices.Equal(s.Missing, []string{plot.String()}) {
		t.Fatalf("tenant khác thấy quan sát: %+v %v", s, err)
	}
	ctx := context.Background()
	app := asEngine(f.env.Pool(t, migrations.MKG, dbadmin.AppInternal))
	for _, q := range []string{`UPDATE mkg_engine.observation SET retracted = true`, `DELETE FROM mkg_engine.observation`,
		`DELETE FROM mkg_engine.observation_feed_state`} {
		err := tenant.WithTenantTx(ctx, app, f.tid, func(tx pgx.Tx) error { _, e := tx.Exec(ctx, q); return e })
		if err == nil || !strings.Contains(err.Error(), "42501") {
			t.Errorf("%s dưới r_engine phải 42501, nhận %v", q, err)
		}
	}
	owner := f.env.Pool(t, migrations.MKG, dbadmin.Migrator)
	err = tenant.WithTenantTx(ctx, owner, f.tid, func(tx pgx.Tx) error {
		_, e := tx.Exec(ctx, `UPDATE mkg_engine.observation SET retracted = true`)
		return e
	})
	if err == nil {
		t.Error("trigger sổ phải chặn UPDATE kể cả chủ sở hữu")
	}
	// Ngoài giao dịch tenant: RLS ẩn mọi dòng.
	var n int
	err = tenant.WithTenantTx(ctx, app, other, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM mkg_engine.observation`).Scan(&n)
	})
	if err != nil || n != 0 {
		t.Errorf("RLS: %d dòng, %v", n, err)
	}
}
