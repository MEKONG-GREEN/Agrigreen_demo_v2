// Package pdftest sinh tệp PDF mẫu cho kiểm thử trích nội dung (US-LIB-03)
// bằng github.com/go-pdf/fpdf (MIT), phông TrueType Unicode nhúng (tập con).
// Không dùng trong mã chạy thật.
//
// Phông Go (golang.org/x/image/font/gofont, BSD-3) KHÔNG có chữ tiếng Việt dựng
// sẵn (vd "ả"), nên tệp mẫu tiếng Việt được sinh một lần bằng phông hệ thống có
// đủ chữ rồi commit (xem content/domain/pdf_test.go, AG_REGEN_PDF_FIXTURE).
// Phông Go chỉ dùng cho tệp mẫu chữ ASCII (mã hóa, ảnh scan).
package pdftest

import (
	"bytes"

	"github.com/go-pdf/fpdf"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/goregular"
)

// Block là một khối chữ: Heading = tiêu đề (cỡ 20), còn lại đoạn thường cỡ 11.
type Block struct {
	Heading bool
	Text    string
}

// Options điều chỉnh tệp sinh ra.
type Options struct {
	Regular, Bold []byte // phông TrueType; nil = phông Go (chỉ đủ chữ Latin cơ bản)
	Password      string // khác rỗng: mã hóa tệp (mật khẩu người dùng)
	ImageOnly     bool   // chỉ vẽ hình, không có lớp chữ (giả lập ảnh scan)
	NewPage       []int  // chỉ số khối bắt đầu trang mới
}

// Generate sinh PDF A4 với các khối chữ; đoạn dài tự xuống dòng (MultiCell).
func Generate(blocks []Block, opt Options) ([]byte, error) {
	if opt.Regular == nil {
		opt.Regular = goregular.TTF
	}
	if opt.Bold == nil {
		opt.Bold = gobold.TTF
	}
	pdf := fpdf.New("P", "mm", "A4", "")
	pdf.SetCreator("agrigreen pdftest", true)
	pdf.AddUTF8FontFromBytes("Body", "", opt.Regular)
	pdf.AddUTF8FontFromBytes("Head", "", opt.Bold)
	if opt.Password != "" {
		pdf.SetProtection(fpdf.CnProtectPrint, opt.Password, opt.Password+"-owner")
	}
	pdf.AddPage()
	if opt.ImageOnly {
		pdf.SetFillColor(200, 200, 200)
		pdf.Rect(20, 20, 170, 120, "F")
	}
	newPage := map[int]bool{}
	for _, i := range opt.NewPage {
		newPage[i] = true
	}
	for i, b := range blocks {
		if opt.ImageOnly {
			break
		}
		if newPage[i] {
			pdf.AddPage()
		}
		if b.Heading {
			pdf.SetFont("Head", "", 20)
			pdf.MultiCell(0, 10, b.Text, "", "L", false)
			pdf.Ln(4)
			continue
		}
		pdf.SetFont("Body", "", 11)
		pdf.MultiCell(0, 5.5, b.Text, "", "L", false)
		pdf.Ln(6)
	}
	var buf bytes.Buffer
	if err := pdf.Output(&buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
