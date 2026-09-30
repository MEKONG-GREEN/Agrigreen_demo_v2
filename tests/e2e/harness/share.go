package harness

import (
	"net/http"
	"time"

	"github.com/google/uuid"
)

// ShareGrant là một cấp quyền chia sẻ đã duyệt (ADR-0020) cùng hai quản trị đã tạo nó.
type ShareGrant struct {
	ID                 string
	Proposer, Approver *User
}

// GrantShare cấp quyền chia sẻ tenant chương trình grantor → tenant grantee (vd MKG) qua Admin API
// thật của pf/party: quản trị A đề xuất, quản trị B (khác người, R-56) duyệt. kinds là các loại dữ
// liệu (party/port.Kind*). Hỏng → Fatal.
func (s *Stack) GrantShare(grantor, grantee uuid.UUID, purpose string, kinds ...string) ShareGrant {
	s.T.Helper()
	a := s.User(UserSpec{Tenant: grantor, MFA: true, Roles: []string{"platform_admin"}})
	b := s.User(UserSpec{Tenant: grantor, MFA: true, Roles: []string{"platform_admin"}})
	r := a.Do(Admin, http.MethodPost, "/v1/share-grants", "", map[string]any{"grantee_tenant_id": grantee.String(), "kinds": kinds,
		"purpose": purpose, "valid_to": time.Now().Add(365 * 24 * time.Hour).UTC().Format(time.RFC3339)})
	if r.Status != http.StatusCreated {
		s.T.Fatalf("đề xuất cấp quyền chia sẻ: %d %s", r.Status, r.Raw)
	}
	id := r.Str("id")
	if r := a.Do(Admin, http.MethodPost, "/v1/share-grants/"+id+"/approve", "", nil); r.Status != http.StatusForbidden {
		s.T.Fatalf("R-56: người đề xuất không được tự duyệt cấp quyền: %d %s", r.Status, r.Raw)
	}
	if r := b.Do(Admin, http.MethodPost, "/v1/share-grants/"+id+"/approve", "", nil); r.Status != http.StatusCreated || r.Str("status") != "active" {
		s.T.Fatalf("duyệt cấp quyền chia sẻ: %d %s", r.Status, r.Raw)
	}
	return ShareGrant{ID: id, Proposer: a, Approver: b}
}
