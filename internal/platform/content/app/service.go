// Package app: use case của pf/content. Giao dịch mở ở đây; kiểm vai, phạm vi
// (ADR-0009) và phân tách nhiệm vụ (R-56) ở đây.
package app

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/content/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/content/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/content/store"
	mdport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata/port"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/objstore"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// Mã lỗi của module (docs/errors.md).
var (
	ErrNotFound           = problem.New(http.StatusNotFound, "content.document_not_found", "Không tìm thấy tài liệu")
	ErrVersionNotFound    = problem.New(http.StatusNotFound, "content.version_not_found", "Không tìm thấy phiên bản")
	ErrOutOfScope         = problem.New(http.StatusForbidden, "content.out_of_scope", "Ngoài phạm vi phụ trách")
	ErrKindNotAllowed     = problem.New(http.StatusForbidden, "content.kind_not_allowed", "Vai của bạn không được soạn loại tài liệu này")
	ErrInvalidTransition  = problem.New(http.StatusConflict, "content.invalid_transition", "Trạng thái tài liệu không cho phép thao tác này")
	ErrParentInvalid      = problem.New(http.StatusUnprocessableEntity, "content.parent_invalid", "Quy trình gốc không hợp lệ")
	ErrTagInvalid         = problem.New(http.StatusUnprocessableEntity, "content.tag_invalid", "Nhãn phân loại không có trong danh mục")
	ErrAudienceInvalid    = problem.New(http.StatusUnprocessableEntity, "content.audience_invalid", "Phạm vi áp dụng không hợp lệ")
	ErrAttachmentNotFound = problem.New(http.StatusUnprocessableEntity, "content.attachment_not_found", "Tệp đính kèm không thuộc tài liệu")
	ErrFileTooLarge       = problem.New(http.StatusRequestEntityTooLarge, "content.file_too_large", "Chỉ hỗ trợ file Word hoặc PDF trong giới hạn dung lượng cho phép.")
	ErrFileType           = problem.New(http.StatusUnsupportedMediaType, "content.file_type_not_allowed", "Loại tệp không được hỗ trợ")
	ErrChecksum           = problem.New(http.StatusUnprocessableEntity, "content.checksum_mismatch", "SHA-256 của tệp không khớp giá trị khai báo")
	ErrExtractUnsupported = problem.New(http.StatusUnprocessableEntity, "content.extract_unsupported", "Chưa hỗ trợ trích nội dung từ định dạng này, vui lòng soạn thủ công")
	ErrExtractFailed      = problem.New(http.StatusUnprocessableEntity, "content.extract_failed", "Không trích được nội dung từ tệp (tệp lỗi hoặc ảnh scan), vui lòng soạn thủ công")
	ErrDependency         = problem.New(http.StatusServiceUnavailable, "content.dependency_unavailable", "Dịch vụ phụ thuộc chưa sẵn sàng")
)

var (
	editorRoles = []string{port.RoleEditorNational, port.RoleEditorProvince}
	staffRoles  = []string{port.RoleEditorNational, port.RoleEditorProvince, port.RoleReviewer}
)

// presignTTL là thời hạn URL tải tệp đính kèm (≤ objstore.MaxPresignTTL).
const presignTTL = 10 * time.Minute

// Service là use case của module.
type Service struct {
	DB       tenant.Beginner // pool vai content_app
	Clock    clock.Clock
	Scope    partyport.ScopeResolver // phạm vi TW → Tỉnh → Xã; nil → ErrDependency
	Catalogs mdport.Catalogs         // kiểm nhãn phân loại; nil → nhãn trả ErrDependency
	Units    partyport.AdminUnits    // tùy chọn: kiểm mã đơn vị, tìm tỉnh cha của xã
	Store    objstore.Store          // tùy chọn: tệp đính kèm; nil → ErrDependency khi tải lên
}

func fieldProblem(err error) error {
	var fe domain.FieldError
	if errors.As(err, &fe) {
		return problem.Validation.WithField(fe.Field, fe.Err.Error())
	}
	return err
}

// scope tính phạm vi của người gọi.
func (s *Service) scope(ctx context.Context) (authz.Scope, error) {
	if s.Scope == nil {
		return authz.Scope{}, ErrDependency.WithDetail("chưa nối pf/party ScopeResolver")
	}
	return s.Scope.Scope(ctx)
}

// maxParentLookups: phạm vi cấp xã (tổ KN, HTX) chỉ có vài mã; phạm vi tỉnh đã
// gồm chính mã tỉnh (ScopeResolver) nên không cần tra tỉnh cha.
const maxParentLookups = 20

// visibility đổi Scope thành tập mã đơn vị được đọc: thêm tỉnh cha của mỗi xã để
// cán bộ xã/HTX thấy tài liệu cấp tỉnh của mình.
func (s *Service) visibility(ctx context.Context, sc authz.Scope) (store.Visibility, error) {
	if sc.All {
		return store.Visibility{All: true}, nil
	}
	units := slices.Clone(sc.Units)
	if s.Units != nil && len(sc.Units) <= maxParentLookups {
		now := s.Clock.Now()
		for _, c := range sc.Units {
			u, err := s.Units.Get(ctx, c, now)
			if errors.Is(err, partyport.ErrUnitNotFound) {
				continue
			}
			if err != nil {
				return store.Visibility{}, err
			}
			if u.ParentCode != "" && !slices.Contains(units, u.ParentCode) {
				units = append(units, u.ParentCode)
			}
		}
	}
	return store.Visibility{Units: units}, nil
}

func visible(vis store.Visibility, d domain.Document) bool {
	if vis.All || d.Audience.Level == port.AudienceNational {
		return true
	}
	for _, u := range d.Audience.Units {
		if slices.Contains(vis.Units, u) {
			return true
		}
	}
	return false
}

// canEdit: người soạn được ghi tài liệu loại kind, phạm vi a.
//   - TTKN TW (content_editor_national): mọi loại, mọi phạm vi.
//   - TTKN tỉnh (content_editor_province): không soạn quy trình chuẩn (KN BR-06),
//     không soạn phạm vi toàn quốc, mọi mã đơn vị phải thuộc phạm vi của mình.
func canEdit(p authz.Principal, sc authz.Scope, kind port.Kind, a domain.Audience) error {
	if p.Has(port.RoleEditorNational) {
		return nil
	}
	if !p.Has(port.RoleEditorProvince) {
		return problem.Forbidden
	}
	if kind == port.KindProcedure {
		return ErrKindNotAllowed.WithDetail("chỉ TTKN Quốc gia soạn quy trình kỹ thuật chuẩn; tỉnh dùng loại supplement (BR-06)")
	}
	if a.Level == port.AudienceNational {
		return ErrOutOfScope.WithDetail("cán bộ tỉnh không soạn tài liệu phạm vi toàn quốc")
	}
	for _, u := range a.Units {
		if !sc.Allows(u) {
			return ErrOutOfScope.WithDetail("đơn vị " + u + " ngoài phạm vi phụ trách")
		}
	}
	return nil
}

// canReview: người duyệt phải có phạm vi bao trọn phạm vi áp dụng.
func canReview(p authz.Principal, sc authz.Scope, a domain.Audience) error {
	if !p.Has(port.RoleReviewer) {
		return problem.Forbidden.WithDetail("cần vai " + port.RoleReviewer)
	}
	if a.Level == port.AudienceNational {
		if !sc.All {
			return ErrOutOfScope.WithDetail("tài liệu toàn quốc do người duyệt cấp TW xuất bản")
		}
		return nil
	}
	for _, u := range a.Units {
		if !sc.Allows(u) {
			return ErrOutOfScope.WithDetail("đơn vị " + u + " ngoài phạm vi phụ trách")
		}
	}
	return nil
}

// resolveAudience kiểm mã đơn vị với masterdata (nếu đã nối) và tìm tỉnh sở hữu.
func (s *Service) resolveAudience(ctx context.Context, a domain.Audience) (domain.Audience, error) {
	switch a.Level {
	case port.AudienceNational:
		a.Units, a.ProvinceCode = []string{}, ""
		return a, nil
	case port.AudienceProvince:
		a.ProvinceCode = a.Units[0]
	}
	if s.Units == nil {
		return a, nil
	}
	now := s.Clock.Now()
	province := ""
	for _, code := range a.Units {
		u, err := s.Units.Get(ctx, code, now)
		if errors.Is(err, partyport.ErrUnitNotFound) {
			return a, ErrAudienceInvalid.WithField("audience.units", "không có đơn vị hành chính "+code)
		}
		if err != nil {
			return a, err
		}
		want := partyport.LevelProvince
		if a.Level == port.AudienceCommune {
			want = partyport.LevelCommune
		}
		if u.Level != want {
			return a, ErrAudienceInvalid.WithField("audience.units", code+" không phải đơn vị cấp "+string(want))
		}
		if a.Level == port.AudienceCommune {
			if province != "" && u.ParentCode != province {
				return a, ErrAudienceInvalid.WithField("audience.units", "các xã phải cùng một tỉnh")
			}
			province = u.ParentCode
		}
	}
	if a.Level == port.AudienceCommune {
		a.ProvinceCode = province
	}
	return a, nil
}

// checkTags kiểm nhãn với danh mục pf/masterdata (mục đang dùng).
func (s *Service) checkTags(ctx context.Context, tags []string) error {
	if len(tags) == 0 {
		return nil
	}
	if s.Catalogs == nil {
		return ErrDependency.WithDetail("chưa nối danh mục pf/masterdata")
	}
	for _, t := range tags {
		cat, code, _ := strings.Cut(t, ":")
		it, err := s.Catalogs.Get(ctx, cat, code)
		if errors.Is(err, mdport.ErrCatalogItemNotFound) || (err == nil && !it.Active) {
			return ErrTagInvalid.WithField("tags", "không có hoặc đã ngừng dùng: "+t)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// ContentInput là nội dung một phiên bản do người soạn gửi.
type ContentInput struct {
	Title      string
	Summary    string
	Body       string
	BodyFormat domain.BodyFormat
	Category   string
	Tags       []string
}

func (in ContentInput) content() domain.Content {
	f := in.BodyFormat
	if f == "" {
		f = domain.FormatMarkdown
	}
	tags := in.Tags
	if tags == nil {
		tags = []string{}
	}
	body := in.Body
	if f == domain.FormatMarkdown || f == domain.FormatHTML {
		body = domain.Sanitize(body, f)
	}
	return domain.Content{
		Title: strings.TrimSpace(in.Title), Summary: strings.TrimSpace(in.Summary), Body: body, BodyFormat: f,
		Category: in.Category, Tags: tags,
	}
}

// View là tài liệu kèm một phiên bản và URL tải tệp.
type View struct {
	Doc         domain.Document
	Version     *domain.Version
	Attachments []AttachmentView
	Supplements []domain.Document
	// Statuses: trạng thái từng phiên bản (chỉ bề mặt quản trị).
	VersionStatus domain.VersionStatus
}

// AttachmentView là tệp đính kèm kèm URL có hạn.
type AttachmentView struct {
	domain.Attachment
	URL       string
	ExpiresAt *time.Time
}

func (s *Service) presign(ctx context.Context, atts []domain.Attachment) []AttachmentView {
	out := make([]AttachmentView, 0, len(atts))
	for _, a := range atts {
		v := AttachmentView{Attachment: a}
		if s.Store != nil {
			if u, err := s.Store.PresignGet(ctx, a.Key, presignTTL); err == nil {
				exp := s.Clock.Now().Add(presignTTL)
				v.URL, v.ExpiresAt = u, &exp
			}
		}
		out = append(out, v)
	}
	return out
}

// tx chạy fn trong giao dịch của tenant người gọi.
func (s *Service) tx(ctx context.Context, p authz.Principal, fn func(pgx.Tx) error) error {
	return tenant.WithTenantTx(ctx, s.DB, p.TenantID, fn)
}
