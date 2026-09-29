package handler_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	syncmod "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/sync"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/sync/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/sync/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/inbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/outbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

const (
	areaConsumer = "pf.mechanization.area_sync"
	hubConsumer  = "pf.geo.hub_valve"
)

func envFor(tid uuid.UUID, typ string) outbox.Envelope {
	return outbox.Envelope{EventID: ids.New(), TenantID: tid, EventType: typ, SchemaVersion: 1, OccurredAt: time.Now().UTC(),
		Producer: "pf/cropcycle", Payload: json.RawMessage(`{}`)}
}

func items(r resp) map[string]map[string]any {
	out := map[string]map[string]any{}
	for _, it := range r.body["items"].([]any) {
		m := it.(map[string]any)
		out[m["consumer"].(string)] = m
	}
	return out
}

// GIS US-SYNC-01 AC-1 / US-SYNC-02 AC-1: màn giám sát đồng bộ gộp theo consumer (nguồn) lần
// xử lý thành công (shared.inbox) và thất bại (pf_sync.integration_failure): thời điểm, số sự
// kiện, lỗi, số sự kiện vào DLQ, độ sâu DLQ; nhật ký lọc theo nguồn/thời gian/trạng thái. Lần
// giao cuối (vào DLQ) phát pf.sync.integration.dead_lettered ĐÚNG MỘT LẦN cho (sự kiện, consumer);
// không lưu thông điệp lỗi tự do (R-14). Cách ly tenant; chỉ Admin xem được.
func TestIntegrationMonitor_GIS_US_SYNC_01_02(t *testing.T) {
	f := newFixture(t)
	f.mod.UseIntegrations([]string{areaConsumer, hubConsumer}, func(context.Context) (map[string]int64, error) {
		return map[string]int64{areaConsumer + ".dlq": 1, hubConsumer + ".dlq": 0}, nil
	})
	ctx := context.Background()
	ok := func(context.Context, pgx.Tx, outbox.Envelope) error { return nil }
	good := envFor(f.tenant, "pf.cropcycle.coop_season_area.changed")
	if done, err := inbox.Process(ctx, f.pool, areaConsumer, good, ok); err != nil || !done {
		t.Fatalf("inbox: %v %v", done, err)
	}
	rec := syncmod.FailureRecorder(f.pool, f.clk)
	bad := envFor(f.tenant, "pf.cropcycle.coop_season_area.changed")
	secret := errors.New("area_ha 0912345678 không hợp lệ") // thông điệp có thể chứa dữ liệu — không được lưu
	if err := rec(ctx, inbox.Failure{Consumer: areaConsumer, Envelope: bad, Err: secret, DeliveryCount: 1}); err != nil {
		t.Fatal(err)
	}
	final := inbox.Failure{Consumer: areaConsumer, Envelope: bad, Err: problem.Validation, DeliveryCount: inbox.DeliveryLimit, DeadLettered: true}
	for range 2 { // giao lặp lần cuối → vẫn một dòng DLQ, một sự kiện cảnh báo
		if err := rec(ctx, final); err != nil {
			t.Fatal(err)
		}
	}
	var evs []outbox.Envelope
	if err := tenant.WithTenantTx(ctx, f.pool, f.tenant, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT payload FROM shared.outbox WHERE event_type = $1`, port.EventIntegrationDeadLettered)
		if err != nil {
			return err
		}
		raws, err := pgx.CollectRows(rows, pgx.RowTo[[]byte])
		for _, r := range raws {
			evs = append(evs, outbox.Envelope{Payload: r})
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if len(evs) != 1 {
		t.Fatalf("một cảnh báo DLQ cho một (sự kiện, consumer): %d", len(evs))
	}
	var pl port.IntegrationDeadLetteredV1
	if err := json.Unmarshal(evs[0].Payload, &pl); err != nil || pl.EventID != bad.EventID || pl.ErrorCode != "validation_failed" ||
		pl.Source != "App HTX → Bản đồ CGH (diện tích vụ)" {
		t.Fatalf("payload DLQ: %+v %v", pl, err)
	}

	adm := f.as(f.tenant, ids.New(), authz.RolePlatformAdmin)
	r := adm.adminGet("/v1/sync/integrations")
	if r.status != 200 || r.body["dlq_available"] != true {
		t.Fatalf("giám sát: %d %s", r.status, r.raw)
	}
	by := items(r)
	area, hub := by[areaConsumer], by[hubConsumer]
	if area["status"] != port.IntegrationError || area["processed_count"].(float64) != 1 || area["failure_count"].(float64) != 2 ||
		area["dead_lettered_count"].(float64) != 1 || area["dlq_depth"].(float64) != 1 || area["last_error_code"] != "validation_failed" ||
		area["last_processed_at"] == nil || area["source"] != "App HTX → Bản đồ CGH (diện tích vụ)" {
		t.Fatalf("luồng App HTX → CGH: %v", area)
	}
	if hub["status"] != port.IntegrationIdle || hub["processed_count"].(float64) != 0 || hub["dlq_depth"].(float64) != 0 {
		t.Fatalf("luồng chưa có sự kiện: %v", hub)
	}
	if r := adm.adminGet("/v1/sync/integrations?status=idle"); len(r.body["items"].([]any)) != 1 {
		t.Fatalf("lọc trạng thái: %s", r.raw)
	}
	// Nhật ký: lọc trạng thái / nguồn; mã lỗi đã chuẩn hóa, không có thông điệp.
	lg := adm.adminGet("/v1/sync/integrations/log?status=error&consumer=" + areaConsumer)
	if lg.status != 200 || len(lg.body["items"].([]any)) != 2 {
		t.Fatalf("nhật ký lỗi: %d %s", lg.status, lg.raw)
	}
	first := lg.body["items"].([]any)[0].(map[string]any)
	if first["dead_lettered"] != true || first["status"] != "error" || first["record_count"].(float64) != 1 {
		t.Fatalf("dòng mới nhất là lần vào DLQ: %v", first)
	}
	if strings.Contains(string(lg.raw), "0912345678") {
		t.Fatal("nhật ký lộ thông điệp lỗi")
	}
	if lg := adm.adminGet("/v1/sync/integrations/log?status=ok"); len(lg.body["items"].([]any)) != 1 {
		t.Fatalf("nhật ký thành công: %s", lg.raw)
	}
	pg := adm.adminGet("/v1/sync/integrations/log?limit=2")
	if len(pg.body["items"].([]any)) != 2 || pg.body["next_cursor"] == nil {
		t.Fatalf("phân trang: %s", pg.raw)
	}
	if nx := adm.adminGet("/v1/sync/integrations/log?limit=2&cursor=" + pg.body["next_cursor"].(string)); len(nx.body["items"].([]any)) != 1 {
		t.Fatalf("trang sau: %s", nx.raw)
	}
	// Cách ly tenant: tenant khác không thấy sự kiện của tenant này.
	other := f.as(ids.New(), ids.New(), authz.RolePlatformAdmin)
	if o := items(other.adminGet("/v1/sync/integrations")); o[areaConsumer]["processed_count"].(float64) != 0 || o[areaConsumer]["failure_count"].(float64) != 0 {
		t.Fatalf("rò tenant: %v", o[areaConsumer])
	}
	// Không phải Admin → 403.
	if r := f.officer("kn_officer").adminGet("/v1/sync/integrations"); r.status != 403 {
		t.Fatalf("cán bộ thường: %d", r.status)
	}
	// Bảng chỉ ghi thêm.
	if err := tenant.WithTenantTx(ctx, f.pool, f.tenant, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, "DELETE FROM pf_sync.integration_failure")
		return err
	}); err == nil {
		t.Fatal("vai ứng dụng không được xóa nhật ký lỗi")
	}
}

// GIS US-SYNC-02: mã lỗi lưu là mã đã chuẩn hóa (problem / SQLSTATE / timeout), không phải thông điệp.
func TestIntegrationErrorCode_GIS_US_SYNC_02(t *testing.T) {
	for _, tc := range []struct {
		err  error
		want string
	}{
		{problem.New(409, "mechanization.state_conflict", "x"), "mechanization.state_conflict"},
		{&pgconn.PgError{Code: "23505", Message: "khóa trùng: 0912345678"}, "db:23505"},
		{context.DeadlineExceeded, "timeout"},
		{errors.New("SĐT 0912345678 sai"), "internal"},
	} {
		if got := app.ErrorCode(tc.err); got != tc.want {
			t.Errorf("%v → %q, muốn %q", tc.err, got, tc.want)
		}
	}
	if !inbox.FinalAttempt(inbox.DeliveryLimit) || inbox.FinalAttempt(inbox.DeliveryLimit-1) {
		t.Fatal("lần giao cuối = x-delivery-count ≥ x-delivery-limit")
	}
	if inbox.DeliveryCount(map[string]any{"x-delivery-count": int64(3)}) != 3 || inbox.DeliveryCount(nil) != 0 {
		t.Fatal("đọc header x-delivery-count")
	}
}
