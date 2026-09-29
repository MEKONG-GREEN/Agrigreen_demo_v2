// Ca kiểm thử hợp lệ: struct mang cả //ag:publication và //ag:event, trường lồng
// nhau, mảng struct, struct nhúng, kiểu có tên, con trỏ.
package port

import (
	"encoding/json"

	"example.com/ag/internal/shared/mass"
	"github.com/google/uuid"
)

// Publication là interface van của vùng mkg.
type Publication interface{ PublicationName() string }

// Point là tọa độ.
type Point struct {
	Lat json.Number `json:"lat"`
	Lng json.Number `json:"lng"`
}

// Code là kiểu có tên dựa trên string.
type Code string

// Zone là một khu.
type Zone struct {
	Code Code  `json:"code"`
	Area int64 `json:"area"`
}

// Base được nhúng (làm phẳng).
type Base struct {
	SiteRef uuid.UUID `json:"site_ref"`
}

// SitePublishedV1 là payload van depot_site.
//
//ag:publication depot_site
//ag:event mkg.depot.site.published v1
type SitePublishedV1 struct {
	Base
	Code       Code     `json:"code"`
	Location   Point    `json:"location"`
	CapacityKg mass.Kg  `json:"capacity_kg"`
	Tags       []string `json:"tags"`
	Zones      []Zone   `json:"zones,omitempty"`
	Note       *string  `json:"note,omitempty"`
	internal   string
	Skip       string `json:"-"`
}

// PublicationName cài đặt Publication.
func (SitePublishedV1) PublicationName() string { return "depot_site" }
