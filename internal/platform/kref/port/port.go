// Package port là hợp đồng của pf/kref (`k/ref`, ADR-0029, kiến trúc v4.1 §17.1): bản chiếu chỉ đọc
// `k_ref` của dữ liệu lõi trong mỗi cụm, và các nguồn mà lần dựng lại toàn bộ đọc.
//
// pf/kref KHÔNG import module nào (lõi dưới cùng): module nguồn (pf/party, pf/masterdata) cài đặt các
// interface nguồn dưới đây bằng vai DB của chính chúng (R-05), gốc lắp ráp (cmd/worker, cmd/drill kref)
// nối vào. Sự kiện mà worker tiêu thụ do module nguồn khai (`//ag:event`), pf/kref chỉ giải mã JSON.
package port

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// Consumer là tên consumer pf.kref của worker (hàng đợi + khóa inbox); outbox-publisher khai hàng đợi
// này qua assembly.Queues() mà không ghép module pf/kref.
const Consumer = "pf.kref"

// Sự kiện consumer pf.kref tiêu thụ (schema ở contracts/events/, chủ là module nguồn).
const (
	EventOrgCreated        = "pf.party.org.created"
	EventOrgMoved          = "pf.party.org.moved"
	EventOrgDeactivated    = "pf.party.org.deactivated"
	EventOrgReactivated    = "pf.party.org.reactivated"
	EventAdminUnitChanged  = "pf.masterdata.admin_unit.changed"
	EventShareGrantApprove = "pf.party.share_grant.approved"
	EventShareGrantRevoke  = "pf.party.share_grant.revoked"
)

// Events là danh sách sự kiện của consumer pf.kref.
var Events = []string{
	EventOrgCreated, EventOrgMoved, EventOrgDeactivated, EventOrgReactivated,
	EventAdminUnitChanged, EventShareGrantApprove, EventShareGrantRevoke,
}

// Tên quan hệ của schema k_ref (nhãn `relation` của chỉ số ag.k_ref.lag, khóa báo cáo diễn tập).
const (
	RelOrg              = "org"
	RelNodeClosure      = "node_closure"
	RelAdminUnit        = "admin_unit"
	RelShareGrantActive = "share_grant_active"
	RelShareScope       = "share_scope"
)

// Relations là mọi quan hệ k_ref theo thứ tự báo cáo.
var Relations = []string{RelOrg, RelNodeClosure, RelAdminUnit, RelShareGrantActive, RelShareScope}

// Org là một Nút của cây tổ chức trong k_ref.org. "Nút" = một dòng pf_party.org (mọi loại tổ chức);
// khu vực là THUỘC TÍNH của Nút xã, phòng ban là cấu hình của đơn vị — không phải Nút (KN v7.7
// FN-AD-04.02/04.03, ADR-0029 §1). Không chứa tên hay dữ liệu cá nhân: chỉ mã, loại, trạng thái.
type Org struct {
	Ref           uuid.UUID
	Kind          string
	Code          string
	AdminUnitCode string    // rỗng = không gắn đơn vị hành chính (cấp quốc gia)
	ParentRef     uuid.UUID // uuid.Nil = gốc
	Active        bool
	Seq           int64 // seq cuối của sổ pf_party.org_event đã áp
}

// AdminUnit là một phiên bản hiệu lực của đơn vị hành chính (ADR-0032), khóa (Code, ValidFrom).
type AdminUnit struct {
	Code           string
	ValidFrom      time.Time  // ngày (UTC 00:00)
	ValidTo        *time.Time // nil = còn hiệu lực
	Name           string
	Level          string
	ParentCode     string
	SuccessorCodes []string // kế thừa khi đóng hiệu lực (W2.2: bảng pf_masterdata.admin_unit_succession)
}

// ShareGrant là một cấp quyền chia sẻ đang hiệu lực (trạng thái active ở pf_party.share_grant).
// Tenant của dòng là bên cấp.
type ShareGrant struct {
	GrantID   uuid.UUID
	Grantee   uuid.UUID
	Kinds     []string // đã sắp
	ValidFrom time.Time
	ValidTo   time.Time
	Seq       int64 // seq cuối của sổ pf_party.share_grant_event đã áp
	// Điều khoản v2 (W4.1, ADR-0030 mục 3; cột k_ref từ W4.2). Class rỗng = cấp phép trước v2 (partner kế thừa).
	Class       string    // GrantClassIntraPlatform | GrantClassPartner | ""
	ModuleTable string    // tên logic '<module>:<share>' (chỉ intra_platform)
	ScopeNode   uuid.UUID // Nút phạm vi của đơn vị chủ (chỉ intra_platform)
	Purpose     string    // mã mục đích, khớp app.purpose của phiên đơn vị xem
	Basis       string    // mã thỏa thuận chia sẻ
}

// Lớp cấp phép (W4.1). Chỉ intra_platform sinh dòng k_ref.share_scope (đọc tại chỗ, W4.2).
const (
	GrantClassIntraPlatform = "intra_platform"
	GrantClassPartner       = "partner"
)

// ShareScopeRow là một dòng k_ref.share_scope: đơn vị xem Viewer đọc được dòng của đơn vị chủ (tenant của bản
// chiếu) gắn Nút Node ở bảng có khóa ModuleTable (modreg.ShareTable) với mục đích Purpose, nhờ cấp phép Grant.
type ShareScopeRow struct {
	Viewer      uuid.UUID
	Purpose     string
	ModuleTable string // khóa '<schema>.<share>', vd pf_coop.household
	Node        uuid.UUID
	Grant       uuid.UUID
	ValidFrom   time.Time
	ValidTo     time.Time
}

// OrgSource liệt kê mọi Nút (kể cả đã vô hiệu hóa) của một tenant — pf/party cài đặt.
type OrgSource interface {
	OrgNodes(ctx context.Context, tenantID uuid.UUID) ([]Org, error)
}

// ShareGrantSource liệt kê cấp quyền đang hiệu lực mà tenant là bên cấp — pf/party cài đặt.
type ShareGrantSource interface {
	ActiveShareGrants(ctx context.Context, tenantID uuid.UUID) ([]ShareGrant, error)
}

// AdminUnitSource liệt kê mọi phiên bản đơn vị hành chính của một tenant — pf/masterdata cài đặt.
type AdminUnitSource interface {
	AdminUnitVersions(ctx context.Context, tenantID uuid.UUID) ([]AdminUnit, error)
}

// Sources gom ba nguồn của lần dựng lại toàn bộ (thiếu nguồn nào → lỗi, không dựng nửa vời).
type Sources struct {
	Orgs        OrgSource
	ShareGrants ShareGrantSource
	AdminUnits  AdminUnitSource
	// Tenants liệt kê tenant cần dựng (nguồn ∪ k_ref hiện có) — mặc định do pf/kref tự tìm.
	Tenants func(ctx context.Context) ([]uuid.UUID, error)
}
