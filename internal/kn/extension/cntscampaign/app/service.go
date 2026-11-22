// Package app: use case Chiến dịch điều tra mở rộng CNTS (W11.50 — BRD CNTS v3.7 FN-21, UR-TINH-26, UR-XA-14; D4 CN10).
//
// Cấp Tỉnh Phòng CNTS tạo đợt (Nháp, sửa được), phát hành → MỘT kỳ native cap/forms profile kn_cnts loại kỳ `adhoc`
// (đơn vị lá = xã, khóa "DT-…"): cán bộ xã thấy việc trong màn nhập biểu kỳ CNTS và nhập/gửi theo vòng đời III.3.0
// (pf/forms). Cấp Tỉnh theo dõi tiến độ theo Khu vực/xã, nhắc xã chưa nộp, gia hạn (lý do), đóng đợt (chốt kỳ native —
// xã chưa gửi chuyển "Không nộp", không gửi được nữa), xem/xuất kết quả; mở lại chỉ Lãnh đạo phòng (lý do + thời hạn).
// Số liệu đợt KHÔNG vào lũy kế báo cáo định kỳ (reportcycle bỏ kỳ `adhoc`) và tổng KHTC (closedfig bỏ kỳ `adhoc`).
package app

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/cntscampaign/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/cntscampaign/store"
	extport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	accport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/port"
	formsport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/port"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// Mã lỗi (docs/errors/pf-extension-cnts-campaign.md).
var (
	ErrNotFound = problem.New(http.StatusNotFound, "extension.cnts_campaign_not_found", "Không tìm thấy đợt điều tra")
	ErrState    = problem.New(http.StatusConflict, "extension.cnts_campaign_state",
		"Thao tác không hợp lệ ở trạng thái hiện tại của đợt (Nháp / Đang thu thập / Đã đóng)")
	ErrTemplate = problem.New(http.StatusUnprocessableEntity, "extension.cnts_campaign_template",
		"Biểu điều tra chưa có ở cấu hình biểu CNTS (thiết kế biểu trước — FN-07)")
	ErrDependency = problem.New(http.StatusServiceUnavailable, "extension.dependency_unavailable", "Dịch vụ phụ thuộc chưa sẵn sàng")
	// ErrBusy, ErrInterrupted: ý định thao tác của đợt (BF-CNTS-CAMPAIGN-POOL, #156).
	ErrBusy = problem.New(http.StatusConflict, "extension.cnts_campaign_busy",
		"Đợt đang có thao tác khác chưa xong (phát hành, gia hạn, nhắc, đóng, mở lại) — thử lại sau ít phút")
	ErrInterrupted = problem.New(http.StatusConflict, "extension.cnts_campaign_interrupted",
		"Thao tác đợt bị gián đoạn (quá hạn giữ đợt, thao tác khác đang giữ) — chưa ghi kết quả; tải lại đợt rồi thử lại")
	errNotCNTS   = problem.Forbidden.WithDetail("chiến dịch điều tra chỉ dành cho Phòng Chăn nuôi – Thủy sản (Cấp Tỉnh, Cấp Quốc gia xem) và Admin xem")
	errNotWriter = problem.Forbidden.WithDetail("chỉ Cấp Tỉnh Phòng Chăn nuôi – Thủy sản tạo, phát hành, gia hạn, nhắc, đóng đợt")
	errNotLeader = problem.Forbidden.WithDetail("mở lại đợt chỉ Lãnh đạo phòng (hoặc người được ủy quyền mở lại kỳ)")
	// errNotOwner: ma trận quyền FN-21 (BRD CNTS v4.0) — viên chức chuyên môn, chủ nhiệm CT/DA chỉ CRU đợt mình là người phụ
	// trách; Lãnh đạo phòng chỉ xem và mở lại đợt.
	errNotOwner = problem.Forbidden.WithDetail("chỉ cán bộ tổng hợp hoặc người phụ trách đợt được tạo, sửa, phát hành, gia hạn, nhắc, đóng đợt")
	// ErrOwner: người phụ trách không phải cán bộ Cấp Tỉnh Phòng CNTS của tỉnh.
	// ErrHousehold: hộ của phạm vi theo hộ không có trong danh sách hộ chăn nuôi, không Đang dùng hoặc ngoài phạm vi Nút
	// (#236, ADR-0056 mục 9: chỉ chọn hộ active).
	ErrHousehold = problem.New(http.StatusUnprocessableEntity, "extension.cnts_campaign_household",
		"Hộ chăn nuôi của phạm vi phải đang dùng (đã check-in vị trí) và thuộc xã trong phạm vi của Nút")
	ErrOwner = problem.New(http.StatusUnprocessableEntity, "extension.cnts_campaign_owner",
		"Người phụ trách đợt phải là cán bộ Cấp Tỉnh Phòng Chăn nuôi – Thủy sản của tỉnh")
)

// Service là use case chiến dịch điều tra.
type Service struct {
	DB    tenant.Beginner // pool của pf/extension (db.ForModule — r_extension)
	Clock clock.Clock
	// Forms, Control: cap/forms (biểu, phát hành kỳ native; đọc tiến độ). Thiếu → 503.
	Forms   formsport.Publisher
	Control formsport.PeriodControl
	// Cond: cổng điều khiển kỳ native CÓ ĐIỀU KIỆN (#159 BF-FORMS-PERIOD-CAS) — MỌI lời gọi ghi kỳ native của đợt (gia hạn, đóng,
	// mở lại + đổi hạn, đối soát, hủy bù phát hành) đi qua cổng này với revision ghi bền trong ý định (#201). Thiếu → thao tác
	// ghi kỳ native trả 503 (fail closed), không bao giờ quay về lời gọi vô điều kiện của Control.
	Cond formsport.ConditionalPeriodControl
	// Figures: số liệu kỳ native (kết quả tổng hợp). Thiếu → kết quả 503.
	Figures formsport.FiguresReader
	// Orgs, Units: tỉnh của Nút (khu vực, tên xã). Regions: khu vực của tỉnh (phạm vi toàn tỉnh/khu vực, tiến độ theo khu
	// vực) — tùy chọn (nil → chỉ phạm vi danh sách xã, tiến độ gom "Chưa gán khu vực").
	Orgs    partyport.Orgs
	Units   partyport.AdminUnits
	Regions func() partyport.Regions
	Names   func() partyport.ActorNames
	// Officers: danh bạ cán bộ KN của pf/accounts (người phụ trách đợt). nil → không kiểm người phụ trách theo danh bạ và
	// danh sách chọn người phụ trách trả 503.
	Officers func() accport.KNDirectory
	// Lease là hạn ý định thao tác của đợt (0 → DefaultLease; kiểm thử đặt ngắn).
	Lease time.Duration
}

func (s *Service) now() time.Time { return s.Clock.Now().UTC() }

func (s *Service) tx(ctx context.Context, tid uuid.UUID, fn func(pgx.Tx) error) error {
	return tenant.WithTenantTx(ctx, s.DB, tid, fn)
}

func (s *Service) regions() partyport.Regions {
	if s.Regions == nil {
		return nil
	}
	return s.Regions()
}

type caller struct {
	p        authz.Principal
	k        extport.SessionContext
	province string
}

// reader: Admin, hoặc Cấp Tỉnh/Cấp Quốc gia Phòng CNTS (kể cả chỉ xem).
func (s *Service) reader(ctx context.Context) (caller, error) {
	if s.DB == nil || s.Forms == nil || s.Control == nil {
		return caller{}, ErrDependency.WithDetail("chưa nối cap/forms cho chiến dịch điều tra")
	}
	p, k, err := extport.RequireSession(ctx)
	if err != nil {
		return caller{}, err
	}
	ok := k.Role == extport.SessionRoleAdmin ||
		(k.Department == domain.Department && (k.Role == extport.SessionRoleProvince || k.Role == extport.SessionRoleNational))
	if !ok {
		return caller{}, errNotCNTS
	}
	c := caller{p: p, k: k}
	c.province = s.provinceOf(ctx, k)
	return c, nil
}

// writer: Cấp Tỉnh Phòng CNTS, không ở chế độ chỉ xem (cấp publish_by/close_by của profile kn_cnts).
func (s *Service) writer(ctx context.Context) (caller, error) {
	c, err := s.reader(ctx)
	if err != nil {
		return c, err
	}
	if c.k.Role != extport.SessionRoleProvince || c.k.Department != domain.Department || c.k.ReadOnly() || c.k.OrgRef == uuid.Nil {
		return c, errNotWriter
	}
	return c, nil
}

// full: cán bộ tổng hợp Cấp Tỉnh — CRU mọi đợt của Nút (ma trận FN-21). Lần gán chưa khai nhóm Cấp Tỉnh giữ quyền đầy đủ
// như trước khi có nhóm (cùng cách lịch trực KSGM, cấp quyền mô hình).
func (c caller) full() bool {
	return c.k.ProvinceGroup == "" || c.k.ProvinceGroup == accport.GroupTongHop
}

// ownerGroup: nhóm được CRU đợt khi là người phụ trách (viên chức chuyên môn, chủ nhiệm CT/DA).
func (c caller) ownerGroup() bool {
	return c.k.ProvinceGroup == accport.GroupVienChuc || c.k.ProvinceGroup == accport.GroupChuNhiem
}

// manages: người gọi (đã qua writer) được sửa, phát hành, gia hạn, nhắc, đóng đợt v — cán bộ tổng hợp, hoặc người phụ trách
// đợt thuộc nhóm viên chức/chủ nhiệm. Lãnh đạo phòng không (chỉ mở lại — Reopen).
func (c caller) manages(v store.Version) bool {
	return c.full() || (c.ownerGroup() && v.OwnerRef != nil && *v.OwnerRef == c.p.ActorRef)
}

// canManage dùng cho phản hồi đọc: người gọi có quyền ghi và quản lý đợt v.
func (c caller) canManage(v store.Version) bool {
	return c.k.Role == extport.SessionRoleProvince && c.k.Department == domain.Department && !c.k.ReadOnly() &&
		c.k.OrgRef != uuid.Nil && c.manages(v)
}

// provinceOf: tỉnh của Nút trong ngữ cảnh (như reportcycle knActor). Rỗng nếu không xác định.
func (s *Service) provinceOf(ctx context.Context, k extport.SessionContext) string {
	if s.Orgs == nil || k.OrgRef == uuid.Nil || k.Role == extport.SessionRoleAdmin || k.Role == extport.SessionRoleNational {
		return ""
	}
	org, err := s.Orgs.Get(ctx, k.OrgRef)
	if err != nil {
		return ""
	}
	if org.Kind == partyport.OrgTTKNProvince || s.Units == nil {
		return org.AdminUnitCode
	}
	if u, err := s.Units.Get(ctx, org.AdminUnitCode, s.now()); err == nil && u.ParentCode != "" && u.Level != partyport.LevelProvince {
		return u.ParentCode
	}
	return org.AdminUnitCode
}

// sees: Cấp Tỉnh chỉ thấy đợt do Nút mình tạo; Admin, Cấp Quốc gia thấy mọi đợt.
func (c caller) sees(v store.Version) bool {
	if c.k.Role == extport.SessionRoleAdmin || c.k.Role == extport.SessionRoleNational {
		return true
	}
	return v.OrgRef == c.k.OrgRef
}

func inputErr(err error) error {
	var ie domain.ErrInput
	if errors.As(err, &ie) {
		return problem.Validation.WithField(ie.Field, ie.Msg)
	}
	return err
}

// classify đổi lỗi DB của hai sổ sang mã lỗi.
func classify(err error) error {
	var pg *pgconn.PgError
	if !errors.As(err, &pg) {
		return err
	}
	switch pg.ConstraintName {
	case "cnts_campaign_not_draft", "cnts_campaign_event_state":
		return ErrState.WithCause(err)
	case "cnts_campaign_busy":
		return ErrBusy.WithCause(err)
	}
	if pg.Code == "23505" {
		return problem.Conflict.WithCause(err)
	}
	return err
}

// ---------------------------------------------------------------------------
// Đọc
// ---------------------------------------------------------------------------

// Campaign là trạng thái hiện tại của một đợt.
type Campaign struct {
	Version  store.Version
	State    string
	Events   []store.Event
	Period   *store.Event // sự kiện phát hành (nil khi Nháp)
	DueAt    time.Time    // hạn nộp hiện hành
	Progress *Progress    // tiến độ (đợt đã phát hành, đọc chi tiết/danh sách)
	// CanManage: người gọi được sửa, phát hành, gia hạn, nhắc, đóng đợt (ma trận FN-21).
	CanManage bool
	// Households: bản hiện hành của các hộ trong phạm vi theo hộ (đọc chi tiết; nil với phạm vi khác).
	Households []store.Household
}

func build(v store.Version, es []store.Event) Campaign {
	c := Campaign{Version: v, Events: es, DueAt: v.DueAt}
	acts := make([]string, 0, len(es))
	for i := range es {
		e := es[i]
		acts = append(acts, e.Action)
		if e.Action == domain.ActPublished {
			c.Period = &es[i]
		}
		if e.DueAt != nil {
			c.DueAt = *e.DueAt
		}
	}
	c.State = domain.StateOf(acts)
	return c
}

func (s *Service) load(ctx context.Context, c caller, id uuid.UUID) (Campaign, error) {
	var out Campaign
	err := s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
		var err error
		out, err = s.current(ctx, tx, c, id)
		return err
	})
	return out, err
}

// List liệt kê đợt (mới trước) kèm tiến độ của đợt đã phát hành.
func (s *Service) List(ctx context.Context) ([]Campaign, error) {
	c, err := s.reader(ctx)
	if err != nil {
		return nil, err
	}
	org := uuid.Nil
	if c.k.Role == extport.SessionRoleProvince {
		org = c.k.OrgRef
	}
	var out []Campaign
	err = s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
		vs, err := store.List(ctx, tx, org, 200)
		if err != nil {
			return err
		}
		ids := make([]uuid.UUID, 0, len(vs))
		for _, v := range vs {
			ids = append(ids, v.CampaignID)
		}
		es, err := store.Events(ctx, tx, ids)
		if err != nil {
			return err
		}
		for _, v := range vs {
			x := build(v, es[v.CampaignID])
			x.CanManage = c.canManage(v)
			out = append(out, x)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	for i := range out {
		if out[i].Period != nil {
			if p, err := s.progress(ctx, c, out[i], false); err == nil {
				out[i].Progress = &p
			}
		}
	}
	return out, nil
}

// Get đọc một đợt kèm tiến độ theo khu vực/xã.
func (s *Service) Get(ctx context.Context, id uuid.UUID) (Campaign, error) {
	c, err := s.reader(ctx)
	if err != nil {
		return Campaign{}, err
	}
	return s.get(ctx, c, id)
}

func (s *Service) get(ctx context.Context, c caller, id uuid.UUID) (Campaign, error) {
	out, err := s.load(ctx, c, id)
	if err != nil {
		return Campaign{}, err
	}
	out.CanManage = c.canManage(out.Version)
	if len(out.Version.Households) > 0 {
		book, err := s.householdBook(ctx, c, out.Version.Households)
		if err != nil {
			return Campaign{}, err
		}
		for _, r := range out.Version.Households {
			h, ok := book[r]
			if !ok {
				h = store.Household{Ref: r}
			}
			out.Households = append(out.Households, h)
		}
	}
	if out.Period != nil {
		p, err := s.progress(ctx, c, out, true)
		if err != nil {
			return Campaign{}, err
		}
		out.Progress = &p
	}
	return out, nil
}

// DisplayNames trả tên hiển thị của người thao tác/người phụ trách (rỗng nếu chưa nối danh bạ).
func (s *Service) DisplayNames(ctx context.Context, refs []uuid.UUID) map[uuid.UUID]string {
	if s.Names == nil {
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

// ---------------------------------------------------------------------------
// Nháp
// ---------------------------------------------------------------------------

// Input là nội dung đợt từ màn "+ Tạo đợt điều tra" (B1).
type Input struct {
	Name, Goal, Field string
	TemplateCode      string
	TemplateVersion   *int64
	ScopeMode         string
	Regions           []string
	Communes          []string
	Households        []uuid.UUID // phạm vi theo hộ: household_ref (ADR-0056)
	From, To          string      // YYYY-MM-DD
	DueAt             time.Time
	OwnerRef          *uuid.UUID
	RemindBeforeHours int
}

func parseDay(field, v string) (time.Time, error) {
	t, err := time.Parse(time.DateOnly, strings.TrimSpace(v))
	if err != nil {
		return time.Time{}, problem.Validation.WithField(field, "dạng YYYY-MM-DD")
	}
	return t, nil
}

// draftOf giải phạm vi thành danh sách xã và kiểm nội dung.
func (s *Service) draftOf(ctx context.Context, c caller, in Input) (domain.Draft, error) {
	from, err := parseDay("from_date", in.From)
	if err != nil {
		return domain.Draft{}, err
	}
	to, err := parseDay("to_date", in.To)
	if err != nil {
		return domain.Draft{}, err
	}
	d := domain.Draft{Name: in.Name, Goal: in.Goal, Field: in.Field, TemplateCode: in.TemplateCode, TemplateVersion: in.TemplateVersion,
		ScopeMode: in.ScopeMode, Regions: in.Regions, Units: in.Communes, Households: in.Households, From: from, To: to, DueAt: in.DueAt, OwnerRef: in.OwnerRef,
		RemindBeforeHours: in.RemindBeforeHours}
	d.Normalize()
	switch d.ScopeMode {
	case domain.ScopeProvince, domain.ScopeRegions:
		rg := s.regions()
		if rg == nil || c.province == "" {
			return d, ErrDependency.WithDetail("chưa nối khu vực của tỉnh — chọn phạm vi theo danh sách xã")
		}
		all, err := rg.CommuneRegions(ctx, c.province)
		if err != nil {
			return d, err
		}
		d.Units = nil
		for code, cr := range all {
			if d.ScopeMode == domain.ScopeProvince || slices.Contains(d.Regions, cr.RegionCode) {
				d.Units = append(d.Units, code)
			}
		}
		sort.Strings(d.Units)
	case domain.ScopeCommunes, domain.ScopeHouseholds:
		if d.ScopeMode == domain.ScopeHouseholds {
			if len(d.Units) > 0 {
				return d, problem.Validation.WithField("communes", "phạm vi theo hộ: xã lấy theo hộ đã chọn")
			}
			parents, err := s.activeHouseholds(ctx, c, d.Households)
			if err != nil {
				return d, err
			}
			for _, p := range parents {
				if !slices.Contains(d.Units, p) {
					d.Units = append(d.Units, p)
				}
			}
			sort.Strings(d.Units)
		}
		if rg := s.regions(); rg != nil && c.province != "" {
			all, err := rg.CommuneRegions(ctx, c.province)
			if err != nil {
				return d, err
			}
			for _, u := range d.Units {
				if _, ok := all[u]; !ok {
					return d, problem.Validation.WithField("communes", u+": không phải xã đang hoạt động của tỉnh")
				}
			}
		}
	}
	for _, u := range d.Units {
		if !c.k.Scope.Allows(u) {
			return d, problem.Validation.WithField("communes", u+": ngoài phạm vi của Nút")
		}
	}
	if err := d.Validate(); err != nil {
		return d, inputErr(err)
	}
	if err := s.checkOwner(ctx, c, *d.OwnerRef); err != nil {
		return d, err
	}
	if _, err := s.template(ctx, d.TemplateCode, d.TemplateVersion); isNotFound(err) {
		field := "template_code"
		if d.TemplateVersion != nil {
			field = "template_version"
		}
		return d, ErrTemplate.WithField(field, d.TemplateCode)
	} else if err != nil {
		return d, err
	}
	return d, nil
}

// checkOwner: người phụ trách (FN-21 B1 "giao người phụ trách"). Viên chức, chủ nhiệm chỉ ghi đợt mình phụ trách nên chỉ giao
// cho chính mình; nhóm khác ngoài cán bộ tổng hợp không ghi đợt. Có danh bạ → người phụ trách phải là cán bộ Cấp Tỉnh Phòng
// CNTS còn hiệu lực của tỉnh.
func (s *Service) checkOwner(ctx context.Context, c caller, owner uuid.UUID) error {
	switch {
	case c.full():
	case c.ownerGroup():
		if owner != c.p.ActorRef {
			return errNotOwner.WithField("owner_ref", "viên chức, chủ nhiệm chỉ tạo/sửa đợt mình là người phụ trách")
		}
	default:
		return errNotOwner
	}
	dir := s.directory()
	if dir == nil {
		return nil
	}
	if c.province == "" {
		return ErrOwner.WithField("owner_ref", "không xác định được tỉnh của Nút")
	}
	os, err := dir.Officers(ctx, c.p.TenantID, domain.Department, c.province)
	if err != nil {
		return err
	}
	for _, o := range os {
		if o.ActorRef == owner && o.Level == accport.ScheduleLevelProvince {
			return nil
		}
	}
	return ErrOwner.WithField("owner_ref", owner.String())
}

// householdBook đọc bản hiện hành của các hộ (sổ hộ chăn nuôi #251).
func (s *Service) householdBook(ctx context.Context, c caller, refs []uuid.UUID) (map[uuid.UUID]store.Household, error) {
	var out map[uuid.UUID]store.Household
	err := s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
		var e error
		out, e = store.Households(ctx, tx, refs)
		return e
	})
	return out, err
}

// activeHouseholds kiểm các hộ của phạm vi theo hộ (ADR-0056 mục 9: chỉ hộ Đang dùng; xã của hộ trong phạm vi Nút) và trả xã
// của từng hộ (song song refs) — đơn vị cha của kỳ native khi phát hành.
func (s *Service) activeHouseholds(ctx context.Context, c caller, refs []uuid.UUID) ([]string, error) {
	book, err := s.householdBook(ctx, c, refs)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(refs))
	for _, r := range refs {
		h, ok := book[r]
		switch {
		case !ok:
			return nil, ErrHousehold.WithField("households", r.String()+": không có trong danh sách hộ chăn nuôi")
		case h.Status != householdActive:
			return nil, ErrHousehold.WithField("households", h.Code+": hộ không ở trạng thái Đang dùng")
		case !c.k.Scope.Allows(h.Commune):
			return nil, ErrHousehold.WithField("households", h.Code+": xã "+h.Commune+" ngoài phạm vi của Nút")
		}
		out = append(out, h.Commune)
	}
	return out, nil
}

// householdActive là trạng thái "Đang dùng" của sổ hộ chăn nuôi (HCN-08).
const householdActive = "active"

func (s *Service) directory() accport.KNDirectory {
	if s.Officers == nil {
		return nil
	}
	return s.Officers()
}

// Owner là một người chọn được làm người phụ trách đợt.
type Owner struct {
	Ref     uuid.UUID
	Display string
}

// Owners liệt kê cán bộ Cấp Tỉnh Phòng CNTS còn hiệu lực của tỉnh (ô "Người phụ trách" của màn tạo đợt). Viên chức, chủ
// nhiệm chỉ giao cho chính mình nên danh sách chỉ có họ.
func (s *Service) Owners(ctx context.Context) ([]Owner, error) {
	c, err := s.writer(ctx)
	if err != nil {
		return nil, err
	}
	dir := s.directory()
	if dir == nil {
		return nil, ErrDependency.WithDetail("chưa nối danh bạ cán bộ (pf/accounts)")
	}
	out := []Owner{}
	if c.province == "" {
		return out, nil
	}
	os, err := dir.Officers(ctx, c.p.TenantID, domain.Department, c.province)
	if err != nil {
		return nil, err
	}
	refs := []uuid.UUID{}
	for _, o := range os {
		if o.Level == accport.ScheduleLevelProvince && !slices.Contains(refs, o.ActorRef) && (c.full() || o.ActorRef == c.p.ActorRef) {
			refs = append(refs, o.ActorRef)
		}
	}
	names := s.DisplayNames(ctx, refs)
	for _, r := range refs {
		out = append(out, Owner{Ref: r, Display: names[r]})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Display < out[j].Display })
	return out, nil
}

// template đọc đúng phiên bản biểu đã chọn (#192, FN-07 BR-01); version nil → phiên bản mới nhất (Nháp ghi trước #192).
func (s *Service) template(ctx context.Context, code string, version *int64) (formsport.TemplateRef, error) {
	if version == nil {
		return s.Forms.LatestTemplate(ctx, domain.Profile, code)
	}
	cat, ok := s.Forms.(formsport.TemplateCatalog)
	if !ok {
		return formsport.TemplateRef{}, ErrDependency.WithDetail("chưa nối danh mục phiên bản biểu (cap/forms)")
	}
	tv, err := cat.TemplateVersion(ctx, domain.Profile, code, *version)
	if err != nil {
		return formsport.TemplateRef{}, err
	}
	return formsport.TemplateRef{ID: tv.ID, Code: tv.Code, Version: tv.Version, Title: tv.Title, Fields: tv.Fields}, nil
}

func versionOf(id uuid.UUID, c caller, d domain.Draft, now time.Time) store.Version {
	v := store.Version{RowID: newID(), CampaignID: id, OrgRef: c.k.OrgRef, Name: d.Name, Field: d.Field, TemplateCode: d.TemplateCode,
		TemplateVersion: d.TemplateVersion, ScopeMode: d.ScopeMode, Regions: d.Regions, Units: d.Units, Households: d.Households, From: d.From, To: d.To, DueAt: d.DueAt, OwnerRef: d.OwnerRef,
		RemindBeforeHours: d.RemindBeforeHours, ActorRef: c.p.ActorRef, EventTime: now}
	if d.Goal != "" {
		g := d.Goal
		v.Goal = &g
	}
	if c.province != "" {
		p := c.province
		v.Province = &p
	}
	return v
}

// Create ghi đợt mới ở trạng thái Nháp. Gọi lặp cùng Idempotency-Key → trả đợt đã tạo.
func (s *Service) Create(ctx context.Context, in Input, idem string) (Campaign, bool, error) {
	c, err := s.writer(ctx)
	if err != nil {
		return Campaign{}, false, err
	}
	if idem == "" {
		return Campaign{}, false, problem.IdempotencyRequired
	}
	return s.insertNew(ctx, c, "cnts_campaign:create:"+idem, func() (Input, error) { return in, nil })
}

// insertNew ghi đợt mới (Nháp) từ nội dung in() với khóa idempotent key; gọi lặp cùng khóa → trả đợt đã tạo.
func (s *Service) insertNew(ctx context.Context, c caller, key string, in func() (Input, error)) (Campaign, bool, error) {
	var prev *store.Version
	if err := s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
		var e error
		prev, e = store.VersionByIdem(ctx, tx, key)
		return e
	}); err != nil {
		return Campaign{}, false, err
	}
	if prev != nil {
		out, err := s.get(ctx, c, prev.CampaignID)
		return out, true, err
	}
	src, err := in()
	if err != nil {
		return Campaign{}, false, err
	}
	d, err := s.draftOf(ctx, c, src)
	if err != nil {
		return Campaign{}, false, err
	}
	id := newID()
	v := versionOf(id, c, d, s.now())
	if err := s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error { return store.InsertVersion(ctx, tx, c.p.TenantID, &v, key) }); err != nil {
		return Campaign{}, false, classify(err)
	}
	out, err := s.get(ctx, c, id)
	return out, false, err
}

// Update ghi phiên bản nội dung mới của đợt Nháp.
func (s *Service) Update(ctx context.Context, id uuid.UUID, in Input, idem string) (Campaign, error) {
	c, err := s.writer(ctx)
	if err != nil {
		return Campaign{}, err
	}
	if idem == "" {
		return Campaign{}, problem.IdempotencyRequired
	}
	cur, err := s.load(ctx, c, id)
	if err != nil {
		return Campaign{}, err
	}
	if !c.manages(cur.Version) {
		return Campaign{}, errNotOwner
	}
	if cur.State != domain.StateDraft {
		return Campaign{}, ErrState.WithDetail("đợt đã phát hành — không sửa nội dung (gia hạn hoặc đóng đợt)")
	}
	key := "cnts_campaign:update:" + id.String() + ":" + idem
	var prev *store.Version
	if err := s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
		var e error
		prev, e = store.VersionByIdem(ctx, tx, key)
		return e
	}); err != nil {
		return Campaign{}, err
	}
	if prev == nil {
		d, err := s.draftOf(ctx, c, in)
		if err != nil {
			return Campaign{}, err
		}
		v := versionOf(id, c, d, s.now())
		v.OrgRef = cur.Version.OrgRef
		if err := s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error { return store.InsertVersion(ctx, tx, c.p.TenantID, &v, key) }); err != nil {
			return Campaign{}, classify(err)
		}
	}
	return s.get(ctx, c, id)
}

// CopyInput là phần đổi khi "sao chép đợt cũ": thời gian, hạn nộp mới; tên, người phụ trách bỏ trống → giữ của đợt nguồn.
type CopyInput struct {
	Name     string
	From, To string // YYYY-MM-DD
	DueAt    time.Time
	OwnerRef *uuid.UUID
}

// Copy "Sao chép đợt cũ" (FN-21 luồng phụ): đợt mới ở trạng thái Nháp mang mục tiêu, lĩnh vực, biểu điều tra, phạm vi (chế độ,
// khu vực, danh sách xã), người phụ trách, giờ nhắc của đợt nguồn (mọi trạng thái); KHÔNG mang số liệu, kỳ native, sự kiện,
// tiến độ của đợt nguồn — đợt mới phát hành thành kỳ native riêng. Phạm vi toàn tỉnh/khu vực giải lại theo khu vực hiện hành.
func (s *Service) Copy(ctx context.Context, src uuid.UUID, in CopyInput, idem string) (Campaign, bool, error) {
	c, err := s.writer(ctx)
	if err != nil {
		return Campaign{}, false, err
	}
	if idem == "" {
		return Campaign{}, false, problem.IdempotencyRequired
	}
	return s.insertNew(ctx, c, "cnts_campaign:copy:"+src.String()+":"+idem, func() (Input, error) {
		cur, err := s.load(ctx, c, src)
		if err != nil {
			return Input{}, err
		}
		v := cur.Version
		out := Input{Name: v.Name, Field: v.Field, TemplateCode: v.TemplateCode, TemplateVersion: v.TemplateVersion, ScopeMode: v.ScopeMode, Regions: v.Regions, Households: v.Households,
			Communes: v.Units, From: in.From, To: in.To, DueAt: in.DueAt, OwnerRef: v.OwnerRef, RemindBeforeHours: v.RemindBeforeHours}
		if v.Goal != nil {
			out.Goal = *v.Goal
		}
		if v.ScopeMode == domain.ScopeHouseholds {
			out.Communes = nil // xã giải lại theo hộ; hộ đã vô hiệu hóa sau đợt nguồn → 422, bỏ hộ đó khi sửa nháp
		}
		if strings.TrimSpace(in.Name) != "" {
			out.Name = in.Name
		}
		if in.OwnerRef != nil {
			out.OwnerRef = in.OwnerRef
		}
		return out, nil
	})
}
