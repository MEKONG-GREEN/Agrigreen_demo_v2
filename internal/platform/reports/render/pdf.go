package render

import (
	"bytes"
	_ "embed" // phông chữ nhúng
	"strconv"
	"time"

	"github.com/go-pdf/fpdf"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/domain"
)

// Phông Roboto (Apache-2.0, đủ dấu tiếng Việt) — tài sản nhúng, không phải
// dependency Go. Giấy phép: fonts/LICENSE-Roboto.txt, ADR-0014.
var (
	//go:embed fonts/Roboto-Regular.ttf
	fontRegular []byte
	//go:embed fonts/Roboto-Bold.ttf
	fontBold []byte
)

// FontFamily là tên họ phông nhúng trong PDF.
const FontFamily = "Roboto"

// PDFOptions điều chỉnh việc dựng PDF.
type PDFOptions struct {
	// NoCompression tắt nén luồng nội dung (chỉ để kiểm thử đọc được văn bản).
	NoCompression bool
}

const (
	margin    = 10.0
	lineH     = 4.6
	fontSize  = 8.5
	titleSize = 13.0
)

// PDF dựng tệp PDF khổ A4 ngang. at là "Ngày xuất" (ghi vào siêu dữ liệu).
func PDF(ds domain.Dataset, at time.Time, opt PDFOptions) ([]byte, error) {
	p := fpdf.New("L", "mm", "A4", "")
	p.SetCompression(!opt.NoCompression)
	p.SetCatalogSort(true)
	p.SetCreationDate(at.UTC())
	p.SetModificationDate(at.UTC())
	p.SetTitle(ds.Title, true)
	p.SetCreator("AgriGreen", true)
	p.AddUTF8FontFromBytes(FontFamily, "", fontRegular)
	p.AddUTF8FontFromBytes(FontFamily, "B", fontBold)
	p.SetMargins(margin, margin, margin)
	p.SetAutoPageBreak(false, margin)
	p.AliasNbPages("{nb}")
	notes := footer(ds)
	p.SetFooterFunc(func() {
		p.SetY(-8)
		p.SetFont(FontFamily, "", 7.5)
		left := "Ngày xuất: " + domain.FormatDateTimeVN(at)
		if ds.Provisional {
			left = domain.ProvisionalLabel + " · " + left
		}
		p.CellFormat(0, 4, left, "", 0, "L", false, 0, "")
		p.SetX(margin)
		p.CellFormat(0, 4, "Trang "+strconv.Itoa(p.PageNo())+"/{nb}", "", 0, "R", false, 0, "")
	})
	p.AddPage()
	pageW, pageH := p.GetPageSize()
	usable := pageW - 2*margin
	bottom := pageH - margin - 8

	p.SetFont(FontFamily, "B", titleSize)
	p.MultiCell(usable, 6, ds.Title, "", "C", false)
	p.Ln(1)
	p.SetFont(FontFamily, "", fontSize)
	for _, m := range ds.Meta {
		p.MultiCell(usable, lineH, m.Label+": "+m.Value, "", "L", false)
	}
	p.Ln(2)

	widths := columnWidths(p, ds, usable)
	header := func() {
		p.SetFont(FontFamily, "B", fontSize)
		p.SetFillColor(220, 239, 227)
		lines := make([][]string, len(ds.Columns))
		n := 1
		for i, c := range ds.Columns {
			lines[i] = p.SplitText(c.Title, widths[i]-2)
			n = max(n, len(lines[i]))
		}
		drawRow(p, widths, lines, n, true, nil)
		p.SetFont(FontFamily, "", fontSize)
	}
	if len(ds.Columns) > 0 {
		header()
	}
	body := func(vals []domain.Value, strong bool) {
		if strong {
			p.SetFont(FontFamily, "B", fontSize)
		}
		lines := make([][]string, len(ds.Columns))
		n := 1
		align := make([]string, len(ds.Columns))
		for i, c := range ds.Columns {
			txt := domain.Display(c, vals[i])
			if vals[i].Null() && c.Kind != domain.KindText {
				txt = "—"
			}
			lines[i] = p.SplitText(txt, widths[i]-2)
			if len(lines[i]) == 0 {
				lines[i] = []string{""}
			}
			n = max(n, len(lines[i]))
			align[i] = "L"
			if c.Kind != domain.KindText {
				align[i] = "R"
			}
		}
		if p.GetY()+float64(n)*lineH > bottom {
			p.AddPage()
			header()
			if strong {
				p.SetFont(FontFamily, "B", fontSize)
			}
		}
		drawRow(p, widths, lines, n, false, align)
		p.SetFont(FontFamily, "", fontSize)
	}
	for _, r := range ds.Rows {
		body(r, false)
	}
	if len(ds.Rows) == 0 {
		p.Ln(2)
		p.MultiCell(usable, lineH, ds.Empty, "", "C", false)
	}
	if ds.Total != nil {
		body(ds.Total, true)
	}
	if len(notes) > 0 {
		p.Ln(3)
		for i, n := range notes {
			if p.GetY()+2*lineH > bottom {
				p.AddPage()
			}
			style := ""
			if i == 0 && ds.Provisional {
				style = "B"
			}
			p.SetFont(FontFamily, style, fontSize)
			p.MultiCell(usable, lineH, n, "", "L", false)
		}
	}
	if err := p.Error(); err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if err := p.Output(&buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// drawRow vẽ một dòng bảng n dòng chữ; mỗi ô là một khung cùng chiều cao.
func drawRow(p *fpdf.Fpdf, widths []float64, lines [][]string, n int, fill bool, align []string) {
	h := float64(n) * lineH
	x0, y0 := p.GetX(), p.GetY()
	x := x0
	for i, w := range widths {
		style := "D"
		if fill {
			style = "FD"
		}
		p.Rect(x, y0, w, h, style)
		a := "C"
		if align != nil {
			a = align[i]
		}
		for j, ln := range lines[i] {
			p.SetXY(x+1, y0+float64(j)*lineH)
			p.CellFormat(w-2, lineH, ln, "", 0, a, false, 0, "")
		}
		x += w
	}
	p.SetXY(x0, y0+h)
}

// columnWidths chia bề rộng theo độ dài nội dung, tối thiểu 14 mm mỗi cột.
func columnWidths(p *fpdf.Fpdf, ds domain.Dataset, usable float64) []float64 {
	p.SetFont(FontFamily, "", fontSize)
	want := make([]float64, len(ds.Columns))
	total := 0.0
	for i, c := range ds.Columns {
		w := min(p.GetStringWidth(c.Title)/1.6, 40)
		for _, r := range ds.Rows {
			w = max(w, min(p.GetStringWidth(domain.Display(c, r[i])), 60))
		}
		w = max(w+3, 14)
		want[i] = w
		total += w
	}
	if total == 0 {
		return want
	}
	for i := range want {
		want[i] = want[i] * usable / total
	}
	return want
}
