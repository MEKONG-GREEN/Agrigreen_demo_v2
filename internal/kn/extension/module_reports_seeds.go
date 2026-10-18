package extension

import (
	"fmt"
	"strings"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/port"
)

// Bộ 14 mẫu Phòng Trồng trọt (BRD v7.7 §8, kn-v7.7-plan §4). Tệp Excel gốc CHƯA có
// (Q-01): mọi phần không biết từ BRD/wireframe đánh dấu Placeholder và ghi vào Pending
// "chờ khách Q-01". Khi khách gửi tệp, Admin nạp bằng importer (POST
// /v1/reports/forms/import hoặc cmd/formimport) thành phiên bản nháp mới thay toàn bộ
// định nghĩa — không cần sửa mã.

// Pending là hậu tố đánh dấu phần chưa biết.
const reportSeedPending = "chờ khách Q-01"

// SeedVersion là nhãn phiên bản bộ seed (ghi vào nguồn định nghĩa).
const reportSeedVersion = "kn-v7.7-seed-2026-10-01"

type reportSeedFormBuilder struct{ d port.FormDefinition }

func reportSeedNewForm(code, title, sheet, completeness string, headerRows int) *reportSeedFormBuilder {
	return &reportSeedFormBuilder{d: port.FormDefinition{
		Schema: port.FormSchema, Code: code, Title: title, SheetName: sheet, Completeness: completeness,
		Header: port.FormHeader{Rows: headerRows, Cells: []port.HeaderCell{}, Merges: []string{}},
		Rows:   port.FormRows{Kind: port.RowsPerCommune, Indicators: []port.Indicator{}},
		Totals: []port.TotalRowDef{
			{Kind: port.TotalRegion, Label: "Cộng {region}", LabelLetter: "B"},
			{Kind: port.TotalProvince, Label: "Toàn tỉnh", LabelLetter: "B"},
		},
		Columns: []port.FormColumn{}, PeriodBlocks: []port.PeriodBlock{}, Groups: []port.ColumnGroup{}, Rules: []port.FormRule{}, Pending: []port.PendingItem{},
		Source: &port.FormSource{Kind: "seed", SeedNote: reportSeedVersion},
	}}
}

// next là chữ cái của cột sẽ thêm.
func (b *reportSeedFormBuilder) next() string { return reportSeedColumnLetter(len(b.d.Columns) + 1) }

func (b *reportSeedFormBuilder) col(c port.FormColumn) string {
	c.Letter = b.next()
	if c.Placeholder && !strings.Contains(c.Title, reportSeedPending) {
		c.Title += " (" + reportSeedPending + ")"
	}
	b.d.Columns = append(b.d.Columns, c)
	return c.Letter
}

func (b *reportSeedFormBuilder) head(row int, letter, text string) {
	b.d.Header.Cells = append(b.d.Header.Cells, port.HeaderCell{Row: row, Letter: letter, Text: text})
}

func (b *reportSeedFormBuilder) merge(from string, r1 int, to string, r2 int) {
	if from == to && r1 == r2 {
		return
	}
	b.d.Header.Merges = append(b.d.Header.Merges, fmt.Sprintf("%s%d:%s%d", from, r1, to, r2))
}

// tall thêm cột có tiêu đề gộp dọc qua mọi dòng tiêu đề.
func (b *reportSeedFormBuilder) tall(c port.FormColumn) string {
	l := b.col(c)
	b.head(1, l, b.d.Columns[len(b.d.Columns)-1].Title)
	b.merge(l, 1, l, b.d.Header.Rows)
	return l
}

func (b *reportSeedFormBuilder) pending(part, detail string) {
	b.d.Pending = append(b.d.Pending, port.PendingItem{Part: part, Detail: detail, Question: "Q-01"})
}

// ids thêm A STT, B Khu vực, C Xã (cố định, hệ thống điền).
func (b *reportSeedFormBuilder) ids(communeTitle string) {
	b.tall(port.FormColumn{Key: "stt", Title: "STT", Kind: port.ColumnFixed, DType: port.DTypeInteger, FixedSource: port.FixedRowNo})
	b.tall(port.FormColumn{Key: "khu_vuc", Title: "Khu vực", Kind: port.ColumnFixed, DType: port.DTypeText, FixedSource: port.FixedRegionName})
	b.tall(port.FormColumn{Key: "xa", Title: communeTitle, Kind: port.ColumnFixed, DType: port.DTypeText, FixedSource: port.FixedCommuneName})
}

func reportSeedHa(title string) port.FormColumn {
	return port.FormColumn{Title: title, Kind: port.ColumnInput, DType: port.DTypeNumber, Unit: "ha"}
}

func reportSeedNum(title, unit string) port.FormColumn {
	return port.FormColumn{Title: title, Kind: port.ColumnInput, DType: port.DTypeNumber, Unit: unit}
}

func reportSeedFx(title, unit, f string) port.FormColumn {
	return port.FormColumn{Title: title, Kind: port.ColumnFormula, DType: port.DTypeNumber, Unit: unit, Formula: f}
}

func reportSeedPh(c port.FormColumn) port.FormColumn { c.Placeholder = true; return c }

func reportSeedRef(l string) string { return l + "#" }

func reportSeedSumOf(ls []string) string {
	parts := make([]string, len(ls))
	for i, l := range ls {
		parts[i] = reportSeedRef(l)
	}
	return strings.Join(parts, "+")
}

func reportSeedRatio(reportSeedNum, den string) string { // tỷ lệ % theo mẫu tạm: chặn chia 0
	return fmt.Sprintf("IF(%s=0,0,%s/%s*100)", reportSeedRef(den), reportSeedRef(reportSeedNum), reportSeedRef(den))
}

func reportSeedYield(out, area string) string { // năng suất = sản lượng / diện tích
	return fmt.Sprintf("IF(%s=0,0,%s/%s)", reportSeedRef(area), reportSeedRef(out), reportSeedRef(area))
}

func (b *reportSeedFormBuilder) build() port.FormDefinition { return b.d }

// TrongTrotSeeds trả 14 mẫu Phòng Trồng trọt: T01–T07 (tuần), M01 (tháng), V01–V06 (vụ).
func reportFormSeedTemplates() []port.FormSeedTemplate {
	w := func(code, name string, d port.FormDefinition) port.FormSeedTemplate {
		return port.FormSeedTemplate{Code: code, Name: name, EntryLevel: port.LevelCommune, ReportType: port.ReportWeekly, Def: d}
	}
	m := func(code, name string, d port.FormDefinition) port.FormSeedTemplate {
		return port.FormSeedTemplate{Code: code, Name: name, EntryLevel: port.LevelCommune, ReportType: port.ReportMonthly, Def: d}
	}
	v := func(code, name string, d port.FormDefinition) port.FormSeedTemplate {
		return port.FormSeedTemplate{Code: code, Name: name, EntryLevel: port.LevelCommune, ReportType: port.ReportSeasonal, Def: d}
	}
	return []port.FormSeedTemplate{
		w("T01", "Tiến độ xuống giống lúa (PL 01)", reportSeedSeedT01()),
		w("T02", "Xuống giống, phân trà lúa mùa (PL 01B)", reportSeedSeedT02()),
		w("T03", "Phân trà lúa (PL 02)", reportSeedSeedT03()),
		w("T04", "Xuống giống, thu hoạch rau màu (PL 03)", reportSeedSeedT04()),
		w("T05", "Sâu bệnh hại lúa (PL 04)", reportSeedSeedPest("T05", "Sâu bệnh hại lúa", "PL 04", "Đối tượng sâu bệnh", 20, "SB")),
		w("T06", "Sâu bệnh hại cây trồng khác (PL 05)", reportSeedSeedPest("T06", "Sâu bệnh hại cây trồng khác", "PL 05", "Cây trồng – sâu bệnh hại", 70, "CS")),
		w("T07", "Tiến độ thu hoạch lúa (PL 07)", reportSeedSeedT07()),
		m("M01", "Cây ăn trái, cây công nghiệp (sheet tháng)", reportSeedSeedM01()),
		v("V01", "Áp dụng 3 giảm 3 tăng (PL1 3G3T)", reportSeedSeedV01()),
		v("V02", "Áp dụng 1 phải 5 giảm (PL2 1P5G)", reportSeedSeedV02()),
		v("V03", "Trang thiết bị phục vụ sản xuất – cơ giới hóa (PL3 CGH)", reportSeedSeedV03()),
		v("V04", "Cơ cấu giống lúa (PL4 CCG)", reportSeedSeedV04()),
		v("V05", "Lượng giống gieo sạ (PL5)", reportSeedSeedV05()),
		v("V06", "Diện tích tham gia mô hình (PL6)", reportSeedSeedV06()),
	}
}

// T01 (PL 01): đủ cột A–O theo BRD §8.2, Hình 14.
func reportSeedSeedT01() port.FormDefinition {
	b := reportSeedNewForm("T01", "TIẾN ĐỘ XUỐNG GIỐNG LÚA {season}", "PL 01_Tđộ XG TĐ 2026", port.CompletenessComplete, 2)
	b.d.Requirements = "Số xuống giống được nhập là số tăng thêm trong tuần, không nhập số lũy kế"
	b.ids("Xã, phường")
	b.tall(port.FormColumn{Key: "kh_tinh_giao", Title: "KH tỉnh giao", Kind: port.ColumnProvincial, DType: port.DTypeNumber, Unit: "ha"})
	b.tall(port.FormColumn{Key: "khxg_dia_phuong", Title: "KHXG (dự kiến của địa phương)", Kind: port.ColumnInput, DType: port.DTypeNumber, Unit: "ha"})
	first := b.next()
	for n := 1; n <= 7; n++ {
		l := b.col(port.FormColumn{Key: fmt.Sprintf("xg_tuan_%d", n), Title: fmt.Sprintf("Tuần %d", n), Kind: port.ColumnPerPeriod, DType: port.DTypeNumber, Unit: "ha", PeriodBlock: "xg", PeriodSlot: n})
		b.d.Header.Cells = append(b.d.Header.Cells, port.HeaderCell{Row: 2, Letter: l, Text: fmt.Sprintf("Tuần %d", n), PeriodBlock: "xg", PeriodSlot: n})
	}
	last := b.d.Columns[len(b.d.Columns)-1].Letter
	b.head(1, first, "DIỆN TÍCH XUỐNG GIỐNG LÚA {season} THEO TUẦN (ha)")
	b.merge(first, 1, last, 1)
	b.d.PeriodBlocks = append(b.d.PeriodBlocks, port.PeriodBlock{ID: "xg", FirstLetter: first, Width: 1, Slots: 7, TitlePattern: "Tuần {n}"})
	b.tall(port.FormColumn{Key: "luy_ke", Title: "LŨY KẾ", Kind: port.ColumnFormula, DType: port.DTypeNumber, Unit: "ha", Formula: fmt.Sprintf("SUM(%s#:%s#)", first, last)})
	b.tall(port.FormColumn{Key: "ngay_ket_thuc_xg", Title: "NGÀY KẾT THÚC XG", Kind: port.ColumnInput, DType: port.DTypeDate})
	b.tall(port.FormColumn{Key: "du_kien_thu_hoach", Title: "DỰ KIẾN NGÀY THU HOẠCH", Kind: port.ColumnInput, DType: port.DTypeDate})
	b.d.FreezeThrough = "C"
	b.pending("Cột F–L", "Số cột tuần tối đa (tạm 7 khe Tuần 1–7 như wireframe); tiêu đề tuần đặt theo kỳ khi phát hành")
	b.pending("Cột N, O", "Kiểu dữ liệu (tạm: ngày YYYY-MM-DD)")
	b.pending("Cột M", "Công thức Lũy kế chính xác của tệp gốc (tạm SUM các tuần)")
	b.pending("Tên sheet", "Tên sheet theo vụ (tạm “PL 01_Tđộ XG TĐ 2026”); đối chiếu tệp gốc")
	return b.build()
}

// T02 (PL 01B): chỉ biết "số tăng thêm; bảng phụ phân trà; xã có lúa mùa".
func reportSeedSeedT02() port.FormDefinition {
	b := reportSeedNewForm("T02", "XUỐNG GIỐNG VÀ PHÂN TRÀ LÚA MÙA {season}", "PL 01B", port.CompletenessPlaceholder, 2)
	b.ids("Xã, phường")
	b.tall(reportSeedPh(port.FormColumn{Title: "KH tỉnh giao", Kind: port.ColumnProvincial, DType: port.DTypeNumber, Unit: "ha"}))
	first := b.next()
	for n := 1; n <= 7; n++ {
		l := b.col(port.FormColumn{Title: fmt.Sprintf("Tuần %d", n), Kind: port.ColumnPerPeriod, DType: port.DTypeNumber, Unit: "ha", PeriodBlock: "xg", PeriodSlot: n})
		b.d.Header.Cells = append(b.d.Header.Cells, port.HeaderCell{Row: 2, Letter: l, Text: fmt.Sprintf("Tuần %d", n), PeriodBlock: "xg", PeriodSlot: n})
	}
	last := b.d.Columns[len(b.d.Columns)-1].Letter
	b.head(1, first, "DIỆN TÍCH XUỐNG GIỐNG LÚA MÙA THEO TUẦN (ha) – số tăng thêm")
	b.merge(first, 1, last, 1)
	b.d.PeriodBlocks = append(b.d.PeriodBlocks, port.PeriodBlock{ID: "xg", FirstLetter: first, Width: 1, Slots: 7, TitlePattern: "Tuần {n}"})
	lk := b.tall(reportSeedFx("LŨY KẾ", "ha", fmt.Sprintf("SUM(%s#:%s#)", first, last)))
	tra := b.next()
	for n := 1; n <= 5; n++ {
		l := b.col(reportSeedPh(reportSeedHa(fmt.Sprintf("Trà %d", n))))
		b.head(2, l, b.d.Columns[len(b.d.Columns)-1].Title)
	}
	traLast := b.d.Columns[len(b.d.Columns)-1].Letter
	b.head(1, tra, "BẢNG PHỤ PHÂN TRÀ (ha) ("+reportSeedPending+")")
	b.merge(tra, 1, traLast, 1)
	tong := b.tall(reportSeedPh(reportSeedFx("Tổng các trà", "ha", fmt.Sprintf("SUM(%s#:%s#)", tra, traLast))))
	b.d.Rules = append(b.d.Rules, port.FormRule{ID: "T02.tong_tra", Expr: reportSeedRef(tong) + "<=" + reportSeedRef(lk), Message: "Tổng các trà không vượt lũy kế xuống giống (tạm, " + reportSeedPending + ")", Severity: port.SeverityWarning, Letters: []string{tong}})
	b.d.FreezeThrough = "C"
	b.pending("Toàn bộ cột", "Bố cục cột, số tuần tối đa, “Yêu cầu biểu mẫu” nguyên văn của PL 01B")
	b.pending("Bảng phụ phân trà", "Danh sách trà và vị trí bảng phụ (tạm 5 cột trà cùng dòng)")
	b.pending("Phạm vi xã", "Danh sách xã có lúa mùa (chỉ các xã này nhận biểu)")
	return b.build()
}

// T03 (PL 02): biết ràng buộc liên biểu Σ trà = DT xuống giống PL 01 và cột cùng kỳ M–O do xã nhập.
func reportSeedSeedT03() port.FormDefinition {
	b := reportSeedNewForm("T03", "PHÂN TRÀ LÚA {season}", "PL 02", port.CompletenessPlaceholder, 2)
	b.ids("Xã, phường")
	b.tall(reportSeedPh(port.FormColumn{Key: "dt_xg_pl01", Title: "DT đã xuống giống (PL 01)", Kind: port.ColumnFixed, DType: port.DTypeNumber, Unit: "ha", FixedSource: "xref:T01:M"}))
	tra := b.next()
	for n := 1; n <= 5; n++ {
		l := b.col(reportSeedPh(reportSeedHa(fmt.Sprintf("Trà %d", n))))
		b.head(2, l, b.d.Columns[len(b.d.Columns)-1].Title)
	}
	traLast := b.d.Columns[len(b.d.Columns)-1].Letter
	b.head(1, tra, "DIỆN TÍCH THEO TRÀ (ha) – tiêu đề ngày theo kỳ phát hành")
	b.merge(tra, 1, traLast, 1)
	tong := b.tall(reportSeedFx("Tổng các trà", "ha", fmt.Sprintf("SUM(%s#:%s#)", tra, traLast)))
	b.tall(reportSeedPh(reportSeedHa("Cột K")))
	b.tall(reportSeedPh(reportSeedHa("Cột L")))
	ck := b.next() // M
	for _, t := range []string{"M", "N", "O"} {
		l := b.col(reportSeedPh(reportSeedHa("Cùng kỳ – cột " + t)))
		b.head(2, l, b.d.Columns[len(b.d.Columns)-1].Title)
	}
	b.head(1, ck, "CÙNG KỲ (Cán bộ xã nhập)")
	b.merge(ck, 1, "O", 1)
	b.d.Rules = append(b.d.Rules, port.FormRule{
		ID: "T03.tong_tra_bang_xg", Expr: reportSeedRef(tong) + `=XREF("T01","M")`, When: `ISNUMBER(XREF("T01","M"))`,
		Message: "Tổng các trà phải bằng diện tích đã xuống giống ở PL 01", Severity: port.SeverityError, Letters: []string{tong},
	})
	b.d.FreezeThrough = "C"
	b.pending("Cột E–I", "Danh sách trà (vd mạ, đẻ nhánh, đòng, trổ, chín…) và số cột")
	b.pending("Cột A–L", "Bố cục cột A–L; nhiều kỳ trên một dòng (khối theo kỳ?)")
	b.pending("Cột M–O", "Ý nghĩa ba cột cùng kỳ")
	return b.build()
}

// T04 (PL 03): A–J theo BRD; từ K: 54 cây × 4 chỉ tiêu tới cột HR, tiêu đề gộp 2 tầng nhóm cây.
func reportSeedSeedT04() port.FormDefinition {
	const crops, perGroup = 54, 6
	b := reportSeedNewForm("T04", "XUỐNG GIỐNG, THU HOẠCH RAU MÀU {season}", "PL 03", port.CompletenessPartial, 3)
	b.d.Requirements = "Số xuống giống được nhập là số lũy kế, cập nhật nếu có thay đổi (do trên cùng 1 diện tích có thể thu hoạch nhiều lần)"
	b.ids("Xã, phường, đặc khu")
	// E, G, I là tổng các cây; F, H, J là công thức — chữ cái các khối cây tính trước.
	firstCrop := 11 // K
	var xg, th, sl []string
	for i := 0; i < crops; i++ {
		base := firstCrop + i*4
		xg = append(xg, reportSeedColumnLetter(base))
		th = append(th, reportSeedColumnLetter(base+1))
		sl = append(sl, reportSeedColumnLetter(base+3))
	}
	b.tall(reportSeedPh(port.FormColumn{Key: "kh_xg", Title: "KH Xuống giống", Kind: port.ColumnProvincial, DType: port.DTypeNumber, Unit: "ha", Note: "Tạm coi là cột Tỉnh giao"}))
	b.tall(port.FormColumn{Key: "dt_xg", Title: "Diện tích xuống giống", Kind: port.ColumnFormula, DType: port.DTypeNumber, Unit: "ha", Formula: reportSeedSumOf(xg)})
	b.tall(port.FormColumn{Key: "pct_khxg", Title: "% KHXG", Kind: port.ColumnFormula, DType: port.DTypeNumber, Unit: "%", Formula: reportSeedRatio("E", "D")})
	b.tall(port.FormColumn{Key: "dt_th", Title: "DT thu hoạch", Kind: port.ColumnFormula, DType: port.DTypeNumber, Unit: "ha", Formula: reportSeedSumOf(th)})
	b.tall(port.FormColumn{Key: "nang_suat", Title: "Năng suất", Kind: port.ColumnFormula, DType: port.DTypeNumber, Unit: "tấn/ha", Formula: reportSeedYield("I", "G")})
	b.tall(port.FormColumn{Key: "san_luong", Title: "Tổng sản lượng", Kind: port.ColumnFormula, DType: port.DTypeNumber, Unit: "tấn", Formula: reportSeedSumOf(sl)})
	b.tall(port.FormColumn{Key: "pct_th", Title: "% DT thu hoạch", Kind: port.ColumnFormula, DType: port.DTypeNumber, Unit: "%", Formula: reportSeedRatio("G", "E")})
	for g := 0; g < crops/perGroup; g++ {
		gid := fmt.Sprintf("nhom_%d", g+1)
		gTitle := fmt.Sprintf("Nhóm cây %d (%s)", g+1, reportSeedPending)
		if g == 0 {
			gTitle = "Rau cải các loại (" + reportSeedPending + ")"
		}
		gFrom := b.next()
		for j := 0; j < perGroup; j++ {
			i := g*perGroup + j
			name := fmt.Sprintf("Cây %d (%s)", i+1, reportSeedPending)
			switch i {
			case 0:
				name = "Rau muống"
			case 1:
				name = "Nấm và cây khác"
			}
			key := fmt.Sprintf("cay_%02d", i+1)
			c1 := b.col(port.FormColumn{Key: key + "_xg", Title: "DT xuống giống", Kind: port.ColumnInput, DType: port.DTypeNumber, Unit: "ha", Group: gid, Placeholder: i > 1})
			c2 := b.col(port.FormColumn{Key: key + "_th", Title: "DT thu hoạch", Kind: port.ColumnInput, DType: port.DTypeNumber, Unit: "ha", Group: gid, Placeholder: i > 1})
			c4 := reportSeedColumnLetter(reportSeedColumnIndex(c2) + 2)
			b.col(port.FormColumn{Key: key + "_ns", Title: "Năng suất", Kind: port.ColumnFormula, DType: port.DTypeNumber, Unit: "tấn/ha", Group: gid, Formula: reportSeedYield(c4, c2), Placeholder: true, Note: "Tạm: năng suất = sản lượng / DT thu hoạch"})
			b.col(port.FormColumn{Key: key + "_sl", Title: "Sản lượng", Kind: port.ColumnInput, DType: port.DTypeNumber, Unit: "tấn", Group: gid, Placeholder: i > 1})
			b.head(2, c1, name)
			b.merge(c1, 2, c4, 2)
			for k, t := range []string{"DT xuống giống (ha)", "DT thu hoạch (ha)", "Năng suất (tấn/ha)", "Sản lượng (tấn)"} {
				b.head(3, reportSeedColumnLetter(reportSeedColumnIndex(c1)+k), t)
			}
		}
		gTo := b.d.Columns[len(b.d.Columns)-1].Letter
		b.head(1, gFrom, gTitle)
		b.merge(gFrom, 1, gTo, 1)
		b.d.Groups = append(b.d.Groups, port.ColumnGroup{ID: gid, Title: gTitle, From: gFrom, To: gTo})
	}
	b.d.FreezeThrough = "J"
	b.pending("Cột K–HR", "Danh sách 54 cây và nhóm cây đúng thứ tự cột (tạm: Rau muống, Nấm và cây khác, Cây 3…54; 9 nhóm × 6 cây)")
	b.pending("4 chỉ tiêu mỗi cây", "Cột nào nhập, cột nào công thức (tạm: năng suất = sản lượng / DT thu hoạch)")
	b.pending("Cột D", "KH Xuống giống là Tỉnh giao hay xã nhập (tạm Tỉnh giao)")
	b.pending("Cột E–J", "Công thức chính xác (tạm: E, G, I = tổng các cây; F = E/D·100; H = I/G; J = G/E·100)")
	return b.build()
}

// T05/T06 (PL 04/PL 05): đối tượng × 3 mức, dòng chỉ tiêu trong mỗi xã.
func reportSeedSeedPest(code, title, sheet, labelTitle string, n int, prefix string) port.FormDefinition {
	b := reportSeedNewForm(code, strings.ToUpper(title)+" – TUẦN", sheet, port.CompletenessPlaceholder, 2)
	b.ids("Xã, phường")
	b.tall(port.FormColumn{Key: "doi_tuong", Title: labelTitle, Kind: port.ColumnFixed, DType: port.DTypeText, FixedSource: port.FixedIndicatorLabel})
	first := b.next()
	for _, lv := range []string{"Nhẹ", "Trung bình", "Nặng"} {
		l := b.col(reportSeedPh(reportSeedHa("Mức " + lv)))
		b.head(2, l, b.d.Columns[len(b.d.Columns)-1].Title)
	}
	last := b.d.Columns[len(b.d.Columns)-1].Letter
	b.head(1, first, "DIỆN TÍCH NHIỄM THEO MỨC (ha)")
	b.merge(first, 1, last, 1)
	b.tall(reportSeedPh(reportSeedFx("Tổng diện tích nhiễm", "ha", fmt.Sprintf("SUM(%s#:%s#)", first, last))))
	for i := 1; i <= n; i++ {
		b.d.Rows.Indicators = append(b.d.Rows.Indicators, port.Indicator{Code: fmt.Sprintf("%s%02d", prefix, i), Label: fmt.Sprintf("%s %d (%s)", labelTitle, i, reportSeedPending), Catalog: "pest", Placeholder: true})
	}
	b.d.FreezeThrough = "D"
	b.pending("Dòng chỉ tiêu", fmt.Sprintf("Danh sách %d %s (danh mục sâu bệnh)", n, strings.ToLower(labelTitle)))
	b.pending("3 mức", "Tên 3 mức (tạm nhẹ/trung bình/nặng, ha); có cột tổng, “trong đó nặng”, diện tích phòng trị?")
	b.pending("Bố cục", "Đối tượng là dòng hay khối cột trên dòng của xã (tạm: dòng chỉ tiêu trong mỗi xã)")
	return b.build()
}

// T07 (PL 07): khối 4 cột mỗi tuần + khối công thức “Lũy kế thu hoạch”.
func reportSeedSeedT07() port.FormDefinition {
	const slots = 4
	b := reportSeedNewForm("T07", "TIẾN ĐỘ THU HOẠCH LÚA {season}", "PL 07", port.CompletenessPlaceholder, 3)
	b.ids("Xã, phường")
	b.tall(reportSeedPh(port.FormColumn{Key: "dt_xg_pl01", Title: "DT xuống giống (PL 01)", Kind: port.ColumnFixed, DType: port.DTypeNumber, Unit: "ha", FixedSource: "xref:T01:M"}))
	first := b.next()
	var dts, sls []string
	for n := 1; n <= slots; n++ {
		dt := b.col(reportSeedPh(port.FormColumn{Title: "DT thu hoạch", Kind: port.ColumnPerPeriod, DType: port.DTypeNumber, Unit: "ha", PeriodBlock: "th", PeriodSlot: n}))
		ns := b.col(reportSeedPh(port.FormColumn{Title: "Năng suất", Kind: port.ColumnPerPeriod, DType: port.DTypeNumber, Unit: "tấn/ha", PeriodBlock: "th", PeriodSlot: n}))
		slc := b.col(reportSeedPh(port.FormColumn{Title: "Sản lượng", Kind: port.ColumnFormula, DType: port.DTypeNumber, Unit: "tấn", Formula: reportSeedRef(dt) + "*" + reportSeedRef(ns), PeriodBlock: "th", PeriodSlot: n, Total: port.TotalSum}))
		pc := b.col(reportSeedPh(port.FormColumn{Title: "% so DT xuống giống", Kind: port.ColumnFormula, DType: port.DTypeNumber, Unit: "%", Formula: reportSeedRatio(dt, "D"), PeriodBlock: "th", PeriodSlot: n}))
		dts, sls = append(dts, dt), append(sls, slc)
		b.d.Header.Cells = append(b.d.Header.Cells, port.HeaderCell{Row: 2, Letter: dt, Text: fmt.Sprintf("Tuần %d", n), PeriodBlock: "th", PeriodSlot: n})
		b.merge(dt, 2, pc, 2)
		for k, t := range []string{"DT thu hoạch (ha)", "Năng suất (tấn/ha)", "Sản lượng (tấn)", "% so DT XG"} {
			b.head(3, reportSeedColumnLetter(reportSeedColumnIndex(dt)+k), t)
		}
	}
	last := b.d.Columns[len(b.d.Columns)-1].Letter
	b.head(1, first, "THU HOẠCH LÚA {season} THEO TUẦN – số tăng thêm")
	b.merge(first, 1, last, 1)
	b.d.PeriodBlocks = append(b.d.PeriodBlocks, port.PeriodBlock{ID: "th", FirstLetter: first, Width: 4, Slots: slots, TitlePattern: "Tuần {n}"})
	lk := b.col(reportSeedFx("DT thu hoạch lũy kế", "ha", reportSeedSumOf(dts)))
	slk := b.col(reportSeedFx("Sản lượng lũy kế", "tấn", reportSeedSumOf(sls)))
	nsk := b.col(reportSeedFx("Năng suất bình quân", "tấn/ha", reportSeedYield(slk, lk)))
	pck := b.col(reportSeedFx("% thu hoạch lũy kế", "%", reportSeedRatio(lk, "D")))
	b.head(1, lk, "LŨY KẾ THU HOẠCH")
	b.merge(lk, 1, pck, 2)
	for _, l := range []string{lk, slk, nsk, pck} {
		col := b.d.Columns[reportSeedColumnIndex(l)-1]
		b.head(3, l, col.Title+" ("+col.Unit+")")
	}
	b.d.FreezeThrough = "D"
	b.pending("Khối tuần", "4 cột của khối mỗi tuần (tạm: DT thu hoạch, năng suất, sản lượng = DT × NS, % so DT XG) và số tuần tối đa (tạm 4)")
	b.pending("Khối Lũy kế thu hoạch", "Công thức khối lũy kế (BRD: “thêm công thức khối Lũy kế thu hoạch”) — tạm tổng các tuần")
	b.pending("Cột cố định đầu", "Cột D (tạm: DT xuống giống lấy từ PL 01 cột M)")
	return b.build()
}

// M01 (sheet “Tháng 7”): 30 cây × 6 cột, có cột tự tính.
func reportSeedSeedM01() port.FormDefinition {
	const crops = 30
	b := reportSeedNewForm("M01", "CÂY ĂN TRÁI, CÂY CÔNG NGHIỆP – THÁNG", "Tháng 7", port.CompletenessPlaceholder, 2)
	b.d.Requirements = ""
	b.ids("Xã, phường")
	for i := 1; i <= crops; i++ {
		gid := fmt.Sprintf("cay_%02d", i)
		name := fmt.Sprintf("Cây %d (%s)", i, reportSeedPending)
		c1 := b.next()
		c2 := reportSeedColumnLetter(reportSeedColumnIndex(c1) + 1)
		c3 := reportSeedColumnLetter(reportSeedColumnIndex(c1) + 2)
		c4 := reportSeedColumnLetter(reportSeedColumnIndex(c1) + 3)
		c5 := reportSeedColumnLetter(reportSeedColumnIndex(c1) + 4)
		cols := []port.FormColumn{
			{Title: "Diện tích hiện có", Kind: port.ColumnFormula, DType: port.DTypeNumber, Unit: "ha", Formula: reportSeedRef(c2) + "+" + reportSeedRef(c3), Note: "“hiện có tự tính” — tạm = trồng mới + trồng các năm trước"},
			{Title: "Trồng mới", Kind: port.ColumnInput, DType: port.DTypeNumber, Unit: "ha"},
			{Title: "Trồng các năm trước", Kind: port.ColumnInput, DType: port.DTypeNumber, Unit: "ha"},
			{Title: "Diện tích cho trái", Kind: port.ColumnInput, DType: port.DTypeNumber, Unit: "ha"},
			{Title: "Sản lượng", Kind: port.ColumnInput, DType: port.DTypeNumber, Unit: "tấn"},
			{Title: "Năng suất", Kind: port.ColumnFormula, DType: port.DTypeNumber, Unit: "tấn/ha", Formula: reportSeedYield(c5, c4)},
		}
		for _, c := range cols {
			c.Group, c.Placeholder = gid, true
			b.head(2, b.col(c), c.Title+" ("+c.Unit+")")
		}
		last := b.d.Columns[len(b.d.Columns)-1].Letter
		b.head(1, c1, name)
		b.merge(c1, 1, last, 1)
		b.d.Groups = append(b.d.Groups, port.ColumnGroup{ID: gid, Title: name, From: c1, To: last})
	}
	b.d.FreezeThrough = "C"
	b.pending("Cột D–GA", "Danh sách 30 cây và 6 cột mỗi cây (tạm: hiện có = trồng mới + năm trước, trồng mới, năm trước, cho trái, sản lượng, năng suất)")
	b.pending("Công thức", "Công thức đúng sau khi “sửa công thức lệch cột” của sheet “Tháng 7”")
	b.pending("Yêu cầu biểu mẫu", "Văn bản nguyên văn; hạn nộp trước ngày 10 tháng sau (Q-17)")
	return b.build()
}

func reportSeedSeedV01() port.FormDefinition {
	b := reportSeedNewForm("V01", "ÁP DỤNG 3 GIẢM 3 TĂNG – {season}", "PL1 3G3T", port.CompletenessPlaceholder, 1)
	b.ids("Xã, phường")
	b.tall(reportSeedPh(reportSeedHa("Diện tích xuống giống")))
	b.tall(reportSeedPh(reportSeedHa("Diện tích áp dụng 3 giảm 3 tăng")))
	b.tall(reportSeedPh(reportSeedFx("Tỷ lệ áp dụng", "%", reportSeedRatio("E", "D"))))
	b.tall(port.FormColumn{Key: "mat_do_sa_150", Title: "Mật độ sạ ≤ 150 kg/ha", Kind: port.ColumnInput, DType: port.DTypeNumber, Unit: "ha", Note: "Ô nhập số thực tế (BRD FN-AD-04.06)"})
	b.d.FreezeThrough = "C"
	b.pending("Toàn bộ cột", "“Theo cột mẫu” — bố cục cột PL1 3G3T; chỉ biết cột “Mật độ sạ ≤ 150 kg/ha” là ô nhập số thực tế")
	return b.build()
}

func reportSeedSeedV02() port.FormDefinition {
	b := reportSeedNewForm("V02", "ÁP DỤNG 1 PHẢI 5 GIẢM – {season}", "PL2 1P5G", port.CompletenessPlaceholder, 1)
	b.ids("Xã, phường")
	b.tall(reportSeedPh(reportSeedHa("Diện tích xuống giống")))
	b.tall(reportSeedPh(reportSeedHa("Diện tích áp dụng 1 phải 5 giảm")))
	for i := 1; b.next() != "Q"; i++ { // khối A–P đầu tiên
		b.tall(reportSeedPh(reportSeedNum(fmt.Sprintf("Chỉ tiêu %d", i), "")))
	}
	b.d.FreezeThrough = "C"
	b.pending("Cột D–P", "“Theo cột mẫu”, dùng khối cột A–P đầu tiên — tên, đơn vị, công thức của từng cột")
	return b.build()
}

func reportSeedSeedV03() port.FormDefinition {
	b := reportSeedNewForm("V03", "TRANG THIẾT BỊ PHỤC VỤ SẢN XUẤT (CƠ GIỚI HÓA) – {season}", "PL3 CGH", port.CompletenessPlaceholder, 2)
	b.ids("Xã, phường")
	for i := 1; i <= 14; i++ {
		gid := fmt.Sprintf("may_%02d", i)
		name := fmt.Sprintf("Loại máy %d (%s)", i, reportSeedPending)
		c1 := b.col(port.FormColumn{Title: "Số lượng", Kind: port.ColumnInput, DType: port.DTypeInteger, Unit: "chiếc", Group: gid, Placeholder: true})
		c2 := b.col(port.FormColumn{Title: "Diện tích áp dụng", Kind: port.ColumnInput, DType: port.DTypeNumber, Unit: "ha", Group: gid, Placeholder: true})
		b.head(1, c1, name)
		b.merge(c1, 1, c2, 1)
		b.head(2, c1, "Số lượng (chiếc)")
		b.head(2, c2, "Diện tích áp dụng (ha)")
		b.d.Groups = append(b.d.Groups, port.ColumnGroup{ID: gid, Title: name, From: c1, To: c2})
	}
	b.d.FreezeThrough = "C"
	b.pending("Cột D–AE", "Danh sách 14 loại máy (= danh mục machine_type) và cột số lượng/diện tích của từng loại")
	return b.build()
}

func reportSeedSeedV04() port.FormDefinition {
	b := reportSeedNewForm("V04", "CƠ CẤU GIỐNG LÚA – {season}", "PL4 CCG", port.CompletenessPartial, 2)
	b.ids("Xã, phường")
	first, last := "E", reportSeedColumnLetter(reportSeedColumnIndex("E")+39)
	b.tall(port.FormColumn{Key: "dtxg", Title: "DTXG", Kind: port.ColumnFormula, DType: port.DTypeNumber, Unit: "ha", Formula: fmt.Sprintf("SUM(%s#:%s#)", first, last), Note: "DTXG = tổng các giống (BRD)"})
	for i := 1; i <= 40; i++ {
		l := b.col(reportSeedPh(port.FormColumn{Key: fmt.Sprintf("giong_%02d", i), Title: fmt.Sprintf("Giống %d", i), Kind: port.ColumnInput, DType: port.DTypeNumber, Unit: "ha"}))
		b.head(2, l, b.d.Columns[len(b.d.Columns)-1].Title)
	}
	b.head(1, first, "CƠ CẤU GIỐNG LÚA (ha)")
	b.merge(first, 1, last, 1)
	b.d.FreezeThrough = "D"
	b.pending("Cột E–AR", "Danh sách 40 giống đúng thứ tự (đối chiếu danh mục giống lúa), nhóm giống")
	return b.build()
}

func reportSeedSeedV05() port.FormDefinition {
	b := reportSeedNewForm("V05", "LƯỢNG GIỐNG GIEO SẠ – {season}", "PL5", port.CompletenessPartial, 2)
	b.ids("Xã, phường")
	groups := []string{"≤ 80 kg/ha", "Trên 80 – dưới 100 kg/ha", "100 – 150 kg/ha", "Trên 150 kg/ha"}
	areas := []string{"E", "G", "I", "K"}
	b.tall(reportSeedPh(reportSeedFx("Tổng diện tích gieo sạ", "ha", reportSeedSumOf(areas))))
	for i, g := range groups {
		gid := fmt.Sprintf("nhom_%d", i+1)
		a := b.col(port.FormColumn{Key: fmt.Sprintf("nhom_%d_dt", i+1), Title: "Diện tích – " + g, Kind: port.ColumnInput, DType: port.DTypeNumber, Unit: "ha", Group: gid})
		r := b.col(reportSeedPh(port.FormColumn{Key: fmt.Sprintf("nhom_%d_tl", i+1), Title: "Tỷ lệ – " + g, Kind: port.ColumnFormula, DType: port.DTypeNumber, Unit: "%", Group: gid, Formula: reportSeedRatio(a, "D")}))
		b.head(1, a, "Nhóm "+g)
		b.merge(a, 1, r, 1)
		b.head(2, a, "Diện tích (ha)")
		b.head(2, r, "Tỷ lệ (%)")
		b.d.Groups = append(b.d.Groups, port.ColumnGroup{ID: gid, Title: g, From: a, To: r})
	}
	b.d.FreezeThrough = "D"
	b.pending("Cột theo nhóm", "Cột diện tích/tỷ lệ cho từng nhóm và cột tổng (biết 4 nhóm: ≤ 80; trên 80 – dưới 100; 100–150; trên 150 kg/ha)")
	return b.build()
}

func reportSeedSeedV06() port.FormDefinition {
	b := reportSeedNewForm("V06", "DIỆN TÍCH THAM GIA MÔ HÌNH – {season}", "PL6", port.CompletenessPlaceholder, 1)
	b.ids("Xã, phường")
	for i := 1; i <= 5; i++ {
		b.tall(reportSeedPh(port.FormColumn{Title: fmt.Sprintf("Mô hình %d – diện tích tham gia", i), Kind: port.ColumnInput, DType: port.DTypeNumber, Unit: "ha"}))
	}
	b.tall(reportSeedPh(reportSeedFx("Tổng diện tích tham gia", "ha", "SUM(D#:H#)")))
	b.d.FreezeThrough = "C"
	b.pending("Toàn bộ cột", "“Theo cột mẫu” — danh sách mô hình (danh mục mô hình sản xuất) và cột của PL6")
	return b.build()
}

// ReportFormSeeds owns the fourteen KN crop-report seed layouts and their defaults.
func ReportFormSeeds() port.FormSeedRegistration {
	return port.FormSeedRegistration{Key: "kn.trong_trot", Default: true, DefaultDepartment: "trong_trot", Version: reportSeedVersion, Templates: reportFormSeedTemplates()}
}
func reportSeedColumnLetter(n int) string {
	var out string
	for n > 0 {
		n--
		out = string(rune('A'+n%26)) + out
		n /= 26
	}
	return out
}
func reportSeedColumnIndex(letter string) int {
	n := 0
	for _, c := range letter {
		n = n*26 + int(c-'A'+1)
	}
	return n
}
