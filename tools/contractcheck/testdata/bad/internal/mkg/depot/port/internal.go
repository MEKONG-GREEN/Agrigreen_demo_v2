// Ca vi phạm sự kiện nội bộ MKG (ADR-0018).
package port

// MovedV1 sai kiểu so với schema nội bộ (kg phải là string).
//
//ag:internal-event mkg.depot.moved.done v1
type MovedV1 struct {
	LotRef string `json:"lot_ref"`
	Kg     int64  `json:"kg"`
}

// NoSchemaV1 không có schema trong contracts/events-internal/.
//
//ag:internal-event mkg.depot.ghost.happened v1
type NoSchemaV1 struct {
	X string `json:"x"`
}

// DualV1 vừa là sự kiện nội bộ vừa là payload van.
//
//ag:publication depot_site
//ag:internal-event mkg.depot.moved.done v1
type DualV1 struct {
	LotRef string `json:"lot_ref"`
}
