package handler_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/mrv/port"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/inbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/outbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

func envelope(tid uuid.UUID, typ string, payload any) outbox.Envelope {
	b, _ := json.Marshal(payload)
	return outbox.Envelope{EventID: ids.New(), EventType: typ, SchemaVersion: 1, TenantID: tid, OccurredAt: time.Now().UTC(),
		RecordedAt: time.Now().UTC(), Producer: "test", Payload: b}
}

func (f *fixture) farmLog(env outbox.Envelope) (bool, error) {
	return inbox.Process(context.Background(), f.pool, port.FarmLogLinkConsumer, env, f.mod.FarmLogLinkHandler())
}

func (f *fixture) valve(env outbox.Envelope) (bool, error) {
	return inbox.Process(context.Background(), f.pool, port.CollectionEvidenceConsumer, env, f.mod.CollectionEvidenceHandler())
}

func (f *fixture) must(ok bool, err error) {
	f.t.Helper()
	if err != nil {
		f.t.Fatal(err)
	}
}

type g3Season struct {
	ref, plot uuid.UUID
	unit      string
}

// openSeason phát pf.cropcycle.season.opened cho một thửa mới ở xã unit.
func (f *fixture) openSeason(tid uuid.UUID, unit string, openedAt time.Time) g3Season {
	f.t.Helper()
	s := g3Season{ref: ids.New(), plot: ids.New(), unit: unit}
	f.must(f.farmLog(envelope(tid, port.EventSeasonOpened, map[string]any{
		"season_ref": s.ref, "event_id": ids.New(), "plot_ref": s.plot, "commune_code": unit, "season_code": "2026-DX",
		"season_year": 2026, "sowing_date": openedAt.Format("2006-01-02"), "actor_ref": ids.New(),
		"event_time": openedAt.Format(time.RFC3339),
	})))
	return s
}

func logRecorded(s g3Season, logRef, logID uuid.UUID, version int, activity string, at time.Time) map[string]any {
	return map[string]any{
		"log_id": logID, "log_ref": logRef, "version": version, "season_ref": s.ref, "plot_ref": s.plot, "household_ref": nil,
		"cooperative_ref": ids.New(), "commune_code": s.unit, "activity_code": activity, "sop_step_seq": nil,
		"location_status": "matched", "gps_source": "device", "batch_ref": nil, "became_current": true, "supersedes_id": nil,
		"on_behalf": false, "author_ref": ids.New(), "event_time": at.Format(time.RFC3339),
	}
}

func collection(s g3Season, ref uuid.UUID, rev int, bucket string) map[string]any {
	return map[string]any{
		"evidence_ref": ref, "plot_ref": s.plot, "season_ref": s.ref, "collected_on": "2026-09-20",
		"collected_kg_bucket": bucket, "bucket_rule_version": "mrv.bucket.v1", "revision": rev,
	}
}

func straw(s g3Season, at time.Time) map[string]any {
	return map[string]any{
		"evidence_type": port.EvidenceStrawHandling, "admin_unit_code": s.unit, "plot_ref": s.plot.String(), "season_code": "2026-DX",
		"values":     map[string]any{"handling_method": "cuon_thu_gom", "observed_burning": false},
		"file_refs":  []any{map[string]any{"file_ref": ids.New().String(), "sha256": sha()}},
		"gps":        map[string]any{"lat": "10.0300000", "lon": "105.7800000"},
		"event_time": at.Format(time.RFC3339),
	}
}

func (f *fixture) count(tid uuid.UUID, table string) int {
	f.t.Helper()
	var n int
	err := tenant.WithTenantTx(context.Background(), f.pool, tid, func(tx pgx.Tx) error {
		return tx.QueryRow(context.Background(), "SELECT count(*) FROM pf_mrv."+table).Scan(&n)
	})
	if err != nil {
		f.t.Fatal(err)
	}
	return n
}

func items(r resp) []map[string]any {
	raw, _ := r.body["items"].([]any)
	out := make([]map[string]any, 0, len(raw))
	for _, it := range raw {
		out = append(out, it.(map[string]any))
	}
	return out
}

func itemByKey(r resp, key string) map[string]any {
	for _, it := range r.body["items"].([]any) {
		m := it.(map[string]any)
		if m["key"] == key {
			return m
		}
	}
	return nil
}

// KN US-MRV-02 + R-61: liên kết nhật ký HTX idempotent (giao lặp, phát lại event_id mới),
// ghim phiên bản danh mục htx_farm_log ĐÃ DUYỆT lúc liên kết; chưa duyệt → không tính.
func TestG3FarmLogLinkIdempotentPinnedKNUSMRV02(t *testing.T) {
	f := newFixture(t)
	view := f.as(f.tenant, f.p89, port.RoleViewer)
	s := f.openSeason(f.tenant, "30001", time.Now().Add(-48*time.Hour))

	// Nhật ký trước khi có danh mục duyệt → liên kết không ghim.
	early := ids.New()
	f.must(f.farmLog(envelope(f.tenant, port.EventFarmLogRecorded, logRecorded(s, early, ids.New(), 1, "lam_dat", time.Now().Add(-40*time.Hour)))))

	f.approveSeed(f.tenant)
	logRef, logID := ids.New(), ids.New()
	env := envelope(f.tenant, port.EventFarmLogRecorded, logRecorded(s, logRef, logID, 1, "tuoi_nuoc", time.Now().Add(-24*time.Hour)))
	for i := 0; i < 2; i++ {
		processed, err := f.farmLog(env)
		if err != nil || processed != (i == 0) {
			t.Fatalf("giao lần %d: %v %v", i+1, processed, err)
		}
	}
	replay := env
	replay.EventID = ids.New() // publisher phát lại cùng phiên bản nhật ký với event_id mới
	f.must(f.farmLog(replay))
	if n := f.count(f.tenant, "farm_log_link"); n != 2 {
		t.Fatalf("mong 2 liên kết (early + logRef), có %d", n)
	}
	// HTX duyệt nhật ký (sự kiện reviewed) — cũng idempotent theo review_id.
	rev := envelope(f.tenant, port.EventFarmLogReviewed, map[string]any{
		"review_id": ids.New(), "log_id": logID, "log_ref": logRef, "season_ref": s.ref, "cooperative_ref": ids.New(),
		"decision": "approved", "note": nil, "author_ref": ids.New(), "reviewer_ref": ids.New(), "event_time": time.Now().Format(time.RFC3339),
	})
	f.must(f.farmLog(rev))
	rev.EventID = ids.New()
	f.must(f.farmLog(rev))
	if n := f.count(f.tenant, "farm_log_link"); n != 3 {
		t.Fatalf("mong 3 dòng sổ liên kết, có %d", n)
	}

	c := view.app(http.MethodGet, "/v1/mrv/seasons/"+s.ref.String()+"/completeness", nil)
	expect(t, c, 200, "")
	logItem := itemByKey(c, port.EvidenceHTXFarmLog)
	if logItem["count"] != float64(2) || logItem["approved_count"] != float64(1) || logItem["status"] != "approved" {
		t.Fatalf("htx_farm_log: %v", logItem)
	}
	cmp := view.app(http.MethodGet, "/v1/mrv/seasons/"+s.ref.String()+"/comparison", nil)
	expect(t, cmp, 200, "")
	pinned := 0
	for _, l := range cmp.body["lines"].([]any) {
		m := l.(map[string]any)
		if m["log_ref"] == logRef.String() {
			if m["catalog_version"] != float64(1) || m["htx_review_decision"] != "approved" {
				t.Fatalf("liên kết phải ghim v1 và mang quyết định HTX: %v", m)
			}
			pinned++
		}
		if m["log_ref"] == early.String() && m["catalog_version"] != nil {
			t.Fatalf("liên kết trước khi duyệt danh mục không được ghim: %v", m)
		}
	}
	if pinned != 1 {
		t.Fatalf("thiếu dòng nhật ký: %v", cmp.body)
	}
}

// Van mrv_collection_evidence: kiểm hợp đồng lúc chạy (khóa lạ → từ chối, không ghi),
// idempotent theo (evidence_ref, revision), bản hiện hành = revision lớn nhất.
func TestG3CollectionValveContractAndIdempotency(t *testing.T) {
	f := newFixture(t)
	s := f.openSeason(f.tenant, "30001", time.Now().Add(-24*time.Hour))
	ref := ids.New()

	bad := collection(s, ref, 1, "2000-5000")
	bad["mass_kg"] = "3456.7" // khối lượng chính xác KHÔNG được qua van
	if _, err := f.valve(envelope(f.mkg, port.EventMKGCollectionEvid, bad)); err == nil {
		t.Fatal("payload có khóa ngoài hợp đồng phải bị từ chối")
	}
	missing := collection(s, ref, 1, "2000-5000")
	delete(missing, "revision")
	if _, err := f.valve(envelope(f.mkg, port.EventMKGCollectionEvid, missing)); err == nil {
		t.Fatal("thiếu trường bắt buộc phải bị từ chối")
	}
	wrong := collection(s, ref, 1, "3000")
	if _, err := f.valve(envelope(f.mkg, port.EventMKGCollectionEvid, wrong)); err == nil {
		t.Fatal("bậc ngoài enum phải bị từ chối")
	}
	if n := f.count(f.tenant, "collection_evidence"); n != 0 {
		t.Fatalf("payload hỏng không được ghi: %d", n)
	}

	env := envelope(f.mkg, port.EventMKGCollectionEvid, collection(s, ref, 1, "2000-5000"))
	f.must(f.valve(env))
	f.must(f.valve(env))
	env.EventID = ids.New()
	f.must(f.valve(env))
	if n := f.count(f.tenant, "collection_evidence"); n != 1 {
		t.Fatalf("cùng revision phải là một dòng, có %d", n)
	}
	f.must(f.valve(envelope(f.mkg, port.EventMKGCollectionEvid, collection(s, ref, 2, "5000-10000"))))
	// revision cũ tới muộn: ghi sổ nhưng không thành bản hiện hành.
	f.must(f.valve(envelope(f.mkg, port.EventMKGCollectionEvid, collection(s, ref, 0, "0-500"))))

	view := f.as(f.tenant, f.p89, port.RoleViewer)
	c := view.app(http.MethodGet, "/v1/mrv/seasons/"+s.ref.String()+"/completeness", nil)
	expect(t, c, 200, "")
	col := itemByKey(c, "collection_evidence")
	if col["count"] != float64(1) || col["status"] != "present" {
		t.Fatalf("collection_evidence: %v", col)
	}
	est := c.body["estimate"].(map[string]any)
	inputs := est["inputs"].([]any)
	if len(inputs) != 1 || inputs[0].(map[string]any)["revision"] != float64(2) {
		t.Fatalf("ước tính phải dùng revision 2: %v", est)
	}
	// ADR-0020: phong bì của tenant MKG ghi vào tenant bên cấp — tenant MKG không có dòng pf_mrv;
	// tenant phong bì chưa có cấp quyền valve_mrv_evidence → party.valve_share_grant_missing (DLQ).
	if n := f.count(f.mkg, "collection_evidence"); n != 0 {
		t.Fatalf("tenant MKG không được có bằng chứng ở pf: %d", n)
	}
	if _, err := f.valve(envelope(ids.New(), port.EventMKGCollectionEvid, collection(s, ids.New(), 1, "2000-5000"))); !errors.Is(err, partyport.ErrValveGrantMissing) {
		t.Fatalf("không có cấp quyền → party.valve_share_grant_missing: %v", err)
	}
	if n := f.count(f.tenant, "collection_evidence"); n != 3 {
		t.Fatalf("phong bì không có cấp quyền không được ghi: %d", n)
	}
}

// KN US-MRV-05: chỉ minh chứng đã "Đạt chuẩn" mới được tính vào giảm phát thải;
// ước tính luôn TẠM khi chưa có phương pháp đã duyệt.
func TestG3OnlyApprovedEvidenceCountsKNUSMRV05(t *testing.T) {
	f := newFixture(t)
	f.approveSeed(f.tenant)
	s := f.openSeason(f.tenant, "30001", time.Now().Add(-72*time.Hour))
	f.must(f.valve(envelope(f.mkg, port.EventMKGCollectionEvid, collection(s, ids.New(), 1, "2000-5000"))))
	view := f.as(f.tenant, f.p89, port.RoleViewer)

	get := func() map[string]any {
		r := view.app(http.MethodGet, "/v1/mrv/estimates?season_ref="+s.ref.String(), nil)
		expect(t, r, 200, "")
		it := items(r)
		if len(it) != 1 {
			t.Fatalf("mong 1 ước tính: %v", r.body)
		}
		return it[0]
	}
	e := get()
	if e["counted_tonnes"] != "0.000" || e["avoided_burning_kgco2e"] != "0.000" || e["status"] != "provisional" || e["official"] != false {
		t.Fatalf("chưa có minh chứng đạt chuẩn → 0, tạm: %v", e)
	}
	if e["rule_version"] != port.RuleEstimate || e["label"] != "Tạm — chờ khách ký phương pháp" || e["transport_status"] != "no_leg_data" {
		t.Fatalf("nhãn/rule_version: %v", e)
	}

	col := f.as(f.tenant, f.t30001, port.RoleCollector)
	ev := col.app(http.MethodPost, "/v1/mrv/evidence", straw(s, time.Now().Add(-time.Hour)))
	expect(t, ev, 201, "")
	// Bị "Yêu cầu kiểm tra lại" → vẫn không tính.
	rev := f.as(f.tenant, f.p89, port.RoleReviewer)
	expect(t, rev.app(http.MethodPost, "/v1/mrv/evidence/"+ev.str("id")+"/reviews",
		map[string]any{"decision": "rejected", "reason": "Ảnh mờ, chụp lại"}), 201, "")
	if e := get(); e["counted_tonnes"] != "0.000" {
		t.Fatalf("minh chứng bị từ chối không được tính: %v", e)
	}
	fix := straw(s, time.Now().Add(-30*time.Minute))
	fix["supersedes_id"], fix["correction_reason"] = ev.str("id"), "Chụp lại ảnh rõ theo yêu cầu"
	ev2 := col.app(http.MethodPost, "/v1/mrv/evidence", fix)
	expect(t, ev2, 201, "")
	expect(t, rev.app(http.MethodPost, "/v1/mrv/evidence/"+ev2.str("id")+"/reviews",
		map[string]any{"decision": "accepted", "reason": "Đạt chuẩn"}), 201, "")
	e = get()
	if e["counted_tonnes"] != "2.000" || e["avoided_burning_kgco2e"] != "2920.000" || e["status"] != "provisional" {
		t.Fatalf("2 t (cận dưới bậc) × 1460 = 2920 kgCO2e, tạm: %v", e)
	}
	params := e["params"].([]any)
	if params[0].(map[string]any)["source"] != "default" {
		t.Fatalf("chưa có tham số duyệt → mặc định: %v", params)
	}
	if e["supersedes_id"] == nil {
		t.Fatalf("tính lại phải là bản ghi bù của ước tính trước: %v", e)
	}
	c := view.app(http.MethodGet, "/v1/mrv/seasons/"+s.ref.String()+"/completeness", nil)
	if c.body["counts_toward_estimate"] != true || itemByKey(c, port.EvidenceStrawHandling)["status"] != "approved" {
		t.Fatalf("completeness sau duyệt: %v", c.body)
	}
	// Tính lại không đổi đầu vào → không ghi dòng mới.
	before := f.count(f.tenant, "estimate")
	expect(t, rev.app(http.MethodPost, "/v1/mrv/seasons/"+s.ref.String()+"/estimate/recompute", nil), 200, "")
	if f.count(f.tenant, "estimate") != before {
		t.Fatal("cùng đầu vào không được ghi thêm ước tính")
	}
}

// Tạm ↔ chính thức (như pf/simulation FN-01 BR-04): chỉ chính thức khi phương pháp
// ĐÃ DUYỆT bởi người khác (R-56) ghim đúng phiên bản các tham số đã duyệt đã dùng.
func TestG3ProvisionalVsOfficialMethodApproval(t *testing.T) {
	f := newFixture(t)
	f.approveSeed(f.tenant)
	s := f.openSeason(f.tenant, "30001", time.Now().Add(-72*time.Hour))
	col := f.as(f.tenant, f.t30001, port.RoleCollector)
	rev := f.as(f.tenant, f.p89, port.RoleReviewer)
	ev := col.app(http.MethodPost, "/v1/mrv/evidence", straw(s, time.Now().Add(-time.Hour)))
	expect(t, ev, 201, "")
	expect(t, rev.app(http.MethodPost, "/v1/mrv/evidence/"+ev.str("id")+"/reviews", map[string]any{"decision": "accepted", "reason": "Đạt chuẩn"}), 201, "")
	f.must(f.valve(envelope(f.mkg, port.EventMKGCollectionEvid, collection(s, ids.New(), 1, "10000+"))))

	f.params.set(port.ParamCO2OpenBurning, "1460", 3)
	f.params.set(port.ParamCO2TransportRoad, "0.062", 1)
	f.params.set(port.ParamCO2TransportWaterway, "0.021", 1)
	recompute := func() map[string]any {
		r := rev.app(http.MethodPost, "/v1/mrv/seasons/"+s.ref.String()+"/estimate/recompute", nil)
		expect(t, r, 200, "")
		return r.body["estimate"].(map[string]any)
	}
	e := recompute()
	if e["status"] != "provisional" || e["avoided_burning_kgco2e"] != "14600.000" {
		t.Fatalf("tham số đã duyệt nhưng chưa có phương pháp → vẫn tạm: %v", e)
	}

	setter := f.as(f.tenant, uuid.Nil, authz.RoleParamSetter)
	approver := f.as(f.tenant, uuid.Nil, authz.RoleParamApprover)
	both := f.as(f.tenant, uuid.Nil, authz.RoleParamSetter, authz.RoleParamApprover)
	pins := []any{
		map[string]any{"key": port.ParamCO2OpenBurning, "version": 3},
		map[string]any{"key": port.ParamCO2TransportRoad, "version": 1},
		map[string]any{"key": port.ParamCO2TransportWaterway, "version": 1},
	}
	// Tự đề xuất rồi tự duyệt → bị chặn (R-56).
	own := both.adm(http.MethodPost, "/v1/mrv/methods", map[string]any{"rule_version": port.RuleEstimate, "params": pins, "source_note": "Thử tự duyệt"})
	expect(t, own, 201, "")
	expect(t, both.adm(http.MethodPost, "/v1/mrv/methods/"+own.str("id")+"/approve", nil), 403, "self_approval_forbidden")

	// Phương pháp ghim sai phiên bản hệ số đốt → duyệt xong vẫn tạm.
	wrongPins := []any{map[string]any{"key": port.ParamCO2OpenBurning, "version": 2}, pins[1], pins[2]}
	w := setter.adm(http.MethodPost, "/v1/mrv/methods", map[string]any{"rule_version": port.RuleEstimate, "params": wrongPins, "source_note": "Bản ký lần 1"})
	expect(t, w, 201, "")
	expect(t, approver.adm(http.MethodPost, "/v1/mrv/methods/"+w.str("id")+"/approve", map[string]any{"note": "Duyệt"}), 200, "")
	if e := recompute(); e["status"] != "provisional" {
		t.Fatalf("lệch phiên bản tham số → tạm: %v", e)
	}
	m := setter.adm(http.MethodPost, "/v1/mrv/methods", map[string]any{"rule_version": port.RuleEstimate, "params": pins, "source_note": "Văn bản phương pháp đã ký (thử)"})
	expect(t, m, 201, "")
	if m.num("version") != 3 || m.str("status") != "submitted" {
		t.Fatalf("phiên bản phương pháp: %v", m.body)
	}
	expect(t, approver.adm(http.MethodPost, "/v1/mrv/methods/"+m.str("id")+"/approve", map[string]any{"note": "Duyệt"}), 200, "")
	expect(t, approver.adm(http.MethodPost, "/v1/mrv/methods/"+m.str("id")+"/reject", map[string]any{"note": "Đổi ý rồi"}), 409, "mrv.method_invalid_transition")
	e = recompute()
	if e["status"] != "official" || e["official"] != true || e["method_version_id"] != m.str("id") {
		t.Fatalf("phương pháp + tham số đã duyệt → chính thức: %v", e)
	}
	// Tham số đổi phiên bản (chưa được phương pháp ghim) → quay về tạm.
	f.params.set(port.ParamCO2OpenBurning, "1500", 4)
	if e := recompute(); e["status"] != "provisional" || e["avoided_burning_kgco2e"] != "15000.000" {
		t.Fatalf("tham số mới chưa ghim → tạm: %v", e)
	}
	// Chỉ người duyệt được tính lại.
	expect(t, col.app(http.MethodPost, "/v1/mrv/seasons/"+s.ref.String()+"/estimate/recompute", nil), 403, "forbidden")
}

// RB-2 + ADR-0009: tenant khác không thấy gì; tổ xã chỉ thấy vụ trong xã mình.
func TestG3TenantIsolationAndScope(t *testing.T) {
	f := newFixture(t)
	s1 := f.openSeason(f.tenant, "30001", time.Now().Add(-24*time.Hour))
	s2 := f.openSeason(f.tenant, "30002", time.Now().Add(-24*time.Hour))
	f.must(f.valve(envelope(f.mkg, port.EventMKGCollectionEvid, collection(s1, ids.New(), 1, "500-1000"))))
	f.must(f.valve(envelope(f.mkg, port.EventMKGCollectionEvid, collection(s2, ids.New(), 1, "500-1000"))))

	other := f.as(ids.New(), f.national, port.RoleViewer)
	expect(t, other.app(http.MethodGet, "/v1/mrv/seasons/"+s1.ref.String()+"/completeness", nil), 404, "mrv.season_not_found")
	if r := other.app(http.MethodGet, "/v1/mrv/estimates", nil); len(items(r)) != 0 {
		t.Fatalf("tenant khác thấy ước tính: %v", r.body)
	}
	if r := other.app(http.MethodGet, "/v1/mrv/completeness", nil); len(items(r)) != 0 {
		t.Fatalf("tenant khác thấy vụ: %v", r.body)
	}

	commune := f.as(f.tenant, f.t30001, port.RoleCollector)
	expect(t, commune.app(http.MethodGet, "/v1/mrv/seasons/"+s1.ref.String()+"/completeness", nil), 200, "")
	expect(t, commune.app(http.MethodGet, "/v1/mrv/seasons/"+s2.ref.String()+"/completeness", nil), 404, "mrv.season_not_found")
	expect(t, commune.app(http.MethodGet, "/v1/mrv/seasons/"+s2.ref.String()+"/comparison", nil), 404, "mrv.season_not_found")
	list := commune.app(http.MethodGet, "/v1/mrv/completeness", nil)
	if it := items(list); len(it) != 1 || it[0]["season"].(map[string]any)["season_ref"] != s1.ref.String() {
		t.Fatalf("danh sách theo phạm vi: %v", list.body)
	}
	if it := items(commune.app(http.MethodGet, "/v1/mrv/estimates", nil)); len(it) != 1 || it[0]["season_ref"] != s1.ref.String() {
		t.Fatalf("ước tính theo phạm vi: %v", it)
	}
	province := f.as(f.tenant, f.p89, port.RoleViewer)
	if it := items(province.app(http.MethodGet, "/v1/mrv/estimates", nil)); len(it) != 2 {
		t.Fatalf("tỉnh thấy cả hai xã: %v", it)
	}
	expect(t, f.as(f.tenant, f.p89, "farmer").app(http.MethodGet, "/v1/mrv/estimates", nil), 403, "forbidden")
}

// KN US-MRV-02: bảng đối chiếu song song + cán bộ đánh dấu Khớp/Khác biệt, lưu gắn
// với bản ghi MRV; sửa bằng bản ghi bù; POST lặp idempotent.
func TestG3ComparisonRecordKNUSMRV02(t *testing.T) {
	f := newFixture(t)
	f.approveSeed(f.tenant)
	s := f.openSeason(f.tenant, "30001", time.Now().Add(-10*24*time.Hour))
	logRef := ids.New()
	f.must(f.farmLog(envelope(f.tenant, port.EventFarmLogRecorded, logRecorded(s, logRef, ids.New(), 1, "tuoi_nuoc", time.Now().Add(-26*time.Hour)))))
	col := f.as(f.tenant, f.t30001, port.RoleCollector)
	body := awd("30001")
	body["plot_ref"] = s.plot.String()
	ev := col.app(http.MethodPost, "/v1/mrv/evidence", body)
	expect(t, ev, 201, "")

	v := col.app(http.MethodGet, "/v1/mrv/seasons/"+s.ref.String()+"/comparison", nil)
	expect(t, v, 200, "")
	line := v.body["lines"].([]any)[0].(map[string]any)
	if line["suggested"] != "match" || line["confirmed"] != false || v.body["window_days"] != float64(3) {
		t.Fatalf("gợi ý khớp AWD ↔ tưới nước: %v", v.body)
	}
	base := map[string]any{"season_ref": s.ref, "log_ref": logRef, "evidence_id": ev.str("id"), "result": "mismatch"}
	r := col.app(http.MethodPost, "/v1/mrv/comparisons", base)
	expect(t, r, 422, "validation_failed")
	if !r.fieldErr("note") {
		t.Fatalf("Khác biệt phải có ghi chú: %v", r.body)
	}
	base["result"] = "match"
	k := "k-" + ids.New().String()
	first := col.do(f.app.URL, http.MethodPost, "/v1/mrv/comparisons", k, base)
	expect(t, first, 201, "")
	again := col.do(f.app.URL, http.MethodPost, "/v1/mrv/comparisons", k, base)
	if again.status != 200 && again.status != 201 || again.str("id") != first.str("id") {
		t.Fatalf("POST lặp phải trả bản cũ: %d %v", again.status, again.body)
	}
	expect(t, col.app(http.MethodPost, "/v1/mrv/comparisons", base), 409, "mrv.comparison_exists")
	fix := map[string]any{"season_ref": s.ref, "log_ref": logRef, "evidence_id": ev.str("id"), "result": "mismatch",
		"note": "Nông dân ghi tưới nhưng ruộng khô", "supersedes_id": first.str("id"), "correction_reason": "Xem lại ảnh hiện trường"}
	second := col.app(http.MethodPost, "/v1/mrv/comparisons", fix)
	expect(t, second, 201, "")
	expect(t, col.app(http.MethodPost, "/v1/mrv/comparisons", fix), 409, "mrv.comparison_superseded")
	v = col.app(http.MethodGet, "/v1/mrv/seasons/"+s.ref.String()+"/comparison", nil)
	line = v.body["lines"].([]any)[0].(map[string]any)
	if line["result"] != "mismatch" || line["confirmed"] != true || len(line["comparisons"].([]any)) != 1 {
		t.Fatalf("kết quả hiện hành phải là bản bù: %v", line)
	}
	// Ngoài phạm vi / không cùng thửa.
	other := f.openSeason(f.tenant, "30001", time.Now())
	bad := map[string]any{"season_ref": other.ref, "log_ref": logRef, "evidence_id": ev.str("id"), "result": "match"}
	expect(t, col.app(http.MethodPost, "/v1/mrv/comparisons", bad), 422, "validation_failed")
	out := f.openSeason(f.tenant, "30002", time.Now())
	bad["season_ref"] = out.ref
	expect(t, col.app(http.MethodPost, "/v1/mrv/comparisons", bad), 404, "mrv.season_not_found")
	expect(t, f.as(f.tenant, f.p89, port.RoleViewer).app(http.MethodPost, "/v1/mrv/comparisons", base), 403, "forbidden")
}

// KN FR-08: vụ đang mở không có bản ghi mới ≥ N ngày → cờ bất thường; lọc anomaly=true.
func TestG3NoRecordAnomalyKNFR08(t *testing.T) {
	f := newFixture(t)
	stale := f.openSeason(f.tenant, "30001", time.Now().Add(-20*24*time.Hour))
	fresh := f.openSeason(f.tenant, "30001", time.Now().Add(-20*24*time.Hour))
	f.must(f.farmLog(envelope(f.tenant, port.EventFarmLogRecorded, logRecorded(fresh, ids.New(), ids.New(), 1, "bon_phan", time.Now().Add(-2*24*time.Hour)))))
	view := f.as(f.tenant, f.p89, port.RoleViewer)
	c := view.app(http.MethodGet, "/v1/mrv/seasons/"+stale.ref.String()+"/completeness", nil)
	a := c.body["anomaly"].(map[string]any)
	if a["no_record"] != true || a["threshold_days"] != float64(14) || a["days_since"].(float64) < 19 {
		t.Fatalf("vụ 20 ngày không bản ghi: %v", a)
	}
	c = view.app(http.MethodGet, "/v1/mrv/seasons/"+fresh.ref.String()+"/completeness", nil)
	if c.body["anomaly"].(map[string]any)["no_record"] != false {
		t.Fatalf("có nhật ký 2 ngày trước: %v", c.body["anomaly"])
	}
	f.params.set(port.ParamAnomalyNoRecordDays, "1", 1)
	c = view.app(http.MethodGet, "/v1/mrv/seasons/"+fresh.ref.String()+"/completeness", nil)
	if c.body["anomaly"].(map[string]any)["no_record"] != true {
		t.Fatalf("ngưỡng là tham số có phiên bản: %v", c.body["anomaly"])
	}
	f.params.set(port.ParamAnomalyNoRecordDays, "14", 2)
	list := view.app(http.MethodGet, "/v1/mrv/completeness?anomaly=true", nil)
	if it := items(list); len(it) != 1 || it[0]["season"].(map[string]any)["season_ref"] != stale.ref.String() {
		t.Fatalf("lọc bất thường: %v", list.body)
	}
	// Vụ đã đóng → không cảnh báo.
	f.must(f.farmLog(envelope(f.tenant, port.EventSeasonCompleted, map[string]any{
		"season_ref": stale.ref, "event_id": ids.New(), "plot_ref": stale.plot, "commune_code": "30001", "season_code": "2026-DX",
		"season_year": 2026, "actor_ref": ids.New(), "event_time": time.Now().Format(time.RFC3339),
	})))
	c = view.app(http.MethodGet, "/v1/mrv/seasons/"+stale.ref.String()+"/completeness", nil)
	if c.body["anomaly"].(map[string]any)["no_record"] != false || c.body["season"].(map[string]any)["status"] != "completed" {
		t.Fatalf("vụ đã đóng: %v", c.body)
	}
}

// Mục 16: gói xuất có liên kết nhật ký, bằng chứng thu gom, đối chiếu, ước tính kèm
// rule_version + phiên bản tham số; băm ảnh chụp dựng lại khớp.
func TestG3ExportIncludesLinksAndEstimates(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.approveSeed(f.tenant)
	s := f.openSeason(f.tenant, "30001", time.Now().Add(-72*time.Hour))
	f.must(f.farmLog(envelope(f.tenant, port.EventFarmLogRecorded, logRecorded(s, ids.New(), ids.New(), 1, "thu_hoach", time.Now().Add(-5*time.Hour)))))
	f.must(f.valve(envelope(f.mkg, port.EventMKGCollectionEvid, collection(s, ids.New(), 1, "1000-2000"))))

	exp := f.as(f.tenant, f.p89, port.RoleExporter)
	j := exp.app(http.MethodPost, "/v1/mrv/exports", map[string]any{"filters": map[string]any{}})
	expect(t, j, 202, "")
	id := j.str("id")
	f.must(inbox.Process(ctx, f.pool, port.ExportConsumer, f.requestedEvent(f.tenant, id), f.mod.ExportHandler()))
	got := exp.app(http.MethodGet, "/v1/mrv/exports/"+id, nil)
	if got.str("status") != "completed" {
		t.Fatalf("job: %v", got.body)
	}
	rc, _, err := f.store.Get(ctx, "packages/"+f.tenant.String()+"/"+id+".json")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(rc)
	_ = rc.Close()
	var pkg struct {
		EstimatesProvisional bool `json:"estimates_provisional"`
		Snapshot             struct {
			FarmLogs    []map[string]any `json:"farm_logs"`
			Collections []map[string]any `json:"collection_evidence"`
			Estimates   []map[string]any `json:"estimates"`
		} `json:"snapshot"`
	}
	if err := json.Unmarshal(body, &pkg); err != nil {
		t.Fatal(err)
	}
	if len(pkg.Snapshot.FarmLogs) != 1 || pkg.Snapshot.FarmLogs[0]["catalog_version"] != float64(1) {
		t.Fatalf("farm_logs: %v", pkg.Snapshot.FarmLogs)
	}
	if len(pkg.Snapshot.Collections) != 1 || pkg.Snapshot.Collections[0]["collected_kg_bucket"] != "1000-2000" {
		t.Fatalf("collection_evidence: %v", pkg.Snapshot.Collections)
	}
	if len(pkg.Snapshot.Estimates) != 1 || pkg.Snapshot.Estimates[0]["rule_version"] != port.RuleEstimate || !pkg.EstimatesProvisional {
		t.Fatalf("estimates: %v", pkg.Snapshot.Estimates)
	}
	if p := pkg.Snapshot.Estimates[0]["params"].([]any); len(p) != 3 || p[0].(map[string]any)["key"] != port.ParamCO2OpenBurning {
		t.Fatalf("phiên bản tham số trong gói: %v", p)
	}
	// Dữ liệu G3 ghi sau as_of không làm đổi băm dựng lại.
	f.must(f.valve(envelope(f.mkg, port.EventMKGCollectionEvid, collection(s, ids.New(), 1, "10000+"))))
	vr := exp.app(http.MethodGet, "/v1/mrv/exports/"+id+"/verify", nil)
	if vr.body["match"] != true {
		t.Fatalf("băm dựng lại: %v", vr.body)
	}
}
