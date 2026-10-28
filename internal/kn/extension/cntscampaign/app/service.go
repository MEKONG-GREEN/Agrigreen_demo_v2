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
	errNotCNTS    = problem.Forbidden.WithDetail("chiến dịch điều tra chỉ dành cho Phòng Chăn nuôi – Thủy sản (Cấp Tỉnh, Cấp Quốc gia xem) và Admin xem")
	errNotWriter  = problem.Forbidden.WithDetail("chỉ Cấp Tỉnh Phòng Chăn nuôi – Thủy sản tạo, phát hành, gia hạn, nhắc, đóng đợt")
	errNotLeader  = problem.Forbidden.WithDetail("mở lại đợt chỉ Lãnh đạo phòng (hoặc người được ủy quyền mở lại kỳ)")
)

// Reopener là phần cổng điều khiển kỳ native mở lại kỳ đã chốt (pf/forms app.Control.ReopenPeriod). Cổng
// formsport.PeriodControl chưa có thao tác này: gốc lắp ráp nối Control nào cài thêm Reopener thì mở lại đợt dùng được,
// không thì trả 503.
type Reopener interface {
	ReopenPeriod(ctx context.Context, profile string, id uuid.UUID, reason, idem string) error
}

// Service là use case chiến dịch điều tra.
type Service struct {
	DB    tenant.Beginner // pool của pf/extension (db.ForModule — r_extension)
	Clock clock.Clock
	// Forms, Control: cap/forms (biểu, phát hành kỳ native; tiến độ, chốt, đổi hạn). Thiếu → 503.
	Forms   formsport.Publisher
	Control formsport.PeriodControl
	// Figures: số liệu kỳ native (kết quả tổng hợp). Thiếu → kết quả 503.
	Figures formsport.FiguresReader
	// Orgs, Units: tỉnh của Nút (khu vực, tên xã). Regions: khu vực của tỉnh (phạm vi toàn tỉnh/khu vực, tiến độ theo khu
	// vực) — tùy chọn (nil → chỉ phạm vi danh sách xã, tiến độ gom "Chưa gán khu vực").
	Orgs    partyport.Orgs
	Units   partyport.AdminUnits
	Regions func() partyport.Regions
	Names   func() partyport.ActorNames
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
		v, err := store.Latest(ctx, tx, id)
		if err != nil {
			return err
		}
		if v == nil || !c.sees(*v) {
			return ErrNotFound
		}
		es, err := store.Events(ctx, tx, []uuid.UUID{id})
		if err != nil {
			return err
		}
		// Đợt đã phát hành: phiên bản mới nhất là phiên bản đã phát hành (trigger chặn phiên bản sau phát hành).
		out = build(*v, es[id])
		return nil
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
			out = append(out, build(v, es[v.CampaignID]))
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
	ScopeMode         string
	Regions           []string
	Communes          []string
	From, To          string // YYYY-MM-DD
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
	d := domain.Draft{Name: in.Name, Goal: in.Goal, Field: in.Field, TemplateCode: in.TemplateCode, ScopeMode: in.ScopeMode,
		Regions: in.Regions, Units: in.Communes, From: from, To: to, DueAt: in.DueAt, OwnerRef: in.OwnerRef,
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
	case domain.ScopeCommunes:
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
	if _, err := s.Forms.LatestTemplate(ctx, domain.Profile, d.TemplateCode); isNotFound(err) {
		return d, ErrTemplate.WithField("template_code", d.TemplateCode)
	} else if err != nil {
		return d, err
	}
	return d, nil
}

func versionOf(id uuid.UUID, c caller, d domain.Draft, now time.Time) store.Version {
	v := store.Version{RowID: newID(), CampaignID: id, OrgRef: c.k.OrgRef, Name: d.Name, Field: d.Field, TemplateCode: d.TemplateCode,
		ScopeMode: d.ScopeMode, Regions: d.Regions, Units: d.Units, From: d.From, To: d.To, DueAt: d.DueAt, OwnerRef: d.OwnerRef,
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
	key := "cnts_campaign:create:" + idem
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
	d, err := s.draftOf(ctx, c, in)
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
