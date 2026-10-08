package ingest

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

// W5.7 (R-INT-10): tổng kiểm ngày của luồng song phương không phụ thuộc thứ tự bản ghi, đổi khi một payload đổi;
// ngày rỗng = 0 + SHA-256 chuỗi rỗng; công thức đúng dòng "<business_key>\t<contract_version>\t<payload_sha256>\n".
func TestDayDigest_W57(t *testing.T) {
	a := PairRecord{BusinessKey: "st-1:1", ContractVersion: 1, PayloadSHA256: "aa"}
	b := PairRecord{BusinessKey: "st-2:1", ContractVersion: 1, PayloadSHA256: "bb"}
	n1, s1 := DayDigest([]PairRecord{a, b})
	n2, s2 := DayDigest([]PairRecord{b, a})
	want := sha256.Sum256([]byte("st-1:1\t1\taa\nst-2:1\t1\tbb\n"))
	if n1 != 2 || n2 != 2 || s1 != s2 || s1 != hex.EncodeToString(want[:]) {
		t.Fatalf("thứ tự / công thức: %d %s %s", n1, s1, s2)
	}
	b.PayloadSHA256 = "bc"
	if _, s3 := DayDigest([]PairRecord{a, b}); s3 == s1 {
		t.Fatal("payload đổi → tổng kiểm đổi")
	}
	empty := sha256.Sum256(nil)
	if n, s := DayDigest(nil); n != 0 || s != hex.EncodeToString(empty[:]) {
		t.Fatalf("ngày rỗng: %d %s", n, s)
	}
}
