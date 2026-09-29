package ingest

import (
	"errors"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/measurement/port"
)

// MKGPorts là các cổng của mkg/measurement mà ingest dùng, cùng mở trên pool vai
// ingest_mkg (cụm mkg). Hiện thực thuộc module measurement (đợt G3-1 khác);
// cmd/ingest nối qua biến wireMeasurement.
type MKGPorts struct {
	Devices  port.DeviceRegistry
	Sessions SessionSource
	Tare     TareSource // nil → ZeroTare
	Tickets  port.TicketWriter
}

// ErrPortsNotWired: cmd/ingest chưa được nối hiện thực measurement.
var ErrPortsNotWired = errors.New("ingest: chưa nối cổng mkg/measurement (DeviceRegistry, SessionSource, TicketWriter)")

// Validate kiểm đủ cổng bắt buộc.
func (p MKGPorts) Validate() error {
	if p.Devices == nil || p.Sessions == nil || p.Tickets == nil {
		return ErrPortsNotWired
	}
	return nil
}
