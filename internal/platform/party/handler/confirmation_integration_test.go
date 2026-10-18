package handler_test

import (
	"net/http"
	"slices"
	"testing"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
)

// D2 (ADR-0052, quy tắc 13/10/2026): danh sách HTX mang trạng thái xác nhận + nhãn "Chưa xác nhận"; HTX tạm có mặt
// trong danh sách mặc định (tính vào tổng); lọc được theo ?confirmation=; giá trị lạ → 422.
func TestCooperativeListConfirmation_D2(t *testing.T) {
	f := newFixture(t)
	tr := f.seedTree(f.root())
	// c1 đã nhận quyền (pf/cooperative tính từ neo), c2 là HTX tạm.
	f.profiles.mu.Lock()
	p := f.profiles.profiles[f.tenant][tr.c1]
	p.Confirmed = true
	f.profiles.profiles[f.tenant][tr.c1] = p
	f.profiles.mu.Unlock()

	nat := f.as(f.tenant, tr.national, port.RoleKNNationalAdmin)
	all := items(nat.appDo(http.MethodGet, "/v1/cooperatives", nil))
	if len(all) != 2 {
		t.Fatalf("mặc định phải gồm cả HTX tạm: %v", all)
	}
	for _, it := range all {
		want, label := "unconfirmed", "Chưa xác nhận"
		if it["ref"] == tr.c1.String() {
			want, label = "confirmed", "Đã xác nhận"
		}
		if it["confirmation_status"] != want || it["confirmation_label"] != label {
			t.Errorf("trạng thái xác nhận của %v: %v / %v", it["ref"], it["confirmation_status"], it["confirmation_label"])
		}
	}
	if got := refs(nat.appDo(http.MethodGet, "/v1/cooperatives?confirmation=unconfirmed", nil), "ref"); !slices.Equal(got, []string{tr.c2.String()}) {
		t.Fatalf("lọc Chưa xác nhận: %v", got)
	}
	if got := refs(nat.appDo(http.MethodGet, "/v1/cooperatives?confirmation=confirmed", nil), "ref"); !slices.Equal(got, []string{tr.c1.String()}) {
		t.Fatalf("lọc Đã xác nhận: %v", got)
	}
	if r := nat.appDo(http.MethodGet, "/v1/cooperatives?confirmation=maybe", nil); r.status != http.StatusUnprocessableEntity {
		t.Fatalf("bộ lọc lạ: %d", r.status)
	}
	one := nat.appDo(http.MethodGet, "/v1/cooperatives/"+tr.c2.String(), nil)
	if one.body["confirmation_label"] != "Chưa xác nhận" {
		t.Fatalf("chi tiết HTX tạm: %v", one.body)
	}
}
