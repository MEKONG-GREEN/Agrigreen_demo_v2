package port

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
)

// OrgKind là loại tổ chức tham gia.
type OrgKind string

// Các loại tổ chức của ĐTK 1 và ĐTK 2.
const (
	OrgTTKNNational  OrgKind = "ttkn_national"  // Trung tâm Khuyến nông Quốc gia
	OrgTTKNProvince  OrgKind = "ttkn_province"  // TTKN tỉnh
	OrgExtensionTeam OrgKind = "extension_team" // Tổ khuyến nông cộng đồng / KNV xã
	OrgDCRD          OrgKind = "dcrd"           // Cục Kinh tế hợp tác và PTNT
	OrgCooperative   OrgKind = "cooperative"    // HTX / THT
	OrgMKG           OrgKind = "mkg"            // Mekong Green
	OrgPartner       OrgKind = "partner"        // bên thứ ba (ĐTK 2)
)

// Org là một tổ chức. Tên tổ chức không phải dữ liệu cá nhân; người đại diện thì là
// (nằm ở kho định danh, tham chiếu bằng RepresentativeRef).
type Org struct {
	Ref               uuid.UUID
	Kind              OrgKind
	Code              string // mã nghiệp vụ, vd mã số thuế HTX hoặc mã đơn vị
	Name              string
	AdminUnitCode     string    // đơn vị hành chính gắn với tổ chức (tỉnh/xã); rỗng với cấp quốc gia
	ParentRef         uuid.UUID // tổ chức cấp trên trong cây phân cấp; Nil ở gốc
	RepresentativeRef uuid.UUID // mã tham chiếu người đại diện trong vault; Nil nếu chưa có
	Active            bool
}

// ErrOrgNotFound: không có tổ chức.
var ErrOrgNotFound = errors.New("party: không có tổ chức")

// Orgs tra cứu tổ chức.
type Orgs interface {
	Get(ctx context.Context, ref uuid.UUID) (Org, error)
}

// OrgCodes tra tổ chức theo mã nghiệp vụ (vd Mã HTX trong tệp nhập). Tách khỏi
// Orgs để không buộc mọi bản cài Orgs phải có; module khác kiểm bằng type assertion.
type OrgCodes interface {
	ByCode(ctx context.Context, code string) (Org, error) // không có → ErrOrgNotFound
}

// ScopeResolver tính phạm vi dữ liệu của người gọi từ Principal.OrgRef (ADR-0009):
//   - ttkn_national, dcrd, mkg, vai platform_admin → All
//   - ttkn_province → tỉnh và mọi xã của tỉnh (theo masterdata tại thời điểm hiện tại)
//   - extension_team, cooperative → xã của tổ chức
//   - không có org_ref → phạm vi rỗng (mặc định chặn)
type ScopeResolver interface {
	Scope(ctx context.Context) (authz.Scope, error)
}
