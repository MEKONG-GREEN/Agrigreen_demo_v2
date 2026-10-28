package app

// BF-CN-VACC (#168, CN-09): bảng lịch tiêm theo mẫu BM-07 "LỊCH TIÊM PHÒNG VACCIN CT GIA CẦM" — một bảng là một loại mô
// hình (sheet của tệp mẫu), mỗi hàng một hộ/lứa (xã, chủ hộ, CB theo dõi MH, ngày nhận giống), mỗi vaccine × lần hai cột
// "ngày tiêm dự kiến" và "ngày tiêm thực tế"; mũi quá hạn tô đỏ. Xuất Excel đúng bố cục sheet mẫu (FN-15 luồng phụ).

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/xuri/excelize/v2"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/cntscal/domain"
	knport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
)

// MatrixQuery lọc bảng BM-07.
type MatrixQuery struct {
	ModelType string // bắt buộc — một bảng một loại mô hình
	Project   *uuid.UUID
	Site      *uuid.UUID
	Commune   string
	From, To  *time.Time // ngày nhận giống
}

// MatrixRow là một hàng (một lứa) của bảng.
type MatrixRow struct {
	BatchView
	Cells []*domain.Dose // theo thứ tự cột; nil = lứa không có mũi này
}

// Matrix là bảng BM-07 của một loại mô hình.
type Matrix struct {
	ModelType    string
	Columns      []domain.MatrixColumn
	Rows         []MatrixRow
	Labels       map[string]string    // "<danh mục>:<mã>" → tên
	Names        map[uuid.UUID]string // cán bộ theo dõi điểm
	CommuneNames map[string]string
	Truncated    bool // quá MaxMatrixRows lứa — thu hẹp bộ lọc
	Today        time.Time
}

// VaccinationMatrix dựng bảng BM-07 trong phạm vi người gọi (lứa chưa hủy, ngày nhận giống cũ trước như thứ tự STT).
func (s *Service) VaccinationMatrix(ctx context.Context, q MatrixQuery) (Matrix, error) {
	c, err := s.reader(ctx)
	if err != nil {
		return Matrix{}, err
	}
	q.ModelType = strings.TrimSpace(q.ModelType)
	if q.ModelType == "" || len(q.ModelType) > 64 {
		return Matrix{}, problem422("model_type_code", "bắt buộc — mỗi bảng BM-07 là một loại mô hình")
	}
	if q.From != nil && q.To != nil && q.To.Before(*q.From) {
		return Matrix{}, problem422("received_to", "không trước received_from")
	}
	views, truncated, err := s.listBatches(ctx, c, BatchQuery{Commune: q.Commune, ModelType: q.ModelType, Project: q.Project, Site: q.Site,
		From: q.From, To: q.To, Limit: domain.MaxMatrixRows})
	if err != nil {
		return Matrix{}, err
	}
	views = slices.DeleteFunc(views, func(v BatchView) bool { return v.Event == domain.BatchCancelled })
	slices.SortStableFunc(views, func(a, b BatchView) int {
		if a.CommuneCode != b.CommuneCode {
			return strings.Compare(a.CommuneCode, b.CommuneCode)
		}
		if !a.Received.Equal(b.Received) {
			return a.Received.Compare(b.Received)
		}
		return strings.Compare(a.Ref.String(), b.Ref.String())
	})
	all := make([][]domain.Dose, 0, len(views))
	var officers []uuid.UUID
	communes := map[string]string{}
	for _, v := range views {
		all = append(all, v.Doses)
		if v.Model != nil && v.Model.OfficerRef != nil {
			officers = append(officers, *v.Model.OfficerRef)
		}
		communes[v.CommuneCode] = ""
	}
	m := Matrix{ModelType: q.ModelType, Columns: domain.MatrixColumns(all), Labels: s.labels(ctx), Names: s.names(ctx, uniq(officers)),
		CommuneNames: map[string]string{}, Truncated: truncated, Today: s.today()}
	if s.Units != nil {
		for code := range communes {
			if u, err := s.Units.Get(ctx, code, s.now()); err == nil {
				m.CommuneNames[code] = u.Name
			}
		}
	}
	for _, v := range views {
		m.Rows = append(m.Rows, MatrixRow{BatchView: v, Cells: domain.Align(m.Columns, v.Doses)})
	}
	return m, nil
}

func problem422(field, reason string) error { return ErrBatchInvalid.WithField(field, reason) }

// HouseholdText là nhãn hộ của một hàng: nhãn chủ hộ đã che bớt (kho định danh), hoặc địa bàn khi lứa không gắn hộ mô hình.
func (r MatrixRow) HouseholdText() string {
	switch {
	case r.Model != nil && r.Model.OwnerLabel != "":
		return r.Model.OwnerLabel
	case r.Locality != "":
		return r.Locality
	case r.HouseholdRef != nil:
		return "Hộ " + r.HouseholdRef.String()[:8]
	}
	return ""
}

// VaccinationMatrixXLSX xuất bảng BM-07 ra Excel đúng bố cục sheet của tệp mẫu: tiêu đề, hàng tiêu đề 3 tầng (vaccine →
// lần → ngày dự kiến/thực tế), một hàng mỗi hộ. Không ghi họ tên/SĐT đầy đủ — chỉ nhãn che bớt.
func (s *Service) VaccinationMatrixXLSX(ctx context.Context, q MatrixQuery) ([]byte, string, error) {
	m, err := s.VaccinationMatrix(ctx, q)
	if err != nil {
		return nil, "", err
	}
	x := excelize.NewFile()
	defer func() { _ = x.Close() }()
	model := m.Labels[knport.CatalogCNTSModelType+":"+m.ModelType]
	if model == "" {
		model = m.ModelType
	}
	sh := sheetName(model)
	if err := x.SetSheetName("Sheet1", sh); err != nil {
		return nil, "", err
	}
	w := xw{f: x, sh: sh}
	fixed := []string{"STT", "Địa chỉ (xã)", "Họ tên nông dân", "CB theo dõi MH", "Ngày nhận giống"}
	last := len(fixed) + 2*len(m.Columns)
	if last < len(fixed)+1 {
		last = len(fixed) + 1
	}
	title := "LỊCH TIÊM PHÒNG VACCIN " + strings.ToUpper(model)
	if q.From != nil {
		title += " NĂM " + fmt.Sprint(q.From.Year())
	}
	w.set(1, 1, title)
	w.merge(1, 1, last, 1)
	w.set(1, 2, fmt.Sprintf("Xuất ngày %s · %d hộ", m.Today.Format("02/01/2006"), len(m.Rows)))
	w.merge(1, 2, last, 2)
	const h1, h2, h3 = 4, 5, 6
	for i, t := range fixed {
		w.set(i+1, h1, t)
		w.merge(i+1, h1, i+1, h3)
	}
	col := len(fixed) + 1
	for i := 0; i < len(m.Columns); {
		j := i
		for j < len(m.Columns) && m.Columns[j].Vaccine == m.Columns[i].Vaccine {
			j++
		}
		name := m.Labels[knport.CatalogCNTSVaccine+":"+m.Columns[i].Vaccine]
		if name == "" {
			name = m.Columns[i].Vaccine
		}
		w.set(col, h1, name)
		w.merge(col, h1, col+2*(j-i)-1, h1)
		for k := i; k < j; k++ {
			c := col + 2*(k-i)
			w.set(c, h2, fmt.Sprintf("Lần %d", m.Columns[k].Shot))
			w.merge(c, h2, c+1, h2)
			w.set(c, h3, "Ngày tiêm dự kiến")
			w.set(c+1, h3, "Ngày tiêm thực tế")
		}
		col += 2 * (j - i)
		i = j
	}
	red, err := x.NewStyle(&excelize.Style{Fill: excelize.Fill{Type: "pattern", Pattern: 1, Color: []string{"F8CBAD"}},
		Font: &excelize.Font{Color: "C00000", Bold: true}})
	if err != nil {
		return nil, "", err
	}
	head, err := x.NewStyle(&excelize.Style{Font: &excelize.Font{Bold: true}, Alignment: &excelize.Alignment{Horizontal: "center", Vertical: "center", WrapText: true}})
	if err != nil {
		return nil, "", err
	}
	if err := w.style(1, 1, last, h3, head); err != nil {
		return nil, "", err
	}
	row := h3 + 1
	for i, r := range m.Rows {
		officer := ""
		if r.Model != nil && r.Model.OfficerRef != nil {
			officer = m.Names[*r.Model.OfficerRef]
		}
		commune := m.CommuneNames[r.CommuneCode]
		if commune == "" {
			commune = r.CommuneCode
		}
		w.set(1, row, i+1)
		w.set(2, row, commune)
		w.set(3, row, r.HouseholdText())
		w.set(4, row, officer)
		w.set(5, row, r.Received.Format("02/01/2006"))
		for k, d := range r.Cells {
			if d == nil {
				continue
			}
			c := len(fixed) + 1 + 2*k
			w.set(c, row, d.Expected.Format("02/01/2006"))
			if d.Actual != nil {
				w.set(c+1, row, d.Actual.Format("02/01/2006"))
			} else if d.Overdue(m.Today) {
				if err := w.style(c, row, c+1, row, red); err != nil {
					return nil, "", err
				}
				w.set(c+1, row, "Quá hạn")
			}
		}
		row++
	}
	if m.Truncated {
		w.set(1, row+1, fmt.Sprintf("Chỉ xuất %d lứa đầu — thu hẹp bộ lọc (dự án, điểm, xã, ngày nhận giống) để xuất đủ.", domain.MaxMatrixRows))
	}
	if w.err != nil {
		return nil, "", w.err
	}
	if err := x.SetColWidth(sh, "B", "D", 22); err != nil {
		return nil, "", err
	}
	buf, err := x.WriteToBuffer()
	if err != nil {
		return nil, "", err
	}
	return buf.Bytes(), "lich-tiem-phong-bm07-" + strings.ToLower(m.ModelType) + ".xlsx", nil
}

// sheetName: tên sheet Excel ≤ 31 ký tự, không có ký tự cấm.
func sheetName(s string) string {
	s = strings.Map(func(r rune) rune {
		if strings.ContainsRune(`[]:*?/\`, r) {
			return '-'
		}
		return r
	}, s)
	if r := []rune(s); len(r) > 31 {
		s = string(r[:31])
	}
	if strings.TrimSpace(s) == "" {
		return "BM-07"
	}
	return s
}

// xw ghi ô theo tọa độ (cột, hàng), giữ lỗi đầu tiên.
type xw struct {
	f   *excelize.File
	sh  string
	err error
}

func (w *xw) cell(col, row int) string {
	c, err := excelize.CoordinatesToCellName(col, row)
	if err != nil && w.err == nil {
		w.err = err
	}
	return c
}

func (w *xw) set(col, row int, v any) {
	if w.err != nil {
		return
	}
	if err := w.f.SetCellValue(w.sh, w.cell(col, row), v); err != nil {
		w.err = err
	}
}

func (w *xw) merge(c1, r1, c2, r2 int) {
	if w.err != nil || (c1 == c2 && r1 == r2) {
		return
	}
	if err := w.f.MergeCell(w.sh, w.cell(c1, r1), w.cell(c2, r2)); err != nil {
		w.err = err
	}
}

func (w *xw) style(c1, r1, c2, r2, id int) error {
	if w.err != nil {
		return w.err
	}
	err := w.f.SetCellStyle(w.sh, w.cell(c1, r1), w.cell(c2, r2), id)
	return errors.Join(err, w.err)
}
