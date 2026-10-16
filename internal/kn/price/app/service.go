// Package app là phần cài đặt của pf/price (xem tài liệu gói gốc internal/kn/price).
package app

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/price/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/price/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/price/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/db"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// DBModule: giao dịch riêng của module chạy SET LOCAL ROLE r_price (ADR-0028). Module mới không có vai đăng nhập cũ:
// pool là vai đăng nhập của đơn vị (app_platform), là thành viên r_price.
var DBModule = db.Module("pf/price")

// Service cài đặt port.Catalog và các thao tác HTTP của khung.
type Service struct {
	DB       tenant.Beginner // đã bọc db.ForModule(…, DBModule)
	Clock    clock.Clock
	profiles map[string]port.Profile
	order    []string
	// Reminders: người nhận nhắc nhập giá (W11.74). Module.New nối PointReminders (W11.72 — cán bộ Xã phụ trách từng xã
	// báo giá); nil → thông báo chủ đề tới cán bộ xã của tenant.
	Reminders port.ReminderTargets
	// Points: điểm nối của xã báo giá + phân công (W11.72): danh bạ cán bộ, "xã đã gửi phiếu" (W11.75).
	Points PointHooks
	// LockSummary (NOTIF): số liệu phiếu của ngày tại mốc khóa cho thông báo tổng hợp Cấp Tỉnh (FN-TI-02.03 B4). Module.New
	// nối với phiếu W11.75; nil → mốc khóa không phát pf.price.lock.summarized.
	LockSummary func(ctx context.Context, tx pgx.Tx, day time.Time) (port.DaySummary, error)
}

var _ port.Catalog = (*Service)(nil)

var errNoPool = errors.New("price: chưa nối pool cho giao dịch của module")

// NewService dựng Service; cấu hình bên dùng trùng tên, thiếu tên hoặc vai lỗi.
func NewService(b tenant.Beginner, clk clock.Clock, profiles []port.Profile) (*Service, error) {
	if clk == nil {
		clk = clock.System{}
	}
	s := &Service{DB: b, Clock: clk, profiles: map[string]port.Profile{}}
	for _, p := range profiles {
		if p.Name == "" || len(p.Roles) == 0 {
			return nil, fmt.Errorf("price: cấu hình %q thiếu tên hoặc vai", p.Name)
		}
		if _, dup := s.profiles[p.Name]; dup {
			return nil, fmt.Errorf("price: cấu hình %q trùng", p.Name)
		}
		s.profiles[p.Name] = p
		s.order = append(s.order, p.Name)
	}
	return s, nil
}

// Profiles trả cấu hình theo thứ tự khai.
func (s *Service) Profiles() []port.Profile {
	out := make([]port.Profile, 0, len(s.order))
	for _, n := range s.order {
		out = append(out, s.profiles[n])
	}
	return out
}

// caller là người gọi đối với một profile.
type caller struct {
	ref, tenant uuid.UUID
	level       string // TW | TINH | XA; rỗng nếu chỉ có vai quản trị
	manage      bool
	// steward: Quản trị nghiệp vụ báo giá (W11.72) — vai quản trị (ManageBy) hoặc Cấp Tỉnh có cờ nhiệm vụ
	// Profile.StewardDuty trên lần gán (KNContext.HasDuty — W11.07; ngữ cảnh chỉ xem không mang cờ).
	steward bool
}

// resolve tìm profile (rỗng → profile đầu tiên) và vai của người gọi trong đó: vai ngữ cảnh KN nếu có, không thì vai realm.
func (s *Service) resolve(ctx context.Context, name string) (port.Profile, caller, error) {
	p, ok := authz.FromContext(ctx)
	if !ok {
		return port.Profile{}, caller{}, problem.Unauthenticated
	}
	if name == "" && len(s.order) > 0 {
		name = s.order[0]
	}
	prof, ok := s.profiles[name]
	if !ok {
		return port.Profile{}, caller{}, port.ErrProfileNotFound
	}
	c := caller{ref: p.ActorRef, tenant: p.TenantID}
	roles := p.Roles
	if p.KN != nil {
		roles = []string{p.KN.Role}
	}
	for _, r := range roles {
		if l, ok := prof.Roles[r]; ok && c.level == "" {
			c.level = l
		}
		if slices.Contains(prof.ManageBy, r) {
			c.manage = true
		}
	}
	c.steward = c.manage || (c.level == "TINH" && prof.StewardDuty != "" && p.KN != nil && p.KN.HasDuty(prof.StewardDuty))
	if c.level == "" && !c.manage {
		return prof, c, problem.Forbidden.WithDetail("vai của người gọi không thuộc cấu hình giá " + name)
	}
	return prof, c, nil
}

func (s *Service) tx(ctx context.Context, tid uuid.UUID, fn func(pgx.Tx) error) error {
	if s.DB == nil {
		return errNoPool
	}
	return db.WithModuleTenantTx(ctx, s.DB, DBModule, tid, fn)
}

// Status là tình trạng khung của tenant người gọi (route sức khỏe của module).
type Status struct {
	Profile     port.Profile
	Level       string
	CanManage   bool
	CanSteward  bool // W11.72: Quản trị nghiệp vụ báo giá (cấu hình lịch, ngưỡng, mặt hàng)
	RuleVersion string
	Day         time.Time
	Units       int
	Groups      int
}

// Status đọc số mục danh mục đang hiệu lực hôm nay (giờ VN) — chứng minh schema, vai r_price và RLS hoạt động.
func (s *Service) Status(ctx context.Context, profile string) (Status, error) {
	prof, c, err := s.resolve(ctx, profile)
	if err != nil {
		return Status{}, err
	}
	st := Status{Profile: prof, Level: c.level, CanManage: c.manage, CanSteward: c.steward, RuleVersion: domain.RuleVersion, Day: domain.BusinessDay(s.Clock.Now())}
	err = s.tx(ctx, c.tenant, func(tx pgx.Tx) error {
		var e error
		if st.Units, e = store.Count(ctx, tx, domain.KindUnit, st.Day); e != nil {
			return e
		}
		st.Groups, e = store.Count(ctx, tx, domain.KindGroup, st.Day)
		return e
	})
	return st, err
}

// CatalogView là danh mục nền đang hiệu lực tại một ngày.
type CatalogView struct {
	Day     time.Time
	Units   []port.Unit
	Groups  []port.Group
	Regions []port.Region // W11.72 nhóm vùng
}

// CatalogAt đọc danh mục nền tại ngày at (zero → hôm nay, giờ VN) cho người gọi thuộc profile.
func (s *Service) CatalogAt(ctx context.Context, profile string, at time.Time) (CatalogView, error) {
	_, c, err := s.resolve(ctx, profile)
	if err != nil {
		return CatalogView{}, err
	}
	day := domain.BusinessDay(s.Clock.Now())
	if !at.IsZero() {
		day = time.Date(at.Year(), at.Month(), at.Day(), 0, 0, 0, 0, time.UTC)
	}
	v := CatalogView{Day: day}
	err = s.tx(ctx, c.tenant, func(tx pgx.Tx) error {
		var e error
		if v.Units, e = units(ctx, tx, day); e != nil {
			return e
		}
		if v.Groups, e = groups(ctx, tx, day); e != nil {
			return e
		}
		v.Regions, e = regions(ctx, tx, day)
		return e
	})
	return v, err
}

// Units (port.Catalog) đọc đơn vị tính tại ngày at cho tenant của ctx (không kiểm vai — bên gọi là module đã kiểm).
func (s *Service) Units(ctx context.Context, at time.Time) ([]port.Unit, error) {
	var out []port.Unit
	err := s.ctxTx(ctx, func(tx pgx.Tx) error {
		var e error
		out, e = units(ctx, tx, domain.BusinessDay(at))
		return e
	})
	return out, err
}

// Groups (port.Catalog) đọc nhóm hàng tại ngày at cho tenant của ctx.
func (s *Service) Groups(ctx context.Context, at time.Time) ([]port.Group, error) {
	var out []port.Group
	err := s.ctxTx(ctx, func(tx pgx.Tx) error {
		var e error
		out, e = groups(ctx, tx, domain.BusinessDay(at))
		return e
	})
	return out, err
}

func (s *Service) ctxTx(ctx context.Context, fn func(pgx.Tx) error) error {
	if s.DB == nil {
		return errNoPool
	}
	return db.WithContextTx(ctx, s.DB, DBModule, fn)
}

func units(ctx context.Context, tx pgx.Tx, day time.Time) ([]port.Unit, error) {
	rs, err := store.Effective(ctx, tx, domain.KindUnit, day)
	if err != nil {
		return nil, err
	}
	out := make([]port.Unit, 0, len(rs))
	for _, r := range rs {
		out = append(out, port.Unit{Code: r.Code, Name: r.Name, SortOrder: r.SortOrder, Version: r.Version, ValidFrom: r.ValidFrom})
	}
	return out, nil
}

func groups(ctx context.Context, tx pgx.Tx, day time.Time) ([]port.Group, error) {
	rs, err := store.Effective(ctx, tx, domain.KindGroup, day)
	if err != nil {
		return nil, err
	}
	out := make([]port.Group, 0, len(rs))
	for _, r := range rs {
		out = append(out, port.Group{Code: r.Code, Name: r.Name, SortOrder: r.SortOrder, Version: r.Version, ValidFrom: r.ValidFrom})
	}
	return out, nil
}

// InstallDefaults ghi danh mục mặc định (5 nhóm hàng BRD, đơn vị tính thường gặp) có hiệu lực từ hôm nay cho tenant
// người gọi — chỉ mã CHƯA có (mã đã có, kể cả đã ngưng, giữ nguyên). Vai ManageBy. Trả số phiên bản đã ghi.
func (s *Service) InstallDefaults(ctx context.Context, profile, idem string) (int, error) {
	_, c, err := s.resolve(ctx, profile)
	if err != nil {
		return 0, err
	}
	if !c.manage {
		return 0, problem.Forbidden.WithDetail("chỉ vai quản trị danh mục giá ghi được danh mục")
	}
	now := s.Clock.Now()
	day := domain.BusinessDay(now)
	entries := append(append(domain.DefaultUnits(day), domain.DefaultGroups(day)...), domain.DefaultRegions(day)...) // W11.72 nhóm vùng
	written := 0
	err = s.tx(ctx, c.tenant, func(tx pgx.Tx) error {
		have := map[domain.Kind]map[string]bool{}
		for _, k := range []domain.Kind{domain.KindUnit, domain.KindGroup, domain.KindRegion} {
			codes, e := store.Codes(ctx, tx, k)
			if e != nil {
				return e
			}
			have[k] = codes
		}
		for _, e := range entries {
			if have[e.Kind][e.Code] {
				continue
			}
			if err := domain.Validate(e); err != nil {
				return problem.Validation.WithDetail(err.Error())
			}
			key := "price:default:" + string(e.Kind) + ":" + e.Code
			if idem != "" {
				key = idem + ":" + string(e.Kind) + ":" + e.Code
			}
			if _, err := store.Insert(ctx, tx, ids.New(), c.tenant, e, now, c.ref, key); err != nil {
				return classify(err)
			}
			written++
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return written, nil
}

// classify đổi lỗi DB của pf_price sang mã lỗi chung.
func classify(err error) error {
	var pg *pgconn.PgError
	if errors.As(err, &pg) && pg.Code == "23505" {
		return problem.Conflict.WithDetail("danh mục đang được ghi đồng thời — thử lại").WithCause(err)
	}
	return err
}
