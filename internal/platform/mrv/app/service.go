// Package app: use case của pf/mrv. Giao dịch mở ở đây; kiểm vai, phạm vi và
// đồng ý theo mục đích trước khi ghi.
package app

import (
	"context"
	"log/slog"
	"net/http"
	"slices"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/meter"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/objstore"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/params"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// Producer là tên nhà phát sự kiện.
const Producer = "pf/mrv"

// Mã lỗi của module (docs/errors.md).
var (
	ErrConsentMissing        = problem.New(http.StatusUnprocessableEntity, "mrv.consent_missing", "Nông hộ chưa đồng ý cho mục đích mrv.evidence")
	ErrOutOfScope            = problem.New(http.StatusForbidden, "mrv.out_of_scope", "Ngoài địa bàn phụ trách")
	ErrCatalogUnavailable    = problem.New(http.StatusUnprocessableEntity, "mrv.catalog_version_unavailable", "Không có phiên bản danh mục ĐÃ DUYỆT cho loại minh chứng này")
	ErrCatalogNotFound       = problem.New(http.StatusNotFound, "mrv.catalog_version_not_found", "Không tìm thấy phiên bản danh mục")
	ErrCatalogTransition     = problem.New(http.StatusConflict, "mrv.catalog_invalid_transition", "Trạng thái phiên bản danh mục không cho phép thao tác này")
	ErrCatalogNotSetter      = problem.New(http.StatusForbidden, "mrv.catalog_not_setter", "Chỉ người đặt được gửi duyệt phiên bản của mình")
	ErrEvidenceNotFound      = problem.New(http.StatusNotFound, "mrv.evidence_not_found", "Không tìm thấy minh chứng")
	ErrEvidenceIDReused      = problem.New(http.StatusConflict, "mrv.evidence_id_reused", "id đã dùng cho bản ghi khác")
	ErrEvidenceSuperseded    = problem.New(http.StatusConflict, "mrv.evidence_superseded", "Minh chứng đã có bản ghi bù mới hơn — thao tác trên bản hiện hành")
	ErrReviewExists          = problem.New(http.StatusConflict, "mrv.review_exists", "Phiên bản minh chứng này đã có quyết định duyệt")
	ErrReviewerIsCapturer    = problem.New(http.StatusForbidden, "mrv.reviewer_is_capturer", "Người duyệt không được là người thu minh chứng")
	ErrExportNotFound        = problem.New(http.StatusNotFound, "mrv.export_not_found", "Không tìm thấy yêu cầu xuất")
	ErrExportNotReady        = problem.New(http.StatusConflict, "mrv.export_not_ready", "Gói chưa sẵn sàng")
	ErrDependencyUnavailable = problem.New(http.StatusServiceUnavailable, "mrv.dependency_unavailable", "Chưa nối phạm vi, đồng ý hoặc kho đối tượng")
)

// Service là use case của module.
type Service struct {
	DB      tenant.Beginner // pool vai mrv_app; nil khi chỉ dùng làm handler của worker
	Clock   clock.Clock
	Scope   partyport.ScopeResolver
	Consent partyport.ConsentChecker
	Store   objstore.Store
	// Params: tham số masterdata đã duyệt (hệ số CO2, ngưỡng FR-08, cửa sổ đối chiếu).
	// nil → mặc định trong mã, ước tính không bao giờ chính thức.
	Params params.Params
	// ValveGrants (ADR-0020): tenant bên cấp nhận van mrv_collection_evidence của tenant phong bì
	// (pf/party). Thiếu → consumer van từ chối (DLQ, party.valve_share_grant_missing).
	ValveGrants partyport.ValveGrants
	// Egress (G5, ADR-0024): điểm kiểm soát xuất (pf/metering) — kênh mrv_package ở yêu cầu xuất và
	// cấp URL tải gói; kênh valve ở consumer van mrv_collection_evidence. Nil → hành vi cũ.
	Egress     meter.Checkpoint
	EgressMode meter.Mode
	Log        *slog.Logger
}

func (s *Service) tx(ctx context.Context, tid uuid.UUID, fn func(pgx.Tx) error) error {
	if s.DB == nil {
		return ErrDependencyUnavailable
	}
	return tenant.WithTenantTx(ctx, s.DB, tid, fn)
}

// scope tính phạm vi của người gọi (ADR-0009).
func (s *Service) scope(ctx context.Context) (authz.Scope, error) {
	if s.Scope == nil {
		return authz.Scope{}, ErrDependencyUnavailable
	}
	return s.Scope.Scope(ctx)
}

func validation(errs []problem.FieldError) error {
	if len(errs) == 0 {
		return nil
	}
	p := problem.Validation
	for _, e := range errs {
		p = p.WithField(e.Field, e.Reason)
	}
	return p
}

func clampLimit(n int) int {
	if n <= 0 || n > 200 {
		return 50
	}
	return n
}

// Page là một trang kết quả phân trang cursor (R-53).
type Page[T any] struct {
	Items []T
	Next  *uuid.UUID
}

func page[T any](items []T, limit int, id func(T) uuid.UUID) Page[T] {
	var pg Page[T]
	if len(items) > limit {
		items = items[:limit]
		last := id(items[limit-1])
		pg.Next = &last
	}
	pg.Items = items
	return pg
}

var evidenceReaders = []string{
	RoleCollector, RoleReviewer, RoleViewer, RoleExporter,
}

func containsID(xs []uuid.UUID, x uuid.UUID) bool { return slices.Contains(xs, x) }
