package app

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"slices"
	"strings"
	"time"

	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/outbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/telemetry"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func ts(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }

// codeRe: mã danh mục (vụ, giống) trong bộ lọc.
var codeRe = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,40}$`)

// reach là phạm vi đơn vị của người gọi. Quản lý HTX thuần (coopOnly, không vai CGH)
// không có phạm vi đơn vị — chỉ HTX của mình — nên không cần pf/party.
func (s *Service) reach(ctx context.Context, p authz.Principal) (authz.Scope, error) {
	if s.coopOnly(p) && !hasAny(p, cghRoles) {
		return authz.Scope{}, nil
	}
	return s.scope(ctx, p)
}

// jobScope giải phạm vi của việc xuất theo nguồn của mẫu. Nguồn HTX với quản lý HTX
// (coopOnly): phạm vi là HTX của người gọi, ghi vào bộ lọc cooperative_ref (lọc HTX
// khác → 403), phạm vi đơn vị để trống (domain.Params.CoopScoped). Còn lại: phạm vi
// đơn vị, rỗng → 403.
func (s *Service) jobScope(p authz.Principal, sc authz.Scope, def domain.Definition, params *domain.Params) (authz.Scope, error) {
	if s.sourceSpec(def.Source).Module == "cropcycle" && s.coopOnly(p) {
		if p.OrgRef == uuid.Nil {
			return sc, ErrOutOfScope.WithDetail("tài khoản chưa gắn HTX")
		}
		if c := strings.TrimSpace(params.CooperativeRef); c != "" && !strings.EqualFold(c, p.OrgRef.String()) {
			return sc, ErrOutOfScope.WithDetail("quản lý HTX chỉ xuất số liệu HTX của mình")
		}
		params.CooperativeRef = p.OrgRef.String()
		return authz.Scope{}, nil
	}
	if sc.Empty() {
		return sc, ErrOutOfScope.WithDetail("tài khoản chưa gắn đơn vị công tác")
	}
	return sc, nil
}

// ExportInput là yêu cầu xuất một mẫu báo cáo.
type ExportInput struct {
	TemplateKey string
	Format      string
	Params      domain.Params
	IdemKey     string
	SourceArgs  json.RawMessage
}

// normalizeParams kiểm và chuẩn hóa bộ lọc theo nguồn của mẫu (FN-10 BR-01: tệp
// khớp đúng bộ lọc). now dùng cho kỳ mặc định (tháng hiện tại).
func (s *Service) normalizeParams(ctx context.Context, def domain.Definition, in domain.Params, sc authz.Scope, now time.Time) (domain.Params, error) {
	src := s.sourceSpec(def.Source)
	p := domain.Params{Level: strings.TrimSpace(in.Level), UnitCode: strings.TrimSpace(in.UnitCode), MachineType: strings.TrimSpace(in.MachineType)}
	if p.Level == "" {
		p.Level = def.Grouping
	}
	if !domain.LevelAllowedWithSources(def.Source, p.Level, s.sourceSpecs()) {
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
		if u.Level != partyport.LevelProvince && u.Level != partyport.LevelCommune {
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
	if src.Module == "cropcycle" {
		// HTX US-DASH-02 AC-1: cùng bộ lọc với bảng điều hành (vụ, năm, HTX, giống).
		p.VarietyCode = strings.TrimSpace(in.VarietyCode)
		if p.VarietyCode != "" && !codeRe.MatchString(p.VarietyCode) {
			return p, problem.Validation.WithField("variety_code", "mã giống không hợp lệ")
		}
		if p.SeasonCode != "" && !codeRe.MatchString(p.SeasonCode) {
			return p, problem.Validation.WithField("season_code", "mã vụ không hợp lệ")
		}
		if c := strings.TrimSpace(in.CooperativeRef); c != "" {
			id, err := uuid.Parse(c)
			if err != nil {
				return p, problem.Validation.WithField("cooperative_ref", "phải là UUID")
			}
			p.CooperativeRef = id.String()
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
	p, err := authz.Require(ctx, s.readerRoles()...)
	if err != nil {
		return domain.Job{}, false, err
	}
	if in.IdemKey == "" {
		return domain.Job{}, false, problem.IdempotencyRequired
	}
	var job domain.Job
	replayed := false
	err = s.tx(ctx, p.TenantID, func(tx pgx.Tx) error {
		if prev, err := store.JobByIdemKey(ctx, tx, in.IdemKey); err != nil {
			return err
		} else if prev != nil {
			if err := s.checkSourceReplay(ctx, p, in, *prev); err != nil {
				return err
			}
			job, replayed = *prev, true
			return nil
		}
		t, err := s.resolveTemplate(ctx, tx, strings.TrimSpace(in.TemplateKey))
		if err != nil {
			return err
		}
		if t.Status != domain.TemplateActive {
			return ErrTemplateRetired
		}
		if !hasAny(p, s.sourceRoles(t.Def.Source)) {
			return problem.Forbidden.WithDetail("vai của bạn không được xuất mẫu này")
		}
		if !slices.Contains(t.Def.Formats, in.Format) {
			return ErrFormat.WithField("format", "mẫu hỗ trợ "+strings.Join(t.Def.Formats, ", "))
		}
		job, err = s.sourceRequestJob(ctx, p, t, in, nil)
		if err != nil {
			return err
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
			if e := s.checkSourceReplay(ctx, p, in, *prev); e != nil {
				return domain.Job{}, false, e
			}
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
func (s *Service) jobVisible(p authz.Principal, sc authz.Scope, j domain.Job) bool {
	if j.RequestedBy == p.ActorRef {
		return true
	}
	if !hasAny(p, s.sourceRoles(j.Def.Source)) {
		return false
	}
	if s.sourceSpec(j.Def.Source).Module == "cropcycle" && s.coopOnly(p) {
		// HTX US-DASH-02: quản lý HTX chỉ thấy việc xuất trong phạm vi HTX của mình.
		return p.OrgRef != uuid.Nil && j.Params.CoopScoped(j.ScopeAll, j.ScopeUnits) && j.Params.CooperativeRef == p.OrgRef.String()
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
	p, err := authz.Require(ctx, s.readerRoles()...)
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
	if j == nil {
		return domain.Job{}, ErrExportNotFound
	}
	if enabled, _ := s.Builders.jobPolicy(j.Def.Source); j.SourceArgs != nil || enabled.Version > 0 {
		if err := s.readSourceJob(ctx, p, *j, port.SourceJobDetail); err != nil {
			return domain.Job{}, err
		}
	} else {
		sc, err := s.reach(ctx, p)
		if err != nil {
			return domain.Job{}, err
		}
		if !s.jobVisible(p, sc, *j) {
			return domain.Job{}, ErrExportNotFound
		}
	}
	return *j, nil
}

// ExportPage là một trang lịch sử xuất của người gọi.
type ExportPage struct {
	Items []domain.Job
	Next  *uuid.UUID // id việc cuối trang; nil = hết
}

// MyExports liệt kê việc xuất do CHÍNH người gọi yêu cầu (kể cả việc của lịch gửi
// họ sở hữu), mới nhất trước, phân trang cursor (R-53). limit 1–200, mặc định 50.
func (s *Service) MyExports(ctx context.Context, before uuid.UUID, limit int) (ExportPage, error) {
	p, err := authz.Require(ctx, s.readerRoles()...)
	if err != nil {
		return ExportPage{}, err
	}
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	var jobs []domain.Job
	if err := s.tx(ctx, p.TenantID, func(tx pgx.Tx) error {
		var e error
		jobs, e = store.JobsOf(ctx, tx, p.ActorRef, before, limit+1)
		return e
	}); err != nil {
		return ExportPage{}, err
	}
	for _, job := range jobs {
		if err := s.readSourceJob(ctx, p, job, port.SourceJobList); err != nil {
			return ExportPage{}, err
		}
	}
	var pg ExportPage
	if len(jobs) > limit {
		jobs = jobs[:limit]
		last := jobs[limit-1].ID
		pg.Next = &last
	}
	pg.Items = jobs
	return pg, nil
}

// Downloadable cho biết việc xuất đã có tệp để tải (GET .../download).
func Downloadable(j domain.Job) bool { return j.Status == domain.JobCompleted && j.ObjectKey != nil }

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
	p, err := authz.Require(ctx, s.readerRoles()...)
	if err != nil {
		return DownloadLink{}, err
	}
	j, err := s.GetExport(ctx, id)
	if err != nil {
		return DownloadLink{}, err
	}
	if err := s.readSourceJob(ctx, p, j, port.SourceJobDownload); err != nil {
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
