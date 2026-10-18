package client

import (
	"context"
	"net/http"
	"net/url"

	"github.com/google/uuid"
)

// W11.22 (vault 000004): hồ sơ nhân sự HCTH (pf/hrm). Mở rộng TÙY CHỌN của Client — tách interface để không bắt mọi
// hiện thực cũ (và kho giả của module khác) phải đổi. Module cần thì gọi AsProfiles.

// Mục đích của hồ sơ nhân sự. PurposeHRMSensitive CHỈ đọc/ghi được ngăn SectionHRMSensitive (thẻ Thống kê BC Sở,
// TK-01..06); kho từ chối mục đích này ở mọi thao tác khác.
const (
	PurposeHRMProfile   Purpose = "hrm.profile"
	PurposeHRMSensitive Purpose = "hrm.sensitive"
)

// Ngăn hồ sơ: mỗi ngăn mã hóa bằng khóa dữ liệu RIÊNG (không chung khóa của định danh), gắn cứng một mục đích.
const (
	SectionHRMProfile   = "hrm.profile"   // ngày sinh, mức chính xác, giới tính, email, ghi chú
	SectionHRMSensitive = "hrm.sensitive" // dân tộc, tôn giáo, ngày vào Đảng, quê quán, nơi sinh, ngày TGCM
)

// Fields là tập trường của một ngăn — CHỈ tồn tại trong bộ nhớ (R-14: không log).
type Fields map[string]string

// Profiles là phần mở rộng của kho cho hồ sơ có nhiều trường định danh.
type Profiles interface {
	// Amend thay họ tên, SĐT, địa chỉ của chủ thể (CCCD, loại, địa bàn giữ nguyên). SĐT trùng chủ thể khác trong
	// tenant → ErrDuplicate. Vai RoleRegistrar; ghi sổ op "amend".
	Amend(ctx context.Context, purpose Purpose, ref uuid.UUID, id Identity) error
	// PutSection ghi (thay toàn bộ) ngăn section; mục đích phải là mục đích của ngăn. Vai RoleRegistrar.
	PutSection(ctx context.Context, purpose Purpose, ref uuid.UUID, section string, fields Fields) error
	// GetSection đọc ngăn (chưa ghi → map rỗng). Ghi sổ op "section_read" mỗi lần; ngăn nhạy cảm cần MFA khi kho bật.
	GetSection(ctx context.Context, purpose Purpose, ref uuid.UUID, section string) (Fields, error)
}

// AsProfiles trả phần mở rộng hồ sơ của c; ok = false nếu hiện thực không hỗ trợ.
func AsProfiles(c Client) (Profiles, bool) {
	p, ok := c.(Profiles)
	return p, ok
}

// --- Dạng trên dây (contracts/openapi/identity-vault.yaml). ---

// AmendRequest là thân POST /v1/subjects/{ref}/amend.
type AmendRequest struct {
	Purpose  string `json:"purpose,omitempty"`
	FullName string `json:"full_name" pii:"true"`
	Phone    string `json:"phone,omitempty" pii:"true"`
	Address  string `json:"address,omitempty" pii:"true"`
}

// SectionRequest là thân POST /v1/subjects/{ref}/sections/{section} (ghi).
type SectionRequest struct {
	Purpose string            `json:"purpose,omitempty"`
	Fields  map[string]string `json:"fields" pii:"true"`
}

// SectionResponse là phản hồi đọc ngăn (POST …/sections/{section}/read).
type SectionResponse struct {
	Ref     string            `json:"ref"`
	Section string            `json:"section"`
	Fields  map[string]string `json:"fields" pii:"true"`
}

// WriteResponse là phản hồi của amend và ghi ngăn — không chứa định danh.
type WriteResponse struct {
	Ref string `json:"ref"`
}

var _ Profiles = (*httpClient)(nil)

func (c *httpClient) Amend(ctx context.Context, purpose Purpose, ref uuid.UUID, id Identity) error {
	if purpose == "" {
		return ProblemPurposeRequired
	}
	return c.do(ctx, call{method: http.MethodPost, path: subjectPath(ref, "/amend"), purpose: purpose, idemOp: "amend",
		in: AmendRequest{Purpose: string(purpose), FullName: id.FullName, Phone: id.Phone, Address: id.Address}, out: &WriteResponse{}})
}

func (c *httpClient) PutSection(ctx context.Context, purpose Purpose, ref uuid.UUID, section string, fields Fields) error {
	if purpose == "" {
		return ProblemPurposeRequired
	}
	if fields == nil {
		fields = Fields{}
	}
	return c.do(ctx, call{method: http.MethodPost, path: subjectPath(ref, "/sections/"+url.PathEscape(section)), purpose: purpose,
		idemOp: "section", in: SectionRequest{Purpose: string(purpose), Fields: fields}, out: &WriteResponse{}})
}

func (c *httpClient) GetSection(ctx context.Context, purpose Purpose, ref uuid.UUID, section string) (Fields, error) {
	if purpose == "" {
		return nil, ProblemPurposeRequired
	}
	var out SectionResponse
	if err := c.do(ctx, call{method: http.MethodPost, path: subjectPath(ref, "/sections/"+url.PathEscape(section)+"/read"),
		purpose: purpose, in: PurposeRequest{Purpose: string(purpose)}, out: &out}); err != nil {
		return nil, err
	}
	if out.Fields == nil {
		return Fields{}, nil
	}
	return out.Fields, nil
}

var _ Profiles = local{}

func (l local) profiles() (Profiles, error) {
	p, ok := l.s.(Profiles)
	if !ok {
		return nil, ProblemProfilesUnsupported
	}
	return p, nil
}

func (l local) Amend(ctx context.Context, purpose Purpose, ref uuid.UUID, id Identity) error {
	if purpose == "" {
		return ProblemPurposeRequired
	}
	p, err := l.profiles()
	if err != nil {
		return err
	}
	return p.Amend(ctx, purpose, ref, id)
}

func (l local) PutSection(ctx context.Context, purpose Purpose, ref uuid.UUID, section string, fields Fields) error {
	if purpose == "" {
		return ProblemPurposeRequired
	}
	p, err := l.profiles()
	if err != nil {
		return err
	}
	return p.PutSection(ctx, purpose, ref, section, fields)
}

func (l local) GetSection(ctx context.Context, purpose Purpose, ref uuid.UUID, section string) (Fields, error) {
	if purpose == "" {
		return nil, ProblemPurposeRequired
	}
	p, err := l.profiles()
	if err != nil {
		return nil, err
	}
	return p.GetSection(ctx, purpose, ref, section)
}
