package kn77_test

// BF-CN-PUBLISH (#108 — CN-07, FN-07, UR-TINH-08, UR-XA-13) trên module THẬT qua HTTP: pf/forms (kỳ native kn_cnts) +
// pf/extension (/v1/extension/cnts/period-series*, period-tracking, period-drafts; sổ cnts_publication, bảng nháp
// cnts_period_draft — pf 20261027200000). Đối tượng nhóm xã (tất cả / khu vực / có nuôi thủy sản / có mô hình) giải ở máy
// chủ, người phụ trách phải là Cấp Tỉnh CNTS cùng Nút, giờ chốt, xem trước số nhiệm vụ; nháp của xã hiện ở màn theo dõi
// và hết hiệu lực khi gửi.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension"
	accport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

// pubRegions giả pf/party: tỉnh 89 có xã X1 (khu vực A), X2 (khu vực B).
type pubRegions struct{}

func (pubRegions) ProvinceRegions(context.Context, string, bool) ([]partyport.Region, error) {
	return nil, nil
}
func (pubRegions) CommuneRegions(_ context.Context, province string) (map[string]partyport.CommuneRegion, error) {
	if province != "89" {
		return map[string]partyport.CommuneRegion{}, nil
	}
	return map[string]partyport.CommuneRegion{
		"X1": {CommuneCode: "X1", RegionCode: "KV-A", RegionName: "Khu vực A", SortOrder: 1},
		"X2": {CommuneCode: "X2", RegionCode: "KV-B", RegionName: "Khu vực B", SortOrder: 2},
	}, nil
}

// pubAssignments giả pf/accounts: một cán bộ Cấp Tỉnh CNTS của Nút node.
type pubAssignments struct{ owner, node uuid.UUID }

func (a pubAssignments) ActiveAssignments(_ context.Context, role, dept string) ([]accport.Assignment, error) {
	if role != authz.KNTinh || dept != aggDept {
		return nil, nil
	}
	return []accport.Assignment{{ID: ids.New(), UserRef: a.owner, Role: authz.KNTinh, OrgRef: a.node, Departments: []string{aggDept},
		ValidFrom: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), Active: true}}, nil
}

// seedSite ghi một điểm trình diễn CNTS (sổ cnts_demo_site) ở xã commune, nhật ký diary; closed → thêm sự kiện đóng điểm.
func seedSite(t *testing.T, tid uuid.UUID, commune, diary string, closed bool) {
	t.Helper()
	ctx := context.Background()
	tx, err := pgtest.Get(t).AdminPool(t, migrations.PF).Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `SELECT set_config('app.tenant_id', $1, true)`, tid.String()); err != nil {
		t.Fatal(err)
	}
	site, project := ids.New(), ids.New()
	events := []string{"created"}
	if closed {
		events = append(events, "closed")
	}
	for i, ev := range events {
		var reason any
		if ev == "closed" {
			reason = "Kết thúc mô hình"
		}
		if _, err := tx.Exec(ctx, `INSERT INTO pf_extension.cnts_demo_site (id, tenant_id, stream_key, site_ref, event, project_ref, commune_code, name,
		    model_type_code, diary_form, status_reason, event_time, actor_ref, idem_key, rule_version)
		  VALUES ($1, $2, 'csite:' || $3::uuid::text, $3::uuid, $4, $5, $6, 'Điểm thử', 'GA', $7, $8, now() + make_interval(secs => $9), $10, $11, 'test')`,
			ids.New(), tid, site, ev, project, commune, diary, reason, i, ids.New(), "seed-"+ids.New().String()); err != nil {
			t.Fatal(err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
}

type pubSeries struct {
	Units         []string `json:"units"`
	Tasks         int      `json:"tasks"`
	Target        string   `json:"target"`
	OwnerRef      *string  `json:"owner_ref"`
	Close         *int     `json:"close_after_hours"`
	PublicationID *string  `json:"publication_id"`
	Communes      []struct {
		CommuneCode string `json:"commune_code"`
		RegionCode  string `json:"region_code"`
	} `json:"communes"`
	Periods []struct {
		ID *string `json:"id"`
	} `json:"periods"`
}

type pubTracking struct {
	Periods []struct {
		PeriodID string  `json:"period_id"`
		OwnerRef *string `json:"owner_ref"`
		Close    *int    `json:"close_after_hours"`
		Target   string  `json:"target"`
	} `json:"periods"`
	Drafts []struct {
		PeriodID string `json:"period_id"`
		Unit     string `json:"unit"`
	} `json:"drafts"`
}

func TestCNTSPublish_TargetOwnerDraft_BF_CN_PUBLISH(t *testing.T) {
	owner := ids.New()
	f := newAggFx(t, facilityDir{}, func(fm *forms.Module, kn *extension.Module) {
		kn.UseCNTSCampaigns(extension.CNTSCampaignDeps{Forms: fm.Publisher(), Control: fm.Control()})
	})
	f.kn.UseReportCycle(extension.ReportCycleDeps{Regions: pubRegions{}, Assignments: pubAssignments{owner: owner, node: f.node}})
	c, b := f.call(http.MethodPost, "/v1/extension/cnts/period-forms/install", authz.KNAdmin, nil)
	f.ok(c, b, nil)
	in := func(extra map[string]any) map[string]any {
		m := map[string]any{"code": "CNTS-BM04", "from_date": "2026-10-13", "to_date": "2026-10-19", "units": []string{}}
		for k, v := range extra {
			m[k] = v
		}
		return m
	}
	preview := func(extra map[string]any) pubSeries {
		t.Helper()
		var s pubSeries
		c, b := f.call(http.MethodPost, "/v1/extension/cnts/period-series/preview", authz.KNTinh, in(extra))
		f.ok(c, b, &s)
		return s
	}

	// Đối tượng: tất cả xã của tỉnh, theo khu vực; xem trước số nhiệm vụ; danh sách xã kèm khu vực để chọn.
	all := preview(map[string]any{"target": "province"})
	if len(all.Units) != 2 || all.Tasks != 2 || len(all.Communes) != 2 || all.Communes[0].RegionCode != "KV-A" || all.Target != "province" {
		t.Fatalf("tất cả xã: %+v", all)
	}
	if kv := preview(map[string]any{"target": "regions", "regions": []string{"KV-B"}}); len(kv.Units) != 1 || kv.Units[0] != "X2" || kv.Tasks != 1 {
		t.Fatalf("theo khu vực: %+v", kv)
	}
	// Không đổi hành vi cũ: không gửi target = chọn từng xã.
	if old := preview(map[string]any{"units": []string{"X1"}}); old.Target != "communes" || old.Tasks != 1 {
		t.Fatalf("mặc định chọn từng xã: %+v", old)
	}
	// Hồi quy lỗi: nhóm không có xã, khu vực rỗng, xã ngoài tỉnh, cơ sở với biểu xã, giờ chốt sai.
	for what, x := range map[string]map[string]any{
		"chưa có điểm mô hình":     {"target": "model"},
		"khu vực rỗng":             {"target": "regions"},
		"xã ngoài tỉnh":            {"target": "communes", "units": []string{"Y9"}},
		"cơ sở với biểu xã":        {"target": "facilities", "units": []string{ids.New().String()}},
		"giờ chốt âm":              {"target": "province", "close_after_hours": -1},
		"giờ chốt quá 30 ngày":     {"target": "province", "close_after_hours": 721},
		"người phụ trách lạ":       {"target": "province", "owner_ref": ids.New().String()},
		"đối tượng không hỗ trợ":   {"target": "households"},
		"khu vực với nhóm khác":    {"target": "province", "regions": []string{"KV-A"}},
		"chọn từng xã không có xã": {"target": "communes"},
	} {
		c, b := f.call(http.MethodPost, "/v1/extension/cnts/period-series/preview", authz.KNTinh, in(x))
		if c != http.StatusUnprocessableEntity {
			t.Fatalf("%s: muốn 422, nhận %d %s", what, c, b)
		}
	}
	// Xã có mô hình = điểm trình diễn đang hoạt động; có nuôi thủy sản = điểm nhật ký BM-19; điểm đã đóng không tính.
	seedSite(t, f.tid, "X1", "BM-08", false)
	seedSite(t, f.tid, "X2", "BM-19", false)
	seedSite(t, f.tid, "X1", "BM-19", true)
	if m := preview(map[string]any{"target": "model"}); len(m.Units) != 2 {
		t.Fatalf("xã có mô hình: %+v", m)
	}
	if a := preview(map[string]any{"target": "aquaculture"}); len(a.Units) != 1 || a.Units[0] != "X2" {
		t.Fatalf("xã có nuôi thủy sản: %+v", a)
	}

	// Phát hành: người phụ trách + giờ chốt ghi vào sổ cnts_publication; màn theo dõi đọc lại.
	var pub pubSeries
	c, b = f.call(http.MethodPost, "/v1/extension/cnts/period-series", authz.KNTinh, in(map[string]any{"target": "province", "owner_ref": owner.String(),
		"close_after_hours": 48}))
	f.ok(c, b, &pub)
	if pub.PublicationID == nil || pub.OwnerRef == nil || *pub.OwnerRef != owner.String() || pub.Close == nil || *pub.Close != 48 || len(pub.Periods) != 1 {
		t.Fatalf("phát hành: %s", b)
	}
	pid := *pub.Periods[0].ID
	rows := f.admin.query(`SELECT owner_ref, target_mode, close_after_hours, cardinality(period_refs), task_count FROM pf_extension.cnts_publication WHERE tenant_id = $1`, f.tid)
	if len(rows) != 1 || rows[0][1] != "province" || rows[0][4].(int32) != 2 {
		t.Fatalf("sổ cnts_publication: %v", rows)
	}
	var tr pubTracking
	c, b = f.call(http.MethodGet, "/v1/extension/cnts/period-tracking?code=CNTS-BM04", authz.KNTinh, nil)
	f.ok(c, b, &tr)
	if len(tr.Periods) != 1 || tr.Periods[0].PeriodID != pid || tr.Periods[0].OwnerRef == nil || *tr.Periods[0].OwnerRef != owner.String() ||
		*tr.Periods[0].Close != 48 || len(tr.Drafts) != 0 {
		t.Fatalf("theo dõi: %s", b)
	}

	// Nháp: Cấp Xã lưu → Cấp Tỉnh thấy dấu nháp; Cấp Tỉnh không lưu nháp; đơn vị ngoài kỳ bị chặn.
	draft := map[string]any{"unit": "X1", "values": map[string]string{"tong_dan_heo": "12"}, "nil_report": false, "note": "đang đếm"}
	c, b = f.call(http.MethodPut, "/v1/extension/cnts/period-drafts/"+pid, authz.KNXa, draft)
	f.ok(c, b, nil)
	c, b = f.call(http.MethodPut, "/v1/extension/cnts/period-drafts/"+pid, authz.KNTinh, draft)
	if c != http.StatusForbidden {
		t.Fatalf("Cấp Tỉnh lưu nháp: %d %s", c, b)
	}
	c, b = f.call(http.MethodPut, "/v1/extension/cnts/period-drafts/"+pid, authz.KNXa, map[string]any{"unit": "X9", "values": map[string]string{}})
	if c < 400 {
		t.Fatalf("đơn vị ngoài kỳ: %d %s", c, b)
	}
	var got struct {
		Draft *struct {
			Values map[string]string `json:"values"`
			Note   string            `json:"note"`
		} `json:"draft"`
	}
	c, b = f.call(http.MethodGet, "/v1/extension/cnts/period-drafts/"+pid+"?unit=X1", authz.KNXa, nil)
	f.ok(c, b, &got)
	if got.Draft == nil || got.Draft.Values["tong_dan_heo"] != "12" || got.Draft.Note != "đang đếm" {
		t.Fatalf("đọc nháp: %s", b)
	}
	tr = pubTracking{}
	c, b = f.call(http.MethodGet, "/v1/extension/cnts/period-tracking?code=CNTS-BM04", authz.KNTinh, nil)
	f.ok(c, b, &tr)
	if len(tr.Drafts) != 1 || tr.Drafts[0].Unit != "X1" || tr.Drafts[0].PeriodID != pid {
		t.Fatalf("dấu nháp trên màn theo dõi: %s", b)
	}

	// Gửi bài → nháp hết hiệu lực (không xóa, chỉ bỏ qua khi đọc).
	f.clk.Advance(time.Minute)
	f.submit(pid, "X1", bm04Values())
	got.Draft = nil
	c, b = f.call(http.MethodGet, "/v1/extension/cnts/period-drafts/"+pid+"?unit=X1", authz.KNXa, nil)
	f.ok(c, b, &got)
	if got.Draft != nil {
		t.Fatalf("nháp cũ hơn lần gửi phải bỏ qua: %s", b)
	}
	// Kỳ đã chốt: không lưu nháp.
	f.closePeriod(pid)
	c, b = f.call(http.MethodPut, "/v1/extension/cnts/period-drafts/"+pid, authz.KNXa, map[string]any{"unit": "X2", "values": map[string]string{}})
	if c != http.StatusConflict {
		t.Fatalf("kỳ đã chốt: %d %s", c, b)
	}
}

// bm04Values: bài BM-04 hợp lệ (tổng đàn 10, mọi chỉ tiêu khác 0).
func bm04Values() map[string]string {
	v := map[string]string{}
	for _, k := range []string{"tong_dan_heo", "tong_dan_de", "tong_dan_trau", "tong_dan_bo", "tong_dan_vit", "tong_dan_ga", "tong_dan_cho_meo",
		"tong_dan_yen", "tiem_ta_heo_cp", "tiem_dich_ta_tht_heo", "tiem_lmlm_heo", "tiem_tai_xanh_heo", "tiem_vdnc_bo", "tiem_tht_bo", "tiem_lmlm_bo",
		"tiem_cum_gia_cam", "tiem_dai", "hoa_chat_gia_suc", "hoa_chat_gia_cam", "ksgm_gia_suc", "ksgm_gia_cam", "vit_chay_dong_dia_phuong",
		"vit_chay_dong_noi_khac"} {
		v[k] = "0"
	}
	v["tong_dan_heo"] = "10"
	return v
}

// callAs gọi API với ngữ cảnh KN tường minh (phạm vi, Nút) — hồi quy phạm vi của review #108.
func (f *aggFx) callAs(method, path string, k authz.KNContext) (int, []byte) {
	f.t.Helper()
	kj, _ := json.Marshal(k)
	req := httptest.NewRequest(method, path, nil)
	req.Header.Set("X-Test-KN", string(kj))
	req.Header.Set("X-Test-Actor", ids.New().String())
	w := httptest.NewRecorder()
	f.eng.ServeHTTP(w, req)
	return w.Code, w.Body.Bytes()
}

func (f *aggFx) trackedPeriods(k authz.KNContext, code string) []string {
	f.t.Helper()
	c, b := f.callAs(http.MethodGet, "/v1/extension/cnts/period-tracking?code="+code, k)
	var tr pubTracking
	f.ok(c, b, &tr)
	out := []string{}
	for _, p := range tr.Periods {
		out = append(out, p.PeriodID)
	}
	slices.Sort(out)
	return out
}

// Review Codex #108 [P2]: màn theo dõi không được lộ metadata (period_id, publication_id, biểu, đối tượng, người phụ
// trách) của lần phát hành ngoài phạm vi người gọi. Bao phép thử tái hiện của Codex (TestCodex108TrackingScope): xã X1
// không thấy kỳ chỉ phát hành cho X2.
func TestCNTSTrackingScope_BF_CN_PUBLISH(t *testing.T) {
	facA, facB := ids.New(), ids.New()
	f := newAggFx(t, facilityDir{facA: "X1", facB: "X2"}, func(fm *forms.Module, kn *extension.Module) {
		kn.UseCNTSCampaigns(extension.CNTSCampaignDeps{Forms: fm.Publisher(), Control: fm.Control()})
	})
	f.kn.UseReportCycle(extension.ReportCycleDeps{Regions: pubRegions{}})
	c, b := f.call(http.MethodPost, "/v1/extension/cnts/period-forms/install", authz.KNAdmin, nil)
	f.ok(c, b, nil)
	onlyX2 := f.publish("CNTS-BM04", "2026-10-13", "2026-10-19", "X2").Periods[0].ID
	onlyX1 := f.publish("CNTS-BM04", "2026-10-20", "2026-10-26", "X1").Periods[0].ID
	facX2 := f.publish("CNTS-BM01", "2026-10-01", "2026-10-01", facB.String()).Periods[0].ID
	facX1 := f.publish("CNTS-BM01", "2026-10-02", "2026-10-02", facA.String()).Periods[0].ID
	both := func(x, y string) []string { s := []string{x, y}; slices.Sort(s); return s }

	xa := func(units ...string) authz.KNContext {
		return authz.KNContext{Role: authz.KNXa, Department: aggDept, OrgRef: ids.New(), Scope: authz.Scope{Units: units}}
	}
	// Cấp Xã X1: chỉ kỳ có X1 (biểu theo xã và biểu theo cơ sở của xã X1), không thấy kỳ chỉ X2.
	if got := f.trackedPeriods(xa("X1"), "CNTS-BM04"); !slices.Equal(got, []string{onlyX1}) {
		t.Fatalf("xã X1 (BM-04): %v, muốn chỉ %s (không thấy %s)", got, onlyX1, onlyX2)
	}
	if got := f.trackedPeriods(xa("X1"), "CNTS-BM01"); !slices.Equal(got, []string{facX1}) {
		t.Fatalf("xã X1 (BM-01): %v, muốn chỉ %s (không thấy %s)", got, facX1, facX2)
	}
	if got := f.trackedPeriods(xa("X2"), ""); !slices.Equal(got, both(onlyX2, facX2)) {
		t.Fatalf("xã X2 (mọi biểu): %v", got)
	}
	if got := f.trackedPeriods(xa("X9"), ""); len(got) != 0 {
		t.Fatalf("xã ngoài mọi đơn vị nhận: %v", got)
	}
	// Nháp: xã X1 không đọc nháp của đơn vị X2 trong kỳ chỉ X2.
	if c, b := f.callAs(http.MethodGet, "/v1/extension/cnts/period-drafts/"+onlyX2+"?unit=X2", xa("X1")); c < 400 {
		t.Fatalf("xã X1 đọc nháp kỳ X2: %d %s", c, b)
	}
	// Cấp Tỉnh cùng Nút: thấy mọi kỳ của Nút; Cấp Tỉnh Nút khác: không thấy.
	tinh := authz.KNContext{Role: authz.KNTinh, Department: aggDept, OrgRef: f.node, Scope: authz.Scope{Units: []string{"X1", "X2"}}}
	if got := f.trackedPeriods(tinh, "CNTS-BM04"); !slices.Equal(got, both(onlyX1, onlyX2)) {
		t.Fatalf("Cấp Tỉnh cùng Nút: %v", got)
	}
	other := tinh
	other.OrgRef = ids.New()
	if got := f.trackedPeriods(other, ""); len(got) != 0 {
		t.Fatalf("Cấp Tỉnh Nút khác: %v", got)
	}
	// Admin thấy mọi kỳ của tenant; tenant khác (kể cả Admin) không thấy gì.
	admin := authz.KNContext{Role: authz.KNAdmin, Scope: authz.Scope{All: true}}
	if got := f.trackedPeriods(admin, ""); len(got) != 4 {
		t.Fatalf("Admin: %v", got)
	}
	f.tid = ids.New()
	for _, k := range []authz.KNContext{admin, tinh, xa("X1", "X2")} {
		if got := f.trackedPeriods(k, ""); len(got) != 0 {
			t.Fatalf("tenant khác (%s): %v", k.Role, got)
		}
	}
}
