package app

import (
	"bytes"
	"context"
	"errors"
	"slices"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcext/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcext/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/objstore"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tabular"
)

// importHeader là cột của "Tải mẫu Excel" (FN-TI-02.11 B3). "Mã chỉ tiêu" hoặc "Chỉ tiêu" (tên) đều được.
var importHeader = []string{"Mã chỉ tiêu", "Chỉ tiêu", "Kỳ", "Giá trị", "ĐVT", "Cơ quan cung cấp", "Văn bản / kênh", "Ngày (YYYY-MM-DD)"}

var importAliases = map[string]string{}

func init() {
	keys := []string{"code", "indicator", "period", "value", "unit", "agency", "document", "date"}
	for i, h := range importHeader {
		importAliases[tabular.NormalizeHeader(h)] = keys[i]
		importAliases[keys[i]] = keys[i]
	}
	for alias, key := range map[string]string{"ngay": "date", "nguon_co_quan": "agency", "nguon": "agency", "co_quan": "agency",
		"van_ban_kenh": "document", "van_ban": "document", "kenh": "document", "don_vi_tinh": "unit", "ky_bao_cao": "period", "ten_chi_tieu": "indicator"} {
		importAliases[alias] = key
	}
}

// Template là tệp mẫu "Tải mẫu Excel": trang nhập + danh mục chỉ tiêu + kỳ còn mở của năm.
func (s *Service) Template(ctx context.Context, year int) ([]byte, string, error) {
	c, err := s.reader(ctx)
	if err != nil {
		return nil, "", err
	}
	if err := validYear(year); err != nil {
		return nil, "", err
	}
	var inds []store.Indicator
	var periods map[uuid.UUID]periodInfo
	if err := s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
		var e error
		if inds, e = store.Indicators(ctx, tx); e != nil {
			return e
		}
		periods, e = periodsOf(ctx, tx, year)
		return e
	}); err != nil {
		return nil, "", err
	}
	head := func(xs ...string) []tabular.Cell {
		out := make([]tabular.Cell, len(xs))
		for i, x := range xs {
			out[i] = tabular.Cell{Text: x, Bold: true}
		}
		return out
	}
	text := func(xs ...string) []tabular.Cell {
		out := make([]tabular.Cell, len(xs))
		for i, x := range xs {
			out[i] = tabular.Cell{Text: x}
		}
		return out
	}
	input := tabular.Sheet{Name: "So lieu", Rows: [][]tabular.Cell{head(importHeader...)}, Widths: []int{16, 40, 22, 14, 12, 30, 30, 18}}
	catalog := tabular.Sheet{Name: "Chi tieu", Rows: [][]tabular.Cell{head("Mã chỉ tiêu", "Chỉ tiêu", "ĐVT", "Nhóm")}, Widths: []int{16, 50, 12, 24}}
	for _, i := range inds {
		catalog.Rows = append(catalog.Rows, text(i.Code, i.Name, i.Unit, i.Group))
	}
	open := tabular.Sheet{Name: "Ky", Rows: [][]tabular.Cell{head("Kỳ (ghi vào cột Kỳ)", "Mã kỳ", "Từ ngày", "Đến ngày")}, Widths: []int{40, 18, 14, 14}}
	list := make([]periodInfo, 0, len(periods))
	for _, p := range periods {
		if !p.Closed {
			list = append(list, p)
		}
	}
	sort.Slice(list, func(i, j int) bool {
		return list[i].To.Before(list[j].To) || list[i].To.Equal(list[j].To) && list[i].ID.String() < list[j].ID.String()
	})
	for _, p := range list {
		key := ""
		if p.Key != nil {
			key = *p.Key
		}
		open.Rows = append(open.Rows, text(domain.ShortName(p.Name), key, p.From.Format("2006-01-02"), p.To.Format("2006-01-02")))
	}
	var buf bytes.Buffer
	if err := tabular.WriteSheetsXLSX(&buf, []tabular.Sheet{input, catalog, open}); err != nil {
		return nil, "", err
	}
	return buf.Bytes(), "mau-so-lieu-nguon-ngoai-" + strconv.Itoa(year) + ".xlsx", nil
}

// ImportView là kết quả một bước nạp.
type ImportView struct {
	ImportID  uuid.UUID          `json:"import_id"`
	Year      int                `json:"year"`
	FileName  string             `json:"file_name"`
	Status    string             `json:"status"` // staged | committed
	Result    domain.CheckResult `json:"result"`
	Recorded  int                `json:"recorded"`
	Skipped   int                `json:"skipped"`
	Overwrite bool               `json:"overwrite"`
}

func viewOf(i store.Import) ImportView {
	status := "staged"
	if i.Action == "committed" {
		status = "committed"
	}
	return ImportView{ImportID: i.ImportID, Year: i.Year, FileName: i.FileName, Status: status, Result: i.Payload.Result,
		Recorded: i.Payload.Recorded, Skipped: i.Payload.Skipped, Overwrite: i.Payload.Overwrite}
}

// checkState kiểm lại các dòng với danh mục, kỳ và giá trị hiệu lực hiện tại (dùng ở cả hai bước).
func (s *Service) checkState(ctx context.Context, tx pgx.Tx, year int, raw []domain.RawRow) (domain.CheckResult, error) {
	periods, err := periodsOf(ctx, tx, year)
	if err != nil {
		return domain.CheckResult{}, err
	}
	refs := make([]domain.PeriodRef, 0, len(periods))
	pids := make([]uuid.UUID, 0, len(periods))
	for _, p := range periods {
		key := ""
		if p.Key != nil {
			key = *p.Key
		}
		refs = append(refs, domain.PeriodRef{ID: p.ID, Type: p.Type, Year: p.Year, Key: key, Name: p.Name, Closed: p.Closed, Cancelled: p.Cancelled})
		pids = append(pids, p.ID)
	}
	inds, err := store.Indicators(ctx, tx)
	if err != nil {
		return domain.CheckResult{}, err
	}
	irefs := make([]domain.IndicatorRef, 0, len(inds))
	for _, i := range inds {
		irefs = append(irefs, domain.IndicatorRef{ID: i.ID, Code: i.Code, Name: i.Name, Unit: i.Unit})
	}
	latest, err := store.Latest(ctx, tx, pids)
	if err != nil {
		return domain.CheckResult{}, err
	}
	current := map[string]string{}
	for _, e := range latest {
		if e.Action == domain.ActionSet && e.Value != nil {
			current[domain.FigureKey(e.PeriodID, e.IndicatorID)] = e.Value.String()
		}
	}
	return domain.CheckRows(raw, refs, irefs, current, s.today()), nil
}

// Stage là bước 1 của "Nạp Excel tổng hợp": đọc tệp, kiểm từng dòng, lưu kết quả (KHÔNG ghi số liệu).
func (s *Service) Stage(ctx context.Context, year int, fileName string, data []byte, idem string) (ImportView, error) {
	if idem == "" {
		return ImportView{}, problem.IdempotencyRequired
	}
	c, err := s.writer(ctx)
	if err != nil {
		return ImportView{}, err
	}
	if err := validYear(year); err != nil {
		return ImportView{}, err
	}
	name := cleanFileName(fileName)
	if name == "" || name == "." {
		name = "so-lieu-nguon-ngoai.xlsx"
	}
	lim := tabular.Limits{MaxBytes: domain.MaxImportBytes, MaxRows: domain.MaxImportRows, MaxCols: 32}
	t, err := tabular.ReadBytes(data, lim)
	if err != nil {
		return ImportView{}, ErrImport.WithDetail(tabular.Message(err, lim))
	}
	col := t.Columns(importAliases)
	if _, hasCode := col["code"]; !hasCode {
		if miss := tabular.Missing(col, "indicator"); len(miss) > 0 {
			return ImportView{}, ErrImport.WithDetail("thiếu cột: Mã chỉ tiêu hoặc Chỉ tiêu")
		}
	}
	if miss := tabular.Missing(col, "period", "value", "agency", "document", "date"); len(miss) > 0 {
		return ImportView{}, ErrImport.WithDetail("thiếu cột: " + strings.Join(miss, ", ") + " (dùng Tải mẫu Excel)")
	}
	raw := make([]domain.RawRow, 0, len(t.Rows))
	for _, r := range t.Rows {
		get := func(k string) string { return strings.TrimSpace(r.Get(col, k)) }
		ind := get("code")
		if ind == "" {
			ind = get("indicator")
		}
		row := domain.RawRow{Line: r.Line, Indicator: ind, Period: get("period"), Value: get("value"), Unit: get("unit"),
			Agency: get("agency"), Document: get("document"), SourceDate: get("date")}
		for _, f := range []*string{&row.Indicator, &row.Period, &row.Value, &row.Unit, &row.Agency, &row.Document, &row.SourceDate} {
			if utf8.RuneCountInString(*f) > 500 {
				*f = string([]rune(*f)[:500])
			}
		}
		raw = append(raw, row)
	}
	if len(raw) == 0 {
		return ImportView{}, ErrImport.WithDetail("tệp không có dòng số liệu")
	}
	rec := store.Import{ID: ids.New(), ImportID: ids.New(), Year: year, Action: "staged", FileName: name, FileSHA: objstore.SHA256Hex(data),
		ActorRef: c.p.ActorRef, At: s.now()}
	err = s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
		if old, e := store.ImportByIdem(ctx, tx, idem); e != nil || old != nil {
			if old != nil {
				rec = *old
			}
			return e
		}
		res, e := s.checkState(ctx, tx, year, raw)
		if e != nil {
			return e
		}
		rec.Payload = store.ImportPayload{Raw: raw, Result: res}
		return store.InsertImport(ctx, tx, c.p.TenantID, &rec, idem)
	})
	if err != nil {
		return ImportView{}, classify(err)
	}
	return viewOf(rec), nil
}

// GetImport đọc một lần nạp (bước mới nhất).
func (s *Service) GetImport(ctx context.Context, importID uuid.UUID) (ImportView, error) {
	c, err := s.reader(ctx)
	if err != nil {
		return ImportView{}, err
	}
	var out ImportView
	err = s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
		for _, action := range []string{"committed", "staged"} {
			i, e := store.ImportStep(ctx, tx, importID, action)
			if e != nil {
				return e
			}
			if i != nil {
				out = viewOf(*i)
				return nil
			}
		}
		return ErrNotFound
	})
	return out, err
}

// CommitInput là bước 2 "Hoàn tất". Overwrite: ghi đè các dòng trùng chỉ tiêu và kỳ (bắt buộc Reason); không chọn →
// bỏ qua các dòng đó.
type CommitInput struct {
	Overwrite bool   `json:"overwrite"`
	Reason    string `json:"reason"`
}

// Commit là bước 2: kiểm lại với trạng thái hiện tại (kỳ vừa đóng, người khác vừa nhập), ghi các dòng hợp lệ trong MỘT
// giao dịch, đánh dấu lần nạp đã hoàn tất (một lần duy nhất).
func (s *Service) Commit(ctx context.Context, importID uuid.UUID, in CommitInput, idem string) (ImportView, error) {
	if idem == "" {
		return ImportView{}, problem.IdempotencyRequired
	}
	c, err := s.writer(ctx)
	if err != nil {
		return ImportView{}, err
	}
	reason := strings.TrimSpace(in.Reason)
	if in.Overwrite && (utf8.RuneCountInString(reason) < domain.MinReason || utf8.RuneCountInString(reason) > 1000) {
		return ImportView{}, ErrInvalid.WithField("reason", "ghi đè bắt buộc lý do 10–1000 ký tự")
	}
	var out ImportView
	err = s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
		if old, e := store.ImportByIdem(ctx, tx, idem); e != nil || old != nil {
			if old != nil {
				if old.ImportID != importID || old.Action != "committed" {
					return problem.IdempotencyConflict
				}
				out = viewOf(*old)
			}
			return e
		}
		if e := store.LockImport(ctx, tx, c.p.TenantID, importID); e != nil {
			return e
		}
		staged, e := store.ImportStep(ctx, tx, importID, "staged")
		if e != nil {
			return e
		}
		if staged == nil {
			return ErrNotFound
		}
		if done, e := store.ImportStep(ctx, tx, importID, "committed"); e != nil || done != nil {
			if done != nil {
				return ErrImportCommitted
			}
			return e
		}
		res, e := s.checkState(ctx, tx, staged.Year, staged.Payload.Raw)
		if e != nil {
			return e
		}
		recorded, skipped := 0, 0
		for i, r := range res.Rows {
			if r.Status == domain.RowError || (r.Status == domain.RowOverwrite && !in.Overwrite) {
				if r.Status == domain.RowOverwrite {
					skipped++
				}
				continue
			}
			f, ferr := domain.FigureInput{PeriodID: *r.PeriodID, IndicatorID: *r.IndicatorID, Value: r.Value, Unit: r.Unit, Agency: r.Agency,
				Document: r.Document, SourceDate: r.SourceDate}.Validate(s.today())
			if ferr != nil {
				return invalid(ferr)
			}
			imp := importID
			entry := store.Entry{ID: ids.New(), PeriodID: f.PeriodID, IndicatorID: f.IndicatorID, Action: domain.ActionSet, Value: &f.Value, Unit: f.Unit,
				Agency: f.Source.Agency, Document: f.Source.Document, SourceDate: &f.Source.Date, Evidence: []uuid.UUID{}, ImportID: &imp,
				ActorRef: c.p.ActorRef, At: s.now()}
			cur, e := currentOf(ctx, tx, f.PeriodID, f.IndicatorID)
			if e != nil {
				return e
			}
			if cur != nil {
				id := cur.ID
				entry.Supersedes, entry.Reason = &id, reason
				if cur.Action != domain.ActionSet {
					entry.Reason = "Nạp lại sau khi rút: " + staged.FileName
					if utf8.RuneCountInString(reason) >= domain.MinReason {
						entry.Reason = reason
					}
				}
			}
			if e := store.InsertEntry(ctx, tx, c.p.TenantID, &entry, idem+":"+strconv.Itoa(i)); e != nil {
				return e
			}
			recorded++
		}
		rec := store.Import{ID: ids.New(), ImportID: importID, Year: staged.Year, Action: "committed", FileName: staged.FileName, FileSHA: staged.FileSHA,
			Payload:  store.ImportPayload{Raw: staged.Payload.Raw, Result: res, Overwrite: in.Overwrite, Reason: reason, Recorded: recorded, Skipped: skipped},
			ActorRef: c.p.ActorRef, At: s.now()}
		if e := store.InsertImport(ctx, tx, c.p.TenantID, &rec, idem); e != nil {
			return e
		}
		out = viewOf(rec)
		return nil
	})
	if err != nil {
		var pe *problem.Problem
		if errors.As(err, &pe) {
			return ImportView{}, err
		}
		return ImportView{}, classify(err)
	}
	return out, nil
}

// ReportFigure là một giá trị hiệu lực dùng ở màn soạn báo cáo (ô Nhập ngoài) — đọc trong giao dịch của bên gọi.
type ReportFigure struct {
	Entry     store.Entry
	Indicator store.Indicator
	Evidence  []store.Evidence
}

// ReportFigures trả giá trị hiệu lực (không gồm bản đã rút) của một kỳ, theo tên chỉ tiêu. Gọi trong giao dịch tenant
// của khtcreport (cùng module, cùng schema).
func ReportFigures(ctx context.Context, tx pgx.Tx, period uuid.UUID) ([]ReportFigure, error) {
	latest, err := store.Latest(ctx, tx, []uuid.UUID{period})
	if err != nil || len(latest) == 0 {
		return nil, err
	}
	inds, err := store.Indicators(ctx, tx)
	if err != nil {
		return nil, err
	}
	byID := map[uuid.UUID]store.Indicator{}
	for _, i := range inds {
		byID[i.ID] = i
	}
	var evIDs []uuid.UUID
	for _, e := range latest {
		evIDs = append(evIDs, e.Evidence...)
	}
	evs, err := store.EvidenceByIDs(ctx, tx, evIDs)
	if err != nil {
		return nil, err
	}
	out := []ReportFigure{}
	for _, e := range latest {
		if e.Action != domain.ActionSet || e.Value == nil {
			continue
		}
		f := ReportFigure{Entry: e, Indicator: byID[e.IndicatorID], Evidence: []store.Evidence{}}
		for _, id := range e.Evidence {
			if v, ok := evs[id]; ok {
				f.Evidence = append(f.Evidence, v)
			}
		}
		out = append(out, f)
	}
	slices.SortStableFunc(out, func(a, b ReportFigure) int {
		if c := strings.Compare(domain.NameKey(a.Indicator.Name), domain.NameKey(b.Indicator.Name)); c != 0 {
			return c
		}
		return strings.Compare(a.Indicator.Code, b.Indicator.Code)
	})
	return out, nil
}
