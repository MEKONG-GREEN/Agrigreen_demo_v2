package app

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/fieldops/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/fieldops/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/fieldops/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// memReplica là bản sao danh mục trong bộ nhớ (thay bảng mkg_fieldops.directory_*_replica).
type memReplica struct {
	mu    sync.Mutex
	htx   map[uuid.UUID]store.ReplicaHTX
	plots map[uuid.UUID]store.ReplicaPlot
	saves int
}

func newMemReplica() *memReplica {
	return &memReplica{htx: map[uuid.UUID]store.ReplicaHTX{}, plots: map[uuid.UUID]store.ReplicaPlot{}}
}

func (m *memReplica) load(_ context.Context, _, htx uuid.UUID, plots []uuid.UUID) (dirSnapshot, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	snap := dirSnapshot{plots: map[uuid.UUID]store.ReplicaPlot{}}
	if h, ok := m.htx[htx]; ok {
		snap.htx = &h
	}
	for _, p := range plots {
		if r, ok := m.plots[p]; ok {
			snap.plots[p] = r
		}
	}
	return snap, nil
}

func (m *memReplica) save(_ context.Context, _ uuid.UUID, htx []store.ReplicaHTX, plots []store.ReplicaPlot) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.saves++
	for _, h := range htx {
		m.htx[h.Ref] = h
	}
	for _, p := range plots {
		m.plots[p.Ref] = p
	}
	return nil
}

func (m *memReplica) refs(context.Context, uuid.UUID) (htx, plots []uuid.UUID, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for k := range m.htx {
		htx = append(htx, k)
	}
	for k := range m.plots {
		plots = append(plots, k)
	}
	return htx, plots, nil
}

// fakeDir là PlatformDirectory giả đếm số lời gọi; delay bỏ qua ctx (bộ điều hợp không tôn trọng hạn).
type fakeDir struct {
	mu    sync.Mutex
	htx   map[uuid.UUID]port.HTX
	plots map[uuid.UUID]port.Plot
	down  bool
	delay time.Duration
	calls int
}

func newFakeDir() *fakeDir {
	return &fakeDir{htx: map[uuid.UUID]port.HTX{}, plots: map[uuid.UUID]port.Plot{}}
}

func (d *fakeDir) enter() (bool, time.Duration) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.calls++
	return d.down, d.delay
}

func (d *fakeDir) Cooperative(_ context.Context, ref uuid.UUID) (port.HTX, error) {
	down, delay := d.enter()
	time.Sleep(delay)
	if down {
		return port.HTX{}, port.ErrDirectoryUnavailable
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	h, ok := d.htx[ref]
	if !ok {
		return port.HTX{}, port.ErrDirectoryNotFound
	}
	return h, nil
}

func (d *fakeDir) Plot(_ context.Context, ref uuid.UUID) (port.Plot, error) {
	down, delay := d.enter()
	time.Sleep(delay)
	if down {
		return port.Plot{}, port.ErrDirectoryUnavailable
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	p, ok := d.plots[ref]
	if !ok {
		return port.Plot{}, port.ErrDirectoryNotFound
	}
	return p, nil
}

func (d *fakeDir) callCount() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.calls
}

type dirCase struct {
	svc  *Service
	rep  *memReplica
	dir  *fakeDir
	clk  *clock.Fixed
	htx  uuid.UUID
	plts []uuid.UUID
}

// newDirCase dựng Service với bản sao có HTX + 2 thửa (as_of = now) và nền tảng giả có cùng dữ liệu.
func newDirCase() *dirCase {
	clk := clock.NewFixed(time.Date(2026, 10, 5, 3, 0, 0, 0, time.UTC))
	c := &dirCase{rep: newMemReplica(), dir: newFakeDir(), clk: clk, htx: ids.New(), plts: []uuid.UUID{ids.New(), ids.New()}}
	c.svc = &Service{Clock: clk, Directory: c.dir, replica: c.rep}
	now := clk.Now()
	c.rep.htx[c.htx] = store.ReplicaHTX{Ref: c.htx, Present: true, CommuneCode: "31117", Active: true, AsOf: now}
	c.dir.htx[c.htx] = port.HTX{Ref: c.htx, CommuneCode: "31117", Active: true}
	for _, p := range c.plts {
		h := c.htx
		c.rep.plots[p] = store.ReplicaPlot{Ref: p, Present: true, CooperativeRef: &h, CommuneCode: "31117", Status: "active", AsOf: now}
		c.dir.plots[p] = port.Plot{Ref: p, CooperativeRef: &h, CommuneCode: "31117", Status: "active"}
	}
	return c
}

func (c *dirCase) input(plots ...uuid.UUID) PlanInput {
	if plots == nil {
		plots = c.plts
	}
	return PlanInput{HTXRef: c.htx, WindowFrom: "2026-10-05", WindowTo: "2026-10-10", PlotRefs: plots, IdemKey: "k"}
}

func codeOf(err error) string {
	var p *problem.Problem
	if errors.As(err, &p) {
		return p.Code
	}
	return ""
}

func TestValidatePlanReadsReplicaWithoutPlatform(t *testing.T) {
	c := newDirCase()
	c.dir.down = true
	pl, err := c.svc.validatePlan(context.Background(), ids.New(), c.input())
	if err != nil {
		t.Fatal(err)
	}
	if pl.commune != "31117" || len(pl.warnings) != 0 || pl.asOf == nil || !pl.asOf.Equal(c.clk.Now()) {
		t.Fatalf("kế hoạch: %+v", pl)
	}
	if n := c.dir.callCount(); n != 0 {
		t.Fatalf("mục đã có trong bản sao không được gọi nền tảng (R-INT-02): %d lời gọi", n)
	}
}

func TestValidatePlanStaleReplicaFlags(t *testing.T) {
	c := newDirCase()
	old := c.clk.Now()
	c.clk.Advance(DefaultDirectoryMaxAge + time.Minute)
	pl, err := c.svc.validatePlan(context.Background(), ids.New(), c.input())
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(pl.warnings, []string{WarnDirectoryStale}) || pl.asOf == nil || !pl.asOf.Equal(old) {
		t.Fatalf("bản sao cũ phải gắn cờ, không chặn: %+v", pl)
	}
	if c.dir.callCount() != 0 {
		t.Fatal("mục cũ không gọi nền tảng — chỉ gắn cờ")
	}
	// max_age cấu hình được.
	c.svc.DirectoryMaxAge = 24 * time.Hour
	if pl, err = c.svc.validatePlan(context.Background(), ids.New(), c.input()); err != nil || len(pl.warnings) != 0 {
		t.Fatalf("max_age 24h: %+v %v", pl, err)
	}
}

func TestValidatePlanMissingEntry(t *testing.T) {
	c := newDirCase()
	fresh := ids.New()
	h := c.htx
	c.dir.plots[fresh] = port.Plot{Ref: fresh, CooperativeRef: &h, CommuneCode: "31117", Status: "active"}

	// Nền tảng ngừng → lập được, gắn cờ chưa đối chiếu; không ghi gì vào bản sao.
	c.dir.down = true
	pl, err := c.svc.validatePlan(context.Background(), ids.New(), c.input(c.plts[0], fresh))
	if err != nil || !slices.Equal(pl.warnings, []string{WarnDirectoryUnverified}) {
		t.Fatalf("nền tảng ngừng: %+v %v", pl, err)
	}
	if _, ok := c.rep.plots[fresh]; ok {
		t.Fatal("tra không được thì không ghi bản sao")
	}
	// Nền tảng chạy → đúng MỘT lời gọi cho mục thiếu, kết quả ghi vào bản sao, không cảnh báo.
	c.dir.down = false
	before := c.dir.callCount()
	pl, err = c.svc.validatePlan(context.Background(), ids.New(), c.input(c.plts[0], fresh))
	if err != nil || len(pl.warnings) != 0 {
		t.Fatalf("tra dự phòng: %+v %v", pl, err)
	}
	if n := c.dir.callCount() - before; n != 1 {
		t.Fatalf("chỉ tra mục thiếu: %d lời gọi", n)
	}
	if r, ok := c.rep.plots[fresh]; !ok || !r.Present {
		t.Fatalf("kết quả tra phải vào bản sao: %+v", r)
	}
	// Tắt tra trực tiếp → chỉ bản sao.
	c.svc.DirectoryLookupBudget = -1
	other := ids.New()
	before = c.dir.callCount()
	if pl, err = c.svc.validatePlan(context.Background(), ids.New(), c.input(other)); err != nil ||
		!slices.Equal(pl.warnings, []string{WarnDirectoryUnverified}) || c.dir.callCount() != before {
		t.Fatalf("chỉ bản sao: %+v %v", pl, err)
	}
}

func TestValidatePlanMissingHTXNeedsCommune(t *testing.T) {
	c := newDirCase()
	c.dir.down = true
	in := c.input()
	in.HTXRef = ids.New()
	if _, err := c.svc.validatePlan(context.Background(), ids.New(), in); codeOf(err) != problem.Validation.Code {
		t.Fatalf("HTX chưa đối chiếu, thiếu mã xã: %v", err)
	}
	in.CommuneCode = "31118"
	// Thửa trong bản sao thuộc HTX khác → vẫn chặn (bằng chứng khẳng định).
	if _, err := c.svc.validatePlan(context.Background(), ids.New(), in); codeOf(err) != ErrPlotOtherHTX.Code {
		t.Fatalf("thửa thuộc HTX khác: %v", err)
	}
	in.PlotRefs = []uuid.UUID{ids.New()}
	pl, err := c.svc.validatePlan(context.Background(), ids.New(), in)
	if err != nil || pl.commune != "31118" || !slices.Equal(pl.warnings, []string{WarnDirectoryUnverified}) {
		t.Fatalf("HTX chưa đối chiếu, có mã xã: %+v %v", pl, err)
	}
}

func TestValidatePlanPositiveEvidenceRejects(t *testing.T) {
	ctx := context.Background()
	t.Run("thửa không có trong bản sao, nền tảng ngừng", func(t *testing.T) {
		c := newDirCase()
		c.rep.plots[c.plts[1]] = store.ReplicaPlot{Ref: c.plts[1], AsOf: c.clk.Now()}
		c.dir.down = true
		if _, err := c.svc.validatePlan(ctx, ids.New(), c.input()); codeOf(err) != ErrPlotNotFound.Code {
			t.Fatalf("muốn plot_not_found: %v", err)
		}
		// Nền tảng trở lại và đã chia sẻ thửa → tra lại, cho lập.
		c.dir.down = false
		if _, err := c.svc.validatePlan(ctx, ids.New(), c.input()); err != nil {
			t.Fatalf("tra lại mục 'không có': %v", err)
		}
	})
	t.Run("thửa đã ngừng dùng", func(t *testing.T) {
		c := newDirCase()
		p := c.rep.plots[c.plts[0]]
		p.Status = "retired"
		c.rep.plots[c.plts[0]] = p
		if _, err := c.svc.validatePlan(ctx, ids.New(), c.input()); codeOf(err) != ErrPlotOtherHTX.Code {
			t.Fatalf("muốn plot_other_htx: %v", err)
		}
	})
	t.Run("HTX vô hiệu", func(t *testing.T) {
		c := newDirCase()
		h := c.rep.htx[c.htx]
		h.Active = false
		c.rep.htx[c.htx] = h
		if _, err := c.svc.validatePlan(ctx, ids.New(), c.input()); codeOf(err) != ErrHTXNotFound.Code {
			t.Fatalf("muốn htx_not_found: %v", err)
		}
	})
	t.Run("chưa cấu hình nguồn", func(t *testing.T) {
		c := newDirCase()
		c.svc.DirectoryUnconfigured = true
		if _, err := c.svc.validatePlan(ctx, ids.New(), c.input()); codeOf(err) != ErrDirectory.Code {
			t.Fatalf("muốn 503: %v", err)
		}
	})
}

// Tra dự phòng có chặn trên thời gian kể cả khi bộ điều hợp treo và bỏ qua ctx.
func TestValidatePlanLookupBounded(t *testing.T) {
	c := newDirCase()
	c.dir.delay = 2 * time.Second
	c.svc.DirectoryLookupBudget = 100 * time.Millisecond
	var plots []uuid.UUID
	for range 50 {
		plots = append(plots, ids.New())
	}
	start := time.Now()
	pl, err := c.svc.validatePlan(context.Background(), ids.New(), c.input(plots...))
	if d := time.Since(start); d > time.Second {
		t.Fatalf("lập lệnh gom phải bị chặn thời gian: %v", d)
	}
	if err != nil || !slices.Equal(pl.warnings, []string{WarnDirectoryUnverified}) {
		t.Fatalf("hết ngân sách → gắn cờ: %+v %v", pl, err)
	}
}

func TestRefreshDirectoryKeepsRowsWhenUnavailable(t *testing.T) {
	c := newDirCase()
	ctx := context.Background()
	gone := ids.New()
	c.rep.plots[gone] = store.ReplicaPlot{Ref: gone, Present: true, AsOf: c.clk.Now()}
	c.clk.Advance(time.Hour)
	r, err := c.svc.RefreshDirectory(ctx, ids.New())
	if err != nil || r.HTX != 1 || r.Plots != 3 || r.Present != 3 || r.Absent != 1 || r.Unavailable != 0 {
		t.Fatalf("làm mới: %+v %v", r, err)
	}
	if p := c.rep.plots[gone]; p.Present || !p.AsOf.Equal(c.clk.Now()) {
		t.Fatalf("thửa thu hồi chia sẻ → không có: %+v", p)
	}
	asOf := c.rep.plots[c.plts[0]].AsOf
	c.dir.down = true
	c.clk.Advance(time.Hour)
	if r, err := c.svc.RefreshDirectory(ctx, ids.New()); !errors.Is(err, ErrDirectoryRefresh) || r.Unavailable != 4 {
		t.Fatalf("nền tảng ngừng: %+v %v", r, err)
	}
	if !c.rep.plots[c.plts[0]].AsOf.Equal(asOf) {
		t.Fatal("tra không được thì giữ dòng cũ (as_of không đổi)")
	}
	c.svc.DirectoryUnconfigured = true
	if r, err := c.svc.RefreshDirectory(ctx, ids.New()); err != nil || r.HTX != 0 {
		t.Fatalf("chưa cấu hình: %+v %v", r, err)
	}
}

// slowHarvest là HarvestCalendar treo, bỏ qua ctx.
type slowHarvest struct{ delay time.Duration }

func (h slowHarvest) PlotSeasons(context.Context, uuid.UUID) ([]port.SeasonDates, error) {
	time.Sleep(h.delay)
	return nil, port.ErrDirectoryNotFound
}

// Ngày gặt (FM-01/02) ngoài giao dịch: tổng thời gian chờ bị chặn, thửa chưa có kết quả → unavailable.
func TestFetchPlotsBounded(t *testing.T) {
	clk := clock.NewFixed(time.Date(2026, 10, 5, 3, 0, 0, 0, time.UTC))
	s := &Service{Clock: clk, Harvest: slowHarvest{delay: 2 * time.Second}}
	var plots []uuid.UUID
	for range 20 {
		plots = append(plots, ids.New())
	}
	start := time.Now()
	hs := s.fetchPlotsWithin(context.Background(), store.Order{OrderID: ids.New(), WindowFrom: clk.Now(), PlotRefs: plots}, 100*time.Millisecond)
	if d := time.Since(start); d > time.Second {
		t.Fatalf("phải bị chặn thời gian: %v", d)
	}
	if len(hs) != len(plots) {
		t.Fatalf("mỗi thửa một kết quả: %d", len(hs))
	}
	for i, h := range hs {
		if h.Check != domain.HarvestUnavailable || h.PlotRef != plots[i] {
			t.Fatalf("thửa %d: %+v", i, h)
		}
	}
	// Nhanh → có kết quả thật (ở đây nền tảng nói không có → unknown).
	//
	// Service MỚI thay vì ghi đè s.Harvest: tám goroutine của lần gọi trên bị runBounded
	// BỎ LẠI (ngân sách 100ms hết trong khi slowHarvest còn ngủ 2s — bỏ lời gọi treo là
	// hành vi CỐ Ý của runBounded, xem directory.go). Chúng đã đọc s.Harvest trong
	// fetchHarvest mà không có cạnh đồng bộ nào với goroutine test, nên ghi vào s.Harvest
	// ở đây là một cuộc đua dữ liệu thật — go test -race báo đúng. Chỗ phải sửa là test.
	s2 := &Service{Clock: clk, Harvest: slowHarvest{}}
	hs = s2.fetchPlotsWithin(context.Background(), store.Order{OrderID: ids.New(), WindowFrom: clk.Now(), PlotRefs: plots}, time.Second)
	for _, h := range hs {
		if h.Check != domain.HarvestUnknown {
			t.Fatalf("muốn unknown: %+v", h)
		}
	}
}
