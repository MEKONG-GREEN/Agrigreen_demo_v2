package domain

import "unicode/utf8"

// Phản hồi của HTX với bảng kê số cuộn MKG (RB-6 chiều mua, G3-2). Quy tắc giống
// mkg_fieldops.htx_confirmation để MKG ghi lại được nguyên văn:
//   - confirmed: số HTX = số MKG trên bảng kê (htx_rolls bỏ trống hoặc bằng số MKG);
//   - disputed : bắt buộc số HTX đếm (≥ 0, KHÁC số MKG) và lý do 10–500 ký tự;
//   - thay phản hồi trước cho CÙNG revision: bắt buộc lý do thay (10–500 ký tự).
//
// Lý do là chữ tự do gửi sang MKG: không được chứa chuỗi số dạng SĐT/CCCD (R-04).

// RuleCollectionResponse là phiên bản quy tắc ghi trên sổ phản hồi.
const RuleCollectionResponse = "cropcycle.collection_response/v1"

// RuleCollectionValve là phiên bản quy tắc ghi trên sổ nhận van.
const RuleCollectionValve = "cropcycle.collection_valve/v1"

// Giới hạn lý do.
const (
	MinResponseReason = 10
	MaxResponseReason = 500
)

// Thông báo dùng chung với App HTX / web.
const (
	MsgDisputeRollsRequired = "Tranh chấp cần nhập số cuộn HTX đếm được (≥ 0)."
	MsgDisputeSameRolls     = "Số cuộn HTX bằng số MKG — hãy chọn Xác nhận."
	MsgConfirmOtherRolls    = "Số cuộn khác số MKG — hãy chọn Tranh chấp và ghi lý do."
	MsgReasonLength         = "Lý do cần từ 10 đến 500 ký tự."
	MsgSupersedeReason      = "Đã có phản hồi cho bảng kê này — ghi lý do thay đổi (10–500 ký tự)."
)

// Response là phản hồi đã chuẩn hóa.
type Response struct {
	Outcome  string
	HTXRolls int
	Reason   string // rỗng khi không có
}

// CheckResponse kiểm một phản hồi với số cuộn MKG của bảng kê. superseding = đã có phản
// hồi cho cùng revision (bản mới thay bản cũ — lý do bắt buộc).
func CheckResponse(outcome string, mkgRolls int, htxRolls *int, reason string, superseding bool) (Response, error) {
	r := Response{Outcome: outcome}
	s, err := CleanText("reason", reason, MaxResponseReason)
	if err != nil {
		return r, err
	}
	if s != "" && ContainsPII(s) {
		return r, FieldError{"reason", "không được chứa số điện thoại hay số CCCD (dữ liệu cá nhân chỉ ở kho định danh)"}
	}
	if s != "" && utf8.RuneCountInString(s) < MinResponseReason {
		return r, FieldError{"reason", MsgReasonLength}
	}
	r.Reason = s
	switch outcome {
	case "confirmed":
		if htxRolls != nil && *htxRolls != mkgRolls {
			return r, FieldError{"htx_rolls", MsgConfirmOtherRolls}
		}
		r.HTXRolls = mkgRolls
	case "disputed":
		if htxRolls == nil || *htxRolls < 0 {
			return r, FieldError{"htx_rolls", MsgDisputeRollsRequired}
		}
		if *htxRolls == mkgRolls {
			return r, FieldError{"htx_rolls", MsgDisputeSameRolls}
		}
		if s == "" {
			return r, FieldError{"reason", MsgReasonLength}
		}
		r.HTXRolls = *htxRolls
	default:
		return r, FieldError{"outcome", "phải là confirmed hoặc disputed"}
	}
	if superseding && s == "" {
		return r, FieldError{"reason", MsgSupersedeReason}
	}
	return r, nil
}
