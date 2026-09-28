package handler_test

import (
	"encoding/json"
	"testing"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata/port"
	syncport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/sync/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
)

// pullAll đọc hết thay đổi của một entity theo trang limit, kiểm con trỏ và
// version tăng dần; trả thay đổi cùng con trỏ cuối.
func pullAll(t *testing.T, c caller, src syncport.Source, sc authz.Scope, entity string, after int64, limit int) ([]syncport.Change, int64) {
	t.Helper()
	var all []syncport.Change
	for range 10000 {
		ch, last, err := src.Changes(c.ctx(), sc, entity, after, limit)
		if err != nil {
			t.Fatalf("Changes(%s, %d): %v", entity, after, err)
		}
		if len(ch) > limit {
			t.Fatalf("%s: trang %d > limit %d", entity, len(ch), limit)
		}
		if len(ch) > 0 && last <= after {
			t.Fatalf("%s: con trỏ không tăng: %d → %d", entity, after, last)
		}
		for _, x := range ch {
			if x.Entity != entity || x.Version <= after || x.Version > last {
				t.Fatalf("%s: thay đổi sai entity/version: %+v (after %d, last %d)", entity, x, after, last)
			}
			if (x.Op == syncport.OpTombstone) != (x.Data == nil) {
				t.Fatalf("%s: tombstone phải không có data: %+v", entity, x)
			}
		}
		all = append(all, ch...)
		after = last
		if len(ch) < limit {
			return all, after
		}
	}
	t.Fatal("không dừng")
	return nil, after
}

func byCode(t *testing.T, chs []syncport.Change) map[string]map[string]any {
	t.Helper()
	out := map[string]map[string]any{}
	for _, c := range chs {
		if c.Op != syncport.OpUpsert {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal(c.Data, &m); err != nil {
			t.Fatal(err)
		}
		key, _ := m["code"].(string)
		if cat, ok := m["catalog"].(string); ok {
			key = cat + "/" + key
		}
		out[key] = m
	}
	return out
}

// catalog_item: toàn bộ danh mục dùng chung theo con trỏ; ngừng dùng → tombstone
// cùng id; dùng lại → upsert version mới; tenant khác không thấy.
func TestSyncCatalogItemCursorAndTombstone(t *testing.T) {
	f := newFixture(t)
	tid := f.seeded()
	admin := f.caller(tid, port.RoleMasterdataAdmin)
	u := f.caller(tid)
	src := f.mod.SyncSource()
	sc := authz.Scope{Units: []string{"92"}}

	if got := src.Entities(); len(got) != 2 || got[0] != port.SyncEntityCatalogItem || got[1] != port.SyncEntityAdminUnit {
		t.Fatalf("Entities = %v", got)
	}
	first, cur := pullAll(t, u, src, sc, port.SyncEntityCatalogItem, 0, 7)
	items := byCode(t, first)
	if len(first) < 20 || len(items) != len(first) {
		t.Fatalf("lượt đầu: %d thay đổi, %d upsert", len(first), len(items))
	}
	seen := map[string]bool{}
	for _, c := range first {
		seen[c.ID.String()] = true
	}
	if len(seen) != len(first) {
		t.Fatal("id trùng trong lượt đầu")
	}
	for _, cat := range []string{port.CatalogRiceVariety, port.CatalogFarmStage, port.CatalogMachineType, port.CatalogSeason, port.CatalogPest} {
		found := false
		for k := range items {
			found = found || len(k) > len(cat) && k[:len(cat)+1] == cat+"/"
		}
		if !found {
			t.Errorf("thiếu danh mục %s", cat)
		}
	}
	// Không có gì mới → trang rỗng, con trỏ giữ nguyên.
	if ch, last, err := src.Changes(u.ctx(), sc, port.SyncEntityCatalogItem, cur, 50); err != nil || len(ch) != 0 || last != cur {
		t.Fatalf("không đổi: %v %d %v", ch, last, err)
	}

	if code, b := admin.adm("POST", "/v1/masterdata/catalogs/pest/items", map[string]any{
		"code": "KT_SAU_MOI", "name": "Sâu kiểm thử", "attrs": map[string]string{"kind": "sau_hai"}}); code != 201 {
		t.Fatalf("thêm mục: %d %v", code, b)
	}
	ch, cur2 := pullAll(t, u, src, sc, port.SyncEntityCatalogItem, cur, 50)
	if len(ch) != 1 || ch[0].Op != syncport.OpUpsert || ch[0].ID != port.CatalogItemSyncID("pest", "KT_SAU_MOI") {
		t.Fatalf("mục mới: %+v", ch)
	}
	if m := byCode(t, ch)["pest/KT_SAU_MOI"]; m["name"] != "Sâu kiểm thử" || m["revision"] != 1.0 || m["attrs"].(map[string]any)["kind"] != "sau_hai" {
		t.Fatalf("payload: %v", m)
	}
	upVersion := ch[0].Version

	// Ngừng dùng → tombstone cùng id, version lớn hơn.
	if code, b := admin.adm("POST", "/v1/masterdata/catalogs/pest/items/KT_SAU_MOI/deactivate", map[string]any{"reason": "Mục kiểm thử, ngừng dùng"}); code != 200 {
		t.Fatalf("ngừng dùng: %d %v", code, b)
	}
	ch, cur3 := pullAll(t, u, src, sc, port.SyncEntityCatalogItem, cur2, 50)
	if len(ch) != 1 || ch[0].Op != syncport.OpTombstone || ch[0].ID != port.CatalogItemSyncID("pest", "KT_SAU_MOI") || ch[0].Version <= upVersion {
		t.Fatalf("tombstone: %+v", ch)
	}
	// Dùng lại → upsert mới.
	if code, b := admin.adm("POST", "/v1/masterdata/catalogs/pest/items/KT_SAU_MOI/reactivate", map[string]any{"reason": "Dùng lại mục kiểm thử"}); code != 200 {
		t.Fatalf("dùng lại: %d %v", code, b)
	}
	ch, _ = pullAll(t, u, src, sc, port.SyncEntityCatalogItem, cur3, 50)
	if len(ch) != 1 || ch[0].Op != syncport.OpUpsert || ch[0].Version <= cur3 {
		t.Fatalf("dùng lại: %+v", ch)
	}

	// Cách ly tenant: tenant khác thấy danh mục của mình, không thấy mục mới ở trên.
	other := f.caller(f.seeded())
	oc, _ := pullAll(t, other, src, sc, port.SyncEntityCatalogItem, 0, 500)
	if _, ok := byCode(t, oc)["pest/KT_SAU_MOI"]; ok || len(oc) != len(first) {
		t.Fatalf("tenant khác: %d thay đổi", len(oc))
	}
}

// admin_unit: chỉ đơn vị trong phạm vi; phiên bản hết hiệu lực → tombstone (kể
// cả đơn vị cũ đã sáp nhập vào địa bàn); đóng hiệu lực lấy sync_seq mới.
func TestSyncAdminUnitScopeAndTombstone(t *testing.T) {
	f := newFixture(t)
	tid := f.seeded()
	admin := f.caller(tid, port.RoleMasterdataAdmin)
	u := f.caller(tid)
	src := f.mod.SyncSource()

	// Phạm vi rỗng → không có gì.
	if ch, last, err := src.Changes(u.ctx(), authz.Scope{}, port.SyncEntityAdminUnit, 0, 100); err != nil || len(ch) != 0 || last != 0 {
		t.Fatalf("phạm vi rỗng: %v %d %v", ch, last, err)
	}

	// Phạm vi tỉnh An Giang mới (91) và một xã.
	sc := authz.Scope{Units: []string{"91", "30742"}}
	ch, cur := pullAll(t, u, src, sc, port.SyncEntityAdminUnit, 0, 3)
	up := byCode(t, ch)
	if len(up) != 2 || up["91"]["name"] != "An Giang" || up["30742"] == nil || up["91"]["parent_code"] != nil || up["30742"]["parent_code"] != "91" {
		t.Fatalf("upsert trong phạm vi: %v", up)
	}
	var tomb int
	for _, c := range ch {
		if c.Op == syncport.OpTombstone {
			tomb++
		}
	}
	// Kiên Giang (91 cũ), An Giang cũ (89 → 91) và các đơn vị cũ đã nhập vào 30742.
	if tomb < 2 {
		t.Fatalf("tombstone đơn vị cũ: %d trong %d", tomb, len(ch))
	}
	ag := port.AdminUnitSyncID("91", day("2025-07-01"))
	for _, c := range ch {
		if c.ID == ag && c.Op != syncport.OpUpsert {
			t.Fatalf("An Giang mới phải là upsert: %+v", c)
		}
	}

	// Toàn quốc: nhiều hơn hẳn; phạm vi khác không lẫn.
	allCh, _ := pullAll(t, u, src, authz.Scope{All: true}, port.SyncEntityAdminUnit, 0, 1000)
	if len(allCh) <= len(ch) || len(byCode(t, allCh)) < 100 {
		t.Fatalf("toàn quốc: %d", len(allCh))
	}
	other, _ := pullAll(t, u, src, authz.Scope{Units: []string{"92"}}, port.SyncEntityAdminUnit, 0, 1000)
	if o := byCode(t, other); o["91"] != nil || o["92"] == nil {
		t.Fatalf("phạm vi 92 lẫn đơn vị khác: %d", len(o))
	}

	// Đóng hiệu lực trong tương lai → upsert có valid_to; đơn vị mới (chưa hiệu lực) cũng đi xuống.
	split := map[string]any{
		"format": "agrigreen.admin_units/v1", "source": "Kiểm thử chia tách",
		"units": []map[string]any{
			{"code": "30742", "name": up["30742"]["name"], "level": "commune", "parent_code": "91", "valid_from": up["30742"]["valid_from"],
				"valid_to": "2099-01-01", "successors": []string{"KT-30742-A"}},
			{"code": "KT-30742-A", "name": "Rạch Giá A", "level": "commune", "parent_code": "91", "valid_from": "2099-01-01"},
		},
	}
	if code, b := admin.adm("POST", "/v1/masterdata/admin-units/import", split); code != 200 || b["closed"] != 1.0 {
		t.Fatalf("nhập: %d %v", code, b)
	}
	sc.Units = append(sc.Units, "KT-30742-A")
	ch, cur = pullAll(t, u, src, sc, port.SyncEntityAdminUnit, cur, 50)
	up = byCode(t, ch)
	if len(ch) != 2 || up["30742"]["valid_to"] != "2099-01-01" || up["KT-30742-A"]["valid_from"] != "2099-01-01" {
		t.Fatalf("đóng trong tương lai: %v", up)
	}

	// Đóng hiệu lực đã qua → tombstone cùng id phiên bản.
	past := map[string]any{"format": "agrigreen.admin_units/v1", "source": "Kiểm thử sáp nhập", "units": []map[string]any{
		{"code": "91", "name": "An Giang", "level": "province", "valid_from": "2025-07-01", "valid_to": "2026-01-01", "successors": []string{"92"}},
	}}
	if code, b := admin.adm("POST", "/v1/masterdata/admin-units/import", past); code != 200 || b["closed"] != 1.0 {
		t.Fatalf("nhập đóng 91: %d %v", code, b)
	}
	ch, _ = pullAll(t, u, src, sc, port.SyncEntityAdminUnit, cur, 50)
	if len(ch) != 1 || ch[0].Op != syncport.OpTombstone || ch[0].ID != ag {
		t.Fatalf("đóng đã qua: %+v", ch)
	}
}
