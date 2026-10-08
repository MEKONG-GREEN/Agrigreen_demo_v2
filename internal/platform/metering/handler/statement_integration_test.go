package handler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/metering/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/inbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/meter"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/outbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// runStatementJobs giao mọi sự kiện pf.metering.statement.requested chưa xử lý của tenant cho consumer
// (như worker): trả số sự kiện đã xử lý lần này.
func (f *fixture) runStatementJobs(tid uuid.UUID) int {
	f.t.Helper()
	ctx := context.Background()
	var envs []outbox.Envelope
	f.in(tid, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT id, payload FROM shared.outbox WHERE event_type = $1 ORDER BY id`, port.EventStatementRequested)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var e outbox.Envelope
			var raw []byte
			if err := rows.Scan(&e.EventID, &raw); err != nil {
				return err
			}
			e.EventType, e.TenantID, e.SchemaVersion, e.Payload = port.EventStatementRequested, tid, 1, json.RawMessage(raw)
			envs = append(envs, e)
		}
		return rows.Err()
	})
	n := 0
	for _, e := range envs {
		ok, err := inbox.Process(ctx, f.pool, port.StatementConsumer, e, f.mod.StatementHandler())
		if err != nil {
			f.t.Fatal(err)
		}
		if ok {
			n++
		}
	}
	return n
}

// draftAndApprove: officer yêu cầu bảng kê kỳ cho client, worker dựng, approver khác duyệt.
func (f *fixture) draftAndApprove(client uuid.UUID, period string) string {
	f.t.Helper()
	officer := f.as(f.op, port.RoleBillingOfficer)
	r := officer.do("POST", "/v1/metering/statements", map[string]any{"period": period, "client_id": client.String()}, "")
	want(f.t, r, 202, "", "yêu cầu bảng kê")
	f.runStatementJobs(f.op)
	job := officer.do("GET", "/v1/metering/statement-jobs/"+r.str("id"), nil, "")
	ids, _ := job.body["statement_ids"].([]any)
	if job.str("status") != "completed" || len(ids) != 1 {
		f.t.Fatalf("job: %s", job.raw)
	}
	sid := ids[0].(string)
	want(f.t, f.as(f.op, port.RoleBillingApprover).do("POST", "/v1/metering/statements/"+sid+"/approve", nil, ""), 201, "", "duyệt")
	return sid
}

// ADR-0023 mục 4 (M-06, M-07, M-09; E2E-G5-09): bảng kê kỳ tháng — kỳ chưa hết → 409; POST → 202 + job
// (gọi lặp cùng Idempotency-Key → cùng job); worker dựng ảnh chụp bất biến (first/last seq, row_hash cuối
// chuỗi, SHA-256) — giao lặp sự kiện không lập thêm (R-61); người yêu cầu tự duyệt → 403
// metering.self_approval (app/) và CHECK DB; người khác duyệt → approved; duyệt lại → 409 statement_frozen;
// CSV tất định byte-cho-byte, tải qua checkpoint report_export; kiểm lại băm khớp sổ; sửa = bảng kê bù.
func TestStatementLifecycle_SoD_Deterministic_ADR0023(t *testing.T) {
	f := newFixture(t, meter.ModeObserve)
	ctx := context.Background()
	client, ct, dt := ids.New(), ids.New(), ids.New()
	for i := 0; i < 5; i++ {
		if _, err := f.mod.Ledger().Record(ctx, f.op, call(client, ct, dt, oct.Add(time.Duration(i)*time.Hour))); err != nil {
			t.Fatal(err)
		}
	}
	bad := call(client, ct, uuid.Nil, oct)
	bad.Status, bad.ProblemCode = 403, "apiclient.forbidden"
	if _, err := f.mod.Ledger().Record(ctx, f.op, bad); err != nil {
		t.Fatal(err)
	}

	officer := f.as(f.op, port.RoleBillingOfficer)
	both := f.as(f.op, port.RoleBillingOfficer, port.RoleBillingApprover)
	approver := f.as(f.op, port.RoleBillingApprover)
	want(t, officer.do("POST", "/v1/metering/statements", map[string]any{"period": "2026-11"}, ""), 409, "metering.period_not_ended", "kỳ chưa hết")
	want(t, approver.do("POST", "/v1/metering/statements", map[string]any{"period": "2026-10"}, ""), 403, "forbidden", "approver không yêu cầu")
	want(t, officer.do("POST", "/v1/metering/statements", map[string]any{"period": "2026-13"}, ""), 422, "validation_failed", "kỳ sai")

	r := both.do("POST", "/v1/metering/statements", map[string]any{"period": "2026-10"}, "stmt-req-0001")
	want(t, r, 202, "", "yêu cầu bảng kê mọi khách hàng")
	if !strings.HasPrefix(r.hdr.Get("Location"), "/v1/metering/statement-jobs/") || r.str("status") != "queued" {
		t.Fatalf("202 + Location: %v %s", r.hdr, r.raw)
	}
	again := both.do("POST", "/v1/metering/statements", map[string]any{"period": "2026-10"}, "stmt-req-0001")
	if again.str("id") != r.str("id") {
		t.Fatalf("gọi lặp Idempotency-Key phải trả cùng job: %s vs %s", again.raw, r.raw)
	}
	if n := f.runStatementJobs(f.op); n != 1 {
		t.Fatalf("worker xử lý %d sự kiện", n)
	}
	if n := f.runStatementJobs(f.op); n != 0 {
		t.Fatal("giao lặp sự kiện phải bỏ qua (R-61)")
	}
	job := officer.do("GET", "/v1/metering/statement-jobs/"+r.str("id"), nil, "")
	sids, _ := job.body["statement_ids"].([]any)
	if job.str("status") != "completed" || len(sids) != 1 {
		t.Fatalf("job: %s", job.raw)
	}
	sid := sids[0].(string)
	st := officer.do("GET", "/v1/metering/statements/"+sid, nil, "")
	want(t, st, 200, "", "đọc bảng kê")
	if st.str("status") != "draft" || st.str("total_vnd") != "" || num(st.body["first_seq"]) != "1" || num(st.body["last_seq"]) != "6" ||
		len(st.str("snapshot_sha256")) != 64 || len(st.str("chain_head_hash")) != 64 {
		t.Fatalf("bảng kê draft (chưa có giá → chỉ đơn vị): %s", st.raw)
	}
	lines, _ := st.body["lines"].([]any)
	if len(lines) != 2 {
		t.Fatalf("muốn 2 dòng (2xx, 4xx): %s", st.raw)
	}
	if l := lines[0].(map[string]any); l["status_class"] != "2xx" || num(l["units"]) != "5" || l["amount_vnd"] != nil {
		t.Fatalf("dòng 2xx: %v", l)
	}

	// R-56: người yêu cầu (có cả vai approver) tự duyệt → 403 ở app/.
	want(t, both.do("POST", "/v1/metering/statements/"+sid+"/approve", nil, ""), 403, "metering.self_approval", "tự duyệt")
	want(t, officer.do("POST", "/v1/metering/statements/"+sid+"/approve", nil, ""), 403, "forbidden", "officer không duyệt")
	// R-56 ở CHECK DB: ghi thẳng dòng approved với actor = requested_by.
	err := tenantInsertSelfApproval(f, uuid.MustParse(sid), both.actor)
	if err == nil || !strings.Contains(err.Error(), "23514") {
		t.Fatalf("CHECK DB phải chặn tự duyệt: %v", err)
	}

	csv1 := officer.do("GET", "/v1/metering/statements/"+sid+"/export.csv", nil, "")
	want(t, csv1, 200, "", "CSV draft")
	ok := approver.do("POST", "/v1/metering/statements/"+sid+"/approve", nil, "")
	want(t, ok, 201, "", "duyệt")
	if ok.str("status") != "approved" || ok.str("approved_by") != approver.actor.String() {
		t.Fatalf("approved: %s", ok.raw)
	}
	want(t, approver.do("POST", "/v1/metering/statements/"+sid+"/approve", nil, ""), 200, "", "duyệt lặp của chính người duyệt")
	want(t, f.as(f.op, port.RoleBillingApprover).do("POST", "/v1/metering/statements/"+sid+"/approve", nil, ""), 409, "metering.statement_frozen", "đã duyệt")

	a := officer.do("GET", "/v1/metering/statements/"+sid+"/export.csv", nil, "")
	b := approver.do("GET", "/v1/metering/statements/"+sid+"/export.csv", nil, "")
	if a.status != 200 || !bytes.Equal(a.raw, b.raw) || a.hdr.Get("Content-Type") != "text/csv; charset=utf-8" || bytes.Equal(a.raw, csv1.raw) {
		t.Fatalf("CSV phải tất định byte-cho-byte (và ghi trạng thái): %d\n%s\n%s", a.status, a.raw, b.raw)
	}
	if !strings.HasPrefix(string(a.raw), "statement_id,client_id,") || !strings.Contains(string(a.raw), ",_total,,6,5,") {
		t.Fatalf("CSV: %s", a.raw)
	}
	if n := f.count(f.op, `SELECT count(*) FROM pf_metering.egress_event WHERE channel = 'report_export' AND product = 'metering_statement'`); n != 3 {
		t.Fatalf("mỗi lần tải CSV qua checkpoint report_export: %d", n)
	}
	v := officer.do("GET", "/v1/metering/statements/"+sid+"/verify", nil, "")
	if v.body["match"] != true || v.str("stored_sha256") != st.str("snapshot_sha256") {
		t.Fatalf("kiểm lại băm: %s", v.raw)
	}

	// Lời gọi đến muộn của kỳ (sau ảnh chụp) không đổi bảng kê đã duyệt; sửa = bảng kê bù.
	if _, err := f.mod.Ledger().Record(ctx, f.op, call(client, ct, dt, oct.Add(10*time.Hour))); err != nil {
		t.Fatal(err)
	}
	if v := officer.do("GET", "/v1/metering/statements/"+sid+"/verify", nil, ""); v.body["match"] != true {
		t.Fatalf("ảnh chụp cố định theo last_seq: %s", v.raw)
	}
	want(t, officer.do("POST", "/v1/metering/statements", map[string]any{"period": "2026-10", "client_id": client.String(),
		"supersedes_id": sid}, ""), 422, "validation_failed", "bù thiếu lý do")
	corr := officer.do("POST", "/v1/metering/statements", map[string]any{"period": "2026-10", "client_id": client.String(),
		"supersedes_id": sid, "correction_reason": "Bổ sung lời gọi ghi muộn của kỳ 10/2026"}, "")
	want(t, corr, 202, "", "bảng kê bù")
	f.runStatementJobs(f.op)
	cj := officer.do("GET", "/v1/metering/statement-jobs/"+corr.str("id"), nil, "")
	csids, _ := cj.body["statement_ids"].([]any)
	if len(csids) != 1 || csids[0] == sid {
		t.Fatalf("job bù: %s", cj.raw)
	}
	nst := officer.do("GET", "/v1/metering/statements/"+csids[0].(string), nil, "")
	if nst.str("supersedes_id") != sid || num(nst.body["last_seq"]) != "7" || nst.str("status") != "draft" {
		t.Fatalf("bảng kê bù: %s", nst.raw)
	}
	old := officer.do("GET", "/v1/metering/statements/"+sid, nil, "")
	if old.str("superseded_by") != csids[0] || old.str("status") != "approved" {
		t.Fatalf("bảng kê cũ giữ nguyên, chỉ ghi bị thay: %s", old.raw)
	}
	want(t, officer.do("POST", "/v1/metering/statements", map[string]any{"period": "2026-10", "client_id": client.String(),
		"supersedes_id": sid, "correction_reason": "Thay lần hai cùng bảng kê cũ"}, ""), 409, "metering.statement_frozen", "đã bị thay")
	list := approver.do("GET", "/v1/metering/statements?period=2026-10", nil, "")
	if items, _ := list.body["items"].([]any); len(items) != 2 {
		t.Fatalf("danh sách: %s", list.raw)
	}
	// Admin: tổng sử dụng + sổ sử dụng có cursor.
	u := f.as(f.op, port.RoleMeteringViewer).do("GET", "/v1/metering/usage?client_id="+client.String()+"&from=2026-10-01&to=2026-10-31", nil, "")
	if items, _ := u.body["items"].([]any); u.status != 200 || len(items) != 1 {
		t.Fatalf("usage: %s", u.raw)
	}
	ev := f.as(f.op, port.RoleMeteringViewer).do("GET", "/v1/metering/usage-events?limit=4&client_id="+client.String(), nil, "")
	if items, _ := ev.body["items"].([]any); len(items) != 4 || ev.str("next_cursor") == "" {
		t.Fatalf("usage-events trang 1: %s", ev.raw)
	}
	ev2 := f.as(f.op, port.RoleMeteringViewer).do("GET", "/v1/metering/usage-events?limit=4&client_id="+client.String()+"&cursor="+ev.str("next_cursor"), nil, "")
	if items, _ := ev2.body["items"].([]any); len(items) != 3 || ev2.body["next_cursor"] != nil {
		t.Fatalf("usage-events trang 2: %s", ev2.raw)
	}
}

func tenantInsertSelfApproval(f *fixture, sid, actor uuid.UUID) error {
	ctx := context.Background()
	var client, ct, job uuid.UUID
	var period, sum string
	f.in(f.op, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT client_id, client_tenant_id, job_id, period, snapshot_sha256 FROM pf_metering.statement_current WHERE statement_id = $1`,
			sid).Scan(&client, &ct, &job, &period, &sum)
	})
	return tenant.WithTenantTx(ctx, f.pool, f.op, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO pf_metering.statement (id, tenant_id, stream_key, statement_id, action, client_id, client_tenant_id, period,
		    job_id, snapshot_sha256, first_seq, last_seq, chain_head_hash, requested_by, event_time, actor_ref, idem_key, rule_version)
		  VALUES ($1,$2,'x',$3,'approved',$4,$5,$6,$7,$8,1,1,'x',$9,now(),$9,$10,'t')`,
			ids.New(), f.op, sid, client, ct, period, job, sum, actor, "self:"+sid.String())
		return err
	})
}

// ADR-0023 mục 4 + Q5-4 (M-08): giá TÙY CHỌN, có phiên bản (pf/masterdata), shared/money — đơn giá
// numeric(18,4), thành tiền numeric(18,2), thuê bao; price_version ghi phiên bản đã dùng.
func TestStatementOptionalPricing_ADR0023(t *testing.T) {
	f := newFixture(t, meter.ModeObserve)
	ctx := context.Background()
	client, ct, dt := ids.New(), ids.New(), ids.New()
	for i := 0; i < 3; i++ {
		if _, err := f.mod.Ledger().Record(ctx, f.op, call(client, ct, dt, oct.Add(time.Duration(i)*time.Hour))); err != nil {
			t.Fatal(err)
		}
	}
	f.params.set(f.op, "metering.price.default.collection_volume.per_unit_vnd", "1500.5")
	f.params.set(f.op, "metering.price.default.subscription_vnd_per_month", "1000000")
	sid := f.draftAndApprove(client, "2026-10")
	st := f.as(f.op, port.RoleMeteringViewer).do("GET", "/v1/metering/statements/"+sid, nil, "")
	l := st.body["lines"].([]any)[0].(map[string]any)
	if l["unit_price_vnd"] != "1500.5000" || l["amount_vnd"] != "4501.50" || st.str("subscription_vnd") != "1000000.00" ||
		st.str("total_vnd") != "1004501.50" || !strings.Contains(st.str("price_version"), "per_unit_vnd@v1") {
		t.Fatalf("giá: %s", st.raw)
	}
	types := map[string]string{}
	f.in(f.op, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT attname, format_type(atttypid, atttypmod) FROM pg_attribute
		  WHERE attrelid = 'pf_metering.statement_line'::regclass AND attname IN ('unit_price_vnd', 'amount_vnd')`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var n, ty string
			if err := rows.Scan(&n, &ty); err != nil {
				return err
			}
			types[n] = ty
		}
		return rows.Err()
	})
	if types["unit_price_vnd"] != "numeric(18,4)" || types["amount_vnd"] != "numeric(18,2)" {
		t.Fatalf("kiểu cột tiền: %v", types)
	}
}
