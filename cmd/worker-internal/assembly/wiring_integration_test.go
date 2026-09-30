package assembly

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/acctbridge"
	abport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/acctbridge/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/costing"
	costport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/costing/port"
	measport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/measurement/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz/authztest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/idempotency"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/inbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/mass"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/outbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/telemetry"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

func TestMain(m *testing.M) { os.Exit(pgtest.Run(m)) }

// wired là worker-internal dựng thật trên database kiểm thử: pool theo vai như cmd/worker-internal.
type wired struct {
	t     *testing.T
	env   *pgtest.Env
	mu    sync.Mutex
	pools map[string]*pgxpool.Pool
	clk   *clock.Fixed
}

func (w *wired) pool(role string) (*pgxpool.Pool, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if role == "" {
		role = OpsRole
	}
	if p, ok := w.pools[role]; ok {
		return p, nil
	}
	p := w.env.Pool(w.t, migrations.MKG, role)
	w.pools[role] = p
	return p, nil
}

// deliver giao sự kiện cho consumer tên name của sổ Consumers (handler dựng như worker-internal).
func (w *wired) deliver(tid uuid.UUID, name, eventType string, payload any) {
	w.t.Helper()
	var c *Consumer
	for i := range Consumers {
		if Consumers[i].Name == name {
			c = &Consumers[i]
		}
	}
	if c == nil {
		w.t.Fatalf("không có consumer %s", name)
	}
	h, err := c.Handler(w.clk, w.pool)
	if err != nil {
		w.t.Fatal(err)
	}
	raw, ok := payload.([]byte)
	if !ok {
		if raw, err = json.Marshal(payload); err != nil {
			w.t.Fatal(err)
		}
	}
	p, _ := w.pool(c.Role)
	env := outbox.Envelope{EventID: ids.New(), EventType: eventType, SchemaVersion: 1, TenantID: tid, OccurredAt: w.clk.Now(), Producer: "test", Payload: raw}
	if _, err := inbox.Process(context.Background(), p, name, env, h); err != nil {
		w.t.Fatalf("%s: %v", name, err)
	}
}

// outboxPayload đọc payload sự kiện nội bộ mới nhất của tenant bằng vai role.
func (w *wired) outboxPayload(tid uuid.UUID, role, eventType string) []byte {
	w.t.Helper()
	p, _ := w.pool(role)
	var raw []byte
	err := tenant.WithTenantTx(context.Background(), p, tid, func(tx pgx.Tx) error {
		return tx.QueryRow(context.Background(), `SELECT payload::text FROM shared.outbox WHERE event_type = $1 AND kind = 'internal'
		  ORDER BY id DESC LIMIT 1`, eventType).Scan(&raw)
	})
	if err != nil {
		w.t.Fatalf("outbox %s: %v", eventType, err)
	}
	return raw
}

func post(t *testing.T, base, path, token string, body any) (int, map[string]any) {
	t.Helper()
	b, _ := json.Marshal(body)
	req, _ := http.NewRequest(http.MethodPost, base+path, bytes.NewReader(b))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", "k-"+ids.New().String())
	r, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = r.Body.Close() }()
	raw, _ := io.ReadAll(r.Body)
	var m map[string]any
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	_ = dec.Decode(&m)
	if r.StatusCode >= 300 {
		t.Logf("%s → %d %s", path, r.StatusCode, raw)
	}
	return r.StatusCode, m
}

func server(t *testing.T, iss *authztest.FakeIssuer, pool *pgxpool.Pool, admin bool, reg func(*gin.RouterGroup)) string {
	eng := httpx.NewEngine(telemetry.NewLogger("test", io.Discard), 1<<20)
	mw := []gin.HandlerFunc{httpx.Authenticate(iss.Verifier())}
	if admin {
		mw = append(mw, httpx.RequireMFA())
	}
	mw = append(mw, idempotency.Middleware(pool))
	reg(eng.Group("/v1", mw...))
	s := httptest.NewServer(eng)
	t.Cleanup(s.Close)
	return s.URL
}

// G4 tích hợp (mục 1, B-02): chuỗi worker-internal THẬT — consumer costing dựng qua sổ Consumers (đủ cổng
// orders/logistics/hub trên pool vai chủ dữ liệu), job đóng kỳ đọc lại chuyến + giao nhận qua cổng thật,
// period.closed → acctbridge (ClosedPeriods trên pool costing_app) → job dựng → bút toán tổng hợp có
// source_hash = result_set_sha256 của kỳ.
func TestWorkerInternalCostingToAcctbridgeWiring_G4(t *testing.T) {
	env := pgtest.Get(t)
	w := &wired{t: t, env: env, pools: map[string]*pgxpool.Pool{}, clk: clock.NewFixed(time.Now().UTC().Truncate(time.Second))}
	ctx := context.Background()
	tid := ids.New()
	iss := authztest.New(t)
	sign := func(roles ...string) (uuid.UUID, string) {
		a := ids.New()
		return a, iss.Sign(t, authztest.Token{Subject: a, TenantID: tid, Roles: roles, MFA: true})
	}
	costPool, _ := w.pool(costing.DBRole)
	abPool, _ := w.pool(acctbridge.DBRole)
	costMod := costing.New(costPool, w.clk, costing.Deps{})
	costURL := server(t, iss, costPool, false, costMod.RegisterApp)
	abMod := acctbridge.New(abPool, w.clk, acctbridge.Deps{})
	abAdmin := server(t, iss, abPool, true, abMod.RegisterAdmin)

	// Bảng ánh xạ tài khoản: vận hành đề xuất ≠ kế toán trưởng duyệt (B-05).
	_, op := sign(abport.RoleAccountingBridgeOperator)
	st, m := post(t, abAdmin, "/v1/acctbridge/mappings", op, map[string]any{"reason": "Bảng ánh xạ tạm (kiểm thử tích hợp)", "effective_from": "2026-01-01"})
	if st != http.StatusCreated {
		t.Fatalf("đề xuất ánh xạ: %d", st)
	}
	ver := m["mapping"].(map[string]any)["version_id"].(string)
	_, chief := sign(abport.RoleChiefAccountant)
	if st, _ := post(t, abAdmin, "/v1/acctbridge/mappings/"+ver+"/decision", chief, map[string]any{"decision": "approved"}); st != http.StatusCreated {
		t.Fatalf("duyệt ánh xạ: %d", st)
	}

	// Đầu vào kỳ tháng trước: phiếu HUB_IN → chi phí sơ chế hub (#55).
	now := w.clk.Now()
	curFrom := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	at := curFrom.AddDate(0, -1, 5)
	lot := ids.New()
	w.deliver(tid, costport.WeighTicketConsumer, measport.EventWeighTicketRecorded, measport.WeighTicketRecordedV1{TicketID: ids.New(), LotRef: lot,
		Point: measport.PointHubIn, Route: measport.RouteHub, Source: measport.SourceEdge, MassKg: mass.MustKg("50000"), EventTime: at, CollectionOrderRef: &lot})
	var pid uuid.UUID
	if err := tenant.WithTenantTx(ctx, costPool, tid, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, "SELECT period_id FROM mkg_costing.period_current WHERE from_at <= $1 AND $1 < to_at", at).Scan(&pid)
	}); err != nil {
		t.Fatalf("kỳ của phiếu cân: %v", err)
	}
	closer, ctok := sign(costport.RolePeriodCloser)
	st, m = post(t, costURL, "/v1/costing/periods/"+pid.String()+"/close", ctok, nil)
	if st != http.StatusAccepted {
		t.Fatalf("đóng kỳ: %d %v", st, m)
	}
	job := uuid.MustParse(m["job_id"].(string))
	w.deliver(tid, costport.PeriodCloseConsumer, costport.EventPeriodCloseRequested, costport.PeriodCloseRequestedV1{JobID: job, PeriodID: pid,
		RequestedBy: closer, RequestedAt: now})
	sum, err := costMod.ClosedPeriods().ClosedPeriod(ctx, tid, pid)
	if err != nil || len(sum.Lines) == 0 {
		t.Fatalf("kỳ chưa đóng qua consumer worker-internal: %v %+v", err, sum)
	}

	// period.closed (outbox costing) → consumer acctbridge dựng qua sổ Consumers → job dựng.
	w.deliver(tid, abport.PeriodClosedConsumer, costport.EventPeriodClosed, w.outboxPayload(tid, costing.DBRole, costport.EventPeriodClosed))
	w.deliver(tid, abport.JournalBuildConsumer, abport.EventJournalBuildRequested, w.outboxPayload(tid, acctbridge.DBRole, abport.EventJournalBuildRequested))
	var hash, status string
	if err := tenant.WithTenantTx(ctx, abPool, tid, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, "SELECT source_hash, status FROM mkg_acctbridge.journal_current WHERE period_id = $1", pid).Scan(&hash, &status)
	}); err != nil {
		t.Fatalf("B-02: chưa có bút toán dựng từ ClosedPeriods thật: %v", err)
	}
	if hash != sum.ResultSetSHA256 || status != "built" {
		t.Fatalf("bút toán: source_hash %s (muốn %s), trạng thái %s", hash, sum.ResultSetSHA256, status)
	}
}
