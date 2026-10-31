package domain

import (
	"slices"
	"strings"
	"unicode/utf8"
)

// AppraisalColumn là một cột của biểu giấy MS05/06. Điểm phiếu không thuộc biểu này (DG11/12).
type AppraisalColumn struct {
	Key   string `json:"key"`
	Title string `json:"title"`
}

// AppraisalSummaryTemplate có phiên bản ở sổ; mỗi đợt giữ một bản sao trọn vẹn.
// Kind giữ quy tắc chia MS05/MS06; Code/Number cho phép nhân bản khi Sở ban hành biểu mới.
type AppraisalSummaryTemplate struct {
	Code      string            `json:"code"`
	Kind      string            `json:"kind"`
	Number    string            `json:"number"`
	Name      string            `json:"name"`
	Title     string            `json:"title"`
	Audience  string            `json:"audience"`
	Signature string            `json:"signature"`
	Columns   []AppraisalColumn `json:"columns"`
	Ratings   map[string]string `json:"ratings"`
}

// DefaultAppraisalSummary trả bản mới mỗi lần; không chia sẻ slice/map giữa mẫu hoặc giữa request.
func DefaultAppraisalSummary(kind string) AppraisalSummaryTemplate {
	code, number, audience := "ms05", "05", "Viên chức không giữ chức vụ lãnh đạo, quản lý và cán bộ khuyến nông cấp xã"
	if kind == "MS06" {
		code, number, audience = "ms06", "06", "Cán bộ lãnh đạo, quản lý thuộc Trung tâm Khuyến nông"
	}
	return AppraisalSummaryTemplate{
		Code: code, Kind: kind, Number: number,
		Name:      "Mẫu số " + number + " · Tổng hợp kết quả đánh giá và đề xuất xếp loại",
		Title:     "TỔNG HỢP KẾT QUẢ ĐÁNH GIÁ VÀ ĐỀ XUẤT XẾP LOẠI QUÝ {QUY}, NĂM {NAM}",
		Audience:  audience,
		Signature: "XÁC NHẬN TẬP THỂ LÃNH ĐẠO CƠ QUAN (Ký, ghi rõ họ tên và đóng dấu)",
		Columns: []AppraisalColumn{
			{"ordinal", "STT"}, {"staff", "Họ và tên"}, {"position_unit", "Chức vụ, đơn vị công tác"},
			{"self_rating", "Cá nhân tự đề xuất mức xếp loại"}, {"collective_rating", "Tập thể đề xuất mức xếp loại"},
			{"reason", "Tóm tắt căn cứ, cơ sở lý do"}, {"personnel_proposal", "Đề xuất nội dung liên quan về công tác cán bộ"}, {"note", "Ghi chú"},
		},
		Ratings: map[string]string{
			"HTXSNV": "Hoàn thành xuất sắc nhiệm vụ", "HTTNV": "Hoàn thành tốt nhiệm vụ",
			"HTNV": "Hoàn thành nhiệm vụ", "KHTNV": "Không hoàn thành nhiệm vụ",
		},
	}
}

func ValidateAppraisalSummary(in AppraisalSummaryTemplate) (AppraisalSummaryTemplate, error) {
	t := in
	t.Columns = slices.Clone(in.Columns)
	t.Ratings = make(map[string]string, len(in.Ratings))
	for code, label := range in.Ratings {
		t.Ratings[code] = label
	}
	if !appraisalCodeSyntax.MatchString(t.Code) {
		return t, appraisalInvalid("code", "mã mẫu gồm chữ thường, số, _, bắt đầu bằng chữ; tối đa 40 ký tự")
	}
	if t.Kind != "MS05" && t.Kind != "MS06" {
		return t, appraisalInvalid("kind", "chọn họ mẫu MS05 hoặc MS06")
	}
	for _, field := range []struct {
		key string
		v   *string
		max int
	}{{"number", &t.Number, 20}, {"name", &t.Name, 200}, {"title", &t.Title, 500}, {"audience", &t.Audience, 500}, {"signature", &t.Signature, 1000}} {
		*field.v = strings.TrimSpace(*field.v)
		if *field.v == "" || utf8.RuneCountInString(*field.v) > field.max {
			return t, appraisalInvalid(field.key, "nhập nội dung bắt buộc trong độ dài cho phép")
		}
	}
	defaults := DefaultAppraisalSummary(t.Kind)
	if len(t.Columns) != len(defaults.Columns) {
		return t, appraisalInvalid("columns", "MS05/MS06 phải giữ đủ 8 cột của biểu Sở, không thêm cột điểm")
	}
	keys := map[string]bool{}
	for _, c := range t.Columns {
		if keys[c.Key] || !slices.ContainsFunc(defaults.Columns, func(known AppraisalColumn) bool { return known.Key == c.Key }) || strings.TrimSpace(c.Title) == "" || utf8.RuneCountInString(c.Title) > 300 {
			return t, appraisalInvalid("columns", "mỗi cột của biểu xuất hiện đúng một lần, tiêu đề tối đa 300 ký tự")
		}
		keys[c.Key] = true
	}
	if len(t.Ratings) != len(Ratings) {
		return t, appraisalInvalid("ratings", "cần đủ bốn mức xếp loại")
	}
	for _, code := range Ratings {
		if strings.TrimSpace(t.Ratings[code]) == "" || utf8.RuneCountInString(t.Ratings[code]) > 200 {
			return t, appraisalInvalid("ratings", "nhãn bốn mức xếp loại bắt buộc, tối đa 200 ký tự")
		}
	}
	return t, nil
}
