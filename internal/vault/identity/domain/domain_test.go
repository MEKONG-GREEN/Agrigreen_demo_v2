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
	if got := MaskPhone("84912345123"); got != "0912•••123" {
		t.Fatalf("MaskPhone = %q", got)
	}
}

// M13: mục đích đúng dạng nhưng ngoài danh mục của thao tác bị từ chối.
func TestAllowedPurpose(t *testing.T) {
	ok := [][2]string{
		{OpLookup, "registry.dedup"}, {OpLookup, "registry.admin"}, {OpResolve, "extension.contact"},
		{OpUnwrap, "mrv.evidence"}, {OpDisplay, "mechanization.owner"}, {OpErase, "registry.erasure"},
		{OpCreate, "mechanization.owner"}, {OpUnwrap, "hr_records"}, // W11.24 tài liệu nhân sự
		{OpUnwrap, "extension.diary"},                                                                    // W11.48 ảnh nhật ký khuyến nông
		{OpCreate, PurposePriceSurvey}, {OpResolve, PurposePriceSurvey}, {OpDisplay, PurposePriceSurvey}, // W11.78 điều tra giá thành
	}
	for _, c := range ok {
		if !AllowedPurpose(c[0], c[1]) {
			t.Errorf("%s/%s phải được phép", c[0], c[1])
		}
	}
	bad := [][2]string{
		{OpLookup, "extension.contact"}, {OpLookup, "mrv.evidence"}, {OpResolve, "mrv.evidence"},
		{OpResolve, "marketing.campaign"}, {OpUnwrap, "registry.admin"}, {"khac", "registry.admin"},
		{OpResolve, "hr_records"}, {OpDisplay, "hr_records"}, {OpCreate, "hr_records"},
		{OpResolve, "extension.diary"}, {OpDisplay, "extension.diary"},
		{OpLookup, PurposePriceSurvey}, {OpUnwrap, PurposePriceSurvey}, // W11.78: không dò theo SĐT, không mở tệp
	}
	for _, c := range bad {
		if AllowedPurpose(c[0], c[1]) {
			t.Errorf("%s/%s phải bị từ chối", c[0], c[1])
		}
	}
	if got := AllowedPurposes(OpLookup); len(got) != 3 || got[0] != PurposeHRMProfile || got[1] != "registry.admin" {
		t.Errorf("AllowedPurposes(lookup) = %v", got)
	}
}

// W11.22: ngăn gắn cứng mục đích; hrm.sensitive chỉ dùng cho ngăn, không đọc định danh; trường lạ/quá dài bị từ chối.
func TestSections(t *testing.T) {
	if p, ok := SectionPurpose(SectionHRMSensitive); !ok || p != PurposeHRMSensitive {
		t.Fatalf("ngăn nhạy cảm: %q %v", p, ok)
	}
	if p, ok := SectionPurpose(SectionHRMProfile); !ok || p != PurposeHRMProfile {
		t.Fatalf("ngăn hồ sơ: %q %v", p, ok)
	}
	if _, ok := SectionPurpose("hrm.salary"); ok {
		t.Fatal("ngăn lạ")
	}
	for _, op := range []string{OpCreate, OpResolve, OpDisplay, OpLookup, OpAmend, OpUnwrap} {
		if AllowedPurpose(op, PurposeHRMSensitive) {
			t.Errorf("%s không được nhận mục đích hrm.sensitive", op)
		}
	}
	if !AllowedPurpose(OpSectionRead, PurposeHRMSensitive) || AllowedPurpose(OpSectionRead, "registry.admin") {
		t.Error("đọc ngăn: chỉ mục đích hồ sơ nhân sự")
	}
	if err := ValidSectionFields(SectionHRMSensitive, map[string]string{"ethnicity": "kinh", "hometown": "Xã Mỹ Hòa Hưng"}); err != nil {
		t.Fatal(err)
	}
	if err := ValidSectionFields(SectionHRMSensitive, map[string]string{"email": "a@b.vn"}); err == nil {
		t.Error("trường của ngăn khác phải bị từ chối")
	}
	if err := ValidSectionFields(SectionHRMProfile, map[string]string{"gender": "khong_ro_rang"}); err == nil {
		t.Error("trường quá dài phải bị từ chối")
	}
}
