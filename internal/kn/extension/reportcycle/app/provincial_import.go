package app

import (
	"context"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	"io"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/reportcycle/domain"
	repport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tabular"
)

// Giới hạn tệp nạp Tỉnh giao (một dòng một xã; An Giang ~ 100 xã/phường).
const (
	MaxProvincialImportBytes = 2 << 20
	MaxProvincialImportRows  = 500
)

// ErrProvincialImportInvalid: tệp nạp Tỉnh giao sai định dạng hoặc sai mẫu (cả tệp bị từ chối).
var ErrProvincialImportInvalid = problem.New(http.StatusUnprocessableEntity, "extension.provincial_import_invalid", "Tệp Tỉnh giao không đúng mẫu")

// ProvincialImportRow là kết quả một dòng của tệp. Reason không chứa giá trị nhập.
type ProvincialImportRow struct {
	Line    int
	Commune string
	Status  string // accepted | rejected
	Reason  string
}

// ProvincialImportColumn là cột của tệp đã nhận ra là cột "Tỉnh giao" của một mẫu.
type ProvincialImportColumn struct {
	Header     string
	TemplateID uuid.UUID
	FormCode   string
	Letter     string
}

// ProvincialImport là kết quả đọc tệp (chưa ghi gì): giá trị để đưa vào Bước 4 rồi phát hành với
// provincial_source = excel. Phạm vi xã của đợt vẫn do xem trước/phát hành kiểm.
type ProvincialImport struct {
	Values   map[uuid.UUID]map[string]map[string]string
	Columns  []ProvincialImportColumn
	Ignored  []string // tiêu đề cột không nhận ra (bỏ qua)
	Rows     []ProvincialImportRow
	Accepted int
	Rejected int
}

// Nhãn trạng thái dòng.
const (
	PVRowAccepted = "accepted"
	PVRowRejected = "rejected"
)

var provincialCommuneAliases = map[string]string{
	"ma_xa": "commune", "ma_xa_phuong": "commune", "commune_code": "commune", "ma_dvhc": "commune", "ma_xa_phuong_dac_khu": "commune",
	"ten_xa": "name", "ten_xa_phuong": "name", "commune_name": "name",
}

// ImportProvincial đọc tệp CSV UTF-8/.xlsx (shared/tabular) chứa giá trị cột "Tỉnh giao" (FN-TI-02.02 Bước 4,
// UR-TI-02 "nạp từ Excel"). Tệp một dòng một xã: cột "Mã xã" bắt buộc, mỗi cột Tỉnh giao có tiêu đề
// "<Mã biểu> <Chữ cột>" (vd "T01 E" hoặc "T01_E Diện tích giao"); khi chỉ chọn một biểu, tiêu đề "E"/"Cột E" cũng nhận.
// Ô công thức → cả tệp bị từ chối (tabular). Số phải là decimal không âm; dòng lỗi bị loại kèm lý do, dòng khác vẫn nhận.
func (s *Service) ImportProvincial(ctx context.Context, reportType, from string, templateIDs []uuid.UUID, src io.Reader) (ProvincialImport, error) {
	a, err := s.writer(ctx, port.SessionRoleProvince)
	if err != nil {
		return ProvincialImport{}, err
	}
	if s.Forms == nil {
		return ProvincialImport{}, ErrDependency.WithDetail("chưa nối engine mẫu báo cáo (pf/reports)")
	}
	if _, ok := domain.ReportPeriodType[reportType]; !ok {
		return ProvincialImport{}, problem.Validation.WithField("report_type", "weekly | monthly | seasonal | annual | adhoc")
	}
	if len(templateIDs) == 0 {
		return ProvincialImport{}, problem.Validation.WithField("template_ids", "chọn ít nhất một biểu")
	}
	at := s.now()
	if from != "" {
		if at, err = domain.ParseDate(from); err != nil {
			return ProvincialImport{}, problem.Validation.WithField("from_date", err.Error())
		}
	}
	forms, err := s.Forms.AssignedForms(ctx, a.p.TenantID, a.k.Department, repport.LevelCommune, reportType, at)
	if err != nil {
		return ProvincialImport{}, err
	}
	var picked []repport.FormVersion
	for _, id := range templateIDs {
		i := slices.IndexFunc(forms, func(f repport.FormVersion) bool { return f.TemplateID == id })
		if i < 0 {
			return ProvincialImport{}, ErrFormNotAssigned.WithDetail(id.String())
		}
		picked = append(picked, forms[i])
	}
	lim := tabular.Limits{MaxBytes: MaxProvincialImportBytes, MaxRows: MaxProvincialImportRows}
	tb, err := tabular.Read(src, lim)
	if err != nil {
		return ProvincialImport{}, ErrProvincialImportInvalid.WithDetail(tabular.Message(err, lim))
	}
	return parseProvincial(tb, picked)
}

type pvTarget struct {
	tpl    uuid.UUID
	code   string
	letter string
	dtype  string
}

// parseProvincial ghép tiêu đề cột với cột Tỉnh giao của các biểu đã chọn rồi kiểm từng dòng.
func parseProvincial(tb *tabular.Table, forms []repport.FormVersion) (ProvincialImport, error) {
	col := tb.Columns(provincialCommuneAliases)
	if missing := tabular.Missing(col, "commune"); len(missing) > 0 {
		return ProvincialImport{}, ErrProvincialImportInvalid.WithDetail("Tệp thiếu cột bắt buộc: Mã xã")
	}
	// Ứng viên tiêu đề (đã chuẩn hóa) → cột Tỉnh giao.
	cands := map[string]pvTarget{}
	nProv := 0
	for _, f := range forms {
		for _, c := range f.Definition.Columns {
			if c.Kind != repport.ColumnProvincial {
				continue
			}
			nProv++
			t := pvTarget{tpl: f.TemplateID, code: f.Code, letter: strings.ToUpper(c.Letter), dtype: c.DType}
			cands[tabular.NormalizeHeader(f.Code+" "+c.Letter)] = t
			cands[tabular.NormalizeHeader(f.Code+" cột "+c.Letter)] = t
			if len(forms) == 1 {
				cands[tabular.NormalizeHeader(c.Letter)] = t
				cands[tabular.NormalizeHeader("cột "+c.Letter)] = t
			}
		}
	}
	if nProv == 0 {
		return ProvincialImport{}, ErrProvincialImportInvalid.WithDetail("Các biểu đã chọn không có cột Tỉnh giao")
	}
	out := ProvincialImport{Values: map[uuid.UUID]map[string]map[string]string{}, Columns: []ProvincialImportColumn{}, Ignored: []string{}, Rows: []ProvincialImportRow{}}
	targets := map[int]pvTarget{}
	seenTarget := map[string]string{}
	for i, k := range tb.Keys {
		if _, ok := provincialCommuneAliases[k]; ok {
			continue
		}
		t, ok := matchProvincial(cands, k)
		if !ok {
			out.Ignored = append(out.Ignored, tb.Header[i])
			continue
		}
		id := t.tpl.String() + "|" + t.letter
		if prev, dup := seenTarget[id]; dup {
			return ProvincialImport{}, ErrProvincialImportInvalid.WithDetail("Cột \"" + tb.Header[i] + "\" trùng với cột \"" + prev + "\"")
		}
		seenTarget[id] = tb.Header[i]
		targets[i] = t
		out.Columns = append(out.Columns, ProvincialImportColumn{Header: tb.Header[i], TemplateID: t.tpl, FormCode: t.code, Letter: t.letter})
	}
	if len(targets) == 0 {
		return ProvincialImport{}, ErrProvincialImportInvalid.WithDetail("Không nhận ra cột Tỉnh giao nào — tiêu đề cột dạng \"<Mã biểu> <Chữ cột>\", vd \"T01 E\"")
	}
	seen := map[string]int{}
	for _, rec := range tb.Rows {
		row := ProvincialImportRow{Line: rec.Line, Commune: strings.TrimSpace(rec.Get(col, "commune")), Status: PVRowRejected}
		reject := func(reason string) {
			row.Reason = reason
			out.Rejected++
			out.Rows = append(out.Rows, row)
		}
		if row.Commune == "" {
			reject("Thiếu mã xã")
			continue
		}
		if prev, dup := seen[row.Commune]; dup {
			reject("Mã xã trùng với dòng " + strconv.Itoa(prev))
			continue
		}
		seen[row.Commune] = rec.Line
		cells := map[uuid.UUID]map[string]string{}
		bad := ""
		for i, t := range targets {
			if i >= len(rec.Cells) {
				continue
			}
			v := strings.TrimSpace(rec.Cells[i])
			if v == "" {
				continue
			}
			if t.dtype == repport.DTypeNumber || t.dtype == repport.DTypeInteger {
				d, err := decimal.NewFromString(strings.ReplaceAll(v, " ", ""))
				if err != nil || d.IsNegative() || (t.dtype == repport.DTypeInteger && !d.IsInteger()) {
					bad = t.code + " cột " + t.letter + ": phải là số không âm"
					break
				}
				v = d.String()
			}
			if cells[t.tpl] == nil {
				cells[t.tpl] = map[string]string{}
			}
			cells[t.tpl][t.letter] = v
		}
		if bad != "" {
			reject(bad)
			continue
		}
		for tpl, m := range cells {
			if out.Values[tpl] == nil {
				out.Values[tpl] = map[string]map[string]string{}
			}
			out.Values[tpl][row.Commune] = m
		}
		row.Status = PVRowAccepted
		out.Accepted++
		out.Rows = append(out.Rows, row)
	}
	return out, nil
}

// matchProvincial: tiêu đề khớp đúng ứng viên, hoặc bắt đầu bằng ứng viên + "_" (phần sau là tên cột tùy ý).
func matchProvincial(cands map[string]pvTarget, key string) (pvTarget, bool) {
	if t, ok := cands[key]; ok {
		return t, true
	}
	best, bestLen := pvTarget{}, 0
	for k, t := range cands {
		if strings.HasPrefix(key, k+"_") && len(k) > bestLen {
			best, bestLen = t, len(k)
		}
	}
	return best, bestLen > 0
}
