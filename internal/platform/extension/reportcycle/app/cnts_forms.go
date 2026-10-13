package app

// Biểu kỳ Cấp Xã CNTS (W11.42 — BRD CNTS v3.7 UR-XA-06…09, D4 CN2): cài biểu BM-01/04/05/17 vào cap/forms (profile
// kn_cnts), chia lịch kỳ (kỳ ngày, tuần mới W11.03 v2, tuần làm việc BM-05) với hạn riêng từng biểu và phát hành lô kỳ
// native qua formsport.Publisher; quy tắc chặn của biểu (formsport.Validator). Nộp, chốt, mở lại, hủy, sao chép kỳ
// trước, đồng bộ ngoại tuyến: /v1/forms (pf/forms).

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extension/reportcycle/domain"
	formsport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// CNTSFormView là một biểu kỳ CNTS kèm phiên bản đã cài ở cap/forms (nil = chưa cài).
type CNTSFormView struct {
	Form      domain.CNTSForm
	Installed *formsport.TemplateRef
	Current   bool // phiên bản đã cài có cùng định nghĩa trường với biểu chuẩn của gói
}

func (s *Service) cntsReady(ctx context.Context) error {
	if s.FormsNative == nil {
		return ErrDependency.WithDetail("chưa nối cap/forms (kỳ native)")
	}
	if _, _, err := authz.RequireKN(ctx); err != nil {
		return err
	}
	return nil
}

// CNTSForms liệt kê biểu kỳ Cấp Xã CNTS và phiên bản đã cài.
func (s *Service) CNTSForms(ctx context.Context) ([]CNTSFormView, error) {
	if err := s.cntsReady(ctx); err != nil {
		return nil, err
	}
	out := make([]CNTSFormView, 0, len(domain.CNTSForms))
	for _, f := range domain.CNTSForms {
		v := CNTSFormView{Form: f}
		ref, err := s.FormsNative.LatestTemplate(ctx, domain.ProfileCNTS, f.Code)
		switch {
		case errors.Is(err, formsport.ErrTemplateNotFound):
		case err != nil:
			return nil, err
		default:
			v.Installed = &ref
			v.Current = sameFields(ref.Fields, f.Fields)
		}
		out = append(out, v)
	}
	return out, nil
}

func sameFields(raw json.RawMessage, fs []domain.CNTSField) bool {
	var got []domain.CNTSField
	if json.Unmarshal(raw, &got) != nil {
		return false
	}
	a, _ := json.Marshal(got)
	b, _ := json.Marshal(fs)
	return string(a) == string(b)
}

// InstallCNTSForms cài (phiên bản mới) các biểu chưa có hoặc khác định nghĩa chuẩn của gói — Admin KN /
// reports_admin (define_by của profile kn_cnts). Biểu đã cài đúng định nghĩa giữ nguyên.
func (s *Service) InstallCNTSForms(ctx context.Context, idem string) ([]CNTSFormView, error) {
	views, err := s.CNTSForms(ctx)
	if err != nil {
		return nil, err
	}
	for i, v := range views {
		if v.Current {
			continue
		}
		raw, err := json.Marshal(v.Form.Fields)
		if err != nil {
			return nil, err
		}
		ref, err := s.FormsNative.DefineTemplate(ctx, formsport.TemplateDef{Profile: domain.ProfileCNTS, Code: v.Form.Code,
			Title: v.Form.BM + " — " + v.Form.Title, Fields: raw}, idemOr(idem, v.Form.Code))
		if err != nil {
			return nil, err
		}
		views[i].Installed, views[i].Current = &ref, true
	}
	return views, nil
}

// CNTSSeriesInput là yêu cầu chia/phát hành một lô kỳ của một biểu.
type CNTSSeriesInput struct {
	Code     string
	From, To string // YYYY-MM-DD
	Units    []string
	// Deadline: hạn khác mặc định của biểu (người phát hành chọn — W11.03). nil = mặc định.
	Deadline *domain.Deadline
}

// CNTSSeries là kết quả chia kỳ (xem trước hoặc đã phát hành).
type CNTSSeries struct {
	Form      domain.CNTSForm
	Periods   []domain.CNTSPeriod
	Units     []string
	Tasks     int                         // số công việc sẽ sinh = kỳ × đơn vị
	Calendar  *domain.Calendar            // lịch tuần của phòng (D8); nil với tuần làm việc BM-05 (không theo lịch tuần)
	Published []formsport.PublishedPeriod // sau khi phát hành
}

// cntsCalendar: lịch tuần của lô CNTS (D8 — Thứ Ba–Thứ Hai, domain.CNTSRule nạp sẵn, hiệu lực ngay; tham số
// extension.report_period_rule:chan_nuoi_thuy_san đè lên). Tham số theo phòng là quy tắc v1 thì vẫn dùng CNTSRule
// (lịch kỳ CNTS cần kỳ ngày/tuần v2).
func (s *Service) cntsCalendar(ctx context.Context, from time.Time) domain.Calendar {
	if c := s.CalendarAt(ctx, domain.DeptChanNuoiThuySan, from); c.Rule.Version == domain.RuleVersionV2 {
		return c
	}
	return domain.Calendar{ID: domain.DeptChanNuoiThuySan, Version: domain.CalendarVersionCNTS, Rule: domain.CNTSRule, Source: domain.CalendarSourceBuiltin}
}

// PreviewCNTSSeries chia kỳ cho một biểu (chưa ghi).
func (s *Service) PreviewCNTSSeries(ctx context.Context, in CNTSSeriesInput) (CNTSSeries, error) {
	if err := s.cntsReady(ctx); err != nil {
		return CNTSSeries{}, err
	}
	f, ok := domain.CNTSFormByCode(in.Code)
	if !ok {
		return CNTSSeries{}, problem.Validation.WithField("code", "biểu kỳ CNTS không có")
	}
	from, err := domain.ParseDate(in.From)
	if err != nil {
		return CNTSSeries{}, problem.Validation.WithField("from_date", err.Error())
	}
	to, err := domain.ParseDate(in.To)
	if err != nil {
		return CNTSSeries{}, problem.Validation.WithField("to_date", err.Error())
	}
	if to.Sub(from) > 366*24*time.Hour {
		return CNTSSeries{}, problem.Validation.WithField("to_date", "tối đa một năm mỗi lô")
	}
	cal := s.cntsCalendar(ctx, from)
	ps, err := domain.SplitCNTS(domain.CNTSSeriesInput{Form: f, From: from, To: to, Rule: cal.Rule, Deadline: in.Deadline, Loc: clock.Zone()})
	if err != nil {
		var pe domain.ErrPeriod
		if errors.As(err, &pe) {
			return CNTSSeries{}, problem.Validation.WithField(pe.Field, pe.Msg)
		}
		return CNTSSeries{}, err
	}
	if len(in.Units) == 0 {
		return CNTSSeries{}, problem.Validation.WithField("units", "chọn ít nhất một đơn vị nhận")
	}
	sr := CNTSSeries{Form: f, Periods: ps, Units: in.Units, Tasks: len(ps) * len(in.Units)}
	if !f.IsWorkWeek() {
		sr.Calendar = &cal
	}
	return sr, nil
}

// PublishCNTSSeries chia kỳ và phát hành lô kỳ native (một giao dịch cap/forms). Quyền, phạm vi đơn vị, cơ sở thuộc
// xã do cap/forms kiểm (cấp publish_by của kn_cnts, danh mục cơ sở giết mổ).
func (s *Service) PublishCNTSSeries(ctx context.Context, in CNTSSeriesInput, idem string) (CNTSSeries, error) {
	sr, err := s.PreviewCNTSSeries(ctx, in)
	if err != nil {
		return sr, err
	}
	ref, err := s.FormsNative.LatestTemplate(ctx, domain.ProfileCNTS, sr.Form.Code)
	if errors.Is(err, formsport.ErrTemplateNotFound) {
		return sr, formsport.ErrTemplateNotFound.WithDetail("biểu " + sr.Form.BM + " chưa cài — Admin cài biểu kỳ CNTS trước")
	}
	if err != nil {
		return sr, err
	}
	bi := formsport.BatchInput{Profile: domain.ProfileCNTS, TemplateID: ref.ID, UnitKind: sr.Form.UnitKind, Units: sr.Units}
	for _, p := range sr.Periods {
		bi.Periods = append(bi.Periods, formsport.PeriodDef{Label: p.Label, DueAt: p.DueAt, Kind: p.Kind, Key: p.Key, From: p.From, To: p.To, Rule: p.Rule})
	}
	if sr.Published, err = s.FormsNative.PublishBatch(ctx, bi, idem); err != nil {
		return sr, err
	}
	return sr, nil
}

// CNTSValidator cài formsport.Validator cho profile kn_cnts (quy tắc chặn của biểu kỳ CNTS).
type CNTSValidator struct{}

var _ formsport.Validator = CNTSValidator{}

// Validate thực hiện formsport.Validator.
func (CNTSValidator) Validate(_ context.Context, code string, values map[string]string) []formsport.Issue {
	var out []formsport.Issue
	for _, is := range domain.ValidateCNTS(code, values) {
		out = append(out, formsport.Issue{Field: is.Field, Reason: is.Reason})
	}
	return out
}
