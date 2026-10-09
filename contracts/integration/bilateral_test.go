package integration

import (
	"slices"
	"testing"
)

// W5.6 (ADR-0034): khóa nghiệp vụ, khóa cặp, băm JSON chuẩn dùng chung cho hai bên của cặp.
func TestBilateralHelpers_W56(t *testing.T) {
	c, err := Get("roll_count_statement", 1)
	if err != nil {
		t.Fatal(err)
	}
	a := []byte(`{"statement_ref":"0199c8a0-0000-7000-8000-000000000001","revision":2,"htx_ref":"0199c8a0-0000-7000-8000-0000000000aa","rolls":5}`)
	b := []byte("{ \"rolls\": 5, \"htx_ref\": \"0199c8a0-0000-7000-8000-0000000000aa\",\n \"revision\": 2, \"statement_ref\": \"0199c8a0-0000-7000-8000-000000000001\" }")
	if k, err := c.BusinessKeyOf(a); err != nil || k != "0199c8a0-0000-7000-8000-000000000001:2" {
		t.Fatalf("business_key: %q %v", k, err)
	}
	if k, err := c.PairKeyOf(a); err != nil || k != "0199c8a0-0000-7000-8000-0000000000aa" {
		t.Fatalf("pair_key: %q %v", k, err)
	}
	sa, _ := PayloadSHA256(a)
	sb, _ := PayloadSHA256(b)
	if sa != sb || len(sa) != 64 {
		t.Fatalf("băm JSON chuẩn không phụ thuộc định dạng: %s %s", sa, sb)
	}
	if got := BilateralContracts(); !slices.Equal(got, []string{"collection_schedule", "htx_roll_confirmation", "roll_count_statement"}) {
		t.Fatalf("hợp đồng song phương: %v", got)
	}
	if n, s := BilateralDayDigest(nil); n != 0 || s != "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855" {
		t.Fatalf("ngày rỗng: %d %s", n, s)
	}
}
