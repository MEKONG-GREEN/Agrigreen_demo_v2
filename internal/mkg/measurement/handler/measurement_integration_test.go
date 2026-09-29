package handler_test

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	hubport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/hub/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/measurement/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

// ADR-0019, R-56: đăng ký thiết bị cân — người đề xuất ≠ người duyệt (app + DB);
// Resolve cho ingest (tenant suy ra từ thiết bị); kỳ mới thay kỳ cũ; thu hồi; MFA.
func TestDeviceRegistrySoDResolveEpochRevoke(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	d := newScale(t, "SCALE-HUBIN-01")
	sup := f.supervisor()
	// MFA bắt buộc ở bề mặt quản trị.
	if r := f.as(f.tenant, false, port.RoleScaleSupervisor).adm(http.MethodPost, "/v1/measurement/devices", d.body("HUB_IN")); code(r) != "mfa_required" {
		t.Fatalf("thiếu MFA: %d %s", r.status, r.raw)
	}
	if r := sup.adm(http.MethodPost, "/v1/measurement/devices", d.body("CUSTOMER_IN")); r.status != http.StatusUnprocessableEntity {
		t.Fatalf("CUSTOMER_IN không qua agent biên: %d", r.status)
	}
	p := sup.adm(http.MethodPost, "/v1/measurement/devices", d.body("HUB_IN"))
	mustStatus(t, p, http.StatusCreated, "đề xuất")
	if p.body["status"] != "pending" || p.body["epoch"] != json.Number("1") || len(str(p.body, "key_id")) != 16 {
		t.Fatalf("đề xuất: %s", p.raw)
	}
	if _, err := f.ingest.Devices.Resolve(ctx, d.id, 1, d.cert); !errors.Is(err, port.ErrUnknownDevice) {
		t.Fatalf("chưa duyệt → unknown: %v", err)
	}
	// Tự duyệt: một người có cả hai vai.
	both := caller{f: f, tid: f.tenant, actor: sup.actor}
	both.token = f.iss.Sign(t, tokenFor(sup.actor, f.tenant, port.RoleScaleSupervisor, port.RoleDeviceApprover))
	if r := both.adm(http.MethodPost, "/v1/measurement/devices/"+d.id+"/epochs/1/decision", map[string]any{"decision": "approved"}); code(r) != "self_approval_forbidden" {
		t.Fatalf("tự duyệt phải bị chặn: %d %s", r.status, r.raw)
	}
	// Lớp DB: CHECK actor_ref <> proposer_ref.
	err := tenant.WithTenantTx(ctx, f.pool, f.tenant, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO mkg_measurement.device_decision (id, tenant_id, stream_key, scale_device_id, epoch, version_id, decision,
		  proposer_ref, event_time, actor_ref, idem_key, rule_version) VALUES ($1, $2, 'device:x', 'x', 1, $3, 'approved', $4, now(), $4, 'k-self', 'v')`,
			ids.New(), f.tenant, ids.New(), sup.actor)
		return err
	})
	if err == nil || !strings.Contains(err.Error(), "check") {
		t.Fatalf("DB phải chặn tự duyệt: %v", err)
	}
	mustStatus(t, f.approver().adm(http.MethodPost, "/v1/measurement/devices/"+d.id+"/epochs/1/decision", map[string]any{"decision": "approved"}),
		http.StatusCreated, "duyệt")
	dev, err := f.ingest.Devices.Resolve(ctx, d.id, 1, d.cert)
	if err != nil || dev.TenantID != f.tenant || dev.Point != port.PointHubIn || string(dev.PublicKey) != string(d.pub) || dev.Status != port.DeviceActive {
		t.Fatalf("Resolve: %+v %v", dev, err)
	}
	if _, err := f.ingest.Devices.Resolve(ctx, d.id, 1, strings.Repeat("0", 64)); !errors.Is(err, port.ErrCertMismatch) {
		t.Fatalf("sai chứng thư: %v", err)
	}
	if _, err := f.ingest.Devices.Resolve(ctx, "SCALE-NONE", 1, d.cert); !errors.Is(err, port.ErrUnknownDevice) {
		t.Fatalf("thiết bị lạ: %v", err)
	}
	// Sức khỏe từ ingest (chỉ cột sức khỏe).
	if err := f.ingest.Health.ReportHealth(ctx, f.tenant, port.HealthReport{DeviceID: d.id, Epoch: 1, LastSeen: f.clk.Now(), BufferPct: "12.5",
		ClockSkewMs: -340, LastCounter: 77}); err != nil {
		t.Fatal(err)
	}
	g := sup.app(http.MethodGet, "/v1/measurement/devices/"+d.id, nil)
	ep := arr(g.body, "epochs")
	if len(ep) != 1 || str(ep[0], "health", "buffer_pct") != "12.50" || obj(ep[0], "health")["clock_skew_ms"] != json.Number("-340") {
		t.Fatalf("sức khỏe: %s", g.raw)
	}
	// Mất đĩa → kỳ mới (khóa mới); kỳ cũ bị thay, bộ đếm cũ không bao giờ nhận thêm.
	d2 := newScale(t, d.id)
	p2 := sup.adm(http.MethodPost, "/v1/measurement/devices", d2.body("HUB_IN"))
	mustStatus(t, p2, http.StatusCreated, "kỳ 2")
	if r := sup.adm(http.MethodPost, "/v1/measurement/devices", d2.body("HUB_IN")); code(r) != "measurement.pending_exists" {
		t.Fatalf("hai đề xuất chờ: %d %s", r.status, r.raw)
	}
	mustStatus(t, f.approver().adm(http.MethodPost, "/v1/measurement/devices/"+d.id+"/epochs/2/decision", map[string]any{"decision": "approved"}),
		http.StatusCreated, "duyệt kỳ 2")
	if _, err := f.ingest.Devices.Resolve(ctx, d.id, 1, d.cert); !errors.Is(err, port.ErrEpochSuperseded) {
		t.Fatalf("kỳ cũ: %v", err)
	}
	if r := sup.adm(http.MethodPost, "/v1/measurement/devices/"+d.id+"/epochs/2/revoke", map[string]any{"reason": "ngắn"}); r.status != http.StatusUnprocessableEntity {
		t.Fatalf("thu hồi thiếu lý do: %d", r.status)
	}
	mustStatus(t, sup.adm(http.MethodPost, "/v1/measurement/devices/"+d.id+"/epochs/2/revoke",
		map[string]any{"reason": "Nghi lộ khóa nguồn sau sự cố tủ điện"}), http.StatusCreated, "thu hồi")
	if _, err := f.ingest.Devices.Resolve(ctx, d.id, 2, d2.cert); !errors.Is(err, port.ErrDeviceRevoked) {
		t.Fatalf("đã thu hồi: %v", err)
	}
	// Mã thiết bị gắn một tenant mãi mãi.
	other := f.as(ids.New(), true, port.RoleScaleSupervisor)
	if r := other.adm(http.MethodPost, "/v1/measurement/devices", newScale(t, d.id).body("HUB_IN")); code(r) != "measurement.device_taken" {
		t.Fatalf("device_id của tenant khác: %d %s", r.status, r.raw)
	}
}

// R-70, RB-1, R-51: phiếu edge qua vai ingest_mkg — idempotent, một phiếu hiệu lực mỗi
// điểm, sửa bằng phiếu bù; sổ bất biến; sự kiện NỘI BỘ; ingest không có quyền ngoài phiếu.
func TestIngestTicketIdempotencyCorrectionAndGrants(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	d := f.approvedScale("SCALE-IN-02", "HUB_IN")
	lot := ids.New()

	// Phiên cân: mở/đóng cửa sổ bộ đếm; ingest đọc cửa sổ đã đóng.
	sup := f.supervisor()
	op := sup.app(http.MethodPost, "/v1/measurement/sessions", map[string]any{"lot_ref": lot.String(), "device_id": d.id, "epoch": d.epoch, "counter_from": 1})
	mustStatus(t, op, http.StatusCreated, "mở phiên")
	sid := uuid.MustParse(str(op.body, "id"))
	if _, err := f.ingest.Sessions.Session(ctx, f.tenant, sid); !errors.Is(err, port.ErrSessionOpen) {
		t.Fatalf("phiên mở: %v", err)
	}
	if r := sup.app(http.MethodPost, "/v1/measurement/sessions", map[string]any{"lot_ref": ids.New().String(), "device_id": d.id, "epoch": d.epoch, "counter_from": 50}); code(r) != "measurement.session_conflict" {
		t.Fatalf("hai phiên mở trên một thiết bị: %d %s", r.status, r.raw)
	}
	mustStatus(t, sup.app(http.MethodPost, "/v1/measurement/sessions/"+sid.String()+"/close", map[string]any{"counter_to": 40}), http.StatusCreated, "đóng")
	s, err := f.ingest.Sessions.Session(ctx, f.tenant, sid)
	if err != nil || s.Window.CounterFrom != 1 || s.Window.CounterTo != 40 || s.Point != port.PointHubIn {
		t.Fatalf("cửa sổ: %+v %v", s, err)
	}
	if r := sup.app(http.MethodPost, "/v1/measurement/sessions", map[string]any{"lot_ref": ids.New().String(), "device_id": d.id, "epoch": d.epoch, "counter_from": 40}); code(r) != "measurement.session_conflict" {
		t.Fatalf("cửa sổ chồng: %d %s", r.status, r.raw)
	}

	t1, err := f.edgeTicket(d, lot, port.PointHubIn, "96800", 1, 40, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	// Gọi lặp cùng IdemKey → cùng phiếu, không ghi thêm.
	dr := t1.TicketDraft
	again, err := f.ingest.Tickets.Record(ctx, f.tenant, dr)
	if err != nil || again.ID != t1.ID || again.Seq != t1.Seq {
		t.Fatalf("phát lại: %+v %v", again, err)
	}
	// Phiếu thứ hai cùng điểm không phải phiếu bù → từ chối.
	if _, err := f.edgeTicket(d, lot, port.PointHubIn, "96000", 41, 60, nil, ""); !errors.Is(err, port.ErrInvalidTicket) {
		t.Fatalf("phiếu thứ hai cùng điểm: %v", err)
	}
	// Edge ở CUSTOMER_IN, thiết bị sai điểm, cửa sổ thiếu → từ chối.
	if _, err := f.edgeTicket(d, lot, port.PointHubOut, "90000", 41, 60, nil, ""); !errors.Is(err, port.ErrInvalidTicket) {
		t.Fatalf("thiết bị HUB_IN ghi HUB_OUT: %v", err)
	}
	// Phiếu bù.
	t2, err := f.edgeTicket(d, lot, port.PointHubIn, "96750.5", 41, 60, &t1.ID, "Tích phân lại sau hiệu chuẩn cân C-6")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.edgeTicket(d, lot, port.PointHubIn, "1", 61, 62, &t1.ID, "Bù phiếu đã bị bù trước đó"); !errors.Is(err, port.ErrInvalidTicket) {
		t.Fatalf("bù phiếu không còn hiệu lực: %v", err)
	}
	v, err := f.ingest.LotMass.LotMass(ctx, f.tenant, lot)
	if err != nil || v.HubInKg.String() != "96750.500" || v.TicketIDs[port.PointHubIn] != t2.ID || v.HubOutKg != nil {
		t.Fatalf("LotMass: %+v %v", v, err)
	}
	lt := sup.app(http.MethodGet, "/v1/measurement/lots/"+lot.String()+"/tickets", nil)
	ts := arr(lt.body, "tickets")
	if len(ts) != 2 || ts[0]["effective"] != false || ts[1]["effective"] != true || str(ts[1], "supersedes_id") != t1.ID.String() ||
		str(ts[1], "rule_version") != "weigh.integrate.v1" || str(obj(lt.body, "mass"), "hub_in", "mass_kg") != "96750.500" {
		t.Fatalf("phiếu gốc giữ nguyên, phiếu bù hiệu lực: %s", lt.raw)
	}
	// Sự kiện nội bộ trên outbox (kind internal), không bao giờ platform.
	f.inTenant(func(tx pgx.Tx) error {
		var kind, typ string
		if err := tx.QueryRow(ctx, "SELECT kind, event_type FROM shared.outbox WHERE id = $1", t2.ID).Scan(&kind, &typ); err != nil {
			return err
		}
		if kind != "internal" || typ != port.EventWeighTicketRecorded {
			t.Fatalf("outbox: %s %s", kind, typ)
		}
		return nil
	})
	// Sổ bất biến: vai ứng dụng, vai ingest và owner.
	for _, pool := range []*pgxpool.Pool{f.pool, f.ingestPool, f.env.Pool(t, migrations.MKG, dbadmin.Migrator)} {
		for _, q := range []string{
			"UPDATE mkg_measurement.weigh_ticket SET mass_kg = 1 WHERE id = '" + t1.ID.String() + "'",
			"DELETE FROM mkg_measurement.weigh_ticket WHERE id = '" + t1.ID.String() + "'",
			"TRUNCATE mkg_measurement.weigh_ticket",
		} {
			if err := tenant.WithTenantTx(ctx, pool, f.tenant, func(tx pgx.Tx) error { _, err := tx.Exec(ctx, q); return err }); err == nil {
				t.Fatalf("sổ phiếu phải chặn: %s", q)
			}
		}
	}
	// Vai ingest chỉ có quyền ghi phiếu: không đối soát, không thiết bị, không tham số, không van.
	for _, q := range []string{
		"SELECT count(*) FROM mkg_measurement.reconciliation",
		"SELECT count(*) FROM mkg_measurement.tolerance_version",
		"SELECT count(*) FROM mkg_measurement.valve_export",
		"SELECT count(*) FROM mkg_measurement.device_version",
		"UPDATE mkg_measurement.device_current SET status = 'active'",
		"INSERT INTO mkg_measurement.device_directory (device_id, tenant_id) VALUES ('EVIL-01', gen_random_uuid())",
	} {
		if err := tenant.WithTenantTx(ctx, f.ingestPool, f.tenant, func(tx pgx.Tx) error { _, err := tx.Exec(ctx, q); return err }); err == nil ||
			!strings.Contains(err.Error(), "permission denied") {
			t.Fatalf("ingest_mkg không được: %s (%v)", q, err)
		}
	}
}

// proto plant-weighing + RB-6 + mục 13.1: tuyến HUB — hao hụt dự kiến/bất thường,
// chênh thương mại vượt dung sai mở tranh chấp; vòng đời tranh chấp; golden seed prototype.
func TestHubReconciliationDisputeLifecycle(t *testing.T) {
	f := newFixture(t)
	din := f.approvedScale("SCALE-IN-03", "HUB_IN")
	dout := f.approvedScale("SCALE-OUT-03", "HUB_OUT")
	lot := ids.New()
	f.rolls.set(lot, 4400, "20") // ghe 1 seed prototype: 4 400 cuộn (việc 5 800 cuộn / 5 700 gom), ước 20 kg/cuộn
	if _, err := f.edgeTicket(din, lot, port.PointHubIn, "96800", 1, 10, nil, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := f.edgeTicket(dout, lot, port.PointHubOut, "93000", 1, 10, nil, ""); err != nil {
		t.Fatal(err)
	}
	off := f.officer()
	if r := off.app(http.MethodPost, "/v1/measurement/lots/"+lot.String()+"/reconciliations", nil); code(r) != "measurement.reconciliation_incomplete" {
		t.Fatalf("chưa có CUSTOMER_IN: %d %s", r.status, r.raw)
	}
	// CUSTOMER_IN nhập tay có bằng chứng: khách cân 85 000 (−8,60 % so HUB_OUT) → tranh chấp mức cờ.
	sup := f.supervisor()
	ev := ids.New()
	body := map[string]any{"point": "CUSTOMER_IN", "mass_kg": "85000", "weighed_at": f.ts(-10 * time.Minute), "evidence_ref": ev.String(),
		"customer_ref": ids.New().String(), "reason": "Phiếu cân nhà máy khách, chưa tích hợp hệ cân (C-5)"}
	if r := sup.app(http.MethodPost, "/v1/measurement/lots/"+lot.String()+"/tickets", map[string]any{"point": "CUSTOMER_IN", "mass_kg": "85000",
		"weighed_at": f.ts(-time.Minute), "reason": "Phiếu cân nhà máy khách"}); code(r) != "validation_failed" {
		t.Fatalf("thiếu bằng chứng: %d %s", r.status, r.raw)
	}
	c := sup.app(http.MethodPost, "/v1/measurement/lots/"+lot.String()+"/tickets", body)
	mustStatus(t, c, http.StatusCreated, "CUSTOMER_IN tay")
	rec := obj(c.body, "reconciliation")
	in, com, roll := obj(rec, "internal"), obj(rec, "commercial"), obj(rec, "roll_check")
	// Ẩm HUB_IN 18 % → định mức 3 % = 2 904 kg; hao 3 800 → bất thường 896 (0,93 % ≤ 1 %) → bình thường.
	if str(in, "expected_loss_kg") != "2904.000" || str(in, "abnormal_loss_kg") != "896.000" || str(in, "status") != "normal" {
		t.Fatalf("nội bộ: %v", in)
	}
	if str(com, "diff_kg") != "-8000.000" || str(com, "diff_pct") != "-8.60" || str(com, "status") != "flag" ||
		str(rec, "outcome") != port.OutcomeDisputeOpened || str(rec, "tolerance_version") != "provisional-default" {
		t.Fatalf("thương mại: %s", c.raw)
	}
	// RB-6 (hợp đồng theo tấn — mặc định): 96 800 vs 4 400 × 20 = 88 000 → +10,00 % → cờ; sản lượng quy đổi 22,000.
	if str(roll, "status") != "flag" || str(roll, "variance_pct") != "10.00" || str(roll, "estimate_kg") != "88000.000" ||
		str(rec, "yield_kg_per_roll") != "22.000" || rec["yield_aggregated"] != false {
		t.Fatalf("cuộn ↔ khối lượng: %v", rec)
	}
	did := str(rec, "dispute_id")
	dp := f.disputer()
	g := dp.app(http.MethodGet, "/v1/measurement/disputes/"+did, nil)
	if g.body["status"] != "open" || g.body["severity"] != "flag" || len(g.body["evidence_refs"].([]any)) != 1 {
		t.Fatalf("hồ sơ mở kèm bằng chứng phiếu khách: %s", g.raw)
	}
	// Đối soát lại không đổi đầu vào → trả lần trước, không ghi.
	r2 := off.app(http.MethodPost, "/v1/measurement/lots/"+lot.String()+"/reconciliations", nil)
	mustStatus(t, r2, http.StatusOK, "đối soát lặp")
	if str(r2.body, "id") != str(rec, "id") {
		t.Fatalf("đầu vào không đổi phải trả lần trước: %s", r2.raw)
	}
	// Vòng đời: resolve trước review → 409; thêm bằng chứng; review; resolve; đóng rồi không nhận thêm.
	act := func(b map[string]any) resp { return dp.app(http.MethodPost, "/v1/measurement/disputes/"+did+"/actions", b) }
	if r := off.app(http.MethodPost, "/v1/measurement/disputes/"+did+"/actions", map[string]any{"action": "start_review"}); r.status != http.StatusForbidden {
		t.Fatalf("vai: %d", r.status)
	}
	if r := act(map[string]any{"action": "resolve", "resolution": "mkg_weight", "note": "Giữ số cân MKG theo biên bản"}); code(r) != "measurement.dispute_transition" {
		t.Fatalf("resolve khi chưa xem xét: %d %s", r.status, r.raw)
	}
	mustStatus(t, act(map[string]any{"action": "add_evidence", "evidence_refs": []string{ids.New().String()}}), http.StatusCreated, "bằng chứng")
	mustStatus(t, act(map[string]any{"action": "start_review"}), http.StatusCreated, "xem xét")
	if r := act(map[string]any{"action": "resolve", "resolution": "agreed_weight", "note": "Hai bên thống nhất"}); r.status != http.StatusUnprocessableEntity {
		t.Fatalf("agreed thiếu kg: %d", r.status)
	}
	res := act(map[string]any{"action": "resolve", "resolution": "agreed_weight", "agreed_kg": "88500", "note": "Hai bên thống nhất theo cân kiểm tra"})
	mustStatus(t, res, http.StatusCreated, "kết luận")
	if res.body["status"] != "resolved" || res.body["agreed_kg"] != "88500.000" || len(res.body["evidence_refs"].([]any)) != 2 || len(res.body["events"].([]any)) != 4 {
		t.Fatalf("kết luận: %s", res.raw)
	}
	if r := act(map[string]any{"action": "add_evidence", "evidence_refs": []string{ids.New().String()}}); code(r) != "measurement.dispute_transition" {
		t.Fatalf("đã đóng: %d", r.status)
	}
	// Phiếu bù CUSTOMER_IN (khách sửa số) → đối soát lại thay lần trước; sổ đối soát giữ lịch sử.
	cin := str(obj(c.body, "ticket"), "id")
	body2 := map[string]any{"point": "CUSTOMER_IN", "mass_kg": "92500", "weighed_at": f.ts(-5 * time.Minute), "evidence_ref": ids.New().String(),
		"reason": "Phiếu cân khách bản sửa có chữ ký", "supersedes_id": cin, "correction_reason": "Khách gửi lại phiếu cân đã sửa sai số xe"}
	c2 := sup.app(http.MethodPost, "/v1/measurement/lots/"+lot.String()+"/tickets", body2)
	mustStatus(t, c2, http.StatusCreated, "bù CUSTOMER_IN")
	rec2 := obj(c2.body, "reconciliation")
	if str(rec2, "outcome") != port.OutcomeWithinTolerance || str(rec2, "supersedes_id") != str(rec, "id") || rec2["dispute_id"] != nil {
		t.Fatalf("đối soát lại: %s", c2.raw)
	}
	hist := off.app(http.MethodGet, "/v1/measurement/lots/"+lot.String()+"/reconciliations", nil)
	if len(arr(hist.body, "items")) != 2 {
		t.Fatalf("lịch sử đối soát: %s", hist.raw)
	}
	// Sự kiện nội bộ reconciliation.completed.
	f.inTenant(func(tx pgx.Tx) error {
		var n int
		if err := tx.QueryRow(context.Background(), `SELECT count(*) FROM shared.outbox WHERE event_type = $1 AND kind = 'internal'`,
			port.EventReconciliationCompleted).Scan(&n); err != nil {
			return err
		}
		if n != 2 {
			t.Fatalf("reconciliation.completed: %d", n)
		}
		return nil
	})
	// Hợp đồng theo cuộn: kiểm chéo chỉ tham khảo.
	mustStatus(t, off.app(http.MethodPost, "/v1/measurement/lots/"+lot.String()+"/terms", map[string]any{"pricing_basis": "per_bale"}), http.StatusCreated, "terms")
	r3 := off.app(http.MethodPost, "/v1/measurement/lots/"+lot.String()+"/reconciliations", nil)
	mustStatus(t, r3, http.StatusCreated, "đối soát per_bale")
	if str(obj(r3.body, "roll_check"), "status") != "informational" || str(r3.body, "pricing_basis") != "per_bale" {
		t.Fatalf("per_bale: %s", r3.raw)
	}
	// Dựng lại bảng chiếu từ sổ (R-22).
	f.inTenant(func(tx pgx.Tx) error {
		_, err := tx.Exec(context.Background(), "UPDATE mkg_measurement.lot_mass_current SET customer_in_kg = 1")
		return err
	})
	rb := off.adm(http.MethodPost, "/v1/measurement/projections/rebuild", nil)
	mustStatus(t, rb, http.StatusOK, "rebuild")
	lm := sup.app(http.MethodGet, "/v1/measurement/lots/"+lot.String()+"/tickets", nil)
	if str(obj(lm.body, "mass"), "customer_in", "mass_kg") != "92500.000" || str(obj(lm.body, "mass"), "hub_out", "mass_kg") != "93000.000" {
		t.Fatalf("dựng lại: %s", lm.raw)
	}
	dr := dp.app(http.MethodGet, "/v1/measurement/disputes/"+did, nil)
	if dr.body["status"] != "resolved" || dr.body["agreed_kg"] != "88500.000" {
		t.Fatalf("dựng lại tranh chấp: %s", dr.raw)
	}
}

// Mục 13.1 đường 2: lô DIRECT chỉ CUSTOMER_IN, không tách hao hụt, sản lượng gộp.
func TestDirectRouteOnlyCustomerIn(t *testing.T) {
	f := newFixture(t)
	d := f.approvedScale("SCALE-IN-04", "HUB_IN")
	lot := ids.New()
	f.rolls.set(lot, 4400, "20")
	sup := f.supervisor()
	c := sup.app(http.MethodPost, "/v1/measurement/lots/"+lot.String()+"/tickets", map[string]any{"point": "CUSTOMER_IN", "route": "DIRECT",
		"mass_kg": "96800", "weighed_at": f.ts(-time.Minute), "evidence_ref": ids.New().String(), "reason": "Ghe AG-12345 thẳng nhà máy"})
	mustStatus(t, c, http.StatusCreated, "DIRECT")
	rec := obj(c.body, "reconciliation")
	if str(rec, "outcome") != port.OutcomeDirectAggregate || rec["yield_aggregated"] != true || str(obj(rec, "internal"), "status") != "not_applicable" ||
		str(obj(rec, "commercial"), "status") != "not_applicable" || str(rec, "yield_kg_per_roll") != "22.000" || str(obj(rec, "roll_check"), "status") != "flag" {
		t.Fatalf("DIRECT: %s", c.raw)
	}
	y := f.officer().app(http.MethodGet, "/v1/measurement/lots/"+lot.String()+"/yield", nil)
	if y.body["aggregated"] != true || y.body["basis_point"] != "CUSTOMER_IN" {
		t.Fatalf("yield: %s", y.raw)
	}
	if _, err := f.edgeTicket(d, lot, port.PointHubIn, "96800", 1, 5, nil, ""); !errors.Is(err, port.ErrInvalidTicket) {
		t.Fatalf("HUB_IN trên lô DIRECT: %v", err)
	}
	if r := sup.app(http.MethodPost, "/v1/measurement/sessions", map[string]any{"lot_ref": lot.String(), "device_id": d.id, "epoch": d.epoch, "counter_from": 1}); code(r) != "measurement.invalid_ticket" {
		t.Fatalf("phiên cân hub trên lô DIRECT: %d %s", r.status, r.raw)
	}
}

// C-4 (tạm): dung sai có phiên bản — người đặt ≠ người duyệt; áp theo THỜI ĐIỂM CÂN.
func TestToleranceVersionsSoDAndEventTime(t *testing.T) {
	f := newFixture(t)
	off, ap := f.officer(), f.tolApprover()
	eff := f.clk.Now().Add(-2 * time.Hour)
	body := map[string]any{"commercial_flag_pct": "10.00", "commercial_critical_pct": "20.00", "roll_flag_pct": "5", "roll_critical_pct": "15",
		"effective_from": eff.Format(time.RFC3339), "reason": "Biên bản làm việc với khách ngày 30/09 (tạm)"}
	if r := off.app(http.MethodPost, "/v1/measurement/tolerance-versions", body); r.status != http.StatusNotFound && r.status != http.StatusMethodNotAllowed {
		t.Fatalf("đề xuất chỉ ở bề mặt quản trị: %d", r.status)
	}
	bad := map[string]any{"commercial_flag_pct": "10", "commercial_critical_pct": "5", "roll_flag_pct": "5", "roll_critical_pct": "15",
		"effective_from": eff.Format(time.RFC3339), "reason": "Biên bản làm việc với khách"}
	if r := off.adm(http.MethodPost, "/v1/measurement/tolerance-versions", bad); r.status != http.StatusUnprocessableEntity {
		t.Fatalf("nghiêm trọng < cờ: %d", r.status)
	}
	p := off.adm(http.MethodPost, "/v1/measurement/tolerance-versions", body)
	mustStatus(t, p, http.StatusCreated, "đề xuất dung sai")
	if p.body["status"] != "pending" || p.body["provisional"] != true || p.body["version"] != json.Number("1") {
		t.Fatalf("đề xuất: %s", p.raw)
	}
	self := caller{f: f, tid: f.tenant, actor: off.actor}
	self.token = f.iss.Sign(t, tokenFor(off.actor, f.tenant, port.RoleReconciliationOfficer, port.RoleToleranceApprover))
	if r := self.adm(http.MethodPost, "/v1/measurement/tolerance-versions/1/decision", map[string]any{"decision": "approved"}); code(r) != "self_approval_forbidden" {
		t.Fatalf("tự duyệt: %d %s", r.status, r.raw)
	}
	mustStatus(t, ap.adm(http.MethodPost, "/v1/measurement/tolerance-versions/1/decision", map[string]any{"decision": "approved"}), http.StatusCreated, "duyệt")
	if r := ap.adm(http.MethodPost, "/v1/measurement/tolerance-versions/1/decision", map[string]any{"decision": "approved"}); code(r) != "measurement.not_pending" {
		t.Fatalf("duyệt hai lần: %d", r.status)
	}
	// Lô A cân SAU effective_from: chênh −8,60 % ≤ 10 % → trong ngưỡng theo v1.
	// Lô B cân TRƯỚC effective_from: dùng mặc định tạm 5 % → tranh chấp.
	run := func(at time.Time) map[string]any {
		lot := ids.New()
		sup := f.supervisor()
		for _, x := range []struct{ pt, kg string }{{"HUB_IN", "96800"}, {"HUB_OUT", "93000"}, {"CUSTOMER_IN", "85000"}} {
			r := sup.app(http.MethodPost, "/v1/measurement/lots/"+lot.String()+"/tickets", map[string]any{"point": x.pt, "mass_kg": x.kg,
				"weighed_at": at.Format(time.RFC3339), "evidence_ref": ids.New().String(), "reason": "Cân hub hỏng, nhập tay theo biên bản"})
			mustStatus(t, r, http.StatusCreated, "phiếu "+x.pt)
			if x.pt == "CUSTOMER_IN" {
				return obj(r.body, "reconciliation")
			}
		}
		return nil
	}
	a := run(f.clk.Now().Add(-time.Hour))
	if str(a, "tolerance_version") != "tolerance:v1" || str(obj(a, "commercial"), "status") != "within" {
		t.Fatalf("sau hiệu lực: %v", a)
	}
	b := run(f.clk.Now().Add(-3 * time.Hour))
	if str(b, "tolerance_version") != "provisional-default" || str(b, "outcome") != port.OutcomeDisputeOpened {
		t.Fatalf("trước hiệu lực: %v", b)
	}
	// Định mức hao hụt: đề xuất + từ chối cần lý do.
	ln := off.adm(http.MethodPost, "/v1/measurement/loss-norm-versions", map[string]any{
		"bands":            []map[string]string{{"moisture_from": "0", "moisture_to": "20", "loss_pct": "2"}, {"moisture_from": "20", "moisture_to": "100", "loss_pct": "8"}},
		"default_loss_pct": "3", "abnormal_alert_pct": "1", "effective_from": eff.Format(time.RFC3339), "reason": "Định mức sấy tạm chờ khách"})
	mustStatus(t, ln, http.StatusCreated, "định mức")
	if r := ap.adm(http.MethodPost, "/v1/measurement/loss-norm-versions/1/decision", map[string]any{"decision": "rejected"}); r.status != http.StatusUnprocessableEntity {
		t.Fatalf("từ chối thiếu lý do: %d", r.status)
	}
	rj := ap.adm(http.MethodPost, "/v1/measurement/loss-norm-versions/1/decision", map[string]any{"decision": "rejected", "reason": "Chưa có số liệu mẻ sấy thực tế"})
	mustStatus(t, rj, http.StatusCreated, "từ chối")
	if rj.body["status"] != "rejected" {
		t.Fatalf("từ chối: %s", rj.raw)
	}
}

// C-5 (tạm): nhập số cân của khách từ tệp → phiếu customer_integration; idempotent.
func TestCustomerWeightImportIdempotent(t *testing.T) {
	f := newFixture(t)
	sup := f.supervisor()
	lot1, lot2 := ids.New(), ids.New()
	body := map[string]any{"channel": "file", "evidence_ref": ids.New().String(), "customer_ref": ids.New().String(),
		"rows": []map[string]any{
			{"lot_ref": lot1.String(), "route": "DIRECT", "mass_kg": "84200", "weighed_at": f.ts(-2 * time.Hour), "customer_ticket_no": "NM-001"},
			{"lot_ref": lot2.String(), "route": "DIRECT", "mass_kg": "91500", "moisture_pct": "16.5", "weighed_at": f.ts(-time.Hour)},
		}}
	k := key()
	r := sup.do(f.app.URL, http.MethodPost, "/v1/measurement/customer-weight-imports", k, body)
	mustStatus(t, r, http.StatusCreated, "nhập")
	im := obj(r.body, "import")
	if im["provisional"] != true || len(im["ticket_ids"].([]any)) != 2 || len(arr(r.body, "reconciliations")) != 2 {
		t.Fatalf("nhập: %s", r.raw)
	}
	tk := sup.app(http.MethodGet, "/v1/measurement/tickets/"+im["ticket_ids"].([]any)[1].(string), nil)
	if tk.body["source"] != "customer_integration" || tk.body["moisture_pct"] != "16.50" || tk.body["moisture_state"] != "as_received" {
		t.Fatalf("phiếu: %s", tk.raw)
	}
	// Lớp 1: middleware idempotency; lớp 2: sổ (xóa khóa bảng idempotency vẫn phát lại).
	again := sup.do(f.app.URL, http.MethodPost, "/v1/measurement/customer-weight-imports", k, body)
	if again.status != http.StatusCreated || str(obj(again.body, "import"), "id") != str(im, "id") {
		t.Fatalf("phát lại: %d %s", again.status, again.raw)
	}
	f.inTenant(func(tx pgx.Tx) error {
		_, err := tx.Exec(context.Background(), "DELETE FROM shared.idempotency_key WHERE idem_key = $1", k)
		return err
	})
	again2 := sup.do(f.app.URL, http.MethodPost, "/v1/measurement/customer-weight-imports", k, body)
	if again2.status != http.StatusOK || str(obj(again2.body, "import"), "id") != str(im, "id") {
		t.Fatalf("phát lại theo sổ: %d %s", again2.status, again2.raw)
	}
	// Một dòng sai → cả lần nhập bị từ chối (lô đã có CUSTOMER_IN hiệu lực).
	bad := map[string]any{"channel": "api", "rows": []map[string]any{
		{"lot_ref": ids.New().String(), "route": "DIRECT", "mass_kg": "100", "weighed_at": f.ts(-time.Hour)},
		{"lot_ref": lot1.String(), "route": "DIRECT", "mass_kg": "100", "weighed_at": f.ts(-time.Hour)}}}
	if r := sup.app(http.MethodPost, "/v1/measurement/customer-weight-imports", bad); code(r) != "measurement.invalid_ticket" {
		t.Fatalf("dòng trùng lô: %d %s", r.status, r.raw)
	}
	if r := sup.app(http.MethodPost, "/v1/measurement/customer-weight-imports", map[string]any{"channel": "manual", "rows": body["rows"]}); r.status != http.StatusUnprocessableEntity {
		t.Fatalf("nhập tay thiếu bằng chứng: %d", r.status)
	}
}

// RB-2: cách ly tenant — tài khoản khác không thấy phiếu, thiết bị, tranh chấp.
func TestTenantIsolation(t *testing.T) {
	f := newFixture(t)
	lot := ids.New()
	sup := f.supervisor()
	c := sup.app(http.MethodPost, "/v1/measurement/lots/"+lot.String()+"/tickets", map[string]any{"point": "CUSTOMER_IN", "route": "DIRECT",
		"mass_kg": "1000", "weighed_at": f.ts(-time.Minute), "evidence_ref": ids.New().String(), "reason": "Phiếu cân khách bản giấy"})
	mustStatus(t, c, http.StatusCreated, "phiếu")
	f.approvedScale("SCALE-ISO-01", "HUB_IN")
	other := f.as(ids.New(), true, port.RoleScaleSupervisor, port.RoleReconciliationOfficer)
	if r := other.app(http.MethodGet, "/v1/measurement/lots/"+lot.String()+"/tickets", nil); r.status != http.StatusNotFound {
		t.Fatalf("lô tenant khác: %d", r.status)
	}
	if r := other.app(http.MethodGet, "/v1/measurement/tickets/"+str(obj(c.body, "ticket"), "id"), nil); r.status != http.StatusNotFound {
		t.Fatalf("phiếu tenant khác: %d", r.status)
	}
	if r := other.app(http.MethodGet, "/v1/measurement/devices", nil); len(arr(r.body, "items")) != 0 {
		t.Fatalf("thiết bị tenant khác: %s", r.raw)
	}
	if _, err := f.ingest.LotMass.LotMass(context.Background(), other.tid, lot); !errors.Is(err, port.ErrNotFound) {
		t.Fatalf("LotMass tenant khác: %v", err)
	}
	// Ghi vào lô cùng mã ở tenant khác tạo lô riêng, không chạm lô MKG.
	o := other.app(http.MethodPost, "/v1/measurement/lots/"+lot.String()+"/tickets", map[string]any{"point": "CUSTOMER_IN", "route": "DIRECT",
		"mass_kg": "5", "weighed_at": f.ts(-time.Minute), "evidence_ref": ids.New().String(), "reason": "Phiếu cân của tài khoản khác"})
	mustStatus(t, o, http.StatusCreated, "tenant khác")
	v, _ := f.ingest.LotMass.LotMass(context.Background(), f.tenant, lot)
	if v.CustomerInKg == nil || v.CustomerInKg.String() != "1000.000" {
		t.Fatalf("lô MKG bị chạm: %+v", v)
	}
}

// Van mrv_collection_evidence: chỉ trường hợp đồng, khối lượng THEO BẬC; revision tăng, evidence_ref giữ.
func TestMRVValveOnlyContractFields(t *testing.T) {
	f := newFixture(t)
	d := f.approvedScale("SCALE-IN-MRV", "HUB_IN")
	lot := ids.New()
	plotA, plotB := ids.New(), ids.New()
	f.lots.m[lot] = hubport.LotInfo{LotID: lot, Sources: []hubport.LotSource{{Kind: "plot", Ref: plotA, Ratio: "0.750000"},
		{Kind: "plot", Ref: plotB, Ratio: "0.250000"}}}
	off := f.officer()
	if r := off.app(http.MethodPost, "/v1/measurement/lots/"+lot.String()+"/mrv-evidence", map[string]any{}); code(r) != "measurement.no_hub_in" {
		t.Fatalf("chưa HUB_IN: %d %s", r.status, r.raw)
	}
	if _, err := f.edgeTicket(d, lot, port.PointHubIn, "96800", 1, 5, nil, ""); err != nil {
		t.Fatal(err)
	}
	p := off.app(http.MethodPost, "/v1/measurement/lots/"+lot.String()+"/mrv-evidence", map[string]any{})
	mustStatus(t, p, http.StatusCreated, "van")
	pubs := arr(p.body, "publications")
	want := contractKeys(t)
	if len(pubs) != 2 {
		t.Fatalf("hai thửa: %s", p.raw)
	}
	buckets := map[string]string{}
	for _, x := range pubs {
		pl := obj(x, "payload")
		ks := keys(pl)
		for _, k := range ks {
			if !contains(want, k) {
				t.Fatalf("trường ngoài hợp đồng %q: %v", k, ks)
			}
		}
		if strings.Contains(string(p.raw), "96800") || strings.Contains(string(p.raw), lot.String()) {
			t.Fatal("khối lượng chính xác / lô không được qua van")
		}
		buckets[str(pl, "plot_ref")] = str(pl, "collected_kg_bucket")
		if pl["revision"] != json.Number("1") || str(pl, "bucket_rule_version") != "mrv.bucket.v1" {
			t.Fatalf("payload: %v", pl)
		}
	}
	if buckets[plotA.String()] != "10000+" || buckets[plotB.String()] != "10000+" {
		t.Fatalf("bậc: %v", buckets)
	}
	// Outbox: kind platform, event_id = id bản ghi van, payload đúng byte đã kiểm.
	id := str(pubs[0], "id")
	f.inTenant(func(tx pgx.Tx) error {
		ctx := context.Background()
		var kind, typ, payload string
		var sum []byte
		if err := tx.QueryRow(ctx, "SELECT kind, event_type, payload::text FROM shared.outbox WHERE id = $1", id).Scan(&kind, &typ, &payload); err != nil {
			return err
		}
		if kind != "platform" || typ != port.EventMRVCollectionEvidencePublished {
			t.Fatalf("outbox: %s %s", kind, typ)
		}
		var raw string
		if err := tx.QueryRow(ctx, "SELECT payload_sha256, payload::text FROM mkg_measurement.valve_export WHERE id = $1", id).Scan(&sum, &raw); err != nil {
			return err
		}
		var pub port.MRVCollectionEvidencePublishedV1
		_ = json.Unmarshal([]byte(raw), &pub)
		b, _ := json.Marshal(pub)
		if s := sha256.Sum256(b); string(s[:]) != string(sum) {
			t.Fatal("payload_sha256")
		}
		return nil
	})
	// Xuất lại: cùng evidence_ref, revision 2.
	p2 := off.app(http.MethodPost, "/v1/measurement/lots/"+lot.String()+"/mrv-evidence", map[string]any{})
	mustStatus(t, p2, http.StatusCreated, "van lần 2")
	ev1 := map[string]string{}
	for _, x := range pubs {
		ev1[str(obj(x, "payload"), "plot_ref")] = str(obj(x, "payload"), "evidence_ref")
	}
	for _, x := range arr(p2.body, "publications") {
		pl := obj(x, "payload")
		if pl["revision"] != json.Number("2") || ev1[str(pl, "plot_ref")] != str(pl, "evidence_ref") {
			t.Fatalf("revision: %v", pl)
		}
	}
}

func contractKeys(t *testing.T) []string {
	t.Helper()
	raw, err := os.ReadFile("../../../../contracts/publication/mrv_collection_evidence.json")
	if err != nil {
		t.Fatal(err)
	}
	var c struct {
		Properties map[string]any `json:"properties"`
	}
	if err := json.Unmarshal(raw, &c); err != nil {
		t.Fatal(err)
	}
	out := make([]string, 0, len(c.Properties))
	for k := range c.Properties {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func contains(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}
