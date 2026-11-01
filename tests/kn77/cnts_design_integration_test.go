package kn77_test

import (
	"bytes"
	"encoding/json"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tabular"
	"github.com/xuri/excelize/v2"
	"net/http"
	"net/http/httptest"
	"testing"
)

func designRaw(f *aggFx, method, path, role, dept string, body []byte) (int, []byte) {
	req := httptest.NewRequest(method, path, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/octet-stream")
	k := authz.KNContext{Role: role, Department: dept, OrgRef: f.node, Scope: authz.Scope{Units: []string{"X1", "X2"}}}
	if role == authz.KNAdmin {
		k.Scope.All = true
	}
	raw, _ := json.Marshal(k)
	req.Header.Set("X-Test-KN", string(raw))
	req.Header.Set("X-Test-Actor", ids.New().String())
	req.Header.Set("Idempotency-Key", "design-"+ids.New().String())
	w := httptest.NewRecorder()
	f.eng.ServeHTTP(w, req)
	return w.Code, w.Body.Bytes()
}
func TestCNTSDesignReadAndImport_BF_CN_FORM_DESIGN(t *testing.T) {
	f := newAggFx(t, facilityDir{})
	const path = "/v1/extension/cnts/form-designs"
	var list struct {
		Items []designView `json:"items"`
	}
	c, b := f.call(http.MethodGet, path, authz.KNTinh, nil)
	f.ok(c, b, &list)
	if len(list.Items) != 5 || list.Items[0].Version != nil {
		t.Fatalf("uninstalled: %s", b)
	}
	c, b = f.call(http.MethodPost, "/v1/extension/cnts/period-forms/install", authz.KNAdmin, nil)
	f.ok(c, b, nil)
	c, b = f.call(http.MethodGet, path, authz.KNTinh, nil)
	f.ok(c, b, &list)
	var oldID string
	for _, v := range list.Items {
		if v.Code == "CNTS-BM04" {
			oldID = *v.TemplateID
			if *v.Version != 1 {
				t.Fatal(v)
			}
		}
	}
	// Admin cài một cấu trúc phiên bản mới qua cổng cap/forms hiện có. Màn phải đọc bản đó, không dựng lại từ builtin.
	fields := []map[string]any{{"key": "design_marker", "label": "Cột phiên bản hai", "type": "text", "required": false}}
	c, b = f.call(http.MethodPost, "/v1/forms/templates", authz.KNAdmin, map[string]any{"profile": "kn_cnts", "code": "CNTS-BM04", "title": "BM04 phiên bản hai", "fields": fields})
	f.ok(c, b, nil)
	c, b = f.call(http.MethodGet, path, authz.KNTinh, nil)
	f.ok(c, b, &list)
	for _, v := range list.Items {
		if v.Code == "CNTS-BM04" && (*v.Version != 2 || len(v.Fields) != 1 || v.Fields[0].Key != "design_marker" || *v.TemplateID == oldID) {
			t.Fatalf("installed version must win: %+v", v)
		}
	}
	c, b = f.call(http.MethodGet, "/v1/forms/templates/"+oldID+"?profile=kn_cnts", authz.KNTinh, nil)
	f.ok(c, b, nil)
	if bytes.Contains(b, []byte("design_marker")) {
		t.Fatal("old template changed")
	}
	for _, x := range []struct{ role, dept string }{{authz.KNXa, aggDept}, {authz.KNTW, aggDept}, {authz.KNTinh, "hanh_chinh_tong_hop"}} {
		for _, method := range []string{http.MethodGet, http.MethodPost} {
			target := path
			if method == http.MethodPost {
				target += "/import-preview"
			}
			c, b = designRaw(f, method, target, x.role, x.dept, nil)
			if c != 403 {
				t.Fatalf("%s %s/%s: %d %s", method, x.role, x.dept, c, b)
			}
		}
	}
	before := len(f.admin.query("SELECT id FROM pf_forms.template"))
	var excel bytes.Buffer
	if err := tabular.WriteXLSX(&excel, "Cau truc", designHeaders, [][]string{{"so_con", "Số con", "integer", "true", "con", "Tổng đàn", "true", ""}}); err != nil {
		t.Fatal(err)
	}
	c, b = designRaw(f, http.MethodPost, path+"/import-preview", authz.KNTinh, aggDept, excel.Bytes())
	f.ok(c, b, nil)
	var result designImport
	if json.Unmarshal(b, &result) != nil || !result.Valid || len(result.Fields) != 1 {
		t.Fatalf("xlsx: %s", b)
	}
	bad := []byte("key,label,type,required,unit,group,non_negative,options\na,Chỉ tiêu,integer,1e100000,con,,false,\n")
	c, b = designRaw(f, http.MethodPost, path+"/import-preview", authz.KNTinh, aggDept, bad)
	f.ok(c, b, nil)
	if json.Unmarshal(b, &result) != nil || result.Valid || len(result.Fields) != 0 || len(result.Issues) == 0 || result.Issues[0].Line != 2 {
		t.Fatalf("invalid CSV: %s", b)
	}
	book := excelize.NewFile()
	if err := book.SetCellFormula("Sheet1", "A1", "1+1"); err != nil {
		t.Fatal(err)
	}
	data, err := book.WriteToBuffer()
	if err != nil {
		t.Fatal(err)
	}
	_ = book.Close()
	c, b = designRaw(f, http.MethodPost, path+"/import-preview", authz.KNAdmin, aggDept, data.Bytes())
	if c != 422 {
		t.Fatalf("formula must reject: %d %s", c, b)
	}
	after := len(f.admin.query("SELECT id FROM pf_forms.template"))
	if before != after {
		t.Fatalf("preview wrote templates: %d -> %d", before, after)
	}
}

// HTTP DTOs are local: external integration tests only import module roots/ports.
var designHeaders = []string{"key", "label", "type", "required", "unit", "group", "non_negative", "options"}

type designField struct {
	Key string `json:"key"`
}
type designView struct {
	Code       string        `json:"code"`
	TemplateID *string       `json:"template_id"`
	Version    *int64        `json:"version"`
	Fields     []designField `json:"fields"`
}
type designImport struct {
	Valid  bool          `json:"valid"`
	Fields []designField `json:"fields"`
	Issues []struct {
		Line int `json:"line"`
	} `json:"issues"`
}
