package main

// Tệp mẫu gửi Phòng HCTH (-template): sheet đầu "Nhan su" chỉ có dòng tiêu đề, mọi ô định dạng Văn bản (@) để Excel
// không bỏ số 0 đầu của SĐT và không đổi bậc "3/9" thành ngày; sheet "Huong dan" mô tả từng cột; sheet "Vi du" có hai
// dòng dữ liệu tổng hợp. Công cụ chỉ đọc sheet đầu tiên.

import (
	"io"

	"github.com/xuri/excelize/v2"
)

// templateRows là số dòng được định dạng sẵn Văn bản.
const templateRows = 1000

// WriteTemplate ghi tệp mẫu .xlsx.
func WriteTemplate(w io.Writer) error {
	f := excelize.NewFile()
	defer func() { _ = f.Close() }()
	bold, err := f.NewStyle(&excelize.Style{Font: &excelize.Font{Bold: true}, NumFmt: 49,
		Fill: excelize.Fill{Type: "pattern", Pattern: 1, Color: []string{"#E2EFDA"}}})
	if err != nil {
		return err
	}
	text, err := f.NewStyle(&excelize.Style{NumFmt: 49})
	if err != nil {
		return err
	}
	header := make([]string, len(Fields))
	for i, fd := range Fields {
		header[i] = fd.Header
	}
	put := func(sheet string, line int, vals []string, style int) error {
		for j, v := range vals {
			ref, err := excelize.CoordinatesToCellName(j+1, line)
			if err != nil {
				return err
			}
			if err := f.SetCellStr(sheet, ref, v); err != nil {
				return err
			}
			if err := f.SetCellStyle(sheet, ref, ref, style); err != nil {
				return err
			}
		}
		return nil
	}
	widths := func(sheet string, n int, wd float64) error {
		last, err := excelize.ColumnNumberToName(n)
		if err != nil {
			return err
		}
		return f.SetColWidth(sheet, "A", last, wd)
	}

	// Sheet 1: nhập liệu.
	const data = "Nhan su"
	if err := f.SetSheetName("Sheet1", data); err != nil {
		return err
	}
	if err := put(data, 1, header, bold); err != nil {
		return err
	}
	last, err := excelize.CoordinatesToCellName(len(Fields), templateRows+1)
	if err != nil {
		return err
	}
	if err := f.SetCellStyle(data, "A2", last, text); err != nil {
		return err
	}
	if err := widths(data, len(Fields), 18); err != nil {
		return err
	}
	if err := f.SetPanes(data, &excelize.Panes{Freeze: true, YSplit: 1, TopLeftCell: "A2", ActivePane: "bottomLeft"}); err != nil {
		return err
	}

	// Sheet 2: hướng dẫn.
	const guide = "Huong dan"
	if _, err := f.NewSheet(guide); err != nil {
		return err
	}
	intro := []string{
		"Nhập mỗi người một dòng ở sheet \"Nhan su\" (giữ sheet này là sheet ĐẦU TIÊN, không đổi tiêu đề, không gộp ô, không công thức).",
		"Ngày: dd/mm/yyyy; nếu chỉ biết tháng/năm hoặc năm thì ghi mm/yyyy hoặc yyyy. SĐT 10 số, giữ số 0 đầu.",
		"Danh mục (chức vụ, ngạch, trình độ, đơn vị…) ghi đúng tên hoặc mã đang dùng trên hệ thống (Admin → Danh mục nhân sự).",
		"Người đã nghỉ việc: ghi Ngày nghỉ việc và Lý do nghỉ việc. Cán bộ khuyến nông cấp xã: ghi Mã xã/phường và Đơn vị quản lý thay cho Đơn vị công tác.",
	}
	for i, s := range intro {
		if err := put(guide, i+1, []string{s}, text); err != nil {
			return err
		}
	}
	gh := []string{"Cột", "Khóa ánh xạ", "Bắt buộc", "Định dạng / ghi chú", "Ví dụ"}
	start := len(intro) + 2
	if err := put(guide, start, gh, bold); err != nil {
		return err
	}
	for i, fd := range Fields {
		if err := put(guide, start+1+i, []string{fd.Header, fd.Key, fd.Required, fd.Help, fd.Example}, text); err != nil {
			return err
		}
	}
	if err := f.SetColWidth(guide, "A", "C", 24); err != nil {
		return err
	}
	if err := f.SetColWidth(guide, "D", "D", 80); err != nil {
		return err
	}
	if err := f.SetColWidth(guide, "E", "E", 28); err != nil {
		return err
	}

	// Sheet 3: ví dụ (dữ liệu tổng hợp).
	const ex = "Vi du"
	if _, err := f.NewSheet(ex); err != nil {
		return err
	}
	if err := put(ex, 1, header, bold); err != nil {
		return err
	}
	row1 := make([]string, len(Fields))
	for i, fd := range Fields {
		row1[i] = fd.Example
	}
	if err := put(ex, 2, row1, text); err != nil {
		return err
	}
	xa := map[string]string{"source_key": "XA-01", "type": "Cán bộ khuyến nông cấp xã", "full_name": "Trần Thị Mẫu", "gender": "Nữ",
		"birth_date": "1992", "phone": "0900000002", "commune": "30001", "mgmt_unit": "Phòng Trồng trọt", "position": "Viên chức",
		"work_start": "03/2018", "education_level": "Trung cấp"}
	row2 := make([]string, len(Fields))
	for i, fd := range Fields {
		row2[i] = xa[fd.Key]
	}
	if err := put(ex, 3, row2, text); err != nil {
		return err
	}
	if err := widths(ex, len(Fields), 18); err != nil {
		return err
	}
	f.SetActiveSheet(0)
	return f.Write(w)
}
