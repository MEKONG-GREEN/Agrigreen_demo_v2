// Package app: use case Phòng Kế hoạch – Tài chính (W11.61, D4 KH1, BRD KHTC v2.2): Danh mục KHTC (FN-AD-04.01), danh
// sách CT/DA chủ (quyết định D20 — KHTC là chủ, module khác tham chiếu qua extension/port.KHTCPrograms), Kế hoạch năm
// 6 tab có phiên bản theo quyết định + ngày hiệu lực (FN-TI-02.02, BR-DL-13, BR-DL-21), sao chép từ năm trước, xuất /
// nhập Excel. Quyền theo ngữ cảnh phiên KN (port.RequireSession), không theo vai realm.
package app

import (
	"context"
	"errors"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtc/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtc/store"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ledger"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/meter"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// Mã lỗi (docs/errors.md, mục pf/extension — KHTC W11.61).
var (
	ErrCatalogExists   = problem.New(http.StatusConflict, "extension.khtc_catalog_exists", "Mã đã có trong danh mục (kể cả mục đã ngừng dùng)")
	ErrCatalogNotFound = problem.New(http.StatusNotFound, "extension.khtc_catalog_not_found", "Không có mục danh mục KHTC")
	ErrDefaultConflict = problem.New(http.StatusConflict, "extension.khtc_default_conflict", "Mẫu đã có người ký mặc định đang dùng")
	ErrProgramExists   = problem.New(http.StatusConflict, "extension.khtc_program_exists", "Mã CT/DA đã có trong danh sách")
	ErrProgramNotFound = problem.New(http.StatusNotFound, "extension.khtc_program_not_found", "Không có CT/DA trong danh sách KHTC")
	ErrPlanNotFound    = problem.New(http.StatusNotFound, "extension.khtc_plan_not_found", "Chưa có kế hoạch năm (hoặc phiên bản) này")
	ErrVersionConflict = problem.New(http.StatusConflict, "extension.khtc_plan_version_conflict", "Kế hoạch đã có phiên bản mới hơn — tải lại rồi điều chỉnh")
	ErrPlanInvalid     = problem.New(http.StatusUnprocessableEntity, "extension.khtc_plan_invalid", "Kế hoạch năm không hợp lệ")
	ErrEffectiveDate   = problem.New(http.StatusUnprocessableEntity, "extension.khtc_effective_date_invalid", "Ngày hiệu lực không hợp lệ")
	ErrEntryInUse      = problem.New(http.StatusConflict, "extension.khtc_entry_in_use", "Dòng đã có số liệu — chỉ được ngừng theo dõi, không xóa")
	ErrImport          = problem.New(http.StatusUnprocessableEntity, "extension.khtc_import_invalid", "Tệp Excel kế hoạch năm không đọc được")
	ErrReadOnly        = problem.New(http.StatusForbidden, "extension.read_only", "Ngữ cảnh chỉ xem")
	ErrSupportReason   = problem.New(http.StatusUnprocessableEntity, "extension.support_reason_required", "Chế độ hỗ trợ: thao tác ghi bắt buộc lý do")
)

// DeptKHTC là phòng ban sở hữu nghiệp vụ.
const DeptKHTC = partyport.DeptKeHoachTaiChinh

// Service là use case KHTC.
type Service struct {
	DB    tenant.Beginner // pool vai extension_app (db.ForModule)
	Clock clock.Clock
	// Names (tùy chọn): tên người thao tác cho lịch sử/log.
	Names func() partyport.ActorNames
	// Egress (tùy chọn, A5/ADR-0024): điểm kiểm soát xuất cho "Xuất Excel" — nội bộ, observe.
	Egress     meter.Checkpoint
	EgressMode meter.Mode
	Log        *slog.Logger
	// ClosedCutoff (tùy chọn, W11.62 nối): ngày chốt của kỳ KHTC đã đóng gần nhất của năm — "Ngày hiệu lực trước ngày
	// chốt của kỳ đã đóng gần nhất → chặn lưu" (FN-TI-02.02). nil = chưa có kỳ (không chặn).
	ClosedCutoff func(ctx context.Context, year int) (*time.Time, error)
	// EntryUsage (tùy chọn, W11.63 nối): dòng kế hoạch nào đã có số liệu ở phiếu — "đã có số liệu chỉ Ngừng theo dõi,
	// không xóa". nil = chưa có phiếu (xóa được).
	EntryUsage func(ctx context.Context, year int, refs []uuid.UUID) (map[uuid.UUID]bool, error)
}

func (s *Service) now() time.Time { return s.Clock.Now().UTC() }

// today là ngày hiện tại theo giờ Việt Nam (UTC, chỉ ngày).
func (s *Service) today() time.Time {
	t := s.now().In(clock.Zone())
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

func (s *Service) tx(ctx context.Context, tid uuid.UUID, fn func(pgx.Tx) error) error {
	return tenant.WithTenantTx(ctx, s.DB, tid, fn)
}

type caller struct {
	p authz.Principal
	k port.SessionContext
}

// reader: Admin; Cấp Tỉnh mọi phòng ban (phòng chuyên môn xem kế hoạch của phòng — FN-PN-03.03; lãnh đạo chỉ xem).
// Cấp Quốc gia, Cấp Xã không có chức năng KHTC (ma trận BRD KHTC §VI).
func (s *Service) reader(ctx context.Context) (caller, error) {
	p, k, err := port.RequireSession(ctx)
	if err != nil {
		return caller{}, err
	}
	c := caller{p: p, k: k}
	if k.Role != port.SessionRoleAdmin && k.Role != port.SessionRoleProvince {
		return c, problem.Forbidden.WithDetail("chỉ Admin và Cấp Tỉnh xem dữ liệu KHTC")
	}
	return c, nil
}

func (c caller) checkWrite() error {
	switch {
	case c.k.ReadOnly():
		return ErrReadOnly
	case c.k.Support != nil && utf8.RuneCountInString(strings.TrimSpace(c.k.Support.Reason)) < 10:
		return ErrSupportReason
	}
	return nil
}

// planWriter: Admin; Cấp Tỉnh phòng Kế hoạch – Tài chính (FN-TI-02.02 CRUD). Lãnh đạo/BGĐ chỉ xem (ReadOnly).
func (s *Service) planWriter(ctx context.Context) (caller, error) {
	c, err := s.reader(ctx)
	if err != nil {
		return c, err
	}
	if c.k.Role != port.SessionRoleAdmin && c.k.Department != DeptKHTC {
		return c, problem.Forbidden.WithDetail("chỉ Phòng Kế hoạch – Tài chính lập, điều chỉnh kế hoạch năm và danh sách CT/DA")
	}
	return c, c.checkWrite()
}

// catalogWriter: Admin (FN-AD-04.01 — Admin CRUD, KHTC xem).
func (s *Service) catalogWriter(ctx context.Context) (caller, error) {
	c, err := s.reader(ctx)
	if err != nil {
		return c, err
	}
	if c.k.Role != port.SessionRoleAdmin {
		return c, problem.Forbidden.WithDetail("chỉ Admin quản lý Danh mục KHTC")
	}
	return c, c.checkWrite()
}

func (c caller) meta(id uuid.UUID, at time.Time, idem string) store.Meta {
	m := store.Meta{ID: id, EventTime: at, ActorRef: c.p.ActorRef, IdemKey: idem}
	if c.k.Support != nil {
		r := strings.TrimSpace(c.k.Support.Reason)
		m.SupportReason = &r
		if c.k.Support.OnBehalfOf != uuid.Nil {
			v := c.k.Support.OnBehalfOf
			m.OnBehalfOf = &v
		}
	}
	return m
}

func (s *Service) names(ctx context.Context, refs []uuid.UUID) map[uuid.UUID]string {
	out := map[uuid.UUID]string{}
	if s.Names == nil || len(refs) == 0 {
		return out
	}
	n := s.Names()
	if n == nil {
		return out
	}
	if m, err := n.DisplayNames(ctx, refs); err == nil {
		return m
	}
	return out
}

// Actor là người thao tác hiển thị ở lịch sử.
type Actor struct {
	Ref     uuid.UUID
	Display *string
}

func (s *Service) actors(ctx context.Context, refs []uuid.UUID) func(uuid.UUID) Actor {
	m := s.names(ctx, refs)
	return func(r uuid.UUID) Actor {
		a := Actor{Ref: r}
		if n, ok := m[r]; ok && n != "" {
			nn := n
			a.Display = &nn
		}
		return a
	}
}

func fieldsProblem(base *problem.Problem, es domain.Errors) *problem.Problem {
	p := base
	for _, e := range es {
		p = p.WithField(e.Field, e.Reason)
	}
	return p
}

func dup(err error) bool { return errors.Is(ledger.Classify(err), ledger.ErrDuplicate) }

// ---------------------------------------------------------------------------
// Danh mục KHTC
// ---------------------------------------------------------------------------

// CatalogView là bản hiện hành (hoặc một phiên bản) của mục danh mục.
type CatalogView struct {
	store.CatalogRow
	Actor Actor
}

func (s *Service) catViews(ctx context.Context, rs []store.CatalogRow) []CatalogView {
	refs := make([]uuid.UUID, 0, len(rs))
	for _, r := range rs {
		refs = append(refs, r.ActorRef)
	}
	act := s.actors(ctx, refs)
	out := make([]CatalogView, 0, len(rs))
	for _, r := range rs {
		out = append(out, CatalogView{CatalogRow: r, Actor: act(r.ActorRef)})
	}
	return out
}

// CatalogCount là số mục đang dùng / tổng của một loại (cột trái màn danh mục).
type CatalogCount struct {
	Kind   string
	Active int
	Total  int
}

// CatalogList là danh sách một loại + số lượng mọi loại.
type CatalogList struct {
	Counts []CatalogCount
	Items  []CatalogView
}

// ListCatalog trả mục hiện hành của một loại ("" = mọi loại) và số lượng theo loại.
func (s *Service) ListCatalog(ctx context.Context, kind string, includeRetired bool) (CatalogList, error) {
	c, err := s.reader(ctx)
	if err != nil {
		return CatalogList{}, err
	}
	if kind != "" && !slices.Contains(domain.CatalogKinds, kind) {
		return CatalogList{}, problem.Validation.WithField("kind", "loại danh mục không hợp lệ")
	}
	var rs []store.CatalogRow
	if err := s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
		var e error
		rs, e = store.CatalogCurrent(ctx, tx, "")
		return e
	}); err != nil {
		return CatalogList{}, err
	}
	counts := map[string]*CatalogCount{}
	out := CatalogList{}
	for _, k := range domain.CatalogKinds {
		counts[k] = &CatalogCount{Kind: k}
	}
	var items []store.CatalogRow
	for _, r := range rs {
		cc := counts[r.Kind]
		cc.Total++
		if r.Status == domain.StatusActive {
			cc.Active++
		}
		if (kind == "" || r.Kind == kind) && (includeRetired || r.Status == domain.StatusActive) {
			items = append(items, r)
		}
	}
	for _, k := range domain.CatalogKinds {
		out.Counts = append(out.Counts, *counts[k])
	}
	out.Items = s.catViews(ctx, items)
	return out, nil
}

// CatalogInput là nội dung thêm/sửa một mục.
type CatalogInput struct {
	Item    domain.CatalogItem
	IdemKey string
}

func (s *Service) writeCatalog(ctx context.Context, in CatalogInput, create bool) (CatalogView, bool, error) {
	c, err := s.catalogWriter(ctx)
	if err != nil {
		return CatalogView{}, false, err
	}
	if in.IdemKey == "" {
		return CatalogView{}, false, problem.IdempotencyRequired
	}
	it := in.Item
	if es := domain.NormalizeCatalog(&it); len(es) > 0 {
		return CatalogView{}, false, fieldsProblem(problem.Validation, es)
	}
	replay := func() (*CatalogView, error) {
		var r *store.CatalogRow
		err := s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
			var e error
			r, e = store.CatalogByIdem(ctx, tx, in.IdemKey)
			return e
		})
		if err != nil || r == nil {
			return nil, err
		}
		v := s.catViews(ctx, []store.CatalogRow{*r})[0]
		return &v, nil
	}
	if v, err := replay(); err != nil || v != nil {
		if err != nil {
			return CatalogView{}, false, err
		}
		return *v, true, nil
	}
	row := store.CatalogRow{CatalogItem: it}
	err = s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
		if err := store.Lock(ctx, tx, "catalog:"+it.Kind+":"+it.Code); err != nil {
			return err
		}
		if it.Kind == domain.KindSigner {
			if err := store.Lock(ctx, tx, "catalog:signer-default"); err != nil {
				return err
			}
		}
		hist, err := store.CatalogHistory(ctx, tx, it.Kind, it.Code)
		if err != nil {
			return err
		}
		switch {
		case create && len(hist) > 0:
			return ErrCatalogExists.WithField("code", "mã đã có")
		case !create && len(hist) == 0:
			return ErrCatalogNotFound
		}
		if it.Kind == domain.KindSigner && it.Status == domain.StatusActive && len(it.DefaultFor) > 0 {
			cur, err := store.CatalogCurrent(ctx, tx, domain.KindSigner)
			if err != nil {
				return err
			}
			for _, o := range cur {
				if o.Code == it.Code || o.Status != domain.StatusActive {
					continue
				}
				for _, t := range it.DefaultFor {
					if slices.Contains(o.DefaultFor, t) {
						return ErrDefaultConflict.WithField("default_for", t+" đã có người ký mặc định "+o.Name+" ("+o.Code+")")
					}
				}
			}
		}
		row.Meta = c.meta(newID(), s.now(), in.IdemKey)
		return store.InsertCatalog(ctx, tx, c.p.TenantID, &row)
	})
	if dup(err) {
		if v, rerr := replay(); rerr == nil && v != nil {
			return *v, true, nil
		}
	}
	if err != nil {
		return CatalogView{}, false, err
	}
	return s.catViews(ctx, []store.CatalogRow{row})[0], false, nil
}

// CreateCatalog thêm một mục (phiên bản 1). Trùng mã (kể cả đã ngừng) → 409.
func (s *Service) CreateCatalog(ctx context.Context, in CatalogInput) (CatalogView, bool, error) {
	return s.writeCatalog(ctx, in, true)
}

// ReviseCatalog ghi phiên bản mới (sửa, ngừng dùng = status retired, dùng lại). Đổi mặc định chỉ áp dụng cho bản soạn
// mới — báo cáo đã xuất giữ người ký, nơi nhận tại thời điểm xuất (W11.64 ghi lại lúc xuất).
func (s *Service) ReviseCatalog(ctx context.Context, in CatalogInput) (CatalogView, bool, error) {
	return s.writeCatalog(ctx, in, false)
}

// CatalogHistory trả mọi phiên bản của một mục, mới nhất trước.
func (s *Service) CatalogHistory(ctx context.Context, kind, code string) ([]CatalogView, error) {
	c, err := s.reader(ctx)
	if err != nil {
		return nil, err
	}
	var rs []store.CatalogRow
	if err := s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
		var e error
		rs, e = store.CatalogHistory(ctx, tx, kind, strings.ToLower(strings.TrimSpace(code)))
		return e
	}); err != nil {
		return nil, err
	}
	if len(rs) == 0 {
		return nil, ErrCatalogNotFound
	}
	return s.catViews(ctx, rs), nil
}

// activeCodes trả mã đang dùng theo loại.
func activeCodes(rs []store.CatalogRow) map[string]map[string]bool {
	out := map[string]map[string]bool{}
	for _, k := range domain.CatalogKinds {
		out[k] = map[string]bool{}
	}
	for _, r := range rs {
		if r.Status == domain.StatusActive {
			out[r.Kind][r.Code] = true
		}
	}
	return out
}
