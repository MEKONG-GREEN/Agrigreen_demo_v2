// Package app: use case của mkg/measurement. Giao dịch mở ở đây bằng
// tenant.WithTenantTx; kiểm vai ở đây (R-55), kể cả chặn tự phê duyệt (R-56).
package app

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	hubport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/hub/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/measurement/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/measurement/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ledger"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// Mã lỗi của module (docs/errors.md, mục mkg/measurement).
var (
	ErrLotNotFound       = problem.New(404, "measurement.lot_not_found", "Lô chưa có phiếu cân")
	ErrTicketNotFound    = problem.New(404, "measurement.ticket_not_found", "Không tìm thấy phiếu cân")
	ErrDeviceNotFound    = problem.New(404, "measurement.device_not_found", "Không tìm thấy thiết bị cân / kỳ đăng ký")
	ErrSessionNotFound   = problem.New(404, "measurement.session_not_found", "Không tìm thấy phiên cân")
	ErrVersionNotFound   = problem.New(404, "measurement.version_not_found", "Không tìm thấy phiên bản tham số")
	ErrDisputeNotFound   = problem.New(404, "measurement.dispute_not_found", "Không tìm thấy hồ sơ tranh chấp")
	ErrImportNotFound    = problem.New(404, "measurement.import_not_found", "Không tìm thấy lần nhập số cân của khách")
	ErrInvalidTicket     = problem.New(422, "measurement.invalid_ticket", "Phiếu cân không hợp lệ")
	ErrWindowIncomplete  = problem.New(422, "measurement.window_incomplete", "Cửa sổ số đo thiếu bộ đếm")
	ErrDeviceTaken       = problem.New(409, "measurement.device_taken", "Mã thiết bị đã đăng ký ở tài khoản khác")
	ErrPendingExists     = problem.New(409, "measurement.pending_exists", "Đang có đề xuất chờ duyệt")
	ErrNotPending        = problem.New(409, "measurement.not_pending", "Đề xuất không còn chờ duyệt")
	ErrDeviceNotActive   = problem.New(409, "measurement.device_not_active", "Thiết bị / kỳ đăng ký không hiệu lực")
	ErrSessionConflict   = problem.New(409, "measurement.session_conflict", "Phiên cân xung đột (thiết bị đang có phiên mở hoặc cửa sổ bộ đếm chồng nhau)")
	ErrSessionClosed     = problem.New(409, "measurement.session_closed", "Phiên cân đã đóng")
	ErrIncomplete        = problem.New(409, "measurement.reconciliation_incomplete", "Lô chưa đủ phiếu cân để đối soát")
	ErrDisputeTransition = problem.New(409, "measurement.dispute_transition", "Thao tác không hợp lệ ở trạng thái hiện tại của hồ sơ")
	ErrNoHubIn           = problem.New(409, "measurement.no_hub_in", "Lô chưa có phiếu HUB_IN để lập bằng chứng MRV")
	ErrNoCustomerIn      = problem.New(409, "measurement.no_customer_in", "Lô DIRECT chưa có phiếu CUSTOMER_IN để lập bằng chứng MRV")
	ErrNoPlotSources     = problem.New(409, "measurement.no_plot_sources", "Lô không có nguồn thửa để lập bằng chứng MRV")
	ErrValveDenied       = problem.New(500, "measurement.valve_denied", "Van công bố từ chối payload")
)

// NoRolls là RollTotals mặc định khi chưa nối mkg/fieldops: luôn ErrNotFound
// (đối soát ghi roll_check = pending_rolls).
type NoRolls struct{}

// RollTotal cài đặt port.RollTotals.
func (NoRolls) RollTotal(context.Context, uuid.UUID, uuid.UUID) (port.RollTotal, error) {
	return port.RollTotal{}, port.ErrNotFound
}

// Service là use case của module.
type Service struct {
	DB      tenant.Beginner // pool vai measurement_app (cụm mkg)
	Clock   clock.Clock
	Writer  *Writer
	Rolls   port.RollTotals
	Origins hubport.LotOrigins   // nguồn gốc lô hub (thửa + tỷ lệ) cho van MRV, tenant tường minh
	Plots   port.CollectionPlots // tỷ lệ thửa theo lệnh gom (fieldops) — lô DIRECT / lô không có ở hub
}

// Vai đọc.
var readRoles = []string{port.RoleScaleSupervisor, port.RoleDeviceApprover, port.RoleReconciliationOfficer, port.RoleDisputeOfficer,
	port.RoleToleranceApprover}

// mapErr quy lỗi domain / port / PostgreSQL về problem.
func mapErr(err error) error {
	if err == nil {
		return nil
	}
	var p *problem.Problem
	if errors.As(err, &p) {
		return err
	}
	var fe domain.FieldError
	switch {
	case errors.As(err, &fe):
		return problem.Validation.WithField(fe.Field, fe.Msg)
	case errors.Is(err, port.ErrWindowIncomplete):
		return ErrWindowIncomplete.WithDetail(err.Error())
	case errors.Is(err, port.ErrInvalidTicket):
		return ErrInvalidTicket.WithDetail(err.Error())
	case errors.Is(err, port.ErrUnknownDevice), errors.Is(err, port.ErrDeviceRevoked), errors.Is(err, port.ErrEpochSuperseded):
		return ErrDeviceNotActive.WithDetail(err.Error())
	case errors.Is(err, domain.ErrIncomplete):
		return ErrIncomplete.WithDetail(err.Error())
	case errors.Is(err, domain.ErrDisputeTransition):
		return ErrDisputeTransition.WithDetail(err.Error())
	}
	var pg *pgconn.PgError
	if errors.As(err, &pg) && pg.Code == "23505" {
		switch pg.ConstraintName {
		case "device_current_one_pending":
			return ErrPendingExists
		case "weighing_session_current_one_open":
			return ErrSessionConflict
		}
	}
	return err
}

// write chạy fn trong giao dịch tenant; replay trả true nếu đã có bản ghi cùng
// khóa. Trùng khóa do ghi song song → chạy replay trong giao dịch mới.
func (s *Service) write(ctx context.Context, tid uuid.UUID, fn func(pgx.Tx) error, replay func(pgx.Tx) (bool, error)) (replayed bool, err error) {
	err = tenant.WithTenantTx(ctx, s.DB, tid, func(tx pgx.Tx) error {
		ok, err := replay(tx)
		if err != nil || ok {
			replayed = ok
			return err
		}
		return fn(tx)
	})
	if errors.Is(err, ledger.ErrDuplicate) {
		rerr := tenant.WithTenantTx(ctx, s.DB, tid, func(tx pgx.Tx) error {
			ok, e := replay(tx)
			if e == nil && !ok {
				return err
			}
			return e
		})
		if rerr == nil {
			return true, nil
		}
	}
	return replayed, mapErr(err)
}

func (s *Service) read(ctx context.Context, fn func(pgx.Tx) error) error {
	p, err := authz.Require(ctx, readRoles...)
	if err != nil {
		return err
	}
	return mapErr(tenant.WithTenantTx(ctx, s.DB, p.TenantID, fn))
}

func requireIdem(key string) error {
	if key == "" {
		return problem.IdempotencyRequired
	}
	return nil
}

func insert(err error) error {
	if err != nil {
		return ledger.Classify(err)
	}
	return nil
}

func reasonPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
