package domain

import (
	"bytes"
	"testing"

	"github.com/google/uuid"
)

func TestNormalizePhone(t *testing.T) {
	ok := map[string]string{
		"0912345678":        "84912345678",
		"091 234 5678":      "84912345678",
		"+84 912-345-678":   "84912345678",
		"84912345678":       "84912345678",
		"0084912345678":     "84912345678",
		"(+84) 912.345.678": "84912345678",
	}
	for in, want := range ok {
		got, err := NormalizePhone(in)
		if err != nil || got != want {
			t.Errorf("NormalizePhone(%q) = %q, %v; muốn %q", in, got, err, want)
		}
	}
	for _, bad := range []string{"", "12345", "0012345678", "091234567a", "+8491234567", "079123456789"} {
		if _, err := NormalizePhone(bad); err == nil {
			t.Errorf("NormalizePhone(%q) phải lỗi", bad)
		}
	}
}

func TestNormalizeNationalID(t *testing.T) {
	if got, err := NormalizeNationalID(" 079 123-456 789 "); err != nil || got != "079123456789" {
		t.Fatalf("CCCD: %q %v", got, err)
	}
	if _, err := NormalizeNationalID("12345"); err == nil {
		t.Fatal("phải lỗi")
	}
	c := LookupCandidates("079123456789")
	if len(c) != 1 || c[0].Kind != BlindNationalID {
		t.Fatalf("CCCD 12 số chỉ là national_id: %v", c)
	}
	c = LookupCandidates("0912345678")
	if len(c) != 1 || c[0].Kind != BlindPhone {
		t.Fatalf("SĐT: %v", c)
	}
}

func TestBlindIndexPerTenant(t *testing.T) {
	key := bytes.Repeat([]byte{7}, 32)
	a, b := uuid.New(), uuid.New()
	if bytes.Equal(BlindIndex(key, a, BlindPhone, "84912345678"), BlindIndex(key, b, BlindPhone, "84912345678")) {
		t.Fatal("hai tenant phải cho hai giá trị khác nhau")
	}
	if !bytes.Equal(BlindIndex(key, a, BlindPhone, "84912345678"), BlindIndex(key, a, BlindPhone, "84912345678")) {
		t.Fatal("phải tất định")
	}
	if bytes.Equal(BlindIndex(key, a, BlindPhone, "84912345678"), BlindIndex(key, a, BlindNationalID, "84912345678")) {
		t.Fatal("loại khác phải cho giá trị khác")
	}
}

func TestLabelAndMask(t *testing.T) {
	if got := Label("  Nguyễn   Văn   an "); got != "Nguyễn V. A." {
		t.Fatalf("Label = %q", got)
	}
	if got := Label("Út"); got != "Ú." {
		t.Fatalf("Label một từ = %q", got)
	}
	if got := MaskPhone("84912345123"); got != "09xx xxx 123" {
		t.Fatalf("MaskPhone = %q", got)
	}
}
