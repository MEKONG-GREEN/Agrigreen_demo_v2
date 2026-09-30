package handler_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/fieldops/adapter"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/fieldops/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/fieldops/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
)

// RB-6 chiều mua (G3-2): HTX xác nhận / tranh chấp trên nền tảng, job leader của api-internal
// kéo luồng phản hồi (Application API công khai, tài khoản dịch vụ tenant MKG) và ghi
// htx_confirmation kênh platform, cập nhật đối soát. Kéo lặp / khởi động lại (con trỏ dựng từ
// sổ) không ghi trùng; phản hồi theo revision cũ, bảng kê lạ, sai quy tắc bị bỏ qua.
func TestHTXResponsesFromPlatform_RB6_BuySide(t *testing.T) {
	f := newFixture(t, nil)
	feed := &adapter.FakeHTXFeed{}
	f.svc.Feed = feed
	id := f.approvedOrder()
	col, sup, p := f.collector(), f.supervisor(), f.plots[0]
	for _, st := range []string{port.StageBale, port.StageGather} {
		mustStatus(t, f.count(col, id, p, st, 120, nil), http.StatusCreated, st)
	}
	bl := f.count(col, id, p, port.StageBoatLoad, 120, nil)
	mustStatus(t, bl, http.StatusCreated, "boat_load")
	stmt := uuid.MustParse(str(bl.body, "reconciliation", "statement_ref"))
	at := f.clk.Now().Add(-time.Minute)
	reason := "HTX đếm được 118 cuộn trên ghe"
	base := port.HTXResponse{StatementRef: stmt, StatementRevision: 1, HTXRef: f.htx, PlotRef: p, MKGRolls: 120, RespondedAt: at}
	confirmed := base
	confirmed.ResponseID, confirmed.Outcome, confirmed.HTXRolls = ids.New(), "confirmed", 120
	feed.Add(confirmed)
	unknown := base
	unknown.ResponseID, unknown.StatementRef, unknown.Outcome, unknown.HTXRolls = ids.New(), ids.New(), "confirmed", 120
	feed.Add(unknown) // bảng kê không do tenant này gửi
	bad := base
	bad.ResponseID, bad.Outcome, bad.HTXRolls = ids.New(), "disputed", 110 // tranh chấp thiếu lý do
	feed.Add(bad)

	ctx := context.Background()
	r, err := f.svc.SyncHTXResponses(ctx, f.tenant)
	if err != nil || r.Recorded != 1 || r.Skipped != 2 || r.Cursor != 3 {
		t.Fatalf("lượt 1: %+v %v", r, err)
	}
	rec := sup.get("/v1/fieldops/reconciliation?order_id=" + id)
	items := arr(rec.body, "items")
	if len(items) != 1 || str(items[0], "status") != "match" {
		t.Fatalf("đối soát khớp: %s", rec.raw)
	}
	cf := sup.get("/v1/fieldops/orders/" + id + "/htx-confirmations")
	cs := arr(cf.body, "items")
	if len(cs) != 1 || str(cs[0], "channel") != app.ChannelPlatform || str(cs[0], "evidence_ref") != confirmed.ResponseID.String() {
		t.Fatalf("xác nhận kênh platform: %s", cf.raw)
	}

	// Lượt sau không có gì mới; khởi động lại (con trỏ trong bộ nhớ mất) → dựng con trỏ từ sổ, không ghi trùng.
	if r, err := f.svc.SyncHTXResponses(ctx, f.tenant); err != nil || r.Recorded != 0 {
		t.Fatalf("lượt 2: %+v %v", r, err)
	}
	f.svc = &app.Service{DB: f.svc.DB, Clock: f.svc.Clock, Directory: f.svc.Directory, CursorKey: f.svc.CursorKey, Feed: feed}
	if r, err := f.svc.SyncHTXResponses(ctx, f.tenant); err != nil || r.Recorded != 0 || r.Duplicate != 0 {
		t.Fatalf("sau khởi động lại chỉ đọc sau con trỏ đã ghi: %+v %v", r, err)
	}

	// MKG sửa số (revision 2, 118 cuộn); phản hồi muộn theo revision 1 bị bỏ qua, theo revision 2 được ghi.
	mustStatus(t, col.post("/v1/fieldops/roll-counts/"+str(bl.body, "roll_count", "id")+"/corrections", map[string]any{
		"rolls": 118, "discrepancy_reason": "hai cuộn rơi xuống kênh", "correction_reason": "đếm lại khi cập bến", "event_time": f.ts(0)}),
		http.StatusCreated, "sửa boat_load")
	stale := base
	stale.ResponseID, stale.Outcome, stale.HTXRolls = ids.New(), "confirmed", 120
	feed.Add(stale)
	disputed := base
	disputed.ResponseID, disputed.StatementRevision, disputed.MKGRolls = ids.New(), 2, 118
	disputed.Outcome, disputed.HTXRolls, disputed.Reason = "disputed", 120, &reason
	feed.Add(disputed)
	r, err = f.svc.SyncHTXResponses(ctx, f.tenant)
	if err != nil || r.Recorded != 1 || r.Skipped != 1 {
		t.Fatalf("lượt 3: %+v %v", r, err)
	}
	rec = sup.get("/v1/fieldops/reconciliation?order_id=" + id)
	if items := arr(rec.body, "items"); str(items[0], "status") != "mismatch" || str(items[0], "htx_rolls") != "120" ||
		str(items[0], "confirmed_revision") != "2" {
		t.Fatalf("đối soát lệch theo tranh chấp: %s", rec.raw)
	}

	// Nền tảng không gọi được → lỗi 503, con trỏ giữ nguyên.
	feed.Down = true
	if _, err := f.svc.SyncHTXResponses(ctx, f.tenant); err == nil {
		t.Fatal("nền tảng không gọi được phải báo lỗi")
	}
	// Tenant khác không có bảng kê → mọi phản hồi bị bỏ qua (RB-2).
	feed.Down = false
	if r, err := f.svc.SyncHTXResponses(ctx, ids.New()); err != nil || r.Recorded != 0 {
		t.Fatalf("tenant khác: %+v %v", r, err)
	}
}
