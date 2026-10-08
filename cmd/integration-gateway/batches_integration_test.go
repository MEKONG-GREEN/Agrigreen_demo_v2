package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/cmd/integration-gateway/assembly"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/cmd/integration-gateway/gateway"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/apiclient"
	apiclientport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/apiclient/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz/authztest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/idempotency"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/meter"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/telemetry"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

type ingestEnv struct {
	*intEnv
	queue *apiclient.IngestMemoryQueue
}

// newIngestEnv dựng cổng THẬT (assembly.Build + gateway.NewHandler) với nạp theo lô trên hàng đợi trong tiến trình.
func newIngestEnv(t *testing.T, allowProposed bool, maxQueued int, owners map[string]apiclient.IngestOwner) *ingestEnv {
	t.Helper()
	pg := pgtest.Get(t)
	t.Setenv(apiclientport.EnvPepperPrefix+"1", strings.Repeat("p", apiclientport.MinPepperBytes))
	t.Setenv(apiclientport.EnvPepperCurrent, "1")
	peppers, err := apiclient.PeppersFromEnv()
	if err != nil || peppers == nil {
		t.Fatalf("pepper: %v", err)
	}
	e := &intEnv{t: t, pg: pg, iss: authztest.New(t), operator: ids.New(), grantor: ids.New(), third: ids.New()}
	clk := clock.System{}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	q := apiclient.NewIngestMemoryQueue(maxQueued)
	q.Backoff = nil
	open := func(_ context.Context, role string) (*pgxpool.Pool, error) {
		return pg.Pool(t, migrations.PF, role), nil
	}
	mod, d, err := assembly.Build(context.Background(), open, assembly.Config{Operator: e.operator, Peppers: peppers, Replicas: 1,
		EgressMode: meter.ModeEnforce, Clock: clk, Log: log, IngestQueue: q, IngestAllowProposed: allowProposed, IngestMaxQueued: maxQueued,
		IngestOwners: owners})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if d.Ingest == nil {
		t.Fatal("có hàng đợi → phải bật nạp theo lô")
	}
	e.mod, e.deps, e.log = mod, d, log
	e.public = httptest.NewServer(gateway.NewHandler(mod, d, nil, log))
	t.Cleanup(e.public.Close)
	tl := telemetry.NewLogger("test", io.Discard)
	adm := httpx.NewEngine(tl, 1<<20)
	g := adm.Group("/v1", httpx.Authenticate(e.iss.Verifier()), httpx.RequireMFA(), idempotency.Middleware(pg.Pool(t, migrations.PF, "platform_api")))
	mod.RegisterAdmin(g)
	e.admin = httptest.NewServer(adm)
	t.Cleanup(e.admin.Close)
	return &ingestEnv{intEnv: e, queue: q}
}

// onboardIngest: khách hàng API của tenant đối tác (đề xuất ≠ duyệt) + đăng ký hợp đồng nạp với scope
// ingest:<hợp đồng>:write + gói hạn mức + khóa.
func (e *ingestEnv) onboardIngest(partner uuid.UUID, contract string) (uuid.UUID, string) {
	t := e.t
	ad, ap := e.adminAs(e.operator, apiclientport.RoleAPIClientAdmin), e.adminAs(e.operator, apiclientport.RoleAPIClientApprover)
	r := ad(http.MethodPost, "/v1/api-clients", map[string]any{"client_tenant_id": partner.String(), "display_name": "Đối tác ghi lô (thử)",
		"purpose": "Gửi lô bản ghi song phương qua cổng tích hợp", "contact_ref": ids.New().String()})
	want(t, r, http.StatusCreated, "đề xuất khách hàng API")
	clientID := r.body["id"].(string)
	want(t, ap(http.MethodPost, "/v1/api-clients/"+clientID+"/approve", nil), http.StatusOK, "duyệt khách hàng")
	r = ad(http.MethodPost, "/v1/api-clients/"+clientID+"/subscriptions", map[string]any{"product": contract,
		"scopes": []string{apiclientport.IngestScope(contract)}})
	want(t, r, http.StatusCreated, "đề xuất đăng ký hợp đồng nạp")
	want(t, ap(http.MethodPost, "/v1/api-clients/"+clientID+"/subscriptions/"+r.body["id"].(string)+"/approve", nil), http.StatusOK, "duyệt đăng ký")
	r = ad(http.MethodPost, "/v1/api-clients/"+clientID+"/plans", map[string]any{"requests_per_minute": 600, "daily_units": 100,
		"monthly_units": 1000, "max_export_rows": 1000})
	want(t, r, http.StatusCreated, "đề xuất gói")
	want(t, ap(http.MethodPost, fmt.Sprintf("/v1/api-clients/%s/plans/%v/approve", clientID, r.body["version"]), nil), http.StatusOK, "duyệt gói")
	r = ad(http.MethodPost, "/v1/api-clients/"+clientID+"/keys", nil)
	want(t, r, http.StatusCreated, "phát hành khóa")
	return uuid.MustParse(clientID), r.body["api_key"].(string)
}

func (e *ingestEnv) drain() { e.queue.Drain(context.Background(), e.deps.Ingest.Worker()) }

func rollRecord(ref string, revision, rolls int) map[string]any {
	return map[string]any{"statement_ref": ref, "schedule_ref": nil, "htx_ref": "01989e00-1111-7222-8333-944455566677",
		"plot_ref": "01989f00-aa11-7b22-8c33-d44e55f66a77", "season_ref": nil, "collected_on": "2026-10-09", "rolls": rolls, "revision": revision}
}

func batchBody(contract, ref string, items ...map[string]any) map[string]any {
	return map[string]any{"contract": contract, "contract_version": 1, "batch_ref": ref, "sent_at": "2026-10-09T18:05:00+07:00", "items": items}
}

func item(key string, rec map[string]any) map[string]any {
	return map[string]any{"entity_key": key, "record": rec}
}

func (e *ingestEnv) post(key, contract string, body map[string]any) intResp {
	e.t.Helper()
	return e.call(http.MethodPost, "/v1/batches/"+contract, key, body, "Idempotency-Key", body["batch_ref"].(string))
}

// W5.4 (R-INT-03, §41.3) trên cổng THẬT + DB thật: 202 + mã lô → bộ xử lý hàng đợi đối tác (inbox) → kết quả từng
// bản ghi; bản ghi lỗi không làm hỏng lô; gọi lặp idempotent; khóa nghiệp vụ idempotent qua lô; sổ đo lường.
func TestBatchIngestRealWiring_W54Integration(t *testing.T) {
	e := newIngestEnv(t, true, 0, nil)
	partner := ids.New()
	cid, key := e.onboardIngest(partner, "roll_count_statement")

	// Mặc định từ chối trước khi đọc thân.
	r := e.call(http.MethodPost, "/v1/batches/khong_co_hop_dong", key, map[string]any{}, "Idempotency-Key", ids.New().String())
	if r.status != http.StatusNotFound || r.code() != "integration.contract_unknown" {
		t.Fatalf("hợp đồng lạ → 404: %d %s", r.status, r.raw)
	}
	r = e.post(key, "collection_schedule", batchBody("collection_schedule", ids.New().String(), item("x", map[string]any{})))
	if r.status != http.StatusForbidden || r.code() != apiclientport.ErrForbidden.Code {
		t.Fatalf("thiếu scope ingest:collection_schedule:write → 403: %d %s", r.status, r.raw)
	}
	r = e.call(http.MethodPost, "/v1/batches/roll_count_statement", key, batchBody("roll_count_statement", ids.New().String()))
	if r.status != http.StatusBadRequest || r.code() != problem.IdempotencyRequired.Code {
		t.Fatalf("thiếu Idempotency-Key → 400: %d %s", r.status, r.raw)
	}

	refA, refB := ids.New().String(), ids.New().String()
	bad := rollRecord(ids.New().String(), 1, 3)
	bad["price"] = 1000 // trường ngoài hợp đồng
	body := batchBody("roll_count_statement", ids.New().String(),
		item(refA, rollRecord(refA, 1, 46)),
		item("sai-khoa", rollRecord(refB, 1, 12)),
		item(bad["statement_ref"].(string), bad))
	r = e.post(key, "roll_count_statement", body)
	want(t, r, http.StatusAccepted, "nhận lô")
	id := r.body["id"].(string)
	if r.header.Get("Location") != "/v1/batches/"+id || r.body["status"] != "queued" || r.body["items"] != float64(3) ||
		r.header.Get(apiclientport.HeaderDataContract) != "roll_count_statement.v1" {
		t.Fatalf("202 + mã lô + Location: %v %s", r.header, r.raw)
	}
	r = e.call(http.MethodGet, "/v1/batches/"+id, key, nil)
	if r.status != http.StatusOK || r.body["status"] != "queued" || r.body["results"] != nil {
		t.Fatalf("trước khi xử lý: queued, chưa có kết quả: %s", r.raw)
	}

	e.drain()
	r = e.call(http.MethodGet, "/v1/batches/"+id, key, nil)
	want(t, r, http.StatusOK, "kết quả lô")
	if r.body["status"] != "completed" || r.body["accepted"] != float64(1) || r.body["rejected"] != float64(2) || r.body["completed_at"] == nil {
		t.Fatalf("lô completed, 1 nhận 2 từ chối: %s", r.raw)
	}
	res := r.body["results"].([]any)
	codes := []string{}
	for _, x := range res {
		m := x.(map[string]any)
		c, _ := m["problem_code"].(string)
		codes = append(codes, m["outcome"].(string)+"/"+c)
	}
	if strings.Join(codes, ",") != "accepted/,rejected/integration.entity_key_mismatch,rejected/integration.record_invalid" {
		t.Fatalf("kết quả từng bản ghi: %v (%s)", codes, r.raw)
	}
	if strings.Contains(string(r.raw), "1000") {
		t.Fatalf("kết quả không chứa giá trị bản ghi: %s", r.raw)
	}

	// Gọi lặp cùng thân → cùng lô, Idempotent-Replayed, không thêm dòng sổ; cùng khóa khác thân → 422.
	r = e.post(key, "roll_count_statement", body)
	if r.status != http.StatusAccepted || r.body["id"] != id || r.header.Get(idempotency.ReplayedHeader) != "true" || r.body["status"] != "completed" {
		t.Fatalf("gọi lặp: %d %v %s", r.status, r.header, r.raw)
	}
	body2 := batchBody("roll_count_statement", body["batch_ref"].(string), item(refA, rollRecord(refA, 1, 47)))
	if r = e.post(key, "roll_count_statement", body2); r.status != http.StatusUnprocessableEntity || r.code() != problem.IdempotencyConflict.Code {
		t.Fatalf("cùng Idempotency-Key, thân khác → 422: %d %s", r.status, r.raw)
	}
	if n := e.count(partner, `SELECT count(*) FROM pf_apiclient.ingest_batch`); n != 1 {
		t.Fatalf("một dòng sổ lô (gọi lặp không ghi thêm), nhận %d", n)
	}
	if n := e.count(partner, `SELECT count(*) FROM pf_apiclient.ingest_staging WHERE contract = 'roll_count_statement' AND business_key = $1`, refA+":1"); n != 1 {
		t.Fatalf("sổ đệm song phương: 1 bản ghi đã nhận, nhận %d", n)
	}

	// Khóa nghiệp vụ idempotent qua lô: cùng (statement_ref, revision) cùng nội dung → accepted; khác nội dung → conflict.
	r = e.post(key, "roll_count_statement", batchBody("roll_count_statement", ids.New().String(),
		item(refA, rollRecord(refA, 1, 46)), item(refA, rollRecord(refA, 1, 99)), item(refA, rollRecord(refA, 2, 99))))
	want(t, r, http.StatusAccepted, "lô thứ hai")
	id2 := r.body["id"].(string)
	e.drain()
	r = e.call(http.MethodGet, "/v1/batches/"+id2, key, nil)
	if r.body["accepted"] != float64(2) || r.body["rejected"] != float64(1) || !strings.Contains(string(r.raw), "integration.business_key_conflict") {
		t.Fatalf("khóa nghiệp vụ idempotent / xung đột: %s", r.raw)
	}
	if n := e.count(partner, `SELECT count(*) FROM pf_apiclient.ingest_staging`); n != 2 {
		t.Fatalf("sổ đệm: revision 1 và 2, nhận %d", n)
	}

	// Sổ đo lường: POST = export_request (0 đơn vị, job_id = mã lô), GET = job_status; product = hợp đồng.
	if n := e.count(e.operator, `SELECT count(*) FROM pf_metering.usage_event WHERE client_id = $1::uuid AND call_kind = 'export_request'
	    AND product = 'roll_count_statement' AND job_id = $2::uuid AND units = 0`, cid, uuid.MustParse(id)); n != 2 {
		t.Fatalf("sổ đo lường POST lô (lần đầu + gọi lặp): %d", n)
	}
	if n := e.count(e.operator, `SELECT count(*) FROM pf_metering.usage_event WHERE client_id = $1::uuid AND call_kind = 'job_status'
	    AND route_template = 'GET /v1/batches/{id}'`, cid); n != 3 {
		t.Fatalf("sổ đo lường GET lô: %d", n)
	}
}

// Hợp đồng proposed không có cờ dev/test → 403 integration.contract_not_servable (production luôn từ chối:
// assembly bỏ cờ khi Production, gateway không khởi động — gateway.IngestFromEnv).
func TestBatchIngestProposedRefusedWithoutFlag_W54Integration(t *testing.T) {
	e := newIngestEnv(t, false, 0, nil)
	_, key := e.onboardIngest(ids.New(), "roll_count_statement")
	ref := ids.New().String()
	r := e.post(key, "roll_count_statement", batchBody("roll_count_statement", ids.New().String(), item(ref, rollRecord(ref, 1, 1))))
	if r.status != http.StatusForbidden || r.code() != "integration.contract_not_servable" {
		t.Fatalf("proposed không cờ → 403: %d %s", r.status, r.raw)
	}
	if _, err := gateway.IngestFromEnv(func(k string) string {
		if k == apiclientport.EnvIngestAllowProposed {
			return "1"
		}
		return ""
	}, true, e.log); err == nil {
		t.Fatal("production + cờ proposed → không khởi động")
	}
}

// Vách ngăn từng đối tác (R-INT-04): hàng đợi A đầy → 429 integration.queue_saturated + Retry-After, B vẫn 202;
// port chủ lỗi tạm với A → giao lại tới giới hạn → DLQ riêng của A, lô dead_lettered; B completed. RLS: đối tác chỉ
// thấy lô của mình (GET lô của A bằng khóa B → 404; đếm dưới tenant B không thấy dòng của A).
func TestBatchIngestPartnerIsolationAndDLQ_W54Integration(t *testing.T) {
	partnerA, partnerB := ids.New(), ids.New()
	var attemptsA int
	owners := map[string]apiclient.IngestOwner{"roll_count_statement": {Target: "test.flaky", Sink: ingestSinkFunc(func(_ context.Context, r apiclient.IngestRecord) error {
		if r.PartnerTenant == partnerA {
			attemptsA++
			return fmt.Errorf("port chủ tạm lỗi")
		}
		return nil
	})}}
	e := newIngestEnv(t, true, 2, owners)
	cidA, keyA := e.onboardIngest(partnerA, "roll_count_statement")
	_, keyB := e.onboardIngest(partnerB, "roll_count_statement")

	ref := ids.New().String()
	var idsA []string
	for range 2 {
		r := e.post(keyA, "roll_count_statement", batchBody("roll_count_statement", ids.New().String(), item(ref, rollRecord(ref, 1, 5))))
		want(t, r, http.StatusAccepted, "lô của A")
		idsA = append(idsA, r.body["id"].(string))
	}
	r := e.post(keyA, "roll_count_statement", batchBody("roll_count_statement", ids.New().String(), item(ref, rollRecord(ref, 1, 5))))
	if r.status != http.StatusTooManyRequests || r.code() != "integration.queue_saturated" || r.header.Get("Retry-After") != "30" {
		t.Fatalf("hàng đợi A đầy → 429 + Retry-After: %d %v %s", r.status, r.header, r.raw)
	}
	r = e.post(keyB, "roll_count_statement", batchBody("roll_count_statement", ids.New().String(), item(ref, rollRecord(ref, 1, 5))))
	want(t, r, http.StatusAccepted, "B không bị A chặn")
	idB := r.body["id"].(string)

	e.drain()
	for _, id := range idsA {
		r = e.call(http.MethodGet, "/v1/batches/"+id, keyA, nil)
		if r.body["status"] != "dead_lettered" || r.body["problem_code"] != "integration.owner_unavailable" {
			t.Fatalf("lô độc của A → dead_lettered: %s", r.raw)
		}
	}
	if attemptsA != 2*6 {
		t.Fatalf("mỗi lô của A giao 6 lần (giới hạn 5 + lần cuối), nhận %d", attemptsA)
	}
	if dl := e.queue.DeadLettered(cidA); len(dl) != 2 {
		t.Fatalf("DLQ riêng của A: %d", len(dl))
	}
	r = e.call(http.MethodGet, "/v1/batches/"+idB, keyB, nil)
	if r.body["status"] != "completed" || r.body["accepted"] != float64(1) {
		t.Fatalf("B completed: %s", r.raw)
	}
	// Hàng đợi A đã rỗng → nhận lại.
	want(t, e.post(keyA, "roll_count_statement", batchBody("roll_count_statement", ids.New().String(), item(ref, rollRecord(ref, 1, 5)))),
		http.StatusAccepted, "A sau khi rút hàng đợi")

	// Cách ly đối tác.
	if r = e.call(http.MethodGet, "/v1/batches/"+idsA[0], keyB, nil); r.status != http.StatusNotFound || r.code() != "integration.batch_not_found" {
		t.Fatalf("B đọc lô của A → 404: %d %s", r.status, r.raw)
	}
	if n := e.count(partnerB, `SELECT count(*) FROM pf_apiclient.ingest_batch`); n != 1 {
		t.Fatalf("RLS: tenant B chỉ thấy 1 lô của mình, nhận %d", n)
	}
	if n := e.count(partnerB, `SELECT count(*) FROM pf_apiclient.ingest_batch_outcome WHERE outcome = 'dead_lettered'`); n != 0 {
		t.Fatalf("RLS: tenant B không thấy kết cục của A, nhận %d", n)
	}
	if n := e.count(partnerA, `SELECT count(*) FROM pf_apiclient.ingest_batch_outcome WHERE outcome = 'dead_lettered'`); n != 2 {
		t.Fatalf("kết cục dead_lettered của A: %d", n)
	}
}

type ingestSinkFunc func(ctx context.Context, r apiclient.IngestRecord) error

func (f ingestSinkFunc) Ingest(ctx context.Context, r apiclient.IngestRecord) error { return f(ctx, r) }
