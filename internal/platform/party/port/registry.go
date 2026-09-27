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
	EventOrgDeactivated = "pf.party.org.deactivated"
	EventOrgReactivated = "pf.party.org.reactivated"
)

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
