package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/cmd/integration-gateway/assembly"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/cmd/integration-gateway/gateway"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/apiclient"
	apiclientport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/apiclient/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz/authztest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/idempotency"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/meter"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/telemetry"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

type fileOpts struct {
	sandbox      bool
	operator     uuid.UUID // Nil → mới
	maxQueued    int
	maxFileBytes int64
}

// newFileEnv dựng cổng THẬT (assembly.Build + gateway.NewHandler) với nạp theo lô + kênh tệp (W5.8) trên hàng đợi
// trong tiến trình; Admin API dùng CÙNG module (sandbox → phát hành khóa agks_).
func newFileEnv(t *testing.T, o fileOpts) *ingestEnv {
	t.Helper()
	pg := pgtest.Get(t)
	t.Setenv(apiclientport.EnvPepperPrefix+"1", strings.Repeat("p", apiclientport.MinPepperBytes))
	t.Setenv(apiclientport.EnvPepperCurrent, "1")
	peppers, err := apiclient.PeppersFromEnv()
	if err != nil || peppers == nil {
		t.Fatalf("pepper: %v", err)
	}
	op := o.operator
	if op == uuid.Nil {
		op = ids.New()
	}
	e := &intEnv{t: t, pg: pg, iss: authztest.New(t), operator: op, grantor: ids.New(), third: ids.New()}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	q := apiclient.NewIngestMemoryQueue(o.maxQueued)
	q.Backoff = nil
	open := func(_ context.Context, role string) (*pgxpool.Pool, error) {
		return pg.Pool(t, migrations.PF, role), nil
	}
	mod, d, err := assembly.Build(context.Background(), open, assembly.Config{Operator: e.operator, Peppers: peppers, Replicas: 1,
		EgressMode: meter.ModeEnforce, Clock: clock.System{}, Log: log, IngestQueue: q, IngestAllowProposed: true, IngestMaxQueued: o.maxQueued,
		IngestMaxFileBytes: o.maxFileBytes, Sandbox: o.sandbox})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	e.mod, e.deps, e.log = mod, d, log
	e.public = httptest.NewServer(gateway.NewHandler(mod, d, nil, log))
	t.Cleanup(e.public.Close)
	adm := httpx.NewEngine(telemetry.NewLogger("test", io.Discard), 1<<20)
	g := adm.Group("/v1", httpx.Authenticate(e.iss.Verifier()), httpx.RequireMFA(), idempotency.Middleware(pg.Pool(t, migrations.PF, "platform_api")))
	mod.RegisterAdmin(g)
	e.admin = httptest.NewServer(adm)
	t.Cleanup(e.admin.Close)
	return &ingestEnv{intEnv: e, queue: q}
}

// upload gửi thân tệp nguyên văn.
func (e *ingestEnv) upload(key, contract, query, contentType string, body []byte) intResp {
	e.t.Helper()
	req, _ := http.NewRequest(http.MethodPost, e.public.URL+"/v1/files/"+contract+query, bytes.NewReader(body))
	req.Header.Set(apiclientport.KeyHeader, key)
	req.Header.Set("Content-Type", contentType)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()
	raw, _ := io.ReadAll(res.Body)
	out := intResp{status: res.StatusCode, header: res.Header, raw: raw}
	_ = json.Unmarshal(raw, &out.body)
	return out
}

func rollLines(n, bad int) []byte {
	var b bytes.Buffer
	for i := 0; i < n; i++ {
		ref := ids.New().String()
		rec := rollRecord(ref, 1, i)
		if i == bad {
			rec["price"] = 987654 // trường ngoài hợp đồng → bản ghi bị từ chối, tệp vẫn nhận
		}
		line, _ := json.Marshal(rec)
		b.Write(line)
		b.WriteString("\r\n")
	}
	return b.Bytes()
}

const ndjson = "application/x-ndjson"

// W5.8 (R-INT-11) trên cổng THẬT + DB thật: tệp NDJSON/CSV → lô ingest_batch.v1 qua CHÍNH đường ống W5.4 (cùng scope,
// hợp đồng mặc định từ chối, ValidateRecord ở inbox, kết quả từng bản ghi) → tệp kết quả; idempotent theo tổng kiểm
// tệp; tệp hỏng/quá cỡ bị từ chối cả tệp; sổ đo lường.
func TestFileChannelRealWiring_W58Integration(t *testing.T) {
	e := newFileEnv(t, fileOpts{maxFileBytes: 256 << 10})
	partner := ids.New()
	cid, key := e.onboardIngest(partner, "roll_count_statement")
	q := "?contract_version=1"

	// Mặc định từ chối / kiểm đồng bộ trước khi đọc tệp.
	if r := e.upload(key, "khong_co_hop_dong", q, ndjson, []byte("{}")); r.status != http.StatusNotFound || r.code() != "integration.contract_unknown" {
		t.Fatalf("hợp đồng lạ → 404: %d %s", r.status, r.raw)
	}
	if r := e.upload(key, "collection_schedule", q, ndjson, []byte("{}")); r.status != http.StatusForbidden || r.code() != apiclientport.ErrForbidden.Code {
		t.Fatalf("thiếu scope → 403: %d %s", r.status, r.raw)
	}
	if r := e.upload(key, "roll_count_statement", "", ndjson, rollLines(1, -1)); r.status != http.StatusUnprocessableEntity {
		t.Fatalf("thiếu contract_version → 422: %d %s", r.status, r.raw)
	}
	if r := e.upload(key, "roll_count_statement", "?contract_version=9", ndjson, rollLines(1, -1)); r.code() != "integration.contract_unknown" {
		t.Fatalf("phiên bản lạ → 404: %d %s", r.status, r.raw)
	}
	if r := e.upload(key, "roll_count_statement", q, "application/json", rollLines(1, -1)); r.status != http.StatusUnsupportedMediaType ||
		r.code() != "integration.file_format_unsupported" {
		t.Fatalf("Content-Type lạ → 415: %d %s", r.status, r.raw)
	}
	if r := e.upload(key, "roll_count_statement", q, ndjson, append(rollLines(1, -1), []byte("\r\nkhông phải json\n")...)); r.status != http.StatusBadRequest ||
		r.code() != "integration.file_invalid" {
		t.Fatalf("tệp hỏng → 400: %d %s", r.status, r.raw)
	}
	if r := e.upload(key, "roll_count_statement", q, ndjson, bytes.Repeat([]byte("x"), 300<<10)); r.status != http.StatusRequestEntityTooLarge ||
		r.code() != "integration.file_too_large" {
		t.Fatalf("tệp quá cỡ → 413: %d %s", r.status, r.raw)
	}
	if n := e.count(partner, `SELECT count(*) FROM pf_apiclient.ingest_file`); n != 0 {
		t.Fatalf("tệp bị từ chối không ghi sổ: %d", n)
	}

	// 501 bản ghi (max_batch 500) → 2 lô; bản ghi 7 có trường lạ.
	file := rollLines(501, 6)
	r := e.upload(key, "roll_count_statement", q, ndjson, file)
	want(t, r, http.StatusAccepted, "nhận tệp")
	fid := r.body["id"].(string)
	bs := r.body["batches"].([]any)
	if r.header.Get("Location") != "/v1/files/"+fid || r.body["records"] != float64(501) || len(bs) != 2 || r.body["status"] != "queued" ||
		r.header.Get(apiclientport.HeaderDataContract) != "roll_count_statement.v1" || r.body["format"] != "ndjson" {
		t.Fatalf("202 + mã tệp + 2 lô: %v %s", r.header, r.raw)
	}
	if r = e.call(http.MethodGet, "/v1/files/"+fid+"/results", key, nil); r.status != http.StatusConflict || r.code() != "integration.file_results_pending" ||
		r.header.Get("Retry-After") == "" {
		t.Fatalf("chưa xử lý → 409: %d %s", r.status, r.raw)
	}

	e.drain()
	r = e.call(http.MethodGet, "/v1/files/"+fid, key, nil)
	if r.status != http.StatusOK || r.body["status"] != "completed" || r.body["accepted"] != float64(500) || r.body["rejected"] != float64(1) {
		t.Fatalf("tệp completed 500/1: %s", r.raw)
	}
	// Lô của tệp là lô W5.4 bình thường (GET /v1/batches/{id}).
	b0 := r.body["batches"].([]any)[0].(map[string]any)
	if rb := e.call(http.MethodGet, "/v1/batches/"+b0["id"].(string), key, nil); rb.body["items"] != float64(500) || rb.body["batch_ref"] != b0["batch_ref"] {
		t.Fatalf("lô 0 của tệp: %s", rb.raw)
	}
	res := e.call(http.MethodGet, "/v1/files/"+fid+"/results", key, nil)
	want(t, res, http.StatusOK, "tệp kết quả")
	lines := strings.Split(strings.TrimSpace(string(res.raw)), "\n")
	if len(lines) != 501 || !strings.HasPrefix(res.header.Get("Content-Type"), "application/x-ndjson") ||
		!strings.Contains(res.header.Get("Content-Disposition"), fid+".results.ndjson") {
		t.Fatalf("NDJSON kết quả 501 dòng: %d %v", len(lines), res.header)
	}
	if !strings.Contains(lines[6], `"record":7`) || !strings.Contains(lines[6], `"integration.record_invalid"`) || strings.Contains(string(res.raw), "987654") {
		t.Fatalf("bản ghi 7 bị từ chối, không lộ giá trị bản ghi: %s", lines[6])
	}
	csvRes := e.call(http.MethodGet, "/v1/files/"+fid+"/results?format=csv", key, nil)
	if !strings.HasPrefix(string(csvRes.raw), "record,entity_key,outcome,problem_code,detail\n1,") || strings.Count(string(csvRes.raw), "\n") != 502 {
		t.Fatalf("CSV kết quả: %.200s", csvRes.raw)
	}

	// Tải lại cùng tệp → cùng mã tệp, Idempotent-Replayed, không thêm sổ tệp/lô.
	r = e.upload(key, "roll_count_statement", q, ndjson, file)
	if r.status != http.StatusAccepted || r.body["id"] != fid || r.header.Get(idempotency.ReplayedHeader) != "true" || r.body["status"] != "completed" {
		t.Fatalf("tải lại: %d %v %s", r.status, r.header, r.raw)
	}
	if n := e.count(partner, `SELECT count(*) FROM pf_apiclient.ingest_file`); n != 1 {
		t.Fatalf("một dòng sổ tệp: %d", n)
	}
	if n := e.count(partner, `SELECT count(*) FROM pf_apiclient.ingest_batch`); n != 2 {
		t.Fatalf("hai lô (tải lại không ghi thêm): %d", n)
	}

	// CSV cùng lược đồ: ô sai kiểu → bản ghi bị từ chối bởi hợp đồng (cùng đường kiểm), không phải cả tệp.
	ref1, ref2 := ids.New().String(), ids.New().String()
	csvFile := "statement_ref;schedule_ref;htx_ref;plot_ref;season_ref;collected_on;rolls;revision\n" +
		ref1 + ";;01989e00-1111-7222-8333-944455566677;01989f00-aa11-7b22-8c33-d44e55f66a77;;2026-10-09;46;1\n" +
		ref2 + ";;01989e00-1111-7222-8333-944455566677;01989f00-aa11-7b22-8c33-d44e55f66a77;;2026-10-09;bốn mươi;1\n"
	r = e.upload(key, "roll_count_statement", q, "text/csv; charset=utf-8", []byte("\xEF\xBB\xBF"+csvFile))
	want(t, r, http.StatusAccepted, "nhận CSV")
	cfid := r.body["id"].(string)
	e.drain()
	res = e.call(http.MethodGet, "/v1/files/"+cfid+"/results", key, nil)
	if !strings.HasPrefix(res.header.Get("Content-Type"), "text/csv") ||
		!strings.Contains(string(res.raw), "1,"+ref1+",accepted,,") || !strings.Contains(string(res.raw), "2,"+ref2+",rejected,integration.record_invalid") {
		t.Fatalf("CSV kết quả: %s", res.raw)
	}

	// Cách ly: khách hàng khác không đọc được tệp.
	_, keyB := e.onboardIngest(ids.New(), "roll_count_statement")
	if r = e.call(http.MethodGet, "/v1/files/"+fid, keyB, nil); r.status != http.StatusNotFound || r.code() != "integration.file_not_found" {
		t.Fatalf("đối tác khác → 404: %d %s", r.status, r.raw)
	}

	// Sổ đo lường (điểm kiểm soát trước byte): POST = export_request 0 đơn vị, job_id = mã tệp.
	if n := e.count(e.operator, `SELECT count(*) FROM pf_metering.usage_event WHERE client_id = $1::uuid AND call_kind = 'export_request'
	    AND route_template = 'POST /v1/files/{contract}' AND job_id = $2::uuid AND units = 0`, cid, uuid.MustParse(fid)); n != 2 {
		t.Fatalf("sổ đo lường POST tệp (lần đầu + tải lại): %d", n)
	}
	if n := e.count(e.operator, `SELECT count(*) FROM pf_metering.usage_event WHERE client_id = $1::uuid
	    AND route_template = 'GET /v1/files/{id}/results' AND call_kind = 'job_status'`, cid); n != 4 {
		t.Fatalf("sổ đo lường tải kết quả: %d", n)
	}
}

// Hàng đợi đối tác đầy giữa tệp → 429 (tệp incomplete); rút hàng đợi rồi tải lại CÙNG tệp → gửi tiếp lô còn thiếu.
func TestFileChannelResumeAfterSaturation_W58Integration(t *testing.T) {
	e := newFileEnv(t, fileOpts{maxQueued: 1})
	partner := ids.New()
	_, key := e.onboardIngest(partner, "roll_count_statement")
	file := rollLines(600, -1)
	r := e.upload(key, "roll_count_statement", "?contract_version=1", ndjson, file)
	if r.status != http.StatusTooManyRequests || r.code() != "integration.queue_saturated" || r.header.Get("Retry-After") == "" {
		t.Fatalf("lô 2 của tệp gặp hàng đợi đầy → 429: %d %s", r.status, r.raw)
	}
	var fid string
	if err := tenant.WithTenantTx(context.Background(), e.pg.Pool(t, migrations.PF, dbadmin.Migrator), partner, func(tx pgx.Tx) error {
		return tx.QueryRow(context.Background(), `SELECT id::text FROM pf_apiclient.ingest_file`).Scan(&fid)
	}); err != nil {
		t.Fatalf("sổ tệp ghi trước khi gửi lô: %v", err)
	}
	if r = e.call(http.MethodGet, "/v1/files/"+fid, key, nil); r.body["status"] != "incomplete" {
		t.Fatalf("tệp incomplete: %s", r.raw)
	}
	e.drain()
	r = e.upload(key, "roll_count_statement", "?contract_version=1", ndjson, file)
	if r.status != http.StatusAccepted || r.body["id"] != fid || r.header.Get(idempotency.ReplayedHeader) != "true" {
		t.Fatalf("tải lại gửi tiếp: %d %s", r.status, r.raw)
	}
	e.drain()
	if r = e.call(http.MethodGet, "/v1/files/"+fid, key, nil); r.body["status"] != "completed" || r.body["accepted"] != float64(600) {
		t.Fatalf("tệp completed sau khi gửi tiếp: %s", r.raw)
	}
}

// Môi trường thử của đối tác (W5.8, R-INT-08): cổng thử phát hành/nhận khóa agks_ và gắn AG-Environment: sandbox;
// cổng thật (cùng DB, cùng đơn vị vận hành) từ chối khóa thử; cổng thử từ chối khóa thật.
func TestPartnerSandboxIsolation_W58Integration(t *testing.T) {
	op := ids.New()
	sb := newFileEnv(t, fileOpts{sandbox: true, operator: op})
	prod := newFileEnv(t, fileOpts{operator: op})
	_, sbKey := sb.onboardIngest(ids.New(), "roll_count_statement")
	_, realKey := prod.onboardIngest(ids.New(), "roll_count_statement")
	if !strings.HasPrefix(sbKey, apiclientport.SandboxKeyTokenPrefix) || strings.HasPrefix(realKey, apiclientport.SandboxKeyTokenPrefix) {
		t.Fatalf("tiền tố khóa: thử %.5s, thật %.5s", sbKey, realKey)
	}
	r := sb.upload(sbKey, "roll_count_statement", "?contract_version=1", ndjson, rollLines(3, -1))
	want(t, r, http.StatusAccepted, "kênh tệp ở môi trường thử")
	if r.header.Get(apiclientport.HeaderEnvironment) != apiclientport.EnvironmentSandbox {
		t.Fatalf("AG-Environment: sandbox: %v", r.header)
	}
	sb.drain()
	if r = sb.call(http.MethodGet, "/v1/files/"+r.body["id"].(string), sbKey, nil); r.body["status"] != "completed" {
		t.Fatalf("môi trường thử chạy đủ đường ống: %s", r.raw)
	}
	for name, c := range map[string]struct {
		env *ingestEnv
		key string
	}{"khóa thử ở cổng thật": {prod, sbKey}, "khóa thật ở cổng thử": {sb, realKey}} {
		r := c.env.call(http.MethodGet, "/v1/files/"+ids.New().String(), c.key, nil)
		if r.status != http.StatusUnauthorized || r.code() != apiclientport.ErrUnauthenticated.Code {
			t.Fatalf("%s → 401: %d %s", name, r.status, r.raw)
		}
	}
	if h := prod.call(http.MethodGet, "/v1/files/x", realKey, nil).header.Get(apiclientport.HeaderEnvironment); h != "" {
		t.Fatalf("cổng thật không gắn AG-Environment: %q", h)
	}
}
