// Ca vi phạm: sự kiện nội bộ MKG khai ở vùng nền tảng.
package port

// PlatformInternal dùng chú thích của vùng mkg.
//
//ag:internal-event mkg.depot.moved.done v1
type PlatformInternal struct {
	LotRef string `json:"lot_ref"`
}
