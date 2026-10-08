package public_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/contracts/dataproduct"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/apiclient/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/apiclient/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/apiclient/public"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/meter"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/objstore"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/outbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

func TestMain(m *testing.M) { os.Exit(pgtest.Run(m)) }

var otherKey = "agk_mnopqrstuvwx_" + strings.Repeat("C", 43)

// R-52, R-51 (P-13, P-14, D-04, E2E-G5-05): xuất 202 + Location; gọi lặp cùng Idempotency-Key → đúng
// phản hồi đầu, 0 đơn vị; worker dựng tệp tất định; tải tệp phát luồng qua API (Digest, đơn vị theo
// dòng); job của khách hàng khác → 404; thu hồi cấp quyền → 403 ở lần tải kế tiếp.
func TestExportJobFlow_ADR0022(t *testing.T) {
	pool := pgtest.Get(t).Pool(t, migrations.PF, port.DBRole)
	store, err := objstore.NewLocal(t.TempDir(), objstore.LocalOptions{})
	if err != nil {
		t.Fatal(err)
	}
	var svc *app.Service
	e := newEnv(t, func(d *public.Deps) {
		svc = &app.Service{DB: pool, Clock: d.Clock, OperatorTenant: d.OperatorTenant, Store: store}
		d.Svc = svc
		other := port.Client{ID: ids.New(), ClientTenantID: ids.New(), Status: port.ClientActive,
			Scopes: []string{"dp:collection_volume:read", "dp:collection_volume:export"}}
		d.Auth.(fakeAuth).callers[otherKey] = other
	})
	svc.Collection = e.coll
	op := e.client.OperatorTenant
	body := []byte(`{"format":"csv","season_code":"2026-HT"}`)
	path := "/v1/data/collection-volumes/exports"

	r1 := e.do(http.MethodPost, path, e.key, body, "Idempotency-Key", "exp-0001-abc")
	if r1.status != http.StatusAccepted || !strings.HasPrefix(r1.header.Get("Location"), "/v1/jobs/") || r1.body["status"] != "queued" {
		t.Fatalf("muốn 202 + Location: %d %v %s", r1.status, r1.header, r1.raw)
	}
	jobID := r1.body["id"].(string)
	r2 := e.do(http.MethodPost, path, e.key, body, "Idempotency-Key", "exp-0001-abc")
	if r2.status != http.StatusAccepted || !bytes.Equal(r1.raw, r2.raw) || r2.header.Get("Idempotent-Replayed") != "true" {
		t.Fatalf("gọi lặp phải trả đúng phản hồi đầu: %s / %s", r1.raw, r2.raw)
	}
	r3 := e.do(http.MethodPost, path, e.key, []byte(`{"format":"json"}`), "Idempotency-Key", "exp-0001-abc")
	if r3.status != http.StatusUnprocessableEntity || r3.code() != "idempotency_key_reused" {
		t.Fatalf("cùng khóa khác thân → 422: %d %s", r3.status, r3.raw)
	}
	calls, units := e.meter.snapshot()
	if len(calls) != 3 || units != 0 || !calls[1].Replayed || calls[0].Replayed || calls[0].Kind != meter.CallExportRequest {
		t.Fatalf("sổ yêu cầu xuất: %+v", calls)
	}

	// Chưa xong → trạng thái queued, tải tệp 409.
	if r := e.do(http.MethodGet, "/v1/jobs/"+jobID, e.key, nil); r.status != 200 || r.body["status"] != "queued" || r.body["file_url"] != nil {
		t.Fatalf("job queued: %d %s", r.status, r.raw)
	}
	if r := e.do(http.MethodGet, "/v1/jobs/"+jobID+"/file", e.key, nil); r.status != 409 || r.code() != app.ErrJobNotReady.Code {
		t.Fatalf("tệp chưa có → 409: %d %s", r.status, r.raw)
	}

	// Worker (consumer pf.apiclient.export): đọc sự kiện từ outbox, dựng tệp.
	ctx := context.Background()
	var env outbox.Envelope
	err = tenant.WithTenantTx(ctx, pool, op, func(tx pgx.Tx) error {
		var payload []byte
		if err := tx.QueryRow(ctx, `SELECT id, payload::text FROM shared.outbox WHERE tenant_id = $1::uuid AND event_type = $2::text`, op, port.EventExportRequested).
			Scan(&env.EventID, &payload); err != nil {
			return err
		}
		env.EventType, env.TenantID, env.Payload = port.EventExportRequested, op, payload
		return svc.HandleExportRequested(ctx, tx, env)
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(env.Payload), jobID) || strings.Contains(string(env.Payload), e.key) {
		t.Fatalf("payload sự kiện chỉ có mã tham chiếu: %s", env.Payload)
	}
	if e.coll.last.K != 10 || e.coll.last.SeasonCode != "2026-HT" || e.coll.last.DataTenant != e.grantor {
		t.Fatalf("worker đọc ảnh chụp đầu vào sai: %+v", e.coll.last)
	}
	// Giao lặp → không làm gì.
	if err := tenant.WithTenantTx(ctx, pool, op, func(tx pgx.Tx) error { return svc.HandleExportRequested(ctx, tx, env) }); err != nil {
		t.Fatal(err)
	}

	jr := e.do(http.MethodGet, "/v1/jobs/"+jobID, e.key, nil)
	if jr.body["status"] != "completed" || jr.body["rows"] != float64(2) || jr.body["file_url"] != "/v1/jobs/"+jobID+"/file" {
		t.Fatalf("job completed: %s", jr.raw)
	}
	fr := e.do(http.MethodGet, "/v1/jobs/"+jobID+"/file", e.key, nil)
	if fr.status != 200 || !strings.HasPrefix(fr.header.Get("Content-Type"), "text/csv") || fr.header.Get(port.HeaderDataContract) != "collection_volume.v1" {
		t.Fatalf("tải tệp: %d %v", fr.status, fr.header)
	}
	sum := sha256.Sum256(fr.raw)
	if fr.header.Get("Digest") != "sha-256="+base64.StdEncoding.EncodeToString(sum[:]) || jr.body["sha256"] == nil {
		t.Fatalf("Digest không khớp thân")
	}
	if lines := strings.Split(strings.TrimSpace(string(fr.raw)), "\n"); len(lines) != 3 || !strings.HasPrefix(lines[0], "province_code,commune_code") {
		t.Fatalf("CSV: %q", fr.raw)
	}
	calls, units = e.meter.snapshot()
	last := calls[len(calls)-1]
	if last.Kind != meter.CallFileDownload || last.Rows != 2 || last.DataTenantID != e.grantor || meter.UnitsV1(last) != 1 || last.Bytes != int64(len(fr.raw)) {
		t.Fatalf("dòng sổ tải tệp: %+v", last)
	}
	if units != 1 { // yêu cầu xuất, gọi lặp, trạng thái job = 0; tải tệp = ceil(2/1000) = 1 (metering.units.v1)
		t.Fatalf("tổng đơn vị muốn 1, nhận %d", units)
	}

	// Khách hàng khác không thấy job.
	if r := e.do(http.MethodGet, "/v1/jobs/"+jobID, otherKey, nil); r.status != 404 || r.code() != app.ErrJobNotFound.Code {
		t.Fatalf("job của khách hàng khác → 404: %d %s", r.status, r.raw)
	}
	// Thu hồi cấp quyền → tải lại bị 403.
	e.grants.revoke(e.thirdParty, partyport.KindDPCollectionVolume)
	if r := e.do(http.MethodGet, "/v1/jobs/"+jobID+"/file", e.key, nil); r.status != 403 {
		t.Fatalf("sau thu hồi cấp quyền tải tệp → 403, nhận %d", r.status)
	}
}

// D-04, ADR-0022 mục 8: vượt max_export_rows → job failed apiclient.export_too_large; tệp tất định.
func TestExportTooLargeAndDeterministic_ADR0022(t *testing.T) {
	p, err := dataproduct.Get(port.ProductCollectionVolume)
	if err != nil {
		t.Fatal(err)
	}
	items := []port.CollectionVolumeV1{cell("30001"), cell("30002")}
	a, err := app.RenderExport(p, "json", items)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := app.RenderExport(p, "json", items)
	if !bytes.Equal(a, b) || !json.Valid(a) {
		t.Fatal("tệp JSON phải tất định và hợp lệ")
	}

	pool := pgtest.Get(t).Pool(t, migrations.PF, port.DBRole)
	store, _ := objstore.NewLocal(t.TempDir(), objstore.LocalOptions{})
	var svc *app.Service
	e := newEnv(t, func(d *public.Deps) {
		d.Plans = fakePlans{plan: port.Plan{RequestsPerMinute: 60, DailyUnits: 100, MonthlyUnits: 100, MaxExportRows: 1}}
		svc = &app.Service{DB: pool, Clock: d.Clock, OperatorTenant: d.OperatorTenant, Store: store}
		d.Svc = svc
	})
	svc.Collection = e.coll
	r := e.do(http.MethodPost, "/v1/data/collection-volumes/exports", e.key, []byte(`{"format":"json"}`), "Idempotency-Key", "exp-big-0001")
	if r.status != 202 {
		t.Fatalf("202: %d %s", r.status, r.raw)
	}
	jobID, _ := uuid.Parse(r.body["id"].(string))
	ctx := context.Background()
	op := e.client.OperatorTenant
	payload, _ := json.Marshal(port.ExportRequestedV1{JobID: jobID.String()})
	if err := tenant.WithTenantTx(ctx, pool, op, func(tx pgx.Tx) error {
		return svc.HandleExportRequested(ctx, tx, outbox.Envelope{EventType: port.EventExportRequested, TenantID: op, Payload: payload})
	}); err != nil {
		t.Fatal(err)
	}
	jr := e.do(http.MethodGet, "/v1/jobs/"+jobID.String(), e.key, nil)
	if jr.body["status"] != "failed" || jr.body["error_code"] != port.ErrExportTooLarge.Code {
		t.Fatalf("muốn failed export_too_large: %s", jr.raw)
	}
}
