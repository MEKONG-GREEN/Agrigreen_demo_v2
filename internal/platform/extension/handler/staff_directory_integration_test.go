package handler_test

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
)

// port.StaffDirectory (cho pf/notify): trả mã tham chiếu + đơn vị của cán bộ đang
// hoạt động; mã tỉnh khớp mọi cán bộ của tỉnh; cách ly tenant.
func TestStaffDirectoryPort(t *testing.T) {
	f := newFixture(t)
	p := f.seedPeople()
	dir := f.mod.Staff()
	ctx := context.Background()

	got, err := dir.ActiveStaff(ctx, f.tenant, []string{"30001"})
	if err != nil {
		t.Fatal(err)
	}
	want := map[uuid.UUID]bool{p.off30001a.actor: true, p.off30001b.actor: true}
	if len(got) != 2 || !want[got[0].ActorRef] || !want[got[1].ActorRef] || got[0].ProvinceLevel() {
		t.Fatalf("xã 30001: %+v", got)
	}
	prov, err := dir.ActiveStaff(ctx, f.tenant, []string{"89"})
	if err != nil {
		t.Fatal(err)
	}
	levels := 0
	for _, s := range prov {
		if s.ProvinceLevel() {
			levels++
			if s.ActorRef != p.admin89.actor {
				t.Fatalf("cán bộ cấp tỉnh sai: %+v", s)
			}
		}
	}
	if len(prov) != 4 || levels != 1 {
		t.Fatalf("tỉnh 89: %d cán bộ, %d cấp tỉnh", len(prov), levels)
	}
	if other, _ := dir.ActiveStaff(ctx, ids.New(), []string{"30001", "89"}); len(other) != 0 {
		t.Fatal("tenant khác không được thấy danh bạ")
	}
	if none, _ := dir.ActiveStaff(ctx, f.tenant, nil); len(none) != 0 {
		t.Fatal("không có đơn vị → rỗng")
	}
}
