package app

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/store"
	mdport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata/port"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/port/extensionsrc"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ledger"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/params"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// Lỗi của báo cáo Đề án (FR-03).
var (
	ErrProgramNotFound = problem.New(http.StatusNotFound, "extension.program_report_not_found", "Không tìm thấy báo cáo tiến độ Đề án")
	ErrProgramApproved = problem.New(http.StatusConflict, "extension.program_report_approved", "Phiên bản này đã được duyệt")
)

// Vai duyệt tiến độ Đề án: Admin tỉnh (TTKN QG duyệt thay khi cần).
var programApprovers = []string{port.RoleKNProvinceAdmin, port.RoleKNNationalAdmin}

// ProgramInput là dữ liệu vào của báo cáo tiến độ Đề án.
type ProgramInput struct {
	ID        *uuid.UUID
	Report    domain.ProgramReport
	EventTime *time.Time
	DeviceID  *string
	IdemKey   string
}

// ProgramResult là kết quả ghi.
type ProgramResult struct {
	Report   domain.ProgramReport
	Replayed bool
}

// SubmitProgramReport ghi tiến độ Đề án của một HTX × vụ (FR-03). Gửi lại cùng
// HTX × vụ tạo phiên bản mới và phải được Admin tỉnh duyệt lại.
func (s *Service) SubmitProgramReport(ctx context.Context, in ProgramInput) (ProgramResult, error) {
	p, err := authz.Require(ctx, fieldWriters...)
	if err != nil {
		return ProgramResult{}, err
	}
	if in.IdemKey == "" {
		return ProgramResult{}, problem.IdempotencyRequired
	}
	r := in.Report
	if err := r.Validate(); err != nil {
		return ProgramResult{}, fieldProblem(err)
	}
	sc, err := s.scope(ctx, p)
	if err != nil {
		return ProgramResult{}, err
	}
	if !sc.Allows(r.CommuneCode) {
		return ProgramResult{}, ErrOutOfScope
	}
	province, level, err := s.provinceOf(ctx, r.CommuneCode, "commune_code")
	if err != nil {
		return ProgramResult{}, err
	}
	if level != partyport.LevelCommune {
		return ProgramResult{}, ErrUnitInvalid.WithField("commune_code", "báo cáo gắn với một xã")
	}
	r.ProvinceCode = province
	if err := s.checkCooperative(ctx, r.CooperativeRef, r.CommuneCode); err != nil {
		return ProgramResult{}, err
	}
	if err := s.checkCatalog(ctx, mdport.CatalogSeason, r.SeasonCode, "season_code"); err != nil {
		return ProgramResult{}, err
	}
	now := s.now()
	r.ID = ids.New()
	if in.ID != nil {
		r.ID = *in.ID
	}
	r.TenantID, r.ActorRef, r.DeviceID, r.IdemKey, r.EventTime = p.TenantID, p.ActorRef, in.DeviceID, in.IdemKey, now
	if in.EventTime != nil && !in.EventTime.IsZero() && !in.EventTime.After(now) {
		r.EventTime = in.EventTime.UTC()
	}
	r.VersionKey = r.ProgramVersionKey()

	var res ProgramResult
	err = tenant.WithTenantTx(ctx, s.DB, p.TenantID, func(tx pgx.Tx) error {
		if prev, err := store.ProgramByIdemKey(ctx, tx, in.IdemKey); err != nil {
			return err
		} else if prev != nil {
			res = ProgramResult{Report: *prev, Replayed: true}
			return nil
		}
		if in.ID != nil {
			if prev, err := store.ProgramByID(ctx, tx, r.ID); err != nil {
				return err
			} else if prev != nil {
				return ErrIDReuse
			}
		}
		cur, err := store.ProgramCurrentByKey(ctx, tx, r.VersionKey)
		if err != nil {
			return err
		}
		r.Version = 1
		if cur != nil {
			r.Version, r.PreviousID = cur.Version+1, cur.ReportID
		}
		if err := store.InsertProgram(ctx, tx, &r); err != nil {
			return err
		}
		if err := store.UpsertProgramCurrent(ctx, tx, r); err != nil {
			return err
		}
		res = ProgramResult{Report: r}
		return nil
	})
	err = ledger.Classify(err)
	if errors.Is(err, ledger.ErrDuplicate) {
		var prev *domain.ProgramReport
		rerr := tenant.WithTenantTx(ctx, s.DB, p.TenantID, func(tx pgx.Tx) error {
			var e error
			prev, e = store.ProgramByIdemKey(ctx, tx, in.IdemKey)
			return e
		})
		if rerr == nil && prev != nil {
			return ProgramResult{Report: *prev, Replayed: true}, nil
		}
	}
	return res, err
}

// ApproveProgramReport: Admin tỉnh duyệt phiên bản hiện hành (FR-03). Người duyệt
// không được là người gửi (R-56); phiên bản đã bị thay → 409.
func (s *Service) ApproveProgramReport(ctx context.Context, id uuid.UUID, note string) (domain.ProgramReport, error) {
	p, err := authz.Require(ctx, programApprovers...)
	if err != nil {
		return domain.ProgramReport{}, err
	}
	note = strings.TrimSpace(note)
	if domain.ContainsPII(note) || len([]rune(note)) > 500 {
		return domain.ProgramReport{}, problem.Validation.WithField("note", "tối đa 500 ký tự, không chứa SĐT/CCCD")
	}
	sc, err := s.scope(ctx, p)
	if err != nil {
		return domain.ProgramReport{}, err
	}
	var out domain.ProgramReport
	err = tenant.WithTenantTx(ctx, s.DB, p.TenantID, func(tx pgx.Tx) error {
		r, err := store.ProgramByID(ctx, tx, id)
		if err != nil {
			return err
		}
		if r == nil || !visible(sc, r.CommuneCode, r.ProvinceCode) {
			return ErrProgramNotFound
		}
		if !sc.Allows(r.ProvinceCode) {
			return ErrOutOfScope.WithDetail("chỉ Admin tỉnh phụ trách được duyệt")
		}
		cur, err := store.ProgramCurrentByKey(ctx, tx, r.VersionKey)
		if err != nil {
			return err
		}
		if cur == nil || cur.ReportID != r.ID {
			return ErrReportSuperseded
		}
		if cur.SubmittedBy == p.ActorRef {
			return problem.SelfApproval
		}
		ok, err := store.ApproveProgram(ctx, tx, p.TenantID, *r, p.ActorRef, s.now(), note)
		if err != nil {
			return err
		}
		if !ok {
			return ErrProgramApproved
		}
		out = *r
		return nil
	})
	return out, err
}

// ProgramProgress trả tiến độ Đề án trong phạm vi người gọi (FR-03, FR-10 lớp bản đồ).
func (s *Service) ProgramProgress(ctx context.Context, unit, season string, year int) (extensionsrc.ProgramData, error) {
	p, err := authz.Require(ctx, knReaders...)
	if err != nil {
		return extensionsrc.ProgramData{}, err
	}
	sc, err := s.scope(ctx, p)
	if err != nil {
		return extensionsrc.ProgramData{}, err
	}
	if sc.Empty() {
		return extensionsrc.ProgramData{}, ErrOutOfScope.WithDetail("tài khoản chưa gắn đơn vị công tác")
	}
	unit = strings.TrimSpace(unit)
	if unit != "" && !sc.Allows(unit) {
		return extensionsrc.ProgramData{}, ErrOutOfScope
	}
	return ReportData{Svc: s}.ProgramProgress(ctx, extensionsrc.ReportQuery{
		TenantID: p.TenantID, Scope: sc, Unit: unit, SeasonCode: strings.TrimSpace(season), SeasonYear: year, At: s.now(),
	})
}

// programTargets đọc mục tiêu Đề án của từng tỉnh tại at (chưa đặt → nil).
func (s *Service) programTargets(ctx context.Context, provinces []string, at time.Time) (map[string]*decimal.Decimal, error) {
	out := map[string]*decimal.Decimal{}
	for _, pc := range provinces {
		if _, ok := out[pc]; ok {
			continue
		}
		if s.Params == nil {
			out[pc] = nil
			continue
		}
		v, err := s.Params.At(ctx, port.ProgramTargetKey(pc), at)
		if errors.Is(err, params.ErrParamNotFound) {
			out[pc] = nil
			continue
		}
		if err != nil {
			return nil, err
		}
		d := v.Value
		out[pc] = &d
	}
	return out, nil
}
