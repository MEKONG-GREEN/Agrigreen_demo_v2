package app

// Đồng bộ ngoại tuyến của vòng đời báo cáo cho field-app Cán bộ xã (KN v7.7 P7, FN-XA-03.01/03.02, N-07;
// docs/dev/sync-contract.md mục "Báo cáo KN v7.7"). Chiều xuống: report_task, form_layout, report_submission của
// các xã phụ trách trong ngữ cảnh phiên (X-AG-Context, vai kn_xa). Chiều lên: report_submission (lưu nháp/gửi) với
// id UUIDv7 máy sinh — đi qua đúng use case Submit của REST (cùng kiểm ô, công thức tính ở máy chủ, chốt kỳ).

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/reportcycle/store"
	repport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/port"
	syncport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/sync/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// ReportSync là Source + Sink của pf/sync cho vòng đời báo cáo.
type ReportSync struct{ Svc *Service }

var _ syncport.Source = ReportSync{}

// ReportSink là Sink riêng (pf/sync khai Source và Sink ở hai danh sách).
type ReportSink struct{ Svc *Service }

var _ syncport.Sink = ReportSink{}

// Entities thực hiện syncport.Source.
func (ReportSync) Entities() []string {
	return []string{port.EntityFormLayout, port.EntityReportSubmission, port.EntityReportTask}
}

// syncActor: chỉ ngữ cảnh Cấp Xã nhận/gửi dữ liệu báo cáo qua đồng bộ. Không có ngữ cảnh hoặc vai khác → 403 để pf/sync
// bỏ qua entity (thiết bị cũ không gửi X-AG-Context vẫn đồng bộ các entity khác).
func syncActor(ctx context.Context, sc authz.Scope) (authz.Principal, port.SessionContext, []string, error) {
	p, ok := authz.FromContext(ctx)
	if !ok {
		return p, port.SessionContext{}, nil, problem.Unauthenticated
	}
	k, err := port.ReadSessionContext(p)
	if err != nil || k.Role != port.SessionRoleCommune {
		return p, port.SessionContext{}, nil, problem.Forbidden.WithDetail("cần ngữ cảnh Cấp Xã (X-AG-Context)")
	}
	communes := []string{}
	for _, c := range k.Communes {
		if sc.Allows(c) && !slices.Contains(communes, c) {
			communes = append(communes, c)
		}
	}
	return p, k, communes, nil
}

func clampSync(limit int) int {
	if limit <= 0 || limit > 1000 {
		return 500
	}
	return limit
}

// reportTaskData là dữ liệu công việc gửi xuống máy — không PII (chỉ mã xã, mã tham chiếu).
type reportTaskData struct {
	ID                    string            `json:"id"`
	BatchID               string            `json:"batch_id"`
	PeriodID              string            `json:"period_id"`
	TemplateID            string            `json:"template_id"`
	VersionID             string            `json:"version_id"`
	FormCode              string            `json:"form_code"`
	FormName              string            `json:"form_name"`
	CommuneCode           string            `json:"commune_code"`
	CommuneName           string            `json:"commune_name"`
	DepartmentCode        string            `json:"department_code"`
	ReportType            string            `json:"report_type"`
	PeriodType            string            `json:"period_type"`
	Slot                  int               `json:"slot"`
	PeriodBlock           bool              `json:"period_block"`
	PeriodLabel           string            `json:"period_label"`
	PeriodRangeLabel      string            `json:"period_range_label"`
	PeriodLabels          []string          `json:"period_labels"`
	SeasonLabel           string            `json:"season_label"`
	PeriodFrom            string            `json:"period_from"`
	PeriodTo              string            `json:"period_to"`
	DueAt                 string            `json:"due_at"`
	AutoCloseAt           string            `json:"auto_close_at"`
	PeriodClosed          bool              `json:"period_closed"`
	ReopenedUntil         *string           `json:"reopened_until"`
	Status                string            `json:"status"`
	DisplayStatus         string            `json:"display_status"`
	Open                  bool              `json:"open"`
	OpenSlots             []int             `json:"open_slots"`
	AdjustmentReason      *string           `json:"adjustment_reason"`
	LatestVersion         int               `json:"latest_version"`
	LatestSubmissionID    *string           `json:"latest_submission_id"`
	SubmittedSubmissionID *string           `json:"submitted_submission_id"`
	SubmittedAt           *string           `json:"submitted_at"`
	Provincial            map[string]string `json:"provincial"`
	UpdatedAt             string            `json:"updated_at"`
}

type formLayoutData struct {
	VersionID  string                 `json:"version_id"`
	TemplateID string                 `json:"template_id"`
	Code       string                 `json:"code"`
	Name       string                 `json:"name"`
	Version    int                    `json:"version"`
	Definition repport.FormDefinition `json:"definition"`
}

type reportSubmissionData struct {
	TaskID       string              `json:"task_id"`
	SubmissionID string              `json:"submission_id"`
	VersionNo    int                 `json:"version_no"`
	Kind         string              `json:"kind"`
	Rows         []store.SubRow      `json:"rows"`
	Issues       []repport.CellIssue `json:"issues"`
	EventTime    string              `json:"event_time"`
	RecordedAt   string              `json:"recorded_at"`
}

func optTime(t *time.Time) *string {
	if t == nil {
		return nil
	}
	v := ts(*t)
	return &v
}

// Changes thực hiện syncport.Source.
func (r ReportSync) Changes(ctx context.Context, sc authz.Scope, entity string, after int64, limit int) ([]syncport.Change, int64, error) {
	p, k, communes, err := syncActor(ctx, sc)
	if err != nil {
		return nil, after, err
	}
	if len(communes) == 0 {
		return nil, after, nil
	}
	limit = clampSync(limit)
	s := r.Svc
	switch entity {
	case port.EntityReportTask:
		return s.syncTasks(ctx, p, k, communes, after, limit)
	case port.EntityReportSubmission:
		return s.syncSubmissions(ctx, p, k, communes, after, limit)
	case port.EntityFormLayout:
		return s.syncLayouts(ctx, p, k, communes, after, limit)
	}
	return nil, after, problem.Validation.WithField("entity", "không có entity "+entity)
}

func (s *Service) syncTasks(ctx context.Context, p authz.Principal, k port.SessionContext, communes []string, after int64, limit int) ([]syncport.Change, int64, error) {
	now := s.now()
	var out []syncport.Change
	last := after
	books := map[string]*nameBook{}
	err := s.tx(ctx, p.TenantID, func(tx pgx.Tx) error {
		tasks, err := store.TasksChangedSince(ctx, tx, k.Department, communes, after, limit, false)
		if err != nil {
			return err
		}
		pids, bids := []uuid.UUID{}, []uuid.UUID{}
		for _, t := range tasks {
			pids = append(pids, t.PeriodID)
			bids = append(bids, t.BatchID)
		}
		periods, err := store.PeriodsByIDs(ctx, tx, pids)
		if err != nil {
			return err
		}
		batches, err := store.BatchesByIDs(ctx, tx, bids)
		if err != nil {
			return err
		}
		labels := map[uuid.UUID][]string{}
		provincial := map[[2]uuid.UUID]map[string]map[string]string{}
		for _, t := range tasks {
			last = max(last, t.SyncSeq)
			b, okB := batches[t.BatchID]
			if t.Withdrawn || !okB || b.Status == "withdrawn" {
				out = append(out, syncport.Change{Entity: port.EntityReportTask, ID: t.ID, Version: t.SyncSeq, Op: syncport.OpTombstone})
				continue
			}
			if _, ok := labels[t.BatchID]; !ok {
				ps, err := store.PeriodsOfBatch(ctx, tx, t.BatchID)
				if err != nil {
					return err
				}
				ls := make([]string, 0, len(ps))
				for _, x := range ps {
					ls = append(ls, x.Label)
				}
				labels[t.BatchID] = ls
			}
			pk := [2]uuid.UUID{t.BatchID, t.TemplateID}
			if _, ok := provincial[pk]; !ok {
				tpl := t.TemplateID
				pv, err := store.ProvincialValues(ctx, tx, t.BatchID, &tpl)
				if err != nil {
					return err
				}
				provincial[pk] = pv[t.TemplateID]
			}
			nb := books[t.ProvinceCode]
			if nb == nil {
				if nb, err = s.regionNames(ctx, t.ProvinceCode); err != nil {
					return err
				}
				books[t.ProvinceCode] = nb
			}
			per := periods[t.PeriodID]
			v := viewOf(t.Task, per, &b, now)
			d := reportTaskData{ID: t.ID.String(), BatchID: t.BatchID.String(), PeriodID: t.PeriodID.String(), TemplateID: t.TemplateID.String(),
				VersionID: t.VersionID.String(), FormCode: t.FormCode, CommuneCode: t.CommuneCode, CommuneName: nb.commune(ctx, s, t.CommuneCode),
				DepartmentCode: t.DepartmentCode, ReportType: b.ReportType, PeriodType: b.PeriodType, Slot: t.Slot, PeriodBlock: t.PeriodBlock,
				PeriodLabel: per.Label, PeriodRangeLabel: per.RangeLabel, PeriodLabels: labels[t.BatchID], SeasonLabel: b.SeasonLabel,
				PeriodFrom: per.From.Format(time.DateOnly), PeriodTo: per.To.Format(time.DateOnly), DueAt: ts(t.DueAt), AutoCloseAt: ts(per.AutoCloseAt),
				PeriodClosed: per.Status == "closed", ReopenedUntil: optTime(t.ReopenedUntil), Status: t.Status, DisplayStatus: v.Display, Open: v.Open,
				OpenSlots: []int{}, AdjustmentReason: t.AdjustmentReason, LatestVersion: t.LatestVersion, LatestSubmissionID: ptrString(t.LatestSubmissionID),
				SubmittedSubmissionID: ptrString(t.SubmittedSubmissionID), SubmittedAt: optTime(t.SubmittedAt),
				Provincial: provincial[pk][t.CommuneCode], UpdatedAt: ts(t.UpdatedAt)}
			if d.Provincial == nil {
				d.Provincial = map[string]string{}
			}
			if d.PeriodLabels == nil {
				d.PeriodLabels = []string{}
			}
			for _, f := range b.Forms {
				if f.TemplateID == t.TemplateID {
					d.FormName = f.Name
				}
			}
			if v.Open && t.PeriodBlock {
				d.OpenSlots = []int{t.Slot}
			}
			raw, err := json.Marshal(d)
			if err != nil {
				return err
			}
			out = append(out, syncport.Change{Entity: port.EntityReportTask, ID: t.ID, Version: t.SyncSeq, Op: syncport.OpUpsert, Data: raw})
		}
		return nil
	})
	return out, last, err
}

func (s *Service) syncSubmissions(ctx context.Context, p authz.Principal, k port.SessionContext, communes []string, after int64, limit int) ([]syncport.Change, int64, error) {
	var out []syncport.Change
	last := after
	err := s.tx(ctx, p.TenantID, func(tx pgx.Tx) error {
		tasks, err := store.TasksChangedSince(ctx, tx, k.Department, communes, after, limit, true)
		if err != nil {
			return err
		}
		for _, t := range tasks {
			last = max(last, t.SyncSeq)
			if t.Withdrawn || t.LatestSubmissionID == nil {
				out = append(out, syncport.Change{Entity: port.EntityReportSubmission, ID: t.ID, Version: t.SyncSeq, Op: syncport.OpTombstone})
				continue
			}
			sub, err := store.SubmissionByID(ctx, tx, *t.LatestSubmissionID)
			if err != nil {
				return err
			}
			if sub == nil {
				continue
			}
			d := reportSubmissionData{TaskID: t.ID.String(), SubmissionID: sub.ID.String(), VersionNo: sub.VersionNo, Kind: sub.Kind, Rows: sub.Rows,
				Issues: []repport.CellIssue{}, EventTime: ts(sub.EventTime), RecordedAt: ts(sub.RecordedAt)}
			if d.Rows == nil {
				d.Rows = []store.SubRow{}
			}
			if len(sub.Issues) > 0 {
				_ = json.Unmarshal(sub.Issues, &d.Issues)
			}
			raw, err := json.Marshal(d)
			if err != nil {
				return err
			}
			out = append(out, syncport.Change{Entity: port.EntityReportSubmission, ID: t.ID, Version: t.SyncSeq, Op: syncport.OpUpsert, Data: raw})
		}
		return nil
	})
	return out, last, err
}

func (s *Service) syncLayouts(ctx context.Context, p authz.Principal, k port.SessionContext, communes []string, after int64, limit int) ([]syncport.Change, int64, error) {
	if s.Forms == nil {
		return nil, after, nil // chưa nối engine mẫu (pf/reports): không giao bố cục, entity khác vẫn đồng bộ
	}
	var vs []store.LayoutVersion
	err := s.tx(ctx, p.TenantID, func(tx pgx.Tx) error {
		var e error
		vs, e = store.LayoutVersionsSince(ctx, tx, k.Department, communes, after, limit)
		return e
	})
	if err != nil {
		return nil, after, err
	}
	var out []syncport.Change
	last := after
	for _, v := range vs {
		last = max(last, v.Seq)
		fv, err := s.Forms.FormVersion(ctx, p.TenantID, v.VersionID)
		if errors.Is(err, repport.ErrFormNotFound) {
			continue
		}
		if err != nil {
			return nil, after, err
		}
		raw, err := json.Marshal(formLayoutData{VersionID: fv.VersionID.String(), TemplateID: fv.TemplateID.String(), Code: fv.Code, Name: fv.Name,
			Version: fv.Version, Definition: fv.Definition})
		if err != nil {
			return nil, after, err
		}
		out = append(out, syncport.Change{Entity: port.EntityFormLayout, ID: fv.VersionID, Version: v.Seq, Op: syncport.OpUpsert, Data: raw})
	}
	return out, last, nil
}

// SubmissionPayload là payload chiều lên report_submission.
type SubmissionPayload struct {
	TaskID      uuid.UUID `json:"task_id"`
	Kind        string    `json:"kind"`
	BaseVersion *int      `json:"base_version"`
	Rows        []struct {
		Indicator string            `json:"indicator"`
		Input     map[string]string `json:"input"`
	} `json:"rows"`
}

// Entities thực hiện syncport.Sink.
func (ReportSink) Entities() []string { return []string{port.EntityReportSubmission} }

// Accept thực hiện syncport.Sink: một lần Lưu nháp / Gửi báo cáo tạo ngoại tuyến. id của bản ghi = id bài nộp (P4 nhận
// gửi lại cùng id → duplicate). Bản nộp dựa trên phiên bản cũ hơn phiên bản người/máy khác vừa ghi → vẫn lưu thành
// phiên bản mới (RB-1, giữ cả hai) nhưng trả conflict để người dùng kiểm lại.
func (r ReportSink) Accept(ctx context.Context, _ authz.Scope, rec syncport.Record) syncport.Result {
	res := syncport.Result{ID: rec.ID}
	reject := func(code string, errs ...problem.FieldError) syncport.Result {
		res.Status, res.Code, res.Errors = syncport.StatusRejected, code, errs
		return res
	}
	if rec.Op != syncport.OpCreate {
		return reject(problem.Validation.Code, problem.FieldError{Field: "op", Reason: "report_submission chỉ nhận create (mỗi lần lưu là một phiên bản mới)"})
	}
	var pl SubmissionPayload
	if err := json.Unmarshal(rec.Payload, &pl); err != nil || pl.TaskID == uuid.Nil {
		return reject(problem.Validation.Code, problem.FieldError{Field: "payload.task_id", Reason: "bắt buộc, UUID"})
	}
	in := SubmissionInput{ID: &rec.ID, Kind: pl.Kind, DeviceID: rec.DeviceID.String()}
	if !rec.EventTime.IsZero() {
		et := rec.EventTime
		in.EventTime = &et
	}
	for _, row := range pl.Rows {
		in.Rows = append(in.Rows, store.SubRow{Indicator: row.Indicator, Input: row.Input})
	}
	out, err := r.Svc.Submit(ctx, pl.TaskID, in, rec.IdemKey)
	if err != nil {
		var pr *problem.Problem
		if !errors.As(err, &pr) || pr.Status >= http.StatusInternalServerError {
			return reject(syncport.CodeSinkFailed)
		}
		errs := append([]problem.FieldError(nil), pr.Errors...)
		for _, is := range out.Issues {
			if is.Severity != repport.SeverityError {
				continue
			}
			f := "rows." + is.RowKey
			if is.Letter != "" {
				f += "." + is.Letter
			}
			errs = append(errs, problem.FieldError{Field: f, Reason: is.Message})
		}
		return reject(pr.Code, errs...)
	}
	if out.Replayed {
		res.Status = syncport.StatusDuplicate
		return res
	}
	res.Status = syncport.StatusAccepted
	if pl.BaseVersion != nil && out.Submission.PreviousID != nil {
		prev, err := r.Svc.previousSubmission(ctx, *out.Submission.PreviousID)
		if err == nil && prev != nil && prev.VersionNo > *pl.BaseVersion && (prev.DeviceID == nil || *prev.DeviceID != in.DeviceID) {
			res.Status = syncport.StatusConflict
		}
	}
	return res
}

func (s *Service) previousSubmission(ctx context.Context, id uuid.UUID) (*store.Submission, error) {
	p, ok := authz.FromContext(ctx)
	if !ok {
		return nil, problem.Unauthenticated
	}
	var sub *store.Submission
	err := s.tx(ctx, p.TenantID, func(tx pgx.Tx) error {
		var e error
		sub, e = store.SubmissionByID(ctx, tx, id)
		return e
	})
	return sub, err
}
