package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/fieldops/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/fieldops/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
)

// memFeeds là feedRepo trong bộ nhớ (thay bảng mkg 000036).
type memFeeds struct {
	mu      sync.Mutex
	st      map[string]store.FeedState
	recs    map[string]map[uuid.UUID]memRec
	lh      []store.ReplicaHTX
	lp      []store.ReplicaPlot
	fail    bool // apply lỗi (giao dịch hỏng)
	applies int
}

type memRec struct {
	seq int64
	rec json.RawMessage
}

func newMemFeeds() *memFeeds {
	return &memFeeds{st: map[string]store.FeedState{}, recs: map[string]map[uuid.UUID]memRec{
		port.FeedCooperativeDirectory: {}, port.FeedPlotDirectory: {}}}
}

func (m *memFeeds) states(context.Context, uuid.UUID) (map[string]store.FeedState, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := map[string]store.FeedState{}
	for k, v := range m.st {
		out[k] = v
	}
	return out, nil
}

func (m *memFeeds) apply(_ context.Context, _ uuid.UUID, feed string, items []feedItem, st store.FeedState) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fail {
		return errors.New("giao dịch hỏng")
	}
	m.applies++
	for _, it := range items {
		cur, ok := m.recs[feed][it.ref]
		switch {
		case it.Op == "retract" && (!ok || cur.seq < it.Seq):
			delete(m.recs[feed], it.ref)
		case it.Op == "upsert" && (!ok || cur.seq < it.Seq):
			m.recs[feed][it.ref] = memRec{seq: it.Seq, rec: it.Record}
		}
	}
	m.st[feed] = st
	return nil
}

func (m *memFeeds) reset(_ context.Context, _ uuid.UUID, feed string, st store.FeedState) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := int64(len(m.recs[feed]))
	m.recs[feed] = map[uuid.UUID]memRec{}
	m.st[feed] = st
	return n, nil
}

func (m *memFeeds) records(_ context.Context, _ uuid.UUID, feed string, refs []uuid.UUID) (map[uuid.UUID]json.RawMessage, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := map[uuid.UUID]json.RawMessage{}
	for ref, r := range m.recs[feed] {
		if refs == nil || slices.Contains(refs, ref) {
			out[ref] = r.rec
		}
	}
	return out, nil
}

func (m *memFeeds) legacy(context.Context, uuid.UUID) ([]store.ReplicaHTX, []store.ReplicaPlot, error) {
	return m.lh, m.lp, nil
}

// fakeGateway là port.DirectoryFeed giả: mỗi feed một danh sách mục; cursor = "s<seq>"; ghi lại cursor đã hỏi.
type fakeGateway struct {
	mu      sync.Mutex
	items   map[string][]port.DirectoryFeedItem
	asOf    time.Time
	maxAge  time.Duration
	page    int
	errOnce map[string]error // lỗi trả MỘT lần cho lời gọi kế tiếp của feed
	errAll  map[string]error
	purged  map[string]int64 // cursor < purged → 410
	asked   map[string][]string
}

func newFakeGateway(asOf time.Time) *fakeGateway {
	return &fakeGateway{items: map[string][]port.DirectoryFeedItem{}, asOf: asOf, maxAge: 6 * time.Hour, page: 2,
		errOnce: map[string]error{}, errAll: map[string]error{}, purged: map[string]int64{}, asked: map[string][]string{}}
}

func (g *fakeGateway) add(feed, op string, ref uuid.UUID, rec any) {
	g.mu.Lock()
	defer g.mu.Unlock()
	seq := int64(len(g.items[feed]) + 1)
	var raw json.RawMessage
	if rec != nil {
		raw, _ = json.Marshal(rec)
	}
	g.items[feed] = append(g.items[feed], port.DirectoryFeedItem{Seq: seq, EventID: ids.New(), EntityKey: ref.String(), Op: op, Record: raw,
		RecordedAt: g.asOf})
}

func (g *fakeGateway) Page(_ context.Context, feed, cursor string, limit int) (port.DirectoryFeedPage, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.asked[feed] = append(g.asked[feed], cursor)
	if err := g.errOnce[feed]; err != nil {
		delete(g.errOnce, feed)
		return port.DirectoryFeedPage{}, err
	}
	if err := g.errAll[feed]; err != nil {
		return port.DirectoryFeedPage{}, err
	}
	after := g.purged[feed] // từ đầu = từ phần còn giữ
	if cursor != "" {
		_, _ = fmt.Sscanf(cursor, "s%d", &after)
		if after < g.purged[feed] {
			return port.DirectoryFeedPage{}, port.ErrFeedCursorExpired
		}
	}
	all := g.items[feed]
	n := min(g.page, limit)
	var out []port.DirectoryFeedItem
	for _, it := range all {
		if it.Seq > after && len(out) < n {
			out = append(out, it)
		}
	}
	next := after
	if len(out) > 0 {
		next = out[len(out)-1].Seq
	}
	return port.DirectoryFeedPage{Feed: feed, ContractVersion: 1, NextCursor: fmt.Sprintf("s%d", next), HasMore: next < int64(len(all)),
		AsOf: g.asOf, MaxAge: g.maxAge, Items: out}, nil
}

type feedCase struct {
	svc  *Service
	gw   *fakeGateway
	repo *memFeeds
	clk  *clock.Fixed
	htx  uuid.UUID
	plts []uuid.UUID
}

// newFeedCase: feed có 1 HTX (xã 31117) + 3 thửa của HTX, chế độ nguồn feed.
func newFeedCase() *feedCase {
	clk := clock.NewFixed(time.Date(2026, 10, 9, 3, 0, 0, 0, time.UTC))
	c := &feedCase{gw: newFakeGateway(clk.Now()), repo: newMemFeeds(), clk: clk, htx: ids.New(), plts: []uuid.UUID{ids.New(), ids.New(), ids.New()}}
	c.svc = &Service{Clock: clk, DirectoryFeed: c.gw, DirectorySource: port.DirectorySourceFeed, feeds: c.repo}
	commune := "31117"
	c.gw.add(port.FeedCooperativeDirectory, "upsert", c.htx, map[string]any{"cooperative_ref": c.htx, "code": "HTX", "name": "HTX",
		"commune_code": commune, "active": true, "lon": nil, "lat": nil, "updated_at": "2026-10-08T00:00:00Z"})
	for _, p := range c.plts {
		c.gw.add(port.FeedPlotDirectory, "upsert", p, plotRec(p, c.htx, "active"))
	}
	return c
}

func plotRec(p, htx uuid.UUID, status string) map[string]any {
	return map[string]any{"plot_ref": p, "code": "T", "commune_code": "31117", "cooperative_ref": htx, "status": status,
		"area_ha": "1.5", "revision": 1, "updated_at": "2026-10-08T00:00:00Z"}
}

func (c *feedCase) input() PlanInput {
	return PlanInput{HTXRef: c.htx, WindowFrom: "2026-10-09", WindowTo: "2026-10-12", PlotRefs: c.plts, IdemKey: "k"}
}

func syncedOf(rs []DirectoryFeedSync, feed string) DirectoryFeedSync {
	for _, r := range rs {
		if r.Feed == feed {
			return r
		}
	}
	return DirectoryFeedSync{}
}

// Cursor chỉ tiến khi trang đã áp; lỗi giữa chừng → lượt sau đọc tiếp đúng chỗ, không đọc lại từ đầu.
func TestDirectoryFeedCursorResume(t *testing.T) {
	c := newFeedCase()
	tid := ids.New()
	rs, err := c.svc.SyncDirectoryFeeds(context.Background(), tid)
	if err != nil {
		t.Fatal(err)
	}
	if r := syncedOf(rs, port.FeedPlotDirectory); !r.CaughtUp || r.Upserts != 3 || r.Pages != 2 {
		t.Fatalf("đọc hết 3 thửa qua 2 trang: %+v", r)
	}
	// Thêm 3 mục; lượt sau: trang đầu (2 mục) áp xong, trang thứ hai lỗi mạng.
	for range 3 {
		ref := ids.New()
		c.gw.add(port.FeedPlotDirectory, "upsert", ref, plotRec(ref, c.htx, "active"))
	}
	failing := &flakyFeed{inner: c.gw, failAt: 2}
	c.svc.DirectoryFeed = failing
	if _, err := c.svc.SyncDirectoryFeeds(context.Background(), tid); err == nil || !errors.Is(err, port.ErrDirectoryUnavailable) {
		t.Fatalf("lỗi mạng giữa chừng → lỗi: %v", err)
	}
	st := c.repo.st[port.FeedPlotDirectory]
	if st.Cursor != "s5" || st.LastError == "" || !st.CaughtUp {
		t.Fatalf("cursor tiến tới trang đã áp (s5), ghi lỗi, giữ caught_up cũ: %+v", st)
	}
	c.svc.DirectoryFeed = c.gw
	c.gw.asked[port.FeedPlotDirectory] = nil
	rs, err = c.svc.SyncDirectoryFeeds(context.Background(), tid)
	if err != nil {
		t.Fatal(err)
	}
	if asked := c.gw.asked[port.FeedPlotDirectory]; len(asked) == 0 || asked[0] != "s5" {
		t.Fatalf("lượt sau đọc tiếp từ cursor đã lưu: %v", asked)
	}
	if r := syncedOf(rs, port.FeedPlotDirectory); r.Upserts != 1 || !r.CaughtUp || r.Stale {
		t.Fatalf("chỉ mục còn lại: %+v", r)
	}
	if n := len(c.repo.recs[port.FeedPlotDirectory]); n != 6 {
		t.Fatalf("6 thửa trong bản sao: %d", n)
	}
	if st := c.repo.st[port.FeedPlotDirectory]; st.Cursor != "s6" || st.LastError != "" {
		t.Fatalf("cursor cuối, xóa lỗi: %+v", st)
	}
	// Áp lại mục cũ (giao ít nhất một lần) là no-op theo seq.
	old := c.repo.recs[port.FeedPlotDirectory][c.plts[0]]
	stale := feedItem{DirectoryFeedItem: port.DirectoryFeedItem{Seq: 1, Op: "retract"}, ref: c.plts[0]}
	if err := c.repo.apply(context.Background(), tid, port.FeedPlotDirectory, []feedItem{stale}, c.repo.st[port.FeedPlotDirectory]); err != nil ||
		c.repo.recs[port.FeedPlotDirectory][c.plts[0]].seq != old.seq {
		t.Fatalf("mục retract cũ (seq 1 ≤ seq đã áp) không xóa bản ghi mới hơn")
	}
}

// flakyFeed trả lỗi mạng ở lời gọi thứ failAt (đếm từ 1) của plot_directory.
type flakyFeed struct {
	inner  port.DirectoryFeed
	failAt int
	n      int
}

func (f *flakyFeed) Page(ctx context.Context, feed, cursor string, limit int) (port.DirectoryFeedPage, error) {
	if feed == port.FeedPlotDirectory {
		f.n++
		if f.n == f.failAt {
			return port.DirectoryFeedPage{}, fmt.Errorf("%w: mạng", port.ErrDirectoryUnavailable)
		}
	}
	return f.inner.Page(ctx, feed, cursor, limit)
}

// as_of / max_age của feed được lưu và áp: quá max_age → lệnh gom vẫn lập nhưng gắn fieldops.directory_stale;
// feed chưa đọc tới đầu → chưa đối chiếu (unverified), không chặn.
func TestDirectoryFeedMaxAgeStaleness(t *testing.T) {
	c := newFeedCase()
	tid := ids.New()
	ctx := context.Background()

	in := c.input()
	in.CommuneCode = "31117" // HTX chưa đối chiếu được thì phải ghi mã xã
	pl, err := c.svc.validatePlan(ctx, tid, in)
	if err != nil || !slices.Contains(pl.warnings, WarnDirectoryUnverified) {
		t.Fatalf("chưa kéo feed → unverified, không chặn: %v %v", pl.warnings, err)
	}
	if _, err := c.svc.SyncDirectoryFeeds(ctx, tid); err != nil {
		t.Fatal(err)
	}
	st := c.repo.st[port.FeedCooperativeDirectory]
	if st.MaxAge != 6*time.Hour || st.AsOf == nil || !st.AsOf.Equal(c.clk.Now()) || !st.CaughtUp {
		t.Fatalf("lưu as_of + max_age của trang: %+v", st)
	}
	pl, err = c.svc.validatePlan(ctx, tid, c.input())
	if err != nil || len(pl.warnings) != 0 || pl.commune != "31117" || pl.asOf == nil || !pl.asOf.Equal(c.clk.Now()) {
		t.Fatalf("feed tươi → không cảnh báo, mã xã từ HTX, as_of của feed: %+v %v", pl, err)
	}
	// Feed đã đọc tới đầu là trạng thái đầy đủ: HTX / thửa vắng → không có (như bề mặt cũ trả 404).
	stranger := c.input()
	stranger.HTXRef = ids.New()
	if _, err := c.svc.validatePlan(ctx, tid, stranger); codeOf(err) != ErrHTXNotFound.Code {
		t.Fatalf("HTX vắng trong feed → fieldops.htx_not_found: %v", err)
	}
	strangePlot := c.input()
	strangePlot.PlotRefs = []uuid.UUID{c.plts[0], ids.New()}
	if _, err := c.svc.validatePlan(ctx, tid, strangePlot); codeOf(err) != ErrPlotNotFound.Code {
		t.Fatalf("thửa vắng trong feed → fieldops.plot_not_found: %v", err)
	}
	// Cổng ngừng 7 giờ (> max_age PT6H): vẫn lập theo bản sao, gắn cờ dữ liệu cũ; job báo stale.
	c.clk.Advance(7 * time.Hour)
	c.gw.errAll[port.FeedCooperativeDirectory] = fmt.Errorf("%w: 503", port.ErrDirectoryUnavailable)
	rs, err := c.svc.SyncDirectoryFeeds(ctx, tid)
	if err == nil || !syncedOf(rs, port.FeedCooperativeDirectory).Stale {
		t.Fatalf("cổng ngừng + quá max_age → job báo stale: %+v %v", rs, err)
	}
	pl, err = c.svc.validatePlan(ctx, tid, c.input())
	if err != nil || !slices.Contains(pl.warnings, WarnDirectoryStale) {
		t.Fatalf("quá max_age → fieldops.directory_stale (không chặn): %v %v", pl.warnings, err)
	}
	// max_age nhỏ hơn mặc định (6h) của cấu hình vẫn theo hợp đồng: PT1H → cũ sau 1 giờ.
	c.gw.maxAge = time.Hour
	delete(c.gw.errAll, port.FeedCooperativeDirectory)
	c.gw.asOf = c.clk.Now()
	if _, err := c.svc.SyncDirectoryFeeds(ctx, tid); err != nil {
		t.Fatal(err)
	}
	c.clk.Advance(90 * time.Minute)
	if pl, _ = c.svc.validatePlan(ctx, tid, c.input()); !slices.Contains(pl.warnings, WarnDirectoryStale) {
		t.Fatalf("max_age PT1H của feed áp: %v", pl.warnings)
	}
}

// Thu hồi cấp quyền → bản sao feed bị xóa, mục coi là "không có" (từ chối như bề mặt cũ 403); cấp lại → đọc lại.
// Cursor quá hạn (410) → đọc lại từ đầu.
func TestDirectoryFeedRevokedAndExpired(t *testing.T) {
	c := newFeedCase()
	tid := ids.New()
	ctx := context.Background()
	if _, err := c.svc.SyncDirectoryFeeds(ctx, tid); err != nil {
		t.Fatal(err)
	}
	c.gw.errOnce[port.FeedCooperativeDirectory] = port.ErrFeedGrantMissing
	rs, err := c.svc.SyncDirectoryFeeds(ctx, tid)
	if err != nil {
		t.Fatal(err)
	}
	if r := syncedOf(rs, port.FeedCooperativeDirectory); !r.Revoked || r.Cleared != 1 || r.Stale {
		t.Fatalf("thu hồi → xóa bản ghi: %+v", r)
	}
	if _, err := c.svc.validatePlan(ctx, tid, c.input()); codeOf(err) != ErrHTXNotFound.Code {
		t.Fatalf("HTX của feed bị thu hồi → fieldops.htx_not_found: %v", err)
	}
	// Cấp lại: đọc lại từ đầu.
	c.gw.asked[port.FeedCooperativeDirectory] = nil
	if _, err := c.svc.SyncDirectoryFeeds(ctx, tid); err != nil {
		t.Fatal(err)
	}
	if a := c.gw.asked[port.FeedCooperativeDirectory]; len(a) == 0 || a[0] != "" {
		t.Fatalf("sau thu hồi đọc lại từ đầu: %v", a)
	}
	if st := c.repo.st[port.FeedCooperativeDirectory]; st.RevokedAt != nil || !st.CaughtUp {
		t.Fatalf("cấp lại → hết revoked: %+v", st)
	}
	if _, err := c.svc.validatePlan(ctx, tid, c.input()); err != nil {
		t.Fatalf("cấp lại → lập được: %v", err)
	}
	// 410: cursor đã lưu (s3) < phần đã dọn → xóa, đọc lại từ đầu trong cùng lượt.
	// Mục 1–4 đã dọn; nền tảng phát lại hai thửa còn hiệu lực (port.PartnerFeedRefresh) ở seq 5–6.
	c.gw.add(port.FeedPlotDirectory, "retract", c.plts[2], nil)
	for _, p := range c.plts[:2] {
		c.gw.add(port.FeedPlotDirectory, "upsert", p, plotRec(p, c.htx, "active"))
	}
	c.gw.purged[port.FeedPlotDirectory] = 4
	c.gw.asked[port.FeedPlotDirectory] = nil
	rs, err = c.svc.SyncDirectoryFeeds(ctx, tid)
	if err != nil {
		t.Fatal(err)
	}
	if r := syncedOf(rs, port.FeedPlotDirectory); !r.Resynced || !r.CaughtUp || r.Cleared != 3 {
		t.Fatalf("410 → đồng bộ lại: %+v", r)
	}
	if a := c.gw.asked[port.FeedPlotDirectory]; len(a) < 2 || a[0] != "s3" || a[1] != "" {
		t.Fatalf("hỏi cursor cũ rồi đọc lại từ đầu: %v", a)
	}
	if _, ok := c.repo.recs[port.FeedPlotDirectory][c.plts[2]]; ok || len(c.repo.recs[port.FeedPlotDirectory]) != 2 {
		t.Fatalf("thửa đã retract không còn: %v", c.repo.recs[port.FeedPlotDirectory])
	}
}

// Mục lệch entity_key / feed lạ → không áp trang, cursor không tiến.
func TestDirectoryFeedRejectsMismatchedItem(t *testing.T) {
	c := newFeedCase()
	c.gw.mu.Lock()
	c.gw.items[port.FeedPlotDirectory][0].EntityKey = ids.New().String()
	c.gw.mu.Unlock()
	if _, err := c.svc.SyncDirectoryFeeds(context.Background(), ids.New()); !errors.Is(err, port.ErrDirectoryUnavailable) {
		t.Fatalf("bản ghi không khớp entity_key → lỗi: %v", err)
	}
	if st := c.repo.st[port.FeedPlotDirectory]; st.Cursor != "" || len(c.repo.recs[port.FeedPlotDirectory]) != 0 {
		t.Fatalf("không áp, cursor không tiến: %+v", st)
	}
}

// Đối soát hai nguồn: khớp, feed thiếu, bản sao cũ ghi không có, lệch trường.
func TestCompareDirectorySources(t *testing.T) {
	c := newFeedCase()
	tid := ids.New()
	ctx := context.Background()
	if cmp, err := c.svc.CompareDirectorySources(ctx, tid); err != nil || !cmp.Skipped {
		t.Fatalf("feed chưa đọc tới đầu → bỏ qua: %+v %v", cmp, err)
	}
	if _, err := c.svc.SyncDirectoryFeeds(ctx, tid); err != nil {
		t.Fatal(err)
	}
	now, h := c.clk.Now(), c.htx
	other := ids.New()
	c.repo.lh = []store.ReplicaHTX{{Ref: c.htx, Present: true, CommuneCode: "31117", Active: true, AsOf: now}}
	c.repo.lp = []store.ReplicaPlot{
		{Ref: c.plts[0], Present: true, CooperativeRef: &h, CommuneCode: "31117", Status: "active", AsOf: now},
		{Ref: c.plts[1], Present: true, CooperativeRef: &h, CommuneCode: "31117", Status: "retired", AsOf: now},
		{Ref: c.plts[2], Present: false, AsOf: now},
		{Ref: other, Present: true, CooperativeRef: &h, CommuneCode: "31117", Status: "active", AsOf: now},
	}
	cmp, err := c.svc.CompareDirectorySources(ctx, tid)
	if err != nil {
		t.Fatal(err)
	}
	if cmp.Skipped || cmp.Compared != 5 || cmp.Matched != 2 || cmp.Mismatch != 1 || cmp.LegacyAbsent != 1 || cmp.FeedMissing != 1 || len(cmp.Diffs) != 3 {
		t.Fatalf("đối soát: %+v", cmp)
	}
	for _, d := range cmp.Diffs {
		if d.Ref == c.plts[1] && (d.Kind != "mismatch" || d.Field != "status") {
			t.Fatalf("lệch trạng thái thửa: %+v", d)
		}
	}
}
