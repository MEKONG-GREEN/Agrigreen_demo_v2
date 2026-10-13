// Package domain: quy tắc thuần của neo HTX theo mã số thuế (D2 — CEO 13/10/2026, ADR-0052).
//
// Neo HTX (htx_anchor) là định danh ỔN ĐỊNH của một HTX trong pf/cooperative, KHÔNG phải tài khoản: dữ liệu (hộ, xã
// viên, thửa, vụ, nhật ký) gắn Nút HTX, Nút gắn neo; neo khóa theo mã số thuế (MST) đã chuẩn hóa — duy nhất trong
// tenant lưu ký. HTX chưa có MST được neo bằng MÃ TẠM (TMP-…) và gắn MST sau (sự kiện mst_attached); nhận quyền
// (claim) bắt buộc có MST.
package domain

import (
	"errors"
	"strings"

	"github.com/google/uuid"
)

// ErrMSTMalformed: MST không đúng dạng sau khi chuẩn hóa.
var ErrMSTMalformed = errors.New("anchor: mã số thuế không đúng dạng (10 chữ số, hoặc 10 chữ số + mã chi nhánh 3 chữ số 001–999)")

// NormalizeMST chuẩn hóa mã số thuế về dạng chính tắc:
//
//   - bỏ mọi khoảng trắng và dấu gạch ngang ("0312 345 678", "0312-345-678-001");
//   - còn 10 chữ số → "NNNNNNNNNN" (đơn vị chính);
//   - còn 13 chữ số → "NNNNNNNNNN-NNN" (đơn vị phụ thuộc / chi nhánh, Luật Quản lý thuế: 10 + 3); hậu tố "000" bị từ
//     chối (không có chi nhánh số 000 — nhập "…000" thường là gõ nhầm mã đơn vị chính);
//   - mọi ký tự khác (chữ, dấu chấm, độ dài khác) → ErrMSTMalformed.
//
// Chi nhánh là NEO RIÊNG (khóa đầy đủ 13 số), không gộp vào neo đơn vị chính: chi nhánh có MST riêng, có thể ký thỏa
// thuận riêng. RootMST cho biết đơn vị chính để tra cứu. Không kiểm số kiểm tra (chữ số thứ 10) — ghi giả định trong
// ADR-0052 (dữ liệu UAT có MST không qua công thức); bật kiểm khi có nguồn tra cứu chính thức.
func NormalizeMST(raw string) (string, error) {
	var b strings.Builder
	for _, r := range raw {
		switch {
		case r == ' ' || r == '\t' || r == '-' || r == ' ':
			continue
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			return "", ErrMSTMalformed
		}
	}
	s := b.String()
	switch len(s) {
	case 10:
		return s, nil
	case 13:
		if s[10:] == "000" {
			return "", ErrMSTMalformed
		}
		return s[:10] + "-" + s[10:], nil
	}
	return "", ErrMSTMalformed
}

// RootMST trả 10 chữ số đơn vị chính của MST chính tắc.
func RootMST(canonical string) string {
	if len(canonical) >= 10 {
		return canonical[:10]
	}
	return canonical
}

// IsBranch cho biết MST chính tắc là của chi nhánh.
func IsBranch(canonical string) bool { return len(canonical) == 14 }

// TempCode là mã tạm của neo chưa có MST: "TMP-" + 12 ký tự hex HOA cuối của mã neo (UUIDv7 — phần ngẫu nhiên).
func TempCode(anchorID uuid.UUID) string {
	h := strings.ToUpper(strings.ReplaceAll(anchorID.String(), "-", ""))
	return "TMP-" + h[len(h)-12:]
}

// Trạng thái neo.
const (
	StatusProvisional = "provisional" // HTX tạm do KN/MKG lập — "Chưa xác nhận"
	StatusClaimed     = "claimed"     // HTX thật đã nhận quyền, MST đã xác minh — "Đã xác nhận"
)

// Bên lập HTX tạm.
const (
	CreatedByKN  = "kn"
	CreatedByMKG = "mkg"
)

// Trạng thái xác nhận trên mọi đường đọc cấp báo cáo (quy tắc người dùng 13/10/2026): HTX tạm vẫn được ghi, dữ
// liệu vẫn vào mọi báo cáo và TÍNH VÀO TỔNG mặc định, mang nhãn "Chưa xác nhận" tới khi HTX thật nhận quyền.
const (
	ConfirmationUnconfirmed = "unconfirmed"
	ConfirmationConfirmed   = "confirmed"
	LabelUnconfirmed        = "Chưa xác nhận"
	LabelConfirmed          = "Đã xác nhận"
)

// Confirmation đổi cờ đã nhận quyền thành mã trạng thái xác nhận.
func Confirmation(claimed bool) string {
	if claimed {
		return ConfirmationConfirmed
	}
	return ConfirmationUnconfirmed
}

// ConfirmationLabel trả nhãn hiển thị của mã trạng thái xác nhận.
func ConfirmationLabel(status string) string {
	if status == ConfirmationConfirmed {
		return LabelConfirmed
	}
	return LabelUnconfirmed
}

// ValidConfirmationFilter kiểm giá trị bộ lọc ?confirmation= (rỗng = không lọc).
func ValidConfirmationFilter(s string) bool {
	return s == "" || s == ConfirmationConfirmed || s == ConfirmationUnconfirmed
}
