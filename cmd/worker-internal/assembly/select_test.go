package assembly

import "testing"

// A-3 (RB-5): AG_WORKER_INTERNAL_CONSUMERS chọn consumer bằng cấu hình — all / rỗng = mọi consumer; tên lạ, lặp,
// danh sách rỗng → lỗi khởi động.
func TestSelectConsumersByConfig_A3(t *testing.T) {
	for _, v := range []string{"", "all", "  all "} {
		if cs, err := Select(v); err != nil || len(cs) != len(Consumers) {
			t.Fatalf("%q: %d %v", v, len(cs), err)
		}
	}
	cs, err := Select(" mkg.costing.period_close , mkg.acctbridge.journal_build")
	if err != nil || len(cs) != 2 || cs[0].Name != "mkg.costing.period_close" || cs[1].Name != "mkg.acctbridge.journal_build" {
		t.Fatalf("chọn hai consumer: %+v %v", cs, err)
	}
	for _, bad := range []string{"mkg.x.y", "mkg.costing.trip,mkg.costing.trip", ",", "mkg.costing.trip,mkg.nope.z"} {
		if _, err := Select(bad); err == nil {
			t.Errorf("%q phải lỗi", bad)
		}
	}
}
