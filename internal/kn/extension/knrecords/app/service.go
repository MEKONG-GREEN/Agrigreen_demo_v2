// Package app: hồ sơ KN riêng cho HTX, Nông hộ, Tổ hợp tác — KNSPLIT-2-KN-RECORDS (#67), nền của TT-08/TT-09 (#102).
//
// Thay đường ghi legacy /v1/kn/cooperatives (pf/party tạo OrgCooperative, sửa CoopProfiles) và nông hộ của HTX
// (pf/cooperative): hồ sơ ở sổ của KN (pf_extension.kn_producer), KN KHÔNG tạo/sửa HTX nền tảng, KHÔNG ghi bảng HTX.
// HTX đã công bố chỉ được ĐỌC qua publication pf_party_pub.v1_org_tree (liên kết đối chiếu tùy chọn htx_ref).
// Họ tên, SĐT người đại diện/chủ hộ chỉ vào kho định danh (mục đích extension.contact); API trả nhãn che.
//
// Quyền theo ngữ cảnh phiên KN (port.RequireSession, không fallback Principal.KN): Cấp Xã và Admin ghi trong xã phụ
// trách; Cấp Tỉnh, Cấp Quốc gia chỉ xem; Admin hỗ trợ ghi bắt buộc lý do. Phạm vi xã lọc trong SQL trước khi trả dữ
// liệu; ngoài phạm vi → 404 (không lộ sự tồn tại). Mọi lời gọi module/dịch vụ khác (đơn vị hành chính, publication,
// kho định danh) chạy NGOÀI giao dịch ghi; giao dịch chỉ đọc/ghi sổ của KN.
package app

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/knrecords/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/knrecords/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/vault/identity/client"
)

// Mã lỗi (docs/errors/kn-extension-records.md).
var (
	ErrNotFound      = problem.New(http.StatusNotFound, "extension.kn_record_not_found", "Không tìm thấy hồ sơ trong phạm vi phụ trách")
	ErrNameTaken     = problem.New(http.StatusConflict, "extension.kn_record_name_taken", "Tên đã có trong xã (cùng loại, đang hoạt động)")
	ErrPersonTaken   = problem.New(http.StatusConflict, "extension.kn_record_household_exists", "Chủ hộ đã có hồ sơ Nông hộ đang hoạt động")
	ErrVersion       = problem.New(http.StatusConflict, "extension.kn_record_version_conflict", "Hồ sơ đã được người khác cập nhật — tải lại rồi sửa")
	ErrState         = problem.New(http.StatusConflict, "extension.kn_record_state_conflict", "Trạng thái hồ sơ không cho phép thao tác này")
	ErrInvalid       = problem.Validation
	ErrReadOnly      = problem.New(http.StatusForbidden, "extension.read_only", "Cấp Quốc gia chỉ xem")
	ErrSupportReason = problem.New(http.StatusUnprocessableEntity, "extension.support_reason_required", "Chế độ hỗ trợ: thao tác ghi bắt buộc lý do")
	ErrDependency    = problem.New(http.StatusServiceUnavailable, "extension.dependency_unavailable", "Dịch vụ phụ thuộc chưa sẵn sàng")
	ErrOutOfScope    = problem.New(http.StatusForbidden, "extension.out_of_scope", "Ngoài phạm vi phụ trách")
	errWriter        = problem.Forbidden.WithDetail("chỉ Cấp Xã và Admin được thêm, sửa hồ sơ HTX, Nông hộ, Tổ hợp tác")
)

// Giới hạn đọc.
const (
	DefaultLimit   = 50
	MaxLimit       = 200
	MaxQuery       = 100
	MaxLogVersions = 100
)

// HTXDirectory đọc HTX ĐÃ CÔNG BỐ (pf_party_pub.v1_org_tree — platformread.Organizations). Chỉ đọc.
type HTXDirectory interface {
	Get(ctx context.Context, ref uuid.UUID) (partyport.Org, error)
	OrgsOfKind(ctx context.Context, kind partyport.OrgKind, units []string, activeOnly bool) ([]partyport.Org, error)
}

// Service là use case hồ sơ KN riêng.
type Service struct {
	DB    tenant.Beginner // pool vai r_extension (db.ForModule)
	Clock clock.Clock
	Units partyport.AdminUnits // tùy chọn: kiểm mã xã; nil → chỉ dựa vào phạm vi phiên (Admin toàn quốc → 503)
	HTX   HTXDirectory         // publication HTX (liên kết đối chiếu); nil → htx_ref trả 503
	Vault client.Client        // kho định danh; nil → ghi trả 503, đọc không có nhãn
	Names func() partyport.ActorNames
}

func (s *Service) now() time.Time { return s.Clock.Now().UTC() }

func (s *Service) tx(ctx context.Context, tid uuid.UUID, fn func(pgx.Tx) error) error {
	return tenant.WithTenantTx(ctx, s.DB, tid, fn)
}

type caller struct {
	p authz.Principal
	k port.SessionContext
}

func (s *Service) reader(ctx context.Context) (caller, error) {
	p, k, err := port.RequireSession(ctx)
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
	case c.k.Role != port.SessionRoleCommune && c.k.Role != port.SessionRoleAdmin:
		return c, errWriter
	case c.k.Support != nil && utf8.RuneCountInString(strings.TrimSpace(c.k.Support.Reason)) < domain.MinReason:
		return c, ErrSupportReason
	}
	return c, nil
}

func (c caller) sees(commune string) bool { return commune != "" && c.k.Scope.Allows(commune) }

func (c caller) meta(now time.Time, idem string) store.Producer {
	p := store.Producer{ID: ids.New(), EventTime: now, ActorRef: c.p.ActorRef, IdemKey: idem}
	if c.k.Support != nil {
		r := strings.TrimSpace(c.k.Support.Reason)
		p.SupportReason = &r
		if c.k.Support.OnBehalfOf != uuid.Nil {
			v := c.k.Support.OnBehalfOf
			p.OnBehalfOf = &v
		}
	}
	return p
}

func idemKey(k string) string { return "knp:" + k }

func fields(es domain.Errors) error {
	p := ErrInvalid
	for _, e := range es {
		p = p.WithField(e.Field, e.Reason)
	}
	return p
}

// ---------------------------------------------------------------------------
// Đọc
// ---------------------------------------------------------------------------

// Person là nhãn che của người đại diện/chủ hộ (RB-3).
type Person struct {
	Label       string `pii:"true"`
	MaskedPhone string `pii:"true"`
	Unavailable bool
}

// View là hồ sơ hiện hành cho API.
type View struct {
	store.Producer
	Status      string
	Person      Person
	ParentName  *string
	ParentKind  *string
	MemberCount int // Nông hộ đang hoạt động (HTX, THT)
	HTXName     *string
	Versions    []store.Producer
	Names       map[uuid.UUID]string
	CanEdit     bool
	Replayed    bool
}

func (s *Service) person(ctx context.Context, ref uuid.UUID) Person {
	if s.Vault == nil {
		return Person{Unavailable: true}
	}
	d, err := s.Vault.Display(ctx, client.PurposeContact, ref)
	if err != nil {
		return Person{Unavailable: true}
	}
	return Person{Label: d.Label, MaskedPhone: d.MaskedPhone}
}

func (s *Service) canEdit(c caller, p store.Producer) bool {
	if c.k.ReadOnly() || (c.k.Role != port.SessionRoleCommune && c.k.Role != port.SessionRoleAdmin) {
		return false
	}
	return c.sees(p.CommuneCode)
}

// Query là bộ lọc danh sách (mỗi danh sách — HTX, Nông hộ, THT — có bộ lọc riêng; TT-09).
type Query struct {
	Kind    string
	Commune string
	Status  string // active (mặc định) | inactive | all
	Q       string // HTX, THT: tên
	Parent  *uuid.UUID
	HTX     *uuid.UUID
	After   uuid.UUID
	Limit   int
}

// Page là một trang hồ sơ.
type Page struct {
	Items []View
	Next  *uuid.UUID
}

// List liệt kê hồ sơ một loại trong phạm vi phiên.
func (s *Service) List(ctx context.Context, q Query) (Page, error) {
	c, err := s.reader(ctx)
	if err != nil {
		return Page{}, err
	}
	if !domain.ValidKind(q.Kind) {
		return Page{}, ErrInvalid.WithField("kind", "bắt buộc: cooperative | household | coop_group")
	}
	switch q.Status {
	case "":
		q.Status = domain.StatusActive
	case domain.StatusActive, domain.StatusInactive, "all":
	default:
		return Page{}, ErrInvalid.WithField("status", "active | inactive | all")
	}
	q.Q = strings.TrimSpace(q.Q)
	if utf8.RuneCountInString(q.Q) > MaxQuery {
		return Page{}, ErrInvalid.WithField("q", "tối đa 100 ký tự")
	}
	if q.Kind == domain.KindHousehold && q.Q != "" {
		return Page{}, ErrInvalid.WithField("q", "Nông hộ: tên chủ hộ ở kho định danh — lọc theo xã, HTX/THT, trạng thái")
	}
	if q.Kind != domain.KindHousehold && q.Parent != nil {
		return Page{}, ErrInvalid.WithField("parent_ref", "chỉ dùng cho Nông hộ")
	}
	if q.Kind != domain.KindCooperative && q.HTX != nil {
		return Page{}, ErrInvalid.WithField("htx_ref", "chỉ dùng cho HTX")
	}
	q.Commune = strings.TrimSpace(q.Commune)
	if q.Commune != "" && !c.k.Scope.Allows(q.Commune) {
		return Page{}, ErrOutOfScope.WithDetail("xã ngoài phạm vi phụ trách")
	}
	if q.Limit <= 0 || q.Limit > MaxLimit {
		q.Limit = DefaultLimit
	}
	if c.k.Scope.Empty() {
		return Page{Items: []View{}}, nil
	}
	var rows []store.Producer
	counts := map[uuid.UUID]int{}
	parents := map[uuid.UUID]store.Producer{}
	err = s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
		var e error
		rows, e = store.List(ctx, tx, store.Filter{Kind: q.Kind, All: c.k.Scope.All, Units: c.k.Scope.Units, Commune: q.Commune,
			Status: q.Status, Q: q.Q, Parent: q.Parent, HTX: q.HTX, After: q.After, Limit: q.Limit + 1})
		if e != nil {
			return e
		}
		refs, prefs := make([]uuid.UUID, 0, len(rows)), []uuid.UUID{}
		for _, r := range rows {
			refs = append(refs, r.Ref)
			if r.ParentRef != nil {
				prefs = append(prefs, *r.ParentRef)
			}
		}
		if q.Kind != domain.KindHousehold {
			if counts, e = store.MemberCounts(ctx, tx, refs); e != nil {
				return e
			}
		}
		parents, e = store.Current(ctx, tx, prefs)
		return e
	})
	if err != nil {
		return Page{}, err
	}
	pg := Page{Items: make([]View, 0, len(rows))}
	if len(rows) > q.Limit {
		rows = rows[:q.Limit]
		n := rows[q.Limit-1].Ref
		pg.Next = &n
	}
	for _, r := range rows {
		v := View{Producer: r, Status: domain.StatusOf(r.Event), MemberCount: counts[r.Ref], CanEdit: s.canEdit(c, r)}
		s.parentInfo(c, &v, parents)
		v.Person = s.person(ctx, r.PersonRef)
		pg.Items = append(pg.Items, v)
	}
	return pg, nil
}

// parentInfo điền tên HTX/THT cha khi cha nằm trong phạm vi người gọi (không lộ tên ngoài phạm vi).
func (s *Service) parentInfo(c caller, v *View, parents map[uuid.UUID]store.Producer) {
	if v.ParentRef == nil {
		return
	}
	if p, ok := parents[*v.ParentRef]; ok && c.sees(p.CommuneCode) && p.Name != nil {
		n, k := *p.Name, p.Kind
		v.ParentName, v.ParentKind = &n, &k
	}
}

// Get đọc chi tiết hồ sơ kèm nhật ký phiên bản.
func (s *Service) Get(ctx context.Context, ref uuid.UUID) (View, error) {
	c, err := s.reader(ctx)
	if err != nil {
		return View{}, err
	}
	var vs []store.Producer
	counts := map[uuid.UUID]int{}
	parents := map[uuid.UUID]store.Producer{}
	err = s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
		var e error
		if vs, e = store.Versions(ctx, tx, ref, MaxLogVersions); e != nil || len(vs) == 0 {
			return e
		}
		cur := vs[len(vs)-1]
		if !c.sees(cur.CommuneCode) {
			vs = nil
			return nil
		}
		if cur.Kind != domain.KindHousehold {
			if counts, e = store.MemberCounts(ctx, tx, []uuid.UUID{ref}); e != nil {
				return e
			}
		}
		if cur.ParentRef != nil {
			parents, e = store.Current(ctx, tx, []uuid.UUID{*cur.ParentRef})
		}
		return e
	})
	if err != nil {
		return View{}, err
	}
	if len(vs) == 0 {
		return View{}, ErrNotFound
	}
	cur := vs[len(vs)-1]
	v := View{Producer: cur, Status: domain.StatusOf(cur.Event), MemberCount: counts[ref], Versions: vs, CanEdit: s.canEdit(c, cur)}
	s.parentInfo(c, &v, parents)
	v.Person = s.person(ctx, cur.PersonRef)
	if cur.HTXRef != nil && s.HTX != nil {
		if o, err := s.HTX.Get(ctx, *cur.HTXRef); err == nil && o.Kind == partyport.OrgCooperative && c.sees(o.AdminUnitCode) {
			n := o.Name
			v.HTXName = &n
		}
	}
	v.Names = s.names(ctx, vs)
	return v, nil
}

func (s *Service) names(ctx context.Context, vs []store.Producer) map[uuid.UUID]string {
	out := map[uuid.UUID]string{}
	if s.Names == nil {
		return out
	}
	n := s.Names()
	if n == nil {
		return out
	}
	refs := make([]uuid.UUID, 0, len(vs))
	for _, v := range vs {
		refs = append(refs, v.ActorRef)
	}
	m, err := n.DisplayNames(ctx, refs)
	if err != nil {
		return out
	}
	return m
}

// PublishedHTX là một HTX đã công bố trong phạm vi (để liên kết đối chiếu khi thêm hồ sơ HTX).
type PublishedHTX struct {
	Ref         uuid.UUID
	Name        string
	CommuneCode string
	LinkedRef   *uuid.UUID // hồ sơ KN đang liên kết (nếu có)
}

// PublishedHTXs liệt kê HTX đã công bố, đang hoạt động, trong phạm vi phiên (chỉ đọc publication).
func (s *Service) PublishedHTXs(ctx context.Context, commune string) ([]PublishedHTX, error) {
	c, err := s.reader(ctx)
	if err != nil {
		return nil, err
	}
	if s.HTX == nil {
		return nil, ErrDependency.WithDetail("chưa nối danh mục HTX đã công bố")
	}
	commune = strings.TrimSpace(commune)
	var units []string
	switch {
	case commune != "":
		if !c.k.Scope.Allows(commune) {
			return nil, ErrOutOfScope.WithDetail("xã ngoài phạm vi phụ trách")
		}
		units = []string{commune}
	case c.k.Scope.All:
		return nil, ErrInvalid.WithField("commune", "bắt buộc khi phạm vi toàn quốc")
	case c.k.Scope.Empty():
		return []PublishedHTX{}, nil
	default:
		units = append([]string{}, c.k.Scope.Units...)
	}
	orgs, err := s.HTX.OrgsOfKind(ctx, partyport.OrgCooperative, units, true)
	if err != nil {
		return nil, ErrDependency.WithDetail("không đọc được HTX đã công bố").WithCause(err)
	}
	out := make([]PublishedHTX, 0, len(orgs))
	for _, o := range orgs {
		if !c.sees(o.AdminUnitCode) {
			continue
		}
		out = append(out, PublishedHTX{Ref: o.Ref, Name: o.Name, CommuneCode: o.AdminUnitCode})
		if len(out) >= MaxLimit {
			break
		}
	}
	if len(out) == 0 {
		return out, nil
	}
	err = s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
		for i := range out {
			ref := out[i].Ref
			rows, e := store.List(ctx, tx, store.Filter{Kind: domain.KindCooperative, All: c.k.Scope.All, Units: c.k.Scope.Units,
				Status: domain.StatusActive, HTX: &ref, Limit: 1})
			if e != nil {
				return e
			}
			if len(rows) == 1 {
				r := rows[0].Ref
				out[i].LinkedRef = &r
			}
		}
		return nil
	})
	return out, err
}

// ---------------------------------------------------------------------------
// Ghi
// ---------------------------------------------------------------------------

// Input là thân tạo/sửa. Sửa thay toàn bộ trường nhập được (trường vắng = xóa giá trị tùy chọn); Person nil khi sửa =
// giữ người đại diện/chủ hộ hiện tại.
type Input struct {
	domain.Record
	AreaRaw     string
	Person      *domain.Person
	ParentRef   *uuid.UUID
	HTXRef      *uuid.UUID
	BaseVersion *int64
	IdemKey     string
}

// checked là dữ liệu đã kiểm, sẵn sàng ghi.
type checked struct {
	rec    domain.Record
	parent *uuid.UUID
	htx    *uuid.UUID
}

// validate kiểm rẻ trước (cú pháp, độ dài, số) rồi tới cổng ngoài giao dịch (đơn vị hành chính, publication HTX).
func (s *Service) validate(ctx context.Context, c caller, in *Input, create bool) (checked, error) {
	es := in.Normalize()
	area, aerr := domain.ParseArea(in.AreaRaw)
	if aerr != nil {
		es = append(es, domain.FieldError{Field: "area_ha", Reason: aerr.Error()})
	}
	if in.Person != nil {
		prefix := "representative"
		if in.Kind == domain.KindHousehold {
			prefix = "holder"
		}
		es = append(es, in.Person.Validate(prefix)...)
	} else if create {
		if in.Kind == domain.KindHousehold {
			es = append(es, domain.FieldError{Field: "holder.full_name", Reason: "bắt buộc"}, domain.FieldError{Field: "holder.phone", Reason: "bắt buộc"})
		} else if domain.ValidKind(in.Kind) {
			es = append(es, domain.FieldError{Field: "representative.full_name", Reason: "bắt buộc"},
				domain.FieldError{Field: "representative.phone", Reason: "bắt buộc"})
		}
	}
	if in.ParentRef != nil && in.Kind != domain.KindHousehold {
		es = append(es, domain.FieldError{Field: "parent_ref", Reason: "chỉ Nông hộ thuộc HTX/Tổ hợp tác"})
	}
	if in.HTXRef != nil && in.Kind != domain.KindCooperative {
		es = append(es, domain.FieldError{Field: "htx_ref", Reason: "chỉ hồ sơ HTX liên kết HTX đã công bố"})
	}
	if len(es) > 0 {
		return checked{}, fields(es)
	}
	in.AreaHa = area
	if !c.k.Scope.Allows(in.CommuneCode) {
		return checked{}, ErrOutOfScope.WithDetail("xã ngoài phạm vi phụ trách")
	}
	if err := s.commune(ctx, c, in.CommuneCode); err != nil {
		return checked{}, err
	}
	if in.HTXRef != nil {
		if err := s.htxLink(ctx, c, *in.HTXRef, in.CommuneCode); err != nil {
			return checked{}, err
		}
	}
	return checked{rec: in.Record, parent: in.ParentRef, htx: in.HTXRef}, nil
}

// commune kiểm mã là xã/phường còn hiệu lực (khi có cổng đơn vị hành chính).
func (s *Service) commune(ctx context.Context, c caller, code string) error {
	if s.Units == nil {
		if c.k.Scope.All {
			return ErrDependency.WithDetail("chưa nối danh mục đơn vị hành chính")
		}
		return nil // phạm vi Cấp Xã đã giới hạn mã xã
	}
	u, err := s.Units.Get(ctx, code, s.now())
	if errors.Is(err, partyport.ErrUnitNotFound) {
		return ErrInvalid.WithField("commune_code", "không có xã/phường còn hiệu lực")
	}
	if err != nil {
		return ErrDependency.WithDetail("không tra được đơn vị hành chính").WithCause(err)
	}
	if u.Level != partyport.LevelCommune {
		return ErrInvalid.WithField("commune_code", "phải là xã/phường")
	}
	return nil
}

// htxLink kiểm HTX đã công bố (chỉ đọc publication): là HTX, đang hoạt động, cùng xã, trong phạm vi.
func (s *Service) htxLink(ctx context.Context, c caller, ref uuid.UUID, commune string) error {
	if s.HTX == nil {
		return ErrDependency.WithDetail("chưa nối danh mục HTX đã công bố")
	}
	o, err := s.HTX.Get(ctx, ref)
	if errors.Is(err, partyport.ErrOrgNotFound) {
		return ErrInvalid.WithField("htx_ref", "không có HTX đã công bố")
	}
	if err != nil {
		return ErrDependency.WithDetail("không tra được HTX đã công bố").WithCause(err)
	}
	if o.Kind != partyport.OrgCooperative || !c.sees(o.AdminUnitCode) || !o.Active {
		return ErrInvalid.WithField("htx_ref", "không có HTX đã công bố, đang hoạt động trong phạm vi")
	}
	if o.AdminUnitCode != commune {
		return ErrInvalid.WithField("htx_ref", "HTX đã công bố ở xã khác")
	}
	return nil
}

// vaultPerson ghi người đại diện/chủ hộ vào kho định danh (NGOÀI giao dịch); trùng SĐT → dùng chủ thể cũ (idempotent).
func (s *Service) vaultPerson(ctx context.Context, kind, commune string, p domain.Person) (uuid.UUID, error) {
	if s.Vault == nil {
		return uuid.Nil, ErrDependency.WithDetail("chưa nối kho định danh")
	}
	sk := client.SubjectPerson
	if kind == domain.KindHousehold {
		sk = client.SubjectHousehold
	}
	cr, err := s.Vault.Create(ctx, client.PurposeContact, client.Identity{Kind: sk, FullName: p.FullName, Phone: p.Phone, AreaCode: commune})
	if errors.Is(err, client.ErrDuplicate) && cr.Ref != uuid.Nil {
		return cr.Ref, nil
	}
	if err != nil {
		return uuid.Nil, ErrDependency.WithDetail("không ghi được kho định danh").WithCause(err)
	}
	return cr.Ref, nil
}

// checkParent (trong giao dịch, sổ của KN): HTX/THT cha tồn tại, trong phạm vi, đang hoạt động.
func checkParent(ctx context.Context, tx pgx.Tx, c caller, ref uuid.UUID) error {
	p, err := store.Latest(ctx, tx, ref)
	if err != nil {
		return err
	}
	if p == nil || !c.sees(p.CommuneCode) || !domain.ParentAllowed(p.Kind) {
		return ErrInvalid.WithField("parent_ref", "không có HTX/Tổ hợp tác trong phạm vi")
	}
	if p.Event == domain.EventDeactivated {
		return ErrInvalid.WithField("parent_ref", "HTX/Tổ hợp tác đã ngừng hoạt động")
	}
	return nil
}

// checkUnique (trong giao dịch, sau LockName): tên trùng (HTX, THT) hoặc chủ hộ trùng (Nông hộ).
func checkUnique(ctx context.Context, tx pgx.Tx, kind, commune, name string, person, except uuid.UUID) error {
	if kind == domain.KindHousehold {
		taken, err := store.PersonTaken(ctx, tx, kind, person, except)
		if err != nil {
			return err
		}
		if taken {
			return ErrPersonTaken
		}
		return nil
	}
	names, err := store.ActiveNames(ctx, tx, kind, commune, except)
	if err != nil {
		return err
	}
	key := domain.NameKey(name)
	for _, n := range names {
		if domain.NameKey(n) == key {
			return ErrNameTaken.WithField("name", "trùng tên "+domain.KindLabel(kind)+" trong xã")
		}
	}
	return nil
}

func optStr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func fill(p *store.Producer, r domain.Record, parent, htx *uuid.UUID) {
	p.CommuneCode, p.Name, p.AreaHa, p.TaxCode, p.Address = r.CommuneCode, optStr(r.Name), r.AreaHa, optStr(r.TaxCode), optStr(r.Address)
	p.ParentRef, p.HTXRef = parent, htx
}

// Create thêm hồ sơ (Cấp Xã, Admin). Lặp cùng Idempotency-Key → trả hồ sơ đã tạo (không ghi thêm, không tạo chủ thể mới).
func (s *Service) Create(ctx context.Context, in Input) (View, error) {
	c, err := s.writer(ctx)
	if err != nil {
		return View{}, err
	}
	if strings.TrimSpace(in.IdemKey) == "" {
		return View{}, problem.IdempotencyRequired
	}
	key := idemKey(in.IdemKey)
	if prev, err := s.replayed(ctx, c, key); err != nil || prev != nil {
		if prev != nil {
			v, err := s.Get(ctx, prev.Ref)
			v.Replayed = true
			return v, err
		}
		return View{}, err
	}
	ok, err := s.validate(ctx, c, &in, true)
	if err != nil {
		return View{}, err
	}
	person, err := s.vaultPerson(ctx, in.Kind, ok.rec.CommuneCode, *in.Person)
	if err != nil {
		return View{}, err
	}
	ref := ids.New()
	replayed := false
	err = s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
		if prev, err := store.ByIdem(ctx, tx, key); err != nil || prev != nil {
			if prev != nil {
				ref, replayed = prev.Ref, true
			}
			return err
		}
		if err := store.LockName(ctx, tx, ok.rec.Kind, ok.rec.CommuneCode); err != nil {
			return err
		}
		if ok.parent != nil {
			if err := checkParent(ctx, tx, c, *ok.parent); err != nil {
				return err
			}
		}
		if err := checkUnique(ctx, tx, ok.rec.Kind, ok.rec.CommuneCode, ok.rec.Name, person, uuid.Nil); err != nil {
			return err
		}
		row := c.meta(s.now(), key)
		row.Ref, row.Kind, row.Event, row.PersonRef = ref, ok.rec.Kind, domain.EventCreated, person
		fill(&row, ok.rec, ok.parent, ok.htx)
		return store.Insert(ctx, tx, c.p.TenantID, &row)
	})
	if err != nil {
		return View{}, err
	}
	v, err := s.Get(ctx, ref)
	v.Replayed = replayed
	return v, err
}

func (s *Service) replayed(ctx context.Context, c caller, key string) (*store.Producer, error) {
	var prev *store.Producer
	err := s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
		var e error
		prev, e = store.ByIdem(ctx, tx, key)
		return e
	})
	return prev, err
}

// current đọc bản hiện hành trong phạm vi (giao dịch ngắn, chỉ sổ KN).
func (s *Service) current(ctx context.Context, c caller, ref uuid.UUID) (store.Producer, error) {
	var cur *store.Producer
	err := s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
		var e error
		cur, e = store.Latest(ctx, tx, ref)
		return e
	})
	if err != nil {
		return store.Producer{}, err
	}
	if cur == nil || !c.sees(cur.CommuneCode) {
		return store.Producer{}, ErrNotFound
	}
	return *cur, nil
}

func sameDec(a, b *decimal.Decimal) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return a.Equal(*b)
}

func sameRef(a, b *uuid.UUID) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

func sameStr(a, b *string) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

func unchanged(cur, next store.Producer) bool {
	return cur.CommuneCode == next.CommuneCode && sameStr(cur.Name, next.Name) && cur.PersonRef == next.PersonRef &&
		sameRef(cur.ParentRef, next.ParentRef) && sameRef(cur.HTXRef, next.HTXRef) && sameDec(cur.AreaHa, next.AreaHa) &&
		sameStr(cur.TaxCode, next.TaxCode) && sameStr(cur.Address, next.Address)
}

// Update sửa hồ sơ (TT-08 "có sửa"): khóa lạc quan theo base_version; loại không đổi; hồ sơ ngừng hoạt động phải kích
// hoạt lại trước. Không đổi gì → không ghi sổ.
func (s *Service) Update(ctx context.Context, ref uuid.UUID, in Input) (View, error) {
	c, err := s.writer(ctx)
	if err != nil {
		return View{}, err
	}
	if in.BaseVersion == nil {
		return View{}, ErrInvalid.WithField("base_version", "bắt buộc")
	}
	key := idemKey(in.IdemKey)
	if in.IdemKey != "" {
		if prev, err := s.replayed(ctx, c, key); err != nil || prev != nil {
			if prev != nil {
				return s.Get(ctx, prev.Ref)
			}
			return View{}, err
		}
	} else {
		key = idemKey("upd:" + ids.New().String())
	}
	cur, err := s.current(ctx, c, ref)
	if err != nil {
		return View{}, err
	}
	if in.Kind == "" {
		in.Kind = cur.Kind
	}
	if in.Kind != cur.Kind {
		return View{}, ErrInvalid.WithField("kind", "không đổi loại hồ sơ — vô hiệu hóa và thêm hồ sơ mới")
	}
	if cur.Event == domain.EventDeactivated {
		return View{}, ErrState.WithDetail("hồ sơ đã ngừng hoạt động — kích hoạt lại trước khi sửa")
	}
	if *in.BaseVersion != cur.Seq {
		return View{}, ErrVersion
	}
	ok, err := s.validate(ctx, c, &in, false)
	if err != nil {
		return View{}, err
	}
	person := cur.PersonRef
	if in.Person != nil {
		if person, err = s.vaultPerson(ctx, cur.Kind, ok.rec.CommuneCode, *in.Person); err != nil {
			return View{}, err
		}
	}
	err = s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
		if prev, err := store.ByIdem(ctx, tx, key); err != nil || prev != nil {
			return err
		}
		if err := store.Lock(ctx, tx, ref); err != nil {
			return err
		}
		if err := store.LockName(ctx, tx, cur.Kind, ok.rec.CommuneCode); err != nil {
			return err
		}
		now, err := store.Latest(ctx, tx, ref)
		if err != nil {
			return err
		}
		if now == nil || now.Seq != *in.BaseVersion {
			return ErrVersion
		}
		if now.Event == domain.EventDeactivated {
			return ErrState.WithDetail("hồ sơ đã ngừng hoạt động — kích hoạt lại trước khi sửa")
		}
		next := *now
		next.PersonRef = person
		fill(&next, ok.rec, ok.parent, ok.htx)
		if unchanged(*now, next) {
			return nil
		}
		if ok.parent != nil {
			if *ok.parent == ref {
				return ErrInvalid.WithField("parent_ref", "không tự liên kết")
			}
			if err := checkParent(ctx, tx, c, *ok.parent); err != nil {
				return err
			}
		}
		if err := checkUnique(ctx, tx, cur.Kind, ok.rec.CommuneCode, ok.rec.Name, person, ref); err != nil {
			return err
		}
		m := c.meta(s.now(), key)
		next.ID, next.EventTime, next.ActorRef, next.OnBehalfOf, next.SupportReason, next.IdemKey = m.ID, m.EventTime, m.ActorRef, m.OnBehalfOf, m.SupportReason, m.IdemKey
		next.Event, next.StatusReason = domain.EventUpdated, nil
		return store.Insert(ctx, tx, c.p.TenantID, &next)
	})
	if err != nil {
		return View{}, err
	}
	return s.Get(ctx, ref)
}

// SetStatus vô hiệu hóa (thay xóa) hoặc kích hoạt lại; lý do bắt buộc. Vô hiệu hóa HTX/THT không đổi Nông hộ thành viên.
func (s *Service) SetStatus(ctx context.Context, ref uuid.UUID, deactivate bool, base *int64, reason, idem string) (View, error) {
	c, err := s.writer(ctx)
	if err != nil {
		return View{}, err
	}
	if base == nil {
		return View{}, ErrInvalid.WithField("base_version", "bắt buộc")
	}
	reason, bad := domain.ValidReason(reason)
	if bad != "" {
		return View{}, ErrInvalid.WithField("reason", bad)
	}
	if strings.TrimSpace(idem) == "" {
		return View{}, problem.IdempotencyRequired
	}
	key := idemKey(idem)
	cur, err := s.current(ctx, c, ref)
	if err != nil {
		return View{}, err
	}
	err = s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
		if prev, err := store.ByIdem(ctx, tx, key); err != nil || prev != nil {
			return err
		}
		if err := store.Lock(ctx, tx, ref); err != nil {
			return err
		}
		if err := store.LockName(ctx, tx, cur.Kind, cur.CommuneCode); err != nil {
			return err
		}
		now, err := store.Latest(ctx, tx, ref)
		if err != nil {
			return err
		}
		if now == nil || now.Seq != *base {
			return ErrVersion
		}
		if (now.Event == domain.EventDeactivated) == deactivate {
			return ErrState.WithDetail("hồ sơ đã ở trạng thái này")
		}
		ev := domain.EventDeactivated
		if !deactivate {
			ev = domain.EventReactivated
			name := ""
			if now.Name != nil {
				name = *now.Name
			}
			if err := checkUnique(ctx, tx, now.Kind, now.CommuneCode, name, now.PersonRef, ref); err != nil {
				return err
			}
		}
		next := *now
		m := c.meta(s.now(), key)
		next.ID, next.EventTime, next.ActorRef, next.OnBehalfOf, next.SupportReason, next.IdemKey = m.ID, m.EventTime, m.ActorRef, m.OnBehalfOf, m.SupportReason, m.IdemKey
		next.Event, next.StatusReason = ev, &reason
		return store.Insert(ctx, tx, c.p.TenantID, &next)
	})
	if err != nil {
		return View{}, err
	}
	return s.Get(ctx, ref)
}
