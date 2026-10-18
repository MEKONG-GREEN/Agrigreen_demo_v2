package app

// Đồng bộ ngoại tuyến của kỳ native `cap/forms` cho field-app Cấp Xã (W11.42, BRD CNTS v3.7 UR-XA-13 (5), NFR "Làm
// việc offline"; docs/dev/sync-contract.md mục "Biểu kỳ cap/forms"). Theo mẫu báo cáo KN v7.7 (extension/reportcycle
// ReportSync): chiều xuống forms_task = một công việc (kỳ × đơn vị trong phạm vi, kèm định nghĩa biểu và bài nộp cuối);
// chiều lên forms_submission = một lần gửi tạo ngoại tuyến, id UUIDv7 máy sinh = mã phiên bản, đi qua đúng use case
// SubmitWith của REST (khóa lạc quan, báo không phát sinh, quy tắc nghiệp vụ). Bản đến sau khi kỳ đã chốt với đơn vị
// được GIỮ "Chờ mở kỳ" (held_submission). Con trỏ = pf_forms.sync_feed.feed_seq (pf 000149).

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/store"
	syncport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/sync/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// SyncSource là Source chiều xuống forms_task.
type SyncSource struct{ Svc *Service }

// SyncSink là Sink chiều lên forms_submission.
type SyncSink struct{ Svc *Service }

var (
	_ syncport.Source = SyncSource{}
	_ syncport.Sink   = SyncSink{}
)

// taskNS là không gian tên UUIDv5 của mã công việc (kỳ × đơn vị) — ổn định giữa các lần đồng bộ.
var taskNS = uuid.MustParse("0192f3a0-5a42-7c11-8e42-42cafe0c0a42")

// TaskID trả mã công việc forms_task của đơn vị unit trong kỳ periodID.
func TaskID(periodID uuid.UUID, unit string) uuid.UUID {
	return uuid.NewSHA1(taskNS, []byte(periodID.String()+"|"+unit))
}

// Entities thực hiện syncport.Source.
func (SyncSource) Entities() []string { return []string{port.EntityFormsTask} }

// leafProfiles trả các profile mà người gọi là cấp lá (nộp bài) — chỉ cấp lá nhận công việc qua đồng bộ. Không có →
// 403 để pf/sync bỏ qua entity.
func (s *Service) leafProfiles(ctx context.Context) ([]string, map[string]actor, error) {
	var names []string
	actors := map[string]actor{}
	for _, n := range s.order {
		_, a, err := s.resolve(ctx, n)
		if err != nil || !a.leaf {
			continue
		}
		names = append(names, n)
		actors[n] = a
	}
	if len(names) == 0 {
		return nil, nil, problem.Forbidden.WithDetail("cần ngữ cảnh cấp nộp bài (X-AG-Context)")
	}
	return names, actors, nil
}

// TaskData là dữ liệu công việc gửi xuống máy — không dữ liệu cá nhân (chỉ mã đơn vị, mã tham chiếu người).
type TaskData struct {
	ID              string            `json:"id"`
	Profile         string            `json:"profile"`
	PeriodID        string            `json:"period_id"`
	Unit            string            `json:"unit"`
	UnitKind        string            `json:"unit_kind"`
	Parent          string            `json:"parent"`
	TemplateID      string            `json:"template_id"`
	TemplateCode    string            `json:"template_code"`
	TemplateTitle   string            `json:"template_title"`
	TemplateVersion int64             `json:"template_version"`
	Fields          []domain.Field    `json:"fields"`
	Label           string            `json:"label"`
	PeriodKind      string            `json:"period_kind"`
	PeriodKey       string            `json:"period_key"`
	PeriodFrom      *string           `json:"period_from"`
	PeriodTo        *string           `json:"period_to"`
	DueAt           string            `json:"due_at"`
	PeriodStatus    string            `json:"period_status"`
	Open            bool              `json:"open"`
	Status          string            `json:"status"`
	Version         int64             `json:"version"`
	SubmissionID    *string           `json:"submission_id"`
	Values          map[string]string `json:"values"`
	NilReport       bool              `json:"nil_report"`
	Note            string            `json:"note"`
	Explanation     string            `json:"explanation"`
	Late            bool              `json:"late"`
	SubmittedAt     *string           `json:"submitted_at"`
	ReturnNote      *string           `json:"return_note"`
	HeldAt          *string           `json:"held_at"`
}

// TaskDataOf dựng dữ liệu công việc (dùng chung cho REST /forms/tasks và đồng bộ).
func TaskDataOf(t Task, tpl *store.Template) TaskData {
	d := TaskData{ID: TaskID(t.Period.ID, t.Unit.Unit).String(), Profile: t.Period.Profile, PeriodID: t.Period.ID.String(), Unit: t.Unit.Unit,
		UnitKind: t.Spec.UnitKind, Parent: t.Unit.Parent, TemplateID: t.Period.TemplateID.String(), TemplateCode: t.Period.TemplateCode,
		TemplateVersion: t.Period.TemplateVersion, Fields: []domain.Field{}, Label: t.Period.Label, PeriodKind: t.Spec.PeriodKind,
		PeriodKey: t.Spec.PeriodKey, DueAt: tsUTC(t.Period.DueAt), PeriodStatus: t.State.Status, Open: t.State.AcceptsUnit(t.Unit.Unit),
		Status: t.Unit.Status, Values: map[string]string{}}
	if tpl != nil {
		d.TemplateTitle, d.Fields = tpl.Title, tpl.Fields
	}
	if t.Spec.From != nil {
		f, to := t.Spec.From.Format(time.DateOnly), t.Spec.To.Format(time.DateOnly)
		d.PeriodFrom, d.PeriodTo = &f, &to
	}
	if sb := t.Unit.Submission; sb != nil {
		id, at := sb.ID.String(), tsUTC(sb.EventTime)
		d.Version, d.SubmissionID, d.Values, d.SubmittedAt = sb.Version, &id, sb.Values, &at
	}
	if m := t.Unit.Meta; m != nil {
		d.NilReport, d.Note, d.Explanation, d.Late = m.NilReport, m.Note, m.Explanation, m.Late
	}
	if r := t.Unit.Review; r != nil && r.Decision == domain.SubReturned && r.Note != "" {
		n := r.Note
		d.ReturnNote = &n
	}
	if h := t.Unit.Held; h != nil {
		at := tsUTC(h.EventTime)
		d.HeldAt = &at
	}
	return d
}

func tsUTC(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }

func clampSync(limit int) int {
	if limit <= 0 || limit > 1000 {
		return 500
	}
	return limit
}

// Changes thực hiện syncport.Source: công việc của các kỳ có thay đổi sau con trỏ after (phát hành, chốt, mở lại,
// hủy → mọi đơn vị trong phạm vi của kỳ; nộp, rà soát, bài giữ → đơn vị đó). Kỳ hủy → tombstone.
func (src SyncSource) Changes(ctx context.Context, sc authz.Scope, entity string, after int64, limit int) ([]syncport.Change, int64, error) {
	s := src.Svc
	if entity != port.EntityFormsTask {
		return nil, after, problem.Validation.WithField("entity", "không có entity "+entity)
	}
	names, actors, err := s.leafProfiles(ctx)
	if err != nil {
		return nil, after, err
	}
	p, _ := authz.FromContext(ctx)
	now := s.Clock.Now().UTC()
	limit = clampSync(limit)
	var out []syncport.Change
	last := after
	err = s.tx(ctx, p.TenantID, func(tx pgx.Tx) error {
		rows, err := store.FeedSince(ctx, tx, names, after, limit)
		if err != nil {
			return err
		}
		views := map[uuid.UUID]*PeriodView{}
		at := map[uuid.UUID]int{} // công việc đã ra trong lần gọi → vị trí (dòng nguồn sau thay dòng trước)
		emit := func(c syncport.Change) {
			if i, ok := at[c.ID]; ok {
				out[i] = c
				return
			}
			at[c.ID] = len(out)
			out = append(out, c)
		}
		tpls := map[uuid.UUID]*store.Template{}
		for _, r := range rows {
			last = max(last, r.Seq)
			v, ok := views[r.PeriodID]
			if !ok {
				if v, err = load(ctx, tx, r.PeriodID, now); err != nil {
					return err
				}
				views[r.PeriodID] = v
			}
			if v == nil {
				continue
			}
			a, ok := actors[v.Period.Profile]
			if !ok {
				continue
			}
			if _, ok := tpls[v.Period.TemplateID]; !ok {
				if tpls[v.Period.TemplateID], err = store.TemplateByID(ctx, tx, v.Period.TemplateID); err != nil {
					return err
				}
			}
			for _, u := range v.Units {
				if (r.Unit != "*" && r.Unit != u.Unit) || !a.allowsUnit(v.Spec, v.Period.Units, u.Unit) || !sc.Allows(u.Parent) {
					continue
				}
				id := TaskID(v.Period.ID, u.Unit)
				if v.State.Status == domain.PeriodCancelled {
					emit(syncport.Change{Entity: port.EntityFormsTask, ID: id, Version: r.Seq, Op: syncport.OpTombstone})
					continue
				}
				raw, err := json.Marshal(TaskDataOf(Task{Period: v.Period, Spec: v.Spec, State: v.State, Unit: u}, tpls[v.Period.TemplateID]))
				if err != nil {
					return err
				}
				emit(syncport.Change{Entity: port.EntityFormsTask, ID: id, Version: r.Seq, Op: syncport.OpUpsert, Data: raw})
			}
		}
		return nil
	})
	return out, last, err
}

// SubmissionPayload là payload chiều lên forms_submission.
type SubmissionPayload struct {
	Profile     string            `json:"profile"`
	PeriodID    uuid.UUID         `json:"period_id"`
	Unit        string            `json:"unit"`
	Values      map[string]string `json:"values"`
	BaseVersion *int64            `json:"base_version"`
	NilReport   bool              `json:"nil_report"`
	Note        string            `json:"note"`
	Explanation string            `json:"explanation"`
}

// CodeAwaitingReopen là mã kèm kết quả accepted khi bài được GIỮ "Chờ mở kỳ" (kỳ đã chốt lúc đồng bộ).
const CodeAwaitingReopen = "forms.awaiting_reopen"

// Entities thực hiện syncport.Sink.
func (SyncSink) Entities() []string { return []string{port.EntityFormsSubmission} }

// Accept thực hiện syncport.Sink: một lần Gửi tạo ngoại tuyến (id bản ghi = mã phiên bản bài nộp; gửi lại cùng id →
// duplicate). Phiên bản đã đổi ở nơi khác (base_version cũ) → rejected forms.submission_conflict: KHÔNG ghi đè, máy
// báo người dùng tải bản mới (CNTS NFR "Xung đột phiên bản: không ghi đè, báo người dùng"). Kỳ đã chốt → accepted kèm
// code forms.awaiting_reopen (bài giữ "Chờ mở kỳ", báo người nhập và Cấp Tỉnh).
func (snk SyncSink) Accept(ctx context.Context, _ authz.Scope, rec syncport.Record) syncport.Result {
	res := syncport.Result{ID: rec.ID}
	reject := func(code string, errs ...problem.FieldError) syncport.Result {
		res.Status, res.Code, res.Errors = syncport.StatusRejected, code, errs
		return res
	}
	if rec.Op != syncport.OpCreate {
		return reject(problem.Validation.Code, problem.FieldError{Field: "op", Reason: "forms_submission chỉ nhận create (mỗi lần gửi là một phiên bản mới)"})
	}
	var pl SubmissionPayload
	if err := json.Unmarshal(rec.Payload, &pl); err != nil || pl.PeriodID == uuid.Nil || pl.Profile == "" {
		return reject(problem.Validation.Code, problem.FieldError{Field: "payload", Reason: "profile, period_id, unit bắt buộc"})
	}
	in := SubmitInput{ID: &rec.ID, Unit: pl.Unit, Values: pl.Values, BaseVersion: pl.BaseVersion, NilReport: pl.NilReport, Note: pl.Note,
		Explanation: pl.Explanation, DeviceID: rec.DeviceID.String()}
	if !rec.EventTime.IsZero() {
		et := rec.EventTime
		in.CapturedAt = &et
	}
	out, err := snk.Svc.SubmitWith(ctx, pl.Profile, pl.PeriodID, in, rec.IdemKey)
	if err != nil {
		var pr *problem.Problem
		if !errors.As(err, &pr) || pr.Status >= http.StatusInternalServerError {
			return reject(syncport.CodeSinkFailed)
		}
		return reject(pr.Code, slices.Clone(pr.Errors)...)
	}
	switch {
	case out.Replayed:
		res.Status = syncport.StatusDuplicate
	case out.Status == domain.SubAwaitingReopen:
		res.Status, res.Code = syncport.StatusAccepted, CodeAwaitingReopen
	default:
		res.Status = syncport.StatusAccepted
	}
	return res
}
