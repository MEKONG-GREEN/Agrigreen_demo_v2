package app

// Khung Word MS05/06 do HRM sở hữu. Mẫu/nhãn giấy của đợt được truyền như dữ liệu,
// không thay phiên bản ngôn ngữ mẫu đã dùng của cap/export.

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/hrm/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/doctpl"
)

const AppraisalWordOwner = "pf/hrm"

func AppraisalWordTemplate(kind string) doctpl.Template {
	t := doctpl.Template{Key: "hrm." + strings.ToLower(kind), Version: 1, Owner: AppraisalWordOwner,
		Title: "Tổng hợp đánh giá " + kind, Landscape: true,
		Vars: []doctpl.Var{{Key: "co_quan", Label: "Cơ quan"}, {Key: "don_vi", Label: "Đơn vị"},
			{Key: "so_mau", Label: "Số mẫu", Required: true}, {Key: "tieu_de", Label: "Tiêu đề", Required: true},
			{Key: "doi_tuong", Label: "Đối tượng"}, {Key: "chu_ky", Label: "Chữ ký", Required: true},
			{Key: "dau_du_lieu", Label: "Dấu dữ liệu"}},
		Lists: []doctpl.List{{Key: "dong", Label: "Danh sách đánh giá"}}}
	widths := []int{500, 1800, 2000, 1800, 1800, 2200, 2200, 1986}
	columns := []doctpl.Column{}
	for i := range 8 {
		key := "cot_" + strconv.Itoa(i)
		t.Vars = append(t.Vars, doctpl.Var{Key: key, Label: fmt.Sprintf("Tiêu đề cột %d", i+1), Required: true})
		t.Lists[0].Fields = append(t.Lists[0].Fields, key)
		columns = append(columns, doctpl.Column{Header: "{" + key + "}", Value: "{dong." + key + "}", Width: widths[i]})
	}
	t.Body = []doctpl.Block{
		{Kind: doctpl.KindColumns, Split: 6000,
			Left:  []doctpl.Line{{Text: "{co_quan}"}, {Text: "{don_vi}", Bold: true}},
			Right: []doctpl.Line{{Text: "CỘNG HÒA XÃ HỘI CHỦ NGHĨA VIỆT NAM", Bold: true}, {Text: "Độc lập - Tự do - Hạnh phúc", Bold: true}, {Text: "Mẫu số {so_mau}"}}},
		doctpl.Title("{tieu_de}"),
		{Kind: doctpl.KindParagraph, Text: "{doi_tuong}", Align: doctpl.AlignCenter, Italic: true},
		{Kind: doctpl.KindTable, Each: "dong", Columns: columns, EmptyText: "Không có người thuộc phạm vi xuất."},
		{Kind: doctpl.KindParagraph, Text: "{chu_ky}", Align: doctpl.AlignRight, Bold: true},
		{Kind: doctpl.KindParagraph, Text: "\n\n\n"},
		{Kind: doctpl.KindParagraph, Text: "Dấu dữ liệu: {dau_du_lieu}", Italic: true},
	}
	return t
}

func AppraisalWordTemplates() []doctpl.Template {
	return []doctpl.Template{AppraisalWordTemplate("MS05"), AppraisalWordTemplate("MS06")}
}

func appraisalTitle(t domain.AppraisalSummaryTemplate, p AppraisalPeriod) string {
	year := strconv.Itoa(p.Year)
	quarter := ""
	if p.Quarter > 0 {
		quarter = []string{"", "I", "II", "III", "IV"}[p.Quarter]
	}
	title := strings.NewReplacer("{NAM}", year, "{QUY}", quarter).Replace(t.Title)
	if p.Quarter == 0 {
		title = strings.ReplaceAll(title, "QUÝ , ", "")
	}
	return title
}

// appraisalPaperCells không thêm điểm hoặc kết luận vào tám cột giấy. Điểm chỉ gợi ý
// DG04/05; kết luận chính thức được giữ ở sổ và hiển thị riêng khi tra cứu.
func appraisalPaperCells(x AppraisalItem, ordinal int, t domain.AppraisalSummaryTemplate) []string {
	if t.Kind == AppraisalScores {
		return appraisalScoreCells(x, ordinal, t)
	}
	values := map[string]string{"ordinal": strconv.Itoa(ordinal), "staff": x.Name,
		"position_unit": strings.Trim(strings.Join([]string{x.Member.PositionName, x.Member.UnitName}, ", "), ", "),
		"self_rating":   t.Ratings[x.SelfRating], "collective_rating": t.Ratings[x.CollectiveRating],
		"reason": x.Reason, "personnel_proposal": x.PersonnelProposal, "note": x.Note}
	if values["staff"] == "" {
		values["staff"] = x.Member.Code
	}
	out := make([]string, 0, 8)
	for _, c := range t.Columns {
		out = append(out, values[c.Key])
	}
	return out
}

func appraisalWordData(t domain.AppraisalSummaryTemplate, p AppraisalPeriod, items []AppraisalItem, stamp string) doctpl.Data {
	d := doctpl.Data{Vars: map[string]string{"co_quan": defaultAgency, "don_vi": defaultOrg,
		"so_mau": t.Number, "tieu_de": appraisalTitle(t, p), "doi_tuong": t.Audience, "chu_ky": t.Signature, "dau_du_lieu": stamp}, Lists: map[string][]doctpl.Item{"dong": {}}}
	for i, c := range t.Columns {
		d.Vars["cot_"+strconv.Itoa(i)] = c.Title
	}
	for i, x := range items {
		row := doctpl.Item{}
		for j, v := range appraisalPaperCells(x, i+1, t) {
			row["cot_"+strconv.Itoa(j)] = v
		}
		d.Lists["dong"] = append(d.Lists["dong"], row)
	}
	return d
}
