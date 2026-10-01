package form

import (
	"strconv"
	"strings"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/port"
)

// LayoutOptions đặt nhãn theo đợt phát hành cho màn nhập/xem (FN-TI-02.02: "Tiêu đề cột
// theo kỳ của biểu đặt theo các kỳ đã chia").
type LayoutOptions struct {
	PeriodLabels []string // nhãn kỳ theo thứ tự khe (vd "Tuần 36"); rỗng = giữ tiêu đề mẫu
	Season       string   // thay "{season}" trong tiêu đề (vd "THU ĐÔNG 2026")
}

// Layout trả bản định nghĩa đã đặt nhãn: cột theo kỳ của khe có nhãn đổi tiêu đề, khe
// vượt số kỳ của đợt đánh dấu inactive (giữ chữ cái cột như tệp Excel).
func Layout(def port.FormDefinition, o LayoutOptions) port.FormDefinition {
	d := Clone(def)
	blocks := map[string]port.PeriodBlock{}
	for _, b := range d.PeriodBlocks {
		blocks[b.ID] = b
	}
	label := func(block string, slot int) (string, bool) {
		if len(o.PeriodLabels) == 0 {
			b := blocks[block]
			if b.TitlePattern == "" {
				return "", false
			}
			return strings.ReplaceAll(b.TitlePattern, "{n}", strconv.Itoa(slot)), true
		}
		if slot <= len(o.PeriodLabels) {
			return o.PeriodLabels[slot-1], true
		}
		return "", false
	}
	for i, c := range d.Columns {
		if c.Kind == port.ColumnPerPeriod {
			if len(o.PeriodLabels) > 0 && c.PeriodSlot > len(o.PeriodLabels) {
				d.Columns[i].Inactive = true
			} else if blocks[c.PeriodBlock].Width == 1 {
				if l, ok := label(c.PeriodBlock, c.PeriodSlot); ok {
					d.Columns[i].Title = l
				}
			}
		}
		d.Columns[i].Title = season(d.Columns[i].Title, o.Season)
	}
	for i, hc := range d.Header.Cells {
		if hc.PeriodSlot > 0 {
			if l, ok := label(hc.PeriodBlock, hc.PeriodSlot); ok {
				d.Header.Cells[i].Text = l
			}
		}
		d.Header.Cells[i].Text = season(d.Header.Cells[i].Text, o.Season)
	}
	d.Title = season(d.Title, o.Season)
	d.SheetName = season(d.SheetName, o.Season)
	return d
}

func season(s, v string) string {
	if v == "" {
		return s
	}
	return strings.ReplaceAll(s, "{season}", v)
}
