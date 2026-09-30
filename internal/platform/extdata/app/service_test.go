package app_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extdata/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extdata/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extdata/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extdata/provider"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

func caller(t *testing.T, roles ...string) context.Context {
	t.Helper()
	tid := uuid.New()
	ctx := authz.WithPrincipal(context.Background(), authz.Principal{ActorRef: uuid.New(), TenantID: tid, Roles: roles, OrgRef: uuid.New()})
	ctx = authz.WithRawToken(ctx, "eyJ.token-thật.của-người-gọi")
	return tenant.WithID(ctx, tid)
}

func newSvc(t *testing.T, clk clock.Clock) (*app.Service, *provider.Fake, *provider.Fake) {
	t.Helper()
	wx, tiles := &provider.Fake{}, &provider.Fake{}
	svc, err := app.NewService(clk, nil, []app.Source{
		{Info: port.SourceInfo{ID: "wx", Kind: port.KindWeather, CellDeg: domain.DefaultCellDeg, TTL: 30 * time.Minute}, Point: wx},
		{Info: port.SourceInfo{ID: "sat", Kind: port.KindTile, MaxZoom: 16, TTL: 24 * time.Hour}, Tile: tiles},
	}, 16)
	if err != nil {
		t.Fatal(err)
	}
	return svc, wx, tiles
}

func code(err error) string {
	var p *problem.Problem
	if errors.As(err, &p) {
		return p.Code
	}
	return ""
}

// ADR-0007 (RB-7): nhà cung cấp chỉ thấy ô lưới thô — không thấy tọa độ gốc, người gọi,
// token hay tenant; hai người khác nhau cùng ô dùng chung một lần gọi ra ngoài.
func TestProviderSeesOnlyCoarseCellNoIdentity_ADR0007(t *testing.T) {
	clk := clock.NewFixed(time.Date(2026, 9, 30, 1, 0, 0, 0, time.UTC))
	svc, wx, _ := newSvc(t, clk)

	a, err := svc.Point(caller(t, "farmer"), "wx", "10.0452", "105.7701")
	if err != nil {
		t.Fatal(err)
	}
	if a.Cached || !a.Cell.Lat.Equal(decimal.RequireFromString("10.05")) || !a.Cell.Lon.Equal(decimal.RequireFromString("105.75")) {
		t.Fatalf("ô trả về = %+v", a.Cell)
	}
	b, err := svc.Point(caller(t, "kn_officer"), "wx", "10.0911", "105.7123")
	if err != nil {
		t.Fatal(err)
	}
	if !b.Cached {
		t.Fatal("người thứ hai cùng ô phải lấy từ bộ nhớ đệm")
	}
	if n, _ := wx.Calls(); n != 1 {
		t.Fatalf("số lần gọi nhà cung cấp = %d, muốn 1", n)
	}
	q := wx.Points[0]
	if q.Source != "wx" || !q.Cell.Lat.Equal(decimal.RequireFromString("10.05")) || !q.Cell.SizeDeg.Equal(domain.DefaultCellDeg) {
		t.Fatalf("truy vấn gửi nhà cung cấp = %+v", q)
	}
	if wx.Identified {
		t.Fatal("context gửi nhà cung cấp còn mang Principal/token/tenant")
	}
}

// ADR-0007: hết hạn đệm → gọi lại; nhà cung cấp lỗi → 502 extdata.provider_unavailable.
func TestCacheTTLAndProviderFailure_ADR0007(t *testing.T) {
	clk := clock.NewFixed(time.Date(2026, 9, 30, 1, 0, 0, 0, time.UTC))
	svc, wx, _ := newSvc(t, clk)
	ctx := caller(t)
	if _, err := svc.Point(ctx, "wx", "10.05", "105.75"); err != nil {
		t.Fatal(err)
	}
	clk.Advance(31 * time.Minute)
	wx.Fail = errors.New("hỏng")
	if _, err := svc.Point(ctx, "wx", "10.05", "105.75"); code(err) != "extdata.provider_unavailable" {
		t.Fatalf("muốn extdata.provider_unavailable, được %v", err)
	}
	if n, _ := wx.Calls(); n != 2 {
		t.Fatalf("hết hạn đệm phải gọi lại nhà cung cấp: %d lần", n)
	}
}

// ADR-0007: danh mục mặc định chặn — nguồn không bật, sai loại → 404; ngoài vùng → 422;
// chưa xác thực → 401; tọa độ sai dạng → validation_failed.
func TestSourceAllowlistAndTerritory_ADR0007(t *testing.T) {
	svc, wx, tiles := newSvc(t, clock.NewFixed(time.Date(2026, 9, 30, 1, 0, 0, 0, time.UTC)))
	ctx := caller(t)
	cases := []struct {
		name string
		err  error
		want string
	}{
		{"nguồn chưa bật", func() error { _, e := svc.Point(ctx, "openweather", "10", "105"); return e }(), "extdata.unknown_source"},
		{"nguồn tile gọi dạng điểm", func() error { _, e := svc.Point(ctx, "sat", "10", "105"); return e }(), "extdata.unknown_source"},
		{"nguồn điểm gọi dạng tile", func() error { _, e := svc.Tile(ctx, "wx", 10, 812, 483); return e }(), "extdata.unknown_source"},
		{"ngoài vùng phục vụ", func() error { _, e := svc.Point(ctx, "wx", "48.85", "2.35"); return e }(), "extdata.outside_territory"},
		{"tọa độ sai dạng", func() error { _, e := svc.Point(ctx, "wx", "mười", "105"); return e }(), "validation_failed"},
		{"zoom vượt trần", func() error { _, e := svc.Tile(ctx, "sat", 17, 104000, 61800); return e }(), "extdata.zoom_too_high"},
		{"tile ngoài vùng", func() error { _, e := svc.Tile(ctx, "sat", 10, 0, 0); return e }(), "extdata.outside_territory"},
		{"chưa xác thực", func() error { _, e := svc.Point(context.Background(), "wx", "10", "105"); return e }(), "unauthenticated"},
	}
	for _, c := range cases {
		if got := code(c.err); got != c.want {
			t.Errorf("%s: mã = %q, muốn %q (%v)", c.name, got, c.want, c.err)
		}
	}
	if p, tl := wx.Calls(); p != 0 {
		t.Errorf("không được gọi nhà cung cấp điểm khi bị chặn: %d", p)
	} else if _, tl2 := tiles.Calls(); tl != 0 || tl2 != 0 {
		t.Errorf("không được gọi nhà cung cấp tile khi bị chặn")
	}
}

// ADR-0007: tile qua proxy — đệm theo z/x/y, không định danh.
func TestTileProxyCached_ADR0007(t *testing.T) {
	svc, _, tiles := newSvc(t, clock.NewFixed(time.Date(2026, 9, 30, 1, 0, 0, 0, time.UTC)))
	for i := 0; i < 3; i++ {
		a, err := svc.Tile(caller(t), "sat", 10, 812, 483)
		if err != nil {
			t.Fatal(err)
		}
		if a.Tile.ContentType != "image/png" || len(a.Tile.Body) == 0 || a.Cached != (i > 0) {
			t.Fatalf("lần %d: %+v", i, a)
		}
	}
	if _, n := tiles.Calls(); n != 1 || tiles.Identified {
		t.Fatalf("gọi tile %d lần, định danh=%v", n, tiles.Identified)
	}
}

// Cache LRU bỏ mục ít dùng nhất khi đầy.
func TestCacheEvictsLeastRecentlyUsed(t *testing.T) {
	c := app.NewCache(2)
	now := time.Now()
	exp := now.Add(time.Hour)
	c.Put("a", app.Entry{Expires: exp})
	c.Put("b", app.Entry{Expires: exp})
	c.Get("a", now)
	c.Put("c", app.Entry{Expires: exp})
	if _, ok := c.Get("b", now); ok || c.Len() != 2 {
		t.Fatalf("b phải bị bỏ; len=%d", c.Len())
	}
	if _, ok := c.Get("a", now); !ok {
		t.Fatal("a vừa dùng phải còn")
	}
}

// Detached giữ hủy nhưng không giữ giá trị.
func TestDetachedDropsValuesKeepsCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(caller(t))
	d := app.Detached(ctx)
	if _, ok := authz.FromContext(d); ok {
		t.Fatal("Principal lọt qua Detached")
	}
	if _, ok := authz.RawToken(d); ok {
		t.Fatal("token lọt qua Detached")
	}
	cancel()
	select {
	case <-d.Done():
	case <-time.After(time.Second):
		t.Fatal("Detached không nhận hủy")
	}
}
