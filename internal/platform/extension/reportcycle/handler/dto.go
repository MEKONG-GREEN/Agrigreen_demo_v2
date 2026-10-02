package handler

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extension/reportcycle/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extension/reportcycle/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extension/reportcycle/store"
	repport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/port"
)

func tsp(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := t.UTC().Format(time.RFC3339)
	return &s
}

func tss(t time.Time) string { return t.UTC().Format(time.RFC3339) }

func date(t time.Time) string { return t.Format(time.DateOnly) }

type formDTO struct {
	TemplateID  uuid.UUID `json:"template_id"`
	VersionID   uuid.UUID `json:"version_id"`
	Code        string    `json:"code"`
	Name        string    `json:"name"`
	Version     int       `json:"version"`
	PeriodBlock bool      `json:"period_block"`
}

type batchDTO struct {
	ID                 uuid.UUID  `json:"id"`
	DepartmentCode     string     `json:"department_code"`
	ProvinceCode       string     `json:"province_code"`
	ReportType         string     `json:"report_type"`
	PeriodType         string     `json:"period_type"`
	SeasonLabel        string     `json:"season_label"`
	FromDate           string     `json:"from_date"`
	ToDate             string     `json:"to_date"`
	PeriodRule         string     `json:"period_rule"`
	DeadlineOffsetDays int        `json:"deadline_offset_days"`
	DeadlineTime       string     `json:"deadline_time"`
	CloseAfterDays     int        `json:"close_after_days"`
	Note               *string    `json:"note"`
	Forms              []formDTO  `json:"forms"`
	Communes           []string   `json:"communes"`
	Status             string     `json:"status"`
	PeriodCount        int        `json:"period_count"`
	TaskCount          int        `json:"task_count"`
	IssuedBy           uuid.UUID  `json:"issued_by"`
	IssuedAt           string     `json:"issued_at"`
	WithdrawnBy        *uuid.UUID `json:"withdrawn_by"`
	WithdrawnAt        *string    `json:"withdrawn_at"`
	WithdrawReason     *string    `json:"withdraw_reason"`
	UpdatedAt          string     `json:"updated_at"`
}

func toBatch(b store.Batch) batchDTO {
	d := batchDTO{ID: b.ID, DepartmentCode: b.DepartmentCode, ProvinceCode: b.ProvinceCode, ReportType: b.ReportType, PeriodType: b.PeriodType,
		SeasonLabel: b.SeasonLabel, FromDate: date(b.From), ToDate: date(b.To), PeriodRule: b.PeriodRule, DeadlineOffsetDays: b.DeadlineOffsetDays,
		DeadlineTime: b.DeadlineTime, CloseAfterDays: b.CloseAfterDays, Note: b.Note, Forms: []formDTO{}, Communes: b.Communes, Status: b.Status,
		PeriodCount: b.PeriodCount, TaskCount: b.TaskCount, IssuedBy: b.IssuedBy, IssuedAt: tss(b.IssuedAt), WithdrawnBy: b.WithdrawnBy,
		WithdrawnAt: tsp(b.WithdrawnAt), WithdrawReason: b.WithdrawReason, UpdatedAt: tss(b.UpdatedAt)}
	if d.Communes == nil {
		d.Communes = []string{}
	}
	for _, f := range b.Forms {
		d.Forms = append(d.Forms, formDTO(f))
	}
	return d
}

type periodDTO struct {
	ID          uuid.UUID `json:"id"`
	BatchID     uuid.UUID `json:"batch_id"`
	Slot        int       `json:"slot"`
	Label       string    `json:"label"`
	RangeLabel  string    `json:"range_label"`
	ISOWeek     *int      `json:"iso_week"`
	FromDate    string    `json:"from_date"`
	ToDate      string    `json:"to_date"`
	DueAt       string    `json:"due_at"`
	AutoCloseAt string    `json:"auto_close_at"`
	Status      string    `json:"status"`
	ClosedAt    *string   `json:"closed_at"`
	ClosedBy    *uuid.UUID `json:"closed_by"`
	CloseMode   *string   `json:"close_mode"`
}

func toPeriod(p store.Period) periodDTO {
	return periodDTO{ID: p.ID, BatchID: p.BatchID, Slot: p.Slot, Label: p.Label, RangeLabel: p.RangeLabel, ISOWeek: p.ISOWeek, FromDate: date(p.From),
		ToDate: date(p.To), DueAt: tss(p.DueAt), AutoCloseAt: tss(p.AutoCloseAt), Status: p.Status, ClosedAt: tsp(p.ClosedAt), ClosedBy: p.ClosedBy,
		CloseMode: p.CloseMode}
}

func toPeriods(ps []store.Period) []periodDTO {
	out := make([]periodDTO, 0, len(ps))
	for _, p := range ps {
		out = append(out, toPeriod(p))
	}
	return out
}

type taskDTO struct {
	ID               uuid.UUID   `json:"id"`
	BatchID          uuid.UUID   `json:"batch_id"`
	PeriodID         uuid.UUID   `json:"period_id"`
	TemplateID       uuid.UUID   `json:"template_id"`
	VersionID        uuid.UUID   `json:"version_id"`
	FormCode         string      `json:"form_code"`
	DepartmentCode   string      `json:"department_code"`
	ProvinceCode     string      `json:"province_code"`
	CommuneCode      string      `json:"commune_code"`
	RegionCode       string      `json:"region_code"`
	Slot             int         `json:"slot"`
	PeriodBlock      bool        `json:"period_block"`
	PeriodLabel      string      `json:"period_label"`
	PeriodRange      string      `json:"period_range"`
	DueAt            string      `json:"due_at"`
	Status           string      `json:"status"`
	StatusLabel      string      `json:"status_label"`
	StoredStatus     string      `json:"stored_status"`
	Open             bool        `json:"open"`
	Assignees        []uuid.UUID `json:"assignees"`
	LatestVersion    int         `json:"latest_version"`
	SubmittedAt      *string     `json:"submitted_at"`
	SubmittedBy      *uuid.UUID  `json:"submitted_by"`
	OnBehalfOf       *uuid.UUID  `json:"on_behalf_of"`
	AdjustmentReason *string     `json:"adjustment_reason"`
	ConfirmedAt      *string     `json:"confirmed_at"`
	LastRemindedAt   *string     `json:"last_reminded_at"`
	ReopenedUntil    *string     `json:"reopened_until"`
	UpdatedAt        string      `json:"updated_at"`
}

func toTask(v app.TaskView) taskDTO {
	t := v.Task
	d := taskDTO{ID: t.ID, BatchID: t.BatchID, PeriodID: t.PeriodID, TemplateID: t.TemplateID, VersionID: t.VersionID, FormCode: t.FormCode,
		DepartmentCode: t.DepartmentCode, ProvinceCode: t.ProvinceCode, CommuneCode: t.CommuneCode, RegionCode: t.RegionCode, Slot: t.Slot,
		PeriodBlock: t.PeriodBlock, PeriodLabel: v.Period.Label, PeriodRange: v.Period.RangeLabel, DueAt: tss(t.DueAt), Status: v.Display,
		StatusLabel: domain.StatusLabel[v.Display], StoredStatus: t.Status, Open: v.Open, Assignees: t.Assignees, LatestVersion: t.LatestVersion,
		SubmittedAt: tsp(t.SubmittedAt), SubmittedBy: t.SubmittedBy, OnBehalfOf: t.OnBehalfOf, AdjustmentReason: t.AdjustmentReason,
		ConfirmedAt: tsp(t.ConfirmedAt), LastRemindedAt: tsp(t.LastRemindedAt), ReopenedUntil: tsp(t.ReopenedUntil), UpdatedAt: tss(t.UpdatedAt)}
	if d.Assignees == nil {
		d.Assignees = []uuid.UUID{}
	}
	if t.Status != domain.TaskAdjustmentRequested {
		d.AdjustmentReason = nil // lý do chỉ hiện khi đang "Yêu cầu điều chỉnh" (lịch sử ở Log)
	}
	return d
}

type subRowDTO struct {
	Key       string            `json:"key"`
	Indicator string            `json:"indicator,omitempty"`
	Input     map[string]string `json:"input"`
	Cells     map[string]string `json:"cells"`
}

type submissionDTO struct {
	ID                 uuid.UUID       `json:"id"`
	TaskID             uuid.UUID       `json:"task_id"`
	VersionNo          int             `json:"version_no"`
	FormVersionID      uuid.UUID       `json:"form_version_id"`
	Kind               string          `json:"kind"`
	Rows               []subRowDTO     `json:"rows"`
	Issues             json.RawMessage `json:"issues"`
	AdjustmentResponse bool            `json:"adjustment_response"`
	PreviousID         *uuid.UUID      `json:"previous_id"`
	OnBehalfOf         *uuid.UUID      `json:"on_behalf_of"`
	SupportSessionID   *uuid.UUID      `json:"support_session_id"`
	SupportReason      *string         `json:"support_reason"`
	EventTime          string          `json:"event_time"`
	RecordedAt         string          `json:"recorded_at"`
	ActorRef           uuid.UUID       `json:"actor_ref"`
	ActorDisplay       string          `json:"actor_display,omitempty"`
}

func toSubmission(s store.Submission, names map[uuid.UUID]string) submissionDTO {
	d := submissionDTO{ID: s.ID, TaskID: s.TaskID, VersionNo: s.VersionNo, FormVersionID: s.FormVersionID, Kind: s.Kind, Rows: []subRowDTO{},
		Issues: s.Issues, AdjustmentResponse: s.AdjustmentResponse, PreviousID: s.PreviousID, OnBehalfOf: s.OnBehalfOf, SupportSessionID: s.SupportSessionID,
		SupportReason: s.SupportReason, EventTime: tss(s.EventTime), RecordedAt: tss(s.RecordedAt), ActorRef: s.ActorRef, ActorDisplay: names[s.ActorRef]}
	if len(d.Issues) == 0 {
		d.Issues = json.RawMessage("[]")
	}
	for _, r := range s.Rows {
		d.Rows = append(d.Rows, subRowDTO(r))
	}
	return d
}

func issuesOf(is []repport.CellIssue) []repport.CellIssue {
	if is == nil {
		return []repport.CellIssue{}
	}
	return is
}

type logDTO struct {
	Event        string          `json:"event"`
	ActorRef     uuid.UUID       `json:"actor_ref"`
	ActorDisplay string          `json:"actor_display"`
	At           string          `json:"at"`
	Changes      []app.Change    `json:"changes"`
	Reason       *string         `json:"reason"`
	OnBehalfOf   *uuid.UUID      `json:"on_behalf_of"`
	Detail       json.RawMessage `json:"detail,omitempty"`
}

func toLog(es []app.LogEntry) []logDTO {
	out := make([]logDTO, 0, len(es))
	for _, e := range es {
		ch := e.Changes
		if ch == nil {
			ch = []app.Change{}
		}
		out = append(out, logDTO{Event: e.Event, ActorRef: e.ActorRef, ActorDisplay: e.ActorDisplay, At: tss(e.At), Changes: ch, Reason: e.Reason,
			OnBehalfOf: e.OnBehalfOf, Detail: e.Detail})
	}
	return out
}

type reminderDTO struct {
	ID             uuid.UUID   `json:"id"`
	BatchID        uuid.UUID   `json:"batch_id"`
	PeriodID       uuid.UUID   `json:"period_id"`
	TemplateID     *uuid.UUID  `json:"template_id"`
	Mode           string      `json:"mode"`
	Message        string      `json:"message"`
	Communes       []string    `json:"communes"`
	TaskIDs        []uuid.UUID `json:"task_ids"`
	RecipientCount int         `json:"recipient_count"`
	ActorRef       uuid.UUID   `json:"actor_ref"`
	ActorDisplay   string      `json:"actor_display"`
	At             string      `json:"at"`
}

func toReminder(r store.Reminder, names map[uuid.UUID]string) reminderDTO {
	return reminderDTO{ID: r.ID, BatchID: r.BatchID, PeriodID: r.PeriodID, TemplateID: r.TemplateID, Mode: r.Mode, Message: r.Message,
		Communes: r.Communes, TaskIDs: r.TaskIDs, RecipientCount: len(r.Recipients), ActorRef: r.ActorRef, ActorDisplay: names[r.ActorRef], At: tss(r.EventTime)}
}

type runDTO struct {
	ID               uuid.UUID         `json:"id"`
	RunNo            int               `json:"run_no"`
	PeriodID         uuid.UUID         `json:"period_id"`
	TemplateID       uuid.UUID         `json:"template_id"`
	VersionID        uuid.UUID         `json:"version_id"`
	FormCode         string            `json:"form_code"`
	Trigger          string            `json:"trigger"`
	ActorRef         uuid.UUID         `json:"actor_ref"`
	ActorDisplay     string            `json:"actor_display"`
	OnBehalfOf       *uuid.UUID        `json:"on_behalf_of"`
	At               string            `json:"at"`
	CommunesTotal    int               `json:"communes_total"`
	CommunesWithData int               `json:"communes_with_data"`
	CommunesChanged  int               `json:"communes_changed"`
	CellsChanged     int               `json:"cells_changed"`
	Provisional      bool              `json:"provisional"`
	MissingCommunes  []string          `json:"missing_communes"`
	Regions          []store.RegionRef `json:"regions"`
	PreviousRunID    *uuid.UUID        `json:"previous_run_id"`
}

func toRun(r store.Run, names map[uuid.UUID]string) runDTO {
	d := runDTO{ID: r.ID, RunNo: r.RunNo, PeriodID: r.PeriodID, TemplateID: r.TemplateID, VersionID: r.VersionID, FormCode: r.FormCode, Trigger: r.Trigger,
		ActorRef: r.ActorRef, ActorDisplay: names[r.ActorRef], OnBehalfOf: r.OnBehalfOf, At: tss(r.EventTime), CommunesTotal: r.CommunesTotal,
		CommunesWithData: r.CommunesWithData, CommunesChanged: r.CommunesChanged, CellsChanged: r.CellsChanged, Provisional: r.Provisional,
		MissingCommunes: r.MissingCommunes, Regions: r.Regions, PreviousRunID: r.PreviousRunID}
	if d.MissingCommunes == nil {
		d.MissingCommunes = []string{}
	}
	if d.Regions == nil {
		d.Regions = []store.RegionRef{}
	}
	return d
}

type changeDTO struct {
	ID           uuid.UUID  `json:"id"`
	CommuneCode  string     `json:"commune_code"`
	RowKey       string     `json:"row_key"`
	Letter       string     `json:"letter"`
	Old          *string    `json:"old"`
	New          *string    `json:"new"`
	Kind         string     `json:"kind"`
	SubmissionID *uuid.UUID `json:"submission_id"`
	ChangedBy    *uuid.UUID `json:"changed_by"`
	ChangedByDisplay string `json:"changed_by_display"`
	OnBehalfOf   *uuid.UUID `json:"on_behalf_of"`
	SubmittedAt  *string    `json:"submitted_at"`
}

func dec(m app.Metric) any {
	if !m.Has {
		return nil
	}
	return m.Value.String()
}

func decs(d *decimal.Decimal) *string {
	if d == nil {
		return nil
	}
	s := d.String()
	return &s
}

func metricDTO(m app.Metric) map[string]any {
	return map[string]any{"value": dec(m), "as_of": tsp(m.AsOf)}
}
