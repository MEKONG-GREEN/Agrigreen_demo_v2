package domain

import (
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestNormalizeMST_D2(t *testing.T) {
	ok := map[string]string{
		"0312345678":         "0312345678",
		" 0312 345 678 ":     "0312345678",
		"0312-345-678":       "0312345678",
		"0312345678-001":     "0312345678-001", // chi nhánh
		"0312345678001":      "0312345678-001", // chi nhánh không gạch
		"0312 345 678 - 002": "0312345678-002",
	}
	for in, want := range ok {
		got, err := NormalizeMST(in)
		if err != nil || got != want {
			t.Errorf("NormalizeMST(%q) = %q, %v; muốn %q", in, got, err, want)
		}
	}
	bad := []string{"", "031234567", "03123456789", "0312345678-000", "0312345678000", "03123A5678", "0312.345.678",
		"0312345678-01", "03123456780012", "MST0312345678"}
	for _, in := range bad {
		if got, err := NormalizeMST(in); !errors.Is(err, ErrMSTMalformed) {
			t.Errorf("NormalizeMST(%q) = %q, %v; muốn ErrMSTMalformed", in, got, err)
		}
	}
}

func TestBranchSuffix_D2(t *testing.T) {
	c, _ := NormalizeMST("0312345678001")
	if !IsBranch(c) || RootMST(c) != "0312345678" {
		t.Fatalf("chi nhánh %q: IsBranch=%v Root=%q", c, IsBranch(c), RootMST(c))
	}
	m, _ := NormalizeMST("0312345678")
	if IsBranch(m) || RootMST(m) != m {
		t.Fatalf("đơn vị chính %q", m)
	}
	if c == m {
		t.Fatal("chi nhánh và đơn vị chính phải là hai khóa neo khác nhau")
	}
}

func TestTempCodeAndConfirmation_D2(t *testing.T) {
	id := uuid.MustParse("01929a3b-1c2d-7e3f-8a4b-5c6d7e8f9a0b")
	if got := TempCode(id); got != "TMP-5C6D7E8F9A0B" || !strings.HasPrefix(got, "TMP-") {
		t.Fatalf("TempCode = %q", got)
	}
	if Confirmation(false) != ConfirmationUnconfirmed || ConfirmationLabel(Confirmation(false)) != "Chưa xác nhận" {
		t.Fatal("HTX tạm phải là Chưa xác nhận")
	}
	if Confirmation(true) != ConfirmationConfirmed || ConfirmationLabel(ConfirmationConfirmed) != "Đã xác nhận" {
		t.Fatal("sau nhận quyền phải là Đã xác nhận")
	}
	if !ValidConfirmationFilter("") || ValidConfirmationFilter("pending") {
		t.Fatal("bộ lọc xác nhận")
	}
}
