// Package app là phần cài đặt của pf/forms (xem tài liệu gói gốc internal/platform/forms): định nghĩa biểu có phiên
// bản, phát hành kỳ, nộp bài, rà soát bốn mắt (cap/workflow), chốt kỳ kèm ảnh chụp đóng băng, mở lại kỳ; lối ghi
// song song (port.Freezer) cho bên dùng có vòng đời kỳ riêng (KN extension/reportcycle).
package app

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/port"
	wfport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/workflow/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/db"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// DBModule: giao dịch riêng của module chạy SET LOCAL ROLE r_forms (ADR-0028). Module mới không có vai đăng nhập
// cũ <module>_app: pool là vai đăng nhập của đơn vị (app_platform), là thành viên r_forms.
var DBModule = db.Module("pf/forms")

// WorkflowKind là kind của yêu cầu duyệt bài nộp ở cap/workflow (subject = mã phiên bản bài nộp).
const WorkflowKind = "forms.submission"

// Service cài đặt use case của pf/forms và port.Freezer.
type Service struct {
	DB        tenant.Beginner // đã bọc db.ForModule(…, DBModule); nil → chỉ dùng được Freezer
	Clock     clock.Clock
	Approvals wfport.Approvals // cap/workflow: rà soát bốn mắt (nil → chỉ CHECK review_four_eyes ở DB)
	profiles  map[string]port.Profile
	order     []string
}

var _ port.Freezer = (*Service)(nil)

// errNoPool: module dựng không có pool (chỉ Freezer trong giao dịch bên gọi).
var errNoPool = errors.New("forms: chưa nối pool cho giao dịch riêng của module")

// NewService kiểm và nạp cấu hình bên dùng; cấu hình sai → lỗi lúc khởi động (không phải lúc chạy request).
func NewService(b tenant.Beginner, clk clock.Clock, approvals wfport.Approvals, profiles []port.Profile) (*Service, error) {
	if clk == nil {
		clk = clock.System{}
	}
	s := &Service{DB: b, Clock: clk, Approvals: approvals, profiles: map[string]port.Profile{}}
	for _, p := range profiles {
		if err := domain.ValidateProfile(p); err != nil {
			return nil, err
		}
		if _, dup := s.profiles[p.Name]; dup {
			return nil, fmt.Errorf("profile %s khai hai lần", p.Name)
		}
		s.profiles[p.Name] = p
		s.order = append(s.order, p.Name)
	}
	return s, nil
}

// Profiles trả cấu hình bên dùng theo thứ tự khai.
func (s *Service) Profiles() []port.Profile {
	out := make([]port.Profile, 0, len(s.order))
	for _, n := range s.order {
		out = append(out, s.profiles[n])
	}
	return out
}

// actor là người gọi đối với MỘT profile.
type actor struct {
	ref, tenant uuid.UUID
	role, level string
	dept        string // phòng ban của ngữ cảnh KN (rỗng: vai realm, Admin)
	leaf        bool   // cấp của người gọi là cấp lá (cấp nộp bài)
	scope       authz.Scope
	node        uuid.UUID
	define      bool
}

// resolve tìm profile và cấp của người gọi: ngữ cảnh phiên KN (vai, phạm vi, Nút) nếu có, không thì vai realm
// (chỉ cấp cao nhất có phạm vi toàn quốc; cấp khác không có phạm vi đơn vị → chỉ thao tác không cần phạm vi).
func (s *Service) resolve(ctx context.Context, name string) (port.Profile, actor, error) {
	p, ok := authz.FromContext(ctx)
	if !ok {
		return port.Profile{}, actor{}, problem.Unauthenticated
	}
	prof, ok := s.profiles[name]
	if !ok {
		return port.Profile{}, actor{}, port.ErrProfileNotFound
	}
	a := actor{ref: p.ActorRef, tenant: p.TenantID, node: p.OrgRef}
	if p.KN != nil {
		a.role, a.scope, a.node, a.dept = p.KN.Role, p.KN.Scope, p.KN.OrgRef, p.KN.Department
		a.level = prof.LevelOf(a.role, a.dept)
		a.define = slices.Contains(prof.DefineBy, a.role)
		if len(prof.Departments) > 0 && a.role != authz.KNAdmin && !slices.Contains(prof.Departments, a.dept) {
			return prof, a, problem.Forbidden.WithDetail("cấu hình biểu " + name + " không dành cho phòng ban của phiên")
		}
	} else {
		for _, r := range p.Roles {
			if l, ok := prof.Roles[r]; ok && a.level == "" {
				a.role, a.level = r, l
			}
			if slices.Contains(prof.DefineBy, r) {
				a.define = true
			}
		}
		if a.level != "" && a.level == prof.Hierarchy[0] {
			a.scope = authz.Scope{All: true}
		}
	}
	if a.level == "" && !a.define {
		return prof, a, problem.Forbidden.WithDetail("vai của người gọi không thuộc cấu hình biểu " + name)
	}
	a.leaf = a.level != "" && a.level == prof.Leaf()
	return prof, a, nil
}

// allowsUnit: người gọi được thao tác/thấy đơn vị u (thuộc danh sách units của kỳ có đặc tả spec) — W11.05:
//   - xã: phạm vi phiên chứa mã xã; cơ sở/hộ: phạm vi phiên chứa mã xã CHA của đơn vị;
//   - phòng ban: cấp lá chỉ phòng ban của phiên; cấp trên (phát hành, rà soát, chốt) mọi phòng ban của tenant.
func (a actor) allowsUnit(spec domain.Spec, units []string, u string) bool {
	if spec.UnitKind == port.UnitDepartment {
		return a.scope.All || (a.leaf && a.dept == u) || (!a.leaf && a.level != "")
	}
	return a.scope.Allows(spec.Parent(units, u))
}

func (a actor) need(level, what string) error {
	if a.level != level {
		return problem.Forbidden.WithDetail(what + " cần cấp " + level)
	}
	return nil
}

func (s *Service) tx(ctx context.Context, tid uuid.UUID, fn func(pgx.Tx) error) error {
	if s.DB == nil {
		return errNoPool
	}
	return db.WithModuleTenantTx(ctx, s.DB, DBModule, tid, fn)
}

func idemOr(key, suffix string) string {
	if key == "" {
		return "forms:" + suffix + ":" + ids.New().String()
	}
	return key + ":" + suffix
}

// classify đổi lỗi DB của pf_forms sang mã lỗi của module.
func classify(err error) error {
	var pg *pgconn.PgError
	if !errors.As(err, &pg) {
		return err
	}
	switch {
	case pg.Code == "AGF01":
		return problem.Conflict.WithDetail("trạng thái kỳ đã đổi").WithCause(err)
	case pg.Code == "AGF02":
		return port.ErrPeriodClosed.WithCause(err)
	case pg.Code == "AGF03":
		return port.ErrSubmissionLimit.WithCause(err)
	case pg.Code == "23514" && pg.ConstraintName == "review_four_eyes":
		return problem.SelfApproval.WithCause(err)
	case pg.Code == "23505":
		return problem.Conflict.WithCause(err)
	case pg.Code == "23503":
		return port.ErrTemplateNotFound.WithCause(err)
	}
	return err
}
