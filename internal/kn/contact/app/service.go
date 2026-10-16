// Package app là phần cài đặt của pf/contact (xem tài liệu gói gốc internal/kn/contact).
package app

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/contact/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/contact/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/contact/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/db"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/vault/identity/client"
)

// DBModule: giao dịch của module chạy SET LOCAL ROLE r_contact (ADR-0028). Pool là vai đăng nhập của đơn vị
// (app_platform), là thành viên r_contact.
var DBModule = db.Module("pf/contact")

// Service là danh bạ dùng chung. KHÔNG ghi log dữ liệu cá nhân (R-14): service không log; lỗi trả về chỉ mang tên trường.
type Service struct {
	DB     tenant.Beginner
	Clock  clock.Clock
	Access port.Access
	// Vault: kho định danh (bắt buộc phần mở rộng client.Directory để đọc thẻ; client.Profiles để ghi liên hệ ngoài tỉnh).
	Vault client.Client
	// Staff (tùy chọn): nguồn cán bộ thuộc tỉnh (pf/hrm). nil → danh bạ chỉ có liên hệ ngoài tỉnh.
	Staff port.Staff
	// Labels (tùy chọn): tên đơn vị nội bộ, tên xã. nil → hiện mã.
	Labels port.Labels
}

var errNoPool = errors.New("contact: chưa nối pool cho giao dịch của module")

// NewService dựng Service; cấu hình quyền thiếu vai → lỗi.
func NewService(b tenant.Beginner, clk clock.Clock, acc port.Access, v client.Client) (*Service, error) {
	if clk == nil {
		clk = clock.System{}
	}
	if len(acc.FullBy)+len(acc.BasicBy)+len(acc.RealmFull)+len(acc.RealmBasic) == 0 {
		return nil, fmt.Errorf("contact: cấu hình quyền thiếu vai xem")
	}
	return &Service{DB: b, Clock: clk, Access: acc, Vault: v}, nil
}

// caller là người gọi đã kiểm quyền.
type caller struct {
	ref, tenant uuid.UUID
	set         domain.FieldSet
	manage      bool
}

// who kiểm người gọi: ngữ cảnh KN (X-AG-Context) thắng vai realm.
func (s *Service) who(ctx context.Context) (caller, error) {
	p, ok := authz.FromContext(ctx)
	if !ok {
		return caller{}, problem.Unauthenticated
	}
	c := caller{ref: p.ActorRef, tenant: p.TenantID}
	a := s.Access
	if p.KN != nil {
		r := p.KN.Role
		switch {
		case slices.Contains(a.FullBy, r):
			c.set = domain.SetFull
		case slices.Contains(a.BasicBy, r):
			c.set = domain.SetBasic
		default:
			return c, problem.Forbidden.WithDetail("Contact: vai trò không có quyền xem danh bạ")
		}
		c.manage = !p.KN.ReadOnly() && (slices.Contains(a.ManageBy, r) ||
			(slices.Contains(a.ManageDeptBy, r) && a.ManageDepartment != "" && p.KN.Department == a.ManageDepartment))
		return c, nil
	}
	for _, r := range p.Roles {
		switch {
		case slices.Contains(a.RealmFull, r):
			c.set = domain.SetFull
		case slices.Contains(a.RealmBasic, r) && c.set == "":
			c.set = domain.SetBasic
		}
		c.manage = c.manage || slices.Contains(a.RealmManage, r)
	}
	if c.set == "" {
		return c, problem.Forbidden.WithDetail("Contact: vai trò không có quyền xem danh bạ")
	}
	return c, nil
}

func (s *Service) tx(ctx context.Context, tid uuid.UUID, fn func(pgx.Tx) error) error {
	if s.DB == nil {
		return errNoPool
	}
	return db.WithModuleTenantTx(ctx, s.DB, DBModule, tid, fn)
}

func (s *Service) now() time.Time { return s.Clock.Now().UTC() }

func (s *Service) directory() (client.Directory, error) {
	d, ok := client.AsDirectory(s.Vault)
	if s.Vault == nil || !ok {
		return nil, port.ErrVaultUnavailable
	}
	return d, nil
}

func (s *Service) profiles() (client.Profiles, error) {
	p, ok := client.AsProfiles(s.Vault)
	if s.Vault == nil || !ok {
		return nil, port.ErrVaultUnavailable
	}
	return p, nil
}

// --- Quyền của người gọi. ---

// Access là quyền của người gọi trên Contact (GET /v1/contacts/access).
type Access struct {
	Set         domain.FieldSet
	Fields      []string
	CanManage   bool
	CanDetail   bool
	StaffSource string // enabled | disabled | none
}

// MyAccess trả bộ trường, quyền cập nhật và trạng thái nguồn cán bộ thuộc tỉnh.
func (s *Service) MyAccess(ctx context.Context) (Access, error) {
	c, err := s.who(ctx)
	if err != nil {
		return Access{}, err
	}
	src := "none"
	if s.Staff != nil {
		src = "enabled"
		if _, err := s.Staff.Staff(ctx); errors.Is(err, port.ErrStaffDisabled) {
			src = "disabled"
		} else if err != nil {
			return Access{}, err
		}
	}
	return Access{Set: c.set, Fields: c.set.Fields(), CanManage: c.manage, CanDetail: c.set == domain.SetFull, StaffSource: src}, nil
}

// --- Thẻ Cán bộ. ---

// Nguồn của liên hệ.
const (
	SourceStaff    = "staff"    // cán bộ thuộc tỉnh — đồng bộ từ hồ sơ nhân sự (FN-07)
	SourceExternal = "external" // liên hệ ngoài tỉnh — HCTH/Admin nhập
)

// Person là một dòng thẻ Cán bộ. Trường ngoài bộ của người gọi để trống — handler chỉ đưa ra trường của bộ.
type Person struct {
	ID        uuid.UUID
	Source    string
	FullName  string
	BirthYear string
	Phone     string
	Title     string
	Unit      string // đơn vị công tác (cán bộ thuộc tỉnh) hoặc cơ quan (liên hệ ngoài tỉnh)
	UnitCode  string
	Level     string
	Email     string
	Hidden    bool
	Erased    bool
}

// cand là ứng viên trước khi đọc kho — không PII.
type cand struct {
	ref     uuid.UUID
	source  string
	level   string
	unit    string
	unitKey string
	title   string
	hidden  bool
	commune string
}

// candidates nạp mọi liên hệ (không PII). staffOn = false khi nguồn cán bộ tắt.
func (s *Service) candidates(ctx context.Context, c caller, includeHidden bool) ([]cand, bool, error) {
	var out []cand
	seen := map[uuid.UUID]bool{}
	staffOn := s.Staff != nil
	if s.Staff != nil {
		st, err := s.Staff.Staff(ctx)
		switch {
		case errors.Is(err, port.ErrStaffDisabled):
			staffOn = false
		case err != nil:
			return nil, false, err
		}
		var units, communes map[string]string
		if s.Labels != nil && len(st) > 0 {
			units, _ = s.Labels.UnitNames(ctx)
			var codes []string
			for _, e := range st {
				if e.Commune != "" && !slices.Contains(codes, e.Commune) {
					codes = append(codes, e.Commune)
				}
			}
			if len(codes) > 0 {
				communes, _ = s.Labels.CommuneNames(ctx, codes)
			}
		}
		for _, e := range st {
			if !e.Active || seen[e.SubjectRef] {
				continue
			}
			seen[e.SubjectRef] = true
			unit := cmp.Or(units[e.Unit], e.Unit)
			if e.Level == port.LevelXa && e.Commune != "" {
				unit = strings.TrimSpace(cmp.Or(communes[e.Commune], e.Commune) + " · " + unit)
				unit = strings.TrimSuffix(unit, " ·")
			}
			out = append(out, cand{ref: e.SubjectRef, source: SourceStaff, level: e.Level, unit: unit, unitKey: e.Unit,
				title: e.Title, commune: e.Commune})
		}
	}
	err := s.tx(ctx, c.tenant, func(tx pgx.Tx) error {
		rows, err := store.CurrentPersons(ctx, tx)
		if err != nil {
			return err
		}
		for _, r := range rows {
			if seen[r.SubjectRef] || (r.Hidden && (!includeHidden || !c.manage)) {
				continue
			}
			seen[r.SubjectRef] = true
			out = append(out, cand{ref: r.SubjectRef, source: SourceExternal, level: r.Meta.Level, unit: deref(r.Meta.Organization),
				title: deref(r.Meta.Title), hidden: r.Hidden})
		}
		return nil
	})
	return out, staffOn, err
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

var levelOrder = map[string]int{port.LevelTW: 0, port.LevelTinh: 1, port.LevelXa: 2, port.LevelKhac: 3}

// cardFields: trường thẻ kho theo bộ — bộ SetBasic KHÔNG hỏi email (kho không trả về).
func cardFields(set domain.FieldSet) []string {
	f := []string{client.CardFullName, client.CardBirthYear, client.CardPhone}
	if set == domain.SetFull {
		f = append(f, client.CardEmail)
	}
	return f
}

// cards đọc thẻ của refs theo lô (≤ MaxCardRefs/lần).
func (s *Service) cards(ctx context.Context, refs []uuid.UUID, set domain.FieldSet) (map[uuid.UUID]client.Card, error) {
	out := make(map[uuid.UUID]client.Card, len(refs))
	if len(refs) == 0 {
		return out, nil
	}
	d, err := s.directory()
	if err != nil {
		return nil, err
	}
	for chunk := range slices.Chunk(refs, client.MaxCardRefs) {
		cs, err := d.Cards(ctx, client.PurposeContactDirectory, chunk, cardFields(set))
		if err != nil {
			return nil, err
		}
		for _, c := range cs {
			out[c.Ref] = c
		}
	}
	return out, nil
}

// person dựng dòng trả về: CHỈ trường của bộ người gọi.
func person(x cand, cd client.Card, set domain.FieldSet) Person {
	p := Person{ID: x.ref, FullName: cd.FullName, BirthYear: cd.BirthYear, Phone: domain.LocalPhone(cd.Phone), Erased: cd.Erased}
	if set == domain.SetFull {
		p.Source, p.Title, p.Unit, p.UnitCode, p.Level, p.Email, p.Hidden = x.source, x.title, x.unit, x.unitKey, x.level, cd.Email, x.hidden
	}
	return p
}

// ListFilter là bộ lọc thẻ Cán bộ. Level, Unit, Title, IncludeHidden chỉ áp cho bộ SetFull (FN-13 B2); bộ SetBasic bỏ
// qua để không suy ra trường bị ẩn.
type ListFilter struct {
	Q             string
	Level         string
	Unit          string
	Title         string
	IncludeHidden bool
	Offset        int
	Limit         int
}

// ListResult là một trang thẻ Cán bộ.
type ListResult struct {
	Set         domain.FieldSet
	Total       int
	StaffSource string
	Items       []Person
}

// MaxCandidates giới hạn số liên hệ duyệt khi tìm theo họ tên/SĐT (họ tên chỉ ở kho — đọc theo lô).
const MaxCandidates = 5000

// ListPersons trả thẻ Cán bộ theo vai. Không có từ khóa: sắp theo cấp, đơn vị (không PII) và chỉ đọc kho cho trang hiện
// tại. Có từ khóa: đọc thẻ của mọi ứng viên (đã lọc không PII) rồi so họ tên/SĐT (bộ SetFull: cả đơn vị, chức danh, email).
func (s *Service) ListPersons(ctx context.Context, f ListFilter) (ListResult, error) {
	c, err := s.who(ctx)
	if err != nil {
		return ListResult{}, err
	}
	if f.Limit <= 0 || f.Limit > 200 {
		f.Limit = 50
	}
	f.Offset = max(f.Offset, 0)
	full := c.set == domain.SetFull
	if !full {
		f.Level, f.Unit, f.Title, f.IncludeHidden = "", "", "", false
	}
	if f.Level != "" {
		if err := domain.CheckLevel(f.Level); err != nil {
			return ListResult{}, problem.Validation.WithField("level", "tw, tinh, xa hoặc khac")
		}
	}
	all, staffOn, err := s.candidates(ctx, c, f.IncludeHidden)
	if err != nil {
		return ListResult{}, err
	}
	res := ListResult{Set: c.set, StaffSource: "none"}
	if s.Staff != nil {
		res.StaffSource = map[bool]string{true: "enabled", false: "disabled"}[staffOn]
	}
	unitQ, titleQ := domain.Fold(f.Unit), domain.Fold(f.Title)
	var cs []cand
	for _, x := range all {
		switch {
		case f.Level != "" && x.level != f.Level:
		case unitQ != "" && x.unitKey != f.Unit && !strings.Contains(domain.Fold(x.unit), unitQ):
		case titleQ != "" && !strings.Contains(domain.Fold(x.title), titleQ):
		default:
			cs = append(cs, x)
		}
	}
	slices.SortStableFunc(cs, func(a, b cand) int {
		return cmp.Or(cmp.Compare(levelOrder[a.level], levelOrder[b.level]), cmp.Compare(domain.Fold(a.unit), domain.Fold(b.unit)),
			cmp.Compare(a.ref.String(), b.ref.String()))
	})
	if strings.TrimSpace(f.Q) == "" {
		res.Total = len(cs)
		page := cs[min(f.Offset, len(cs)):min(f.Offset+f.Limit, len(cs))]
		refs := make([]uuid.UUID, len(page))
		for i, x := range page {
			refs[i] = x.ref
		}
		cards, err := s.cards(ctx, refs, c.set)
		if err != nil {
			return ListResult{}, err
		}
		for _, x := range page {
			res.Items = append(res.Items, person(x, cards[x.ref], c.set))
		}
		return res, nil
	}
	if len(cs) > MaxCandidates {
		return ListResult{}, problem.Validation.WithDetail("danh bạ quá lớn để tìm theo họ tên — lọc theo cấp/đơn vị trước")
	}
	refs := make([]uuid.UUID, len(cs))
	for i, x := range cs {
		refs[i] = x.ref
	}
	cards, err := s.cards(ctx, refs, c.set)
	if err != nil {
		return ListResult{}, err
	}
	var hits []Person
	for _, x := range cs {
		cd := cards[x.ref]
		var extra []string
		if full {
			extra = []string{x.unit, x.title, cd.Email}
		}
		if domain.Match(f.Q, cd.FullName, cd.Phone, extra...) {
			hits = append(hits, person(x, cd, c.set))
		}
	}
	slices.SortStableFunc(hits, func(a, b Person) int { return cmp.Compare(domain.Fold(a.FullName), domain.Fold(b.FullName)) })
	res.Total = len(hits)
	res.Items = hits[min(f.Offset, len(hits)):min(f.Offset+f.Limit, len(hits))]
	return res, nil
}

// HistoryEntry là một phiên bản của liên hệ ngoài tỉnh (TÊN trường đã đổi, không giá trị — BR-05).
type HistoryEntry struct {
	Version       int64
	Change        string
	ChangedFields []string
	At            time.Time
	Actor         uuid.UUID
}

// Detail là chi tiết một liên hệ (chỉ bộ SetFull).
type Detail struct {
	Person
	Organization string
	CanEdit      bool
	History      []HistoryEntry
}

// GetPerson trả chi tiết. Bộ SetBasic → ErrDetailForbidden (BR-03: không xem chi tiết).
func (s *Service) GetPerson(ctx context.Context, id uuid.UUID) (Detail, error) {
	c, err := s.who(ctx)
	if err != nil {
		return Detail{}, err
	}
	if c.set != domain.SetFull {
		return Detail{}, port.ErrDetailForbidden
	}
	all, _, err := s.candidates(ctx, c, true)
	if err != nil {
		return Detail{}, err
	}
	i := slices.IndexFunc(all, func(x cand) bool { return x.ref == id })
	if i < 0 {
		return Detail{}, port.ErrNotFound
	}
	x := all[i]
	cards, err := s.cards(ctx, []uuid.UUID{id}, c.set)
	if err != nil {
		return Detail{}, err
	}
	d := Detail{Person: person(x, cards[id], c.set)}
	if x.source == SourceExternal {
		d.Organization, d.CanEdit = x.unit, c.manage
		err = s.tx(ctx, c.tenant, func(tx pgx.Tx) error {
			h, err := store.PersonHistory(ctx, tx, id)
			for _, v := range h {
				d.History = append(d.History, HistoryEntry{Version: v.Seq, Change: v.Change, ChangedFields: v.ChangedFields,
					At: v.RecordedAt, Actor: v.Actor})
			}
			return err
		})
	}
	return d, err
}
