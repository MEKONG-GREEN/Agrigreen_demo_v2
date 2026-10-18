package assembly

import "testing"

// /healthz trả commit của image khi AG_BUILD_SHA là mã hex hợp lệ; giá trị lạ không được phản chiếu.
func TestHealthBodyCommit(t *testing.T) {
	sha := "0123456789abcdef0123456789abcdef01234567"
	if b := healthBody(sha); b["commit"] != sha || b["status"] != "ok" {
		t.Fatalf("%v", b)
	}
	for _, v := range []string{"", "main", "<script>", "0123456789ABCDEF0123456789ABCDEF01234567", sha + "0"} {
		if b := healthBody(v); b["commit"] != nil {
			t.Fatalf("%q: không được trả commit: %v", v, b)
		}
	}
}
