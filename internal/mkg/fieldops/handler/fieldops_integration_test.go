package handler_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/contracts/publication"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/fieldops/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ledger"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// R-56: lệnh gom do một giám sát lập, giám sát KHÁC duyệt; tổ gom không duyệt
// được; DB chặn lần hai bằng CHECK. Duyệt → van collection_schedule.
func TestOrderApprovalSeparationOfDuties(t *testing.T) {
	f := newFixture(t, nil)
	creator := f.supervisor()
	r := creator.post("/v1/fieldops/orders", f.planBody())
	mustStatus(t, r, http.StatusCreated, "lập")
	id := str(r.body, "order", "order_id")
	if str(r.body, "order", "status") != "planned" || str(r.body, "order", "commune_code") != "31117" {
		t.Fatalf("lệnh mới: %s", r.raw)
	}
	if n := len(arr(r.body, "publications")); n != 0 {
		t.Fatalf("lệnh chưa duyệt không được công bố, có %d", n)
	}
	// Tổ gom không lập, không duyệt.
	mustStatus(t, f.collector().post("/v1/fieldops/orders", f.planBody()), http.StatusForbidden, "tổ gom lập")
	mustStatus(t, f.collector().post("/v1/fieldops/orders/"+id+"/decision", map[string]any{"decision": "approved"}), http.StatusForbidden, "tổ gom duyệt")
	// Tự duyệt bị chặn.
	mustCode(t, creator.post("/v1/fieldops/orders/"+id+"/decision", map[string]any{"decision": "approved"}), http.StatusForbidden,
		"self_approval_forbidden", "tự duyệt")
	// Người sửa kế hoạch thành người lập: người lập cũ duyệt được, người sửa thì không.
	reviser := f.supervisor()
	rv := reviser.post("/v1/fieldops/orders/"+id+"/revisions", f.planBody())
	mustStatus(t, rv, http.StatusCreated, "sửa kế hoạch")
	if str(rv.body, "order", "revision") != "2" {
		t.Fatalf("revision: %s", rv.raw)
	}
	mustCode(t, reviser.post("/v1/fieldops/orders/"+id+"/decision", map[string]any{"decision": "approved"}), http.StatusForbidden,
		"self_approval_forbidden", "người sửa tự duyệt")
	d := creator.post("/v1/fieldops/orders/"+id+"/decision", map[string]any{"decision": "approved"})
	mustStatus(t, d, http.StatusCreated, "duyệt")
	if str(d.body, "order", "approved_by") != creator.actor.String() || str(d.body, "order", "status") != "approved" {
		t.Fatalf("duyệt: %s", d.raw)
	}
	pubs := arr(d.body, "publications")
	if len(pubs) != 1 || str(pubs[0], "contract") != port.ContractCollectionSchedule {
		t.Fatalf("van lịch gom: %s", d.raw)
	}
	payload, _ := pubs[0]["payload"].(map[string]any)
	if str(payload, "status") != "confirmed" || str(payload, "schedule_ref") != id {
		t.Fatalf("payload lịch: %v", payload)
	}
	// Sửa sau duyệt bị chặn (hủy rồi lập mới).
	mustCode(t, reviser.post("/v1/fieldops/orders/"+id+"/revisions", f.planBody()), http.StatusConflict, "fieldops.order_transition", "sửa sau duyệt")

	// Lớp hai: DB CHECK chặn dòng approved có actor_ref = creator_ref.
	f.inTenant(func(tx pgx.Tx) error {
		_, err := tx.Exec(context.Background(), `SAVEPOINT s1`)
		if err != nil {
			return err
		}
		_, err = tx.Exec(context.Background(), `INSERT INTO mkg_fieldops.collection_order_event
		    (id, tenant_id, stream_key, order_id, kind, revision, htx_ref, commune_code, window_from, window_to, plot_refs,
		     creator_ref, event_time, actor_ref, idem_key, rule_version)
		  VALUES ($1, $2, 'order:x', $3, 'approved', 1, $4, '31117', current_date, current_date, ARRAY[$4::uuid], $5, now(), $5, $6, 'x')`,
			ids.New(), f.tenant, ids.New(), f.htx, creator.actor, key())
		if err == nil || !strings.Contains(err.Error(), "check") {
			t.Fatalf("DB phải chặn tự duyệt: %v", err)
		}
		_, err = tx.Exec(context.Background(), `ROLLBACK TO SAVEPOINT s1`)
		return err
	})

	// Giao tổ, đóng; hủy lệnh đã đóng bị chặn.
	crew := f.crew("TO-SOD", "5000")
	mustStatus(t, f.assign(creator, id, crew, 1000, nil), http.StatusCreated, "giao tổ")
	cl := creator.post("/v1/fieldops/orders/"+id+"/close", map[string]any{})
	mustStatus(t, cl, http.StatusCreated, "đóng")
	if p, _ := arr(cl.body, "publications")[0]["payload"].(map[string]any); str(p, "status") != "completed" {
		t.Fatalf("đóng → completed: %s", cl.raw)
	}
	mustCode(t, creator.post("/v1/fieldops/orders/"+id+"/cancel", map[string]any{"reason": "hủy sau khi đã đóng lệnh"}),
		http.StatusConflict, "fieldops.order_transition", "hủy lệnh đã đóng")
	g := creator.get("/v1/fieldops/orders/" + id)
	if n := len(arr(g.body, "history")); n != 5 {
		t.Fatalf("lịch sử phải có 5 sự kiện (planned×2, approved, assigned, closed), có %d", n)
	}
}

// Nguồn thửa/HTX qua PlatformDirectory: thửa HTX khác bị chặn, nền tảng không gọi được → 503.
func TestOrderValidatesPlatformDirectory(t *testing.T) {
	f := newFixture(t, nil)
	sup := f.supervisor()
	other := ids.New()
	f.dir.AddHTX(other, "31118")
	foreign := ids.New()
	f.dir.AddPlot(foreign, other, "31118")
	b := f.planBody()
	b["plot_refs"] = []string{f.plots[0].String(), foreign.String()}
	mustCode(t, sup.post("/v1/fieldops/orders", b), http.StatusUnprocessableEntity, "fieldops.plot_other_htx", "thửa HTX khác")
	b["htx_ref"] = ids.New().String()
	mustCode(t, sup.post("/v1/fieldops/orders", b), http.StatusUnprocessableEntity, "fieldops.htx_not_found", "HTX lạ (RB-2)")
	f.dir.Down = true
	mustCode(t, sup.post("/v1/fieldops/orders", f.planBody()), http.StatusServiceUnavailable, "fieldops.directory_unavailable", "nền tảng lỗi")
}

// proto FM-03/FM-04/FM-09: mỗi công đoạn bằng công đoạn trước hoặc ít hơn kèm lý do.
func TestStageCountsFM04(t *testing.T) {
	f := newFixture(t, nil)
	sup := f.supervisor()
	r := sup.post("/v1/fieldops/orders", f.planBody())
	planned := str(r.body, "order", "order_id")
	col := f.collector()
	mustCode(t, f.count(col, planned, f.plots[0], port.StageBale, 10, nil), http.StatusConflict, "fieldops.order_not_open", "lệnh chưa duyệt")

	id := f.approvedOrder()
	p := f.plots[0]
	mustCode(t, f.count(col, id, p, port.StageGather, 5800, nil), http.StatusUnprocessableEntity, "fieldops.previous_stage_missing", "FM-03")
	mustCode(t, f.count(col, id, ids.New(), port.StageBale, 10, nil), http.StatusUnprocessableEntity, "fieldops.plot_not_in_order", "thửa ngoài lệnh")
	mustStatus(t, col.post("/v1/fieldops/orders/"+id+"/roll-counts", map[string]any{"plot_ref": p.String(), "stage": "bale", "event_time": f.ts(0)}),
		http.StatusUnprocessableEntity, "FM-09 thiếu số cuộn")
	mustStatus(t, f.count(col, id, p, port.StageBale, 10, map[string]any{"loose_kg": "100"}), http.StatusUnprocessableEntity, "FM-09 có cuộn lẫn tấn")

	mustStatus(t, f.count(col, id, p, port.StageBale, 5800, nil), http.StatusCreated, "bale")
	mustCode(t, f.count(col, id, p, port.StageBale, 5800, nil), http.StatusConflict, "fieldops.stage_already_counted", "đếm hai lần")
	mustCode(t, f.count(col, id, p, port.StageGather, 5801, nil), http.StatusUnprocessableEntity, "fieldops.stage_exceeds_previous", "nhiều hơn")
	g := f.count(col, id, p, port.StageGather, 5800, nil)
	mustStatus(t, g, http.StatusCreated, "gather bằng")
	mustCode(t, f.count(col, id, p, port.StageBoatLoad, 5700, nil), http.StatusUnprocessableEntity, "fieldops.discrepancy_reason_required", "ít hơn không lý do")
	b := f.count(col, id, p, port.StageBoatLoad, 5700, map[string]any{"discrepancy_reason": "100 cuộn ướt để lại bờ kênh"})
	mustStatus(t, b, http.StatusCreated, "boat_load ít hơn có lý do")

	// Rơm rời: rolls = 0 kèm loose_kg; công đoạn sau cũng phải là rơm rời.
	p2 := f.plots[1]
	mustStatus(t, f.count(col, id, p2, port.StageBale, 0, map[string]any{"loose_kg": "1500.5"}), http.StatusCreated, "rơm rời")
	mustCode(t, f.count(col, id, p2, port.StageGather, 75, nil), http.StatusUnprocessableEntity, "fieldops.unit_mismatch", "rời → cuộn")

	c := sup.get("/v1/fieldops/orders/" + id + "/roll-counts")
	tot, _ := c.body["order_totals"].(map[string]any)
	if str(tot, "bale") != "5800" || str(tot, "gather") != "5800" || str(tot, "boat_load") != "5700" {
		t.Fatalf("tổng theo công đoạn: %s", c.raw)
	}
	// Sự kiện nội bộ mkg.fieldops.roll_count.recorded (KindInternal) cho mỗi bản ghi.
	f.inTenant(func(tx pgx.Tx) error {
		var n int
		if err := tx.QueryRow(context.Background(), `SELECT count(*) FROM shared.outbox WHERE event_type = $1 AND kind = 'internal'`,
			port.EventRollCountRecorded).Scan(&n); err != nil {
			return err
		}
		if n != 4 {
			t.Fatalf("sự kiện nội bộ đếm cuộn: %d (muốn 4)", n)
		}
		var raw string
		if err := tx.QueryRow(context.Background(), `SELECT payload::text FROM shared.outbox WHERE event_type = $1 ORDER BY id LIMIT 1`,
			port.EventRollCountRecorded).Scan(&raw); err != nil {
			return err
		}
		var m map[string]any
		_ = json.Unmarshal([]byte(raw), &m)
		if m["lot_ref"] != id || m["stage"] != "bale" || m["rule_version"] != "fieldops.roll_count.v1" {
			t.Fatalf("payload nội bộ: %s", raw)
		}
		return nil
	})
}

// RB-1: sửa sai bằng bản ghi bù; bản gốc giữ nguyên; bù bản đã bù bị chặn; sửa
// công đoạn trước không được làm công đoạn sau vi phạm FM-04. Sổ bất biến.
func TestRollCountCorrectionByCompensation(t *testing.T) {
	f := newFixture(t, nil)
	id := f.approvedOrder()
	col, p := f.collector(), f.plots[0]
	b := f.count(col, id, p, port.StageBale, 100, nil)
	baleID := str(b.body, "roll_count", "id")
	mustStatus(t, f.count(col, id, p, port.StageGather, 100, nil), http.StatusCreated, "gather")

	corr := func(target string, rolls int, extra map[string]any) resp {
		body := map[string]any{"rolls": rolls, "correction_reason": "đếm lại cùng đại diện HTX", "event_time": f.ts(0)}
		for k, v := range extra {
			body[k] = v
		}
		return col.post("/v1/fieldops/roll-counts/"+target+"/corrections", body)
	}
	mustCode(t, corr(baleID, 90, nil), http.StatusUnprocessableEntity, "fieldops.next_stage_conflict", "bale < gather")
	mustCode(t, corr(baleID, 110, nil), http.StatusUnprocessableEntity, "fieldops.next_stage_conflict", "bale > gather không lý do")
	mustStatus(t, col.post("/v1/fieldops/roll-counts/"+baleID+"/corrections", map[string]any{"rolls": 100, "event_time": f.ts(0)}),
		http.StatusUnprocessableEntity, "bù thiếu lý do")
	c := corr(baleID, 100, map[string]any{"discrepancy_reason": ""})
	mustStatus(t, c, http.StatusCreated, "bù cùng số")
	newID := str(c.body, "roll_count", "id")
	if str(c.body, "roll_count", "supersedes_id") != baleID || str(c.body, "total", "roll_count_id") != newID {
		t.Fatalf("bản bù: %s", c.raw)
	}
	mustCode(t, corr(baleID, 100, nil), http.StatusConflict, "fieldops.already_corrected", "bù bản đã bù")

	all := f.supervisor().get("/v1/fieldops/orders/" + id + "/roll-counts")
	if n := len(arr(all.body, "counts")); n != 3 {
		t.Fatalf("sổ giữ cả bản gốc và bản bù: %d", n)
	}
	// Sổ chỉ ghi thêm: vai ứng dụng không UPDATE/DELETE được.
	ctx := context.Background()
	for _, q := range []string{
		"UPDATE mkg_fieldops.roll_count SET rolls = 1 WHERE id = $1",
		"DELETE FROM mkg_fieldops.roll_count WHERE id = $1",
		"UPDATE mkg_fieldops.collection_order_event SET kind = 'closed' WHERE order_id = $1",
	} {
		err := pgxTx(t, f, func(tx pgx.Tx) error {
			arg := baleID
			if strings.Contains(q, "collection_order_event") {
				arg = id
			}
			_, e := tx.Exec(ctx, q, arg)
			return e
		})
		if err == nil {
			t.Fatalf("%s phải bị chặn", q)
		}
	}
	// Chuỗi băm của lô gom còn nguyên.
	f.inTenant(func(tx pgx.Tx) error {
		v, err := ledger.Verify(ctx, tx, "mkg_fieldops.roll_count", f.tenant, "lot:"+id)
		if err != nil {
			return err
		}
		if !v.OK || v.RowsChecked != 3 {
			t.Fatalf("chuỗi băm: %+v", v)
		}
		return nil
	})
}

func pgxTx(t *testing.T, f *fixture, fn func(tx pgx.Tx) error) error {
	t.Helper()
	return tenant.WithTenantTx(context.Background(), f.pool, f.tenant, fn)
}

func parseID(t *testing.T, s string) uuid.UUID {
	t.Helper()
	id, err := uuid.Parse(s)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

// RB-6 chiều mua: số MKG (xếp ghe/xe) ↔ xác nhận HTX: pending → match → (MKG sửa)
// mismatch; bảng kê qua van htx_collection_statement chỉ có trường hợp đồng.
func TestBuySideReconciliation(t *testing.T) {
	f := newFixture(t, nil)
	id := f.approvedOrder()
	col, sup, p := f.collector(), f.supervisor(), f.plots[0]
	mustStatus(t, f.count(col, id, p, port.StageBale, 120, nil), http.StatusCreated, "bale")
	mustStatus(t, f.count(col, id, p, port.StageGather, 120, nil), http.StatusCreated, "gather")
	confirm := func(body map[string]any) resp {
		body["plot_ref"] = p.String()
		body["event_time"] = f.ts(0)
		return sup.post("/v1/fieldops/orders/"+id+"/htx-confirmations", body)
	}
	mustCode(t, confirm(map[string]any{"outcome": "confirmed"}), http.StatusConflict, "fieldops.nothing_to_confirm", "chưa xếp ghe")

	bl := f.count(col, id, p, port.StageBoatLoad, 120, nil)
	mustStatus(t, bl, http.StatusCreated, "boat_load")
	if str(bl.body, "reconciliation", "status") != "pending" || str(bl.body, "reconciliation", "statement_revision") != "1" {
		t.Fatalf("đối soát mới: %s", bl.raw)
	}
	pubs := arr(bl.body, "publications")
	if len(pubs) != 1 || str(pubs[0], "contract") != port.ContractHTXCollectionStatement {
		t.Fatalf("bảng kê qua van: %s", bl.raw)
	}
	payload, _ := pubs[0]["payload"].(map[string]any)
	c, err := publication.Get(port.ContractHTXCollectionStatement)
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range keys(payload) {
		if !slices.Contains(c.AllowedFields(), k) {
			t.Fatalf("trường %q ngoài hợp đồng van", k)
		}
	}
	if str(payload, "rolls") != "120" || str(payload, "plot_ref") != p.String() {
		t.Fatalf("payload bảng kê: %v", payload)
	}

	mustStatus(t, confirm(map[string]any{"outcome": "confirmed", "htx_rolls": 119}), http.StatusUnprocessableEntity, "confirmed khác số")
	mustStatus(t, confirm(map[string]any{"outcome": "disputed", "htx_rolls": 110}), http.StatusUnprocessableEntity, "tranh chấp thiếu lý do")
	ok := confirm(map[string]any{"outcome": "confirmed", "statement_revision": 1})
	mustStatus(t, ok, http.StatusCreated, "HTX xác nhận")
	if str(ok.body, "reconciliation", "status") != "match" || str(ok.body, "confirmation", "channel") != "mkg_on_behalf" {
		t.Fatalf("khớp: %s", ok.raw)
	}
	// MKG sửa số xếp ghe sau khi HTX đã xác nhận → lệch, bảng kê phiên bản 2.
	cr := col.post("/v1/fieldops/roll-counts/"+str(bl.body, "roll_count", "id")+"/corrections", map[string]any{
		"rolls": 118, "discrepancy_reason": "hai cuộn rơi xuống kênh", "correction_reason": "đếm lại khi cập bến", "event_time": f.ts(0)})
	mustStatus(t, cr, http.StatusCreated, "sửa boat_load")
	if str(cr.body, "reconciliation", "status") != "mismatch" || str(cr.body, "reconciliation", "statement_revision") != "2" {
		t.Fatalf("lệch sau sửa: %s", cr.raw)
	}
	mustCode(t, confirm(map[string]any{"outcome": "disputed", "htx_rolls": 120, "reason": "HTX đếm được 120 cuộn", "statement_revision": 1}),
		http.StatusConflict, "fieldops.statement_stale", "xác nhận bảng kê cũ")
	d := confirm(map[string]any{"outcome": "disputed", "htx_rolls": 120, "reason": "HTX đếm được 120 cuộn trên ghe"})
	mustStatus(t, d, http.StatusCreated, "tranh chấp")
	if str(d.body, "reconciliation", "status") != "mismatch" || str(d.body, "reconciliation", "htx_rolls") != "120" {
		t.Fatalf("tranh chấp: %s", d.raw)
	}
	rec := sup.get("/v1/fieldops/reconciliation?status=mismatch&order_id=" + id)
	if n := len(arr(rec.body, "items")); n != 1 {
		t.Fatalf("đối soát lệch: %s", rec.raw)
	}
	f.inTenant(func(tx pgx.Tx) error {
		var plat, internal int
		if err := tx.QueryRow(context.Background(), `SELECT count(*) FILTER (WHERE kind = 'platform'), count(*) FILTER (WHERE kind = 'internal')
		    FROM shared.outbox WHERE producer = 'mkg/fieldops'`).Scan(&plat, &internal); err != nil {
			return err
		}
		// Van: lịch (duyệt) + 2 bảng kê; nội bộ: 4 bản ghi đếm.
		if plat != 3 || internal != 4 {
			t.Fatalf("outbox platform=%d internal=%d", plat, internal)
		}
		return nil
	})
}

// Van chỉ mang trường hợp đồng: không khối lượng, kg/cuộn, giá, tổ gom.
func TestValvePayloadContractFieldsOnly(t *testing.T) {
	f := newFixture(t, nil)
	id := f.approvedOrder()
	col, p := f.collector(), f.plots[0]
	f.count(col, id, p, port.StageBale, 50, nil)
	f.count(col, id, p, port.StageGather, 50, nil)
	f.count(col, id, p, port.StageBoatLoad, 50, nil)
	mustStatus(t, f.assign(f.supervisor(), id, f.crew("TO-VAN", "5000"), 150, nil), http.StatusCreated, "giao tổ")
	r := f.supervisor().get("/v1/fieldops/orders/" + id + "/publications")
	items := arr(r.body, "items")
	if len(items) != 3 {
		t.Fatalf("3 bản xuất (lịch duyệt, bảng kê, lịch giao tổ): %s", r.raw)
	}
	for _, x := range items {
		c, err := publication.Get(str(x, "contract"))
		if err != nil {
			t.Fatal(err)
		}
		payload, _ := x["payload"].(map[string]any)
		for _, k := range keys(payload) {
			if !slices.Contains(c.AllowedFields(), k) {
				t.Fatalf("%s: trường %q ngoài hợp đồng", c.Name, k)
			}
			for _, banned := range []string{"kg", "mass", "price", "crew", "yield", "ton"} {
				if strings.Contains(k, banned) {
					t.Fatalf("%s: trường cấm %q", c.Name, k)
				}
			}
		}
	}
}

// Đồng bộ app hiện trường MKG: push idempotent theo idem_key và id, kết quả theo
// từng bản ghi; pull theo con trỏ có chữ ký.
func TestSyncPushIdempotentAndPull(t *testing.T) {
	f := newFixture(t, nil)
	id := f.approvedOrder()
	col := f.collector()
	col.device = ids.New().String()
	// Lọc pull theo tổ: tài khoản tổ gom là thành viên tổ được giao lệnh này.
	mustStatus(t, f.assign(f.supervisor(), id, f.crew("TO-SYNC", "5000", col.actor), 200, nil), http.StatusCreated, "giao tổ")
	rec := func(stage string, rolls int, seq string) map[string]any {
		return map[string]any{
			"id": ids.New().String(), "idem_key": "dev:" + col.device + ":" + seq, "entity": "roll_count", "op": "create",
			"event_time": f.ts(-time.Hour), "supersedes_id": nil, "correction_reason": nil,
			"payload": map[string]any{"order_id": id, "plot_ref": f.plots[0].String(), "stage": stage, "rolls": rolls},
		}
	}
	r1, r2 := rec("bale", 80, "1"), rec("gather", 90, "2")
	batch := map[string]any{"device_id": col.device, "records": []any{r1, r2}}
	k := key()
	p := col.do(http.MethodPost, "/v1/fieldops/sync/push", k, batch)
	mustStatus(t, p, http.StatusOK, "push")
	res := arr(p.body, "results")
	if str(res[0], "status") != "accepted" || str(res[1], "status") != "rejected" || str(res[1], "code") != "fieldops.stage_exceeds_previous" {
		t.Fatalf("kết quả theo bản ghi: %s", p.raw)
	}
	// Cùng Idempotency-Key → cùng phản hồi.
	again := col.do(http.MethodPost, "/v1/fieldops/sync/push", k, batch)
	if again.status != http.StatusOK || string(again.raw) != string(p.raw) {
		t.Fatalf("phát lại lô: %s", again.raw)
	}
	// Lô khác gửi lại r1 → duplicate; id của r1 với idem_key mới → id_reused; idem_key của máy khác → rejected.
	r3 := rec("bale", 80, "3")
	r3["id"] = r1["id"]
	r4 := rec("bale", 80, "4")
	r4["idem_key"] = "dev:" + ids.New().String() + ":4"
	p2 := col.post("/v1/fieldops/sync/push", map[string]any{"device_id": col.device, "records": []any{r1, r3, r4}})
	res = arr(p2.body, "results")
	if str(res[0], "status") != "duplicate" || str(res[1], "code") != "fieldops.id_reused" || str(res[2], "code") != "validation_failed" {
		t.Fatalf("lô 2: %s", p2.raw)
	}
	// device_id khác header → 403.
	mustCode(t, col.post("/v1/fieldops/sync/push", map[string]any{"device_id": ids.New().String(), "records": []any{}}),
		http.StatusForbidden, "fieldops.device_mismatch", "thiết bị khác")
	c := f.supervisor().get("/v1/fieldops/orders/" + id + "/roll-counts")
	counts := arr(c.body, "counts")
	if len(counts) != 1 || str(counts[0], "device_id") != col.device || str(counts[0], "id") != r1["id"] {
		t.Fatalf("sổ chỉ có một bản ghi từ máy: %s", c.raw)
	}

	// Pull: lệnh đã duyệt đi xuống; con trỏ sửa tay bị từ chối.
	pl := col.get("/v1/fieldops/sync/pull")
	mustStatus(t, pl, http.StatusOK, "pull")
	ch := arr(pl.body, "changes")
	if len(ch) != 1 || str(ch[0], "id") != id || str(ch[0], "op") != "upsert" || str(ch[0], "entity") != "collection_order" {
		t.Fatalf("pull: %s", pl.raw)
	}
	cur := str(pl.body, "next_cursor")
	if n := len(arr(col.get("/v1/fieldops/sync/pull?cursor="+cur).body, "changes")); n != 0 {
		t.Fatalf("pull sau con trỏ phải rỗng: %d", n)
	}
	f.supervisor().post("/v1/fieldops/orders/"+id+"/cancel", map[string]any{"reason": "mưa lớn, hoãn gom sang tuần sau"})
	tomb := arr(col.get("/v1/fieldops/sync/pull?cursor="+cur).body, "changes")
	if len(tomb) != 1 || str(tomb[0], "op") != "tombstone" {
		t.Fatalf("hủy → tombstone: %v", tomb)
	}
	bad := cur[:len(cur)-2] + "AA"
	mustCode(t, col.get("/v1/fieldops/sync/pull?cursor="+bad), http.StatusBadRequest, "fieldops.cursor_invalid", "con trỏ giả")
	other := f.collector()
	other.device = ids.New().String()
	mustCode(t, other.get("/v1/fieldops/sync/pull?cursor="+cur), http.StatusBadRequest, "fieldops.cursor_invalid", "con trỏ máy khác")
}

// kg/cuộn: HTX (30 lô cân gần nhất) → hệ thống → cấu hình → 20. Không bao giờ qua van.
func TestKgPerRollLearningFallback(t *testing.T) {
	cfg := decimal.RequireFromString("18.5")
	f := newFixture(t, nil)
	sup := f.supervisor()
	get := func(fx *fixture, htx string) (string, string) {
		r := fx.supervisor().get("/v1/fieldops/htx/" + htx + "/kg-per-roll")
		mustStatus(t, r, http.StatusOK, "kg/cuộn")
		return str(r.body, "kg_per_roll"), str(r.body, "source")
	}
	if kg, src := get(f, f.htx.String()); kg != "20.0" || src != "default" {
		t.Fatalf("mặc định: %s %s", kg, src)
	}
	g := newFixture(t, &cfg)
	if kg, src := get(g, g.htx.String()); kg != "18.5" || src != "config" {
		t.Fatalf("cấu hình: %s %s", kg, src)
	}
	// Lô 5800 → 5700 cuộn cân 96,8 t (dữ liệu golden) → 17,0 kg/cuộn cho HTX.
	id := f.approvedOrder()
	col, p := f.collector(), f.plots[0]
	f.count(col, id, p, port.StageBale, 5800, nil)
	f.count(col, id, p, port.StageGather, 5800, nil)
	f.count(col, id, p, port.StageBoatLoad, 5700, map[string]any{"discrepancy_reason": "100 cuộn ướt để lại bờ kênh"})
	mustStatus(t, sup.post("/v1/fieldops/kg-per-roll/refresh", map[string]any{}), http.StatusOK, "refresh chưa cân")
	if _, src := get(f, f.htx.String()); src != "default" {
		t.Fatalf("chưa cân thì chưa học: %s", src)
	}
	f.weighed.Set(parseID(t, id), "96800", f.clk.Now())
	rf := sup.post("/v1/fieldops/kg-per-roll/refresh", map[string]any{})
	if str(rf.body, "weighed_lots_added") != "1" {
		t.Fatalf("refresh: %s", rf.raw)
	}
	if kg, src := get(f, f.htx.String()); kg != "17.0" || src != "htx" {
		t.Fatalf("học theo HTX: %s %s", kg, src)
	}
	if kg, src := get(f, ids.New().String()); kg != "17.0" || src != "system" {
		t.Fatalf("HTX khác → hệ thống: %s %s", kg, src)
	}
	mustStatus(t, f.collector().post("/v1/fieldops/kg-per-roll/refresh", map[string]any{}), http.StatusForbidden, "tổ gom không refresh")
}

// RB-2: tenant khác không thấy, không ghi được vào lệnh gom của tenant MKG.
func TestTenantIsolation(t *testing.T) {
	f := newFixture(t, nil)
	id := f.approvedOrder()
	f.count(f.collector(), id, f.plots[0], port.StageBale, 10, nil)
	other := ids.New()
	sup := f.as(other, port.RoleFieldSupervisor)
	mustCode(t, sup.get("/v1/fieldops/orders/"+id), http.StatusNotFound, "fieldops.order_not_found", "đọc chéo tenant")
	mustCode(t, sup.get("/v1/fieldops/orders/"+id+"/roll-counts"), http.StatusNotFound, "fieldops.order_not_found", "sổ chéo tenant")
	col := f.as(other, port.RoleFieldCollector)
	mustCode(t, f.count(col, id, f.plots[0], port.StageGather, 10, nil), http.StatusNotFound, "fieldops.order_not_found", "ghi chéo tenant")
	if n := len(arr(sup.get("/v1/fieldops/orders").body, "items")); n != 0 {
		t.Fatalf("danh sách chéo tenant: %d", n)
	}
	if n := len(arr(sup.get("/v1/fieldops/reconciliation").body, "items")); n != 0 {
		t.Fatalf("đối soát chéo tenant: %d", n)
	}
	// RollTotals (port cho measurement/costing) cũng theo tenant.
	if _, err := f.svc.LotRolls(context.Background(), other, parseID(t, id)); !errors.Is(err, port.ErrLotNotFound) {
		t.Fatalf("LotRolls chéo tenant: %v", err)
	}
	lr, err := f.svc.LotRolls(context.Background(), f.tenant, parseID(t, id))
	if err != nil || lr.Rolls != 10 || lr.Stage != port.StageBale {
		t.Fatalf("LotRolls: %+v %v", lr, err)
	}
}

// Idempotency-Key: POST lặp trả cùng lệnh gom (200), không ghi thêm sự kiện.
func TestIdempotentOrderReplay(t *testing.T) {
	f := newFixture(t, nil)
	sup := f.supervisor()
	k := key()
	a := sup.do(http.MethodPost, "/v1/fieldops/orders", k, f.planBody())
	mustStatus(t, a, http.StatusCreated, "lần 1")
	b := sup.do(http.MethodPost, "/v1/fieldops/orders", k, f.planBody())
	if b.status != http.StatusCreated && b.status != http.StatusOK {
		t.Fatalf("lần 2: %d", b.status)
	}
	if str(a.body, "order", "order_id") != str(b.body, "order", "order_id") {
		t.Fatal("phát lại phải trả cùng lệnh gom")
	}
	if n := len(arr(sup.get("/v1/fieldops/orders").body, "items")); n != 1 {
		t.Fatalf("chỉ một lệnh gom: %d", n)
	}
	// Ca tổ gom: ghi được trên lệnh đã duyệt.
	id := f.approvedOrder()
	s := f.collector().post("/v1/fieldops/orders/"+id+"/crew-shifts", map[string]any{
		"crew_ref": ids.New().String(), "kind": "opened", "headcount": 6, "equipment_refs": []string{"MAY-CUON-01"}, "event_time": f.ts(0)})
	mustStatus(t, s, http.StatusCreated, "mở ca")
	if n := len(arr(f.collector().get("/v1/fieldops/orders/"+id+"/crew-shifts").body, "items")); n != 1 {
		t.Fatalf("ca: %d", n)
	}
}
