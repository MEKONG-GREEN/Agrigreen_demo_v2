package app

// BF-TT-MAT (#105, TT-12) "Nạp Excel" vật tư: đọc tệp CSV UTF-8/.xlsx (shared/tabular — ô công thức → cả tệp bị
// từ chối) thành các dòng của bảng nhập trên web. CHỈ ĐỌC, không ghi sổ: người dùng xem lại trong bảng rồi bấm Lưu
// (POST /v1/kn/materials, máy chủ kiểm lại toàn bộ). Lý do lỗi từng dòng không chứa giá trị nhập.

import (
	"context"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/material/domain"
	mdport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tabular"
)

// Giới hạn tệp nạp vật tư (một lần Lưu tối đa domain.MaxLines dòng).
const (
	MaxImportBytes = 2 << 20
	MaxImportRows  = domain.MaxLines
)

// ErrImportInvalid: tệp sai định dạng hoặc thiếu cột bắt buộc (cả tệp bị từ chối).
var ErrImportInvalid = problem.New(http.StatusUnprocessableEntity, "extension.material_import_invalid", "Tệp vật tư không đúng mẫu")

// Trạng thái dòng tệp nạp.
const (
	ImportAccepted = "accepted"
	ImportRejected = "rejected"
)

// ImportLine là một dòng tệp đã đọc. Dòng bị loại vẫn trả mã/tên vật tư nếu nhận ra để người dùng sửa trong bảng.
type ImportLine struct {
	Row          int
	MaterialCode string
	MaterialName string
	Unit         string
	Quantity     string // decimal đã chuẩn hóa; rỗng nếu không đọc được
	RecordedOn   string // YYYY-MM-DD; rỗng = dùng Thời điểm đang chọn trên màn
	Note         string
	Status       string
	Reason       string
}

// ImportResult là kết quả đọc tệp.
type ImportResult struct {
	Lines    []ImportLine
	Accepted int
	Rejected int
}

var importAliases = map[string]string{
	"ma_vat_tu": "material", "vat_tu": "material", "loai_vat_tu": "material", "ten_vat_tu": "material", "material_code": "material", "material": "material",
	"so_luong": "quantity", "quantity": "quantity", "sl": "quantity",
	"thoi_diem": "date", "thoi_diem_ghi_nhan": "date", "ngay": "date", "ngay_ghi_nhan": "date", "recorded_on": "date",
	"ghi_chu": "note", "note": "note",
	"don_vi_tinh": "unit", "dvt": "unit", "don_vi": "unit", "unit": "unit",
}

// ParseImport đọc tệp vật tư cho bảng nhập (Cấp Xã, Admin — cùng quyền với ghi nhận). Không ghi gì.
func (s *Service) ParseImport(ctx context.Context, src io.Reader) (ImportResult, error) {
	if _, err := s.writer(ctx); err != nil {
		return ImportResult{}, err
	}
	if s.Catalogs == nil {
		return ImportResult{}, ErrDependency.WithDetail("chưa nối danh mục vật tư")
	}
	items, err := s.Catalogs.List(ctx, mdport.CatalogMaterial)
	if err != nil {
		return ImportResult{}, ErrDependency.WithDetail("không tra được danh mục vật tư").WithCause(err)
	}
	lim := tabular.Limits{MaxBytes: MaxImportBytes, MaxRows: MaxImportRows}
	tb, err := tabular.Read(src, lim)
	if err != nil {
		return ImportResult{}, ErrImportInvalid.WithDetail(tabular.Message(err, lim))
	}
	return ParseImportTable(tb, items, domain.Today(s.now()))
}

// ParseImportTable ghép cột theo bí danh rồi kiểm từng dòng theo danh mục (mục đang dùng có ĐVT) và quy tắc ĐVT.
func ParseImportTable(tb *tabular.Table, items []mdport.CatalogItem, today time.Time) (ImportResult, error) {
	col := tb.Columns(importAliases)
	if missing := tabular.Missing(col, "material", "quantity"); len(missing) > 0 {
		names := map[string]string{"material": "Vật tư", "quantity": "Số lượng"}
		out := make([]string, 0, len(missing))
		for _, m := range missing {
			out = append(out, names[m])
		}
		return ImportResult{}, ErrImportInvalid.WithDetail("Tệp thiếu cột bắt buộc: " + strings.Join(out, ", "))
	}
	byCode := map[string]mdport.CatalogItem{}
	byName := map[string]mdport.CatalogItem{}
	for _, it := range items {
		if !it.Active || strings.TrimSpace(it.Attrs["unit"]) == "" {
			continue
		}
		byCode[strings.ToLower(strings.TrimSpace(it.Code))] = it
		byName[tabular.NormalizeHeader(it.Name)] = it
	}
	res := ImportResult{Lines: []ImportLine{}}
	for _, rec := range tb.Rows {
		l := ImportLine{Row: rec.Line, Status: ImportRejected}
		var reasons []string
		raw := strings.TrimSpace(rec.Get(col, "material"))
		it, ok := byCode[strings.ToLower(raw)]
		if !ok {
			it, ok = byName[tabular.NormalizeHeader(raw)]
		}
		unit := ""
		switch {
		case raw == "":
			reasons = append(reasons, "thiếu vật tư")
		case !ok:
			reasons = append(reasons, "vật tư không có trong danh mục đang dùng")
		default:
			unit = strings.TrimSpace(it.Attrs["unit"])
			l.MaterialCode, l.MaterialName, l.Unit = it.Code, it.Name, unit
		}
		if q := strings.TrimSpace(rec.Get(col, "quantity")); q == "" {
			reasons = append(reasons, "thiếu số lượng")
		} else if d, err := parseQuantity(q); err != nil {
			reasons = append(reasons, "số lượng không phải số")
		} else if e := domain.CheckQuantity(d, unit); e != nil {
			reasons = append(reasons, e.Error())
		} else {
			l.Quantity = d.String()
		}
		if fileUnit := strings.TrimSpace(rec.Get(col, "unit")); fileUnit != "" && unit != "" && !strings.EqualFold(fileUnit, unit) {
			reasons = append(reasons, "đơn vị tính khác danh mục ("+unit+")")
		}
		if ds := strings.TrimSpace(rec.Get(col, "date")); ds != "" {
			if d, ok := parseDay(ds); !ok {
				reasons = append(reasons, "thời điểm phải là ngày dd/mm/yyyy")
			} else if d.After(today) {
				reasons = append(reasons, "thời điểm không được ở tương lai")
			} else {
				l.RecordedOn = d.Format(time.DateOnly)
			}
		}
		note := strings.TrimSpace(rec.Get(col, "note"))
		if r := domain.NoteError(note); r != "" {
			reasons = append(reasons, "ghi chú: "+r)
		} else {
			l.Note = note
		}
		if len(reasons) == 0 {
			l.Status = ImportAccepted
			res.Accepted++
		} else {
			l.Reason = strings.Join(reasons, "; ")
			res.Rejected++
		}
		res.Lines = append(res.Lines, l)
	}
	return res, nil
}

// parseQuantity nhận "1234.5", "1 234,5", "1.234,5" (kiểu Việt Nam) — không qua float (R-10).
func parseQuantity(s string) (decimal.Decimal, error) {
	s = strings.ReplaceAll(strings.ReplaceAll(s, " ", ""), " ", "")
	if strings.Contains(s, ",") {
		s = strings.ReplaceAll(strings.ReplaceAll(s, ".", ""), ",", ".")
	}
	return decimal.NewFromString(s)
}

var dayLayouts = []string{time.DateOnly, "2/1/2006", "02/01/2006", "2-1-2006", "02-01-2006"}

func parseDay(s string) (time.Time, bool) {
	for _, layout := range dayLayouts {
		if d, err := time.Parse(layout, s); err == nil {
			return d, true
		}
	}
	return time.Time{}, false
}
