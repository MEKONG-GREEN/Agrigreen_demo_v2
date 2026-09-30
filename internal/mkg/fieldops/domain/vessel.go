package domain

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/fieldops/port"
)

// RuleVessel: số hiệu ghe/sà lan của bản ghi xếp ghe (G4, proto FM-05) — tùy chọn.
const RuleVessel = "fieldops.vessel_code.v1"

// MaxVesselCodeLen là độ dài tối đa (ký tự) của số hiệu phương tiện đã chuẩn hóa.
const MaxVesselCodeLen = 32

// NormalizeVesselCode chuẩn hóa số hiệu phương tiện như danh mục ghe của mkg/logistics (proto GH-01): bỏ
// mọi khoảng trắng, chữ hoa. Chuỗi rỗng sau chuẩn hóa → "" (không có số hiệu).
func NormalizeVesselCode(s string) string {
	return strings.ToUpper(strings.Join(strings.Fields(s), ""))
}

// CheckVesselCode kiểm số hiệu (đã hoặc chưa chuẩn hóa) cho công đoạn stage và trả dạng chuẩn hóa.
// nil / rỗng → nil (trường tùy chọn). Chỉ công đoạn xếp ghe (boat_load) mang số hiệu; ký tự chỉ gồm chữ,
// số và . - / (số hiệu đăng kiểm VN có chữ Đ).
func CheckVesselCode(field string, code *string, stage string) (*string, error) {
	if code == nil {
		return nil, nil
	}
	n := NormalizeVesselCode(*code)
	if n == "" {
		return nil, nil
	}
	if stage != port.StageBoatLoad {
		return nil, fieldErr(field, "chỉ dùng cho công đoạn boat_load (xếp ghe)")
	}
	if utf8.RuneCountInString(n) > MaxVesselCodeLen {
		return nil, fieldErr(field, "tối đa 32 ký tự")
	}
	for _, r := range n {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '.' && r != '-' && r != '/' {
			return nil, fieldErr(field, "chỉ gồm chữ, số và . - /")
		}
	}
	return &n, nil
}
