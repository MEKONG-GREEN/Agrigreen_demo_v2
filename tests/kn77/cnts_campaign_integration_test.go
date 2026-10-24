package kn77_test

// W11.50 — Chiến dịch điều tra mở rộng CNTS (BRD CNTS v3.7 FN-21, UR-TINH-26, UR-XA-14; D4 CN10) trên module THẬT qua
// HTTP và PostgreSQL: pf/forms (kỳ native kn_cnts loại `adhoc`) + pf/extension (/v1/extension/cnts/campaigns*, sổ pf
// 20261024091058). Kiểm vòng đời Nháp → Đang thu thập → Đã đóng → mở lại (Lãnh đạo phòng), và số liệu đợt KHÔNG vào lũy
// kế Phụ lục 2 (BM-18) dù đợt dùng cùng biểu BM-17, KHÔNG vào bộ đọc/quy đổi số đã chốt của KHTC (closedfig).

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension"
	extport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/idempotency"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

type campaignView struct {
	ID            string  `json:"id"`
	Version       int64   `json:"version"`
	Name          string  `json:"name"`
	State         string  `json:"state"`
	Tasks         int     `json:"tasks"`
	DueAt         string  `json:"due_at"`
	FormsPeriodID *string `json:"forms_period_id"`
	PeriodKey     *string `json:"period_key"`
	Progress      *struct {
		PeriodStatus string `json:"period_status"`
		Total        int    `json:"total"`
		Submitted    int    `json:"submitted"`
		Pending      int    `json:"pending"`
		Missed       int    `json:"missed"`
		Regions      []struct {
			Name  string `json:"name"`
			Total int    `json:"total"`
		} `json:"regions"`
		Units []struct {
			Commune string `json:"commune"`
			Status  string `json:"status"`
		} `json:"units"`
	} `json:"progress"`
	Events []struct {
		Action string `json:"action"`
	} `json:"events"`
}

// campaignCall gọi API với phiên Cấp Tỉnh CNTS (manager = Lãnh đạo phòng) hoặc vai khác.
func (f *aggFx) campaignCall(method, path, role string, manager bool, body any) (int, []byte) {
	f.t.Helper()
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest(method, path, bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	k := authz.KNContext{Role: role, Department: aggDept, OrgRef: f.node, Scope: authz.Scope{Units: []string{"X1", "X2"}}, Manager: manager}
	if role == authz.KNXa {
		k.OrgRef = ids.New()
	}
	kj, _ := json.Marshal(k)
	req.Header.Set("X-Test-KN", string(kj))
	req.Header.Set("X-Test-Actor", ids.New().String())
	if method == http.MethodPost {
		req.Header.Set(idempotency.Header, "t-"+ids.New().String())
	}
	w := httptest.NewRecorder()
	f.eng.ServeHTTP(w, req)
	return w.Code, w.Body.Bytes()
}

func TestCNTSCampaign_LifecycleSeparateFromCumulative_W1150(t *testing.T) {
	f := newAggFx(t, facilityDir{}, func(fm *forms.Module, kn *extension.Module) {
		kn.UseCNTSCampaigns(extension.CNTSCampaignDeps{Forms: fm.Publisher(), Control: fm.Control(), Figures: fm.Figures()})
	})
	// k_ref.org: Nút TTKN tỉnh 89 của phiên Cấp Tỉnh (view pf_forms_pub lấy tỉnh của kỳ native từ Nút phát hành).
	ctx := context.Background()
	if err := tenant.WithTenantTx(ctx, pgtest.Get(t).AdminPool(t, migrations.PF), f.tid, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO k_ref.org (tenant_id, ref, kind, code, admin_unit_code, active, source_seq)
		    VALUES ($1, $2, 'ttkn_province', 'TTKN89', '89', true, 1)`, f.tid, f.node)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	c, b := f.call(http.MethodPost, "/v1/extension/cnts/period-forms/install", authz.KNAdmin, nil)
	f.ok(c, b, nil)
	// Báo cáo định kỳ: BM-17 tuần 39, 40 (lũy kế năm tuần 40 = 11.5 như W11.44).
	s := f.publish("CNTS-BM17", "2026-09-22", "2026-10-05", "X1", "X2")
	w39, w40 := s.Periods[0].ID, s.Periods[1].ID
	f.submit(w39, "X1", catchVals("2", "1", "0", "0"))
	f.submit(w39, "X2", catchVals("3", "0", "0", "0.5"))
	f.submit(w40, "X1", catchVals("4", "0", "1", "0"))
	bm18Total := func() cols {
		t.Helper()
		var v bm18
		c, b := f.call(http.MethodGet, "/v1/extension/cnts/bm18?week=2026-W40", authz.KNTinh, nil)
		f.ok(c, b, &v)
		return v.Catch["tong"]
	}
	before := bm18Total()
	if before.Cur != "5" || before.YTD != "11.5" {
		t.Fatalf("lũy kế trước đợt: %+v", before)
	}

	input := map[string]any{"name": "Điều tra sản lượng khai thác 01/10/2026", "goal": "Khảo sát nhanh sau lũ", "field": "thuy_san",
		"template_code": "CNTS-BM17", "scope_mode": "communes", "communes": []string{"X1", "X2"}, "from_date": "2026-10-01",
		"to_date": "2026-10-05", "due_at": "2026-10-09T03:00:00Z"}
	var cv campaignView

	t.Run("Nháp: chỉ Cấp Tỉnh CNTS tạo; biểu phải có; sửa nháp thành phiên bản mới", func(t *testing.T) {
		c, b := f.campaignCall(http.MethodPost, "/v1/extension/cnts/campaigns", authz.KNXa, false, input)
		if c != http.StatusForbidden {
			t.Fatalf("Cấp Xã tạo đợt: %d %s", c, b)
		}
		bad := map[string]any{}
		for k, v := range input {
			bad[k] = v
		}
		bad["template_code"] = "CNTS-KHONG-CO"
		c, b = f.campaignCall(http.MethodPost, "/v1/extension/cnts/campaigns", authz.KNTinh, false, bad)
		f.fail(c, b, "extension.cnts_campaign_template")
		bad["template_code"], bad["communes"] = "CNTS-BM17", []string{"X9"}
		c, b = f.campaignCall(http.MethodPost, "/v1/extension/cnts/campaigns", authz.KNTinh, false, bad)
		f.fail(c, b, "validation_failed")
		c, b = f.campaignCall(http.MethodPost, "/v1/extension/cnts/campaigns", authz.KNTinh, false, input)
		f.ok(c, b, &cv)
		if c != http.StatusCreated || cv.State != "draft" || cv.Tasks != 2 || cv.FormsPeriodID != nil || cv.Version != 1 {
			t.Fatalf("nháp: %d %s", c, b)
		}
		input["name"] = "Điều tra sản lượng khai thác sau lũ 10/2026"
		c, b = f.campaignCall(http.MethodPost, "/v1/extension/cnts/campaigns/"+cv.ID+"/draft", authz.KNTinh, false, input)
		f.ok(c, b, &cv)
		if cv.Version != 2 || cv.Name != input["name"] {
			t.Fatalf("sửa nháp: %s", b)
		}
		c, b = f.campaignCall(http.MethodPost, "/v1/extension/cnts/campaigns/"+cv.ID+"/close", authz.KNTinh, false, nil)
		f.fail(c, b, "extension.cnts_campaign_state")
	})

	t.Run("Phát hành: kỳ native adhoc, xã thấy việc; không sửa nội dung sau phát hành", func(t *testing.T) {
		c, b := f.campaignCall(http.MethodPost, "/v1/extension/cnts/campaigns/"+cv.ID+"/publish", authz.KNTinh, false, nil)
		f.ok(c, b, &cv)
		if cv.State != "collecting" || cv.FormsPeriodID == nil || cv.PeriodKey == nil || len(*cv.PeriodKey) != 18 ||
			cv.Progress == nil || cv.Progress.Total != 2 || cv.Progress.Pending != 2 {
			t.Fatalf("phát hành: %s", b)
		}
		c, b = f.campaignCall(http.MethodPost, "/v1/extension/cnts/campaigns/"+cv.ID+"/draft", authz.KNTinh, false, input)
		f.fail(c, b, "extension.cnts_campaign_state")
		c, b = f.campaignCall(http.MethodPost, "/v1/extension/cnts/campaigns/"+cv.ID+"/publish", authz.KNTinh, false, nil)
		f.fail(c, b, "extension.cnts_campaign_state")
		var tasks struct {
			Items []struct {
				PeriodID string `json:"period_id"`
			} `json:"items"`
		}
		c, b = f.call(http.MethodGet, "/v1/forms/tasks?profile=kn_cnts&code=CNTS-BM17", authz.KNXa, nil)
		f.ok(c, b, &tasks)
		found := 0
		for _, it := range tasks.Items {
			if it.PeriodID == *cv.FormsPeriodID {
				found++
			}
		}
		if found != 2 {
			t.Fatalf("việc của đợt ở Cấp Xã: %d — %s", found, b)
		}
		rows := f.admin.query(`SELECT period_kind, period_key FROM pf_forms.period_spec WHERE tenant_id = $1 AND period_id = $2`, f.tid, *cv.FormsPeriodID)
		if len(rows) != 1 || rows[0][0] != "adhoc" || rows[0][1] != *cv.PeriodKey {
			t.Fatalf("kỳ native: %v", rows)
		}
	})

	t.Run("Số liệu đợt (cùng biểu BM-17) không đổi lũy kế Phụ lục 2 (AC-04, BR-02)", func(t *testing.T) {
		f.submit(*cv.FormsPeriodID, "X1", catchVals("100", "200", "0", "0"))
		after := bm18Total()
		if after != before {
			t.Fatalf("lũy kế đổi theo đợt điều tra: trước %+v sau %+v", before, after)
		}
		c, b := f.campaignCall(http.MethodGet, "/v1/extension/cnts/campaigns/"+cv.ID, authz.KNTinh, false, nil)
		f.ok(c, b, &cv)
		if cv.Progress.Submitted != 1 || cv.Progress.Pending != 1 || len(cv.Progress.Units) != 2 {
			t.Fatalf("tiến độ: %s", b)
		}
		var r struct {
			Basis    string            `json:"basis"`
			Total    map[string]string `json:"total"`
			Reported int               `json:"reported"`
			Units    int               `json:"units"`
		}
		c, b = f.campaignCall(http.MethodGet, "/v1/extension/cnts/campaigns/"+cv.ID+"/results", authz.KNTinh, false, nil)
		f.ok(c, b, &r)
		if r.Basis != "submitted" || r.Total["ca"] != "100" || r.Total["tom"] != "200" || r.Reported != 1 || r.Units != 2 {
			t.Fatalf("kết quả: %s", b)
		}
	})

	t.Run("Nhắc xã chưa nộp, gia hạn có lý do", func(t *testing.T) {
		var rm struct {
			Reminded int `json:"reminded"`
		}
		c, b := f.campaignCall(http.MethodPost, "/v1/extension/cnts/campaigns/"+cv.ID+"/remind", authz.KNTinh, false, nil)
		f.ok(c, b, &rm)
		if rm.Reminded != 1 {
			t.Fatalf("nhắc: %s", b)
		}
		rows := f.admin.query(`SELECT payload->>'commune_code' FROM shared.outbox WHERE tenant_id = $1 AND event_type = $2`, f.tid, extport.EventCNTSFormReminded)
		if len(rows) != 1 || rows[0][0] != "X2" {
			t.Fatalf("sự kiện nhắc: %v", rows)
		}
		c, b = f.campaignCall(http.MethodPost, "/v1/extension/cnts/campaigns/"+cv.ID+"/extend", authz.KNTinh, false,
			map[string]any{"due_at": "2026-10-12T03:00:00Z", "reason": "ngắn"})
		f.fail(c, b, "validation_failed")
		c, b = f.campaignCall(http.MethodPost, "/v1/extension/cnts/campaigns/"+cv.ID+"/extend", authz.KNTinh, false,
			map[string]any{"due_at": "2026-10-08T03:00:00Z", "reason": "Xã X2 mất điện nhiều ngày"})
		f.fail(c, b, "validation_failed")
		c, b = f.campaignCall(http.MethodPost, "/v1/extension/cnts/campaigns/"+cv.ID+"/extend", authz.KNTinh, false,
			map[string]any{"due_at": "2026-10-12T03:00:00Z", "reason": "Xã X2 mất điện nhiều ngày"})
		f.ok(c, b, &cv)
		if cv.DueAt != "2026-10-12T03:00:00Z" {
			t.Fatalf("gia hạn: %s", b)
		}
	})

	t.Run("Đóng đợt: xã chưa gửi → Không nộp, không gửi được; kết quả theo ảnh chụp", func(t *testing.T) {
		c, b := f.campaignCall(http.MethodPost, "/v1/extension/cnts/campaigns/"+cv.ID+"/close", authz.KNTinh, false, nil)
		f.ok(c, b, &cv)
		if cv.State != "closed" || cv.Progress.PeriodStatus != "closed" || cv.Progress.Missed != 1 {
			t.Fatalf("đóng đợt: %s", b)
		}
		c, b = f.call(http.MethodPost, "/v1/forms/periods/"+*cv.FormsPeriodID+"/submissions?profile=kn_cnts", authz.KNXa,
			map[string]any{"unit": "X2", "values": catchVals("1", "1", "1", "1")})
		f.fail(c, b, "forms.period_closed")
		var r struct {
			Basis string            `json:"basis"`
			Total map[string]string `json:"total"`
		}
		c, b = f.campaignCall(http.MethodGet, "/v1/extension/cnts/campaigns/"+cv.ID+"/results", authz.KNTinh, false, nil)
		f.ok(c, b, &r)
		if r.Basis != "closed" || r.Total["ca"] != "100" {
			t.Fatalf("kết quả sau đóng: %s", b)
		}
		c, _ = f.campaignCall(http.MethodGet, "/v1/extension/cnts/campaigns/"+cv.ID+"/results/xlsx", authz.KNTinh, false, nil)
		if c != http.StatusOK {
			t.Fatalf("xuất Excel: %d", c)
		}
	})

	t.Run("KHTC: kỳ adhoc có ở view pf_forms_pub nhưng bộ đọc/quy đổi số đã chốt bỏ qua", func(t *testing.T) {
		f.closePeriod(w40)
		rows := f.admin.query(`SELECT count(*) FROM pf_forms_pub.v1_closed_figures WHERE tenant_id = $1 AND period_type = 'adhoc'`, f.tid)
		if rows[0][0] != int64(1) {
			t.Fatalf("view pf_forms_pub phải còn dòng adhoc (lọc ở bộ đọc KN): %v", rows)
		}
		figs, err := f.kn.ClosedFigures().ClosedFigures(context.Background(), f.tid, extport.ClosedFiguresQuery{Department: aggDept})
		if err != nil {
			t.Fatal(err)
		}
		weekRows := 0
		for _, fg := range figs {
			if fg.PeriodType == "adhoc" {
				t.Fatalf("bộ đọc KHTC trả kỳ đợt điều tra: %+v", fg)
			}
			if fg.PeriodKey == "2026-W40" && fg.FormCode == "CNTS-BM17" {
				weekRows++
			}
		}
		if weekRows != 1 {
			t.Fatalf("kỳ định kỳ W40 phải còn: %d", weekRows)
		}
		conv, err := f.kn.ClosedFigures().Converted(context.Background(), f.tid, extport.ClosedFiguresQuery{Department: aggDept},
			extport.CrosswalkTarget{PeriodType: "week"})
		if err != nil {
			t.Fatal(err)
		}
		for _, tf := range conv {
			for _, src := range tf.Sources {
				if src.PeriodType == "adhoc" || src.SourcePeriodID.String() == *cv.FormsPeriodID {
					t.Fatalf("quy đổi KHTC cộng kỳ đợt điều tra: %+v", tf)
				}
			}
		}
	})

	t.Run("Mở lại: chỉ Lãnh đạo phòng, lý do + thời hạn; xã gửi được lại", func(t *testing.T) {
		body := map[string]any{"due_at": "2026-10-15T03:00:00Z", "reason": "Bổ sung số liệu xã X2 theo chỉ đạo"}
		c, b := f.campaignCall(http.MethodPost, "/v1/extension/cnts/campaigns/"+cv.ID+"/reopen", authz.KNTinh, false, body)
		if c != http.StatusForbidden {
			t.Fatalf("cán bộ không phải lãnh đạo mở lại: %d %s", c, b)
		}
		c, b = f.campaignCall(http.MethodPost, "/v1/extension/cnts/campaigns/"+cv.ID+"/reopen", authz.KNTinh, true,
			map[string]any{"due_at": "2026-10-15T03:00:00Z", "reason": "ngắn"})
		f.fail(c, b, "validation_failed")
		c, b = f.campaignCall(http.MethodPost, "/v1/extension/cnts/campaigns/"+cv.ID+"/reopen", authz.KNTinh, true, body)
		f.ok(c, b, &cv)
		if cv.State != "collecting" || cv.DueAt != "2026-10-15T03:00:00Z" || cv.Progress.PeriodStatus != "reopened" {
			t.Fatalf("mở lại: %s", b)
		}
		f.submit(*cv.FormsPeriodID, "X2", catchVals("7", "0", "0", "0"))
		if after := bm18Total(); after != before {
			t.Fatalf("lũy kế đổi sau mở lại đợt: %+v", after)
		}
		var list struct {
			Items []campaignView `json:"items"`
		}
		c, b = f.campaignCall(http.MethodGet, "/v1/extension/cnts/campaigns", authz.KNTinh, false, nil)
		f.ok(c, b, &list)
		if len(list.Items) != 1 || list.Items[0].Progress == nil || list.Items[0].Progress.Submitted != 2 {
			t.Fatalf("danh sách: %s", b)
		}
		acts := []string{}
		for _, e := range cv.Events {
			acts = append(acts, e.Action)
		}
		want := []string{"published", "reminded", "extended", "closed", "reopened"}
		if len(acts) != len(want) {
			t.Fatalf("sự kiện: %v", acts)
		}
		for i := range want {
			if acts[i] != want[i] {
				t.Fatalf("sự kiện: %v", acts)
			}
		}
		// Sổ chỉ ghi thêm.
		if _, err := f.adminExec(`UPDATE pf_extension.cnts_campaign_event SET reason = 'sửa trái phép sổ'`); err == nil {
			t.Fatal("sổ sự kiện đợt phải chặn UPDATE")
		}
	})
}
