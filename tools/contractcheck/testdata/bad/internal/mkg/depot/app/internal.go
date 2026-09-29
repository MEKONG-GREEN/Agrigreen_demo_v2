// Ca vi phạm: sự kiện nội bộ lên exchange nền tảng, van lên vhost nội bộ, Kind không kiểm được.
package app

import (
	"example.com/ag/internal/mkg/depot/port"
	ob "example.com/ag/internal/shared/outbox"
)

func internalOnPlatform() ob.Event { return ob.Event{Payload: port.MovedV1{}} }

func internalOnPlatformExplicit(m port.MovedV1) ob.Event {
	return ob.Event{Kind: ob.KindPlatform, Payload: m}
}

func valveOnInternal() ob.Event {
	return ob.Event{Kind: ob.KindInternal, Payload: port.SitePublishedV1{}}
}

func kindFromVariable(k ob.Kind) ob.Event { return ob.Event{Kind: k, Payload: port.MovedV1{}} }

func assignKind() ob.Event {
	e := ob.Event{Payload: port.SitePublishedV1{}}
	e.Kind = ob.KindInternal
	return e
}
