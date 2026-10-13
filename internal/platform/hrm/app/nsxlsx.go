package app

// W11.28: bộ ghi Excel của biểu báo cáo Sở — giữ bố cục mẫu hiện hành (FN-12 "Mọi biểu xuất giữ bố cục mẫu hiện hành:
// tiêu đề cơ quan, quốc hiệu, hàng đánh số cột, người lập biểu, Giám đốc"), chân trang người xuất + thời điểm (BR-03).
// Mọi ô là chuỗi hoặc số nguyên — KHÔNG công thức (như shared/tabular). Đầu ra TẤT ĐỊNH (cùng dữ liệu → cùng byte) để
// tải lại so được SHA-256 với lần xuất gốc. Bộ ghi .docx dùng chung thuộc W10.17.

import (
	"bytes"
	"strconv"
	"time"

	"github.com/xuri/excelize/v2"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/hrm/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
)

// Tiêu đề cơ quan mặc định (KN An Giang).
const (
	defaultAgency = "SỞ NÔNG NGHIỆP VÀ MÔI TRƯỜNG TỈNH AN GIANG"
	defaultOrg    = "TRUNG TÂM KHUYẾN NÔNG"
)

type xlsxMeta struct {
	Agency, Org      string
	Preparer, Signer string
	Exporter         string
	ExportedAt       time.Time
}

func cellName(col, row int) string {
	n, _ := excelize.CoordinatesToCellName(col, row)
	return n
}

// renderXLSX ghi biểu t ra .xlsx.
func renderXLSX(t domain.Table, m xlsxMeta) ([]byte, error) {
	if m.Agency == "" {
		m.Agency = defaultAgency
	}
	if m.Org == "" {
		m.Org = defaultOrg
	}
	f := excelize.NewFile()
	defer func() { _ = f.Close() }()
	sheet := string(t.Code)
	if err := f.SetSheetName("Sheet1", sheet); err != nil {
		return nil, err
	}
	n := len(t.Columns)
	half := max(n/2, 2)
	bold, _ := f.NewStyle(&excelize.Style{Font: &excelize.Font{Bold: true, Family: "Times New Roman", Size: 12},
		Alignment: &excelize.Alignment{Horizontal: "center", Vertical: "center", WrapText: true}})
	plain, _ := f.NewStyle(&excelize.Style{Font: &excelize.Font{Family: "Times New Roman", Size: 11},
		Alignment: &excelize.Alignment{Horizontal: "center", Vertical: "center", WrapText: true}})
	italic, _ := f.NewStyle(&excelize.Style{Font: &excelize.Font{Italic: true, Family: "Times New Roman", Size: 11},
		Alignment: &excelize.Alignment{Horizontal: "center"}})
	border := []excelize.Border{{Type: "left", Color: "000000", Style: 1}, {Type: "right", Color: "000000", Style: 1},
		{Type: "top", Color: "000000", Style: 1}, {Type: "bottom", Color: "000000", Style: 1}}
	head, _ := f.NewStyle(&excelize.Style{Font: &excelize.Font{Bold: true, Family: "Times New Roman", Size: 11}, Border: border,
		Alignment: &excelize.Alignment{Horizontal: "center", Vertical: "center", WrapText: true}})
	body, _ := f.NewStyle(&excelize.Style{Font: &excelize.Font{Family: "Times New Roman", Size: 11}, Border: border,
		Alignment: &excelize.Alignment{Vertical: "center", WrapText: true}})
	num, _ := f.NewStyle(&excelize.Style{Font: &excelize.Font{Family: "Times New Roman", Size: 11}, Border: border,
		Alignment: &excelize.Alignment{Horizontal: "center", Vertical: "center"}})
	group, _ := f.NewStyle(&excelize.Style{Font: &excelize.Font{Bold: true, Family: "Times New Roman", Size: 11}, Border: border,
		Alignment: &excelize.Alignment{Vertical: "center"}})

	put := func(col, row int, v string, style int) {
		c := cellName(col, row)
		_ = f.SetCellStr(sheet, c, v)
		_ = f.SetCellStyle(sheet, c, c, style)
	}
	merge := func(c1, r1, c2, r2 int, v string, style int) {
		if c2 > c1 || r2 > r1 {
			_ = f.MergeCell(sheet, cellName(c1, r1), cellName(c2, r2))
		}
		put(c1, r1, v, style)
		_ = f.SetCellStyle(sheet, cellName(c1, r1), cellName(c2, r2), style)
	}
	// Tiêu đề cơ quan + quốc hiệu.
	merge(1, 1, half, 1, m.Agency, plain)
	merge(half+1, 1, n, 1, "CỘNG HÒA XÃ HỘI CHỦ NGHĨA VIỆT NAM", bold)
	merge(1, 2, half, 2, m.Org, bold)
	merge(half+1, 2, n, 2, "Độc lập - Tự do - Hạnh phúc", bold)
	merge(1, 4, n, 4, t.Title, bold)
	merge(1, 5, n, 5, t.Period, bold)
	merge(1, 6, n, 6, "(Số liệu tính đến ngày "+t.AsOf.Format("02/01/2006")+")", italic)
	// Tiêu đề cột: hàng nhóm + hàng nhãn (cột không nhóm gộp hai hàng) + hàng đánh số cột.
	r := 8
	for i := 0; i < n; {
		c := t.Columns[i]
		if c.Group == "" {
			merge(i+1, r, i+1, r+1, c.Label, head)
			i++
			continue
		}
		j := i
		for j+1 < n && t.Columns[j+1].Group == c.Group {
			j++
		}
		merge(i+1, r, j+1, r, c.Group, head)
		for k := i; k <= j; k++ {
			put(k+1, r+1, t.Columns[k].Label, head)
		}
		i = j + 1
	}
	for i := 0; i < n; i++ {
		put(i+1, r+2, strconv.Itoa(i+1), head)
	}
	r += 3
	listing := !t.Columns[n-1].Numeric // biểu danh sách: dòng đơn vị/tổng là một ô gộp
	for _, row := range t.Rows {
		switch {
		case row.Kind == domain.RowGroup || listing && row.Kind != domain.RowPerson:
			merge(1, r, n, r, row.Cells[1], group)
		default:
			for i, v := range row.Cells {
				style := body
				if t.Columns[i].Numeric || i == 0 {
					style = num
				}
				if row.Kind == domain.RowTotal {
					style = head
				}
				if t.Columns[i].Numeric {
					if x, err := strconv.Atoi(v); err == nil {
						c := cellName(i+1, r)
						_ = f.SetCellInt(sheet, c, int64(x))
						_ = f.SetCellStyle(sheet, c, c, style)
						continue
					}
				}
				put(i+1, r, v, style)
			}
		}
		r++
	}
	// Chữ ký + chân trang.
	r++
	local := m.ExportedAt.In(clock.Zone())
	merge(half+1, r, n, r, "An Giang, ngày "+local.Format("02")+" tháng "+local.Format("01")+" năm "+local.Format("2006"), italic)
	r++
	merge(1, r, half, r, "NGƯỜI LẬP BIỂU", bold)
	merge(half+1, r, n, r, "GIÁM ĐỐC", bold)
	r += 4
	merge(1, r, half, r, m.Preparer, bold)
	merge(half+1, r, n, r, m.Signer, bold)
	r += 2
	merge(1, r, n, r, "Xuất từ hệ thống AgriGreen bởi "+m.Exporter+" lúc "+local.Format("15:04 02/01/2006")+
		" — biểu có dữ liệu cá nhân, chỉ dùng cho mục đích báo cáo Sở.", italic)
	// Độ rộng cột: STT hẹp, họ tên/đơn vị rộng.
	for i, c := range t.Columns {
		w := 12.0
		switch {
		case c.Key == "stt":
			w = 5
		case c.Key == "ho_ten" || c.Key == "don_vi" || c.Key == "phong_ban":
			w = 26
		case c.Numeric:
			w = 8
		}
		col, _ := excelize.ColumnNumberToName(i + 1)
		_ = f.SetColWidth(sheet, col, col, w)
	}
	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
