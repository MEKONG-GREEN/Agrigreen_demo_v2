// Ca hợp lệ: payload outbox.Event của vùng mkg chứng minh được là hợp đồng van.
package app

import (
	"example.com/ag/internal/mkg/depot/port"
	"example.com/ag/internal/shared/outbox"
)

type other struct{ Payload string }

func viaInterface(pub port.Publication) outbox.Event { return outbox.Event{Type: "x", Payload: pub} }

func viaLiteral() outbox.Event {
	return outbox.Event{Payload: &port.SitePublishedV1{Code: "A"}}
}

func viaLocal() outbox.Event {
	p := port.SitePublishedV1{Code: "B"}
	return outbox.Event{Payload: p}
}

func viaParam(p port.SitePublishedV1) outbox.Event { return outbox.Event{Payload: p} }

// Gán .Payload của struct khác outbox.Event là hợp lệ.
func notAnEvent() {
	var o other
	o.Payload = "x"
	_ = o
}
