// Package app: use case của pf/cropcycle sau W3.3 — chỉ còn van thu gom MKG ↔ HTX (collection_*, pf 000040):
// consumer van công bố, lịch gom / bảng kê / phản hồi của HTX, luồng phản hồi cho MKG qua bề mặt đối tác, sản phẩm
// dữ liệu collection_volume (G5) và đồng bộ ngoại tuyến của van. Vụ, nhật ký, SOP, sản lượng chuyển sang
// pf/cooperative (schema pf_coop, docs/design/cooperative.md). Giao dịch mở ở đây (tenant.WithTenantTx); vai kiểm
// bằng authz.Require:
//   - platform_admin: toàn tenant;
//   - coop_manager: HTX của mình (Principal.OrgRef);
//   - cán bộ KN (kn_officer, kn_province_admin, kn_national_admin): CHỈ ĐỌC lịch gom theo phạm vi xã (ADR-0009).
package app

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	coopport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cooperative/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cropcycle/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cropcycle/port"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/meter"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/outbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/telemetry"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// Mã lỗi chung của pf/cropcycle (docs/errors.md). Mã của vụ/nhật ký/SOP (cropcycle.*) giữ nguyên tên nhưng do
// pf/cooperative phát từ W3.3 (hợp đồng HTTP không đổi).
var (
	ErrIDReused   = problem.New(http.StatusConflict, "cropcycle.id_reused", "id đã dùng cho bản ghi khác")
	ErrDependency = problem.New(http.StatusServiceUnavailable, "cropcycle.dependency_unavailable", "Dịch vụ phụ thuộc chưa sẵn sàng")
)

// Vai cán bộ khuyến nông (chỉ đọc theo phạm vi xã).
var knRoles = []string{partyport.RoleKNOfficer, partyport.RoleKNProvinceAdmin, partyport.RoleKNNationalAdmin}

// PlotFacts là phần của cooperativeport.Plots mà sản phẩm collection_volume cần (bên tiêu thụ khai — gốc lắp ráp nối
// pf/cooperative — gốc module không kéo kho định danh, RB-3).
type PlotFacts interface {
	Facts(ctx context.Context, refs []uuid.UUID) (map[uuid.UUID]coopport.Plot, error)
}

// SeasonFacts là phần của cooperativeport.Seasons mà sản phẩm collection_volume và nguồn ô thô của feed (W5.3) cần:
// mã vụ / năm theo season_ref và tiến độ vụ chưa hủy (W3.3: vụ ở pf_coop, không đọc thẳng season_current).
type SeasonFacts interface {
	Codes(ctx context.Context, refs []uuid.UUID) (map[uuid.UUID]coopport.SeasonKey, error)
	Progress(ctx context.Context, asOf time.Time) ([]coopport.SeasonProgress, error)
}

// Service là use case của pf/cropcycle (van thu gom).
type Service struct {
	DB    tenant.Beginner // pool, giao dịch chạy dưới r_cropcycle (db.ForModule)
	Clock clock.Clock
	Scope partyport.ScopeResolver // phạm vi cán bộ KN; thiếu → vai KN nhận 503
	// Plots: mã thửa của bảng kê (best effort) — pf/cooperative.
	Plots coopport.Plots
	// Sharing (ADR-0020): cấp quyền chia sẻ + sổ đọc của bề mặt đối tác (pf/party); thiếu → 403.
	Sharing partyport.PartnerAccess
	// ValveGrants (ADR-0020): tenant bên cấp nhận van collection_* của tenant phong bì; thiếu → DLQ.
	ValveGrants partyport.ValveGrants
	// PlotFacts + Seasons + Units (G5): sản phẩm collection_volume — xã/nông hộ/diện tích thửa và mã vụ của bảng
	// kê (pf/cooperative Plots.Facts, Seasons.Codes — W3.3: vụ ở pf_coop, không JOIN chéo schema), xã → tỉnh
	// (pf/masterdata). Thiếu → nguồn trả 503 cropcycle.dependency_unavailable.
	PlotFacts PlotFacts
	Seasons   SeasonFacts
	Units     partyport.AdminUnits
	// Egress (G5, ADR-0024): điểm kiểm soát xuất (pf/metering) — consumer van collection gọi Guard
	// kênh valve sau ValveTargets, mỗi bên cấp một lần. Nil → hành vi cũ.
	Egress     meter.Checkpoint
	EgressMode meter.Mode
	Log        *slog.Logger
}

func (s *Service) now() time.Time { return s.Clock.Now().UTC() }

func (s *Service) inTx(ctx context.Context, tid uuid.UUID, fn func(pgx.Tx) error) error {
	return tenant.WithTenantTx(ctx, s.DB, tid, fn)
}

// ---------------------------------------------------------------------------
// Quyền và phạm vi
// ---------------------------------------------------------------------------

type accessKind int

const (
	accAll accessKind = iota + 1
	accCoop
	accKN
)

type access struct {
	p    authz.Principal
	kind accessKind
	sc   authz.Scope
}

// access tính quyền của người gọi có một trong roles.
func (s *Service) access(ctx context.Context, roles ...string) (access, error) {
	p, err := authz.Require(ctx, roles...)
	if err != nil {
		return access{}, err
	}
	has := func(r string) bool { return p.Has(r) && slices.Contains(roles, r) }
	switch {
	case has(authz.RolePlatformAdmin):
		return access{p: p, kind: accAll, sc: authz.Scope{All: true}}, nil
	case has(port.RoleCoopManager):
		return access{p: p, kind: accCoop}, nil
	}
	for _, r := range knRoles {
		if has(r) {
			if s.Scope == nil {
				return access{}, ErrDependency.WithDetail("chưa nối bộ tính phạm vi (pf/party)")
			}
			sc, err := s.Scope.Scope(ctx)
			if err != nil {
				return access{}, err
			}
			return access{p: p, kind: accKN, sc: sc}, nil
		}
	}
	return access{}, problem.Forbidden
}

func enqueue(ctx context.Context, tx pgx.Tx, tid uuid.UUID, typ string, at time.Time, payload any) error {
	return outbox.Enqueue(ctx, tx, outbox.Event{
		ID: ids.New(), TenantID: tid, Type: typ, SchemaVersion: 1, OccurredAt: at,
		TraceID: telemetry.TraceID(ctx), Producer: "pf/cropcycle", Payload: payload,
	})
}

// Page là một trang kết quả có cursor.
type Page[T any] struct {
	Items []T
	Next  *uuid.UUID
}

func clampLimit(n int) int {
	if n <= 0 || n > 200 {
		return 50
	}
	return n
}

// sees cho biết người gọi đọc được bản ghi thuộc xã/HTX này.
func (a access) sees(commune string, coop uuid.UUID) bool {
	switch a.kind {
	case accAll:
		return true
	case accCoop:
		return a.p.OrgRef != uuid.Nil && coop == a.p.OrgRef
	case accKN:
		return a.sc.Allows(commune)
	}
	return false
}

// fieldProblem quy lỗi domain về problem.
func fieldProblem(err error) error {
	if fe, ok := domain.AsFieldError(err); ok {
		return problem.Validation.WithField(fe.Field, fe.Reason)
	}
	return err
}

// isUnique cho biết err là vi phạm ràng buộc duy nhất constraint.
func isUnique(err error, constraint string) bool {
	var pg *pgconn.PgError
	return errors.As(err, &pg) && pg.Code == "23505" && (constraint == "" || pg.ConstraintName == constraint)
}

func strPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
