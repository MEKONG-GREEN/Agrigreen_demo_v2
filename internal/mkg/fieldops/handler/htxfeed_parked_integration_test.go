package handler_test

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/fieldops/adapter"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/fieldops/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/fieldops/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/fieldops/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// Mục hỏng (poison) trong luồng phản hồi HTX: ghi lỗi VĨNH VIỄN (ở đây: lý do chứa ký tự NUL — PostgreSQL
// từ chối 22021) → gác vào htx_feed_parked kèm lỗi, con trỏ vẫn tiến, mục sau vẫn được ghi; lượt sau và sau
// khởi động lại không đọc / gác lại mục đó. Lỗi tạm thời (nền tảng ngừng) giữ hành vi cũ.
func TestHTXFeedPoisonItemParked(t *testing.T) {
	f := newFixture(t, nil)
	feed := &adapter.FakeHTXFeed{}
	f.svc.Feed = feed
	id := f.approvedOrder()
	col, p := f.collector(), f.plots[0]
	for _, st := range []string{port.StageBale, port.StageGather} {
		mustStatus(t, f.count(col, id, p, st, 120, nil), http.StatusCreated, st)
	}
	bl := f.count(col, id, p, port.StageBoatLoad, 120, nil)
	mustStatus(t, bl, http.StatusCreated, "boat_load")
	stmt := uuid.MustParse(str(bl.body, "reconciliation", "statement_ref"))
	base := port.HTXResponse{StatementRef: stmt, StatementRevision: 1, HTXRef: f.htx, PlotRef: p, MKGRolls: 120,
		RespondedAt: f.clk.Now().Add(-time.Minute)}
	poison := base
	bad := "HTX đếm được\x00 118 cuộn trên ghe"
	poison.ResponseID, poison.Outcome, poison.HTXRolls, poison.Reason = ids.New(), "disputed", 118, &bad
	feed.Add(poison)
	ok := base
	ok.ResponseID, ok.Outcome, ok.HTXRolls = ids.New(), "confirmed", 120
	feed.Add(ok)

	ctx := context.Background()
	r, err := f.svc.SyncHTXResponses(ctx, f.tenant)
	if err != nil || r.Parked != 1 || r.Recorded != 1 || r.Cursor != 2 || !strings.HasPrefix(r.LastParked, "sqlstate.22") {
		t.Fatalf("lượt 1 phải gác mục hỏng và đi tiếp: %+v %v", r, err)
	}
	if f.svc.FeedParkedTotal() != 1 {
		t.Fatalf("metric gác: %d", f.svc.FeedParkedTotal())
	}
	if err := tenant.WithTenantTx(ctx, f.pool, f.tenant, func(tx pgx.Tx) error {
		xs, err := store.ParkedResponses(ctx, tx, 0, 10)
		if err != nil {
			return err
		}
		if len(xs) != 1 || xs[0].ResponseID != poison.ResponseID || xs[0].Cursor != 1 || xs[0].Code != r.LastParked ||
			xs[0].Detail == "" || len(xs[0].Payload) == 0 {
			t.Fatalf("htx_feed_parked: %+v", xs)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	// Lượt sau: không đọc lại mục đã gác.
	if r, err := f.svc.SyncHTXResponses(ctx, f.tenant); err != nil || r.Parked != 0 || r.Recorded != 0 {
		t.Fatalf("lượt 2: %+v %v", r, err)
	}
	// Khởi động lại (con trỏ dựng từ sổ + bảng gác): không gác trùng.
	f.svc = &app.Service{DB: f.svc.DB, Clock: f.svc.Clock, Directory: f.svc.Directory, CursorKey: f.svc.CursorKey, Feed: feed}
	if r, err := f.svc.SyncHTXResponses(ctx, f.tenant); err != nil || r.Parked != 0 || r.Duplicate != 0 || r.Cursor != 2 {
		t.Fatalf("sau khởi động lại: %+v %v", r, err)
	}
	// Mục hỏng là mục CUỐI của luồng: con trỏ dựng lại từ bảng gác (không có xác nhận nào sau nó).
	last := base
	last.ResponseID, last.Outcome, last.HTXRolls, last.Reason = ids.New(), "disputed", 117, &bad
	feed.Add(last)
	if r, err := f.svc.SyncHTXResponses(ctx, f.tenant); err != nil || r.Parked != 1 || r.Cursor != 3 {
		t.Fatalf("gác mục cuối: %+v %v", r, err)
	}
	f.svc = &app.Service{DB: f.svc.DB, Clock: f.svc.Clock, Directory: f.svc.Directory, CursorKey: f.svc.CursorKey, Feed: feed}
	if r, err := f.svc.SyncHTXResponses(ctx, f.tenant); err != nil || r.Parked != 0 || r.Cursor != 3 {
		t.Fatalf("con trỏ dựng từ bảng gác: %+v %v", r, err)
	}
	// Lỗi tạm thời: báo lỗi, không gác.
	feed.Down = true
	if r, err := f.svc.SyncHTXResponses(ctx, f.tenant); err == nil || r.Parked != 0 {
		t.Fatalf("nền tảng ngừng phải báo lỗi, không gác: %+v %v", r, err)
	}
}
