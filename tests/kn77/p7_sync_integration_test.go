package kn77_test

// KN v7.7 P7 — nhập báo cáo ngoại tuyến trên field-app Cán bộ xã (FN-XA-03.01/03.02, N-07): pf/sync THẬT (vai sync_app)
// + pf/extension reportcycle (Source report_task/form_layout/report_submission, Sink report_submission) + pf/reports
// (engine mẫu). Phạm vi pull/push = ngữ cảnh phiên (X-AG-Context), không theo org_ref.

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/sync"
	syncport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/sync/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/idempotency"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/telemetry"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

// p7Sync là máy chủ Application API có pf/sync nối Source/Sink báo cáo của pf/extension.
type p7Sync struct {
	*cycle
	srv *httptest.Server
}

func setupP7Sync(t *testing.T) *p7Sync {
	c := setupCycle(t)
	f := c.f
	snc := sync.New(f.env.Pool(t, migrations.PF, sync.DBRole), f.clk, sync.Deps{CursorKey: []byte("kn77-p7-sync-cursor-key-32-bytes!!"),
		Sources: []syncport.Source{f.ext.ReportSyncSource()}, Sinks: []syncport.Sink{f.ext.ReportSyncSink()}})
	eng := httpx.NewEngine(telemetry.NewLogger("test", io.Discard), 8<<20)
	eng.Use(logErrors(t))
	g := eng.Group("/v1", httpx.Authenticate(f.iss.Verifier()), f.knMiddleware(), idempotency.Middleware(f.env.Pool(t, migrations.PF, "platform_api")))
	snc.RegisterApp(g)
	srv := httptest.NewServer(eng)
	t.Cleanup(srv.Close)
	return &p7Sync{cycle: c, srv: srv}
}

// device là thiết bị đã đăng ký của một người gọi.
type device struct {
	s      *p7Sync
	c      caller
	id     string
	seq    int
	cursor string
}

func (s *p7Sync) register(t *testing.T, c caller) *device {
	t.Helper()
	d := &device{s: s, c: c, id: ids.New().String()}
	r := d.do(t, http.MethodPost, "/v1/devices", "devreg:"+d.id, map[string]any{"device_id": d.id, "platform": "android", "app_version": "0.7.0"})
	if r.status != http.StatusCreated {
		t.Fatalf("đăng ký thiết bị: %d %s", r.status, string(r.raw))
	}
	return d
}

func (d *device) do(t *testing.T, method, path, idem string, body any) resp {
	t.Helper()
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, d.s.srv.URL+path, rd)
	req.Header.Set("Authorization", "Bearer "+d.c.token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(syncport.HeaderDeviceID, d.id)
	if d.c.ctxID != uuid.Nil {
		req.Header.Set(authz.ContextHeader, d.c.ctxID.String())
	}
	if idem != "" {
		req.Header.Set("Idempotency-Key", idem)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()
	raw, _ := io.ReadAll(res.Body)
	var m map[string]any
	_ = json.Unmarshal(raw, &m)
	return resp{status: res.StatusCode, body: m, raw: raw}
}

// pullAll kéo hết các trang từ con trỏ hiện tại; trả thay đổi theo entity và cờ reset của trang đầu.
func (d *device) pullAll(t *testing.T, limit string) (map[string][]map[string]any, bool) {
	t.Helper()
	out := map[string][]map[string]any{}
	reset := false
	for i := 0; i < 50; i++ {
		q := "/v1/sync/pull?limit=" + limit
		if d.cursor != "" {
			q += "&cursor=" + d.cursor
		}
		r := d.do(t, http.MethodGet, q, "", nil)
		if r.status != http.StatusOK {
			t.Fatalf("pull: %d %s", r.status, string(r.raw))
		}
		if i == 0 {
			reset, _ = r.body["reset"].(bool)
		}
		for _, ch := range arr(r.body, "changes") {
			out[str(ch, "entity")] = append(out[str(ch, "entity")], ch)
		}
		d.cursor = str(r.body, "next_cursor")
		if more, _ := r.body["has_more"].(bool); !more {
			return out, reset
		}
	}
	t.Fatal("pull không dừng")
	return nil, false
}

// push gửi một lô bản ghi report_submission; trả kết quả theo bản ghi.
func (d *device) push(t *testing.T, recs ...map[string]any) []map[string]any {
	t.Helper()
	r := d.do(t, http.MethodPost, "/v1/sync/push", "batch:"+ids.New().String(), map[string]any{"device_id": d.id, "records": recs})
	if r.status != http.StatusOK {
		t.Fatalf("push: %d %s", r.status, string(r.raw))
	}
	return arr(r.body, "results")
}

func (d *device) record(id, taskID, kind string, base int, input map[string]string) map[string]any {
	d.seq++
	return map[string]any{"id": id, "idem_key": "dev:" + d.id + ":" + itoa(d.seq), "entity": "report_submission", "op": "create",
		"event_time": "2026-09-07T07:30:00+07:00", "payload": map[string]any{"task_id": taskID, "kind": kind, "base_version": base,
			"rows": []any{map[string]any{"input": input}}}}
}

func itoa(n int) string { b, _ := json.Marshal(n); return string(b) }

func byID(chs []map[string]any, id string) map[string]any {
	for _, ch := range chs {
		if str(ch, "id") == id {
			return ch
		}
	}
	return nil
}

// TestFN_XA_03_02_OfflineReportSync (N-07, FN-XA-03.01/03.02): máy Cán bộ xã kéo công việc + bố cục mẫu của các xã phụ
// trách (phòng ban/xã/tenant khác không thấy), lưu nháp ngoại tuyến → push (id UUIDv7 máy sinh) → gửi lại cùng bản ghi
// không trùng → pull nhận ô công thức do máy chủ tính; bản dựa trên phiên bản cũ khi web đã sửa → conflict (giữ cả hai);
// khe kỳ khác → rejected kèm lỗi tại ô; sau chốt kỳ → công việc đi xuống "đã chốt", push bị từ chối; đổi ngữ cảnh → reset.
func TestFN_XA_03_02_OfflineReportSync(t *testing.T) {
	s := setupP7Sync(t)
	c := s.cycle
	f := c.f
	c1, c2, c3 := f.communes89[0], f.communes89[1], f.communes89[2]
	c.issue(t, "T01", "T05")

	d1 := s.register(t, c.xa1)
	got, _ := d1.pullAll(t, "7") // trang nhỏ: vòng chia hạn mức không bỏ đói entity nào
	tasks, layouts := got["report_task"], got["form_layout"]
	if len(tasks) != 8 || len(layouts) != 2 || len(got["report_submission"]) != 0 {
		t.Fatalf("xã 1: 2 xã × 2 biểu × 2 kỳ = 8 công việc, 2 bố cục, 0 bài nộp: %d %d %d", len(tasks), len(layouts), len(got["report_submission"]))
	}
	var t1 map[string]any // T01, xã 1, kỳ 1
	for _, ch := range tasks {
		dt := obj(ch, "data")
		if str(dt, "commune_code") == c3 {
			t.Fatalf("FN-XA-03.02 AC: không thấy xã ngoài phạm vi: %v", dt)
		}
		if str(dt, "form_code") == "T01" && str(dt, "commune_code") == c1 && str(dt, "period_id") == c.p1 {
			t1 = dt
		}
	}
	if t1 == nil || str(t1, "status") != "not_started" || str(t1, "period_label") != "Tuần 1" || len(t1["period_labels"].([]any)) != 2 ||
		str(obj(t1, "provincial"), "D") != "100" || str(t1, "season_label") != "Thu Đông 2026" || t1["open"] != true {
		t.Fatalf("dữ liệu công việc T01 kỳ 1: %v", t1)
	}
	if sl, _ := t1["open_slots"].([]any); len(sl) != 1 || sl[0].(float64) != 1 {
		t.Fatalf("chỉ khe kỳ đang mở nhập được: %v", t1["open_slots"])
	}
	lay := byID(layouts, str(t1, "version_id"))
	if lay == nil || str(obj(lay, "data"), "code") != "T01" || len(arr(obj(obj(lay, "data"), "definition"), "columns")) == 0 {
		t.Fatalf("bố cục T01: %v", lay)
	}
	if again, _ := d1.pullAll(t, "500"); len(again) != 0 {
		t.Fatalf("pull lặp từ con trỏ cuối: không có gì mới: %v", again)
	}

	// Phạm vi: xã khác, phòng ban khác, tenant khác.
	if g, _ := s.register(t, c.xa2).pullAll(t, "500"); len(g["report_task"]) != 4 {
		t.Fatalf("xã 2 chỉ xã %s: %d", c3, len(g["report_task"]))
	}
	if g, _ := s.register(t, c.bvtv).pullAll(t, "500"); len(g["report_task"]) != 0 || len(g["form_layout"]) != 0 {
		t.Fatalf("Cán bộ xã phòng ban khác không nhận: %v", g)
	}
	if g, _ := s.register(t, c.tinh).pullAll(t, "500"); len(g["report_task"]) != 0 {
		t.Fatalf("chỉ ngữ cảnh Cấp Xã nhận công việc qua đồng bộ: %d", len(g["report_task"]))
	}
	other := f.knFor(ids.New(), ids.New(), authz.KNXa, dept, f.p89, []string{c1, c2}, nil)
	if g, _ := s.register(t, other).pullAll(t, "500"); len(g["report_task"]) != 0 {
		t.Fatalf("cách ly tenant: %d", len(g["report_task"]))
	}
	// Thiết bị cũ không gửi X-AG-Context: pull vẫn 200, không có entity báo cáo (không 428).
	legacy := c.xa1
	legacy.ctxID = uuid.Nil
	if g, _ := s.register(t, legacy).pullAll(t, "500"); len(g["report_task"]) != 0 {
		t.Fatalf("không ngữ cảnh: %v", g)
	}

	// Lưu nháp ngoại tuyến → push → accepted; gửi lại cùng bản ghi (lô khác) → duplicate; cùng id, khóa khác → duplicate.
	tid := str(t1, "id")
	subA := ids.New().String()
	recA := d1.record(subA, tid, "draft", 0, map[string]string{"F": "10"})
	if r := d1.push(t, recA); str(r[0], "status") != "accepted" {
		t.Fatalf("nháp ngoại tuyến: %v", r)
	}
	if r := d1.push(t, recA); str(r[0], "status") != "duplicate" {
		t.Fatalf("gửi lại cùng idem_key: %v", r)
	}
	if r := d1.push(t, d1.record(subA, tid, "draft", 0, map[string]string{"F": "10"})); str(r[0], "status") != "duplicate" {
		t.Fatalf("gửi lại cùng id bài nộp (P4 replay): %v", r)
	}
	if n := f.count(`SELECT count(*) FROM pf_extension.submission_version WHERE tenant_id = $1 AND task_id = $2`, f.tenant, tid); n != 1 {
		t.Fatalf("N-07: không trùng — một phiên bản: %d", n)
	}
	got, _ = d1.pullAll(t, "500")
	ts := byID(got["report_task"], tid)
	sub := byID(got["report_submission"], tid)
	if ts == nil || str(obj(ts, "data"), "status") != "draft" || num(obj(ts, "data"), "latest_version") != 1 {
		t.Fatalf("công việc sau nháp: %v", ts)
	}
	if sub == nil || str(obj(sub, "data"), "submission_id") != subA || str(obj(sub, "data"), "kind") != "draft" {
		t.Fatalf("bài nộp mới nhất đi xuống: %v", sub)
	}
	cells := obj(arr(obj(sub, "data"), "rows")[0], "cells")
	if str(cells, "M") != "10" || str(cells, "D") != "100" {
		t.Fatalf("ô công thức do máy chủ tính (Lũy kế M), Tỉnh giao D: %v", cells)
	}

	// Web sửa (REST, không thiết bị) → máy gửi bản dựa trên phiên bản 1 → conflict, vẫn lưu thành phiên bản 3 (RB-1).
	c.xa1.must(submit(c.xa1, tid, "draft", map[string]string{"F": "12"}), http.StatusCreated, "web lưu nháp")
	if r := d1.push(t, d1.record(ids.New().String(), tid, "draft", 1, map[string]string{"F": "11"})); str(r[0], "status") != "conflict" || str(r[0], "conflict_id") == "" {
		t.Fatalf("xung đột phiên bản: %v", r)
	}
	if n := f.count(`SELECT count(*) FROM pf_extension.submission_version WHERE tenant_id = $1 AND task_id = $2`, f.tenant, tid); n != 3 {
		t.Fatalf("giữ cả hai phiên bản: %d", n)
	}
	if n := f.count(`SELECT count(*) FROM pf_sync.conflict WHERE tenant_id = $1 AND entity = 'report_submission'`, f.tenant); n != 1 {
		t.Fatalf("pf/sync mở xung đột: %d", n)
	}
	// Hai bản liên tiếp của CÙNG máy (ngoại tuyến) không phải xung đột.
	got, _ = d1.pullAll(t, "500")
	base := int(num(obj(byID(got["report_task"], tid), "data"), "latest_version"))
	r := d1.push(t, d1.record(ids.New().String(), tid, "draft", base, map[string]string{"F": "13"}), d1.record(ids.New().String(), tid, "draft", base, map[string]string{"F": "14"}))
	if str(r[0], "status") != "accepted" || str(r[1], "status") != "accepted" {
		t.Fatalf("chuỗi bản của cùng máy: %v", r)
	}
	// Khe kỳ khác (G = Tuần 2) khi Gửi → rejected kèm lỗi tại ô.
	r = d1.push(t, d1.record(ids.New().String(), tid, "submitted", base+2, map[string]string{"F": "20", "G": "5"}))
	if str(r[0], "status") != "rejected" || str(r[0], "code") != "extension.submission_invalid" || !strings.Contains(mustJSON(r[0]), ".G") {
		t.Fatalf("vi phạm mẫu → không cho gửi, lỗi tại ô: %v", r)
	}
	if r := d1.push(t, d1.record(ids.New().String(), tid, "submitted", base+2, map[string]string{"F": "20"})); str(r[0], "status") != "accepted" {
		t.Fatalf("gửi ngoại tuyến: %v", r)
	}
	got, _ = d1.pullAll(t, "500")
	if st := str(obj(byID(got["report_task"], tid), "data"), "status"); st != "submitted" {
		t.Fatalf("FN-XA-03.01: Đã nộp sau đồng bộ: %s", st)
	}

	// Chốt kỳ → công việc đi xuống "đã chốt"; push sau chốt bị từ chối.
	c.tinh.must(c.tinh.post("/v1/extension/report-periods/"+c.p1+"/close", map[string]any{}), http.StatusOK, "chốt kỳ")
	got, _ = d1.pullAll(t, "500")
	closed := obj(byID(got["report_task"], tid), "data")
	if closed == nil || closed["period_closed"] != true || closed["open"] != false || str(closed, "display_status") != "closed" {
		t.Fatalf("Đã chốt đi xuống máy: %v", closed)
	}
	if r := d1.push(t, d1.record(ids.New().String(), tid, "submitted", base+3, map[string]string{"F": "21"})); str(r[0], "code") != "extension.period_closed" {
		t.Fatalf("gửi sau chốt: %v", r)
	}

	// Đổi ngữ cảnh (xã phụ trách khác) → con trỏ cũ reset.
	sw := d1.c
	sw.ctxID = f.knFor(f.tenant, c.xa1User, authz.KNXa, dept, f.p89, []string{c1}, nil).ctxID
	d1.c = sw
	if g, reset := d1.pullAll(t, "500"); !reset || len(g["report_task"]) != 4 {
		t.Fatalf("đổi ngữ cảnh → reset, chỉ xã %s: %v %d", c1, reset, len(g["report_task"]))
	}
}

func mustJSON(v any) string { b, _ := json.Marshal(v); return string(b) }
