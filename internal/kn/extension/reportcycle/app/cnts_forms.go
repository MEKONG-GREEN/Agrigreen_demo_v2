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

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/reportcycle/domain"
	formsport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/port"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
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
	if _, _, err := port.RequireSession(ctx); err != nil {
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

// InstallCNTSForms cài các biểu chưa có — Admin KN (chính sách định nghĩa kn_cnts, #192). Mẫu KNQG (BR-02) đã cài khác
// cấu trúc chuẩn thì cài lại đúng chuẩn; biểu Cấp Tỉnh được sửa (BM-17) đã cài thì giữ phiên bản Cấp Tỉnh đã lưu (FN-07),
// không ghi đè bằng cấu trúc mã nguồn. Biểu đã cài đúng định nghĩa giữ nguyên.
func (s *Service) InstallCNTSForms(ctx context.Context, idem string) ([]CNTSFormView, error) {
	views, err := s.CNTSForms(ctx)
	if err != nil {
		return nil, err
	}
	for i, v := range views {
		if v.Current || (v.Installed != nil && !domain.CNTSLocked(v.Form.Code)) {
			continue
		}
		raw, err := json.Marshal(v.Form.Fields)
		if err != nil {
			return nil, err
		}
		var base int64
		if v.Installed != nil {
			base = v.Installed.Version
		}
		ref, err := s.FormsNative.DefineTemplate(ctx, formsport.TemplateDef{Profile: domain.ProfileCNTS, Code: v.Form.Code,
			Title: v.Form.BM + " — " + v.Form.Title, Fields: raw, BaseVersion: &base}, idemOr(idem, v.Form.Code))
		if err != nil {
			return nil, err
		}
		views[i].Installed, views[i].Current = &ref, true
	}
	return views, nil
}

// CNTSSeriesInput là yêu cầu chia/phát hành một lô kỳ của một biểu.
type CNTSSeriesInput struct {
	// Publication pins the exact immutable catalog identity selected by the caller.
	TemplateID      uuid.UUID
	TemplateVersion int64
	Code            string
	From, To        string // YYYY-MM-DD
	// Units: mã xã (Target communes) hoặc facility_ref cơ sở giết mổ (BM-01). Với nhóm xã (Target province/regions/
	// aquaculture/model) máy chủ tự giải danh sách xã — Units bỏ qua.
	Units []string
	// Deadline: hạn khác mặc định của biểu (người phát hành chọn — W11.03). nil = mặc định.
	Deadline *domain.Deadline
	// BF-CN-PUBLISH (CN-07): đối tượng (rỗng = communes), khu vực (Target regions), cán bộ tỉnh phụ trách, giờ chốt =
	// hạn nộp + CloseAfterHours giờ (nil = Cấp Tỉnh tự chốt, không đặt giờ).
	Target          string
	Regions         []string
	OwnerRef        *uuid.UUID
	CloseAfterHours *int
}

// CNTSSeries là kết quả chia kỳ (xem trước hoặc đã phát hành).
type CNTSSeries struct {
	Template  *formsport.TemplateRef
	Form      domain.CNTSForm
	Periods   []domain.CNTSPeriod
	Units     []string
	Tasks     int                         // số công việc sẽ sinh = kỳ × đơn vị
	Calendar  *domain.Calendar            // lịch tuần của phòng (D8); nil với tuần làm việc BM-05 (không theo lịch tuần)
	Published []formsport.PublishedPeriod // sau khi phát hành
	// BF-CN-PUBLISH: đối tượng đã giải, người phụ trách, giờ chốt, xã của tỉnh (kèm khu vực — để chọn), lần phát hành.
	Target          string
	Regions         []string
	OwnerRef        *uuid.UUID
	CloseAfterHours *int
	Communes        []partyport.CommuneRegion
	PublicationID   *uuid.UUID
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
	if len(in.Units) > 2000 || len(in.Regions) > 100 || len(in.Code) > 64 {
		return CNTSSeries{}, problem.Validation.WithDetail("lô vượt giới hạn đơn vị, khu vực hoặc mã biểu")
	}
	for _, xs := range [][]string{in.Units, in.Regions} {
		for _, v := range xs {
			if len(v) > 64 {
				return CNTSSeries{}, problem.Validation.WithField("units", "mã tối đa 64 byte")
			}
		}
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
	sr := CNTSSeries{Form: f, Periods: ps, OwnerRef: in.OwnerRef, CloseAfterHours: in.CloseAfterHours}
	if in.TemplateID != uuid.Nil || in.TemplateVersion != 0 {
		selected, err := s.selectedCNTSTemplate(ctx, in)
		if err != nil {
			return sr, err
		}
		sr.Template = &selected
		fields, err := decodeFields(selected.Fields)
		if err != nil {
			return sr, err
		}
		sr.Form.Fields, sr.Form.Title = fields, selected.Title
	}
	if err := s.cntsTarget(ctx, in, &sr); err != nil {
		return sr, err
	}
	if len(sr.Units) == 0 {
		return sr, problem.Validation.WithField("units", "chọn ít nhất một đơn vị nhận")
	}
	sr.Tasks = len(ps) * len(sr.Units)
	if !f.IsWorkWeek() {
		sr.Calendar = &cal
	}
	return sr, nil
}

// PublishCNTSSeries chia kỳ và phát hành lô kỳ native (một giao dịch cap/forms). Quyền, phạm vi đơn vị, cơ sở thuộc
// xã do cap/forms kiểm (cấp publish_by của kn_cnts, danh mục cơ sở giết mổ).
func (s *Service) PublishCNTSSeries(ctx context.Context, in CNTSSeriesInput, idem string) (CNTSSeries, error) {
	return s.publishCNTS275(ctx, in, idem)
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
