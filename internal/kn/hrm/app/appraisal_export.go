package app

// Dòng xuất giữ tham chiếu phiên bản, không giữ tên/byte Excel trong sổ HRM.
// Các bước chuẩn bị SQL, đọc định danh và cap/export luôn tách khỏi giao dịch ghi.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/hrm/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/hrm/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/hrm/store"
	exportport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/export/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/vault/identity/client"
)

type AppraisalExportInput struct {
	Kind    string `json:"kind"`
	Format  string `json:"format"`
	Unit    string `json:"unit,omitempty"`
	Version int64  `json:"version"`
}
type AppraisalExport struct {
	ScoreLayout   *AppraisalScoreLayout `json:"score_layout,omitempty"`
	ID            uuid.UUID             `json:"id"`
	PeriodID      uuid.UUID             `json:"period_id"`
	PeriodVersion uuid.UUID             `json:"period_version"`
	Kind          string                `json:"kind"`
	Format        string                `json:"format"`
	Unit          string                `json:"unit"`
	EntryVersions []uuid.UUID           `json:"entry_versions"`
	FileName      string                `json:"file_name"`
	SHA256        string                `json:"sha256"`
	Size          int                   `json:"size"`
	Count         int                   `json:"count"`
	Status        string                `json:"status"`
	WordRun       *uuid.UUID            `json:"word_run,omitempty"`
	Actor         uuid.UUID             `json:"actor_ref"`
	At            time.Time             `json:"recorded_at"`
}
type AppraisalPaper struct {
	Period   AppraisalPeriod                 `json:"period"`
	Template domain.AppraisalSummaryTemplate `json:"template"`
	Items    []AppraisalItem                 `json:"items"`
}

func (s *Service) appraisalExportCaller(ctx context.Context, write bool) (appraisalActor, domain.Scope, error) {
	c, err := s.appraisalCaller(ctx)
	if err != nil {
		return c, domain.ScopeNone, err
	}
	if err = s.appraisalRecords(); err != nil {
		return c, domain.ScopeNone, err
	}
	if (write && c.readOnly) || (!c.center() && c.group != domain.GroupManager) {
		return c, domain.ScopeNone, problem.Forbidden.WithDetail("chỉ HCTH/Admin hoặc lãnh đạo đơn vị được xuất đề xuất")
	}
	scope := domain.ScopeNone
	err = s.tx(ctx, c.tenant, func(tx pgx.Tx) error {
		m, e := s.matrixOf(ctx, tx, c.group, c.ref)
		scope = m[domain.PermExport]
		return e
	})
	if err == nil && scope != domain.ScopeAll && scope != domain.ScopeUnit {
		err = problem.Forbidden.WithDetail("chưa có quyền xuất theo đơn vị/toàn Trung tâm")
	}
	if scope == domain.ScopeUnit && c.scope == domain.ScopeAll {
		c.scope = domain.ScopeUnit
	} // audit cùng giao quyền thực tế
	return c, scope, err
}
func appraisalPaperOrder(items []AppraisalItem) {
	slices.SortFunc(items, func(a, b AppraisalItem) int {
		if a.Member.ManagementUnit != b.Member.ManagementUnit {
			if a.Member.ManagementUnit < b.Member.ManagementUnit {
				return -1
			}
			return 1
		}
		if a.Member.PositionOrder != b.Member.PositionOrder {
			return a.Member.PositionOrder - b.Member.PositionOrder
		}
		if a.Member.Code < b.Member.Code {
			return -1
		}
		if a.Member.Code > b.Member.Code {
			return 1
		}
		return 0
	})
}
func appraisalExportRow(row store.AppraisalEvent) (AppraisalExport, error) {
	x, err := appraisalsReadObject[AppraisalExport](row)
	return x, err
}
func (s *Service) appraisalPaperTx(ctx context.Context, tx pgx.Tx, c appraisalActor, scope domain.Scope, period uuid.UUID, kind, unit string) (AppraisalPaper, []uuid.UUID, error) {
	p, err := loadAppraisalPeriod(ctx, tx, period)
	if err != nil {
		return AppraisalPaper{}, nil, err
	}
	if kind != "MS05" && kind != "MS06" && kind != AppraisalScores {
		return AppraisalPaper{}, nil, problem.Validation.WithField("kind", "MS05 hoặc MS06")
	}
	if !c.center() && ((kind != "MS05" && kind != AppraisalScores) || unit != "" && unit != c.unit) {
		return AppraisalPaper{}, nil, problem.Forbidden
	}
	t := p.Summaries[kind]
	if kind == AppraisalScores {
		score := appraisalScoreTemplate(defaultAppraisalScoreLayout())
		t.Summary = &score
	}
	if t.Summary == nil {
		return AppraisalPaper{}, nil, port.ErrAppraisalNotFound
	}
	rows, err := store.AppraisalEntrySummaries(ctx, tx, period, true)
	if err != nil {
		return AppraisalPaper{}, nil, err
	}
	out := AppraisalPaper{Period: redactedAppraisalPeriod(c, p), Template: *t.Summary, Items: []AppraisalItem{}}
	for _, row := range rows {
		x, e := appraisalItem(row, c)
		if e != nil {
			return out, nil, e
		}
		if (kind != AppraisalScores && x.Member.SummaryKind != kind) || (!c.center() && x.Member.SummaryKind != "MS05") || x.Member.ExcludedReason != "" || !c.canRead(x.Member) || (unit != "" && unit != x.Member.ManagementUnit) ||
			!scope.Allows(x.Member.AccountRef != nil && *x.Member.AccountRef == c.ref, c.unit != "" && c.unit == x.Member.ManagementUnit) {
			continue
		}
		out.Items = append(out.Items, x)
	}
	if len(out.Items) == 0 {
		return out, nil, port.ErrAppraisalNotFound
	}
	appraisalPaperOrder(out.Items)
	versions := make([]uuid.UUID, 0, len(out.Items))
	for _, x := range out.Items {
		versions = append(versions, x.ID)
	}
	return out, versions, nil
}

func (s *Service) PreviewAppraisal(ctx context.Context, period uuid.UUID, kind, unit string) (AppraisalPaper, error) {
	c, scope, err := s.appraisalExportCaller(ctx, false)
	if err != nil {
		return AppraisalPaper{}, err
	}
	var out AppraisalPaper
	err = s.tx(ctx, c.tenant, func(tx pgx.Tx) error {
		var e error
		out, _, e = s.appraisalPaperTx(ctx, tx, c, scope, period, kind, unit)
		if e != nil {
			return e
		}
		return appraisalFileAccess(ctx, tx, c, out.Items, "preview", period, s.Clock.Now())
	})
	if err != nil {
		return out, classify(err)
	}
	out.Items, err = s.appraisalPaperNames(ctx, out.Items)
	return out, err
}

func appraisalFileAccess(ctx context.Context, tx pgx.Tx, c appraisalActor, items []AppraisalItem, action string, period uuid.UUID, at time.Time) error {
	staff := make([]uuid.UUID, 0, len(items))
	for _, x := range items {
		staff = append(staff, x.Member.StaffID)
	}
	return store.InsertAppraisalAccess(ctx, tx, c.tenant, store.Access{ID: ids.New(), Action: "export", StaffIDs: staff, ItemCount: len(staff), Scope: string(c.scope), Actor: c.ref,
		Query: map[string]string{"appraisal_period": period.String(), "action": action}}, at)
}

// Giấy để ký chỉ dùng đề xuất đã gửi HCTH hoặc bản đã chốt. Bản xem trước vẫn
// có thể hiển thị tiến độ chưa đủ, nhưng không tạo một lần xuất hợp thức hóa dữ liệu thiếu.
func validateAppraisalExport(items []AppraisalItem) error {
	for _, item := range items {
		if item.Status != domain.AppraisalSent && item.Status != domain.AppraisalClosed {
			return problem.Validation.WithField("entries", "còn người chưa gửi đủ đề xuất HCTH")
		}
		if err := domain.ValidateAppraisalProposal(item.SelfRating, item.CollectiveRating, item.Reason, true); err != nil {
			return appraisalValidation(err)
		}
	}
	return nil
}

// reserveAppraisalExport chụp đúng các phiên bản dưới cùng khóa đợt. Retry sau lỗi kho/Word
// tiếp tục snapshot này, không âm thầm lấy nội dung mới rồi dùng lại Idempotency-Key.
func (s *Service) reserveAppraisalExport(ctx context.Context, c appraisalActor, scope domain.Scope, period uuid.UUID, idem, hash string, in AppraisalExportInput) (AppraisalExport, error) {
	var out AppraisalExport
	err := s.tx(ctx, c.tenant, func(tx pgx.Tx) error {
		old, ok, e := appraisalReplay(ctx, tx, c, idem, hash)
		if e != nil {
			return e
		}
		if ok {
			latest, exists, e := store.AppraisalLatest(ctx, tx, "export", old.Key)
			if e != nil {
				return e
			}
			if !exists {
				return port.ErrAppraisalNotFound
			}
			out, e = appraisalExportRow(latest)
			return e
		}
		if e = store.LockAppraisal(ctx, tx, c.tenant, "period-id:"+period.String()); e != nil {
			return e
		}
		paper, versions, e := s.appraisalPaperTx(ctx, tx, c, scope, period, in.Kind, in.Unit)
		if e != nil {
			return e
		}
		if paper.Period.Version != in.Version {
			return port.ErrAppraisalConflict
		}
		if in.Kind != AppraisalScores {
			if e = validateAppraisalExport(paper.Items); e != nil {
				return e
			}
		}
		p, exists, e := store.AppraisalPeriodByRef(ctx, tx, period)
		if e != nil {
			return e
		}
		if !exists {
			return port.ErrAppraisalNotFound
		}
		out = AppraisalExport{ID: ids.New(), PeriodID: period, PeriodVersion: p.ID, Kind: in.Kind, Format: in.Format, Unit: in.Unit,
			EntryVersions: versions, Status: "pending", Count: len(versions), Actor: c.ref, At: s.Clock.Now().UTC()}
		if in.Kind == AppraisalScores {
			layout := defaultAppraisalScoreLayout()
			out.ScoreLayout = &layout
		}
		out.FileName = fmt.Sprintf("%s-%d-q%d-%s.%s", in.Kind, paper.Period.Year, paper.Period.Quarter, out.ID.String(), in.Format)
		raw, e := appraisalJSON(out)
		if e != nil {
			return e
		}
		if _, e = store.InsertAppraisal(ctx, tx, c.tenant, store.AppraisalEvent{ID: ids.New(), Kind: "export", Key: out.ID.String(), PeriodRef: &period,
			Action: "draft", Payload: raw, RequestHash: hash, IdemKey: idem, Actor: c.ref}, s.Clock.Now()); e != nil {
			return e
		}
		return appraisalFileAccess(ctx, tx, c, paper.Items, "prepare", period, s.Clock.Now())
	})
	return out, classify(err)
}

func (s *Service) frozenAppraisalPaper(ctx context.Context, c appraisalActor, scope domain.Scope, x AppraisalExport) (AppraisalPaper, error) {
	var out AppraisalPaper
	err := s.tx(ctx, c.tenant, func(tx pgx.Tx) error {
		pr, ok, e := store.AppraisalVersion(ctx, tx, x.PeriodVersion)
		if e != nil {
			return e
		}
		if !ok || pr.Kind != "period" || pr.PeriodRef == nil || *pr.PeriodRef != x.PeriodID {
			return port.ErrAppraisalNotFound
		}
		out.Period, e = appraisalPeriod(pr)
		if e != nil {
			return e
		}
		t := out.Period.Summaries[x.Kind]
		if x.Kind == AppraisalScores && x.ScoreLayout != nil {
			score := appraisalScoreTemplate(*x.ScoreLayout)
			t.Summary = &score
		}
		if t.Summary == nil {
			return port.ErrAppraisalNotFound
		}
		out.Template = *t.Summary
		if !c.center() && ((x.Kind != "MS05" && x.Kind != AppraisalScores) || x.Unit != "" && x.Unit != c.unit) {
			return problem.Forbidden
		}
		out.Items = []AppraisalItem{}
		for _, id := range x.EntryVersions {
			r, exists, e := store.AppraisalVersion(ctx, tx, id)
			if e != nil {
				return e
			}
			if !exists || r.Kind != "entry" || r.PeriodRef == nil || *r.PeriodRef != x.PeriodID {
				return port.ErrAppraisalNotFound
			}
			item, e := appraisalItem(r, c)
			if e != nil {
				return e
			}
			if !c.canRead(item.Member) || !scope.Allows(item.Member.AccountRef != nil && *item.Member.AccountRef == c.ref, c.unit != "" && c.unit == item.Member.ManagementUnit) {
				return problem.Forbidden
			}
			out.Items = append(out.Items, item)
		}
		return appraisalFileAccess(ctx, tx, c, out.Items, "render", x.PeriodID, s.Clock.Now())
	})
	if err != nil {
		return out, classify(err)
	}
	out.Items, err = s.appraisalPaperNames(ctx, out.Items)
	return out, err
}

// Xuất giấy không âm thầm thay tên bằng mã khi kho từ chối. Quyền đánh giá không
// tự cấp quyền kho định danh; việc đã xóa định danh vẫn dùng nhãn xóa chuẩn của kho.
func (s *Service) appraisalPaperNames(ctx context.Context, items []AppraisalItem) ([]AppraisalItem, error) {
	refs := make([]uuid.UUID, 0, len(items))
	for _, x := range items {
		refs = append(refs, x.Member.SubjectRef)
	}
	names, err := s.readPII(ctx, refs, client.PurposeHRMProfile, true, "")
	if err != nil {
		return nil, err
	}
	for i := range items {
		n := names[items[i].Member.SubjectRef]
		items[i].Name = n.FullName
		if n.Erased {
			items[i].Name = client.ErasedLabel
		}
	}
	return items, nil
}

func appraisalExcel(x AppraisalExport, p AppraisalPaper) ([]byte, error) {
	cutoff, _ := time.Parse(time.DateOnly, p.Period.CutoffOn)
	t := domain.Table{Code: domain.ReportCode(x.Kind), Title: appraisalTitle(p.Template, p.Period), Period: p.Template.Audience, AsOf: cutoff}
	for _, c := range p.Template.Columns {
		t.Columns = append(t.Columns, domain.Column{Key: c.Key, Label: c.Title})
	}
	for i, item := range p.Items {
		t.Rows = append(t.Rows, domain.Row{Kind: domain.RowPerson, Cells: appraisalPaperCells(item, i+1, p.Template)})
	}
	return renderXLSX(t, xlsxMeta{SignerTitle: p.Template.Signature, Exporter: x.Actor.String(), ExportedAt: x.At})
}

func (s *Service) ExportAppraisal(ctx context.Context, period uuid.UUID, idem string, in AppraisalExportInput) (AppraisalExport, error) {
	c, scope, err := s.appraisalExportCaller(ctx, true)
	if err != nil {
		return AppraisalExport{}, err
	}
	if err = appraisalKey(idem); err != nil {
		return AppraisalExport{}, err
	}
	if !slices.Contains([]string{"xlsx", "docx"}, in.Format) {
		return AppraisalExport{}, problem.Validation.WithField("format", "xlsx hoặc docx")
	}
	if in.Kind == AppraisalScores && in.Format != "xlsx" {
		return AppraisalExport{}, problem.Validation.WithField("format", "bảng tổng hợp điểm chỉ xuất Excel nội bộ")
	}
	if in.Format == "docx" && s.AppraisalExporter == nil {
		return AppraisalExport{}, port.ErrAppraisalUnavailable.WithDetail("chưa nối cap/export")
	}
	hash, err := appraisalHash(c.ref, "export:"+period.String(), in)
	if err != nil {
		return AppraisalExport{}, err
	}
	x, err := s.reserveAppraisalExport(ctx, c, scope, period, idem, hash, in)
	if err != nil {
		return x, err
	}
	// Cả replay thành công vẫn phải kiểm lại quyền đối với từng dòng snapshot trước trả metadata.
	p, err := s.frozenAppraisalPaper(ctx, c, scope, x)
	if err != nil {
		return x, err
	}
	if x.Status == "ready" {
		return x, nil
	}
	if x.Format == "xlsx" {
		data, e := appraisalExcel(x, p)
		if e != nil {
			return x, e
		}
		sum := sha256.Sum256(data)
		x.SHA256, x.Size = hex.EncodeToString(sum[:]), len(data)
	} else {
		// Cổng bên dùng đã kiểm assessment/export; cap/export không được dùng để cấp quyền nghiệp vụ.
		run, e := s.AppraisalExporter.Export(ctx, exportport.Request{Template: AppraisalWordTemplate(x.Kind).Key, Version: 1, Subject: "hrm_appraisal:" + x.ID.String(),
			FileName: x.FileName, Data: appraisalWordData(p.Template, p.Period, p.Items, x.ID.String()), IdemKey: x.ID.String()})
		if e != nil {
			return x, e
		}
		x.WordRun, x.SHA256, x.Size = &run.ID, run.BodyHash, len(run.Body)
	}
	x.Status = "ready"
	err = s.tx(ctx, c.tenant, func(tx pgx.Tx) error {
		if e := store.LockAppraisal(ctx, tx, c.tenant, "request:"+idem); e != nil {
			return e
		}
		old, ok, e := store.AppraisalLatest(ctx, tx, "export", x.ID.String())
		if e != nil {
			return e
		}
		if !ok {
			return port.ErrAppraisalNotFound
		}
		v, e := appraisalExportRow(old)
		if e != nil {
			return e
		}
		if v.Status == "ready" {
			x = v
			return nil
		}
		raw, e := appraisalJSON(x)
		if e != nil {
			return e
		}
		_, e = store.InsertAppraisal(ctx, tx, c.tenant, store.AppraisalEvent{ID: ids.New(), Kind: "export", Key: x.ID.String(), PeriodRef: &period, Action: "export",
			Payload: raw, RequestHash: hash, IdemKey: idem + ":complete", Actor: c.ref}, s.Clock.Now())
		return e
	})
	return x, classify(err)
}

func (s *Service) AppraisalExports(ctx context.Context, period uuid.UUID) ([]AppraisalExport, error) {
	c, scope, err := s.appraisalExportCaller(ctx, false)
	if err != nil {
		return nil, err
	}
	out := []AppraisalExport{}
	err = s.tx(ctx, c.tenant, func(tx pgx.Tx) error {
		if _, e := loadAppraisalPeriod(ctx, tx, period); e != nil {
			return e
		}
		rows, e := store.AppraisalCurrent(ctx, tx, "export", &period)
		if e != nil {
			return e
		}
		for _, row := range rows {
			x, e := appraisalExportRow(row)
			if e != nil {
				return e
			}
			if x.Actor == c.ref || c.center() && c.scope == domain.ScopeAll && scope == domain.ScopeAll {
				out = append(out, x)
			}
		}
		return nil
	})
	slices.SortFunc(out, func(a, b AppraisalExport) int { return b.At.Compare(a.At) })
	return out, classify(err)
}

type AppraisalExportFile struct {
	Data                  []byte
	FileName, ContentType string
	Drift                 bool
	SHA256                string
}

func (s *Service) DownloadAppraisalExport(ctx context.Context, period, id uuid.UUID) (AppraisalExportFile, error) {
	c, scope, err := s.appraisalExportCaller(ctx, false)
	if err != nil {
		return AppraisalExportFile{}, err
	}
	var x AppraisalExport
	err = s.tx(ctx, c.tenant, func(tx pgx.Tx) error {
		r, ok, e := store.AppraisalLatest(ctx, tx, "export", id.String())
		if e != nil {
			return e
		}
		if !ok {
			return port.ErrAppraisalNotFound
		}
		x, e = appraisalExportRow(r)
		return e
	})
	if err != nil {
		return AppraisalExportFile{}, classify(err)
	}
	if x.PeriodID != period || x.Status != "ready" {
		return AppraisalExportFile{}, port.ErrAppraisalNotFound
	}
	p, err := s.frozenAppraisalPaper(ctx, c, scope, x)
	if err != nil {
		return AppraisalExportFile{}, err
	}
	out := AppraisalExportFile{FileName: x.FileName}
	if x.Format == "xlsx" {
		out.Data, err = appraisalExcel(x, p)
		out.ContentType = "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
	} else {
		if s.AppraisalExporter == nil || x.WordRun == nil {
			return out, port.ErrAppraisalUnavailable
		}
		var run exportport.Run
		run, err = s.AppraisalExporter.Run(ctx, "hrm_appraisal:"+x.ID.String(), *x.WordRun)
		out.Data, out.ContentType = run.Body, run.ContentType
	}
	if err != nil {
		return out, err
	}
	sum := sha256.Sum256(out.Data)
	out.SHA256 = hex.EncodeToString(sum[:])
	out.Drift = out.SHA256 != x.SHA256
	// DLP/đo xuất cùng cơ chế HRM hiện hữu, sau khi mọi giao dịch đã đóng.
	s.reportEgress(ctx, rc{ref: c.ref, tenant: c.tenant}, domain.ReportCode(x.Kind), x.Count, len(out.Data))
	return out, nil
}
