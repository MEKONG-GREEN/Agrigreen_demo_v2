package app

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extension/reportcycle/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extension/reportcycle/store"
	formsport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/db"
)

// Ghi song song sang cap/forms (W6.2, nguyên tắc 3 của kế hoạch v4.1): route v1 của vòng đời báo cáo KN giữ nguyên
// sổ pf_extension.* và hành vi; mỗi lần chốt (thủ công, tự động, chốt lại) và mở lại kỳ còn ghi sự kiện origin =
// mirror vào pf_forms.period_event — chốt kèm ẢNH CHỤP ĐÓNG BĂNG (phiên bản bài nộp đang tính của từng xã × biểu,
// trạng thái, lần tổng hợp) băm SHA-256 ở DB — CÙNG GIAO DỊCH (R-21; lỗi ghi song song làm hỏng thao tác cũ).
// Freezer nil → không ghi (kiểm thử cũ, lắp ráp chưa nối).
//
// W11.05 (đơn vị lá tổng quát): ảnh chụp ghi loại đơn vị lá (unit_kind = "commune" — đơn vị của vòng đời KN v7.7 là
// xã) và khóa kỳ theo lịch kỳ hệ thống (period_key, tính lại từ quy tắc chia kỳ đã lưu ở đợt — W11.03; rỗng với
// vụ/đột xuất); thu hồi đợt ghi sự kiện 'cancelled' (hủy kỳ) cho mọi kỳ của đợt.

// FormsProfile là tên cấu hình bên dùng KN ở cap/forms.
const FormsProfile = "kn"

// mirrorTask là một dòng ảnh chụp KN.
type mirrorTask struct {
	TaskID       string  `json:"task_id"`
	Commune      string  `json:"commune"`
	Unit         string  `json:"unit"` // = Commune (đơn vị lá loại commune)
	TemplateID   string  `json:"template_id"`
	FormCode     string  `json:"form_code"`
	VersionID    string  `json:"form_version_id"`
	Status       string  `json:"status"`
	SubmissionID *string `json:"submission_id"`
	ReopenedTill *string `json:"reopened_until"`
}

// mirrorContent là nội dung ảnh chụp KN (schema kn.report_period/v1).
type mirrorContent struct {
	Schema     string       `json:"schema"`
	PeriodID   string       `json:"period_id"`
	BatchID    string       `json:"batch_id"`
	Label      string       `json:"label"`
	UnitKind   string       `json:"unit_kind"`
	PeriodType string       `json:"period_type"`
	PeriodKey  string       `json:"period_key"`
	PeriodRule string       `json:"period_rule"`
	Department string       `json:"department_code"`
	Province   string       `json:"province_code"`
	Action     string       `json:"action"`
	Tasks      []mirrorTask `json:"tasks"`
	Runs       []string     `json:"consolidation_runs"`
}

// mirrorForms ghi chốt (closed) hoặc mở lại (reopened) của kỳ p sang cap/forms trong giao dịch tx.
func (s *Service) mirrorForms(ctx context.Context, tx pgx.Tx, p store.Period, action, mode string, communes []string, reason string,
	actor uuid.UUID, idem string, now time.Time, runs []store.Run) error {
	if s.Freezer == nil {
		return nil
	}
	in := formsport.MirrorInput{Profile: FormsProfile, PeriodRef: p.ID, Label: p.Label, Action: action, Mode: mode, Units: communes,
		Reason: reason, Actor: actor, At: now, IdemKey: "kn:" + idem}
	if action == formsport.ActionClosed {
		tasks, err := store.TasksOfPeriod(ctx, tx, p.ID, nil, false)
		if err != nil {
			return err
		}
		c := mirrorContent{Schema: "kn.report_period/v1", PeriodID: p.ID.String(), BatchID: p.BatchID.String(), Label: p.Label,
			UnitKind: formsport.UnitCommune, Department: p.DepartmentCode, Province: p.ProvinceCode, Action: action, Tasks: []mirrorTask{}, Runs: []string{}}
		b, err := store.BatchByID(ctx, tx, p.BatchID, false)
		if err != nil {
			return err
		}
		if b != nil {
			c.PeriodType, c.PeriodRule, c.PeriodKey = b.PeriodType, b.PeriodRule, periodKey(*b, p)
		}
		for _, t := range tasks {
			mt := mirrorTask{TaskID: t.ID.String(), Commune: t.CommuneCode, Unit: t.CommuneCode, TemplateID: t.TemplateID.String(), FormCode: t.FormCode,
				VersionID: t.VersionID.String(), Status: t.Status, SubmissionID: ptrString(t.SubmittedSubmissionID)}
			if t.ReopenedUntil != nil {
				v := ts(*t.ReopenedUntil)
				mt.ReopenedTill = &v
			}
			c.Tasks = append(c.Tasks, mt)
		}
		for _, r := range runs {
			c.Runs = append(c.Runs, r.ID.String())
		}
		raw, err := json.Marshal(c)
		if err != nil {
			return err
		}
		in.Content = raw
	}
	_, err := s.Freezer.Record(db.JoinTx(ctx, tx), in)
	return err
}

// periodKey trả khóa kỳ theo lịch kỳ hệ thống của kỳ p (domain.Span.Key — "2026-W41", "2026-10", "2026-Q4"…) tính từ
// quy tắc chia kỳ lưu ở đợt; vụ/đột xuất hoặc quy tắc không đọc được → rỗng. Không tra tham số KSGM: đợt chưa có kỳ
// ksgm_week (pf_extension.report_batch.period_type chưa nhận — cần migration của extension).
func periodKey(b store.Batch, p store.Period) string {
	r, err := domain.ParseRule(b.PeriodRule)
	if err != nil {
		return ""
	}
	sp, err := r.PeriodOf(b.PeriodType, p.From, nil)
	if err != nil {
		return ""
	}
	return sp.Key
}

// mirrorWithdrawn ghi hủy kỳ (cancelled) sang cap/forms cho mọi kỳ của đợt bị thu hồi, trong giao dịch tx.
func (s *Service) mirrorWithdrawn(ctx context.Context, tx pgx.Tx, batchID uuid.UUID, reason string, actor uuid.UUID, idem string, now time.Time) error {
	if s.Freezer == nil {
		return nil
	}
	ps, err := store.PeriodsOfBatch(ctx, tx, batchID)
	if err != nil {
		return err
	}
	for _, p := range ps {
		in := formsport.MirrorInput{Profile: FormsProfile, PeriodRef: p.ID, Label: p.Label, Action: formsport.ActionCancelled, Mode: formsport.ModeManual,
			Reason: reason, Actor: actor, At: now, IdemKey: "kn:" + idem + ":" + p.ID.String()}
		if _, err := s.Freezer.Record(db.JoinTx(ctx, tx), in); err != nil {
			return err
		}
	}
	return nil
}
