package port

import "github.com/google/uuid"

// Vai nghiệp vụ của sổ tổ chức, HTX và nông hộ (tên vai trong Keycloak).
const (
	// RoleRegistryAdmin: quản trị dữ liệu chủ — tạo/di chuyển tổ chức, vô hiệu hóa HTX, nhập hàng loạt.
	RoleRegistryAdmin = "registry_admin"
	// RoleKNNationalAdmin: cán bộ TTKN Quốc gia — xem toàn quốc, chỉ đọc (KN US-HTX-01).
	RoleKNNationalAdmin = "kn_national_admin"
	// RoleKNProvinceAdmin: cán bộ TTKN tỉnh — đọc/ghi trong tỉnh.
	RoleKNProvinceAdmin = "kn_province_admin"
	// RoleKNOfficer: cán bộ xã / Tổ KNCĐ — đọc/ghi trong xã phụ trách.
	RoleKNOfficer = "kn_officer"
	// RoleCoopManager: quản lý HTX — nông hộ của chính HTX mình (HTX US-HH-01).
	RoleCoopManager = "coop_manager"
	// RoleCGHViewer: Cục KTHT xem bản đồ CGH — chỉ đọc hồ sơ HTX.
	RoleCGHViewer = "cgh_viewer"
)

// Sự kiện của sổ tổ chức (schema trong contracts/events/).
const (
	EventOrgCreated     = "pf.party.org.created"
	EventOrgMoved       = "pf.party.org.moved"
	EventOrgDeactivated = "pf.party.org.deactivated"
	EventOrgReactivated = "pf.party.org.reactivated"
)

// OrgCreatedV1 là payload của pf.party.org.created: ảnh chụp Nút mới (W2.5, ADR-0029 — worker pf.kref
// dựng k_ref.org + k_ref.node_closure). Chỉ mã, loại, đơn vị hành chính, cha, trạng thái — không tên, không PII.
//
//ag:event pf.party.org.created v1
type OrgCreatedV1 struct {
	OrgRef        uuid.UUID  `json:"org_ref"`
	Kind          string     `json:"kind"`
	Code          string     `json:"code"`
	AdminUnitCode string     `json:"admin_unit_code"`
	ParentRef     *uuid.UUID `json:"parent_ref"`
	Active        bool       `json:"active"`
	ActorRef      uuid.UUID  `json:"actor_ref"`
	EventTime     string     `json:"event_time"`
	Seq           int64      `json:"seq"`
}

// OrgMovedV1 là payload của pf.party.org.moved: Nút đổi cha (kèm cả nhánh con) và/hoặc đổi mã đơn vị hành
// chính tại chỗ (sáp nhập ĐVHC) — ảnh chụp SAU sự việc và vị trí trước (W2.5, ADR-0029).
//
//ag:event pf.party.org.moved v1
type OrgMovedV1 struct {
	OrgRef            uuid.UUID  `json:"org_ref"`
	Kind              string     `json:"kind"`
	Code              string     `json:"code"`
	AdminUnitCode     string     `json:"admin_unit_code"`
	ParentRef         *uuid.UUID `json:"parent_ref"`
	PrevParentRef     *uuid.UUID `json:"prev_parent_ref"`
	PrevAdminUnitCode string     `json:"prev_admin_unit_code"`
	Active            bool       `json:"active"`
	Reason            string     `json:"reason"`
	ActorRef          uuid.UUID  `json:"actor_ref"`
	EventTime         string     `json:"event_time"`
	Seq               int64      `json:"seq"`
}

// OrgDeactivatedV1 là payload của pf.party.org.deactivated. Consumer (quản trị
// tài khoản) khóa mọi tài khoản thuộc tổ chức (HTX US-HTXSTATUS-01 AC-2) và ghi
// nhớ lý do khóa để kích hoạt lại chỉ mở đúng các tài khoản đó.
//
//ag:event pf.party.org.deactivated v1
type OrgDeactivatedV1 struct {
	OrgRef           uuid.UUID `json:"org_ref"`
	Kind             string    `json:"kind"`
	Code             string    `json:"code"`
	AdminUnitCode    string    `json:"admin_unit_code"`
	Reason           string    `json:"reason"`
	ActorRef         uuid.UUID `json:"actor_ref"`
	EventTime        string    `json:"event_time"`
	Seq              int64     `json:"seq"`
	ActiveHouseholds int       `json:"active_households"`
}

// OrgReactivatedV1 là payload của pf.party.org.reactivated: consumer mở khóa
// CHỈ các tài khoản đã bị khóa vì pf.party.org.deactivated (US-HTXSTATUS-02 AC-2).
//
//ag:event pf.party.org.reactivated v1
type OrgReactivatedV1 struct {
	OrgRef        uuid.UUID `json:"org_ref"`
	Kind          string    `json:"kind"`
	Code          string    `json:"code"`
	AdminUnitCode string    `json:"admin_unit_code"`
	Reason        string    `json:"reason,omitempty"`
	ActorRef      uuid.UUID `json:"actor_ref"`
	EventTime     string    `json:"event_time"`
	Seq           int64     `json:"seq"`
}
