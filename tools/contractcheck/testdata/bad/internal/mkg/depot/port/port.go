// Ca kiểm thử vi phạm — mỗi vi phạm có một dòng mong đợi trong main_test.go.
package port

import "github.com/google/uuid"

// Publication là interface van của vùng mkg.
type Publication interface{ PublicationName() string }

// Point có trường lồng ngoài hợp đồng (alt).
type Point struct {
	Lat string `json:"lat"` // hợp đồng: number
	Lng int    `json:"lng"` // integer vẫn khớp number
	Alt int    `json:"alt"`
}

// Zone sai kiểu lồng trong mảng.
type Zone struct {
	Code bool `json:"code"`
}

// SitePublishedV1 vi phạm hợp đồng depot_site.
//
//ag:publication depot_site
//ag:event mkg.depot.site.published v2
type SitePublishedV1 struct {
	SiteRef    uuid.UUID `json:"site_ref"`
	Location   Point     `json:"location"`
	CapacityKg int64     `json:"capacity_kg"`
	Tags       []int     `json:"tags"`
	Zones      []Zone    `json:"zones"`
	Secret     string    `json:"owner_phone"`
}

// PublicationName trả sai tên hợp đồng.
func (SitePublishedV1) PublicationName() string { return "depot" }

// ThingHappenedV1 là sự kiện vùng mkg không qua van.
//
//ag:event mkg.depot.thing.happened v1
type ThingHappenedV1 struct {
	ThingRef string `json:"thing_ref"`
}

// Rogue cài đặt Publication mà không có hợp đồng.
type Rogue struct{ X string }

// PublicationName cài đặt Publication.
func (Rogue) PublicationName() string { return "rogue" }

// SelfApproved dùng hợp đồng tự phê duyệt — coi như không có.
//
//ag:publication self_ok
type SelfApproved struct {
	SiteRef string `json:"site_ref"`
}
