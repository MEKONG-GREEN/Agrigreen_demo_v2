// Package app: use case "Nhập thông tin vật tư" KN v7.7 (FN-XA-03.06, gói P7) — Cấp Xã ghi nhận vật tư theo danh mục
// masterdata "material" (ĐVT = attrs.unit), sửa bằng bản ghi bù; Cấp Tỉnh/Quốc gia xem, tổng hợp theo xã. Quyền theo
// ngữ cảnh phiên KN (authz.RequireKN), không theo vai realm.
package app

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extension/material/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extension/material/store"
	mdport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata/port"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ledger"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// Mã lỗi (docs/errors.md, mục pf/extension — vật tư KN v7.7).
var (
	ErrNotFound        = problem.New(http.StatusNotFound, "extension.material_record_not_found", "Không tìm thấy bản ghi vật tư")
	ErrUnknown         = problem.New(http.StatusUnprocessableEntity, "extension.material_unknown", "Vật tư không thuộc danh mục Vật tư đang dùng")
	ErrQuantity        = problem.New(http.StatusUnprocessableEntity, "extension.material_quantity_invalid", "Số lượng không hợp lệ theo đơn vị tính")
	ErrSuperseded      = problem.New(http.StatusConflict, "extension.material_superseded", "Bản ghi đã được sửa bằng bản ghi khác — hãy sửa bản hiện hành")
	ErrReadOnly        = problem.New(http.StatusForbidden, "extension.read_only", "Cấp Quốc gia chỉ xem")
	ErrOutOfScope      = problem.New(http.StatusForbidden, "extension.out_of_scope", "Ngoài phạm vi phụ trách")
	ErrSupportReason   = problem.New(http.StatusUnprocessableEntity, "extension.support_reason_required", "Chế độ hỗ trợ: thao tác ghi bắt buộc lý do")
	ErrDependency      = problem.New(http.StatusServiceUnavailable, "extension.dependency_unavailable", "Dịch vụ phụ thuộc chưa sẵn sàng")
	errWriterForbidden = problem.Forbidden.WithDetail("chỉ Cấp Xã và Admin được ghi nhận vật tư")
)

// DefaultDepartment là phòng ban khi Admin ghi không chọn phòng ban (tạm, chờ khách).
const DefaultDepartment = "trong_trot"

var deptRe = regexp.MustCompile(`^[a-z][a-z0-9_]{1,39}$`)

// Service là use case sổ vật tư.
type Service struct {
	DB       tenant.Beginner // pool vai extension_app
	Clock    clock.Clock
	Catalogs mdport.Catalogs      // danh mục "material" (attrs.unit = ĐVT)
	Units    partyport.AdminUnits // tỉnh của xã (province_code)
	// Names (tùy chọn): tên người thao tác (actor_display); hàm để gốc lắp ráp nối sau.
	Names func() partyport.ActorNames
}

func (s *Service) now() time.Time { return s.Clock.Now().UTC() }

func (s *Service) tx(ctx context.Context, tid uuid.UUID, fn func(pgx.Tx) error) error {
	return tenant.WithTenantTx(ctx, s.DB, tid, fn)
}

type caller struct {
	p authz.Principal
	k authz.KNContext
}

func (s *Service) reader(ctx context.Context) (caller, error) {
	p, k, err := authz.RequireKN(ctx)
	return caller{p: p, k: k}, err
}

func (s *Service) writer(ctx context.Context) (caller, error) {
	c, err := s.reader(ctx)
	if err != nil {
		return c, err
	}
	switch {
	case c.k.ReadOnly():
		return c, ErrReadOnly
	case c.k.Role != authz.KNXa && c.k.Role != authz.KNAdmin:
		return c, errWriterForbidden
	case c.k.Support != nil && utf8.RuneCountInString(strings.TrimSpace(c.k.Support.Reason)) < 10:
		return c, ErrSupportReason
	}
	return c, nil
}

// readDept: Admin đọc mọi phòng ban (hoặc phòng ban chọn), vai khác phòng ban của phiên.
func (c caller) readDept(param string) string {
	if c.k.Role == authz.KNAdmin {
		return strings.TrimSpace(param)
	}
	if c.k.Department == "" {
		return "-" // không phòng ban → không thấy gì (mặc định chặn)
	}
	return c.k.Department
}

func (c caller) writeDept(param string) (string, error) {
	if c.k.Role != authz.KNAdmin {
		if c.k.Department == "" {
			return "", problem.Forbidden.WithDetail("ngữ cảnh phiên chưa có phòng ban")
		}
		return c.k.Department, nil
	}
	param = strings.TrimSpace(param)
	if param == "" {
		return DefaultDepartment, nil
	}
	if !deptRe.MatchString(param) {
		return "", problem.Validation.WithField("department", "mã phòng ban không hợp lệ")
	}
	return param, nil
}

func (c caller) sees(r store.Record) bool {
	return c.k.Scope.Allows(r.CommuneCode) && c.k.AllowsDepartment(r.DepartmentCode)
}

func (c caller) support() (*uuid.UUID, *string) {
	if c.k.Support == nil {
		return nil, nil
	}
	r := strings.TrimSpace(c.k.Support.Reason)
	if c.k.Support.OnBehalfOf == uuid.Nil {
		return nil, &r
	}
	v := c.k.Support.OnBehalfOf
	return &v, &r
}

// Names tra tên hiển thị người thao tác (lỗi không chặn màn hình).
func (s *Service) names(ctx context.Context, refs []uuid.UUID) map[uuid.UUID]string {
	out := map[uuid.UUID]string{}
	if s.Names == nil || len(refs) == 0 {
		return out
	}
	n := s.Names()
	if n == nil {
		return out
	}
	m, err := n.DisplayNames(ctx, refs)
	if err != nil {
		return out
	}
	return m
}

// View là bản ghi kèm tên người thao tác.
type View struct {
	store.Record
	ActorDisplay *string
}

func (s *Service) views(ctx context.Context, rs []store.Record) []View {
	refs := make([]uuid.UUID, 0, len(rs))
	for _, r := range rs {
		refs = append(refs, r.ActorRef)
	}
	m := s.names(ctx, refs)
	out := make([]View, 0, len(rs))
	for _, r := range rs {
		v := View{Record: r}
		if n, ok := m[r.ActorRef]; ok && n != "" {
			nn := n
			v.ActorDisplay = &nn
		}
		out = append(out, v)
	}
	return out
}

// ---------------------------------------------------------------------------
// Ghi nhận
// ---------------------------------------------------------------------------

// Line là một dòng vật tư gửi lên.
type Line struct {
	MaterialCode string
	Quantity     *decimal.Decimal
	Note         string
}

// RecordInput là một lần "Lưu" vật tư của xã.
type RecordInput struct {
	CommuneCode string
	RecordedOn  string
	Lines       []Line
	Department  string // Admin (tùy chọn)
	IdemKey     string
}

func lineKey(key string, i int) string { return fmt.Sprintf("%s:%d", key, i) }

func (s *Service) province(ctx context.Context, commune string) (string, error) {
	if s.Units == nil {
		return "", ErrDependency.WithDetail("chưa nối danh mục đơn vị hành chính")
	}
	u, err := s.Units.Get(ctx, commune, s.now())
	if errors.Is(err, partyport.ErrUnitNotFound) {
		return "", problem.Validation.WithField("commune_code", "không có đơn vị hành chính còn hiệu lực với mã này")
	}
	if err != nil {
		return "", ErrDependency.WithDetail("không tra được đơn vị hành chính").WithCause(err)
	}
	return u.ParentCode, nil
}

func (s *Service) material(ctx context.Context, field, code string) (mdport.CatalogItem, error) {
	if s.Catalogs == nil {
		return mdport.CatalogItem{}, ErrDependency.WithDetail("chưa nối danh mục vật tư")
	}
	it, err := s.Catalogs.Get(ctx, mdport.CatalogMaterial, code)
	if errors.Is(err, mdport.ErrCatalogItemNotFound) || (err == nil && !it.Active) {
		return it, ErrUnknown.WithField(field, "vật tư không có hoặc đã ngừng dùng trong danh mục")
	}
	if err != nil {
		return it, ErrDependency.WithDetail("không tra được danh mục vật tư").WithCause(err)
	}
	if strings.TrimSpace(it.Attrs["unit"]) == "" {
		return it, ErrUnknown.WithField(field, "mục danh mục chưa có đơn vị tính (attrs.unit)")
	}
	return it, nil
}

// Record ghi nhận 1–200 dòng vật tư của một xã tại ngày recorded_on (một giao dịch; POST lặp cùng khóa → cùng kết quả).
func (s *Service) Record(ctx context.Context, in RecordInput) ([]View, bool, error) {
	c, err := s.writer(ctx)
	if err != nil {
		return nil, false, err
	}
	if in.IdemKey == "" {
		return nil, false, problem.IdempotencyRequired
	}
	commune := strings.TrimSpace(in.CommuneCode)
	if commune == "" {
		return nil, false, problem.Validation.WithField("commune_code", "bắt buộc")
	}
	if !c.k.Scope.Allows(commune) {
		return nil, false, ErrOutOfScope.WithDetail("xã ngoài phạm vi phụ trách")
	}
	dept, err := c.writeDept(in.Department)
	if err != nil {
		return nil, false, err
	}
	on, err := domain.ParseDate(in.RecordedOn)
	if err != nil {
		return nil, false, problem.Validation.WithField("recorded_on", "phải là ngày YYYY-MM-DD")
	}
	now := s.now()
	if on.After(domain.Today(now)) {
		return nil, false, problem.Validation.WithField("recorded_on", "không được ở tương lai (giờ Việt Nam)")
	}
	if len(in.Lines) == 0 || len(in.Lines) > domain.MaxLines {
		return nil, false, problem.Validation.WithField("lines", "1–200 dòng")
	}
	keys := make([]string, len(in.Lines))
	for i := range in.Lines {
		keys[i] = lineKey(in.IdemKey, i)
	}
	if prev, err := s.replay(ctx, c, keys); err != nil || prev != nil {
		return prev, prev != nil, err
	}
	prov, err := s.province(ctx, commune)
	if err != nil {
		return nil, false, err
	}
	onBehalf, supReason := c.support()
	recs := make([]store.Record, 0, len(in.Lines))
	qerr, verr := ErrQuantity, problem.Validation
	badQ, badV := false, false
	for i, l := range in.Lines {
		f := func(n string) string { return fmt.Sprintf("lines.%d.%s", i, n) }
		code := strings.TrimSpace(l.MaterialCode)
		if code == "" {
			verr, badV = verr.WithField(f("material_code"), "bắt buộc"), true
			continue
		}
		it, err := s.material(ctx, f("material_code"), code)
		if err != nil {
			return nil, false, err
		}
		unit := strings.TrimSpace(it.Attrs["unit"])
		if l.Quantity == nil {
			verr, badV = verr.WithField(f("quantity"), "bắt buộc"), true
			continue
		}
		if e := domain.CheckQuantity(*l.Quantity, unit); e != nil {
			qerr, badQ = qerr.WithField(f("quantity"), e.Error()+" (ĐVT "+unit+")"), true
			continue
		}
		note := strings.TrimSpace(l.Note)
		if r := domain.NoteError(note); r != "" {
			verr, badV = verr.WithField(f("note"), r), true
			continue
		}
		var np *string
		if note != "" {
			np = &note
		}
		recs = append(recs, store.Record{ID: ids.New(), CommuneCode: commune, ProvinceCode: prov, DepartmentCode: dept, RecordedOn: on,
			MaterialCode: it.Code, MaterialName: it.Name, Quantity: *l.Quantity, Unit: unit, Note: np, Source: "manual",
			OnBehalfOf: onBehalf, SupportReason: supReason, EventTime: now, ActorRef: c.p.ActorRef, IdemKey: keys[i]})
	}
	if badV {
		return nil, false, verr
	}
	if badQ {
		return nil, false, qerr
	}
	err = s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
		for i := range recs {
			if err := store.Insert(ctx, tx, c.p.TenantID, &recs[i]); err != nil {
				return err
			}
		}
		return nil
	})
	if err = ledger.Classify(err); errors.Is(err, ledger.ErrDuplicate) {
		prev, rerr := s.replay(ctx, c, keys)
		if rerr == nil && prev != nil {
			return prev, true, nil
		}
	}
	if err != nil {
		return nil, false, err
	}
	return s.views(ctx, recs), false, nil
}

func (s *Service) replay(ctx context.Context, c caller, keys []string) ([]View, error) {
	var rs []store.Record
	err := s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
		var e error
		rs, e = store.ByIdemKeys(ctx, tx, keys)
		return e
	})
	if err != nil || len(rs) == 0 {
		return nil, err
	}
	return s.views(ctx, rs), nil
}

// CorrectInput là "Sửa" một bản ghi: bản ghi bù thay bản hiện hành.
type CorrectInput struct {
	ID       uuid.UUID
	Quantity *decimal.Decimal
	Note     *string
	Reason   string
	IdemKey  string
}

// Correct ghi bản bù (supersedes_id + correction_reason); bản cũ hết hiện hành; sửa bản không hiện hành → 409.
func (s *Service) Correct(ctx context.Context, in CorrectInput) (View, bool, error) {
	c, err := s.writer(ctx)
	if err != nil {
		return View{}, false, err
	}
	if in.IdemKey == "" {
		return View{}, false, problem.IdempotencyRequired
	}
	reason := strings.TrimSpace(in.Reason)
	if !domain.ValidReason(reason) {
		return View{}, false, problem.Validation.WithField("reason", "bắt buộc, 10–500 ký tự")
	}
	if domain.ContainsPII(reason) {
		return View{}, false, problem.Validation.WithField("reason", "không ghi SĐT hoặc số CCCD vào lý do")
	}
	if in.Quantity == nil {
		return View{}, false, problem.Validation.WithField("quantity", "bắt buộc")
	}
	if prev, err := s.replay(ctx, c, []string{in.IdemKey}); err != nil || prev != nil {
		if err != nil {
			return View{}, false, err
		}
		return prev[0], true, nil
	}
	now := s.now()
	onBehalf, supReason := c.support()
	var out store.Record
	err = s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
		if err := store.Lock(ctx, tx, in.ID); err != nil {
			return err
		}
		old, err := store.ByID(ctx, tx, in.ID)
		if err != nil {
			return err
		}
		if old == nil || !c.sees(*old) {
			return ErrNotFound
		}
		if old.Superseded {
			return ErrSuperseded
		}
		if e := domain.CheckQuantity(*in.Quantity, old.Unit); e != nil {
			return ErrQuantity.WithField("quantity", e.Error()+" (ĐVT "+old.Unit+")")
		}
		note := old.Note
		if in.Note != nil {
			n := strings.TrimSpace(*in.Note)
			if r := domain.NoteError(n); r != "" {
				return problem.Validation.WithField("note", r)
			}
			note = nil
			if n != "" {
				note = &n
			}
		}
		sup := old.ID
		out = store.Record{ID: ids.New(), CommuneCode: old.CommuneCode, ProvinceCode: old.ProvinceCode, DepartmentCode: old.DepartmentCode,
			RecordedOn: old.RecordedOn, MaterialCode: old.MaterialCode, MaterialName: old.MaterialName, Quantity: *in.Quantity, Unit: old.Unit,
			Note: note, Source: "manual", OnBehalfOf: onBehalf, SupportReason: supReason, EventTime: now, SupersedesID: &sup,
			CorrectionReason: &reason, ActorRef: c.p.ActorRef, IdemKey: in.IdemKey}
		return store.Insert(ctx, tx, c.p.TenantID, &out)
	})
	if err = ledger.Classify(err); errors.Is(err, ledger.ErrDuplicate) {
		if prev, rerr := s.replay(ctx, c, []string{in.IdemKey}); rerr == nil && prev != nil {
			return prev[0], true, nil
		}
	}
	if err != nil {
		return View{}, false, err
	}
	return s.views(ctx, []store.Record{out})[0], false, nil
}

// ---------------------------------------------------------------------------
// Đọc
// ---------------------------------------------------------------------------

// Query là bộ lọc danh sách / tổng hợp.
type Query struct {
	Commune      string
	From, To     string
	MaterialCode string
	Department   string // Admin (tùy chọn)
	AfterOn      *time.Time
	AfterID      uuid.UUID
	Limit        int
}

func (s *Service) filter(c caller, q Query) (store.Filter, error) {
	f := store.Filter{All: c.k.Scope.All, Units: c.k.Scope.Units, Commune: strings.TrimSpace(q.Commune), Dept: c.readDept(q.Department),
		MaterialCode: strings.TrimSpace(q.MaterialCode), AfterOn: q.AfterOn, AfterID: q.AfterID, Limit: q.Limit}
	if f.Commune != "" && !c.k.Scope.Allows(f.Commune) {
		return f, ErrOutOfScope.WithDetail("xã ngoài phạm vi phụ trách")
	}
	for _, d := range []struct {
		name string
		v    string
		dst  **time.Time
	}{{"from", q.From, &f.From}, {"to", q.To, &f.To}} {
		if strings.TrimSpace(d.v) == "" {
			continue
		}
		t, err := domain.ParseDate(d.v)
		if err != nil {
			return f, problem.Validation.WithField(d.name, "phải là ngày YYYY-MM-DD")
		}
		*d.dst = &t
	}
	if f.Limit <= 0 || f.Limit > 500 {
		f.Limit = 100
	}
	return f, nil
}

// Page là một trang bản hiện hành.
type Page struct {
	Items  []View
	NextOn *time.Time
	NextID uuid.UUID
}

// List trả bản hiện hành (không bị bản bù thay) trong phạm vi + phòng ban.
func (s *Service) List(ctx context.Context, q Query) (Page, error) {
	c, err := s.reader(ctx)
	if err != nil {
		return Page{}, err
	}
	f, err := s.filter(c, q)
	if err != nil {
		return Page{}, err
	}
	if c.k.Scope.Empty() {
		return Page{Items: []View{}}, nil
	}
	limit := f.Limit
	f.Limit++
	var rs []store.Record
	if err := s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
		var e error
		rs, e = store.ListCurrent(ctx, tx, f)
		return e
	}); err != nil {
		return Page{}, err
	}
	pg := Page{}
	if len(rs) > limit {
		rs = rs[:limit]
		last := rs[limit-1]
		on := last.RecordedOn
		pg.NextOn, pg.NextID = &on, last.ID
	}
	pg.Items = s.views(ctx, rs)
	return pg, nil
}

// Summary tổng bản hiện hành theo xã × vật tư (Cấp Tỉnh: các xã trong tỉnh).
func (s *Service) Summary(ctx context.Context, q Query) ([]store.SummaryRow, error) {
	c, err := s.reader(ctx)
	if err != nil {
		return nil, err
	}
	f, err := s.filter(c, q)
	if err != nil {
		return nil, err
	}
	if c.k.Scope.Empty() {
		return []store.SummaryRow{}, nil
	}
	var out []store.SummaryRow
	err = s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
		var e error
		out, e = store.Summary(ctx, tx, f)
		return e
	})
	return out, err
}

// LogChange là một trường đổi.
type LogChange struct {
	Field string `json:"field"`
	Old   any    `json:"old"`
	New   any    `json:"new"`
}

// LogItem là một dòng Log chuẩn.
type LogItem struct {
	ID           uuid.UUID
	Event        string
	ActorRef     uuid.UUID
	ActorDisplay *string
	At           time.Time
	Changes      []LogChange
	Reason       *string
}

func noteVal(n *string) any {
	if n == nil {
		return nil
	}
	return *n
}

// Log trả lịch sử chuỗi sửa của bản ghi (ghi nhận → các bản bù), mới nhất trước.
func (s *Service) Log(ctx context.Context, id uuid.UUID) ([]LogItem, error) {
	c, err := s.reader(ctx)
	if err != nil {
		return nil, err
	}
	var rs []store.Record
	if err := s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
		var e error
		rs, e = store.Chain(ctx, tx, id)
		return e
	}); err != nil {
		return nil, err
	}
	if len(rs) == 0 || !c.sees(rs[0]) {
		return nil, ErrNotFound
	}
	byID := map[uuid.UUID]store.Record{}
	for _, r := range rs {
		byID[r.ID] = r
	}
	views := s.views(ctx, rs)
	out := make([]LogItem, 0, len(views))
	for i := len(views) - 1; i >= 0; i-- {
		v := views[i]
		it := LogItem{ID: v.ID, Event: "recorded", ActorRef: v.ActorRef, ActorDisplay: v.ActorDisplay, At: v.RecordedAt, Reason: v.CorrectionReason}
		if v.SupersedesID == nil {
			it.Changes = []LogChange{{Field: "quantity", Old: nil, New: v.Quantity.String()}, {Field: "note", Old: nil, New: noteVal(v.Note)}}
		} else {
			it.Event = "corrected"
			old := byID[*v.SupersedesID]
			it.Changes = []LogChange{{Field: "quantity", Old: old.Quantity.String(), New: v.Quantity.String()}}
			if noteVal(old.Note) != noteVal(v.Note) {
				it.Changes = append(it.Changes, LogChange{Field: "note", Old: noteVal(old.Note), New: noteVal(v.Note)})
			}
		}
		out = append(out, it)
	}
	return out, nil
}
