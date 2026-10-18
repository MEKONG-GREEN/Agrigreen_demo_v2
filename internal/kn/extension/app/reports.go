package app

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/store"
	mdport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata/port"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ledger"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// ReportInput là dữ liệu vào của báo cáo hiện trường (tạo mới hoặc bản ghi bù).
type ReportInput struct {
	ID               *uuid.UUID // UUIDv7 sinh trên máy; nil → server sinh
	Report           domain.Report
	SupersedesID     *uuid.UUID // bản ghi bù: phiên bản bị thay
	CorrectionReason *string
	EventTime        *time.Time
	DeviceID         *string
	IdemKey          string
}

// ReportResult là kết quả ghi báo cáo.
type ReportResult struct {
	Report   domain.Report
	Replayed bool
}

// SubmitReport ghi báo cáo hiện trường (FR-01, FR-02).
//
//   - Kỳ còn mở: gửi lại cùng khóa phiên bản (vd cùng xã × HTX × vụ × giống) tạo
//     phiên bản mới, không cần lý do.
//   - Kỳ đã chốt ở tỉnh: không nhận báo cáo mới; chỉ bản ghi bù (SupersedesID +
//     lý do ≥ 10 ký tự) — "số liệu đã chốt kỳ không sửa, chỉ tạo phiên bản mới kèm
//     lý do" (US-DASH-02 AC-2).
//   - Bản ghi bù luôn cần lý do (sync-contract: op=correct) và phải thay phiên bản hiện hành.
func (s *Service) SubmitReport(ctx context.Context, in ReportInput) (ReportResult, error) {
	p, err := authz.Require(ctx, fieldWriters...)
	if err != nil {
		return ReportResult{}, err
	}
	if in.IdemKey == "" {
		return ReportResult{}, problem.IdempotencyRequired
	}
	correcting := in.SupersedesID != nil
	in.CorrectionReason = trimPtr(in.CorrectionReason)
	if in.CorrectionReason != nil && *in.CorrectionReason == "" {
		in.CorrectionReason = nil
	}
	if correcting && !domain.ValidReason(in.CorrectionReason) {
		return ReportResult{}, ErrReasonRequired.WithField("correction_reason", "bắt buộc, ≥ 10 ký tự")
	}
	if !correcting && in.CorrectionReason != nil {
		return ReportResult{}, problem.Validation.WithField("correction_reason", "chỉ dùng với bản ghi bù (supersedes_id)")
	}
	now := s.now()
	r := in.Report
	if correcting {
		// Bản ghi bù giữ nguyên loại, xã, HTX, kỳ, vụ, giống của bản bị thay (cùng khóa phiên bản).
		prev, err := s.reportByID(ctx, p.TenantID, *in.SupersedesID)
		if err != nil {
			return ReportResult{}, err
		}
		if prev == nil {
			return ReportResult{}, ErrReportNotFound.WithField("supersedes_id", "không có báo cáo này")
		}
		r.Kind, r.CommuneCode, r.CooperativeRef, r.Period = prev.Kind, prev.CommuneCode, prev.CooperativeRef, prev.Period
		if prev.Kind == domain.KindSowing {
			r.SeasonCode, r.VarietyCode = prev.SeasonCode, prev.VarietyCode
		}
	}
	if err := r.Validate(now, clock.Zone()); err != nil {
		return ReportResult{}, fieldProblem(err)
	}
	sc, err := s.scope(ctx, p)
	if err != nil {
		return ReportResult{}, err
	}
	if !sc.Allows(r.CommuneCode) {
		return ReportResult{}, ErrOutOfScope
	}
	province, level, err := s.provinceOf(ctx, r.CommuneCode, "commune_code")
	if err != nil {
		return ReportResult{}, err
	}
	if level != partyport.LevelCommune {
		return ReportResult{}, ErrUnitInvalid.WithField("commune_code", "báo cáo gắn với một xã")
	}
	r.ProvinceCode = province
	if err := s.checkCooperative(ctx, r.CooperativeRef, r.CommuneCode); err != nil {
		return ReportResult{}, err
	}
	switch r.Kind {
	case domain.KindSowing:
		if err := s.checkCatalog(ctx, mdport.CatalogRiceVariety, r.VarietyCode, "variety_code"); err != nil {
			return ReportResult{}, err
		}
		if err := s.checkCatalog(ctx, mdport.CatalogSeason, r.SeasonCode, "season_code"); err != nil {
			return ReportResult{}, err
		}
	case domain.KindGrowthPest:
		if err := s.checkCatalog(ctx, mdport.CatalogPest, r.PestCode, "pest_code"); err != nil {
			return ReportResult{}, err
		}
	}
	r.ID = ids.New()
	if in.ID != nil {
		r.ID = *in.ID
	}
	r.TenantID, r.ActorRef, r.DeviceID, r.IdemKey = p.TenantID, p.ActorRef, in.DeviceID, in.IdemKey
	r.EventTime = now
	if in.EventTime != nil && !in.EventTime.IsZero() && !in.EventTime.After(now) {
		r.EventTime = in.EventTime.UTC()
	}
	if correcting {
		r.SupersedesID, r.CorrectionReason = in.SupersedesID, in.CorrectionReason
	}

	var res ReportResult
	err = tenant.WithTenantTx(ctx, s.DB, p.TenantID, func(tx pgx.Tx) error {
		if prev, err := store.ReportByIdemKey(ctx, tx, in.IdemKey); err != nil {
			return err
		} else if prev != nil {
			res = ReportResult{Report: *prev, Replayed: true}
			return nil
		}
		if in.ID != nil {
			if prev, err := store.ReportByID(ctx, tx, r.ID); err != nil {
				return err
			} else if prev != nil {
				return ErrIDReuse
			}
		}
		closed, err := store.PeriodClosed(ctx, tx, r.Period, province)
		if err != nil {
			return err
		}
		if correcting {
			prev, err := store.ReportByID(ctx, tx, *in.SupersedesID)
			if err != nil {
				return err
			}
			cur, err := store.CurrentByKey(ctx, tx, prev.VersionKey)
			if err != nil {
				return err
			}
			if cur == nil || cur.ReportID != prev.ID {
				return ErrReportSuperseded
			}
			r.VersionKey, r.Version, r.PreviousID = prev.VersionKey, cur.Version+1, prev.ID
		} else {
			if closed {
				return ErrPeriodClosed.With("period", r.Period).With("province_code", province)
			}
			r.VersionKey = r.ComputeVersionKey(r.ID)
			cur, err := store.CurrentByKey(ctx, tx, r.VersionKey)
			if err != nil {
				return err
			}
			r.Version = 1
			if cur != nil {
				r.Version, r.PreviousID = cur.Version+1, cur.ReportID
			}
		}
		if err := store.InsertReport(ctx, tx, &r); err != nil {
			return err
		}
		if err := store.UpsertCurrent(ctx, tx, r); err != nil {
			return err
		}
		res = ReportResult{Report: r}
		return nil
	})
	err = ledger.Classify(err)
	if errors.Is(err, ledger.ErrDuplicate) {
		var prev *domain.Report
		rerr := tenant.WithTenantTx(ctx, s.DB, p.TenantID, func(tx pgx.Tx) error {
			var e error
			prev, e = store.ReportByIdemKey(ctx, tx, in.IdemKey)
			return e
		})
		if rerr == nil && prev != nil {
			return ReportResult{Report: *prev, Replayed: true}, nil
		}
	}
	return res, err
}

func (s *Service) reportByID(ctx context.Context, tid, id uuid.UUID) (*domain.Report, error) {
	var r *domain.Report
	err := tenant.WithTenantTx(ctx, s.DB, tid, func(tx pgx.Tx) error {
		var e error
		r, e = store.ReportByID(ctx, tx, id)
		return e
	})
	return r, err
}

// ReportDetail là báo cáo kèm mọi phiên bản của cùng khóa.
type ReportDetail struct {
	Report   domain.Report
	Versions []domain.Report
}

// GetReport đọc một phiên bản báo cáo và lịch sử phiên bản; ngoài phạm vi → 404.
func (s *Service) GetReport(ctx context.Context, id uuid.UUID) (ReportDetail, error) {
	p, err := authz.Require(ctx, knReaders...)
	if err != nil {
		return ReportDetail{}, err
	}
	sc, err := s.scope(ctx, p)
	if err != nil {
		return ReportDetail{}, err
	}
	var d ReportDetail
	var found bool
	err = tenant.WithTenantTx(ctx, s.DB, p.TenantID, func(tx pgx.Tx) error {
		r, err := store.ReportByID(ctx, tx, id)
		if err != nil || r == nil || !visible(sc, r.CommuneCode, r.ProvinceCode) {
			return err
		}
		found, d.Report = true, *r
		d.Versions, err = store.ReportVersions(ctx, tx, r.VersionKey)
		return err
	})
	if err != nil {
		return ReportDetail{}, err
	}
	if !found {
		return ReportDetail{}, ErrReportNotFound
	}
	return d, nil
}

// ReportQuery là tham số liệt kê báo cáo hiện hành.
type ReportQuery struct {
	Period string
	Kind   string
	After  uuid.UUID
	Limit  int
}

// ReportPage là một trang báo cáo hiện hành.
type ReportPage struct {
	Items []domain.Report
	Next  *uuid.UUID
}

// ListReports liệt kê phiên bản hiện hành trong phạm vi.
func (s *Service) ListReports(ctx context.Context, q ReportQuery) (ReportPage, error) {
	p, err := authz.Require(ctx, knReaders...)
	if err != nil {
		return ReportPage{}, err
	}
	if q.Limit <= 0 || q.Limit > 200 {
		q.Limit = 50
	}
	if q.Period != "" && !domain.ValidPeriod(q.Period) {
		return ReportPage{}, problem.Validation.WithField("period", "định dạng YYYY-MM")
	}
	if q.Kind != "" && q.Kind != domain.KindSowing && q.Kind != domain.KindGrowthPest {
		return ReportPage{}, problem.Validation.WithField("kind", "sowing_progress hoặc growth_pest")
	}
	sc, err := s.scope(ctx, p)
	if err != nil || sc.Empty() {
		return ReportPage{Items: []domain.Report{}}, err
	}
	var items []domain.Report
	err = tenant.WithTenantTx(ctx, s.DB, p.TenantID, func(tx pgx.Tx) error {
		var e error
		items, e = store.ListCurrentReports(ctx, tx, store.ReportFilter{
			All: sc.All, Units: sc.Units, Period: q.Period, Kind: q.Kind, After: q.After, Limit: q.Limit + 1,
		})
		return e
	})
	if err != nil {
		return ReportPage{}, err
	}
	pg := ReportPage{Items: items}
	if len(items) > q.Limit {
		pg.Items = items[:q.Limit]
		last := pg.Items[q.Limit-1].ID
		pg.Next = &last
	}
	if pg.Items == nil {
		pg.Items = []domain.Report{}
	}
	return pg, nil
}

// ClosePeriodInput: chốt kỳ báo cáo của một tỉnh (FR-05).
type ClosePeriodInput struct {
	Period       string
	ProvinceCode string
	Note         string
}

// ClosePeriod chốt kỳ (TTKN tỉnh cho tỉnh mình; TTKN QG chốt thay khi cần);
// chốt lặp → closed = false (giữ lần đầu).
func (s *Service) ClosePeriod(ctx context.Context, in ClosePeriodInput) (bool, error) {
	p, err := authz.Require(ctx, supervisors...)
	if err != nil {
		return false, err
	}
	in.ProvinceCode = strings.TrimSpace(in.ProvinceCode)
	if !domain.ValidPeriod(in.Period) {
		return false, problem.Validation.WithField("period", "định dạng YYYY-MM")
	}
	if in.Period > domain.PeriodOf(s.now(), clock.Zone()) {
		return false, problem.Validation.WithField("period", "không chốt kỳ tương lai")
	}
	if domain.ContainsPII(in.Note) {
		return false, problem.Validation.WithField("note", domain.ErrPIIInText.Error())
	}
	sc, err := s.scope(ctx, p)
	if err != nil {
		return false, err
	}
	if !sc.Allows(in.ProvinceCode) {
		return false, ErrOutOfScope
	}
	prov, level, err := s.provinceOf(ctx, in.ProvinceCode, "province_code")
	if err != nil {
		return false, err
	}
	if level != partyport.LevelProvince || prov != in.ProvinceCode {
		return false, ErrUnitInvalid.WithField("province_code", "phải là mã tỉnh")
	}
	var closed bool
	err = tenant.WithTenantTx(ctx, s.DB, p.TenantID, func(tx pgx.Tx) error {
		var e error
		closed, e = store.ClosePeriod(ctx, tx, p.TenantID, in.Period, in.ProvinceCode, p.ActorRef, s.now(), strings.TrimSpace(in.Note))
		return e
	})
	return closed, err
}
