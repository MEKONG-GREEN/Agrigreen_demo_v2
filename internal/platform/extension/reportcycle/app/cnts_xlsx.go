package app

// W11.44: tệp Excel chia sẻ Chi cục — dựng lại TỪ NỘI DUNG ĐÃ LƯU của phiên bản (pf_extension.cnts_agg_share), nên tải
// lại phiên bản cũ cho đúng số đã gửi. Đầu tệp ghi số phiên bản, ngày giờ, ghi chú "Thay đổi so với bản đã gửi ngày …".
//   - BM-02: một sheet theo mẫu BCSLKSGM (xã → cơ sở, mã mộc dấu GS/GC, loại, 4 loài, số ngày có báo cáo, dòng cộng xã,
//     tổng cộng, ghi chú "Thiếu n ngày"/điều chỉnh).
//   - BM-18: sheet "TỔNG" = Phụ lục 2 phần III (Tuần trước liền kề, Trong kỳ, Lũy kế quý, Lũy kế năm) và 4 sheet khai
//     thác cá, tôm, mực, thủy sản khác theo xã (gom Khu vực) × tuần trong năm.

import (
	"bytes"
	"fmt"
	"strconv"
	"time"

	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extension/reportcycle/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extension/reportcycle/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tabular"
)

// sheetWriter gom dòng của một trang tính (tabular.Sheet — đổi sang số Excel ở shared/tabular, package này cấm float).
type sheetWriter struct {
	sh *tabular.Sheet
}

func newSheet(book *[]tabular.Sheet, name string) *sheetWriter {
	*book = append(*book, tabular.Sheet{Name: name})
	return &sheetWriter{sh: &(*book)[len(*book)-1]}
}

// cell là một ô: chuỗi hoặc số thập phân (chuỗi decimal → ô số).
type cell struct {
	s     string
	num   bool
	title bool
}

func txt(s string) cell { return cell{s: s} }

func num(s string) cell {
	if s == "" {
		return cell{}
	}
	return cell{s: s, num: true}
}

func (w *sheetWriter) row(bold bool, cells ...cell) error {
	out := make([]tabular.Cell, len(cells))
	for j, c := range cells {
		if c.num {
			if _, err := decimal.NewFromString(c.s); err != nil {
				return err
			}
			out[j] = tabular.Cell{Num: c.s, Bold: bold}
		} else {
			out[j] = tabular.Cell{Text: c.s, Bold: bold && c.s != "", Title: c.title}
		}
	}
	w.sh.Rows = append(w.sh.Rows, out)
	return nil
}

func (w *sheetWriter) blank() { w.sh.Rows = append(w.sh.Rows, nil) }

func (w *sheetWriter) titleLine(s string) error { return w.row(false, cell{s: s, title: true}) }

func (w *sheetWriter) widths(ws ...int) error {
	w.sh.Widths = ws
	return nil
}

func (w *sheetWriter) header(c ShareContent, sh store.Share, title, period string) error {
	if err := w.titleLine(title); err != nil {
		return err
	}
	lines := []string{
		"Tỉnh: " + c.ProvinceName + " — Phòng Chăn nuôi – Thủy sản",
		"Kỳ: " + period,
		fmt.Sprintf("Phiên bản %d — %s — gửi %s", sh.Version, sh.At.In(clock.Zone()).Format("02/01/2006 15:04"), sh.Recipient),
	}
	if sh.Basis == domain.BasisClosed {
		lines = append(lines, "Số liệu: đã chốt")
	} else {
		lines = append(lines, "Số liệu: đã gửi (chưa chốt)")
	}
	if sh.Note != "" {
		lines = append(lines, sh.Note)
	}
	for _, l := range lines {
		if err := w.row(false, txt(l)); err != nil {
			return err
		}
	}
	w.blank()
	return nil
}

func joinCodes(xs []string) string {
	out := ""
	for i, x := range xs {
		if i > 0 {
			out += "; "
		}
		out += x
	}
	return out
}

var facilityKindLabel = map[string]string{"tap_trung": "Tập trung", "nho_le": "Nhỏ lẻ"}

func shareXLSX(c ShareContent, sh store.Share) ([]byte, error) {
	var book []tabular.Sheet
	var err error
	switch {
	case c.BM02 != nil:
		err = bm02Sheet(&book, c, sh)
	case c.BM18 != nil:
		err = bm18Sheets(&book, c, sh)
	default:
		err = fmt.Errorf("nội dung phiên bản không có bảng")
	}
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if err := tabular.WriteSheetsXLSX(&buf, book); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func bm02Sheet(book *[]tabular.Sheet, c ShareContent, sh store.Share) error {
	v := c.BM02
	w := newSheet(book, "BM-02")
	title := "BÁO CÁO SỐ LƯỢNG KIỂM SOÁT GIẾT MỔ TUẦN"
	if v.PeriodType == domain.AggMonth {
		title = "BÁO CÁO SỐ LƯỢNG KIỂM SOÁT GIẾT MỔ THÁNG"
	}
	if err := w.header(c, sh, title, v.Label+" ("+v.From+" – "+v.To+")"); err != nil {
		return err
	}
	head := []cell{txt("STT"), txt("Mã xã"), txt("Xã/phường"), txt("Cơ sở giết mổ"), txt("Mã số GS"), txt("Mã số GC"), txt("Loại")}
	for _, sp := range v.Species {
		head = append(head, txt(sp.Label+" (con)"))
	}
	head = append(head, txt("Số ngày có báo cáo"), txt("Ghi chú"))
	if err := w.row(true, head...); err != nil {
		return err
	}
	stt := 0
	byCommune := map[string]BM02CommuneView{}
	for _, cv := range v.Communes {
		byCommune[cv.CommuneCode] = cv
	}
	for i, r := range v.Rows {
		stt++
		line := []cell{txt(strconv.Itoa(stt)), txt(r.CommuneCode), txt(r.CommuneName), txt(r.FacilityName), txt(joinCodes(r.StampsGS)),
			txt(joinCodes(r.StampsGC)), txt(facilityKindLabel[r.FacilityKind])}
		for _, sp := range v.Species {
			line = append(line, num(r.Total[sp.Key]))
		}
		note := ""
		if r.MissingDays > 0 {
			note = fmt.Sprintf("Thiếu %d ngày", r.MissingDays)
		}
		for _, sp := range v.Species {
			if a := r.Adjust[sp.Key]; a != "" && a != "0" {
				if note != "" {
					note += "; "
				}
				note += "Có điều chỉnh"
				break
			}
		}
		line = append(line, txt(fmt.Sprintf("%d/%d", r.DaysReported, r.DaysExpected)), txt(note))
		if err := w.row(false, line...); err != nil {
			return err
		}
		if i == len(v.Rows)-1 || v.Rows[i+1].CommuneCode != r.CommuneCode {
			cv := byCommune[r.CommuneCode]
			sub := []cell{{}, txt(r.CommuneCode), txt("Cộng " + r.CommuneName), {}, {}, {}, {}}
			for _, sp := range v.Species {
				sub = append(sub, num(cv.Total[sp.Key]))
			}
			if err := w.row(true, sub...); err != nil {
				return err
			}
		}
	}
	tot := []cell{{}, {}, txt("TỔNG CỘNG"), txt(fmt.Sprintf("%d cơ sở (%d có báo cáo)", v.UnitsTotal, v.UnitsReported)), {}, {}, {}}
	for _, sp := range v.Species {
		tot = append(tot, num(v.Total[sp.Key]))
	}
	if err := w.row(true, tot...); err != nil {
		return err
	}
	if len(v.Adjustments) > 0 {
		w.blank()
		if err := w.row(true, txt("Dòng điều chỉnh (cộng/trừ vào số tự cộng từ BM-01)")); err != nil {
			return err
		}
		head := []cell{txt("Tuần KSGM"), txt("Cơ sở"), txt("Lý do")}
		for _, sp := range v.Species {
			head = append(head, txt(sp.Label))
		}
		head = append(head, txt("Người, thời điểm"))
		if err := w.row(true, head...); err != nil {
			return err
		}
		for _, a := range v.Adjustments {
			line := []cell{txt(a.WeekKey), txt(a.FacilityName), txt(a.Reason)}
			for _, sp := range v.Species {
				line = append(line, num(a.Deltas[sp.Key]))
			}
			at := a.At
			if t, err := time.Parse(time.RFC3339, a.At); err == nil {
				at = t.In(clock.Zone()).Format("02/01/2006 15:04")
			}
			line = append(line, txt(a.ActorName+" "+at))
			if err := w.row(false, line...); err != nil {
				return err
			}
		}
	}
	return w.widths(6, 10, 22, 30, 16, 16, 11, 11, 11, 11, 11, 14, 24)
}

func bm18Sheets(book *[]tabular.Sheet, c ShareContent, sh store.Share) error {
	v := c.BM18
	w := newSheet(book, "TỔNG")
	if err := w.header(c, sh, "PHỤ LỤC 2 — ƯỚC KẾT QUẢ THỰC HIỆN CHỈ TIÊU SẢN XUẤT", "Phần III. Khai thác và nuôi trồng thủy sản — "+v.Week.Label); err != nil {
		return err
	}
	if err := w.row(true, txt("STT"), txt("Chỉ tiêu"), txt("ĐVT"), txt("Tuần trước liền kề"), txt("Trong kỳ báo cáo"), txt("Lũy kế từ đầu quý"),
		txt("Lũy kế từ đầu năm")); err != nil {
		return err
	}
	colsRow := func(bold bool, stt, label, unit string, cv ColsView) error {
		return w.row(bold, txt(stt), txt(label), txt(unit), num(cv.Prev), num(cv.Cur), num(cv.QTD), num(cv.YTD))
	}
	if err := colsRow(true, "III", "Tổng sản lượng thủy sản (khai thác + nuôi trồng)", domain.UnitTon, v.Total); err != nil {
		return err
	}
	if err := colsRow(true, "1", "Khai thác", domain.UnitTon, v.Catch[domain.CatchTotal]); err != nil {
		return err
	}
	for i, g := range domain.CatchGroups {
		if err := colsRow(false, "1."+strconv.Itoa(i+1), g.Label, domain.UnitTon, v.Catch[g.Key]); err != nil {
			return err
		}
	}
	if err := w.row(true, txt("2"), txt("Nuôi trồng")); err != nil {
		return err
	}
	if err := colsRow(true, "", "Tổng diện tích nuôi", domain.UnitHa, v.AquaArea); err != nil {
		return err
	}
	if err := colsRow(true, "", "Tổng sản lượng nuôi", domain.UnitTon, v.AquaOutput); err != nil {
		return err
	}
	groupLabel := map[string]string{}
	for _, g := range v.AquaGroups {
		groupLabel[g.Key] = g.Label
	}
	lastGroup := ""
	for i, a := range v.Aqua {
		if a.Group != "" && a.Group != lastGroup {
			if err := w.row(true, txt(""), txt(groupLabel[a.Group])); err != nil {
				return err
			}
		}
		lastGroup = a.Group
		label := a.Label
		if a.Group != "" {
			label = "   " + label
		}
		areaLabel := "diện tích"
		if a.AreaUnit == domain.UnitCage {
			areaLabel = "số lồng"
		}
		if err := colsRow(false, "2."+strconv.Itoa(i+1), label+" — "+areaLabel, a.AreaUnit, a.Area); err != nil {
			return err
		}
		if err := colsRow(false, "", label+" — sản lượng", a.OutputUnit, a.Output); err != nil {
			return err
		}
	}
	w.blank()
	if err := w.row(false, txt(fmt.Sprintf("Tổng hợp từ %d/%d xã có báo cáo BM-17 trong tuần.", v.UnitsReported, v.UnitsTotal))); err != nil {
		return err
	}
	if err := w.widths(7, 52, 12, 16, 16, 16, 16); err != nil {
		return err
	}
	for _, g := range domain.CatchGroups {
		if err := bm18CatchSheet(book, c, sh, g); err != nil {
			return err
		}
	}
	return nil
}

var catchSheetName = map[string]string{"ca": "Cá", "tom": "Tôm", "muc": "Mực", "khac": "TS khác"}

// bm18CatchSheet: khai thác một nhóm theo xã (gom Khu vực, dòng cộng Khu vực) × tuần trong năm + lũy kế năm.
func bm18CatchSheet(book *[]tabular.Sheet, c ShareContent, sh store.Share, g domain.Key) error {
	v := c.BM18
	w := newSheet(book, catchSheetName[g.Key])
	if err := w.header(c, sh, "SẢN LƯỢNG KHAI THÁC — "+g.Label+" (tấn)", "Các tuần năm "+v.Week.To[:4]+" tới "+v.Week.Label); err != nil {
		return err
	}
	head := []cell{txt("Khu vực"), txt("Xã/phường")}
	for _, wk := range v.Weeks {
		head = append(head, txt(wk.Key))
	}
	head = append(head, txt("Lũy kế năm"))
	if err := w.row(true, head...); err != nil {
		return err
	}
	regionCells := map[string]ColsView{}
	for _, r := range v.Regions {
		regionCells[r.RegionCode] = r.Cells[g.Key]
	}
	regionWeek := map[string]map[string]decimal.Decimal{}
	for i, cm := range v.Communes {
		line := []cell{txt(cm.RegionName), txt(cm.CommuneName)}
		if regionWeek[cm.RegionCode] == nil {
			regionWeek[cm.RegionCode] = map[string]decimal.Decimal{}
		}
		for _, wk := range v.Weeks {
			x, ok := cm.ByWeek[wk.Key][g.Key]
			if !ok {
				line = append(line, txt("Thiếu kỳ"))
				continue
			}
			d, _ := decimal.NewFromString(x)
			regionWeek[cm.RegionCode][wk.Key] = regionWeek[cm.RegionCode][wk.Key].Add(d)
			line = append(line, num(x))
		}
		line = append(line, num(cm.Cells[g.Key].YTD))
		if err := w.row(false, line...); err != nil {
			return err
		}
		if i == len(v.Communes)-1 || v.Communes[i+1].RegionCode != cm.RegionCode {
			sub := []cell{txt("Cộng " + cm.RegionName), {}}
			for _, wk := range v.Weeks {
				sub = append(sub, num(regionWeek[cm.RegionCode][wk.Key].String()))
			}
			sub = append(sub, num(regionCells[cm.RegionCode].YTD))
			if err := w.row(true, sub...); err != nil {
				return err
			}
		}
	}
	tot := []cell{txt("TOÀN TỈNH"), {}}
	for _, wk := range v.Weeks {
		sum := decimal.Zero
		for _, m := range regionWeek {
			sum = sum.Add(m[wk.Key])
		}
		tot = append(tot, num(sum.String()))
	}
	tot = append(tot, num(v.Catch[g.Key].YTD))
	if err := w.row(true, tot...); err != nil {
		return err
	}
	return w.widths(18, 24)
}
