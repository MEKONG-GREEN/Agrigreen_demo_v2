package client

import (
	"context"
	"net/http"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// W11.23 (vault 20261013165158): thẻ danh bạ cho pf/contact (`cap/directory`, BRD HCTH v3.0 FN-13). Mở rộng TÙY CHỌN của
// Client như Profiles — module cần thì gọi AsDirectory.

// PurposeContactDirectory là mục đích "danh bạ liên hệ" (Q-071 — đề xuất của dev): đọc thẻ danh bạ; ghi/sửa định danh và
// ngăn SectionContactCard của liên hệ ngoài tỉnh.
const PurposeContactDirectory Purpose = "contact.directory"

// SectionContactCard là ngăn của liên hệ ngoài tỉnh: email, năm sinh (trường "email", "birth_year").
const SectionContactCard = "contact.card"

// RoleDirectory là vai realm được đọc thẻ danh bạ (OpCard) — realm cấp qua vai tổng hợp kn_user để MỌI cán bộ KN tra
// được Contact. Vai này KHÔNG đọc được định danh đầy đủ (Resolve), CCCD, địa chỉ hay ngăn hồ sơ.
const RoleDirectory = "vault_directory"

// Trường của thẻ danh bạ (CardsRequest.Fields).
const (
	CardFullName  = "full_name"
	CardPhone     = "phone"
	CardBirthYear = "birth_year"
	CardEmail     = "email"
)

// MaxCardRefs là số chủ thể tối đa của một lần Cards.
const MaxCardRefs = 200

// Card là thẻ danh bạ của một chủ thể — chỉ trường được hỏi có giá trị. CHỈ trong bộ nhớ (R-14: không log).
type Card struct {
	Ref       uuid.UUID
	FullName  string `pii:"true"`
	Phone     string `pii:"true"`
	BirthYear string `pii:"true"` // năm sinh (FN-13 BR-04) — từ ngăn contact.card, hoặc năm của ngày sinh ở ngăn hrm.profile
	Email     string `pii:"true"` // từ ngăn contact.card, hoặc email ở ngăn hrm.profile
	Erased    bool   // chủ thể đã xóa định danh (mọi trường rỗng)
}

// Directory là phần mở rộng của kho cho danh bạ.
type Directory interface {
	// Cards đọc thẻ danh bạ của 1–MaxCardRefs chủ thể; fields ⊆ {CardFullName, CardPhone, CardBirthYear, CardEmail}.
	// Ref không có trong tenant bị bỏ qua (không lỗi). Vai RoleRegistrar, RoleDisplay hoặc RoleDirectory; mục đích
	// PurposeContactDirectory. Kho ghi sổ op "card" cho từng chủ thể đọc được.
	Cards(ctx context.Context, purpose Purpose, refs []uuid.UUID, fields []string) ([]Card, error)
}

// AsDirectory trả phần mở rộng danh bạ của c; ok = false nếu hiện thực không hỗ trợ.
func AsDirectory(c Client) (Directory, bool) {
	d, ok := c.(Directory)
	return d, ok
}

// CardsRequest là thân POST /v1/subjects/cards.
type CardsRequest struct {
	Purpose string   `json:"purpose"`
	Refs    []string `json:"refs"`
	Fields  []string `json:"fields"`
}

// CardItem là một thẻ trên dây.
type CardItem struct {
	Ref       string `json:"ref"`
	FullName  string `json:"full_name,omitempty" pii:"true"`
	Phone     string `json:"phone,omitempty" pii:"true"`
	BirthYear string `json:"birth_year,omitempty" pii:"true"`
	Email     string `json:"email,omitempty" pii:"true"`
	Erased    bool   `json:"erased,omitempty"`
}

// CardsResponse là phản hồi đọc thẻ.
type CardsResponse struct {
	Items []CardItem `json:"items"`
}

var _ Directory = (*httpClient)(nil)

func (c *httpClient) Cards(ctx context.Context, purpose Purpose, refs []uuid.UUID, fields []string) ([]Card, error) {
	if purpose == "" {
		return nil, ProblemPurposeRequired
	}
	if len(refs) == 0 {
		return nil, nil
	}
	in := CardsRequest{Purpose: string(purpose), Fields: fields, Refs: make([]string, len(refs))}
	for i, r := range refs {
		in.Refs[i] = r.String()
	}
	var out CardsResponse
	if err := c.do(ctx, call{method: http.MethodPost, path: "/v1/subjects/cards", purpose: purpose, in: in, out: &out}); err != nil {
		return nil, err
	}
	res := make([]Card, 0, len(out.Items))
	for _, it := range out.Items {
		ref, err := uuid.Parse(it.Ref)
		if err != nil {
			continue
		}
		res = append(res, Card{Ref: ref, FullName: it.FullName, Phone: it.Phone, BirthYear: it.BirthYear, Email: it.Email, Erased: it.Erased})
	}
	return res, nil
}

var _ Directory = local{}

func (l local) Cards(ctx context.Context, purpose Purpose, refs []uuid.UUID, fields []string) ([]Card, error) {
	if purpose == "" {
		return nil, ProblemPurposeRequired
	}
	d, ok := l.s.(Directory)
	if !ok {
		return nil, ProblemDirectoryUnsupported
	}
	return d.Cards(ctx, purpose, refs, fields)
}

// CodeDirectoryUnsupported: hiện thực kho không có phần mở rộng danh bạ — lỗi cấu hình.
const CodeDirectoryUnsupported = "vault.directory_unsupported"

// ProblemDirectoryUnsupported: 503 — kho được nối không hỗ trợ thẻ danh bạ.
var ProblemDirectoryUnsupported = problem.New(http.StatusServiceUnavailable, CodeDirectoryUnsupported, "Kho định danh chưa hỗ trợ danh bạ")
