// Ca hợp lệ: sự kiện nội bộ đi với outbox.KindInternal; van đi với KindPlatform (hoặc không ghi Kind).
package app

import (
	"example.com/ag/internal/mkg/depot/port"
	"example.com/ag/internal/shared/outbox"
)

func internalLiteral() outbox.Event {
	return outbox.Event{Kind: outbox.KindInternal, Payload: port.StockMovedV1{}}
}

func internalParam(ev *port.StockMovedV1) outbox.Event {
	return outbox.Event{Payload: ev, Kind: outbox.KindInternal}
}

func platformExplicit(p port.SitePublishedV1) outbox.Event {
	return outbox.Event{Kind: outbox.KindPlatform, Payload: p}
}
