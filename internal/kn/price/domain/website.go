package domain

import "time"

// WebsiteNotConnected is a local acknowledgement, never a delivery result.
const WebsiteNotConnected = "not_connected"

// WebsiteStatus describes the latest local request for the current board version.
type WebsiteStatus struct {
	Status      string
	Version     int
	RequestedAt *time.Time
}
