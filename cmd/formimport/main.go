// Lệnh formimport dựng định nghĩa mẫu báo cáo KN v7.7 (form-template.v1) từ một sheet
// Excel (.xlsx) — tiêu đề gộp nhiều tầng, chữ cái cột, công thức của dòng dữ liệu — để nạp
// tệp gốc của TTKN An Giang khi khách gửi (Q-01). Không cần DB.
//
//	go run ./cmd/formimport -in "PL 01.xlsx" -sheet "PL 01_Tđộ XG TĐ 2026" -code T01 -out t01.json
//	go run ./cmd/formimport -in tep.xlsx -list                       # liệt kê sheet
//	go run ./cmd/formimport -seed T04 -xlsx t04.xlsx                 # xuất mẫu khởi tạo ra Excel
//
// Định nghĩa JSON nạp vào hệ thống qua Admin API: POST /v1/reports/forms/{id}/versions/{v}/draft
// {"definition": …} (nháp mới) hoặc nạp thẳng tệp bằng POST /v1/reports/forms/import.
// Báo cáo phân tích (cảnh báo: công thức tham chiếu dòng khác, hàm ngoài tập con, cột cố
// định chưa rõ nguồn…) in ra stderr; mã thoát 1 nếu định nghĩa không hợp lệ.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/port"
)

func main() {
	in := flag.String("in", "", "tệp .xlsx cần nạp")
	sheet := flag.String("sheet", "", "tên sheet (rỗng = sheet đầu)")
	code := flag.String("code", "", "mã mẫu, vd T01")
	title := flag.String("title", "", "tên mẫu (rỗng = tên sheet)")
	headerStart := flag.Int("header-start", 0, "dòng tiêu đề đầu (0 = 1)")
	dataRow := flag.Int("data-row", 0, "dòng dữ liệu đầu (0 = tự nhận)")
	out := flag.String("out", "", "ghi định nghĩa JSON ra tệp (rỗng = stdout)")
	list := flag.Bool("list", false, "chỉ liệt kê sheet của tệp")
	seed := flag.String("seed", "", "xuất mẫu khởi tạo (T01…V06) ra Excel thay vì nạp")
	xlsxOut := flag.String("xlsx", "", "tệp .xlsx đích của -seed")
	flag.Parse()
	if err := run(*in, *sheet, *code, *title, *headerStart, *dataRow, *out, *list, *seed, *xlsxOut); err != nil {
		fmt.Fprintln(os.Stderr, "formimport:", err)
		os.Exit(1)
	}
}

func run(in, sheet, code, title string, headerStart, dataRow int, out string, list bool, seed, xlsxOut string) error {
	if seed != "" {
		if xlsxOut == "" {
			return fmt.Errorf("-seed cần -xlsx")
		}
		for _, seedTemplate := range extension.ReportFormSeeds().Templates {
			d := seedTemplate.Def
			if d.Code == seed {
				b, err := reports.ExportFormXLSX(d)
				if err != nil {
					return err
				}
				return os.WriteFile(xlsxOut, b, 0o600)
			}
		}
		return fmt.Errorf("không có mẫu khởi tạo %s", seed)
	}
	if in == "" {
		return fmt.Errorf("thiếu -in")
	}
	f, err := os.Open(filepath.Clean(in))
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	if !list && code == "" {
		return fmt.Errorf("thiếu -code")
	}
	if list {
		code = "XX"
	}
	def, rep, err := reports.ImportFormXLSX(f, port.FormImportOptions{Sheet: sheet, Code: code, Title: title, HeaderStart: headerStart, DataRow: dataRow, FileName: in})
	if list {
		for _, s := range rep.Sheets {
			if _, err := fmt.Fprintln(os.Stdout, s); err != nil {
				return err
			}
		}
		return nil
	}
	rb, _ := json.MarshalIndent(rep, "", "  ")
	fmt.Fprintln(os.Stderr, string(rb))
	if err != nil {
		return err
	}
	b, err := json.MarshalIndent(def, "", "  ")
	if err != nil {
		return err
	}
	if out == "" {
		_, err = os.Stdout.Write(append(b, '\n'))
		return err
	}
	return os.WriteFile(out, b, 0o600)
}
