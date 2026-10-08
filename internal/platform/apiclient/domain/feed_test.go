package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"
)

func TestCursorRoundTrip(t *testing.T) {
	for _, n := range []int64{0, 1, 4120, 1 << 40} {
		c := EncodeCursor(n)
		got, err := DecodeCursor(c)
		if err != nil || got != n {
			t.Fatalf("cursor %d → %q → %d (%v)", n, c, got, err)
		}
		for _, ch := range c {
			if !strings.ContainsRune("ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789_-", ch) {
				t.Fatalf("cursor phải khớp ^[A-Za-z0-9_-]+$ của phong bì: %q", c)
			}
		}
	}
	if EncodeCursor(4120) != "c2VxOjQxMjA" {
		t.Fatalf("dạng mờ như mẫu hợp đồng: %s", EncodeCursor(4120))
	}
	for _, bad := range []string{"", "!!", "c2VxOi0x", "Zm9vOjE", "c2VxOjAx", strings.Repeat("A", 201)} {
		if _, err := DecodeCursor(bad); err == nil {
			t.Fatalf("cursor sai dạng phải lỗi: %q", bad)
		}
	}
}

// R-INT-10: tổng kiểm = số dòng + SHA-256 của chính ảnh chụp NDJSON, tất định theo entity_key.
func TestSnapshotChecksum(t *testing.T) {
	a, _ := CanonicalRecord([]byte(`{"b": 1, "a": {"y": "2", "x": null}}`))
	if string(a) != `{"a":{"x":null,"y":"2"},"b":1}` {
		t.Fatalf("JSON chuẩn sắp khóa: %s", a)
	}
	b, _ := CanonicalRecord([]byte(`{"a":1.50}`))
	if string(b) != `{"a":1.50}` {
		t.Fatalf("số giữ nguyên dạng: %s", b)
	}
	recs := []KeyedRecord{{EntityKey: "z", Record: b}, {EntityKey: "a", Record: a}}
	body, n, sum, err := Snapshot(recs)
	if err != nil || n != 2 {
		t.Fatal(err, n)
	}
	want := `{"entity_key":"a","record":{"a":{"x":null,"y":"2"},"b":1}}` + "\n" + `{"entity_key":"z","record":{"a":1.50}}` + "\n"
	if string(body) != want {
		t.Fatalf("ảnh chụp:\n%s", body)
	}
	h := sha256.Sum256(body)
	if sum != hex.EncodeToString(h[:]) {
		t.Fatal("sha256 phải là băm của tệp")
	}
	_, _, sum2, _ := Snapshot([]KeyedRecord{recs[1], recs[0]})
	if sum2 != sum {
		t.Fatal("tổng kiểm không phụ thuộc thứ tự đầu vào")
	}
	if _, _, _, err := Snapshot([]KeyedRecord{recs[0], recs[0]}); err == nil {
		t.Fatal("trùng entity_key phải lỗi")
	}
	if RecordSHA256(a) == RecordSHA256(b) || !json.Valid(body[:strings.IndexByte(string(body), '\n')]) {
		t.Fatal("băm bản ghi")
	}
	if _, n, _, _ := Snapshot(nil); n != 0 {
		t.Fatal("tập rỗng")
	}
}
