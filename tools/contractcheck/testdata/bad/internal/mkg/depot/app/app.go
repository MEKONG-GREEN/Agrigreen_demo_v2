// Ca vi phạm: payload outbox.Event của vùng mkg không chứng minh được là hợp đồng van.
package app

import (
	"example.com/ag/internal/mkg/depot/port"
	ob "example.com/ag/internal/shared/outbox"
)

type costRow struct {
	LotRef string `json:"lot_ref"`
	Price  string `json:"price"`
}

func build() any { return costRow{} }

func leakStruct() ob.Event { return ob.Event{Payload: costRow{LotRef: "x"}} }

func leakCall() ob.Event { return ob.Event{Payload: build()} }

func leakAssign() ob.Event {
	var e ob.Event
	e.Payload = costRow{}
	return e
}

func leakInternalEvent() ob.Event { return ob.Event{Payload: port.ThingHappenedV1{}} }

func positional() ob.Event { return ob.Event{nil} }
