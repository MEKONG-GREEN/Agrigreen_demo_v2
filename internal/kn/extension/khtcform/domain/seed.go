package domain

// Mã phòng nhập (khớp party/port Dept* — domain không import module khác).
const (
	DeptTT   = "trong_trot"
	DeptCNTS = "chan_nuoi_thuy_san"
	DeptTTTT = "thong_tin_truyen_thong"
	DeptHCTH = "hanh_chinh_tong_hop"
)

// Mẫu phiếu nạp sẵn (G1 — W11.63 rút gọn, Bảng FN-PN-03.02 "Phạm vi phiếu theo phòng"). Admin thiết kế mẫu mới trên màn
// Quản lý mẫu dùng chung và "So sánh lần gửi" để sau G1 (docs/kn/gia-dinh/W11.63.md). Đổi bố cục = phiên bản mới của
// cùng mã (Version + 1) — bảng số liệu đã lưu ghi mã + phiên bản mẫu đã dùng.

// gridColumns là lưới hạng mục chung của phiếu tuần (BRD Hình 22, 23: cột G "Điều chỉnh kỳ trước" cạnh cột F "Thực hiện
// trong kỳ"). Dòng = hạng mục phòng phụ trách trong Kế hoạch năm hiệu lực (BR-DL-21), không nhập tay.
func gridColumns() []Column {
	return []Column{
		{Letter: "A", Title: "STT", Kind: ColFixed, DType: DTypeText, Source: SrcRowNo},
		{Letter: "B", Title: "Hạng mục", Kind: ColFixed, DType: DTypeText, Source: SrcEntryName},
		{Letter: "C", Title: "ĐVT", Kind: ColFromPlan, DType: DTypeText, Source: SrcUnit},
		{Letter: "D", Title: "Khối lượng KH năm", Kind: ColFromPlan, DType: DTypeNumber, Source: SrcPlannedQty,
			Note: "Phiên bản Kế hoạch năm có ngày hiệu lực ≤ ngày chốt của kỳ (BR-DL-13)"},
		{Letter: "E", Title: "Lũy kế kỳ trước", Kind: ColPriorCumulative, DType: DTypeNumber,
			Note: "Lũy kế đến kỳ KHTC đã đóng gần nhất cùng năm kế hoạch (BR-DL-05)"},
		{Letter: "F", Title: "Thực hiện trong kỳ", Kind: ColInput, DType: DTypeNumber, Input: InDone, NonNegative: true,
			Note: "Số phát sinh thật của kỳ; không nhập lũy kế, không gồm điều chỉnh"},
		{Letter: "G", Title: "Điều chỉnh kỳ trước", Kind: ColFormula, DType: DTypeNumber, Formula: FxAdjust,
			Note: "Tổng khối \"Điều chỉnh kỳ trước\" cùng năm kế hoạch (BR-DL-12)"},
		{Letter: "H", Title: "Lũy kế", Kind: ColFormula, DType: DTypeNumber, Formula: FxCumulative, Note: "E + F + G"},
		{Letter: "I", Title: "% KH", Kind: ColFormula, DType: DTypeNumber, Formula: FxPercent, Unit: "%", Note: "H / D × 100"},
		{Letter: "J", Title: "Tình trạng", Kind: ColInput, DType: DTypeText, Input: InStatus},
		{Letter: "K", Title: "Nội dung, xã/điểm thực hiện", Kind: ColInput, DType: DTypeText, Input: InContent},
		{Letter: "L", Title: "Số người tham gia", Kind: ColInput, DType: DTypeInteger, Input: InPeople, NonNegative: true},
		{Letter: "M", Title: "Kinh phí", Kind: ColInput, DType: DTypeNumber, Input: InBudget, NonNegative: true, Unit: "đồng"},
	}
}

const gridRequirements = "Cột F nhập số phát sinh thật của kỳ (không nhập lũy kế, không số âm); bỏ trống tính là 0. " +
	"Số bù hoặc trừ cho kỳ đã khóa nhập ở khối \"Điều chỉnh kỳ trước\" kèm kỳ được điều chỉnh và lý do. " +
	"Hạng mục lấy từ module không có ô nhập."

func commonSections(first string) []Section {
	return []Section{
		{Key: "cong_tac_khac", Title: first, Required: true},
		{Key: "kho_khan", Title: "Khó khăn, vướng mắc, đề xuất"},
		{Key: "nhiem_vu_ky_toi", Title: "Nhiệm vụ kỳ tới"},
	}
}

// Seeds trả 4 mẫu phiếu nạp sẵn (một mẫu một phòng nhập) và mẫu chung cho phòng khác.
func Seeds() []Template {
	return []Template{
		{Code: "KH-TTTT", Version: 1, Department: DeptTTTT, Title: "Phiếu Phòng Thông tin tuyên truyền",
			Requirements: gridRequirements + " Số phiếu giá cả đã thu thập do hệ thống điền từ module Báo giá.",
			Columns:      gridColumns(), Sections: commonSections("Công tác khác (thông tin, tuyên truyền)")},
		{Code: "KH-TT", Version: 1, Department: DeptTT, Title: "Phiếu Phòng Trồng trọt",
			Requirements: gridRequirements + " Số liệu sản xuất (A.I.1) lấy từ module Trồng trọt, không nhập.",
			Columns:      gridColumns(), Sections: commonSections("Công tác khác; CT, DA năm trước; hợp tác, đề tài phòng phụ trách")},
		{Code: "KH-CNTS", Version: 1, Department: DeptCNTS, Title: "Phiếu Phòng Chăn nuôi - Thủy sản",
			Requirements: gridRequirements + " Tổng đàn, sản lượng thủy sản (A.I.2, A.I.3) lấy từ module CNTS, không nhập.",
			Columns:      gridColumns(), Sections: commonSections("Công tác khác; dự án TW, hợp tác, đề tài, dịch vụ tư vấn")},
		{Code: "KH-HCTH", Version: 1, Department: DeptHCTH, Title: "Phiếu Phòng Hành chính - Tổng hợp",
			Requirements: "Phòng HCTH không có tab chương trình; nhập lời văn từng mục.",
			Sections: []Section{
				{Key: "cchc", Title: "Cải cách hành chính, tổ chức cán bộ", Required: true},
				{Key: "nhiem_vu_cchc", Title: "Nhiệm vụ kỳ tới về CCHC"},
				{Key: "kho_khan", Title: "Khó khăn, vướng mắc, đề xuất"},
			}},
	}
}

// Generic là mẫu chung cho phòng tham gia chưa có mẫu riêng.
func Generic() Template {
	return Template{Code: "KH-CHUNG", Version: 1, Title: "Phiếu báo cáo cho KHTC", Requirements: gridRequirements,
		Columns: gridColumns(), Sections: commonSections("Kết quả thực hiện, công tác khác")}
}

// TemplateFor trả mẫu phiếu của phòng nhập (mẫu chung nếu phòng chưa có mẫu riêng).
func TemplateFor(department string) Template {
	for _, t := range Seeds() {
		if t.Department == department {
			return t
		}
	}
	return Generic()
}
