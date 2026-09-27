package domain

import (
	"testing"

	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
)

// MST-01: mã số thuế 10 hoặc 13 chữ số.
func TestNormalizeTaxCode(t *testing.T) {
	cases := map[string]string{"0301234567": "0301234567", "030-123-4567": "0301234567", "0301234567001": "0301234567-001"}
	for in, want := range cases {
		if got, err := NormalizeTaxCode(in); err != nil || got != want {
			t.Errorf("%q → %q, %v; muốn %q", in, got, err, want)
		}
	}
	for _, bad := range []string{"", "12345", "03012345678", "03012345AB"} {
		if _, err := NormalizeTaxCode(bad); err == nil {
			t.Errorf("%q phải bị từ chối", bad)
		}
	}
}

// US-HTXSTATUS-01 AC-3: đếm ký tự (rune), không đếm byte.
func TestDeactivateReasonCountsRunes(t *testing.T) {
	if ValidateDeactivateReason("Giải thể theo QĐ 12") == nil { // 19 ký tự, > 20 byte
		t.Fatal("19 ký tự phải bị từ chối")
	}
	if err := ValidateDeactivateReason("Giải thể theo QĐ 123"); err != nil {
		t.Fatalf("20 ký tự phải hợp lệ: %v", err)
	}
	if ValidateDeactivateReason("   Giải thể theo QĐ 12   ") == nil {
		t.Fatal("khoảng trắng đầu cuối không được tính")
	}
}

// KN US-ADM-07: cây 3 cấp TW → Tỉnh → Xã.
func TestAllowedParent(t *testing.T) {
	k := func(x port.OrgKind) *port.OrgKind { return &x }
	ok := []struct {
		child  port.OrgKind
		parent *port.OrgKind
	}{
		{port.OrgTTKNNational, nil}, {port.OrgTTKNProvince, k(port.OrgTTKNNational)},
		{port.OrgExtensionTeam, k(port.OrgTTKNProvince)}, {port.OrgCooperative, k(port.OrgExtensionTeam)}, {port.OrgCooperative, nil},
	}
	for _, c := range ok {
		if !AllowedParent(c.child, c.parent) {
			t.Errorf("%s dưới %v phải hợp lệ", c.child, c.parent)
		}
	}
	bad := []struct {
		child  port.OrgKind
		parent *port.OrgKind
	}{
		{port.OrgTTKNNational, k(port.OrgTTKNProvince)}, {port.OrgExtensionTeam, nil},
		{port.OrgExtensionTeam, k(port.OrgTTKNNational)}, {port.OrgCooperative, k(port.OrgCooperative)},
	}
	for _, c := range bad {
		if AllowedParent(c.child, c.parent) {
			t.Errorf("%s dưới %v phải bị chặn", c.child, c.parent)
		}
	}
}

func TestValidArea(t *testing.T) {
	for s, want := range map[string]bool{"0": true, "12.3456": true, "12.34567": false, "-1": false} {
		if got := ValidArea(decimal.RequireFromString(s)); got != want {
			t.Errorf("%s: %v", s, got)
		}
	}
}
