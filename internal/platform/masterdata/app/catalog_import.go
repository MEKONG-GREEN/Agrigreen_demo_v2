package app

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tabular"
)

// Giới hạn nhập danh mục hàng loạt (HTX US-CAT-02) — xử lý đồng bộ < 2 giây (R-52).
const (
	MaxCatalogImportBytes = 2 << 20
	MaxCatalogImportRows  = 500
)

// ErrCatalogImportInvalid: tệp sai mẫu — từ chối cả tệp (mã lỗi masterdata.catalog_import_invalid).
var ErrCatalogImportInvalid = problem.New(http.StatusUnprocessableEntity, "masterdata.catalog_import_invalid",
	"Tệp không đúng mẫu quy định, vui lòng tải mẫu chuẩn và thử lại")

// Trạng thái một dòng nhập.
const (
	ImportRowCreated  = "created"
	ImportRowSkipped  = "skipped" // mục đã có, cùng nội dung — nhập lại tệp không tạo trùng
	ImportRowRejected = "rejected"
)

// CatalogImportRow là kết quả một dòng. Reason không chứa dữ liệu cá nhân (danh mục là dữ liệu chủ).
type CatalogImportRow struct {
	Row     int    `json:"row"`
	Status  string `json:"status"`
	Catalog string `json:"catalog,omitempty"`
	Code    string `json:"code,omitempty"`
	Reason  string `json:"reason,omitempty"`
}

// CatalogImportResult là kết quả cả tệp (US-CAT-02 AC-1/AC-2).
type CatalogImportResult struct {
	ID       uuid.UUID          `json:"import_id"`
	Total    int                `json:"total"`
	Created  int                `json:"created"`
	Skipped  int                `json:"skipped"`
	Rejected int                `json:"rejected"`
	Rows     []CatalogImportRow `json:"rows"`
	Replayed bool               `json:"replayed"`
}

// Cột của tệp mẫu (tiêu đề đã chuẩn hóa bằng tabular.NormalizeHeader). Cột khác
// là thuộc tính của mục (tên thuộc tính = tiêu đề đã chuẩn hóa, vd growth_days);
// ô trống bỏ qua.
var catalogImportAliases = map[string]string{
	"catalog": "catalog", "danh_muc": "catalog",
	"code": "code", "ma": "code", "ma_muc": "code",
	"name": "name", "ten": "name", "ten_muc": "name",
	// W11.41: ngày áp dụng của danh mục CNTS (YYYY-MM-DD; trống = hôm nay).
	"valid_from": "valid_from", "hieu_luc_tu": "valid_from", "ngay_hieu_luc": "valid_from",
	// BF-CN-CATALOG #170: cột trạng thái của tệp "Xuất danh mục" chỉ để đọc — nạp lại tệp xuất thì bỏ qua cột này (không
	// coi là thuộc tính, không đổi trạng thái; ngừng/dùng lại qua thao tác riêng có lý do).
	"trang_thai": "status", "status": "status",
}

// ImportCatalogs nạp nhiều mục danh mục từ CSV/XLSX (HTX US-CAT-02). Tệp sai mẫu →
// từ chối cả tệp; dòng lỗi bị loại kèm lý do, dòng hợp lệ vẫn được tạo — trong MỘT
// giao dịch cùng bản ghi kết quả catalog_import (lỗi hệ thống giữa chừng → không
// dòng nào được ghi, gửi lại cùng Idempotency-Key). Mục đã có cùng nội dung → skipped;
// khác nội dung → rejected (sửa từng mục qua PUT để có lịch sử trước → sau).
func (s *Service) ImportCatalogs(ctx context.Context, body io.Reader, key string) (CatalogImportResult, error) {
	p, err := authz.Require(ctx, port.RoleMasterdataAdmin)
	if err != nil {
		return CatalogImportResult{}, err
	}
	if key == "" {
		return CatalogImportResult{}, problem.IdempotencyRequired
	}
	lim := tabular.Limits{MaxBytes: MaxCatalogImportBytes, MaxRows: MaxCatalogImportRows}
	raw, err := io.ReadAll(io.LimitReader(body, lim.MaxBytes+1))
	if err != nil {
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) {
			return CatalogImportResult{}, ErrCatalogImportInvalid.WithDetail(tabular.Message(tabular.ErrTooLarge, lim))
		}
		return CatalogImportResult{}, err
	}
	tb, err := tabular.ReadBytes(raw, lim)
	if err != nil {
		return CatalogImportResult{}, ErrCatalogImportInvalid.WithDetail(tabular.Message(err, lim))
	}
	col := tb.Columns(catalogImportAliases)
	if missing := tabular.Missing(col, "catalog", "code", "name"); len(missing) > 0 {
		return CatalogImportResult{}, ErrCatalogImportInvalid.WithDetail("Thiếu cột bắt buộc: " + strings.Join(missing, ", "))
	}
	attrCols := map[int]string{}
	for i, k := range tb.Keys {
		if _, known := catalogImportAliases[k]; !known && k != "" {
			attrCols[i] = k
		}
	}
	sum := sha256.Sum256(raw)
	now := s.Clock.Now()
	res := CatalogImportResult{ID: ids.New(), Rows: []CatalogImportRow{}}
	err = s.tx(ctx, p.TenantID, func(tx pgx.Tx) error {
		if prev, err := store.CatalogImportByKey(ctx, tx, key); err != nil {
			return err
		} else if prev != nil {
			if res, err = fromStore(prev); err != nil {
				return err
			}
			res.Replayed = true
			return nil
		}
		seen := map[string]int{}
		for _, rec := range tb.Rows {
			row := CatalogImportRow{Row: rec.Line, Status: ImportRowRejected}
			it := domain.Item{Catalog: strings.TrimSpace(rec.Get(col, "catalog")), Code: rec.Get(col, "code"), Name: rec.Get(col, "name"), Attrs: map[string]string{}}
			for i, k := range attrCols {
				if i < len(rec.Cells) && strings.TrimSpace(rec.Cells[i]) != "" {
					it.Attrs[k] = strings.TrimSpace(rec.Cells[i])
				}
			}
			row.Catalog, row.Code = it.Catalog, strings.TrimSpace(it.Code)
			if vf := strings.TrimSpace(rec.Get(col, "valid_from")); vf != "" {
				d, err := domain.ParseDay(vf)
				if err == nil {
					err = domain.ValidateValidFrom(d)
				}
				if err != nil || !s.catalogEffective(it.Catalog) {
					row.Reason = s.invalidCatalogEffectiveDate()
					res.add(row)
					continue
				}
				it.ValidFrom = d
			}
			// Ô bị tệp xuất thêm dấu nháy chống công thức được so với mục đã có trước khi chuẩn hóa (độ dài tên/thuộc tính).
			if prev, err := store.Item(ctx, tx, it.Catalog, it.Code, false); err != nil {
				return err
			} else if prev != nil {
				it = csvFormulaEscapedAs(it, *prev)
			}
			norm, err := s.normalizeCatalogItem(s.prepareCatalogIdentity(it))
			if err != nil {
				row.Reason = importReason(err)
				res.add(row)
				continue
			}
			norm.ValidFrom = s.effectiveFrom(norm)
			row.Code = norm.Code
			k := norm.Catalog + "/" + norm.Code
			if prev, dup := seen[k]; dup {
				row.Reason = "Trùng với dòng " + strconv.Itoa(prev) + " trong tệp"
				res.add(row)
				continue
			}
			seen[k] = rec.Line
			cur, err := store.Item(ctx, tx, norm.Catalog, norm.Code, true)
			if err != nil {
				return err
			}
			if cur != nil {
				norm = s.keepCatalogIdentity(norm, *cur)
				if cur.SameContent(norm) {
					row.Status = ImportRowSkipped
				} else {
					row.Reason = "Mã đã có trong danh mục với nội dung khác — sửa từng mục trên màn hình quản trị"
				}
				res.add(row)
				continue
			}
			if err := s.checkRefs(ctx, tx, norm, norm.ValidFrom); err != nil {
				if pr := problem.From(err); pr.Status == http.StatusUnprocessableEntity {
					row.Reason = pr.Title
					res.add(row)
					continue
				}
				return err
			}
			norm.Active, norm.Revision, norm.UpdatedBy, norm.UpdatedAt = true, 1, p.ActorRef, now
			c := &domain.Change{ID: ids.New(), Catalog: norm.Catalog, Code: norm.Code, Action: domain.ActionCreated,
				After: norm.Snapshot(), Reason: "Nhập danh mục hàng loạt", EventTime: now, ActorRef: p.ActorRef,
				IdemKey: key + ":row:" + strconv.Itoa(rec.Line)}
			if err := s.writeChange(ctx, tx, p.TenantID, c, norm); err != nil {
				return err
			}
			row.Status = ImportRowCreated
			res.add(row)
		}
		rows, err := json.Marshal(res.Rows)
		if err != nil {
			return err
		}
		return store.InsertCatalogImport(ctx, tx, p.TenantID, store.CatalogImport{
			ID: res.ID, ActorRef: p.ActorRef, Format: tb.Format, FileSHA256: sum[:], Total: res.Total, Created: res.Created,
			Skipped: res.Skipped, Rejected: res.Rejected, Rows: rows, IdemKey: key, EventTime: now,
		})
	})
	if err != nil {
		return CatalogImportResult{}, err
	}
	return res, nil
}

// csvFormulaEscapedAs: tệp "Xuất danh mục" thêm dấu nháy đơn trước ô bắt đầu bằng = + - @ tab CR để Excel không chạy
// công thức (#170). Khi so với mục đã có, ô đúng bằng "'" + giá trị đã lưu (giá trị đó bắt đầu bằng ký tự công thức)
// được coi là chính giá trị đã lưu, nên tệp vừa xuất nạp lại được. Chỉ áp khi mã đã có; dòng tạo mới giữ nguyên ô.
func csvFormulaEscapedAs(in, cur domain.Item) domain.Item {
	in.Name = csvUnescapeAs(in.Name, cur.Name)
	if len(in.Attrs) == 0 {
		return in
	}
	attrs := make(map[string]string, len(in.Attrs))
	for k, v := range in.Attrs {
		attrs[k] = csvUnescapeAs(v, cur.Attrs[k])
	}
	in.Attrs = attrs
	return in
}

func csvUnescapeAs(cell, stored string) string {
	if stored != "" && strings.ContainsRune("=+-@\t\r", rune(stored[0])) && strings.TrimSpace(cell) == "'"+stored {
		return stored
	}
	return cell
}

func (r *CatalogImportResult) add(row CatalogImportRow) {
	r.Total++
	switch row.Status {
	case ImportRowCreated:
		r.Created++
	case ImportRowSkipped:
		r.Skipped++
	default:
		r.Rejected++
	}
	r.Rows = append(r.Rows, row)
}

// importReason quy lỗi kiểm dữ liệu về lý do hiển thị theo dòng.
func importReason(err error) string {
	var fe domain.FieldError
	if errors.As(err, &fe) {
		return fe.Field + ": " + fe.Err.Error()
	}
	return problem.From(err).Title
}

// CatalogImport đọc lại kết quả một lần nhập (không cần tải tệp riêng, US-CAT-02 AC-2).
func (s *Service) CatalogImport(ctx context.Context, id uuid.UUID) (CatalogImportResult, error) {
	p, err := authz.Require(ctx, port.RoleMasterdataAdmin)
	if err != nil {
		return CatalogImportResult{}, err
	}
	var ci *store.CatalogImport
	err = s.tx(ctx, p.TenantID, func(tx pgx.Tx) error {
		var e error
		ci, e = store.CatalogImportByID(ctx, tx, id)
		return e
	})
	if err != nil {
		return CatalogImportResult{}, err
	}
	if ci == nil {
		return CatalogImportResult{}, ErrCatalogImportNotFound
	}
	return fromStore(ci)
}

func fromStore(ci *store.CatalogImport) (CatalogImportResult, error) {
	out := CatalogImportResult{ID: ci.ID, Total: ci.Total, Created: ci.Created, Skipped: ci.Skipped, Rejected: ci.Rejected, Rows: []CatalogImportRow{}}
	return out, json.Unmarshal(ci.Rows, &out.Rows)
}

// ErrCatalogImportNotFound: không có lần nhập với mã đó.
var ErrCatalogImportNotFound = problem.New(http.StatusNotFound, "masterdata.catalog_import_not_found", "Không có lần nhập danh mục này")
