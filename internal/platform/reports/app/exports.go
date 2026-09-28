package app

import (
	"context"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	mdport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/outbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/telemetry"
)

func ts(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }

// ExportInput là yêu cầu xuất một mẫu báo cáo.
type ExportInput struct {
	TemplateKey string
	Format      string
	Params      domain.Params
	IdemKey     string
}

// normalizeParams kiểm và chuẩn hóa bộ lọc theo nguồn của mẫu (FN-10 BR-01: tệp
// khớp đúng bộ lọc). now dùng cho kỳ mặc định (tháng hiện tại).
func (s *Service) normalizeParams(ctx context.Context, def domain.Definition, in domain.Params, sc authz.Scope, now time.Time) (domain.Params, error) {
	src := domain.Sources[def.Source]
	p := domain.Params{Level: strings.TrimSpace(in.Level), UnitCode: strings.TrimSpace(in.UnitCode), MachineType: strings.TrimSpace(in.MachineType)}
	if p.Level == "" {
		p.Level = def.Grouping
	}
	if !domain.LevelAllowed(def.Source, p.Level) {
		return p, problem.Validation.WithField("level", "mẫu này hỗ trợ cấp "+strings.Join(src.Levels, ", "))
	}
	if p.UnitCode != "" {
		if !sc.Allows(p.UnitCode) {
			return p, ErrOutOfScope.WithDetail("đơn vị " + p.UnitCode + " ngoài phạm vi")
		}
		u, err := s.unitLevel(ctx, p.UnitCode)
		if err != nil {
			return p, err
		}
		if u.Level != mdport.LevelProvince && u.Level != mdport.LevelCommune {
			return p, ErrUnitInvalid.WithField("unit_code", "chỉ nhận tỉnh hoặc xã hiện hành")
		}
	}
	if src.NeedsPeriod {
		p.Period = strings.TrimSpace(in.Period)
		if p.Period == "" {
			p.Period = domain.MonthOf(now)
		}
		if _, err := domain.ParsePeriod(p.Period); err != nil {
			return p, problem.Validation.WithField("period", domain.ErrPeriod.Error())
		}
	}
	if src.UsesSeason {
		p.SeasonCode, p.SeasonYear = strings.TrimSpace(in.SeasonCode), in.SeasonYear
		if p.SeasonCode != "" && (p.SeasonYear < 2000 || p.SeasonYear > 2100) && src.Module == "mechanization" {
			return p, problem.Validation.WithField("season_year", "bắt buộc khi chọn vụ")
		}
		if p.SeasonYear != 0 && (p.SeasonYear < 2000 || p.SeasonYear > 2100) {
			return p, problem.Validation.WithField("season_year", "năm vụ không hợp lệ")
		}
	}
	if src.Module == "mechanization" && !src.NeedsCompare && in.RunID != "" {
		if _, err := uuid.Parse(in.RunID); err != nil {
			return p, problem.Validation.WithField("run_id", "phải là UUID")
		}
		p.RunID = in.RunID
	}
	if src.NeedsCompare {
		p.SeasonCodeB, p.SeasonYearB = strings.TrimSpace(in.SeasonCodeB), in.SeasonYearB
		switch {
		case p.SeasonCode == "" || p.SeasonYear == 0:
			return p, problem.Validation.WithField("season_code", "chọn Vụ A (kỳ trước)")
		case p.SeasonCodeB == "" || p.SeasonYearB < 2000 || p.SeasonYearB > 2100:
			return p, problem.Validation.WithField("season_code_b", "chọn Vụ B (kỳ này)")
		case p.SeasonCode == p.SeasonCodeB && p.SeasonYear == p.SeasonYearB:
			return p, problem.Validation.WithField("season_code_b", "Vui lòng chọn hai vụ khác nhau để đối chiếu")
		}
	}
	return p, nil
}

// RequestExport tạo việc xuất (mục 12, R-52): kiểm vai theo nguồn, giải phạm vi,
// qua điểm kiểm soát DLP, ghi job + outbox pf.reports.export.requested trong MỘT
// giao dịch; worker dựng tệp. Trả 202 ở handler.
func (s *Service) RequestExport(ctx context.Context, in ExportInput) (domain.Job, bool, error) {
	p, err := authz.Require(ctx, allReaders...)
	if err != nil {
		return domain.Job{}, false, err
	}
	if in.IdemKey == "" {
		return domain.Job{}, false, problem.IdempotencyRequired
	}
	sc, err := s.scope(ctx, p)
	if err != nil {
		return domain.Job{}, false, err
	}
	if sc.Empty() {
		return domain.Job{}, false, ErrOutOfScope.WithDetail("tài khoản chưa gắn đơn vị công tác")
	}
	now := s.now()
	var job domain.Job
	replayed := false
	err = s.tx(ctx, p.TenantID, func(tx pgx.Tx) error {
		if prev, err := store.JobByIdemKey(ctx, tx, in.IdemKey); err != nil {
			return err
		} else if prev != nil {
			job, replayed = *prev, true
			return nil
		}
		t, err := resolveTemplate(ctx, tx, strings.TrimSpace(in.TemplateKey))
		if err != nil {
			return err
		}
		if t.Status != domain.TemplateActive {
			return ErrTemplateRetired
		}
		if !hasAny(p, rolesFor(t.Def.Source)) {
			return problem.Forbidden.WithDetail("vai của bạn không được xuất mẫu này")
		}
		if !slices.Contains(t.Def.Formats, in.Format) {
			return ErrFormat.WithField("format", "mẫu hỗ trợ "+strings.Join(t.Def.Formats, ", "))
		}
		params, err := s.normalizeParams(ctx, t.Def, in.Params, sc, now)
		if err != nil {
			return err
		}
		job = domain.Job{
			ID: ids.New(), TenantID: p.TenantID, TemplateKey: t.Key, TemplateVersion: t.Version, Def: t.Def, Format: in.Format,
			Params: params, ScopeAll: sc.All, ScopeUnits: sc.Units, RequestedBy: p.ActorRef, RequestedAt: now,
			Trigger: domain.TriggerAPI, IdemKey: in.IdemKey, Status: domain.JobQueued,
		}
		if err := s.egress(ctx, tx, Egress{Kind: EgressExport, TenantID: p.TenantID, ActorRef: p.ActorRef, TemplateKey: t.Key,
			Format: in.Format, JobID: job.ID}); err != nil {
			return err
		}
		return s.enqueueJob(ctx, tx, &job)
	})
	var pg *pgconn.PgError
	if errors.As(err, &pg) && pg.Code == "23505" {
		var prev *domain.Job
		if rerr := s.tx(ctx, p.TenantID, func(tx pgx.Tx) error {
			var e error
			prev, e = store.JobByIdemKey(ctx, tx, in.IdemKey)
			return e
		}); rerr == nil && prev != nil {
			return *prev, true, nil
		}
	}
	return job, replayed, err
}

// enqueueJob ghi job và sự kiện yêu cầu trong giao dịch đang mở.
func (s *Service) enqueueJob(ctx context.Context, tx pgx.Tx, j *domain.Job) error {
	if err := store.InsertJob(ctx, tx, j); err != nil {
		return err
	}
	return outbox.Enqueue(ctx, tx, outbox.Event{
		ID: ids.New(), TenantID: j.TenantID, Type: port.EventExportRequested, SchemaVersion: 1, OccurredAt: j.RequestedAt,
		TraceID: telemetry.TraceID(ctx), Producer: port.Producer,
		Payload: port.ExportRequestedV1{
			JobID: j.ID.String(), TemplateKey: j.TemplateKey, Format: j.Format, RequestedBy: j.RequestedBy.String(), RequestedAt: ts(j.RequestedAt),
		},
	})
}

// jobVisible: người yêu cầu luôn thấy việc của mình; người khác cần vai của nguồn
// và phạm vi bao trọn phạm vi của việc xuất.
func jobVisible(p authz.Principal, sc authz.Scope, j domain.Job) bool {
	if j.RequestedBy == p.ActorRef {
		return true
	}
	if !hasAny(p, rolesFor(j.Def.Source)) {
		return false
	}
	if sc.All {
		return true
	}
	if j.ScopeAll {
		return false
	}
	for _, u := range j.ScopeUnits {
		if !sc.Allows(u) {
			return false
		}
	}
	return len(j.ScopeUnits) > 0
}

// GetExport đọc trạng thái việc xuất (ngoài phạm vi → 404, không lộ sự tồn tại).
func (s *Service) GetExport(ctx context.Context, id uuid.UUID) (domain.Job, error) {
	p, err := authz.Require(ctx, allReaders...)
	if err != nil {
		return domain.Job{}, err
	}
	sc, err := s.scope(ctx, p)
	if err != nil {
		return domain.Job{}, err
	}
	var j *domain.Job
	if err := s.tx(ctx, p.TenantID, func(tx pgx.Tx) error {
		var e error
		j, e = store.JobByID(ctx, tx, id, false)
		return e
	}); err != nil {
		return domain.Job{}, err
	}
	if j == nil || !jobVisible(p, sc, *j) {
		return domain.Job{}, ErrExportNotFound
	}
	return *j, nil
}

// DownloadLink là URL tải tệp có hạn.
type DownloadLink struct {
	Job       domain.Job
	URL       string
	ExpiresAt time.Time
	FileName  string
}

// Download cấp URL tải ngắn hạn — cũng đi qua điểm kiểm soát DLP (ghi nhật ký).
// Không ghi URL có chữ ký vào log (R-92).
func (s *Service) Download(ctx context.Context, id uuid.UUID) (DownloadLink, error) {
	p, err := authz.Require(ctx, allReaders...)
	if err != nil {
		return DownloadLink{}, err
	}
	j, err := s.GetExport(ctx, id)
	if err != nil {
		return DownloadLink{}, err
	}
	if j.Status != domain.JobCompleted || j.ObjectKey == nil {
		return DownloadLink{}, ErrExportNotReady.WithDetail("trạng thái: " + string(j.Status))
	}
	if s.Store == nil {
		return DownloadLink{}, ErrDependency.WithDetail("chưa cấu hình kho đối tượng")
	}
	if err := s.tx(ctx, p.TenantID, func(tx pgx.Tx) error {
		return s.egress(ctx, tx, Egress{Kind: EgressDownload, TenantID: p.TenantID, ActorRef: p.ActorRef, TemplateKey: j.TemplateKey,
			Format: j.Format, JobID: j.ID})
	}); err != nil {
		return DownloadLink{}, err
	}
	u, err := s.Store.PresignGet(ctx, *j.ObjectKey, DownloadTTL)
	if err != nil {
		return DownloadLink{}, err
	}
	return DownloadLink{Job: j, URL: u, ExpiresAt: s.now().Add(DownloadTTL), FileName: domain.FileName(j)}, nil
}
