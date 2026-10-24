package main

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports"
	repport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/db"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/outbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

func TestMain(m *testing.M) { os.Exit(pgtest.Run(m)) }

func dispatchEnvelope(t *testing.T) outbox.Envelope {
	t.Helper()
	p, err := json.Marshal(repport.DispatchRequestedV1{DispatchID: ids.New().String(), Attempt: 1})
	if err != nil {
		t.Fatal(err)
	}
	return outbox.Envelope{EventID: ids.New(), TenantID: ids.New(), EventType: repport.EventDispatchRequested, Payload: p}
}

// TestReportsWorkerNilDBFailsFast — #128: module dựng thiếu DB (lỗi cũ reports.New(nil, …)) bị chặn ở kiểm phụ thuộc lúc
// khởi động, và handler (nếu vẫn được gọi) trả lỗi 503 chứ không panic giữa lúc gửi email.
func TestReportsWorkerNilDBFailsFast(t *testing.T) {
	m := reports.New(nil, clock.System{}, reports.Deps{})
	err := m.RequireWorkerDeps()
	if err == nil || !strings.Contains(err.Error(), "DB") {
		t.Fatalf("thiếu DB phải lỗi khởi động: %v", err)
	}
	if err := m.DispatchHandler()(context.Background(), nil, dispatchEnvelope(t)); err == nil {
		t.Fatal("handler thiếu DB phải trả lỗi")
	}
	// Cấu hình sai thời hạn nhận việc → lỗi khởi động.
	if _, err := reports.SendLeaseFromEnv(func(string) string { return "15" }); err == nil {
		t.Fatal("AG_REPORT_DISPATCH_LEASE sai định dạng phải lỗi")
	}
	if d, err := reports.SendLeaseFromEnv(func(string) string { return "" }); err != nil || d.Minutes() != 15 {
		t.Fatalf("mặc định 15 phút: %v %v", d, err)
	}
}

// TestReportsWorkerStartupWiresDB — #128: worker dựng pf/reports trên pool app_worker THẬT (PostgreSQL), qua kiểm phụ
// thuộc khởi động, và handler gửi Chi cục chạy giao dịch được (lần gửi không tồn tại → bỏ qua, không panic).
func TestReportsWorkerStartupWiresDB(t *testing.T) {
	env := pgtest.Get(t)
	pool := env.Pool(t, migrations.PF, dbadmin.AppWorker)
	t.Setenv("AG_S3_ENDPOINT", "")
	t.Setenv("AG_OBJSTORE_DIR", t.TempDir())
	for _, k := range []string{"AG_KC_ADMIN_URL", "AG_KC_REALM", "AG_KC_CLIENT_ID", "AG_KC_CLIENT_SECRET", "AG_SMTP_HOST", "AG_REPORT_DISPATCH_LEASE"} {
		t.Setenv(k, "")
	}
	log := slog.New(slog.DiscardHandler)
	m, err := buildReportsModule(context.Background(), log, pool)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.RequireWorkerDeps(); err != nil {
		t.Fatalf("phụ thuộc worker: %v", err)
	}
	ev := dispatchEnvelope(t)
	b := db.ForModule(pool, reports.DBModule)
	if err := tenant.WithTenantTx(context.Background(), b, ev.TenantID, func(tx pgx.Tx) error {
		return m.DispatchHandler()(context.Background(), tx, ev)
	}); err != nil {
		t.Fatalf("handler trên pool thật: %v", err)
	}
	// Consumer pf/reports có bước prepare: thiếu cấu hình bắt buộc → lỗi ngay lúc khởi động consumer.
	t.Setenv("AG_DB_URL", "")
	var n int
	for _, c := range reportsConsumers(log) {
		if c.prepare == nil {
			t.Fatalf("consumer %s thiếu prepare", c.name)
		}
		n++
		if err := c.prepare(context.Background()); err == nil || errors.Is(err, context.Canceled) {
			t.Fatalf("consumer %s: thiếu AG_DB_URL phải lỗi khởi động: %v", c.name, err)
		}
	}
	if n != 3 {
		t.Fatalf("số consumer pf/reports: %d", n)
	}
}
