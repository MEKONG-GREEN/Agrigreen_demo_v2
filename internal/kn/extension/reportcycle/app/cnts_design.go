package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/reportcycle/domain"
	accport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/port"
	formsport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/port"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tabular"
)

const MaxDesignImportBytes = 2 << 20

// Loại biểu trên màn thiết kế (UR-TINH-07 "loại"): biểu kỳ báo cáo của phòng và biểu điều tra của FN-21.
const (
	DesignKindReport = "report"
	DesignKindSurvey = "survey"
)

var (
	errDesignLocked = problem.Forbidden.WithDetail("Mẫu cố định theo TTKN Quốc gia — không sửa cấu trúc (FN-07 BR-02); sao chép thành biểu mới nếu cần")
	errDesignWriter = problem.Forbidden.WithDetail("Chỉ cán bộ tổng hợp Cấp Tỉnh Phòng Chăn nuôi – Thủy sản hoặc Admin được lưu phiên bản biểu")
)

type CNTSDesignView struct {
	Code       string             `json:"code"`
	BM         string             `json:"bm"`
	Title      string             `json:"title"`
	Kind       string             `json:"kind"`
	PeriodType string             `json:"period_type"`
	Due        string             `json:"due"`
	Locked     bool               `json:"locked"`
	CanEdit    bool               `json:"can_edit"`
	TemplateID *string            `json:"template_id"`
	Version    *int64             `json:"version"`
	Fields     []domain.CNTSField `json:"fields"`
}

// CNTSDesignVersion là một phiên bản đã lưu (lịch sử — FN-07 B2 "phát hành phiên bản").
type CNTSDesignVersion struct {
	TemplateID string             `json:"template_id"`
	Version    int64              `json:"version"`
	Title      string             `json:"title"`
	Fields     []domain.CNTSField `json:"fields"`
}

func designReader(ctx context.Context) (port.SessionContext, error) {
	_, k, err := port.RequireSession(ctx, port.SessionRoleAdmin, port.SessionRoleProvince)
	if err != nil {
		return k, err
	}
	if k.Role != port.SessionRoleAdmin && !k.AllowsDepartment(partyport.DeptChanNuoiThuySan) {
		return k, problem.Forbidden.WithDetail("Thiết kế biểu mẫu thuộc phòng Chăn nuôi – Thủy sản")
	}
	return k, nil
}

// designWriter: ma trận FN-07 (CNTS v4.0 mục VI) — Admin CRUD; Cấp Tỉnh Phòng CNTS nhóm "Cán bộ tổng hợp" CRU (thiết kế,
// phát hành); viên chức, lãnh đạo phòng, Ban Giám đốc, chỉ xem, Cấp Quốc gia chỉ đọc.
// Nhóm của lần gán phải tường minh; Cấp Tỉnh chỉ lưu BM-17 và biểu điều tra.
func designWriter(k port.SessionContext, code string) bool {
	if k.ReadOnly() {
		return false
	}
	if k.Role == port.SessionRoleAdmin {
		return true
	}
	return k.Role == port.SessionRoleProvince && k.Department == partyport.DeptChanNuoiThuySan &&
		k.ProvinceGroup == accport.GroupTongHop && domain.ProvinceDesignable(code)
}

func (s *Service) designCatalog() (formsport.TemplateCatalog, error) {
	if s.FormsNative == nil {
		return nil, ErrDependency.WithDetail("chưa nối cap/forms")
	}
	cat, ok := s.FormsNative.(formsport.TemplateCatalog)
	if !ok {
		return nil, ErrDependency.WithDetail("chưa nối danh mục phiên bản biểu (cap/forms)")
	}
	return cat, nil
}

func decodeFields(raw json.RawMessage) ([]domain.CNTSField, error) {
	var fields []domain.CNTSField
	if json.Unmarshal(raw, &fields) != nil || len(fields) == 0 || len(fields) > domain.MaxDesignFields {
		return nil, ErrDependency.WithDetail("cấu trúc phiên bản biểu không hợp lệ")
	}
	return fields, nil
}

// CNTSDesign đọc đúng cấu trúc phiên bản đã cài, không dùng cấu trúc mã nguồn thay thế bản đó: năm biểu kỳ (#174) và các
// biểu điều tra đã lưu trong danh mục cap/forms (#192 — không có danh sách mã cố định).
func (s *Service) CNTSDesign(ctx context.Context) ([]CNTSDesignView, error) {
	k, err := designReader(ctx)
	if err != nil {
		return nil, err
	}
	cat, err := s.designCatalog()
	if err != nil {
		return nil, err
	}
	out := make([]CNTSDesignView, 0, len(domain.CNTSForms))
	for _, f := range domain.CNTSForms {
		v := CNTSDesignView{Code: f.Code, BM: f.BM, Title: f.Title, Kind: DesignKindReport, PeriodType: f.PeriodType, Due: f.Due,
			Locked: domain.CNTSLocked(f.Code), CanEdit: !domain.CNTSLocked(f.Code) && designWriter(k, f.Code), Fields: f.Fields}
		ref, err := s.FormsNative.LatestTemplate(ctx, domain.ProfileCNTS, f.Code)
		if err != nil && !errors.Is(err, formsport.ErrTemplateNotFound) {
			return nil, err
		}
		if err == nil {
			fields, err := decodeFields(ref.Fields)
			if err != nil {
				return nil, err
			}
			id, version := ref.ID.String(), ref.Version
			v.TemplateID, v.Version, v.Title, v.Fields = &id, &version, ref.Title, fields
		}
		out = append(out, v)
	}
	all, err := cat.ListTemplates(ctx, domain.ProfileCNTS)
	if err != nil {
		return nil, err
	}
	for _, t := range all {
		if !domain.IsSurveyCode(t.Code) {
			continue
		}
		fields, err := decodeFields(t.Fields)
		if err != nil {
			return nil, err
		}
		id, version := t.ID.String(), t.Version
		out = append(out, CNTSDesignView{Code: t.Code, BM: t.Code, Title: t.Title, Kind: DesignKindSurvey, PeriodType: "adhoc",
			Due: "Theo đợt điều tra", CanEdit: designWriter(k, t.Code), TemplateID: &id, Version: &version, Fields: fields})
	}
	return out, nil
}

// knownDesign: biểu kỳ của phòng hoặc mã biểu điều tra hợp lệ.
func knownDesign(code string) bool {
	_, ok := domain.CNTSFormByCode(code)
	return ok || domain.IsSurveyCode(code)
}

// CNTSDesignHistory đọc lịch sử phiên bản (mới trước; before=0 là mới nhất, limit 1–200).
func (s *Service) CNTSDesignHistory(ctx context.Context, code string, before int64, limit int) ([]CNTSDesignVersion, error) {
	if _, err := designReader(ctx); err != nil {
		return nil, err
	}
	if !knownDesign(code) {
		return nil, formsport.ErrTemplateNotFound
	}
	cat, err := s.designCatalog()
	if err != nil {
		return nil, err
	}
	vs, err := cat.TemplateHistory(ctx, domain.ProfileCNTS, code, before, limit)
	if err != nil {
		return nil, err
	}
	out := make([]CNTSDesignVersion, 0, len(vs))
	for _, v := range vs {
		fields, err := decodeFields(v.Fields)
		if err != nil {
			return nil, err
		}
		out = append(out, CNTSDesignVersion{TemplateID: v.ID.String(), Version: v.Version, Title: v.Title, Fields: fields})
	}
	return out, nil
}

// CNTSDesignSave là yêu cầu lưu phiên bản mới (FN-07 B1–B2; kết quả kiểm Excel hợp lệ cũng lưu qua đây).
type CNTSDesignSave struct {
	BaseVersion int64
	Title       string
	Fields      []domain.CNTSField
}

func checkTitle(title string) (string, error) {
	title = strings.TrimSpace(title)
	if n := utf8.RuneCountInString(title); n < 1 || n > 200 {
		return "", problem.Validation.WithField("title", "Tên biểu cần 1–200 ký tự")
	}
	return title, nil
}

func fieldsError(issues []domain.DesignIssue) error {
	if len(issues) == 0 {
		return nil
	}
	x := issues[0]
	if x.Line == 0 {
		return problem.Validation.WithField(x.Field, x.Message)
	}
	return problem.Validation.WithField(fmt.Sprintf("fields[%d].%s", x.Line-1, x.Field), x.Message)
}

func (s *Service) define(ctx context.Context, code, title string, fields []domain.CNTSField, base int64, idem, kind string, input any) (CNTSDesignVersion, error) {
	raw, err := json.Marshal(fields)
	if err != nil {
		return CNTSDesignVersion{}, err
	}
	def, err := s.fenceCNTSDefinition(ctx, kind, code, input, formsport.TemplateDef{Profile: domain.ProfileCNTS, Code: code, Title: title, Fields: raw, BaseVersion: &base}, idem)
	if err != nil {
		return CNTSDesignVersion{}, err
	}
	// Keep the native operation key for existing same-code receipts. The KN fence
	// binds new/first-seen requests across codes before the code-scoped native key.
	ref, err := s.FormsNative.DefineTemplate(ctx, def, idem)
	if err != nil {
		return CNTSDesignVersion{}, err
	}
	actual, err := decodeFields(ref.Fields)
	if err != nil {
		return CNTSDesignVersion{}, err
	}
	return CNTSDesignVersion{TemplateID: ref.ID.String(), Version: ref.Version, Title: ref.Title, Fields: actual}, nil
}

// SaveCNTSDesign lưu cấu trúc thành phiên bản mới (base_version = phiên bản đã đọc; cap/forms chặn ghi đè khi người khác
// lưu trước — 409). Kỳ đã phát hành giữ phiên bản cũ (BR-01, AC-03); mẫu KNQG bị từ chối (BR-02).
func (s *Service) SaveCNTSDesign(ctx context.Context, code string, in CNTSDesignSave, idem string) (CNTSDesignVersion, error) {
	k, err := designReader(ctx)
	if err != nil {
		return CNTSDesignVersion{}, err
	}
	if err := s.currentCNTSWrite(k); err != nil {
		return CNTSDesignVersion{}, err
	}
	if idem == "" {
		return CNTSDesignVersion{}, problem.IdempotencyRequired
	}
	if !knownDesign(code) {
		return CNTSDesignVersion{}, formsport.ErrTemplateNotFound
	}
	if domain.CNTSLocked(code) {
		return CNTSDesignVersion{}, errDesignLocked
	}
	if !designWriter(k, code) {
		return CNTSDesignVersion{}, errDesignWriter
	}
	if s.FormsNative == nil {
		return CNTSDesignVersion{}, ErrDependency.WithDetail("chưa nối cap/forms")
	}
	title, err := checkTitle(in.Title)
	if err != nil {
		return CNTSDesignVersion{}, err
	}
	if in.BaseVersion < 0 {
		return CNTSDesignVersion{}, problem.Validation.WithField("base_version", "phiên bản gốc không được âm")
	}
	if err := fieldsError(domain.ValidateDesignFields(in.Fields)); err != nil {
		return CNTSDesignVersion{}, err
	}
	if in.BaseVersion > 0 {
		cat, err := s.designCatalog()
		if err != nil {
			return CNTSDesignVersion{}, err
		}
		ref, err := cat.TemplateVersion(ctx, domain.ProfileCNTS, code, in.BaseVersion)
		if err != nil {
			return CNTSDesignVersion{}, err
		}
		cur, err := decodeFields(ref.Fields)
		if err != nil {
			return CNTSDesignVersion{}, err
		}
		if is := domain.DesignChange(cur, in.Fields); len(is) > 0 {
			return CNTSDesignVersion{}, problem.Validation.WithField("fields", is[0].Field+": "+is[0].Message)
		}
	}
	return s.define(ctx, code, title, in.Fields, in.BaseVersion, idem, "save", in)
}

// CNTSDesignCreate là yêu cầu tạo biểu điều tra mới hoặc sao chép biểu có sẵn (FN-07 luồng phụ "Sao chép biểu").
type CNTSDesignCreate struct {
	CodeSuffix    string
	Title         string
	SourceCode    string // rỗng = tạo từ Fields
	SourceVersion int64  // sao chép: phiên bản nguồn bất biến, từ 1 trở lên
	Fields        []domain.CNTSField
}

// CreateCNTSDesign tạo biểu điều tra mã mới "CNTS-DT-<hậu tố>" ở phiên bản 1. Sao chép lấy đúng cấu trúc phiên bản nguồn
// (kể cả mẫu KNQG — bản sao là biểu mới, mẫu gốc không đổi).
func (s *Service) CreateCNTSDesign(ctx context.Context, in CNTSDesignCreate, idem string) (string, CNTSDesignVersion, error) {
	k, err := designReader(ctx)
	if err != nil {
		return "", CNTSDesignVersion{}, err
	}
	if err := s.currentCNTSWrite(k); err != nil {
		return "", CNTSDesignVersion{}, err
	}
	if idem == "" {
		return "", CNTSDesignVersion{}, problem.IdempotencyRequired
	}
	code, ok := domain.SurveyCode(in.CodeSuffix)
	if !ok {
		return "", CNTSDesignVersion{}, problem.Validation.WithField("code_suffix", "Chữ in hoa, số, gạch ngang; 1–40 ký tự, bắt đầu bằng chữ hoặc số")
	}
	if !designWriter(k, code) {
		return "", CNTSDesignVersion{}, errDesignWriter
	}
	if s.FormsNative == nil {
		return "", CNTSDesignVersion{}, ErrDependency.WithDetail("chưa nối cap/forms")
	}
	title, err := checkTitle(in.Title)
	if err != nil {
		return "", CNTSDesignVersion{}, err
	}
	fields := in.Fields
	if in.SourceCode != "" {
		if len(in.Fields) > 0 {
			return "", CNTSDesignVersion{}, problem.Validation.WithField("fields", "Sao chép lấy cấu trúc của biểu nguồn — không gửi kèm chỉ tiêu")
		}
		if fields, err = s.sourceFields(ctx, in.SourceCode, in.SourceVersion); err != nil {
			return "", CNTSDesignVersion{}, err
		}
	}
	if err := fieldsError(domain.ValidateDesignFields(fields)); err != nil {
		return "", CNTSDesignVersion{}, err
	}
	// Native base_version=0 and the durable definition receipt distinguish a new
	// conflicting command from a retry, even after the template head advances.
	v, err := s.define(ctx, code, title, fields, 0, idem, "create", in)
	return code, v, err
}

func (s *Service) sourceFields(ctx context.Context, code string, version int64) ([]domain.CNTSField, error) {
	if !knownDesign(code) {
		return nil, formsport.ErrTemplateNotFound.WithField("source_code", code)
	}
	if version < 1 {
		return nil, problem.Validation.WithField("source_version", "chọn phiên bản nguồn bất biến từ 1 trở lên")
	}
	cat, err := s.designCatalog()
	if err != nil {
		return nil, err
	}
	tv, err := cat.TemplateVersion(ctx, domain.ProfileCNTS, code, version)
	if err != nil {
		return nil, err
	}
	return decodeFields(tv.Fields)
}

// CNTSDefinitionPolicy là chính sách định nghĩa biểu của profile kn_cnts (cổng #191), áp cho MỌI đường ghi phiên bản —
// REST /v1/forms/templates, Publisher.DefineTemplate, màn FN-07, cài biểu chuẩn:
//   - chỉ Admin hoặc cán bộ tổng hợp Cấp Tỉnh Phòng CNTS (designWriter); Cấp Tỉnh chỉ BM-17 và biểu điều tra;
//   - mẫu KNQG (BR-02): chỉ chấp nhận đúng cấu trúc chuẩn của mẫu (cài/cập nhật biểu chuẩn), mọi thay đổi khác bị từ chối;
//   - kiểm thay đổi trên base_version đã chọn; native CAS và receipt xử lý head tiến.
//     Hạn chế add-only của #192 còn chờ public usage supplier, chưa là full UR-TINH-07.
//
// Chạy ngoài giao dịch ghi của cap/forms, không ghi dữ liệu; mặc định từ chối.
func (s *Service) CNTSDefinitionPolicy(ctx context.Context, r formsport.TemplateDefinitionRequest) (formsport.TemplateDefinitionDecision, error) {
	deny := formsport.TemplateDefinitionDecision{}
	if r.Proposed.Profile != domain.ProfileCNTS {
		return deny, nil
	}
	_, k, err := port.RequireSession(authz.WithPrincipal(ctx, r.Principal))
	if err != nil {
		return deny, err
	}
	code := r.Proposed.Code
	if err := s.currentCNTSWrite(k); err != nil {
		return deny, err
	}
	if !knownDesign(code) || !designWriter(k, code) {
		return deny, nil
	}
	var next []domain.CNTSField
	if json.Unmarshal(r.Proposed.Fields, &next) != nil || len(domain.ValidateDesignFields(next)) > 0 {
		return deny, nil
	}
	if domain.CNTSLocked(code) {
		f, _ := domain.CNTSFormByCode(code)
		return formsport.TemplateDefinitionDecision{Allowed: sameFields(r.Proposed.Fields, f.Fields)}, nil
	}
	if r.BaseVersion != nil && *r.BaseVersion > 0 {
		cat, err := s.designCatalog()
		if err != nil {
			return deny, err
		}
		base, err := cat.TemplateVersion(ctx, domain.ProfileCNTS, code, *r.BaseVersion)
		if err != nil {
			return deny, err
		}
		cur, err := decodeFields(base.Fields)
		if err != nil {
			return deny, err
		}
		if len(domain.DesignChange(cur, next)) > 0 {
			return deny, nil
		}
	}
	return formsport.TemplateDefinitionDecision{Allowed: true}, nil
}

// PreviewCNTSDesignImport chỉ đọc/kiểm tệp; không gọi DefineTemplate, không ghi DB/kỳ/nhật ký. Lưu là thao tác tường minh
// riêng (SaveCNTSDesign/CreateCNTSDesign) với cấu trúc hợp lệ trả về ở đây.
func (s *Service) PreviewCNTSDesignImport(ctx context.Context, src io.Reader) (domain.DesignImport, error) {
	if _, err := designReader(ctx); err != nil {
		return domain.DesignImport{}, err
	}
	lim := tabular.Limits{MaxBytes: MaxDesignImportBytes, MaxRows: domain.MaxDesignFields, MaxCols: len(domain.DesignHeaders)}
	tb, err := tabular.Read(src, lim)
	if err != nil {
		return domain.DesignImport{}, problem.Validation.WithField("file", tabular.Message(err, lim))
	}
	return domain.ParseDesign(tb), nil
}
