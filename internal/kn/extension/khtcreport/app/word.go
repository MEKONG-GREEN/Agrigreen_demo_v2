package app

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcreport/domain"
	source "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcsource/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/doctpl"
)

// Mẫu Word KHTC Mẫu 1–5 (W10.17): khung lời văn + bảng số của cùng nội dung đóng băng đã xuất Excel (W11.66). Word là
// bản trình ký gọn — số liệu chi tiết, nguồn, mốc dữ liệu vẫn ở Excel. Đổi khung = tăng WordTemplateVersion (pf/export
// từ chối cùng phiên bản khác nội dung).
const (
	WordTemplateVersion = 1
	WordSeries          = "BC-TTKN"
	WordOwner           = "pf/extension"
	VarNumber           = "so_van_ban"
	VarPlaceDate        = "dia_danh_ngay"
)

// WordTemplateKey là khóa mẫu Word của một Mẫu KHTC (mau_1 … mau_5).
func WordTemplateKey(template string) string { return "khtc." + template }

func textVar(section string) string {
	return "loi_van_" + strings.ToLower(strings.NewReplacer(".", "_", "-", "_").Replace(section))
}

var wordVars = []doctpl.Var{
	{Key: "co_quan_cap_tren", Label: "Cơ quan cấp trên"}, {Key: VarNumber, Label: "Số văn bản"},
	{Key: VarPlaceDate, Label: "Địa danh, ngày", Required: true}, {Key: "tieu_de", Label: "Trích yếu", Required: true},
	{Key: "ky", Label: "Tên kỳ"}, {Key: "ky_toi", Label: "Kỳ tới"}, {Key: "nam", Label: "Năm"}, {Key: "so_lieu_den", Label: "Số liệu đến ngày"},
	{Key: "phien_ban", Label: "Phiên bản Excel"}, {Key: "ngay_bao_cao", Label: "Ngày báo cáo"},
	{Key: "kinh_gui", Label: "Kính gửi"}, {Key: "can_cu", Label: "Căn cứ"}, {Key: "loc", Label: "Bộ lọc khi xuất"},
	{Key: "noi_nhan", Label: "Nơi nhận"}, {Key: "tham_quyen", Label: "Thẩm quyền ký"}, {Key: "chuc_vu", Label: "Chức vụ người ký"},
	{Key: "nguoi_ky", Label: "Người ký"}, {Key: "dau_du_lieu", Label: "Dấu dữ liệu"},
}

var (
	programFields = []string{"tt", "ten", "dvt", "ke_hoach", "trong_ky", "luy_ke", "phan_tram", "tinh_trang", "nhom"}
	metricFields  = []string{"ten", "dvt", "trong_ky", "luy_ke", "luy_ke_nam", "chua_xac_nhan", "trang_thai"}
	priceFields   = []string{"mat_hang", "dvt", "ngay_truoc", "tu_1", "den_1", "ngay_sau", "tu_2", "den_2"}
	textFields    = []string{"phong", "noi_dung"}
	appendixField = []string{"tt", "ten", "quy_mo", "tien_do", "ghi_chu", "nhom"}
)

const (
	listPrevious = "ct_nam_truoc"
	listCurrent  = "ct_trong_nam"
	listOther    = "cong_tac_khac"
	listHCTH     = "cong_tac_hcth"
	listPrice    = "gia_ca"
	listAppendix = "phu_luc"
	listReasons  = "tam_tinh"
)

func metricList(section string) string { return "chi_tieu_" + textVar(section)[len("loi_van_"):] }

func programTable(list string) doctpl.Block {
	return doctpl.Block{Kind: doctpl.KindTable, Each: list, EmptyText: "Không có chương trình, hạng mục.", Columns: []doctpl.Column{
		{Header: "TT", Value: "{" + list + ".tt}", Width: 600, Align: doctpl.AlignCenter, BoldIf: "nhom"},
		{Header: "Chương trình, nhóm, hạng mục", Value: "{" + list + ".ten}", Width: 2900, BoldIf: "nhom"},
		{Header: "ĐVT", Value: "{" + list + ".dvt}", Width: 700, Align: doctpl.AlignCenter},
		{Header: "Khối lượng KH", Value: "{" + list + ".ke_hoach}", Width: 900, Align: doctpl.AlignRight},
		{Header: "Trong kỳ", Value: "{" + list + ".trong_ky}", Width: 900, Align: doctpl.AlignRight},
		{Header: "Lũy kế năm", Value: "{" + list + ".luy_ke}", Width: 900, Align: doctpl.AlignRight},
		{Header: "% KH", Value: "{" + list + ".phan_tram}", Width: 700, Align: doctpl.AlignRight},
		{Header: "Tình trạng", Value: "{" + list + ".tinh_trang}", Width: 1754},
	}}
}

func metricTable(list string) doctpl.Block {
	return doctpl.Block{Kind: doctpl.KindTable, Each: list, EmptyText: "Không có số liệu nguồn.", Columns: []doctpl.Column{
		{Header: "Chỉ tiêu", Value: "{" + list + ".ten}", Width: 2754},
		{Header: "ĐVT", Value: "{" + list + ".dvt}", Width: 800, Align: doctpl.AlignCenter},
		{Header: "Trong kỳ", Value: "{" + list + ".trong_ky}", Width: 1150, Align: doctpl.AlignRight},
		{Header: "Lũy kế nguồn / hiện có", Value: "{" + list + ".luy_ke}", Width: 1150, Align: doctpl.AlignRight},
		{Header: "Lũy kế năm", Value: "{" + list + ".luy_ke_nam}", Width: 1150, Align: doctpl.AlignRight},
		{Header: "Chưa xác nhận", Value: "{" + list + ".chua_xac_nhan}", Width: 1150, Align: doctpl.AlignRight},
		{Header: "Trạng thái", Value: "{" + list + ".trang_thai}", Width: 1200},
	}}
}

func textTable(list string) doctpl.Block {
	return doctpl.Block{Kind: doctpl.KindTable, Each: list, If: list, Columns: []doctpl.Column{
		{Header: "Phòng", Value: "{" + list + ".phong}", Width: 2500},
		{Header: "Nội dung", Value: "{" + list + ".noi_dung}", Width: 6854},
	}}
}

var priceTable = doctpl.Block{Kind: doctpl.KindTable, Each: listPrice, EmptyText: "Không có bảng giá đã duyệt.", Columns: []doctpl.Column{
	{Header: "Mặt hàng", Value: "{gia_ca.mat_hang}", Width: 2154}, {Header: "ĐVT", Value: "{gia_ca.dvt}", Width: 900, Align: doctpl.AlignCenter},
	{Header: "Ngày trước", Value: "{gia_ca.ngay_truoc}", Width: 1100, Align: doctpl.AlignCenter},
	{Header: "Giá từ", Value: "{gia_ca.tu_1}", Width: 1050, Align: doctpl.AlignRight}, {Header: "Giá đến", Value: "{gia_ca.den_1}", Width: 1050, Align: doctpl.AlignRight},
	{Header: "Ngày sau", Value: "{gia_ca.ngay_sau}", Width: 1100, Align: doctpl.AlignCenter},
	{Header: "Giá từ", Value: "{gia_ca.tu_2}", Width: 1000, Align: doctpl.AlignRight}, {Header: "Giá đến", Value: "{gia_ca.den_2}", Width: 1000, Align: doctpl.AlignRight},
}}

// WordTemplate dựng mẫu Word của một Mẫu KHTC từ khung mục (domain.Sections) — cùng thứ tự, cùng nội dung với Excel.
func WordTemplate(template string) doctpl.Template {
	t := doctpl.Template{Key: WordTemplateKey(template), Version: WordTemplateVersion, Owner: WordOwner,
		Title: "Báo cáo KHTC " + strings.ToUpper(strings.ReplaceAll(template, "_", " ")), Vars: append([]doctpl.Var{}, wordVars...)}
	lists := []doctpl.List{{Key: listReasons, Label: "Lý do Tạm tính", Fields: []string{"ly_do"}}}
	body := []doctpl.Block{
		{Kind: doctpl.KindColumns, Split: 3900,
			Left: []doctpl.Line{{Text: "{co_quan_cap_tren}", If: "co_quan_cap_tren"}, {Text: "TRUNG TÂM KHUYẾN NÔNG", Bold: true}, {Text: "Số: {so_van_ban}"}},
			Right: []doctpl.Line{{Text: "CỘNG HÒA XÃ HỘI CHỦ NGHĨA VIỆT NAM", Bold: true}, {Text: "Độc lập - Tự do - Hạnh phúc", Bold: true},
				{Text: "{dia_danh_ngay}", Italic: true}}},
		doctpl.Title("BÁO CÁO"),
		{Kind: doctpl.KindParagraph, Text: "{tieu_de}", Align: doctpl.AlignCenter, Bold: true},
		{Kind: doctpl.KindParagraph, Text: "Số liệu đến ngày {so_lieu_den} — bản xuất Excel phiên bản {phien_ban}", Align: doctpl.AlignCenter, Italic: true},
		{Kind: doctpl.KindSection, Key: "tam_tinh", Text: "TẠM TÍNH — nguồn/phiếu chưa đầy đủ", If: listReasons, Blocks: []doctpl.Block{
			{Kind: doctpl.KindLoop, Each: listReasons, Blocks: []doctpl.Block{{Kind: doctpl.KindParagraph, Text: "- {tam_tinh.ly_do}", Italic: true}}}}},
		{Kind: doctpl.KindParagraph, Text: "Xuất theo bộ lọc: {loc}", If: "loc", Italic: true},
	}
	if template == "mau_5" || template == "adhoc" {
		body = append(body,
			doctpl.Block{Kind: doctpl.KindParagraph, Text: "Kính gửi: {kinh_gui}", If: "kinh_gui", Align: doctpl.AlignCenter},
			doctpl.Block{Kind: doctpl.KindParagraph, Text: "{can_cu}", If: "can_cu", Indent: true, Align: doctpl.AlignJustify})
	}
	seen := map[string]bool{}
	addList := func(key, label string, fields []string) {
		if !seen[key] {
			seen[key] = true
			lists = append(lists, doctpl.List{Key: key, Label: label, Fields: fields})
		}
	}
	for _, section := range domain.Sections(template) {
		if section.Key == "phu_luc" {
			continue
		}
		title := section.Key + ". " + section.Title
		if section.Key == "IV" || section.Key == "II" && template == "mau_5" || strings.HasPrefix(section.Key, "C.") {
			title += " {ky_toi}"
		}
		sec := doctpl.Block{Kind: doctpl.KindSection, Key: section.Key, Text: title}
		switch section.Key {
		case "chi_tieu":
			addList("adhoc_figures", "Chỉ tiêu được chọn", []string{"ten", "dvt", "gia_tri", "nguon"})
			sec.Blocks = append(sec.Blocks, adhocWordTable())
		case "A.I.1", "A.I.2", "A.I.3":
			list := metricList(section.Key)
			addList(list, "Chỉ tiêu "+section.Key, metricFields)
			sec.Blocks = append(sec.Blocks, metricTable(list))
		case "A.III":
			addList(listPrice, "Giá cả thị trường", priceFields)
			sec.Blocks = append(sec.Blocks, priceTable)
		case "A.II.1", "I.1":
			addList(listPrevious, "Chương trình còn theo dõi năm trước", programFields)
			sec.Blocks = append(sec.Blocks, programTable(listPrevious))
		case "A.II.2", "I.2":
			addList(listCurrent, "Chương trình triển khai trong năm", programFields)
			sec.Blocks = append(sec.Blocks, programTable(listCurrent))
		case "A.II.3.2", "I.3.2":
			addList(listOther, "Công tác khác theo phòng", textFields)
			sec.Blocks = append(sec.Blocks, textTable(listOther))
		case "A.IV", "II.1", "II.2":
			addList(listHCTH, "Công tác Phòng Hành chính tổng hợp", textFields)
			sec.Blocks = append(sec.Blocks, textTable(listHCTH))
		}
		if section.Editable {
			key := textVar(section.Key)
			t.Vars = append(t.Vars, doctpl.Var{Key: key, Label: section.Key + ". " + section.Title})
			sec.Blocks = append(sec.Blocks, doctpl.Block{Kind: doctpl.KindParagraph, Text: "{" + key + "}", Indent: true, Align: doctpl.AlignJustify})
		}
		body = append(body, sec)
	}
	body = append(body,
		doctpl.Block{Kind: doctpl.KindParagraph, Text: "Trên đây là báo cáo {ky} gửi {kinh_gui} tổng hợp./.", Indent: true, Align: doctpl.AlignJustify},
		doctpl.Block{Kind: doctpl.KindColumns, Split: 4400,
			Left: []doctpl.Line{{Text: "Nơi nhận:", Bold: true, Italic: true}, {Text: "{noi_nhan}"}},
			Right: []doctpl.Line{{Text: "{tham_quyen}", Bold: true, If: "tham_quyen"}, {Text: "{chuc_vu}", Bold: true}, {Text: ""}, {Text: ""}, {Text: ""},
				{Text: "{nguoi_ky}", Bold: true}}},
	)
	if template == "mau_3" || template == "mau_4" || template == "mau_5" {
		addList(listAppendix, "Phụ lục tiến độ chương trình", appendixField)
		body = append(body, doctpl.Block{Kind: doctpl.KindPageBreak},
			doctpl.Title("PHỤ LỤC"),
			doctpl.Block{Kind: doctpl.KindParagraph, Text: "Tiến độ thực hiện chương trình khuyến nông năm {nam} (đến ngày {so_lieu_den})", Align: doctpl.AlignCenter, Bold: true},
			doctpl.Block{Kind: doctpl.KindParagraph, Text: "(Kèm theo Báo cáo số {so_van_ban}{ngay_bao_cao})", Align: doctpl.AlignCenter, Italic: true},
			doctpl.Block{Kind: doctpl.KindTable, Each: listAppendix, EmptyText: "Không có chương trình.", Columns: []doctpl.Column{
				{Header: "TT", Value: "{phu_luc.tt}", Width: 600, Align: doctpl.AlignCenter, BoldIf: "nhom"},
				{Header: "Tên chương trình", Value: "{phu_luc.ten}", Width: 3000, BoldIf: "nhom"},
				{Header: "Quy mô", Value: "{phu_luc.quy_mo}", Width: 1900},
				{Header: "Tiến độ", Value: "{phu_luc.tien_do}", Width: 2154},
				{Header: "Ghi chú", Value: "{phu_luc.ghi_chu}", Width: 1700}}})
	}
	body = append(body, doctpl.Block{Kind: doctpl.KindParagraph, Text: "Dấu dữ liệu: {dau_du_lieu}", If: "dau_du_lieu", Italic: true, Align: doctpl.AlignRight})
	t.Lists, t.Body = lists, body
	return t
}

// WordTemplates là mẫu Word của Mẫu 1–5 (đăng ký vào pf/export ở gốc lắp ráp).
func WordTemplates() []doctpl.Template {
	out := make([]doctpl.Template, 0, 7)
	for _, m := range []string{"mau_1", "mau_2", "mau_3", "mau_4", "mau_5", "adhoc"} {
		out = append(out, WordTemplate(m))
	}
	out = append(out, FixedWordTemplate())
	return out
}

var plainNumber = regexp.MustCompile(`^-?[0-9]+(\.[0-9]+)?$`)

// VNNumber định dạng chuỗi số thập phân chính xác theo kiểu Việt Nam (1.234,5) — không qua số thực.
func VNNumber(s string) string {
	if !plainNumber.MatchString(s) {
		return s
	}
	sign := ""
	if strings.HasPrefix(s, "-") {
		sign, s = "-", s[1:]
	}
	whole, frac, _ := strings.Cut(s, ".")
	var b strings.Builder
	for i, r := range whole {
		if i > 0 && (len(whole)-i)%3 == 0 {
			b.WriteByte('.')
		}
		b.WriteRune(r)
	}
	if frac != "" {
		b.WriteString("," + frac)
	}
	return sign + b.String()
}

func vnDay(day string) string {
	if t, err := time.Parse(time.DateOnly, day); err == nil {
		return t.Format("02/01/2006")
	}
	return day
}

func programItems(rows []domain.ProgramRow, tab string) []doctpl.Item {
	out := []doctpl.Item{}
	for _, row := range rows {
		if tab == "previous" && row.Tab != "prev_year" || tab == "current" && row.Tab == "prev_year" {
			continue
		}
		it := doctpl.Item{"tt": row.Roman, "ten": row.Name, "dvt": row.Unit, "tinh_trang": row.Status}
		if row.Kind != "item" {
			it["nhom"] = "1"
			out = append(out, it)
			continue
		}
		if it["tt"] == "" {
			it["tt"] = "–"
		}
		if row.Planned != nil {
			it["ke_hoach"] = VNNumber(*row.Planned)
		}
		it["trong_ky"], it["luy_ke"] = VNNumber(row.InPeriod), VNNumber(row.YearToDate)
		if row.Percent != nil {
			it["phan_tram"] = VNNumber(*row.Percent) + "%"
		}
		switch {
		case row.Missing && row.SourceKey != "":
			it["tinh_trang"] = departmentName(row.Department) + " chưa chốt số liệu."
		case row.Missing:
			it["tinh_trang"] = departmentName(row.Department) + " chưa gửi số liệu."
		case row.InPeriod == "0" && row.Content == "":
			it["tinh_trang"] = "Trong kỳ không tổ chức."
		case row.Content != "" && row.Status != "":
			it["tinh_trang"] = row.Status + "\n" + row.Content
		case row.Content != "":
			it["tinh_trang"] = row.Content
		}
		out = append(out, it)
	}
	return out
}

func appendixItems(rows []domain.ProgramRow) []doctpl.Item {
	out := []doctpl.Item{}
	for _, row := range rows {
		it := doctpl.Item{"tt": row.Roman, "ten": row.Name, "quy_mo": row.Scale, "tien_do": row.Status, "ghi_chu": row.Content}
		if row.Kind != "item" {
			it["nhom"] = "1"
			out = append(out, it)
			continue
		}
		it["tt"] = "–"
		if row.Planned != nil {
			it["quy_mo"] = strings.TrimSpace(VNNumber(*row.Planned) + " " + row.Unit + " " + row.Scale)
		}
		if row.Percent != nil && row.Planned != nil {
			it["tien_do"] = VNNumber(row.YearToDate) + "/" + VNNumber(*row.Planned) + "; đạt " + VNNumber(*row.Percent) + "% kế hoạch"
		} else if it["tien_do"] == "" {
			it["tien_do"] = row.Content
		}
		if row.Missing {
			it["ghi_chu"] = strings.TrimSpace(departmentName(row.Department) + " chưa gửi/chốt số liệu. " + row.Content)
		}
		out = append(out, it)
	}
	return out
}

func metricItems(metrics []domain.SourceMetric, section string) []doctpl.Item {
	out := []doctpl.Item{}
	hasAggregate := false
	for _, metric := range metrics {
		if metric.Form == "cnts-bm18" || metric.Form == "bm-18" {
			hasAggregate = true
		}
	}
	for _, metric := range metrics {
		switch {
		case metric.Value.Mode == source.ModePrice,
			section == "A.I.3" && hasAggregate && (metric.Form == "cnts-bm17" || metric.Form == "bm-17"),
			section == "A.I.1" && metric.Department != "trong_trot",
			section == "A.I.2" && (metric.Department != "chan_nuoi_thuy_san" || !metric.Stock),
			section == "A.I.3" && (metric.Department != "chan_nuoi_thuy_san" || metric.Stock):
			continue
		}
		v := metric.Value
		in := v.Increment
		if v.Sum != nil {
			in = v.Sum
		}
		it := doctpl.Item{"ten": metric.Label, "dvt": metric.Unit, "trong_ky": VNNumber(dec(in)), "luy_ke": VNNumber(dec(v.Cumulative)),
			"luy_ke_nam": VNNumber(dec(v.YearToDate)), "chua_xac_nhan": VNNumber(dec(v.Unconfirmed)), "trang_thai": "Đã chốt"}
		if metric.Stock {
			it["trong_ky"], it["luy_ke_nam"] = "", ""
		}
		if v.Provisional() {
			it["trang_thai"] = "Tạm tính"
		} else if v.NoSource {
			it["trang_thai"] = "Không phát sinh"
		}
		out = append(out, it)
	}
	return out
}

func priceItems(metrics []domain.SourceMetric) []doctpl.Item {
	out := []doctpl.Item{}
	for _, metric := range metrics {
		if metric.Value.Mode != source.ModePrice {
			continue
		}
		it := doctpl.Item{"mat_hang": metric.Label, "dvt": metric.Unit, "ngay_truoc": "–", "tu_1": "–", "den_1": "–", "ngay_sau": "–", "tu_2": "–", "den_2": "–"}
		keys := [2][3]string{{"ngay_truoc", "tu_1", "den_1"}, {"ngay_sau", "tu_2", "den_2"}}
		for i, k := range keys {
			if i >= len(metric.Value.PriceDays) {
				break
			}
			day := metric.Value.PriceDays[i]
			it[k[0]], it[k[1]], it[k[2]] = vnDay(day.Day), VNNumber(day.Min.String()), VNNumber(day.Max.String())
		}
		out = append(out, it)
	}
	return out
}

func formTextItems(data domain.Data, hcth bool) []doctpl.Item {
	out := []doctpl.Item{}
	keys := make([]string, 0, len(data.FormTexts))
	for key := range data.FormTexts {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, department := range keys {
		if (department == "hanh_chinh_tong_hop") != hcth {
			continue
		}
		fields := data.FormTexts[department]
		names := make([]string, 0, len(fields))
		for name := range fields {
			if name != "kho_khan" && name != "nhiem_vu_ky_toi" {
				names = append(names, name)
			}
		}
		sort.Strings(names)
		for _, name := range names {
			if fields[name] != "" {
				out = append(out, doctpl.Item{"phong": departmentName(department), "noi_dung": fields[name]})
			}
		}
	}
	return out
}

// WordData dựng dữ liệu Word từ nội dung đóng băng của một bản xuất Excel (cùng bộ lọc, cùng phiên bản). Ô số văn bản
// và dòng địa danh/ngày để trống khi ô hành chính trống — pf/export tự cấp/điền lúc xuất.
func WordData(d domain.Draft, filter domain.Filter, version int64) doctpl.Data {
	data := d.Data
	if data.Template == "adhoc" {
		data = domain.AdhocData(d)
		d.Data = data
	}
	rows := domain.SelectRows(data.Rows, filter)
	title := "Kết quả thực hiện nhiệm vụ công tác khuyến nông " + data.Period.Name
	if data.Template == "mau_5" {
		title = fmt.Sprintf("Kết quả tiến độ thực hiện Chương trình khuyến nông địa phương năm %d — %s", data.Period.Year, data.Period.Name)
	}
	if data.Template == "adhoc" {
		title = data.Period.Name
	}
	place := strings.TrimSpace(admin(d, "place"))
	vars := map[string]string{
		"tieu_de": title, "ky": data.Period.Name, "ky_toi": "— " + nextPeriod(data.Period), "nam": strconv.Itoa(data.Period.Year),
		"so_lieu_den": data.Period.Cutoff.Format("02/01/2006"), "phien_ban": strconv.FormatInt(version, 10),
		"kinh_gui": admin(d, "recipient"), "can_cu": admin(d, "basis"), "noi_nhan": admin(d, "recipients"),
		"tham_quyen": admin(d, "authority"), "chuc_vu": admin(d, "signer_title"), "nguoi_ky": admin(d, "signer"), "dau_du_lieu": data.InputHash,
	}
	if place != "" {
		vars["co_quan_cap_tren"] = "SỞ NÔNG NGHIỆP VÀ MÔI TRƯỜNG " + strings.ToUpper(place)
	}
	if no := strings.TrimSpace(admin(d, "document_no")); no != "" {
		vars[VarNumber] = no + "/" + WordSeries
		if strings.Contains(no, "/") {
			vars[VarNumber] = no
		}
	}
	if day := strings.TrimSpace(admin(d, "report_date")); day != "" {
		if at, err := time.ParseInLocation(time.DateOnly, day, clock.Zone()); err == nil {
			vars[VarPlaceDate] = doctpl.PlaceDateLine(place, at)
			vars["ngay_bao_cao"] = " ngày " + at.Format("02/01/2006")
		}
	}
	if filter.Department != "" || filter.Program != "" || filter.Commune != "" || filter.Level != "" {
		parts := []string{}
		for _, p := range []string{departmentName(filter.Department), filter.Program, filter.Commune, filter.Level} {
			if strings.TrimSpace(p) != "" {
				parts = append(parts, p)
			}
		}
		vars["loc"] = strings.Join(parts, "; ")
	}
	for _, section := range domain.Sections(data.Template) {
		if section.Editable {
			vars[textVar(section.Key)] = text(d, section.Key)
		}
	}
	reasons := []doctpl.Item{}
	for _, r := range data.Provisional {
		reasons = append(reasons, doctpl.Item{"ly_do": reasonTitle(r)})
	}
	lists := map[string][]doctpl.Item{
		listReasons: reasons, listPrevious: programItems(rows, "previous"), listCurrent: programItems(rows, "current"),
		listOther: formTextItems(data, false), listHCTH: formTextItems(data, true), listPrice: priceItems(data.Metrics), listAppendix: appendixItems(rows),
	}
	if data.Template == "adhoc" {
		lists["adhoc_figures"] = adhocWordItems(data)
	}
	for _, s := range []string{"A.I.1", "A.I.2", "A.I.3"} {
		lists[metricList(s)] = metricItems(data.Metrics, s)
	}
	// Chỉ giữ danh sách mẫu khai báo (Mẫu 2–5 không có bảng chỉ tiêu/giá) — dữ liệu băm đúng phần dùng.
	declared := map[string]bool{}
	for _, l := range WordTemplate(data.Template).Lists {
		declared[l.Key] = true
	}
	for k := range lists {
		if !declared[k] {
			delete(lists, k)
		}
	}
	return doctpl.Data{Vars: vars, Lists: lists}
}
