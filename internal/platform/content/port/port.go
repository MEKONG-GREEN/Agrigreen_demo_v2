// Package port là giao diện công khai DUY NHẤT của pf/content mà module khác
// được import (R-02): tên vai, loại tài liệu, sự kiện phát ra.
//
// pf/content là thư viện quy trình kỹ thuật, tài liệu, tin tức và cảnh báo
// khẩn dùng chung cho App Khuyến nông và App HTX (ADR-0011; KN FR-12..16,
// HTX FN-06/FN-14).
package port

// Vai nghiệp vụ của module (tên vai trong Keycloak).
const (
	// RoleEditorNational: cán bộ TTKN Quốc gia — soạn mọi loại tài liệu, mọi phạm vi;
	// duy nhất được soạn quy trình kỹ thuật chuẩn quốc gia (KN BR-06).
	RoleEditorNational = "content_editor_national"
	// RoleEditorProvince: cán bộ TTKN tỉnh — chỉ soạn cho phạm vi tỉnh/xã của mình;
	// bổ sung hướng dẫn đặc thù địa phương vào quy trình TW (KN FR-13, US-LIB-02).
	RoleEditorProvince = "content_editor_province"
	// RoleReviewer duyệt và xuất bản; không được xuất bản phiên bản do chính mình soạn (R-56).
	RoleReviewer = "content_reviewer"
)

// Kind là loại tài liệu.
type Kind string

// Các loại tài liệu.
const (
	KindProcedure  Kind = "procedure"  // quy trình kỹ thuật chuẩn quốc gia (KN FR-12)
	KindGuide      Kind = "guide"      // sổ tay, tài liệu hướng dẫn, video (KN FR-14)
	KindNews       Kind = "news"       // tin tức khuyến nông (HTX FN-06/FN-14)
	KindAlert      Kind = "alert"      // cảnh báo khẩn cấp — phát thông báo đẩy theo vùng (HTX US-NEWS-03)
	KindSupplement Kind = "supplement" // hướng dẫn đặc thù địa phương gắn quy trình TW (KN FR-13)
)

// AudienceLevel là cấp phạm vi áp dụng của tài liệu.
type AudienceLevel string

// Các cấp phạm vi.
const (
	AudienceNational AudienceLevel = "national" // toàn quốc
	AudienceProvince AudienceLevel = "province" // một tỉnh (mã tỉnh)
	AudienceCommune  AudienceLevel = "commune"  // một hoặc nhiều xã cùng tỉnh
)

// Entity đồng bộ chiều xuống (docs/dev/sync-contract.md).
const SyncEntityDocument = "document"

// Sự kiện module phát ra (schema trong contracts/events/).
const (
	EventDocumentPublished = "pf.content.document.published"
	EventAlertPublished    = "pf.content.alert.published"
)

// DocumentPublishedV1 là payload của pf.content.document.published, schema_version 1.
// Consumer thông báo (KN FR-16, US-LIB-06) gửi thông báo theo phạm vi áp dụng.
//
//ag:event pf.content.document.published v1
type DocumentPublishedV1 struct {
	DocumentRef   string   `json:"document_ref"`
	Kind          string   `json:"kind"`
	VersionNo     int      `json:"version_no"`
	VersionID     string   `json:"version_id"`
	Title         string   `json:"title"`
	Category      *string  `json:"category"`
	Tags          []string `json:"tags"`
	AudienceLevel string   `json:"audience_level"`
	AudienceUnits []string `json:"audience_units"`
	ProvinceCode  *string  `json:"province_code"`
	ParentRef     *string  `json:"parent_ref"`
	AuthorRef     string   `json:"author_ref"`
	PublisherRef  string   `json:"publisher_ref"`
	PublishedAt   string   `json:"published_at"`
	FirstVersion  bool     `json:"first_version"`
	Seq           int64    `json:"seq"`
}

// AlertPublishedV1 là payload của pf.content.alert.published, schema_version 1.
// Worker thông báo đẩy gửi tới mọi nông dân/HTX/cán bộ có địa bàn thuộc
// AudienceUnits (rỗng + level national = toàn quốc), không gửi tràn (US-NEWS-03, US-LIB-06).
//
//ag:event pf.content.alert.published v1
type AlertPublishedV1 struct {
	DocumentRef   string   `json:"document_ref"`
	VersionNo     int      `json:"version_no"`
	Title         string   `json:"title"`
	Summary       string   `json:"summary"`
	AudienceLevel string   `json:"audience_level"`
	AudienceUnits []string `json:"audience_units"`
	ProvinceCode  *string  `json:"province_code"`
	PublisherRef  string   `json:"publisher_ref"`
	PublishedAt   string   `json:"published_at"`
}
