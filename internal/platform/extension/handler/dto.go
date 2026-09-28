package handler

import (
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extension/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extension/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extension/store"
)

func ref(u uuid.UUID) *string {
	if u == uuid.Nil {
		return nil
	}
	s := u.String()
	return &s
}

func ts(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }

func tsp(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := ts(*t)
	return &s
}

func decp(d *decimal.Decimal) *string {
	if d == nil {
		return nil
	}
	s := d.String()
	return &s
}

type taskDTO struct {
	TaskID           string  `json:"task_id"`
	CommuneCode      string  `json:"commune_code"`
	ProvinceCode     string  `json:"province_code"`
	CooperativeRef   *string `json:"cooperative_ref"`
	HouseholdRef     *string `json:"household_ref"`
	PlotRef          *string `json:"plot_ref"`
	Category         string  `json:"category"`
	Description      string  `json:"description"`
	Priority         string  `json:"priority"`
	Source           string  `json:"source"`
	Status           string  `json:"status"`
	AssigneeRef      *string `json:"assignee_ref"`
	CreatedBy        string  `json:"created_by"`
	CreatedAt        string  `json:"created_at"`
	SLAHours         string  `json:"sla_hours"`
	DueAt            string  `json:"due_at"`
	Overdue          bool    `json:"overdue"`
	EscalatedAt      *string `json:"escalated_at"`
	EscalatedUnit    *string `json:"escalated_unit"`
	EscalationOrgRef *string `json:"escalation_org_ref"`
	ResolvedAt       *string `json:"resolved_at"`
	LastNote         *string `json:"last_note"`
	Version          int64   `json:"version"`
	// Tên hiển thị (G-09): cán bộ được giao (danh bạ trực), HTX (pf/party), xã (pf/masterdata).
	// null khi chưa tra được — web hiện mã.
	AssigneeName    *string `json:"assignee_name"`
	CooperativeName *string `json:"cooperative_name"`
	CommuneName     *string `json:"commune_name"`
}

// withNames điền tên hiển thị G-09 vào DTO nhiệm vụ.
func withNames(d taskDTO, t domain.Task, n app.TaskNames) taskDTO {
	d.AssigneeName = opt(n.Assignees[t.AssigneeRef])
	d.CooperativeName = opt(n.Cooperatives[t.CooperativeRef])
	d.CommuneName = opt(n.Communes[t.CommuneCode])
	return d
}

func toTask(t domain.Task, now time.Time) taskDTO {
	d := taskDTO{
		TaskID: t.ID.String(), CommuneCode: t.CommuneCode, ProvinceCode: t.ProvinceCode,
		CooperativeRef: ref(t.CooperativeRef), HouseholdRef: ref(t.HouseholdRef), PlotRef: ref(t.PlotRef),
		Category: t.Category, Description: t.Description, Priority: t.Priority, Source: t.Source, Status: string(t.Status),
		AssigneeRef: ref(t.AssigneeRef), CreatedBy: t.CreatedBy.String(), CreatedAt: ts(t.CreatedAt),
		SLAHours: t.SLAHours.String(), DueAt: ts(t.DueAt), Overdue: t.Overdue(now), EscalatedAt: tsp(t.EscalatedAt),
		EscalationOrgRef: ref(t.EscalationOrgRef), ResolvedAt: tsp(t.ResolvedAt), Version: t.LastSeq,
	}
	if t.EscalatedAt != nil && t.EscalatedUnit != "" {
		u := t.EscalatedUnit
		d.EscalatedUnit = &u
	}
	if t.LastNote != "" {
		n := t.LastNote
		d.LastNote = &n
	}
	return d
}

type eventDTO struct {
	EventID     string  `json:"event_id"`
	Action      string  `json:"action"`
	StatusAfter string  `json:"status_after"`
	AssigneeRef *string `json:"assignee_ref"`
	DueAt       string  `json:"due_at"`
	Note        *string `json:"note"`
	ActorRef    string  `json:"actor_ref"`
	EventTime   string  `json:"event_time"`
	RecordedAt  string  `json:"recorded_at"`
	Seq         int64   `json:"seq"`
}

func toEvent(e domain.Event) eventDTO {
	d := eventDTO{
		EventID: e.ID.String(), Action: string(e.Action), StatusAfter: string(e.StatusAfter), AssigneeRef: ref(e.AssigneeRef),
		DueAt: ts(e.DueAt), ActorRef: e.ActorRef.String(), EventTime: ts(e.EventTime), RecordedAt: ts(e.RecordedAt), Seq: e.Seq,
	}
	if e.Note != "" {
		n := e.Note
		d.Note = &n
	}
	return d
}

type directiveDTO struct {
	DirectiveID   string   `json:"directive_id"`
	Title         string   `json:"title"`
	Body          string   `json:"body"`
	Severity      string   `json:"severity"`
	AudienceUnits []string `json:"audience_units"`
	ProvinceCodes []string `json:"province_codes"`
	IssuerRef     string   `json:"issuer_ref"`
	IssuedAt      string   `json:"issued_at"`
	ExpiresAt     *string  `json:"expires_at"`
	TargetCount   int      `json:"target_count"`
	AckCount      int      `json:"ack_count"`
	AckRate       *string  `json:"ack_rate"`
	IsTarget      bool     `json:"is_target"`
	AckedAt       *string  `json:"acked_at"`
}

func toDirective(d domain.Directive) directiveDTO {
	return directiveDTO{
		DirectiveID: d.ID.String(), Title: d.Title, Body: d.Body, Severity: d.Severity, AudienceUnits: nonNil(d.AudienceUnits),
		ProvinceCodes: nonNil(d.ProvinceCodes), IssuerRef: d.IssuerRef.String(), IssuedAt: ts(d.IssuedAt),
		ExpiresAt: tsp(d.ExpiresAt), TargetCount: d.TargetCount, AckCount: d.AckCount,
		AckRate: decp(domain.AckRate(int64(d.AckCount), int64(d.TargetCount))), IsTarget: d.IsTarget, AckedAt: tsp(d.AckedByMe),
	}
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

type targetDTO struct {
	ActorRef     string `json:"actor_ref"`
	UnitCode     string `json:"unit_code"`
	ProvinceCode string `json:"province_code"`
}

func toTargets(ts []store.Target) []targetDTO {
	out := make([]targetDTO, 0, len(ts))
	for _, t := range ts {
		out = append(out, targetDTO{ActorRef: t.ActorRef.String(), UnitCode: t.UnitCode, ProvinceCode: t.ProvinceCode})
	}
	return out
}

type reportDTO struct {
	ID               string  `json:"id"`
	Kind             string  `json:"kind"`
	CommuneCode      string  `json:"commune_code"`
	ProvinceCode     string  `json:"province_code"`
	CooperativeRef   *string `json:"cooperative_ref"`
	Period           string  `json:"period"`
	SeasonCode       *string `json:"season_code"`
	VarietyCode      *string `json:"variety_code"`
	SownAreaHa       *string `json:"sown_area_ha"`
	SownOn           *string `json:"sown_on"`
	GrowthStage      *string `json:"growth_stage"`
	PestCode         *string `json:"pest_code"`
	Severity         *string `json:"severity"`
	AffectedAreaHa   *string `json:"affected_area_ha"`
	Urgent           bool    `json:"urgent"`
	Note             *string `json:"note"`
	Version          int     `json:"version"`
	PreviousID       *string `json:"previous_id"`
	SupersedesID     *string `json:"supersedes_id"`
	CorrectionReason *string `json:"correction_reason"`
	ActorRef         string  `json:"actor_ref"`
	EventTime        string  `json:"event_time"`
	RecordedAt       string  `json:"recorded_at"`
	Seq              int64   `json:"seq"`
}

func opt(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func toReport(r domain.Report) reportDTO {
	d := reportDTO{
		ID: r.ID.String(), Kind: r.Kind, CommuneCode: r.CommuneCode, ProvinceCode: r.ProvinceCode,
		CooperativeRef: ref(r.CooperativeRef), Period: r.Period, SeasonCode: opt(r.SeasonCode), VarietyCode: opt(r.VarietyCode),
		SownAreaHa: decp(r.SownAreaHa), GrowthStage: opt(r.GrowthStage), PestCode: opt(r.PestCode), Severity: opt(r.Severity),
		AffectedAreaHa: decp(r.AffectedAreaHa), Urgent: r.Urgent, Note: opt(r.Note), Version: r.Version,
		PreviousID: ref(r.PreviousID), CorrectionReason: r.CorrectionReason, ActorRef: r.ActorRef.String(),
		EventTime: ts(r.EventTime), RecordedAt: ts(r.RecordedAt), Seq: r.Seq,
	}
	if r.SownOn != nil {
		s := r.SownOn.Format(time.DateOnly)
		d.SownOn = &s
	}
	if r.SupersedesID != nil {
		d.SupersedesID = ref(*r.SupersedesID)
	}
	return d
}

type kpiDTO struct {
	SownAreaHa       string   `json:"sown_area_ha"`
	Cooperatives     int64    `json:"cooperatives_reporting"`
	OpenTasks        int64    `json:"open_tasks"`
	OverdueTasks     int64    `json:"overdue_tasks"`
	DirectiveTargets int64    `json:"directive_targets"`
	DirectiveAcks    int64    `json:"directive_acks"`
	DirectiveAckRate *string  `json:"directive_ack_rate"`
	ExpectedCommunes int64    `json:"expected_communes"`
	ReportedCommunes int64    `json:"reported_communes"`
	MissingCommunes  []string `json:"missing_communes"`
	Provisional      bool     `json:"provisional"`
	ProvisionalLabel *string  `json:"provisional_label"`
}

func toKPI(k app.KPI) kpiDTO {
	d := kpiDTO{
		SownAreaHa: k.SownAreaHa.String(), Cooperatives: k.Cooperatives, OpenTasks: k.OpenTasks, OverdueTasks: k.OverdueTasks,
		DirectiveTargets: k.DirectiveTargets, DirectiveAcks: k.DirectiveAcks, DirectiveAckRate: decp(k.DirectiveAckRate),
		ExpectedCommunes: k.ExpectedCommunes, ReportedCommunes: k.ReportedCommunes, MissingCommunes: nonNil(k.MissingCommunes),
		Provisional: k.Provisional,
	}
	if k.Provisional {
		l := "Số liệu tạm tính"
		d.ProvisionalLabel = &l
	}
	return d
}

type staffDTO struct {
	ActorRef     string `json:"actor_ref"`
	UnitCode     string `json:"unit_code"`
	ProvinceCode string `json:"province_code"`
	RoleTitle    string `json:"role_title"`
	DisplayName  string `json:"display_name"`
	Active       bool   `json:"active"`
	UpdatedAt    string `json:"updated_at"`
}

func toStaff(s domain.Staff) staffDTO {
	return staffDTO{
		ActorRef: s.ActorRef.String(), UnitCode: s.UnitCode, ProvinceCode: s.ProvinceCode, RoleTitle: s.RoleTitle,
		DisplayName: s.DisplayName, Active: s.Active, UpdatedAt: ts(s.UpdatedAt),
	}
}

type rosterDTO struct {
	staffDTO
	DutyStatus string  `json:"duty_status"`
	Source     string  `json:"status_source"`
	Until      *string `json:"until"`
}

func toRoster(es []app.RosterEntry) []rosterDTO {
	out := make([]rosterDTO, 0, len(es))
	for _, e := range es {
		out = append(out, rosterDTO{staffDTO: toStaff(e.Staff), DutyStatus: e.Status, Source: e.Source, Until: tsp(e.Until)})
	}
	return out
}
