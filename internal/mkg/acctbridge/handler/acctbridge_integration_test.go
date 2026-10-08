package handler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/acctbridge"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/acctbridge/adapter"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/acctbridge/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/acctbridge/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/acctbridge/port"
	cp "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/costing/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz/authztest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/objstore"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/outbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
	"github.com/jackc/pgx/v5/pgconn"
)

func jobOf(t *testing.T, c caller, id string) map[string]any {
	t.Helper()
	r := c.get("/v1/acctbridge/jobs/" + id)
	mustStatus(t, r, http.StatusOK, "đọc job")
	return r.body
}

// B-01, B-02, B-03, B-06, B-08: kỳ đóng → job dựng (idempotent) → bút toán tổng hợp cân, source_hash = băm
// kết quả costing → duyệt → đẩy qua bộ xuất tệp (kho đối tượng) → journal.pushed nội bộ; tệp tải về = tệp đã
// ghi, byte-cho-byte.
func TestAcctbridgeFlow_B01_B08(t *testing.T) {
	st, err := objstore.NewLocal(t.TempDir(), objstore.LocalOptions{})
	if err != nil {
		t.Fatal(err)
	}
	f := newFixture(t, nil)
	f.svc.Adapter = &adapter.FileExport{Store: st, Clock: f.clk}
	ctx := context.Background()
	f.approvedMapping(f.tenant, nil)
	period, closer := ids.New(), ids.New()
	run, eid := f.closePeriod(f.tenant, period, closer)
	// R-61: giao lặp cùng sự kiện → như một lần; sự kiện khác cùng (kỳ, lần đóng) → vẫn một job.
	s, _ := f.periods.ClosedPeriod(ctx, f.tenant, period)
	if f.deliverClosed(f.tenant, s, closer, eid) {
		t.Fatal("giao lặp phải bị inbox bỏ qua")
	}
	f.deliverClosed(f.tenant, s, closer, ids.New())
	f.inTenant(f.tenant, func(tx pgx.Tx) error {
		var n int
		if err := tx.QueryRow(ctx, "SELECT count(*) FROM mkg_acctbridge.job WHERE kind = 'build' AND period_id = $1", period).Scan(&n); err != nil {
			return err
		}
		if n != 1 {
			t.Fatalf("B-02: một job dựng cho (kỳ, lần đóng, ánh xạ), có %d", n)
		}
		return nil
	})
	if n := f.drain(f.tenant); n != 1 {
		t.Fatalf("worker xử lý %d sự kiện", n)
	}
	op, chief := f.operator(), f.chief()
	g := f.journalOf(op, period)
	j := g["journal"].(map[string]any)
	if str(j, "status") != "built" || str(j, "source_hash") != s.ResultSetSHA256 || str(j, "mapping_version") != "1" ||
		str(j, "total_debit") != str(j, "total_credit") || str(j, "total_debit") != "450100000.00" || str(j, "built_by") != closer.String() ||
		str(j, "period_code") != "2026-09" || str(j, "currency") != "VND" {
		t.Fatalf("bút toán dựng: %s", mustJSON(j))
	}
	lines := arr(j, "lines")
	if len(lines) != 8 || str(lines[0], "side") != "debit" || str(lines[len(lines)-1], "side") != "credit" {
		t.Fatalf("dòng: %s", mustJSON(lines))
	}
	// Chỉ tổng (B-01): dòng không mang bản ghi nguồn; số dòng ≤ 2 × dòng tổng hợp của costing.
	for _, l := range lines {
		if _, ok := l["entries"]; ok {
			t.Fatal("dòng bút toán không được mang chi tiết nguồn")
		}
	}
	jid := str(j, "journal_id")
	// Duyệt (bề mặt quản trị, MFA) → 202 + job đẩy lần 1.
	a := chief.adminPost("/v1/acctbridge/journals/"+jid+"/approve", nil)
	mustStatus(t, a, http.StatusAccepted, "duyệt bút toán")
	pushJob := str(a.body, "job", "job_id")
	if str(a.body, "job", "kind") != "push" || str(a.body, "job", "attempt") != "1" || str(a.body, "journal", "status") != "approved" {
		t.Fatalf("duyệt: %s", a.raw)
	}
	if n := f.drain(f.tenant); n != 1 {
		t.Fatalf("worker đẩy %d", n)
	}
	g = f.journalOf(op, period)
	j = g["journal"].(map[string]any)
	if str(j, "status") != "pushed" || str(j, "adapter") != adapter.NameFileExport || !strings.HasPrefix(str(j, "external_ref"), "acctbridge/"+f.tenant.String()+"/2026-09_") {
		t.Fatalf("đã đẩy: %s", mustJSON(j))
	}
	if atts := arr(g, "push_attempts"); len(atts) != 1 || str(atts[0], "outcome") != "succeeded" {
		t.Fatalf("lần đẩy: %s", mustJSON(atts))
	}
	if str(jobOf(t, op, pushJob), "job", "status") != "done" {
		t.Fatal("job đẩy phải done")
	}
	// Tệp tải về (API) = tệp đã ghi ở kho đối tượng, byte-cho-byte; tệp ký khớp artifact_sha256.
	for _, format := range []string{"csv", "xml"} {
		x := op.get("/v1/acctbridge/journals/" + jid + "/export?format=" + format)
		mustStatus(t, x, http.StatusOK, "tải "+format)
		rc, _, err := st.Get(ctx, str(j, "external_ref")+"."+format)
		if err != nil {
			t.Fatal(err)
		}
		stored, _ := io.ReadAll(rc)
		_ = rc.Close()
		if !bytes.Equal(stored, x.raw) || x.header.Get("X-Content-SHA256") != domain.SHA256Hex(stored) {
			t.Fatalf("%s tải về khác tệp đã đẩy", format)
		}
	}
	mustCode(t, op.get("/v1/acctbridge/journals/"+jid+"/export?format=pdf"), http.StatusUnprocessableEntity, "acctbridge.format_unsupported", "định dạng lạ")
	// B-08: journal.pushed chỉ đi vhost nội bộ, payload đúng hợp đồng.
	f.inTenant(f.tenant, func(tx pgx.Tx) error {
		var kind string
		var payload []byte
		if err := tx.QueryRow(ctx, "SELECT kind, payload FROM shared.outbox WHERE event_type = $1", port.EventJournalPushed).Scan(&kind, &payload); err != nil {
			return err
		}
		var p port.JournalPushedV1
		if err := json.Unmarshal(payload, &p); err != nil {
			return err
		}
		if kind != "internal" || p.JournalID.String() != jid || p.PeriodID != period || p.CloseRunID != run || p.SourceHash != s.ResultSetSHA256 ||
			p.Attempt != 1 || p.MappingVersion != 1 || p.LineCount != 8 || !p.TotalDebit.Equal(p.TotalCredit) || p.Adapter != adapter.NameFileExport {
			t.Fatalf("journal.pushed: %s %s", kind, payload)
		}
		var plat int
		if err := tx.QueryRow(ctx, "SELECT count(*) FROM shared.outbox WHERE producer = 'mkg/acctbridge' AND kind <> 'internal'").Scan(&plat); err != nil {
			return err
		}
		if plat != 0 {
			t.Fatal("acctbridge không bao giờ phát sự kiện nền tảng (bút toán không qua van)")
		}
		return nil
	})
}

func authzToken(tid uuid.UUID, roles ...string) authztest.Token {
	return authztest.Token{Subject: ids.New(), TenantID: tid, Roles: roles}
}

func isCheck(err error) bool {
	var pg *pgconn.PgError
	return errors.As(err, &pg) && pg.Code == "23514"
}

func sameTime(s string, want time.Time) bool {
	got, err := time.Parse(time.RFC3339Nano, s)
	return err == nil && got.Equal(want)
}

func mustJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

// B-03: cùng kết quả costing + cùng ánh xạ → cùng dòng và cùng băm dòng ở hai tenant khác nhau; source_hash =
// result_set_sha256 của costing (không tự tính lại).
func TestAcctbridgeSourceHashStable_B03(t *testing.T) {
	f := newFixture(t, nil)
	t2 := ids.New()
	var hashes, sources []string
	sum := summary(ids.New(), ids.New(), hashOf("x"))
	for _, tid := range []uuid.UUID{f.tenant, t2} {
		f.approvedMapping(tid, nil)
		s := sum
		f.periods.put(tid, s)
		f.deliverClosed(tid, s, ids.New(), ids.New())
		f.drain(tid)
		j := f.journalOf(f.as(tid, port.RoleChiefAccountant), s.PeriodID)["journal"].(map[string]any)
		hashes = append(hashes, str(j, "lines_sha256"))
		sources = append(sources, str(j, "source_hash"))
	}
	if hashes[0] != hashes[1] || sources[0] != sum.ResultSetSHA256 || sources[1] != sum.ResultSetSHA256 {
		t.Fatalf("băm không ổn định: %v %v", hashes, sources)
	}
}

// B-04 (R-56): người dựng không duyệt được bút toán của mình (app/ 403 + CHECK DB); người đề xuất ánh xạ
// không duyệt được; vai sai → 403.
func TestAcctbridgeSoD_B04(t *testing.T) {
	f := newFixture(t, nil)
	ctx := context.Background()
	// Người đề xuất ánh xạ có cả hai vai vẫn không tự duyệt được.
	both := f.as(f.tenant, port.RoleAccountingBridgeOperator, port.RoleChiefAccountant)
	r := both.adminPost("/v1/acctbridge/mappings", map[string]any{"reason": "Bảng ánh xạ tạm theo VAS, chờ khách chốt", "effective_from": "2026-01-01"})
	mustStatus(t, r, http.StatusCreated, "đề xuất")
	mustCode(t, both.adminPost("/v1/acctbridge/mappings/"+str(r.body, "mapping", "version_id")+"/decision", map[string]any{"decision": "approved"}),
		http.StatusForbidden, "self_approval_forbidden", "tự duyệt ánh xạ")
	mustCode(t, f.operator().adminPost("/v1/acctbridge/mappings/"+str(r.body, "mapping", "version_id")+"/decision", map[string]any{"decision": "approved"}),
		http.StatusForbidden, "forbidden", "vận hành không duyệt ánh xạ")
	d := f.chief().adminPost("/v1/acctbridge/mappings/"+str(r.body, "mapping", "version_id")+"/decision", map[string]any{"decision": "approved"})
	mustStatus(t, d, http.StatusCreated, "kế toán trưởng khác duyệt")
	// Người đóng kỳ (người dựng tự động) có vai kế toán trưởng: không duyệt được bút toán của kỳ mình đóng.
	closer := f.as(f.tenant, port.RoleChiefAccountant)
	period := ids.New()
	f.closePeriod(f.tenant, period, closer.actor)
	f.drain(f.tenant)
	jid := str(f.journalOf(closer, period)["journal"].(map[string]any), "journal_id")
	mustCode(t, closer.adminPost("/v1/acctbridge/journals/"+jid+"/approve", nil), http.StatusForbidden, "self_approval_forbidden", "tự duyệt bút toán")
	mustCode(t, f.operator().adminPost("/v1/acctbridge/journals/"+jid+"/approve", nil), http.StatusForbidden, "forbidden", "vận hành không duyệt")
	// Bề mặt quản trị bắt buộc MFA.
	noMFA := caller{f: f, tid: f.tenant, token: f.iss.Sign(t, authzToken(f.tenant, port.RoleChiefAccountant))}
	if r := noMFA.adminPost("/v1/acctbridge/journals/"+jid+"/approve", nil); r.status != http.StatusForbidden {
		t.Fatalf("thiếu MFA: %d %s", r.status, r.raw)
	}
	// CHECK DB lớp hai: ghi thẳng sự kiện duyệt / bảng chiếu với người duyệt = người dựng → từ chối.
	jidU := uuid.MustParse(jid)
	f.inTenant(f.tenant, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, "SAVEPOINT a"); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, "UPDATE mkg_acctbridge.journal_current SET status = 'approved', approved_by = built_by, approved_at = now() WHERE journal_id = $1", jidU)
		if !isCheck(err) {
			t.Fatalf("CHECK R-56 phải chặn approved_by = built_by: %v", err)
		}
		if _, err := tx.Exec(ctx, "ROLLBACK TO SAVEPOINT a"); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `INSERT INTO mkg_acctbridge.journal_event (id, tenant_id, stream_key, journal_id, period_id, kind, version, period_code,
		    close_run_id, source_hash, lines_sha256, mapping_version, currency, line_count, total_debit, total_credit, built_by, event_time, actor_ref,
		    idem_key, rule_version)
		  SELECT $1, tenant_id, 'period:' || period_id, journal_id, period_id, 'approved', version, period_code, close_run_id, source_hash,
		    lines_sha256, mapping_version, currency, line_count, total_debit, total_credit, built_by, now(), built_by, 'selfapprove', 'x'
		  FROM mkg_acctbridge.journal_current WHERE journal_id = $2`, ids.New(), jidU)
		if !isCheck(err) {
			t.Fatalf("CHECK R-56 của sổ phải chặn actor_ref = built_by: %v", err)
		}
		_, err = tx.Exec(ctx, "ROLLBACK TO SAVEPOINT a")
		return err
	})
	// Kế toán trưởng khác duyệt được.
	mustStatus(t, f.chief().adminPost("/v1/acctbridge/journals/"+jid+"/approve", nil), http.StatusAccepted, "duyệt bởi người khác")
}

// B-07 (idempotency): đẩy lại → cùng ExternalRef, đích chỉ nhận một lần — kể cả khi giao dịch sau khi đích
// đã nhận bị hỏng (worker giao lại) và khi job đẩy trùng.
func TestAcctbridgePushIdempotent_B07(t *testing.T) {
	f := newFixture(t, nil)
	ctx := context.Background()
	f.approvedMapping(f.tenant, nil)
	period := ids.New()
	f.closePeriod(f.tenant, period, ids.New())
	f.drain(f.tenant)
	jid := str(f.journalOf(f.chief(), period)["journal"].(map[string]any), "journal_id")
	key, chief := key(), f.chief()
	a1 := chief.call(f.admin.URL, http.MethodPost, "/v1/acctbridge/journals/"+jid+"/approve", key, nil)
	mustStatus(t, a1, http.StatusAccepted, "duyệt")
	// Idempotent POST: cùng khóa → phát lại, không job thứ hai.
	a1b := chief.call(f.admin.URL, http.MethodPost, "/v1/acctbridge/journals/"+jid+"/approve", key, nil)
	if a1b.status != http.StatusAccepted && a1b.status != http.StatusOK {
		t.Fatalf("phát lại: %d %s", a1b.status, a1b.raw)
	}
	if str(a1b.body, "job", "job_id") != str(a1.body, "job", "job_id") {
		t.Fatalf("phát lại phải trả cùng job: %s vs %s", a1b.raw, a1.raw)
	}
	// Giao dịch của worker hỏng SAU KHI đích đã nhận → rollback; lần giao lại gọi đích lần nữa với cùng khóa.
	var env []byte
	f.inTenant(f.tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, "SELECT payload FROM shared.outbox WHERE event_type = $1", port.EventJournalPushRequested).Scan(&env)
	})
	var pl port.JournalPushRequestedV1
	if err := json.Unmarshal(env, &pl); err != nil {
		t.Fatal(err)
	}
	boom := errors.New("mất kết nối DB khi commit")
	err := tenant.WithTenantTx(ctx, f.pool, f.tenant, func(tx pgx.Tx) error {
		if err := f.svc.HandlePushRequested(ctx, tx, envelope(f.tenant, port.EventJournalPushRequested, env)); err != nil {
			return err
		}
		return boom
	})
	if !errors.Is(err, boom) || f.fake.Calls() != 1 || f.fake.Accepted() != 1 {
		t.Fatalf("lần 1: %v calls=%d", err, f.fake.Calls())
	}
	f.drain(f.tenant)
	f.drain(f.tenant) // giao lặp
	j := f.journalOf(f.chief(), period)["journal"].(map[string]any)
	if str(j, "status") != "pushed" || f.fake.Calls() != 2 || f.fake.Accepted() != 1 {
		t.Fatalf("đẩy lại: %s calls=%d accepted=%d", mustJSON(j), f.fake.Calls(), f.fake.Accepted())
	}
	ref := str(j, "external_ref")
	if got := f.fake.Received(); len(got) != 1 || got[0].IdempotencyKey != domain.IdempotencyKey(uuid.MustParse(jid)) || got[0].ApprovedBy == uuid.Nil {
		t.Fatalf("bộ giả nhận: %+v", got)
	}
	// Job đẩy thứ hai (vd vận hành bấm hai lần) cho bút toán đã đẩy: không gọi đích, không đổi ExternalRef.
	f.inTenant(f.tenant, func(tx pgx.Tx) error {
		var n int
		if err := tx.QueryRow(ctx, "SELECT count(*) FROM mkg_acctbridge.journal_event WHERE journal_id = $1 AND kind = 'pushed'", uuid.MustParse(jid)).Scan(&n); err != nil {
			return err
		}
		if n != 1 {
			t.Fatalf("một sự kiện pushed, có %d", n)
		}
		return nil
	})
	mustCode(t, f.operator().post("/v1/acctbridge/journals/"+jid+"/retry-push", nil), http.StatusConflict, "acctbridge.journal_not_retryable", "đẩy lại bút toán đã đẩy")
	mustCode(t, f.chief().adminPost("/v1/acctbridge/journals/"+jid+"/approve", nil), http.StatusConflict, "acctbridge.journal_pushed_immutable", "duyệt lại")
	if j2 := f.journalOf(f.chief(), period)["journal"].(map[string]any); str(j2, "external_ref") != ref {
		t.Fatal("ExternalRef đổi")
	}
}

func envelope(tid uuid.UUID, typ string, payload []byte) outbox.Envelope {
	return outbox.Envelope{EventID: ids.New(), EventType: typ, SchemaVersion: 1, TenantID: tid, OccurredAt: time.Now().UTC(), Producer: app.Producer,
		Payload: payload}
}

// R-62: lỗi tạm thời → thử lại có giãn cách (job định kỳ), thành công ở lần sau; lỗi vĩnh viễn → DLQ
// (push_failed) + cảnh báo ngay, không tự thử lại; hết lượt → DLQ; vận hành đẩy lại từ DLQ.
func TestAcctbridgeRetryVersusDLQ_R62(t *testing.T) {
	f := newFixture(t, nil)
	ctx := context.Background()
	f.approvedMapping(f.tenant, nil)
	tenants := func(context.Context) ([]uuid.UUID, error) { return []uuid.UUID{f.tenant}, nil }
	chief, op := f.chief(), f.operator()
	approve := func(period uuid.UUID) string {
		t.Helper()
		f.closePeriod(f.tenant, period, ids.New())
		f.drain(f.tenant)
		jid := str(f.journalOf(chief, period)["journal"].(map[string]any), "journal_id")
		mustStatus(t, chief.adminPost("/v1/acctbridge/journals/"+jid+"/approve", nil), http.StatusAccepted, "duyệt")
		return jid
	}
	// 1) Tạm không nhận hai lần rồi nhận.
	p1 := ids.New()
	f.fake.FailNext(port.ErrAdapterUnavailable, port.ErrAdapterUnavailable)
	approve(p1)
	f.drain(f.tenant)
	j := f.journalOf(op, p1)["journal"].(map[string]any)
	if str(j, "status") != "approved" || str(j, "push_attempts") != "1" || str(j, "last_failure_code") != app.CodeUnavailable ||
		!sameTime(str(j, "next_retry_at"), f.clk.Now().Add(time.Minute)) {
		t.Fatalf("sau lần 1: %s", mustJSON(j))
	}
	if r, err := f.svc.RetryDue(ctx, tenants); err != nil || r.Enqueued != 0 {
		t.Fatalf("chưa đến hạn: %+v %v", r, err)
	}
	// Job định kỳ ở api-internal tìm tenant bằng vai ag_ops_jobs (chỉ ba cột, mkg 000019).
	if _, err := acctbridge.PendingRetryTenants(f.env.Pool(t, migrations.MKG, dbadmin.OpsJobs))(ctx); err != nil {
		t.Fatalf("PendingRetryTenants: %v", err)
	}
	f.clk.Advance(time.Minute)
	if r, err := f.svc.RetryDue(ctx, tenants); err != nil || r.Enqueued != 1 {
		t.Fatalf("đến hạn: %+v %v", r, err)
	}
	if r, _ := f.svc.RetryDue(ctx, tenants); r.Enqueued != 0 {
		t.Fatal("thử lại phải idempotent (hạn đã xóa)")
	}
	f.drain(f.tenant)
	j = f.journalOf(op, p1)["journal"].(map[string]any)
	if str(j, "push_attempts") != "2" || !sameTime(str(j, "next_retry_at"), f.clk.Now().Add(2*time.Minute)) {
		t.Fatalf("giãn cách mũ: %s", mustJSON(j))
	}
	f.clk.Advance(2 * time.Minute)
	if r, _ := f.svc.RetryDue(ctx, tenants); r.Enqueued != 1 {
		t.Fatal("lần 3 phải được xếp")
	}
	f.drain(f.tenant)
	g := f.journalOf(op, p1)
	if j = g["journal"].(map[string]any); str(j, "status") != "pushed" || str(j, "push_attempts") != "3" {
		t.Fatalf("lần 3 nhận: %s", mustJSON(j))
	}
	if atts := arr(g, "push_attempts"); len(atts) != 3 || str(atts[0], "outcome") != "unavailable" || str(atts[2], "outcome") != "succeeded" {
		t.Fatalf("lần đẩy: %s", mustJSON(atts))
	}
	if f.svc.Metrics.Count("attempts:unavailable") != 2 || f.svc.Metrics.Count("dead_lettered:rejected") != 0 {
		t.Fatal("metric thử lại")
	}
	// 2) Từ chối vĩnh viễn → DLQ ngay, không hẹn giờ, cảnh báo; đẩy lại từ DLQ sau khi xử lý nguyên nhân.
	p2 := ids.New()
	f.fake.FailNext(port.ErrAdapterRejected)
	jid2 := approve(p2)
	f.drain(f.tenant)
	j = f.journalOf(op, p2)["journal"].(map[string]any)
	if str(j, "status") != "push_failed" || str(j, "last_failure_code") != app.CodeRejected || j["next_retry_at"] != nil {
		t.Fatalf("DLQ: %s", mustJSON(j))
	}
	if f.svc.Metrics.Count("dead_lettered:rejected") != 1 {
		t.Fatal("R-62: DLQ phải sinh cảnh báo")
	}
	f.clk.Advance(2 * time.Hour)
	if r, _ := f.svc.RetryDue(ctx, tenants); r.Enqueued != 0 {
		t.Fatal("lỗi vĩnh viễn không tự thử lại")
	}
	mustCode(t, chief.post("/v1/acctbridge/journals/"+jid2+"/retry-push", nil), http.StatusForbidden, "forbidden", "đẩy lại cần vai vận hành")
	rp := op.post("/v1/acctbridge/journals/"+jid2+"/retry-push", nil)
	mustStatus(t, rp, http.StatusAccepted, "đẩy lại từ DLQ")
	if str(rp.body, "job", "attempt") != "2" {
		t.Fatalf("đẩy lại: %s", rp.raw)
	}
	f.drain(f.tenant)
	if j = f.journalOf(op, p2)["journal"].(map[string]any); str(j, "status") != "pushed" {
		t.Fatalf("sau đẩy lại: %s", mustJSON(j))
	}
	// 3) Hết lượt (RetryMax = 3) → DLQ retry_exhausted.
	p3 := ids.New()
	f.fake.FailNext(port.ErrAdapterUnavailable, port.ErrAdapterUnavailable, port.ErrAdapterUnavailable)
	approve(p3)
	for i := 0; i < 3; i++ {
		f.drain(f.tenant)
		f.clk.Advance(time.Hour)
		if _, err := f.svc.RetryDue(ctx, tenants); err != nil {
			t.Fatal(err)
		}
	}
	f.drain(f.tenant)
	if j = f.journalOf(op, p3)["journal"].(map[string]any); str(j, "status") != "push_failed" || str(j, "last_failure_code") != app.CodeRetryExhausted ||
		str(j, "push_attempts") != "3" {
		t.Fatalf("hết lượt: %s", mustJSON(j))
	}
	if f.svc.Metrics.Count("dead_lettered:retry_exhausted") != 1 {
		t.Fatal("hết lượt phải cảnh báo")
	}
}

// Sửa sai: bản dựng lại supersedes bản cũ CHỈ TRƯỚC khi đẩy; sau khi đẩy không bao giờ sửa (app/ + trigger
// DB) — điều chỉnh đến từ bút toán điều chỉnh của costing ở kỳ sau.
func TestAcctbridgeCorrectionBeforePushOnly(t *testing.T) {
	f := newFixture(t, nil)
	ctx := context.Background()
	f.approvedMapping(f.tenant, nil)
	op, chief := f.operator(), f.chief()
	period := ids.New()
	f.closePeriod(f.tenant, period, ids.New())
	f.drain(f.tenant)
	v1 := f.journalOf(op, period)["journal"].(map[string]any)
	mustCode(t, op.post("/v1/acctbridge/periods/"+period.String()+"/journal-builds", nil), http.StatusConflict, "acctbridge.rebuild_no_change", "dựng lại không đổi")
	mustCode(t, op.post("/v1/acctbridge/periods/"+ids.New().String()+"/journal-builds", nil), http.StatusNotFound, "acctbridge.period_unknown", "kỳ lạ")
	// Ánh xạ v2 (vận chuyển thủy về 6277) → dựng lại → v2 supersedes v1.
	rules := domain.DefaultRules()
	for i := range rules {
		if rules[i].Category == cp.CatTransportWaterway {
			rules[i].DebitAccount = "6277"
		}
	}
	f.approvedMapping(f.tenant, rules)
	rb := op.post("/v1/acctbridge/periods/"+period.String()+"/journal-builds", nil)
	mustStatus(t, rb, http.StatusAccepted, "dựng lại")
	f.drain(f.tenant)
	if str(jobOf(t, op, str(rb.body, "job", "job_id")), "job", "status") != "done" {
		t.Fatal("job dựng lại phải done")
	}
	v2 := f.journalOf(op, period)["journal"].(map[string]any)
	if str(v2, "version") != "2" || str(v2, "mapping_version") != "2" || str(v2, "supersedes_journal_id") != str(v1, "journal_id") ||
		str(v2, "built_by") != op.actor.String() {
		t.Fatalf("v2: %s", mustJSON(v2))
	}
	old := op.get("/v1/acctbridge/journals/" + str(v1, "journal_id"))
	if str(old.body, "journal", "status") != "superseded" || str(old.body, "journal", "superseded_by") != str(v2, "journal_id") {
		t.Fatalf("v1 phải superseded (giữ nguyên, không xóa): %s", old.raw)
	}
	f.inTenant(f.tenant, func(tx pgx.Tx) error {
		var sup, reason *string
		if err := tx.QueryRow(ctx, `SELECT supersedes_id::text, correction_reason FROM mkg_acctbridge.journal_event WHERE journal_id = $1 AND kind = 'built'`,
			uuid.MustParse(str(v2, "journal_id"))).Scan(&sup, &reason); err != nil {
			return err
		}
		if sup == nil || reason == nil {
			t.Fatal("bản dựng lại phải trỏ supersedes_id + lý do")
		}
		return nil
	})
	mustCode(t, chief.adminPost("/v1/acctbridge/journals/"+str(v1, "journal_id")+"/approve", nil), http.StatusConflict, "acctbridge.journal_not_built", "duyệt bản cũ")
	mustStatus(t, chief.adminPost("/v1/acctbridge/journals/"+str(v2, "journal_id")+"/approve", nil), http.StatusAccepted, "duyệt v2")
	f.drain(f.tenant)
	pushed := f.journalOf(op, period)["journal"].(map[string]any)
	if str(pushed, "status") != "pushed" {
		t.Fatalf("v2 đẩy: %s", mustJSON(pushed))
	}
	// Sau khi đẩy: ánh xạ mới + dựng lại → 409; sự kiện đóng kỳ mới cho kỳ đã đẩy → job lỗi, bút toán giữ nguyên.
	f.approvedMapping(f.tenant, nil)
	mustCode(t, op.post("/v1/acctbridge/periods/"+period.String()+"/journal-builds", nil), http.StatusConflict, "acctbridge.journal_pushed_immutable", "dựng lại sau đẩy")
	f.closePeriod(f.tenant, period, ids.New())
	f.drain(f.tenant)
	f.inTenant(f.tenant, func(tx pgx.Tx) error {
		var n int
		if err := tx.QueryRow(ctx, "SELECT count(*) FROM mkg_acctbridge.job WHERE failure_code = $1", app.CodePushedImmutable).Scan(&n); err != nil {
			return err
		}
		if n != 1 {
			t.Fatalf("job dựng cho kỳ đã đẩy phải lỗi journal_pushed_immutable, có %d", n)
		}
		return nil
	})
	if again := f.journalOf(op, period)["journal"].(map[string]any); str(again, "journal_id") != str(v2, "journal_id") || str(again, "status") != "pushed" {
		t.Fatal("bút toán đã đẩy phải giữ nguyên")
	}
	// Trigger DB: không sửa dòng đã đẩy, kể cả vai owner.
	for _, pool := range []*pgxpool.Pool{f.pool, f.env.Pool(t, migrations.MKG, dbadmin.Migrator)} {
		if err := tenant.WithTenantTx(ctx, pool, f.tenant, func(tx pgx.Tx) error {
			_, err := tx.Exec(ctx, "UPDATE mkg_acctbridge.journal_current SET total_debit = total_debit + 1, total_credit = total_credit + 1 WHERE journal_id = $1",
				uuid.MustParse(str(v2, "journal_id")))
			return err
		}); err == nil {
			t.Fatal("bút toán đã đẩy không được sửa")
		}
	}
}

// B-02: lỗi nghiệp vụ của job dựng ghi mã và ACK; costing chưa nối → lỗi tạm thời (giao lại, DLQ).
func TestAcctbridgeBuildFailures_B02(t *testing.T) {
	f := newFixture(t, nil)
	ctx := context.Background()
	op := f.operator()
	jobCode := func(period uuid.UUID) string {
		var code *string
		f.inTenant(f.tenant, func(tx pgx.Tx) error {
			return tx.QueryRow(ctx, "SELECT failure_code FROM mkg_acctbridge.job WHERE period_id = $1 ORDER BY requested_at DESC, job_id DESC LIMIT 1",
				period).Scan(&code)
		})
		if code == nil {
			return ""
		}
		return *code
	}
	// Chưa có ánh xạ được duyệt.
	p1 := ids.New()
	f.closePeriod(f.tenant, p1, ids.New())
	f.drain(f.tenant)
	if c := jobCode(p1); c != app.CodeMappingMissing {
		t.Fatalf("thiếu ánh xạ: %q", c)
	}
	if f.svc.Metrics.Count("build_failed:"+app.CodeMappingMissing) != 1 {
		t.Fatal("metric build_failed")
	}
	f.approvedMapping(f.tenant, nil)
	// Sau khi có ánh xạ: vận hành yêu cầu dựng lại → thành công.
	mustStatus(t, op.post("/v1/acctbridge/periods/"+p1.String()+"/journal-builds", nil), http.StatusAccepted, "dựng lại sau khi có ánh xạ")
	f.drain(f.tenant)
	if j := f.journalOf(op, p1)["journal"].(map[string]any); str(j, "status") != "built" {
		t.Fatal("dựng sau khi có ánh xạ")
	}
	// Băm kết quả costing lệch sự kiện.
	p2 := ids.New()
	run := ids.New()
	s := summary(p2, run, hashOf("a"))
	f.periods.put(f.tenant, s)
	s.ResultSetSHA256 = hashOf("b")
	f.deliverClosed(f.tenant, s, ids.New(), ids.New())
	f.drain(f.tenant)
	if c := jobCode(p2); c != app.CodeSourceMismatch {
		t.Fatalf("băm lệch: %q", c)
	}
	// Kỳ chưa đóng ở costing.
	p3 := ids.New()
	s3 := summary(p3, ids.New(), hashOf("c"))
	f.deliverClosed(f.tenant, s3, ids.New(), ids.New())
	f.drain(f.tenant)
	if c := jobCode(p3); c != app.CodePeriodNotClosed {
		t.Fatalf("chưa đóng: %q", c)
	}
	// Không nối costing: lỗi tạm thời (handler trả lỗi → hàng đợi giao lại → DLQ RabbitMQ).
	p4 := ids.New()
	f.closePeriod(f.tenant, p4, ids.New())
	f.svc.Periods = nil
	if _, err := drainTenant(ctx, f.pool, f.svc, f.tenant); err == nil {
		t.Fatal("chưa nối ClosedPeriods phải trả lỗi")
	}
	f.svc.Periods = f.periods
	f.drain(f.tenant)
	if j := f.journalOf(op, p4)["journal"].(map[string]any); str(j, "status") != "built" {
		t.Fatal("giao lại sau khi nối")
	}
	// Payload hỏng → lỗi (DLQ).
	err := tenant.WithTenantTx(ctx, f.pool, f.tenant, func(tx pgx.Tx) error {
		return f.svc.HandlePeriodClosed(ctx, tx, envelope(f.tenant, cp.EventPeriodClosed, []byte(`{"period_id":"x"}`)))
	})
	if err == nil {
		t.Fatal("payload hỏng phải lỗi")
	}
}

// B-05: bảng ánh xạ có phiên bản; đề xuất không kèm rules = mặc định tạm (provisional); một phiên bản chờ;
// rules sai → 422; đề xuất lặp khóa → phát lại.
func TestAcctbridgeMappingVersions_B05(t *testing.T) {
	f := newFixture(t, nil)
	op := f.operator()
	body := map[string]any{"reason": "Bảng ánh xạ tạm theo VAS, chờ khách chốt"}
	k := key()
	r := op.call(f.admin.URL, http.MethodPost, "/v1/acctbridge/mappings", k, body)
	mustStatus(t, r, http.StatusCreated, "đề xuất")
	if str(r.body, "mapping", "status") != "pending" || r.body["mapping"].(map[string]any)["provisional"] != true || str(r.body, "mapping", "version") != "1" {
		t.Fatalf("mặc định tạm: %s", r.raw)
	}
	rp := op.call(f.admin.URL, http.MethodPost, "/v1/acctbridge/mappings", k, body)
	if (rp.status != http.StatusOK && rp.status != http.StatusCreated) || str(rp.body, "mapping", "version_id") != str(r.body, "mapping", "version_id") {
		t.Fatalf("phát lại: %d %s", rp.status, rp.raw)
	}
	mustCode(t, op.adminPost("/v1/acctbridge/mappings", body), http.StatusConflict, "acctbridge.mapping_pending", "hai phiên bản chờ")
	mustCode(t, f.chief().adminPost("/v1/acctbridge/mappings/"+str(r.body, "mapping", "version_id")+"/decision", map[string]any{"decision": "rejected"}),
		http.StatusUnprocessableEntity, "validation_failed", "từ chối không lý do")
	mustStatus(t, f.chief().adminPost("/v1/acctbridge/mappings/"+str(r.body, "mapping", "version_id")+"/decision",
		map[string]any{"decision": "rejected", "reason": "Chờ kế toán trưởng xác nhận chế độ kế toán"}), http.StatusCreated, "từ chối")
	mustCode(t, f.chief().adminPost("/v1/acctbridge/mappings/"+str(r.body, "mapping", "version_id")+"/decision", map[string]any{"decision": "approved"}),
		http.StatusConflict, "acctbridge.mapping_decided", "quyết định lại")
	bad := map[string]any{"reason": "Bảng ánh xạ sai để kiểm", "rules": []map[string]any{{"category": "straw_purchase", "dimension_kind": "*",
		"debit_account": "152", "credit_account": "152", "dimension_on": "none"}}}
	mustCode(t, op.adminPost("/v1/acctbridge/mappings", bad), http.StatusUnprocessableEntity, "validation_failed", "rules sai")
	v := f.approvedMapping(f.tenant, nil)
	m := op.get("/v1/acctbridge/mappings")
	mustStatus(t, m, http.StatusOK, "đọc ánh xạ")
	if str(m.body, "effective", "version_id") != v || len(arr(m.body, "versions")) != 2 || len(arr(m.body, "default_rules")) != 12 {
		t.Fatalf("ánh xạ: %s", m.raw)
	}
}

// RB-1: sổ mapping_event, journal_event chỉ ghi thêm (UPDATE/DELETE/TRUNCATE × vai ứng dụng, owner);
// push_attempt (bảng vận hành) chỉ INSERT với vai ứng dụng.
func TestAcctbridgeLedgersImmutable(t *testing.T) {
	f := newFixture(t, nil)
	ctx := context.Background()
	f.approvedMapping(f.tenant, nil)
	period := ids.New()
	f.closePeriod(f.tenant, period, ids.New())
	f.drain(f.tenant)
	jid := str(f.journalOf(f.chief(), period)["journal"].(map[string]any), "journal_id")
	mustStatus(t, f.chief().adminPost("/v1/acctbridge/journals/"+jid+"/approve", nil), http.StatusAccepted, "duyệt")
	f.drain(f.tenant)
	for _, pool := range []*pgxpool.Pool{f.pool, f.env.Pool(t, migrations.MKG, dbadmin.Migrator)} {
		for _, q := range []string{
			"UPDATE mkg_acctbridge.journal_event SET total_debit = 1 WHERE journal_id = '" + jid + "'",
			"DELETE FROM mkg_acctbridge.journal_event WHERE journal_id = '" + jid + "'",
			"TRUNCATE mkg_acctbridge.journal_event",
			"UPDATE mkg_acctbridge.mapping_event SET provisional = false",
			"DELETE FROM mkg_acctbridge.mapping_event",
			"TRUNCATE mkg_acctbridge.mapping_event",
		} {
			if err := tenant.WithTenantTx(ctx, pool, f.tenant, func(tx pgx.Tx) error { _, err := tx.Exec(ctx, q); return err }); err == nil {
				t.Fatalf("sổ phải chặn: %s", q)
			}
		}
	}
	for _, q := range []string{
		"UPDATE mkg_acctbridge.push_attempt SET outcome = 'rejected'",
		"DELETE FROM mkg_acctbridge.push_attempt",
	} {
		if err := tenant.WithTenantTx(ctx, f.pool, f.tenant, func(tx pgx.Tx) error { _, err := tx.Exec(ctx, q); return err }); err == nil {
			t.Fatalf("push_attempt chỉ INSERT: %s", q)
		}
	}
	f.inTenant(f.tenant, func(tx pgx.Tx) error {
		for _, tb := range []string{"mkg_acctbridge.journal_event", "mkg_acctbridge.mapping_event"} {
			var ok bool
			if err := tx.QueryRow(ctx, "SELECT ok FROM shared.verify_chain($1::regclass, $2, $3)", tb, f.tenant,
				map[string]string{"mkg_acctbridge.journal_event": "period:" + period.String(), "mkg_acctbridge.mapping_event": "mapping"}[tb]).Scan(&ok); err != nil {
				return err
			}
			if !ok {
				t.Fatalf("verify_chain %s", tb)
			}
		}
		return nil
	})
	var missing int
	if err := f.env.AdminPool(t, migrations.MKG).QueryRow(ctx, "SELECT count(*) FROM shared.v_missing_immutability WHERE schema_name = 'mkg_acctbridge'").Scan(&missing); err != nil {
		t.Fatal(err)
	}
	if missing != 0 {
		t.Fatal("v_missing_immutability phải rỗng")
	}
}

// R-22: dựng lại bảng chiếu từ sổ cho kết quả như bảng chiếu hiện hành (trừ updated_at).
func TestAcctbridgeProjectionRebuild_R22(t *testing.T) {
	f := newFixture(t, nil)
	ctx := context.Background()
	f.approvedMapping(f.tenant, nil)
	op, chief := f.operator(), f.chief()
	// Kỳ 1: đẩy thành công sau một lần tạm lỗi; kỳ 2: DLQ; kỳ 3: dựng lại (superseded) + đang chờ thử lại.
	p1, p2, p3 := ids.New(), ids.New(), ids.New()
	for _, p := range []uuid.UUID{p1, p2, p3} {
		f.closePeriod(f.tenant, p, ids.New())
	}
	f.drain(f.tenant)
	f.fake.FailNext(port.ErrAdapterUnavailable)
	mustStatus(t, chief.adminPost("/v1/acctbridge/journals/"+str(f.journalOf(op, p1)["journal"].(map[string]any), "journal_id")+"/approve", nil),
		http.StatusAccepted, "duyệt 1")
	f.drain(f.tenant)
	f.clk.Advance(time.Minute)
	if _, err := f.svc.RetryDue(ctx, func(context.Context) ([]uuid.UUID, error) { return []uuid.UUID{f.tenant}, nil }); err != nil {
		t.Fatal(err)
	}
	f.drain(f.tenant)
	f.fake.FailNext(port.ErrAdapterRejected)
	mustStatus(t, chief.adminPost("/v1/acctbridge/journals/"+str(f.journalOf(op, p2)["journal"].(map[string]any), "journal_id")+"/approve", nil),
		http.StatusAccepted, "duyệt 2")
	f.drain(f.tenant)
	rules := domain.DefaultRules()
	rules[0].DebitAccount = "1311"
	f.approvedMapping(f.tenant, rules)
	mustStatus(t, op.post("/v1/acctbridge/periods/"+p3.String()+"/journal-builds", nil), http.StatusAccepted, "dựng lại 3")
	f.drain(f.tenant)
	f.fake.FailNext(port.ErrAdapterUnavailable)
	mustStatus(t, chief.adminPost("/v1/acctbridge/journals/"+str(f.journalOf(op, p3)["journal"].(map[string]any), "journal_id")+"/approve", nil),
		http.StatusAccepted, "duyệt 3")
	f.drain(f.tenant)
	snap := func() string {
		var s string
		f.inTenant(f.tenant, func(tx pgx.Tx) error {
			return tx.QueryRow(ctx, `SELECT coalesce(json_agg(x ORDER BY x->>'k')::text, '') FROM (
			    SELECT jsonb_build_object('k', 'j' || journal_id) || (to_jsonb(j) - 'updated_at') AS x FROM mkg_acctbridge.journal_current j
			    UNION ALL SELECT jsonb_build_object('k', 'm' || version_id) || (to_jsonb(m) - 'updated_at') FROM mkg_acctbridge.mapping_current m) q`).Scan(&s)
		})
		return s
	}
	before := snap()
	r := chief.adminPost("/v1/acctbridge/projections/rebuild", nil)
	mustStatus(t, r, http.StatusOK, "dựng lại bảng chiếu")
	if str(r.body, "journals") != "4" {
		t.Fatalf("dựng lại: %s", r.raw)
	}
	if after := snap(); after != before {
		t.Fatalf("bảng chiếu dựng lại khác:\n%s\n%s", before, after)
	}
	mustCode(t, op.adminPost("/v1/acctbridge/projections/rebuild", nil), http.StatusForbidden, "forbidden", "vận hành không dựng lại")
}
