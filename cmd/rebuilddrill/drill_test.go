package main

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/costing"
	costport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/costing/port"
	fieldport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/fieldops/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/logistics"
	logport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/logistics/port"
	measport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/measurement/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/orders"
	ordport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/orders/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/inbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/mass"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/outbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

func TestMain(m *testing.M) { os.Exit(pgtest.Run(m)) }

type pools struct {
	t   *testing.T
	env *pgtest.Env
	mu  sync.Mutex
	m   map[string]*pgxpool.Pool
}

func (p *pools) get(role string) (*pgxpool.Pool, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if x, ok := p.m[role]; ok {
		return x, nil
	}
	x := p.env.Pool(p.t, migrations.MKG, role)
	p.m[role] = x
	return x, nil
}

func deliver(t *testing.T, pool *pgxpool.Pool, tid uuid.UUID, consumer, eventType string, h inbox.Handler, payload any) {
	t.Helper()
	raw, _ := json.Marshal(payload)
	env := outbox.Envelope{EventID: ids.New(), EventType: eventType, SchemaVersion: 1, TenantID: tid, OccurredAt: time.Now(), Producer: "test", Payload: raw}
	if _, err := inbox.Process(context.Background(), pool, consumer, env, h); err != nil {
		t.Fatalf("%s: %v", consumer, err)
	}
}

// R-22 (A-4, G4 tích hợp mục 7): diễn tập dựng lại trên database kiểm thử — dữ liệu sinh qua consumer thật của
// logistics / orders / costing; lần 1 mọi bảng chiếu khớp (băm trước = sau); bảng chiếu bị sửa tay (giả lập lệch)
// → diễn tập báo LỆCH và dựng lại từ sổ; lần sau khớp lại. Mọi module (kể cả fieldops từ G4) có hàm dựng lại.
func TestRebuildDrillChecksumsBeforeAfter_R22(t *testing.T) {
	env := pgtest.Get(t)
	ctx := context.Background()
	p := &pools{t: t, env: env, m: map[string]*pgxpool.Pool{}}
	clk := clock.NewFixed(time.Now().UTC().Truncate(time.Second))
	tid := ids.New()
	lp, _ := p.get(logistics.DBRole)
	op, _ := p.get(orders.DBRole)
	cp, _ := p.get(costing.DBRole)
	lot := ids.New()
	at := clk.Now().Add(-3 * time.Hour)
	lc := logistics.NewConsumers(clk, nil)
	deliver(t, lp, tid, logport.RollCountConsumer, fieldport.EventRollCountRecorded, lc.RollCount(), fieldport.RollCountRecordedV1{
		RollCountID: ids.New(), LotRef: lot, PlotRef: ids.New(), HTXRef: ids.New(), Rolls: 1000, Stage: fieldport.StageBoatLoad,
		RuleVersion: "fieldops.count.v1", EventTime: at})
	deliver(t, lp, tid, logport.RollCountConsumer, fieldport.EventRollCountRecorded, lc.RollCount(), fieldport.RollCountRecordedV1{
		RollCountID: ids.New(), LotRef: ids.New(), PlotRef: ids.New(), HTXRef: ids.New(), Rolls: 500, Stage: fieldport.StageBoatLoad,
		RuleVersion: "fieldops.count.v1", EventTime: at})
	ticket := measport.WeighTicketRecordedV1{TicketID: ids.New(), LotRef: lot, Point: measport.PointHubIn, Route: measport.RouteHub,
		Source: measport.SourceEdge, MassKg: mass.MustKg("20500"), EventTime: at.Add(time.Hour), CollectionOrderRef: &lot}
	deliver(t, lp, tid, logport.WeighTicketConsumer, measport.EventWeighTicketRecorded, lc.WeighTicket(), ticket)
	deliver(t, op, tid, ordport.WeighTicketConsumer, measport.EventWeighTicketRecorded, orders.NewConsumers(clk).WeighTicket(), ticket)
	deliver(t, cp, tid, costport.WeighTicketConsumer, measport.EventWeighTicketRecorded, costing.NewConsumers(clk, costing.Deps{}).WeighTicket(), ticket)

	cfg := Config{Pool: p.get, Tenants: []uuid.UUID{tid}, Clock: clk, Database: "ag_mkg_test"}
	rep, err := Run(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !rep.OK || rep.Mismatches != 0 || len(rep.Modules) != len(Targets) {
		t.Fatalf("lần 1 phải khớp: %s", rep.Markdown())
	}
	byTable := map[string]TableResult{}
	for _, tr := range rep.Tables {
		byTable[tr.Table] = tr
	}
	if tr := byTable["mkg_logistics.trip_current"]; tr.RowsBefore != 2 || !tr.Rebuilt || tr.SHABefore == "" {
		t.Fatalf("trip_current: %+v", tr)
	}
	if tr := byTable["mkg_costing.lot_cost_current"]; tr.RowsBefore == 0 || !tr.Rebuilt {
		t.Fatalf("lot_cost_current: %+v", tr)
	}
	if tr := byTable["mkg_orders.lot_ticket_current"]; tr.RowsBefore != 1 || !tr.Rebuilt {
		t.Fatalf("lot_ticket_current: %+v", tr)
	}
	if _, ok := byTable["mkg_hub.lot_balance"]; !ok {
		t.Fatal("thiếu mkg_hub.lot_balance")
	}
	// G4: mọi module mkg có hàm dựng lại (fieldops từ G4 — TestFieldopsProjectionRebuildEqualsLive_R22).
	for _, m := range rep.Modules {
		if !m.Supported || m.Error != "" {
			t.Fatalf("module %s phải dựng lại được: %+v", m.Module, m)
		}
	}
	if strings.Contains(rep.Markdown(), "**chưa có**") {
		t.Fatal("báo cáo không còn module chưa có dựng lại")
	}
	if tr, ok := byTable["mkg_fieldops.collection_order_current"]; !ok || !tr.Rebuilt || !tr.Equal {
		t.Fatalf("fieldops collection_order_current: %+v", tr)
	}
	if tr := byTable["mkg_fieldops.weighed_loading_current"]; tr.Rebuilt || !tr.Equal {
		t.Fatalf("bảng đệm fieldops không dựng lại, phải khớp: %+v", tr)
	}

	// Giả lập bảng chiếu lệch sổ (sửa thẳng bằng quản trị cụm) → diễn tập báo LỆCH, dựng lại sửa về đúng sổ.
	admin := env.AdminPool(t, migrations.MKG)
	if _, err := admin.Exec(ctx, "UPDATE mkg_logistics.trip_current SET planned_kg = 1 WHERE tenant_id = $1", tid); err != nil {
		t.Fatal(err)
	}
	cfg.Modules = []string{"logistics"}
	rep, err = Run(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if rep.OK || rep.Mismatches != 1 || !strings.Contains(rep.Markdown(), "**LỆCH**") || !strings.Contains(rep.Markdown(), "HỎNG") {
		t.Fatalf("bảng chiếu lệch phải bị báo: %s", rep.Markdown())
	}
	rep, _ = Run(ctx, cfg)
	if !rep.OK {
		t.Fatalf("sau dựng lại phải khớp: %s", rep.Markdown())
	}

	// Tìm tenant qua vai ag_ops_jobs (outbox cụm mkg có trip.completed của tenant).
	pub, _ := p.get(dbadmin.OpsJobs)
	all, err := OutboxTenants(pub)(ctx)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, x := range all {
		found = found || x == tid
	}
	if !found {
		t.Fatal("OutboxTenants thiếu tenant kiểm thử")
	}
}

func TestRebuildDrillRequiresConfirmCopy(t *testing.T) {
	if code, err := run(nil); code != 2 || err == nil {
		t.Fatalf("thiếu -confirm-copy phải mã 2: %d %v", code, err)
	}
	t.Setenv("AG_MKG_DB_URL", "")
	if code, _ := run([]string{"-confirm-copy"}); code != 2 {
		t.Fatalf("thiếu AG_MKG_DB_URL phải mã 2: %d", code)
	}
	t.Setenv("AG_MKG_DB_URL", "postgres://127.0.0.1:1/?sslmode=disable")
	if code, _ := run([]string{"-confirm-copy", "-tenants", "không-phải-uuid"}); code != 2 {
		t.Fatalf("tenant sai phải mã 2: %d", code)
	}
}
