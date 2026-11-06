package app

import (
	"fmt"
	"strconv"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tabular"
	"github.com/xuri/excelize/v2"
)

func renderPNProgress215(view PNProgress215) ([]byte, error) {
	f := excelize.NewFile()
	defer func() { _ = f.Close() }()
	const sheet = "Tiến độ phòng"
	if err := f.SetSheetName("Sheet1", sheet); err != nil {
		return nil, err
	}
	if err := tabular.SetColumnWidth(f, sheet, "A", "F", 18); err != nil {
		return nil, err
	}
	if err := tabular.SetColumnWidth(f, sheet, "B", "B", 48); err != nil {
		return nil, err
	}
	headers := [][]string{{"Tiến độ chương trình của phòng"}, {departmentName(view.Department)}, {view.Period.Name}, {fmt.Sprintf("Mẫu5 v%d, mốc số liệu %s", view.Snapshot.Version, view.Snapshot.CapturedAt.Format("02/01/2006 15:04:05 MST"))}, {"TT", "Chương trình / hạng mục", "ĐVT", "KH năm", "Lũy kế", "% KH"}}
	for y, row := range headers {
		for x, v := range row {
			cell, err := excelize.CoordinatesToCellName(x+1, y+1)
			if err != nil {
				return nil, err
			}
			if err = f.SetCellStr(sheet, cell, v); err != nil {
				return nil, err
			}
		}
	}
	for i, r := range view.Rows {
		planned, percent := "—", "—"
		if r.Planned != nil {
			planned = *r.Planned
		}
		if r.Percent != nil && !r.Missing {
			percent = *r.Percent
		}
		qty := r.YearToDate
		if r.Kind != "item" {
			planned, qty, percent = "", "", ""
		}
		values := []string{strconv.Itoa(i + 1), r.Name, r.Unit, planned, qty, percent}
		for column, v := range values {
			cell, err := excelize.CoordinatesToCellName(column+1, i+6)
			if err != nil {
				return nil, err
			}
			if column >= 3 && v != "" && v != "—" {
				err = f.SetCellDefault(sheet, cell, v)
			} else {
				err = f.SetCellStr(sheet, cell, v)
			}
			if err != nil {
				return nil, err
			}
		}
	}
	buf, err := f.WriteToBuffer()
	if err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
