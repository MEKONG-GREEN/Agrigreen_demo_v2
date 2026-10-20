// Package app là use case "Số liệu nguồn ngoài" KHTC (W11.67 — BRD KHTC v2.2 FN-TI-02.11, BR-DL-19, BR-DL-04; D4 KH7):
// danh mục chỉ tiêu nguồn ngoài, "Thêm số liệu" / "Sửa" (lý do bắt buộc, lịch sử cũ → mới), "Rút", "Tải mẫu Excel",
// "Nạp Excel tổng hợp" hai bước (kiểm → Hoàn tất), tệp minh chứng ở kho đối tượng. Mọi ghi là bản ghi thêm; sửa = bản
// ghi bù (supersedes). Màn soạn báo cáo (W11.66, extension/khtcreport) đọc giá trị hiệu lực của kỳ khi "Cập nhật số
// liệu"; đổi giá trị sau khi xuất → "Nguồn đã thay đổi" (BR-DL-04) qua băm đầu vào của bản soạn.
package app

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcext/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcext/store"
	pstore "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcperiod/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/objstore"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// Lỗi (docs/errors/pf-extension-khtc-external.md).
var (
	ErrNotFound           = problem.New(http.StatusNotFound, "extension.khtc_ext_not_found", "Không tìm thấy số liệu, chỉ tiêu, tệp hoặc lần nạp")
	ErrClosed             = problem.New(http.StatusConflict, "extension.khtc_ext_period_closed", "Kỳ đã đóng hoặc đã hủy — số liệu nguồn ngoài chỉ xem")
	ErrFinal              = problem.New(http.StatusConflict, "extension.khtc_report_final", "Báo cáo đã có Bản cuối, chỉ được xem và tải lại")
	ErrDuplicate          = problem.New(http.StatusConflict, "extension.khtc_ext_duplicate", "Chỉ tiêu đã có giá trị trong kỳ: xác nhận ghi đè và nhập lý do")
	ErrConflict           = problem.New(http.StatusConflict, "extension.khtc_ext_conflict", "Số liệu vừa được người khác thay đổi, tải lại trước khi sửa")
	ErrIndicatorDuplicate = problem.New(http.StatusConflict, "extension.khtc_ext_indicator_duplicate", "Mã hoặc tên chỉ tiêu đã có trong danh mục")
	ErrInvalid            = problem.New(http.StatusUnprocessableEntity, "extension.khtc_ext_invalid", "Số liệu nguồn ngoài không hợp lệ")
	ErrImport             = problem.New(http.StatusUnprocessableEntity, "extension.khtc_ext_import_invalid", "Không đọc được tệp nạp")
	ErrImportCommitted    = problem.New(http.StatusConflict, "extension.khtc_ext_import_committed", "Lần nạp đã hoàn tất")
	ErrEvidence           = problem.New(http.StatusUnprocessableEntity, "extension.khtc_ext_evidence_invalid", "Tệp minh chứng không hợp lệ")
	ErrDependency         = problem.New(http.StatusServiceUnavailable, "extension.dependency_unavailable", "Dịch vụ phụ thuộc chưa sẵn sàng")
	errNotKHTC            = problem.Forbidden.WithDetail("Số liệu nguồn ngoài dành cho Phòng Kế hoạch – Tài chính (Cấp Tỉnh) và Admin xem")
	errNotWriter          = problem.Forbidden.WithDetail("chỉ cán bộ Cấp Tỉnh Phòng Kế hoạch – Tài chính nhập số liệu nguồn ngoài")
)

// Service là use case số liệu nguồn ngoài.
type Service struct {
	DB    tenant.Beginner
	Clock clock.Clock
	// Store: kho đối tượng cho tệp minh chứng (tùy chọn; thiếu → tải tệp trả 503, phần còn lại vẫn chạy).
	Store objstore.Store
	// Names: tên người thao tác cho lịch sử (tùy chọn).
	Names func() partyport.ActorNames
}

type caller struct {
	p authz.Principal
	k port.SessionContext
}

func (s *Service) reader(ctx context.Context) (caller, error) {
	if s.DB == nil {
		return caller{}, ErrDependency
	}
	p, k, err := port.RequireSession(ctx)
	if err != nil {
		return caller{}, err
	}
	if k.Role == port.SessionRoleAdmin || (k.Department == partyport.DeptKeHoachTaiChinh && (k.Role == port.SessionRoleProvince || k.Role == port.SessionRoleNational)) {
		return caller{p: p, k: k}, nil
	}
	return caller{}, errNotKHTC
}

func canWrite(c caller) bool {
	return c.k.Role == port.SessionRoleProvince && c.k.Department == partyport.DeptKeHoachTaiChinh && !c.k.ReadOnly()
}

func (s *Service) writer(ctx context.Context) (caller, error) {
	c, err := s.reader(ctx)
	if err != nil {
		return c, err
	}
	if !canWrite(c) {
		return c, errNotWriter
	}
	return c, nil
}

func (s *Service) tx(ctx context.Context, tid uuid.UUID, fn func(pgx.Tx) error) error {
	return tenant.WithTenantTx(ctx, s.DB, tid, fn)
}

func (s *Service) now() time.Time { return s.Clock.Now().UTC() }

func (s *Service) today() time.Time {
	n := s.now().In(clock.Zone())
	return time.Date(n.Year(), n.Month(), n.Day(), 0, 0, 0, 0, time.UTC)
}

// classify đổi lỗi ràng buộc của trigger/bảng thành lỗi nghiệp vụ.
func classify(err error) error {
	var pg *pgconn.PgError
	if !errors.As(err, &pg) {
		return err
	}
	switch pg.ConstraintName {
	case "khtc_ext_period_fkey", "khtc_ext_import_missing":
		return ErrNotFound
	case "khtc_ext_period_closed":
		return ErrClosed
	case "khtc_report_already_final":
		return ErrFinal
	case "khtc_ext_figure_stale":
		return ErrConflict
	case "khtc_ext_indicator_fkey":
		return ErrInvalid.WithField("indicator_id", "chỉ tiêu không có trong danh mục")
	case "khtc_ext_evidence_fkey":
		return ErrInvalid.WithField("evidence_ids", "tệp minh chứng không có")
	case "khtc_ext_indicator_code", "khtc_ext_indicator_name":
		return ErrIndicatorDuplicate
	case "khtc_ext_import_committed":
		return ErrImportCommitted
	}
	return err
}

func invalid(err error) error {
	var fe domain.FieldErrors
	if errors.As(err, &fe) {
		p := ErrInvalid
		for _, f := range fe {
			p = p.WithField(f.Field, f.Reason)
		}
		return p
	}
	return ErrInvalid.WithCause(err)
}

// periodInfo là kỳ KHTC kèm trạng thái kết thúc.
type periodInfo struct {
	pstore.Period
	Closed    bool
	Cancelled bool
}

func periodsOf(ctx context.Context, tx pgx.Tx, year int) (map[uuid.UUID]periodInfo, error) {
	ps, err := pstore.Periods(ctx, tx, year, 10000)
	if err != nil {
		return nil, err
	}
	idList := make([]uuid.UUID, 0, len(ps))
	for _, p := range ps {
		idList = append(idList, p.ID)
	}
	evs, err := pstore.Events(ctx, tx, idList)
	if err != nil {
		return nil, err
	}
	out := map[uuid.UUID]periodInfo{}
	for _, p := range ps {
		pi := periodInfo{Period: p}
		for _, e := range evs[p.ID] {
			switch e.Action {
			case pstore.ActionClosed:
				pi.Closed = true
			case pstore.ActionCancelled:
				pi.Closed, pi.Cancelled = true, true
			}
		}
		out[p.ID] = pi
	}
	return out, nil
}

func periodOne(ctx context.Context, tx pgx.Tx, id uuid.UUID) (periodInfo, error) {
	p, err := pstore.PeriodByID(ctx, tx, id)
	if err != nil {
		return periodInfo{}, err
	}
	if p == nil {
		return periodInfo{}, ErrNotFound.WithDetail("Không có kỳ báo cáo")
	}
	all, err := periodsOf(ctx, tx, p.Year)
	if err != nil {
		return periodInfo{}, err
	}
	return all[id], nil
}

func (s *Service) names(ctx context.Context, refs []uuid.UUID) map[uuid.UUID]string {
	if s.Names == nil || len(refs) == 0 {
		return map[uuid.UUID]string{}
	}
	n := s.Names()
	if n == nil {
		return map[uuid.UUID]string{}
	}
	m, err := n.DisplayNames(ctx, refs)
	if err != nil || m == nil {
		return map[uuid.UUID]string{}
	}
	return m
}

// ---------- Danh mục chỉ tiêu ----------

// Indicators liệt kê danh mục chỉ tiêu nguồn ngoài.
func (s *Service) Indicators(ctx context.Context) ([]store.Indicator, error) {
	c, err := s.reader(ctx)
	if err != nil {
		return nil, err
	}
	var out []store.Indicator
	err = s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error { var e error; out, e = store.Indicators(ctx, tx); return e })
	return out, err
}

// AddIndicator thêm chỉ tiêu mới ngay trên màn (tên, ĐVT, nhóm; mã tự sinh nếu bỏ trống).
func (s *Service) AddIndicator(ctx context.Context, in domain.IndicatorInput, idem string) (store.Indicator, error) {
	if idem == "" {
		return store.Indicator{}, problem.IdempotencyRequired
	}
	c, err := s.writer(ctx)
	if err != nil {
		return store.Indicator{}, err
	}
	id := ids.New()
	in, err = in.Normalize(func() string { h := strings.ReplaceAll(id.String(), "-", ""); return "ng-" + h[len(h)-10:] })
	if err != nil {
		return store.Indicator{}, invalid(err)
	}
	row := store.Indicator{ID: id, Code: in.Code, Name: in.Name, Unit: in.Unit, Group: in.Group, ActorRef: c.p.ActorRef}
	err = s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
		old, e := store.IndicatorByIdem(ctx, tx, idem)
		if e != nil || old != nil {
			if old != nil {
				row = *old
			}
			return e
		}
		return store.InsertIndicator(ctx, tx, c.p.TenantID, &row, idem)
	})
	return row, classify(err)
}

// ---------- Tệp minh chứng ----------

var evidenceTypes = map[string]string{".pdf": "application/pdf", ".png": "image/png", ".jpg": "image/jpeg", ".jpeg": "image/jpeg",
	".docx": "application/vnd.openxmlformats-officedocument.wordprocessingml.document", ".xlsx": "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
	".doc": "application/msword", ".xls": "application/vnd.ms-excel", ".eml": "message/rfc822", ".txt": "text/plain"}

// sniff khớp chữ ký nội dung với phần mở rộng (không tin tên tệp).
func sniff(name string, data []byte) (string, bool) {
	ext := strings.ToLower(filepath.Ext(name))
	ct, ok := evidenceTypes[ext]
	if !ok {
		return "", false
	}
	switch ext {
	case ".pdf":
		return ct, bytes.HasPrefix(data, []byte("%PDF-"))
	case ".png":
		return ct, bytes.HasPrefix(data, []byte("\x89PNG\r\n\x1a\n"))
	case ".jpg", ".jpeg":
		return ct, bytes.HasPrefix(data, []byte{0xFF, 0xD8, 0xFF})
	case ".docx", ".xlsx":
		return ct, bytes.HasPrefix(data, []byte("PK\x03\x04"))
	case ".doc", ".xls":
		return ct, bytes.HasPrefix(data, []byte{0xD0, 0xCF, 0x11, 0xE0})
	}
	// .eml, .txt: văn bản UTF-8, không có byte 0.
	return ct, utf8.Valid(data) && !bytes.ContainsRune(data, 0)
}

func cleanFileName(name string) string {
	name = filepath.Base(strings.ReplaceAll(strings.TrimSpace(name), "\\", "/"))
	name = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f || strings.ContainsRune(`"<>|?*:`, r) {
			return -1
		}
		return r
	}, name)
	if utf8.RuneCountInString(name) > 200 {
		ext := filepath.Ext(name)
		rs := []rune(strings.TrimSuffix(name, ext))
		name = string(rs[:200-utf8.RuneCountInString(ext)]) + ext
	}
	return name
}

// UploadEvidence lưu tệp minh chứng (PDF, ảnh, Word/Excel, email) vào kho đối tượng ghi một lần, khóa theo băm; ghi siêu
// dữ liệu. Văn bản của cơ quan ngoài không chứa dữ liệu cá nhân — không mã hóa phong bì theo hộ (objstore R-40 ngoại lệ
// tài liệu không định danh, như pf/content).
func (s *Service) UploadEvidence(ctx context.Context, fileName string, data []byte, idem string) (store.Evidence, bool, error) {
	if idem == "" {
		return store.Evidence{}, false, problem.IdempotencyRequired
	}
	c, err := s.writer(ctx)
	if err != nil {
		return store.Evidence{}, false, err
	}
	if s.Store == nil {
		return store.Evidence{}, false, ErrDependency.WithDetail("chưa cấu hình kho đối tượng cho tệp minh chứng")
	}
	name := cleanFileName(fileName)
	if len(data) == 0 || name == "" || name == "." {
		return store.Evidence{}, false, ErrEvidence.WithDetail("tệp rỗng hoặc thiếu tên tệp")
	}
	if len(data) > domain.MaxEvidenceSize {
		return store.Evidence{}, false, ErrEvidence.WithDetail("tối đa 15 MB mỗi tệp")
	}
	ct, ok := sniff(name, data)
	if !ok {
		return store.Evidence{}, false, ErrEvidence.WithDetail("chỉ nhận PDF, ảnh JPEG/PNG, Word, Excel, email (.eml) hoặc văn bản; nội dung phải khớp phần mở rộng")
	}
	sum := objstore.SHA256Hex(data)
	key := "khtc-ext/" + c.p.TenantID.String() + "/" + sum
	if _, err := s.Store.Put(ctx, key, bytes.NewReader(data), int64(len(data)), sum, ct); err != nil && !errors.Is(err, objstore.ErrExists) {
		return store.Evidence{}, false, err
	}
	row := store.Evidence{ID: ids.New(), FileName: name, ContentType: ct, Size: int64(len(data)), SHA256: sum, ObjectKey: key, ActorRef: c.p.ActorRef}
	var replayed bool
	err = s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
		var e error
		replayed, e = store.InsertEvidence(ctx, tx, c.p.TenantID, &row, idem)
		return e
	})
	return row, replayed, err
}

// OpenEvidence trả siêu dữ liệu và nội dung một tệp minh chứng (người đọc KHTC, Admin).
func (s *Service) OpenEvidence(ctx context.Context, id uuid.UUID) (store.Evidence, []byte, error) {
	c, err := s.reader(ctx)
	if err != nil {
		return store.Evidence{}, nil, err
	}
	if s.Store == nil {
		return store.Evidence{}, nil, ErrDependency.WithDetail("chưa cấu hình kho đối tượng cho tệp minh chứng")
	}
	var ev store.Evidence
	err = s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
		m, e := store.EvidenceByIDs(ctx, tx, []uuid.UUID{id})
		if e != nil {
			return e
		}
		var ok bool
		if ev, ok = m[id]; !ok {
			return ErrNotFound
		}
		return nil
	})
	if err != nil {
		return ev, nil, err
	}
	r, info, err := s.Store.Get(ctx, ev.ObjectKey)
	if err != nil {
		return ev, nil, ErrDependency.WithCause(err)
	}
	defer func() { _ = r.Close() }()
	data, err := io.ReadAll(io.LimitReader(r, domain.MaxEvidenceSize+1))
	if err != nil {
		return ev, nil, err
	}
	if !strings.EqualFold(info.SHA256, ev.SHA256) && objstore.SHA256Hex(data) != ev.SHA256 {
		return ev, nil, ErrDependency.WithDetail("băm tệp minh chứng không khớp")
	}
	return ev, data, nil
}

// ---------- Số liệu ----------

// EvidenceView là tệp minh chứng gắn với số liệu.
type EvidenceView struct {
	ID          uuid.UUID `json:"id"`
	FileName    string    `json:"file_name"`
	ContentType string    `json:"content_type"`
	Size        int64     `json:"size"`
}

// FigureView là một dòng của danh sách "Số liệu nguồn ngoài".
type FigureView struct {
	EntryID     uuid.UUID      `json:"entry_id"`
	Version     int64          `json:"version"`
	PeriodID    uuid.UUID      `json:"period_id"`
	PeriodName  string         `json:"period_name"`
	PeriodType  string         `json:"period_type"`
	Year        int            `json:"year"`
	Closed      bool           `json:"closed"`
	IndicatorID uuid.UUID      `json:"indicator_id"`
	Code        string         `json:"code"`
	Name        string         `json:"name"`
	Group       string         `json:"group"`
	Value       string         `json:"value"`
	Unit        string         `json:"unit"`
	Agency      string         `json:"agency"`
	Document    string         `json:"document"`
	SourceDate  string         `json:"source_date"`
	Evidence    []EvidenceView `json:"evidence"`
	UsedIn      string         `json:"used_in"`
	ActorRef    uuid.UUID      `json:"actor_ref"`
	ActorName   string         `json:"actor_name"`
	At          time.Time      `json:"at"`
	Editable    bool           `json:"editable"`
}

// Query là bộ lọc danh sách (B1: năm, kỳ, cơ quan cung cấp, tìm chỉ tiêu).
type Query struct {
	Year     int
	PeriodID *uuid.UUID
	Agency   string
	Search   string
}

func validYear(y int) error {
	if y < 2000 || y > 2100 {
		return ErrInvalid.WithField("year", "2000–2100")
	}
	return nil
}

func dateText(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.Format(time.DateOnly)
}

// views dựng dòng hiển thị từ bản mới nhất (bỏ bản đã rút).
func (s *Service) views(ctx context.Context, tx pgx.Tx, c caller, latest []store.Entry, periods map[uuid.UUID]periodInfo) ([]FigureView, error) {
	inds, err := store.Indicators(ctx, tx)
	if err != nil {
		return nil, err
	}
	byID := map[uuid.UUID]store.Indicator{}
	for _, i := range inds {
		byID[i.ID] = i
	}
	var evIDs, actors []uuid.UUID
	for _, e := range latest {
		evIDs = append(evIDs, e.Evidence...)
		actors = append(actors, e.ActorRef)
	}
	evs, err := store.EvidenceByIDs(ctx, tx, evIDs)
	if err != nil {
		return nil, err
	}
	names := s.names(ctx, actors)
	out := []FigureView{}
	for _, e := range latest {
		p, ok := periods[e.PeriodID]
		if e.Action != domain.ActionSet || e.Value == nil || !ok {
			continue
		}
		ind := byID[e.IndicatorID]
		v := FigureView{EntryID: e.ID, Version: e.Seq, PeriodID: e.PeriodID, PeriodName: domain.ShortName(p.Name), PeriodType: p.Type, Year: p.Year,
			Closed: p.Closed, IndicatorID: e.IndicatorID, Code: ind.Code, Name: ind.Name, Group: ind.Group, Value: e.Value.String(), Unit: e.Unit,
			Agency: e.Agency, Document: e.Document, SourceDate: dateText(e.SourceDate), Evidence: []EvidenceView{}, UsedIn: domain.Template(p.Type),
			ActorRef: e.ActorRef, ActorName: names[e.ActorRef], At: e.At, Editable: canWrite(c) && !p.Closed}
		for _, id := range e.Evidence {
			if f, ok := evs[id]; ok {
				v.Evidence = append(v.Evidence, EvidenceView{ID: f.ID, FileName: f.FileName, ContentType: f.ContentType, Size: f.Size})
			}
		}
		out = append(out, v)
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.PeriodName != b.PeriodName {
			return a.PeriodName < b.PeriodName
		}
		return strings.ToLower(a.Name) < strings.ToLower(b.Name)
	})
	return out, nil
}

// List là danh sách giá trị hiệu lực theo bộ lọc.
func (s *Service) List(ctx context.Context, q Query) ([]FigureView, error) {
	c, err := s.reader(ctx)
	if err != nil {
		return nil, err
	}
	if err := validYear(q.Year); err != nil {
		return nil, err
	}
	var out []FigureView
	err = s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
		periods, e := periodsOf(ctx, tx, q.Year)
		if e != nil {
			return e
		}
		pids := make([]uuid.UUID, 0, len(periods))
		for id := range periods {
			if q.PeriodID == nil || *q.PeriodID == id {
				pids = append(pids, id)
			}
		}
		latest, e := store.Latest(ctx, tx, pids)
		if e != nil {
			return e
		}
		out, e = s.views(ctx, tx, c, latest, periods)
		return e
	})
	if err != nil {
		return nil, err
	}
	agency, search := strings.ToLower(strings.TrimSpace(q.Agency)), domain.NameKey(q.Search)
	out = slices.DeleteFunc(out, func(v FigureView) bool {
		return (agency != "" && !strings.Contains(strings.ToLower(v.Agency), agency)) ||
			(search != "" && !strings.Contains(domain.NameKey(v.Name), search) && !strings.Contains(strings.ToLower(v.Code), search))
	})
	return out, nil
}

// SaveInput là "Thêm số liệu" / "Sửa" (B2, B4). Overwrite: người dùng đã xác nhận ghi đè khi trùng chỉ tiêu và kỳ.
type SaveInput struct {
	domain.FigureInput
	Overwrite   bool   `json:"overwrite"`
	BaseVersion *int64 `json:"base_version"`
}

// Save ghi một giá trị: lần đầu của chỉ tiêu × kỳ → bản 1; đã có → cần xác nhận ghi đè (Overwrite) và lý do ≥ 10 ký tự,
// ghi bản bù trỏ bản hiệu lực (lịch sử cũ → mới). Kỳ đã đóng → 409 (BR-DL-19).
func (s *Service) Save(ctx context.Context, in SaveInput, idem string) (FigureView, error) {
	if idem == "" {
		return FigureView{}, problem.IdempotencyRequired
	}
	c, err := s.writer(ctx)
	if err != nil {
		return FigureView{}, err
	}
	f, err := in.Validate(s.today())
	if err != nil {
		return FigureView{}, invalid(err)
	}
	var out FigureView
	err = s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
		if old, e := store.EntryByIdem(ctx, tx, idem); e != nil || old != nil {
			if e != nil {
				return e
			}
			return s.viewOne(ctx, tx, c, *old, &out)
		}
		p, e := periodOne(ctx, tx, f.PeriodID)
		if e != nil {
			return e
		}
		if p.Closed {
			return ErrClosed
		}
		cur, e := currentOf(ctx, tx, f.PeriodID, f.IndicatorID)
		if e != nil {
			return e
		}
		entry := store.Entry{ID: ids.New(), PeriodID: f.PeriodID, IndicatorID: f.IndicatorID, Action: domain.ActionSet, Value: &f.Value,
			Unit: f.Unit, Agency: f.Source.Agency, Document: f.Source.Document, SourceDate: &f.Source.Date, Evidence: f.Evidence,
			ActorRef: c.p.ActorRef, At: s.now()}
		if cur != nil {
			if in.BaseVersion != nil && *in.BaseVersion != cur.Seq {
				return ErrConflict
			}
			if cur.Action == domain.ActionSet && in.BaseVersion == nil && !in.Overwrite {
				return ErrDuplicate.WithDetail("Giá trị hiện có: "+cur.Value.String()+" "+cur.Unit).With("current_version", cur.Seq)
			}
			if domain.NeedsReason(true, f.Reason) {
				return ErrInvalid.WithField("reason", "sửa hoặc ghi đè bắt buộc lý do (tối thiểu 10 ký tự)")
			}
			id := cur.ID
			entry.Supersedes, entry.Reason = &id, f.Reason
		} else if in.BaseVersion != nil && *in.BaseVersion != 0 {
			return ErrConflict
		}
		if entry.Unit == "" {
			if inds, e := store.Indicators(ctx, tx); e == nil {
				for _, i := range inds {
					if i.ID == f.IndicatorID {
						entry.Unit = i.Unit
					}
				}
			}
		}
		if e := store.InsertEntry(ctx, tx, c.p.TenantID, &entry, idem); e != nil {
			return e
		}
		return s.viewOne(ctx, tx, c, entry, &out)
	})
	return out, classify(err)
}

// WithdrawInput là "Rút số liệu" (ghi bù không có giá trị, lý do bắt buộc).
type WithdrawInput struct {
	PeriodID    uuid.UUID `json:"period_id"`
	IndicatorID uuid.UUID `json:"indicator_id"`
	BaseVersion int64     `json:"base_version"`
	Reason      string    `json:"reason"`
}

// Withdraw rút giá trị hiệu lực của chỉ tiêu × kỳ (nhập nhầm chỉ tiêu/kỳ): bản ghi bù 'withdraw'; ô Nhập ngoài của bản
// soạn hết giá trị sau "Cập nhật số liệu".
func (s *Service) Withdraw(ctx context.Context, in WithdrawInput, idem string) error {
	if idem == "" {
		return problem.IdempotencyRequired
	}
	c, err := s.writer(ctx)
	if err != nil {
		return err
	}
	reason := strings.TrimSpace(in.Reason)
	if utf8.RuneCountInString(reason) < domain.MinReason || utf8.RuneCountInString(reason) > 1000 {
		return ErrInvalid.WithField("reason", "lý do 10–1000 ký tự")
	}
	err = s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
		if old, e := store.EntryByIdem(ctx, tx, idem); e != nil || old != nil {
			return e
		}
		p, e := periodOne(ctx, tx, in.PeriodID)
		if e != nil {
			return e
		}
		if p.Closed {
			return ErrClosed
		}
		cur, e := currentOf(ctx, tx, in.PeriodID, in.IndicatorID)
		if e != nil {
			return e
		}
		if cur == nil || cur.Action != domain.ActionSet {
			return ErrNotFound
		}
		if cur.Seq != in.BaseVersion {
			return ErrConflict
		}
		id := cur.ID
		entry := store.Entry{ID: ids.New(), PeriodID: in.PeriodID, IndicatorID: in.IndicatorID, Action: domain.ActionWithdraw, Unit: cur.Unit,
			Supersedes: &id, Reason: reason, ActorRef: c.p.ActorRef, At: s.now()}
		return store.InsertEntry(ctx, tx, c.p.TenantID, &entry, idem)
	})
	return classify(err)
}

func currentOf(ctx context.Context, tx pgx.Tx, period, indicator uuid.UUID) (*store.Entry, error) {
	latest, err := store.Latest(ctx, tx, []uuid.UUID{period})
	if err != nil {
		return nil, err
	}
	for _, e := range latest {
		if e.IndicatorID == indicator {
			return &e, nil
		}
	}
	return nil, nil
}

func (s *Service) viewOne(ctx context.Context, tx pgx.Tx, c caller, e store.Entry, out *FigureView) error {
	p, err := periodOne(ctx, tx, e.PeriodID)
	if err != nil {
		return err
	}
	vs, err := s.views(ctx, tx, c, []store.Entry{e}, map[uuid.UUID]periodInfo{p.ID: p})
	if err != nil {
		return err
	}
	if len(vs) == 1 {
		*out = vs[0]
	}
	return nil
}

// HistoryView là một dòng "Lịch sử": cũ → mới, lý do, người, lúc.
type HistoryView struct {
	EntryID       uuid.UUID  `json:"entry_id"`
	Version       int64      `json:"version"`
	Action        string     `json:"action"`
	PeriodID      uuid.UUID  `json:"period_id"`
	PeriodName    string     `json:"period_name"`
	IndicatorID   uuid.UUID  `json:"indicator_id"`
	IndicatorName string     `json:"indicator_name"`
	Old           *string    `json:"old_value"`
	New           *string    `json:"new_value"`
	Unit          string     `json:"unit"`
	Reason        string     `json:"reason"`
	ImportID      *uuid.UUID `json:"import_id"`
	ActorRef      uuid.UUID  `json:"actor_ref"`
	ActorName     string     `json:"actor_name"`
	At            time.Time  `json:"at"`
}

// History liệt kê lịch sử nhập/sửa của năm (mới trước).
func (s *Service) History(ctx context.Context, year int, period *uuid.UUID, limit int) ([]HistoryView, error) {
	c, err := s.reader(ctx)
	if err != nil {
		return nil, err
	}
	if err := validYear(year); err != nil {
		return nil, err
	}
	out := []HistoryView{}
	err = s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
		periods, e := periodsOf(ctx, tx, year)
		if e != nil {
			return e
		}
		pids := []uuid.UUID{}
		for id := range periods {
			if period == nil || *period == id {
				pids = append(pids, id)
			}
		}
		hist, e := store.History(ctx, tx, pids, limit)
		if e != nil {
			return e
		}
		var prevIDs, actors []uuid.UUID
		for _, h := range hist {
			if h.Supersedes != nil {
				prevIDs = append(prevIDs, *h.Supersedes)
			}
			actors = append(actors, h.ActorRef)
		}
		prev, e := store.EntriesByIDs(ctx, tx, prevIDs)
		if e != nil {
			return e
		}
		inds, e := store.Indicators(ctx, tx)
		if e != nil {
			return e
		}
		indName := map[uuid.UUID]string{}
		for _, i := range inds {
			indName[i.ID] = i.Name
		}
		names := s.names(ctx, actors)
		for _, h := range hist {
			v := HistoryView{EntryID: h.ID, Version: h.Seq, Action: h.Action, PeriodID: h.PeriodID, PeriodName: domain.ShortName(periods[h.PeriodID].Name),
				IndicatorID: h.IndicatorID, IndicatorName: indName[h.IndicatorID], Unit: h.Unit, Reason: h.Reason, ImportID: h.ImportID,
				ActorRef: h.ActorRef, ActorName: names[h.ActorRef], At: h.At}
			if h.Value != nil {
				n := h.Value.String()
				v.New = &n
			}
			if h.Supersedes != nil {
				if p, ok := prev[*h.Supersedes]; ok && p.Value != nil {
					o := p.Value.String()
					v.Old = &o
				}
			}
			out = append(out, v)
		}
		return nil
	})
	return out, err
}
