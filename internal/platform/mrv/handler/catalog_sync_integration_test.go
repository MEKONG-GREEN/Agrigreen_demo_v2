package handler_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/mrv/port"
	syncport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/sync/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// syncCtx là context như pf/sync gọi Source (sau middleware xác thực).
func syncCtx(tid uuid.UUID, roles ...string) context.Context {
	ctx := authz.WithPrincipal(context.Background(), authz.Principal{ActorRef: ids.New(), TenantID: tid, Roles: roles})
	return tenant.WithID(ctx, tid)
}

func pullCatalogs(t *testing.T, src syncport.Source, ctx context.Context, after int64) ([]syncport.Change, int64) {
	t.Helper()
	ch, last, err := src.Changes(ctx, authz.Scope{Units: []string{"30001"}}, port.SyncEntityCatalog, after, 500)
	if err != nil {
		t.Fatal(err)
	}
	if len(ch) == 0 && last != after {
		t.Fatalf("trang rỗng phải giữ con trỏ: %d → %d", after, last)
	}
	return ch, last
}

func catalogData(t *testing.T, c syncport.Change) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(c.Data, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

// mrv_catalog: chỉ bản ĐÃ DUYỆT; id cố định theo loại, version = số phiên bản
// danh mục; con trỏ theo thời điểm duyệt; người không có vai đọc danh mục không
// nhận gì (không lỗi); tenant khác không thấy. Kèm GET /v1/mrv/evidence-types.
func TestSyncMRVCatalogApprovedOnly(t *testing.T) {
	f := newFixture(t)
	setter := f.as(f.tenant, uuid.Nil, authz.RoleParamSetter)
	approver := f.as(f.tenant, uuid.Nil, authz.RoleParamApprover)
	col := f.as(f.tenant, f.t30001, port.RoleCollector)
	src := f.mod.SyncSource()
	ctx := syncCtx(f.tenant, port.RoleCollector)

	if got := src.Entities(); len(got) != 1 || got[0] != port.SyncEntityCatalog {
		t.Fatalf("Entities = %v", got)
	}
	// Bản nháp chưa duyệt → không có gì.
	expect(t, setter.adm(http.MethodPost, "/v1/mrv/catalog/seed", nil), 200, "")
	if ch, _ := pullCatalogs(t, src, ctx, 0); len(ch) != 0 {
		t.Fatalf("nháp không được đi xuống: %+v", ch)
	}
	r := col.app(http.MethodGet, "/v1/mrv/evidence-types", nil)
	if expect(t, r, 200, ""); len(r.body["items"].([]any)) != 0 {
		t.Fatalf("chưa duyệt → không có loại nào: %v", r.body)
	}

	// Duyệt danh mục mẫu (tạo lại tenant để dùng approveSeed).
	tid := ids.New()
	f.approveSeed(tid)
	ctx = syncCtx(tid, port.RoleCollector)
	ch, cur := pullCatalogs(t, src, ctx, 0)
	if len(ch) != 6 || cur <= 0 {
		t.Fatalf("lượt đầu: %+v, con trỏ %d", ch, cur)
	}
	for _, c := range ch {
		m := catalogData(t, c)
		et, _ := m["evidence_type"].(string)
		if c.Op != syncport.OpUpsert || c.ID != port.CatalogSyncID(et) || c.Version != 1 || m["status"] != "approved" ||
			m["version"] != 1.0 || len(m["fields"].([]any)) == 0 {
			t.Fatalf("thay đổi sai: %+v %v", c, m)
		}
	}
	if again, last := pullCatalogs(t, src, ctx, cur); len(again) != 0 || last != cur {
		t.Fatalf("không đổi: %+v", again)
	}

	// Phiên bản 2 của awd_log: nháp/gửi duyệt/từ chối → không đi xuống; duyệt → thay bản cũ cùng id.
	setter2 := f.as(tid, uuid.Nil, authz.RoleParamSetter)
	approver2 := f.as(tid, uuid.Nil, authz.RoleParamApprover)
	propose := func(et string) string {
		v := setter2.adm(http.MethodPost, "/v1/mrv/catalog/versions", map[string]any{
			"evidence_type": et, "provisional": true, "source_note": "Kiểm thử đồng bộ", "fields": []any{
				map[string]any{"key": "awd_water_level_cm", "label_vi": "Mực nước", "data_type": "decimal", "unit": "cm", "required": true, "min": "-30", "max": "30"},
				map[string]any{"key": "photo", "label_vi": "Ảnh", "data_type": "photo", "required": true, "min": "1"},
			}})
		expect(t, v, 201, "")
		expect(t, setter2.adm(http.MethodPost, "/v1/mrv/catalog/versions/"+v.str("id")+"/submit", nil), 200, "")
		return v.str("id")
	}
	v2 := propose(port.EvidenceAWDLog)
	rej := propose("rice_straw_log")
	expect(t, approver2.adm(http.MethodPost, "/v1/mrv/catalog/versions/"+rej+"/reject", map[string]any{"note": "Chưa đủ căn cứ"}), 200, "")
	if ch, _ := pullCatalogs(t, src, ctx, cur); len(ch) != 0 {
		t.Fatalf("chưa duyệt/từ chối không được đi xuống: %+v", ch)
	}
	expect(t, approver2.adm(http.MethodPost, "/v1/mrv/catalog/versions/"+v2+"/approve", map[string]any{"note": "Duyệt v2"}), 200, "")
	ch, cur2 := pullCatalogs(t, src, ctx, cur)
	if len(ch) != 1 || cur2 <= cur || ch[0].ID != port.CatalogSyncID(port.EvidenceAWDLog) || ch[0].Version != 2 {
		t.Fatalf("v2: %+v", ch)
	}
	if m := catalogData(t, ch[0]); m["id"] != v2 || len(m["fields"].([]any)) != 2 {
		t.Fatalf("payload v2: %v", m)
	}

	// Không có vai đọc danh mục → rỗng, không lỗi; tenant khác không thấy.
	if ch, _ := pullCatalogs(t, src, syncCtx(tid, "extension_officer"), 0); len(ch) != 0 {
		t.Fatalf("thiếu vai: %+v", ch)
	}
	if ch, _ := pullCatalogs(t, src, syncCtx(ids.New(), port.RoleCollector), 0); len(ch) != 0 {
		t.Fatalf("tenant khác: %+v", ch)
	}

	// GET /v1/mrv/evidence-types: loại có bản đã duyệt, kèm phiên bản mới nhất.
	col2 := f.as(tid, f.t30001, port.RoleCollector)
	r = col2.app(http.MethodGet, "/v1/mrv/evidence-types", nil)
	expect(t, r, 200, "")
	items := r.body["items"].([]any)
	if len(items) != 6 {
		t.Fatalf("evidence-types: %v", r.body)
	}
	awd, chk := items[0].(map[string]any), items[1].(map[string]any)
	if awd["evidence_type"] != port.EvidenceAWDLog || awd["version"] != 2.0 || awd["catalog_id"] != v2 || awd["field_count"] != 2.0 ||
		awd["provisional"] != true || awd["approved_at"] == nil {
		t.Fatalf("awd_log: %v", awd)
	}
	if chk["evidence_type"] != port.EvidenceFieldChecklist || chk["version"] != 1.0 || chk["field_count"].(float64) < 1 {
		t.Fatalf("field_checklist: %v", chk)
	}
	// Quản trị danh mục cũng đọc được; người không có vai → 403.
	expect(t, approver.app(http.MethodGet, "/v1/mrv/evidence-types", nil), 200, "")
	expect(t, f.as(tid, f.t30001, "extension_officer").app(http.MethodGet, "/v1/mrv/evidence-types", nil), 403, "forbidden")
}
