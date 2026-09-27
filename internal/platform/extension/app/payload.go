package app

import (
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extension/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// ReportPayload là thân JSON của báo cáo hiện trường — dùng chung cho API và bản
// ghi đồng bộ field_report (docs/dev/sync-contract.md). Số là chuỗi decimal (R-10).
type ReportPayload struct {
	Kind           string  `json:"kind"`
	CommuneCode    string  `json:"commune_code"`
	CooperativeRef *string `json:"cooperative_ref"`
	Period         string  `json:"period"`
	SeasonCode     string  `json:"season_code"`
	VarietyCode    string  `json:"variety_code"`
	SownAreaHa     *string `json:"sown_area_ha"`
	SownOn         *string `json:"sown_on"`
	GrowthStage    string  `json:"growth_stage"`
	PestCode       string  `json:"pest_code"`
	Severity       string  `json:"severity"`
	AffectedAreaHa *string `json:"affected_area_ha"`
	Urgent         bool    `json:"urgent"`
	Note           string  `json:"note"`
}

// ToReport chuyển thân JSON sang domain.Report; lỗi định dạng trả problem 422 theo trường.
func (b ReportPayload) ToReport() (domain.Report, error) {
	r := domain.Report{
		Kind: strings.TrimSpace(b.Kind), CommuneCode: b.CommuneCode, Period: strings.TrimSpace(b.Period),
		SeasonCode: strings.TrimSpace(b.SeasonCode), VarietyCode: strings.TrimSpace(b.VarietyCode),
		GrowthStage: strings.TrimSpace(b.GrowthStage), PestCode: strings.TrimSpace(b.PestCode),
		Severity: strings.TrimSpace(b.Severity), Urgent: b.Urgent, Note: b.Note,
	}
	v, bad := problem.Validation, false
	if b.CooperativeRef != nil && *b.CooperativeRef != "" {
		if ref, err := ids.ParseRef(*b.CooperativeRef); err != nil {
			v, bad = v.WithField("cooperative_ref", "phải là UUID"), true
		} else {
			r.CooperativeRef = ref
		}
	}
	dec := func(field string, s *string) *decimal.Decimal {
		if s == nil || *s == "" {
			return nil
		}
		d, err := decimal.NewFromString(*s)
		if err != nil {
			v, bad = v.WithField(field, "phải là số thập phân dạng chuỗi"), true
			return nil
		}
		return &d
	}
	r.SownAreaHa = dec("sown_area_ha", b.SownAreaHa)
	r.AffectedAreaHa = dec("affected_area_ha", b.AffectedAreaHa)
	if b.SownOn != nil && *b.SownOn != "" {
		t, err := time.Parse(time.DateOnly, *b.SownOn)
		if err != nil {
			v, bad = v.WithField("sown_on", "định dạng YYYY-MM-DD"), true
		} else {
			r.SownOn = &t
		}
	}
	if bad {
		return r, v
	}
	return r, nil
}

// TaskUpdatePayload là thân bản ghi đồng bộ task_update.
type TaskUpdatePayload struct {
	TaskID      string  `json:"task_id"`
	Action      string  `json:"action"`
	Note        string  `json:"note"`
	AssigneeRef *string `json:"assignee_ref"`
}

// ToInput kiểm và chuyển sang UpdateInput (chưa có IdemKey/DeviceID).
func (b TaskUpdatePayload) ToInput() (UpdateInput, error) {
	var in UpdateInput
	v, bad := problem.Validation, false
	id, err := uuid.Parse(b.TaskID)
	if err != nil {
		v, bad = v.WithField("task_id", "phải là UUID"), true
	}
	in.TaskID = id
	a, ok := domain.ParseAction(b.Action)
	if !ok {
		v, bad = v.WithField("action", "assigned, accepted, progress, resolved, escalated hoặc reopened"), true
	}
	in.Action = a
	in.Note = b.Note
	if b.AssigneeRef != nil && *b.AssigneeRef != "" {
		ref, err := ids.ParseRef(*b.AssigneeRef)
		if err != nil {
			v, bad = v.WithField("assignee_ref", "phải là UUID"), true
		}
		in.AssigneeRef = ref
	}
	if bad {
		return in, v
	}
	return in, nil
}
