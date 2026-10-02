package render

import (
	"bytes"
	"strings"
	"time"

	"github.com/shopspring/decimal"
	"github.com/xuri/excelize/v2"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/formula"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
)

// Bộ vẽ "Báo cáo tổng hợp các xã" ĐÚNG BỐ CỤC MẪU (KN v7.7 FN-TI-02.05/02.06): tiêu đề gộp nhiều tầng của định
// nghĩa lưới, hàng chữ cái cột Excel, mỗi dòng một xã gom theo khu vực, dòng "Cộng khu vực"/"Toàn tỉnh" tô xanh. Số
// lấy NGUYÊN chuỗi decimal của lần tổng hợp (SetCellDefault — không qua float, "số trong file = số tổng hợp").

// Loại dòng của lưới.
const (
	GridCommune  = "commune"
	GridRegion   = "region"
	GridProvince = "province"
)

// GridRow là một dòng của lưới xuất.
type GridRow struct {
	Kind  string
	Cells map[string]string
}

// GridMeta là phần đầu/chân của tệp.
type GridMeta struct {
	Title       string // vd "BÁO CÁO TỔNG HỢP CÁC XÃ — T01"
	Lines       []string
	Provisional bool
	Missing     []string // tên xã chưa nộp (in kèm khi Tạm tính)
	At          time.Time
}

const gridTop = 1 // dòng đầu của khung tiêu đề

func gridHeaderStart(meta GridMeta) int { return gridTop + 1 + len(meta.Lines) + 1 }

// GridDataRow trả dòng Excel của dòng dữ liệu đầu tiên (kiểm thử tra ô).
func GridDataRow(def port.FormDefinition, meta GridMeta) int {
	return gridHeaderStart(meta) + def.Header.Rows + 1
}

func numeric(col port.FormColumn) bool {
	return col.DType == port.DTypeNumber || col.DType == port.DTypeInteger
}

// FormGridXLSX dựng tệp Excel lưới tổng hợp theo định nghĩa mẫu (đã đặt nhãn kỳ).
func FormGridXLSX(def port.FormDefinition, rows []GridRow, meta GridMeta) ([]byte, error) {
	f := excelize.NewFile()
	defer func() { _ = f.Close() }()
	sh := gridSheet(def)
	if err := f.SetSheetName("Sheet1", sh); err != nil {
		return nil, err
	}
	stamp := meta.At.UTC().Format(time.RFC3339)
	if err := f.SetDocProps(&excelize.DocProperties{Title: meta.Title, Creator: "AgriGreen", LastModifiedBy: "AgriGreen", Created: stamp, Modified: stamp,
		Language: "vi-VN"}); err != nil {
		return nil, err
	}
	border := []excelize.Border{{Type: "left", Style: 1}, {Type: "right", Style: 1}, {Type: "top", Style: 1}, {Type: "bottom", Style: 1}}
	title, err := f.NewStyle(&excelize.Style{Font: &excelize.Font{Bold: true, Size: 13}})
	if err != nil {
		return nil, err
	}
	head, err := f.NewStyle(&excelize.Style{Font: &excelize.Font{Bold: true}, Border: border,
		Alignment: &excelize.Alignment{WrapText: true, Vertical: "center", Horizontal: "center"}})
	if err != nil {
		return nil, err
	}
	letters, err := f.NewStyle(&excelize.Style{Font: &excelize.Font{Italic: true, Color: "666666"}, Border: border,
		Alignment: &excelize.Alignment{Horizontal: "center"}, Fill: excelize.Fill{Type: "pattern", Pattern: 1, Color: []string{"EEEEEE"}}})
	if err != nil {
		return nil, err
	}
	numFmt := "#,##0.##"
	cell, err := f.NewStyle(&excelize.Style{Border: border, CustomNumFmt: &numFmt})
	if err != nil {
		return nil, err
	}
	total, err := f.NewStyle(&excelize.Style{Border: border, CustomNumFmt: &numFmt, Font: &excelize.Font{Bold: true},
		Fill: excelize.Fill{Type: "pattern", Pattern: 1, Color: []string{"DCEFE3"}}})
	if err != nil {
		return nil, err
	}
	name := func(col, row int) string { n, _ := excelize.CoordinatesToCellName(col, row); return n }
	last := len(def.Columns)
	if last == 0 {
		last = 1
	}
	if err := f.SetCellStr(sh, name(1, gridTop), meta.Title); err != nil {
		return nil, err
	}
	_ = f.SetCellStyle(sh, name(1, gridTop), name(1, gridTop), title)
	for i, l := range meta.Lines {
		if err := f.SetCellStr(sh, name(1, gridTop+1+i), l); err != nil {
			return nil, err
		}
	}
	h0 := gridHeaderStart(meta) - 1 // dòng tiêu đề 1 của mẫu = h0 + 1
	for _, hc := range def.Header.Cells {
		if err := f.SetCellStr(sh, name(formula.ColumnIndex(hc.Letter), h0+hc.Row), hc.Text); err != nil {
			return nil, err
		}
	}
	for _, m := range def.Header.Merges {
		a, b, ok := strings.Cut(m, ":")
		if !ok {
			continue
		}
		ca, ra, e1 := excelize.CellNameToCoordinates(a)
		cb, rb, e2 := excelize.CellNameToCoordinates(b)
		if e1 != nil || e2 != nil {
			continue
		}
		if err := f.MergeCell(sh, name(ca, h0+ra), name(cb, h0+rb)); err != nil {
			return nil, err
		}
	}
	if def.Header.Rows > 0 {
		_ = f.SetCellStyle(sh, name(1, h0+1), name(last, h0+def.Header.Rows), head)
	}
	lr := h0 + def.Header.Rows + 1
	for i, c := range def.Columns {
		if err := f.SetCellStr(sh, name(i+1, lr), c.Letter); err != nil {
			return nil, err
		}
	}
	_ = f.SetCellStyle(sh, name(1, lr), name(last, lr), letters)
	r := lr + 1
	for _, row := range rows {
		for i, c := range def.Columns {
			v, ok := row.Cells[c.Letter]
			if !ok || v == "" {
				continue
			}
			if numeric(c) {
				if _, err := decimal.NewFromString(v); err == nil {
					if err := f.SetCellDefault(sh, name(i+1, r), v); err != nil {
						return nil, err
					}
					continue
				}
			}
			if err := f.SetCellStr(sh, name(i+1, r), v); err != nil {
				return nil, err
			}
		}
		st := cell
		if row.Kind != GridCommune {
			st = total
		}
		_ = f.SetCellStyle(sh, name(1, r), name(last, r), st)
		r++
	}
	r++
	if def.Requirements != "" {
		_ = f.SetCellStr(sh, name(1, r), "Yêu cầu biểu mẫu: "+def.Requirements)
		r++
	}
	if meta.Provisional {
		_ = f.SetCellStr(sh, name(1, r), domain.ProvisionalLabel)
		r++
		if len(meta.Missing) > 0 {
			_ = f.SetCellStr(sh, name(1, r), "Xã chưa nộp: "+strings.Join(meta.Missing, ", "))
			r++
		}
	}
	_ = f.SetCellStr(sh, name(1, r), "Ngày xuất: "+meta.At.In(clock.Zone()).Format("02/01/2006 15:04"))
	if def.FreezeThrough != "" {
		xs := formula.ColumnIndex(def.FreezeThrough)
		_ = f.SetPanes(sh, &excelize.Panes{Freeze: true, XSplit: xs, YSplit: lr, TopLeftCell: name(xs+1, lr+1), ActivePane: "bottomRight"})
	}
	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func gridSheet(def port.FormDefinition) string {
	s := def.SheetName
	if s == "" {
		s = def.Code
	}
	s = strings.Map(func(r rune) rune {
		if strings.ContainsRune(`:\/?*[]`, r) {
			return '_'
		}
		return r
	}, s)
	if rs := []rune(s); len(rs) > 31 {
		s = string(rs[:31])
	}
	if s == "" {
		s = "Sheet1"
	}
	return s
}

// GridDataset chuyển lưới sang domain.Dataset để in PDF khổ ngang (ADR-0014): mỗi cột "chữ cái · tiêu đề".
func GridDataset(def port.FormDefinition, rows []GridRow, meta GridMeta) domain.Dataset {
	ds := domain.Dataset{Title: meta.Title, Empty: domain.EmptyMessage, Provisional: meta.Provisional}
	for _, l := range meta.Lines {
		k, v, ok := strings.Cut(l, ": ")
		if !ok {
			k, v = l, ""
		}
		ds.Meta = append(ds.Meta, domain.Meta{Label: k, Value: v})
	}
	for _, c := range def.Columns {
		kind := domain.KindText
		if numeric(c) {
			kind = domain.KindDecimal
		}
		ds.Columns = append(ds.Columns, domain.DataColumn{Key: c.Letter, Title: c.Letter + " · " + c.Title, Kind: kind, Scale: 2})
	}
	for _, row := range rows {
		vals := make([]domain.Value, len(def.Columns))
		for i, c := range def.Columns {
			v := row.Cells[c.Letter]
			if numeric(c) {
				if d, err := decimal.NewFromString(v); err == nil {
					vals[i] = domain.D(d)
					continue
				}
			}
			vals[i] = domain.Txt(v)
		}
		ds.Rows = append(ds.Rows, vals)
	}
	if meta.Provisional && len(meta.Missing) > 0 {
		ds.Notes = append(ds.Notes, "Xã chưa nộp: "+strings.Join(meta.Missing, ", "))
	}
	return ds
}
