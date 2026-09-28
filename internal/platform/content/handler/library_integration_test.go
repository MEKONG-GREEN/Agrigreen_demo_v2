package handler_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	syncport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/sync/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// KN US-LIB-05: danh mục cá nhân — lưu, bỏ lưu, phạm vi, tách theo người, đồng bộ xuống máy.
func TestPersonalLibraryUSLIB05(t *testing.T) {
	f := newFixture(t)
	a := f.actors()
	d := createDoc(t, a.p89Editor, map[string]any{"kind": "guide", "audience": map[string]any{"level": "province", "units": []string{"89"}},
		"title": "Sổ tay bón phân", "body": "Bón lót trước khi sạ."})
	publish(t, a.p89Editor, a.p89Reviewer, d)

	reader := a.reader30001
	r := reader.do(f.app.URL, http.MethodPost, "/v1/documents/"+d.ref+"/save", map[string]any{"note": "đọc ngoài đồng"})
	expect(t, r, http.StatusOK, "")
	if r.body["available"] != true || str(r.body, "title") != "Sổ tay bón phân" || str(r.body, "note") != "đọc ngoài đồng" {
		t.Fatalf("lưu: %v", r.body)
	}
	// Tỉnh khác không thấy tài liệu → không lưu được (không lộ sự tồn tại).
	expect(t, a.reader92.do(f.app.URL, http.MethodPost, "/v1/documents/"+d.ref+"/save", nil), http.StatusNotFound, "content.document_not_found")
	// Danh mục tách theo người dùng.
	if n := len(items(a.reader89.appGet("/v1/library"))); n != 0 {
		t.Fatalf("người khác không thấy danh mục của reader: %d", n)
	}
	lib := reader.appGet("/v1/library")
	if its := items(lib); len(its) != 1 || its[0]["document_ref"] != d.ref {
		t.Fatalf("danh mục: %v", lib.body)
	}

	// Đồng bộ: entity library_item chỉ cho chính người lưu.
	src := f.mod.SyncSource()
	pull := func(c caller, after int64) ([]syncport.Change, int64) {
		ctx := authz.WithPrincipal(tenant.WithID(context.Background(), f.tenant), authz.Principal{ActorRef: c.actor, TenantID: f.tenant, OrgRef: c.org})
		ch, last, err := src.Changes(ctx, authz.Scope{Units: []string{"30001"}}, "library_item", after, 100)
		if err != nil {
			t.Fatal(err)
		}
		return ch, last
	}
	ch, last := pull(reader, 0)
	if len(ch) != 1 || ch[0].Op != syncport.OpUpsert || ch[0].ID.String() != d.ref {
		t.Fatalf("library_item upsert: %+v", ch)
	}
	var pl map[string]any
	_ = json.Unmarshal(ch[0].Data, &pl)
	if pl["available"] != true || pl["document_ref"] != d.ref {
		t.Fatalf("payload: %v", pl)
	}
	if other, _ := pull(a.reader89, 0); len(other) != 0 {
		t.Fatalf("người khác không nhận: %+v", other)
	}

	// Bỏ lưu → tombstone; bỏ lưu lần hai → 404.
	expect(t, reader.do(f.app.URL, http.MethodPost, "/v1/documents/"+d.ref+"/unsave", nil), http.StatusOK, "")
	expect(t, reader.do(f.app.URL, http.MethodPost, "/v1/documents/"+d.ref+"/unsave", nil), http.StatusNotFound, "content.library_item_not_found")
	ch, _ = pull(reader, last)
	if len(ch) != 1 || ch[0].Op != syncport.OpTombstone {
		t.Fatalf("tombstone: %+v", ch)
	}
	if n := len(items(reader.appGet("/v1/library"))); n != 0 {
		t.Fatalf("đã bỏ lưu: %d", n)
	}
}
