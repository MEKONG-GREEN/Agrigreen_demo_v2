package kn77_test

// BF-CN-FORM-DESIGN-EDIT (#192) — BRD CNTS v4.0 FN-07 (B1–B2, luồng phụ sao chép/nạp Excel, BR-01, BR-02, AC-03), UR-TINH-07,
// ma trận quyền FN-07 (cán bộ tổng hợp CRU), FN-21 B1 "biểu và phiên bản". PG thật qua pf/forms + pf/extension.

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms"
	formsport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/idempotency"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

// designAs gọi API với phiên Cấp Tỉnh CNTS thuộc nhóm group (rỗng = chưa khai nhóm) hoặc vai khác.
func designAs(f *aggFx, method, path, role, group string, body any) (int, []byte) {
	f.t.Helper()
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest(method, path, bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	k := authz.KNContext{Role: role, Department: aggDept, OrgRef: f.node, Scope: authz.Scope{Units: []string{"X1", "X2"}}, ProvinceGroup: group}
	if role == authz.KNAdmin {
		k = authz.KNContext{Role: role, Scope: authz.Scope{All: true}}
	}
	kj, _ := json.Marshal(k)
	req.Header.Set("X-Test-KN", string(kj))
	req.Header.Set("X-Test-Actor", ids.New().String())
	if method == http.MethodPost {
		req.Header.Set(idempotency.Header, "d-"+ids.New().String())
	}
	w := httptest.NewRecorder()
	f.eng.ServeHTTP(w, req)
	return w.Code, w.Body.Bytes()
}

type designEditField struct {
	Key         string   `json:"key"`
	Label       string   `json:"label"`
	Type        string   `json:"type"`
	Required    bool     `json:"required"`
	Unit        string   `json:"unit,omitempty"`
	NonNegative bool     `json:"non_negative,omitempty"`
	Group       string   `json:"group,omitempty"`
	Options     []string `json:"options,omitempty"`
}

type designEditView struct {
	Code       string            `json:"code"`
	Kind       string            `json:"kind"`
	Locked     bool              `json:"locked"`
	CanEdit    bool              `json:"can_edit"`
	TemplateID *string           `json:"template_id"`
	Version    *int64            `json:"version"`
	Fields     []designEditField `json:"fields"`
}

type designSaved struct {
	Code    string `json:"code"`
	Version struct {
		TemplateID string            `json:"template_id"`
		Version    int64             `json:"version"`
		Fields     []designEditField `json:"fields"`
	} `json:"version"`
}

type formsPeriodView struct {
	Period struct {
		TemplateID      string `json:"template_id"`
		TemplateVersion int64  `json:"template_version"`
	} `json:"period"`
	Units []struct {
		Unit       string `json:"unit"`
		Submission *struct {
			Values map[string]string `json:"values"`
		} `json:"submission"`
	} `json:"units"`
}

func TestCNTSDesignEdit_FN07_192(t *testing.T) {
	var pub formsport.Publisher
	f := newAggFx(t, facilityDir{}, func(fm *forms.Module, kn *extension.Module) {
		pub = fm.Publisher()
		kn.UseCNTSCampaigns(extension.CNTSCampaignDeps{Forms: fm.Publisher(), Control: fm.Control(), Conditional: fm.ConditionalControl(), Figures: fm.Figures()})
	})
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
	const base = "/v1/extension/cnts/form-designs"
	list := func(group string) map[string]designEditView {
		t.Helper()
		var out struct {
			Items []designEditView `json:"items"`
		}
		c, b := designAs(f, http.MethodGet, base, authz.KNTinh, group, nil)
		f.ok(c, b, &out)
		m := map[string]designEditView{}
		for _, v := range out.Items {
			m[v.Code] = v
		}
		return m
	}
	templates := func() int { return len(f.admin.query("SELECT id FROM pf_forms.template")) }
	bm17 := list("tong_hop")["CNTS-BM17"]
	if bm17.Locked || !bm17.CanEdit || bm17.Version == nil || *bm17.Version != 1 {
		t.Fatalf("BM-17 sửa được: %+v", bm17)
	}
	if v := list("tong_hop")["CNTS-BM04"]; !v.Locked || v.CanEdit {
		t.Fatalf("BM-04 KNQG khóa: %+v", v)
	}
	if v := list("vien_chuc")["CNTS-BM17"]; v.CanEdit {
		t.Fatalf("viên chức chỉ xem: %+v", v)
	}
	added := append(append([]designEditField{}, bm17.Fields...), designEditField{Key: "cua", Label: "TS-05 Cua", Type: "decimal", Unit: "tấn",
		NonNegative: true, Group: "Sản lượng khai thác trong tuần"})

	t.Run("quyền: viên chức, lãnh đạo phòng, Cấp Quốc gia, Cấp Xã không lưu phiên bản", func(t *testing.T) {
		before := templates()
		for _, x := range []struct{ role, group string }{{authz.KNTinh, "vien_chuc"}, {authz.KNTinh, "lanh_dao_phong"}, {authz.KNTW, ""}, {authz.KNXa, ""}} {
			c, b := designAs(f, http.MethodPost, base+"/CNTS-BM17/versions", x.role, x.group, map[string]any{"base_version": 1, "title": "x", "fields": added})
			if c != http.StatusForbidden {
				t.Fatalf("%s/%s: %d %s", x.role, x.group, c, b)
			}
		}
		if templates() != before {
			t.Fatal("ghi khi bị từ chối")
		}
	})

	t.Run("BR-02: mẫu KNQG từ chối qua API KN, REST cap/forms và Publisher", func(t *testing.T) {
		before := templates()
		c, b := designAs(f, http.MethodPost, base+"/CNTS-BM04/versions", authz.KNTinh, "tong_hop", map[string]any{"base_version": 1, "title": "BM-04", "fields": added})
		if c != http.StatusForbidden {
			t.Fatalf("API KN: %d %s", c, b)
		}
		c, b = designAs(f, http.MethodPost, "/v1/forms/templates", authz.KNTinh, "tong_hop", map[string]any{"profile": "kn_cnts", "code": "CNTS-BM04", "title": "BM-04",
			"fields": added, "base_version": 1})
		if c != http.StatusForbidden {
			t.Fatalf("REST: %d %s", c, b)
		}
		admin := authz.WithPrincipal(ctx, bindKNTestSession(t, authz.Principal{ActorRef: ids.New(), TenantID: f.tid,
			KN: &authz.KNContext{ID: ids.New(), Role: authz.KNAdmin, Scope: authz.Scope{All: true}}}))
		raw, _ := json.Marshal(added)
		one := int64(1)
		if _, err := pub.DefineTemplate(admin, formsport.TemplateDef{Profile: "kn_cnts", Code: "CNTS-BM04", Title: "BM-04", Fields: raw, BaseVersion: &one}, "knqg-192"); err == nil {
			t.Fatal("Publisher: Admin đổi cấu trúc KNQG phải bị từ chối")
		}
		if templates() != before {
			t.Fatal("KNQG bị ghi")
		}
	})

	t.Run("AC-03: sửa cấu trúc không đổi kỳ đã nộp; kỳ mới dùng phiên bản mới", func(t *testing.T) {
		old := f.publish("CNTS-BM17", "2026-09-29", "2026-10-05", "X1")
		f.submit(old.Periods[0].ID, "X1", catchVals("4", "0", "1", "0"))
		c, b := designAs(f, http.MethodPost, base+"/CNTS-BM17/versions", authz.KNTinh, "tong_hop", map[string]any{"base_version": 1,
			"title": "BM-17 — có cột cua", "fields": added})
		var saved designSaved
		f.ok(c, b, &saved)
		if c != http.StatusCreated || saved.Version.Version != 2 || len(saved.Version.Fields) != 5 {
			t.Fatalf("lưu v2: %d %s", c, b)
		}
		// Lưu chồng trên bản đã cũ → 409, không ghi.
		c, b = designAs(f, http.MethodPost, base+"/CNTS-BM17/versions", authz.KNTinh, "tong_hop", map[string]any{"base_version": 1,
			"title": "BM-17 cũ", "fields": added})
		if c != http.StatusConflict {
			t.Fatalf("base cũ: %d %s", c, b)
		}
		// UR-TINH-07: mẫu đã phát hành chỉ thêm cột — xóa chỉ tiêu bị chặn (API KN 422, REST cap/forms 403 qua chính sách).
		c, b = designAs(f, http.MethodPost, base+"/CNTS-BM17/versions", authz.KNTinh, "tong_hop", map[string]any{"base_version": 2,
			"title": "BM-17 xóa", "fields": added[1:]})
		f.fail(c, b, "validation_failed")
		c, b = designAs(f, http.MethodPost, "/v1/forms/templates", authz.KNTinh, "tong_hop", map[string]any{"profile": "kn_cnts", "code": "CNTS-BM17",
			"title": "BM-17 xóa", "fields": added[1:], "base_version": 2})
		if c != http.StatusForbidden {
			t.Fatalf("REST xóa cột: %d %s", c, b)
		}
		var pv formsPeriodView
		c, b = f.call(http.MethodGet, "/v1/forms/periods/"+old.Periods[0].ID+"?profile=kn_cnts", authz.KNTinh, nil)
		f.ok(c, b, &pv)
		if pv.Period.TemplateVersion != 1 || pv.Period.TemplateID != *bm17.TemplateID || len(pv.Units) != 1 || pv.Units[0].Submission == nil ||
			pv.Units[0].Submission.Values["ca"] != "4" || pv.Units[0].Submission.Values["muc"] != "1" {
			t.Fatalf("kỳ đã nộp phải giữ phiên bản 1 và số liệu: %s", b)
		}
		if _, ok := pv.Units[0].Submission.Values["cua"]; ok {
			t.Fatalf("bài cũ không có cột mới: %s", b)
		}
		next := f.publish("CNTS-BM17", "2026-10-06", "2026-10-12", "X1")
		c, b = f.call(http.MethodGet, "/v1/forms/periods/"+next.Periods[0].ID+"?profile=kn_cnts", authz.KNTinh, nil)
		f.ok(c, b, &pv)
		if pv.Period.TemplateVersion != 2 || pv.Period.TemplateID != saved.Version.TemplateID {
			t.Fatalf("kỳ mới dùng phiên bản 2: %s", b)
		}
		if v := list("tong_hop")["CNTS-BM17"]; *v.Version != 2 || len(v.Fields) != 5 {
			t.Fatalf("màn đọc phiên bản đã lưu: %+v", v)
		}
		// Cài lại biểu chuẩn không ghi đè phiên bản Cấp Tỉnh đã lưu.
		c, b = f.call(http.MethodPost, "/v1/extension/cnts/period-forms/install", authz.KNAdmin, nil)
		f.ok(c, b, nil)
		if v := list("tong_hop")["CNTS-BM17"]; *v.Version != 2 {
			t.Fatalf("cài lại ghi đè BM-17: %+v", v)
		}
		var hist struct {
			Items []struct {
				Version int64 `json:"version"`
			} `json:"items"`
		}
		c, b = designAs(f, http.MethodGet, base+"/CNTS-BM17/versions", authz.KNTinh, "vien_chuc", nil)
		f.ok(c, b, &hist)
		if len(hist.Items) != 2 || hist.Items[0].Version != 2 || hist.Items[1].Version != 1 {
			t.Fatalf("lịch sử: %s", b)
		}
	})

	t.Run("sao chép biểu thành biểu điều tra phiên bản 1; mã trùng 409; cấu trúc sai không ghi", func(t *testing.T) {
		c, b := designAs(f, http.MethodPost, base, authz.KNTinh, "tong_hop", map[string]any{"code_suffix": "tongdan", "title": "Điều tra tổng đàn",
			"source_code": "CNTS-BM04"})
		var saved designSaved
		f.ok(c, b, &saved)
		bm04 := list("tong_hop")["CNTS-BM04"]
		if saved.Code != "CNTS-DT-TONGDAN" || saved.Version.Version != 1 || len(saved.Version.Fields) != len(bm04.Fields) {
			t.Fatalf("sao chép: %s", b)
		}
		if v := list("tong_hop")["CNTS-DT-TONGDAN"]; v.Kind != "survey" || !v.CanEdit || *v.Version != 1 {
			t.Fatalf("danh sách có biểu điều tra: %+v", v)
		}
		if v := list("tong_hop")["CNTS-BM04"]; *v.Version != 1 {
			t.Fatalf("mẫu gốc không đổi: %+v", v)
		}
		c, b = designAs(f, http.MethodPost, base, authz.KNTinh, "tong_hop", map[string]any{"code_suffix": "TONGDAN", "title": "Trùng",
			"source_code": "CNTS-BM17"})
		if c != http.StatusConflict {
			t.Fatalf("mã trùng: %d %s", c, b)
		}
		before := templates()
		for _, fs := range [][]designEditField{{{Key: "Sai Ma", Label: "x", Type: "integer"}}, {{Key: "a", Label: "x", Type: "integer"}, {Key: "a", Label: "y", Type: "text"}},
			{{Key: "a", Label: "x", Type: "choice"}}} {
			c, b = designAs(f, http.MethodPost, base, authz.KNTinh, "tong_hop", map[string]any{"code_suffix": "SAI", "title": "Sai", "fields": fs})
			f.fail(c, b, "validation_failed")
		}
		c, b = designAs(f, http.MethodPost, base, authz.KNTinh, "tong_hop", map[string]any{"code_suffix": "bad suffix!", "title": "Sai",
			"fields": []designEditField{{Key: "a", Label: "x", Type: "integer"}}})
		f.fail(c, b, "validation_failed")
		if templates() != before {
			t.Fatal("cấu trúc sai vẫn ghi")
		}
		// Kết quả kiểm Excel hợp lệ lưu bằng thao tác tường minh (fields trả từ import-preview).
		c, b = designAs(f, http.MethodPost, base, authz.KNTinh, "", map[string]any{"code_suffix": "THIETHAI", "title": "Khảo sát thiệt hại dịch",
			"fields": []designEditField{{Key: "so_con_chet", Label: "Số con chết", Type: "integer", Required: true, NonNegative: true}}})
		f.ok(c, b, &saved)
		if saved.Code != "CNTS-DT-THIETHAI" || saved.Version.Version != 1 {
			t.Fatalf("lưu từ Excel: %s", b)
		}
	})

	t.Run("FN-21: đợt điều tra phát hành đúng phiên bản biểu đã chọn", func(t *testing.T) {
		in := map[string]any{"owner_ref": ids.New().String(), "name": "Điều tra tổng đàn 10/2026", "goal": "Tổng đàn", "field": "chan_nuoi", "template_code": "CNTS-DT-TONGDAN",
			"template_version": 9, "scope_mode": "communes", "communes": []string{"X1", "X2"}, "from_date": "2026-10-08", "to_date": "2026-10-10",
			"due_at": "2026-10-12T03:00:00Z"}
		c, b := f.campaignCall(http.MethodPost, "/v1/extension/cnts/campaigns", authz.KNTinh, false, in)
		f.fail(c, b, "extension.cnts_campaign_template")
		in["template_version"] = 1
		var cv struct {
			campaignView
			TemplateVersion *int64 `json:"template_version"`
		}
		c, b = f.campaignCall(http.MethodPost, "/v1/extension/cnts/campaigns", authz.KNTinh, false, in)
		f.ok(c, b, &cv)
		if cv.TemplateVersion == nil || *cv.TemplateVersion != 1 {
			t.Fatalf("nháp ghi phiên bản đã chọn: %s", b)
		}
		survey := list("tong_hop")["CNTS-DT-TONGDAN"]
		more := append(append([]designEditField{}, survey.Fields...), designEditField{Key: "ghi_chu_dt", Label: "Ghi chú điều tra", Type: "text"})
		c, b = designAs(f, http.MethodPost, base+"/CNTS-DT-TONGDAN/versions", authz.KNTinh, "tong_hop", map[string]any{"base_version": 1,
			"title": "Điều tra tổng đàn v2", "fields": more})
		f.ok(c, b, nil)
		c, b = f.campaignCall(http.MethodPost, "/v1/extension/cnts/campaigns/"+cv.ID+"/publish", authz.KNTinh, false, nil)
		f.ok(c, b, &cv)
		if cv.FormsPeriodID == nil || cv.TemplateVersion == nil || *cv.TemplateVersion != 1 {
			t.Fatalf("phát hành: %s", b)
		}
		var pv formsPeriodView
		c, b = f.call(http.MethodGet, "/v1/forms/periods/"+*cv.FormsPeriodID+"?profile=kn_cnts", authz.KNTinh, nil)
		f.ok(c, b, &pv)
		if pv.Period.TemplateVersion != 1 || pv.Period.TemplateID != *survey.TemplateID {
			t.Fatalf("kỳ của đợt phải dùng phiên bản 1 đã chọn, không phải mới nhất: %s", b)
		}
	})
}
