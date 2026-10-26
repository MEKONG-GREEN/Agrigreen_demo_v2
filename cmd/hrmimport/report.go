package main

// Báo cáo theo dòng (CSV UTF-8 có BOM hoặc .xlsx theo phần mở rộng) để Phòng HCTH sửa dòng lỗi và xác nhận (A2). Một
// dòng báo cáo cho mỗi lỗi/cảnh báo; dòng nguồn không có vấn đề in một dòng trạng thái. Báo cáo KHÔNG chứa giá trị của
// trường dữ liệu cá nhân — chỉ số dòng, Mã cũ, mã hồ sơ, tên cột và nội dung lỗi.

import (
	"bytes"
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tabular"
)

// ReportHeader là tiêu đề cột của báo cáo.
var ReportHeader = []string{"Dòng", "Mã cũ", "Trạng thái", "Mã hồ sơ", "Mức", "Cột", "Nội dung"}

// safeCell chặn công thức bảng tính (CSV injection): ô bắt đầu bằng = + - @ được thêm dấu nháy.
func safeCell(s string) string {
	if s != "" && strings.ContainsRune("=+-@\t\r", rune(s[0])) {
		return "'" + s
	}
	return s
}

// reportRows dựng các dòng báo cáo. header: tiêu đề gốc của tệp; col: trường → chỉ số cột.
func reportRows(rows []*Row, header []string, col map[string]int) [][]string {
	colName := func(k string) string {
		if k == "" {
			return ""
		}
		if i, ok := col[k]; ok && i < len(header) {
			return header[i]
		}
		if f, ok := fieldByKey(k); ok {
			return f.Header
		}
		return k
	}
	var out [][]string
	for _, r := range rows {
		base := []string{fmt.Sprint(r.Line), safeCell(r.SourceKey), r.Status, r.Code}
		if len(r.Issues) == 0 {
			out = append(out, append(base, "", "", ""))
			continue
		}
		for _, is := range r.Issues {
			out = append(out, append(append([]string(nil), base...), is.Level, safeCell(colName(is.Field)), safeCell(is.Message)))
		}
	}
	return out
}

// writeReport ghi báo cáo ra path (.xlsx → Excel, khác → CSV UTF-8 có BOM); path "-" → w (CSV).
func writeReport(path string, w io.Writer, rows [][]string) error {
	if strings.EqualFold(filepath.Ext(path), ".xlsx") {
		var b bytes.Buffer
		if err := tabular.WriteXLSX(&b, "Bao cao", ReportHeader, rows); err != nil {
			return err
		}
		return os.WriteFile(filepath.Clean(path), b.Bytes(), 0o600)
	}
	var b bytes.Buffer
	b.WriteString("\xef\xbb\xbf")
	cw := csv.NewWriter(&b)
	_ = cw.Write(ReportHeader)
	_ = cw.WriteAll(rows)
	if err := cw.Error(); err != nil {
		return err
	}
	if path == "" || path == "-" {
		_, err := w.Write(b.Bytes())
		return err
	}
	return os.WriteFile(filepath.Clean(path), b.Bytes(), 0o600)
}

// Summary là tổng kết một lần chạy.
type Summary struct {
	Total, Valid, Errors, Warnings, Created, Exists, Skipped int
}

func summarize(rows []*Row) Summary {
	var s Summary
	s.Total = len(rows)
	for _, r := range rows {
		for _, i := range r.Issues {
			if i.Level == LevelWarn {
				s.Warnings++
			}
		}
		switch r.Status {
		case StatusValid:
			s.Valid++
		case StatusError:
			s.Errors++
		case StatusCreated:
			s.Created++
		case StatusExists:
			s.Exists++
		case StatusSkipped:
			s.Skipped++
		}
	}
	return s
}

func (s Summary) String() string {
	return fmt.Sprintf("Tổng %d dòng: hợp lệ %d, lỗi %d, đã tạo %d, đã có %d, chưa ghi %d; %d cảnh báo",
		s.Total, s.Valid, s.Errors, s.Created, s.Exists, s.Skipped, s.Warnings)
}
