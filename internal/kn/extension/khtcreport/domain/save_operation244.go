package domain

import (
	"encoding/json"
	"slices"
	"time"

	"github.com/google/uuid"
)

// SaveReceipt describes only this committed save. Unknown does not prove rollback.
type SaveReceipt struct {
	OperationID uuid.UUID   `json:"operation_id"`
	PeriodID    uuid.UUID   `json:"period_id"`
	Outcome     string      `json:"outcome"`
	Applied     *SavedDraft `json:"applied,omitempty"`
}
type SavedDraft struct {
	DraftID     uuid.UUID     `json:"draft_id"`
	BaseDraftID *uuid.UUID    `json:"base_draft_id"`
	BaseVersion int64         `json:"base_version"`
	Version     int64         `json:"version"`
	RequestHash string        `json:"request_hash"`
	ActorRef    uuid.UUID     `json:"actor_ref"`
	At          time.Time     `json:"at"`
	Changes     []DraftChange `json:"changes"`
}
type DraftChange struct {
	Field         string `json:"field"`
	Before        string `json:"before"`
	After         string `json:"after"`
	BeforeDefault bool   `json:"before_default"`
	AfterDefault  bool   `json:"after_default"`
}

// SaveChanges records editable values, including an explicit reset to system.
// Numeric/source cells have no write representation in a report save.
func SaveChanges(before, after Draft) []DraftChange {
	out := []DraftChange{}
	for _, key := range AdminKeys {
		a, b := before.Admin[key], after.Admin[key]
		if a.Value() != b.Value() || (a.Override == nil) != (b.Override == nil) {
			out = append(out, DraftChange{Field: "admin." + key, Before: a.Value(), After: b.Value(), BeforeDefault: a.Override == nil, AfterDefault: b.Override == nil})
		}
	}
	keys := map[string]bool{}
	for key := range before.Texts {
		keys[key] = true
	}
	for key := range after.Texts {
		keys[key] = true
	}
	ordered := make([]string, 0, len(keys))
	for key := range keys {
		ordered = append(ordered, key)
	}
	slices.Sort(ordered)
	for _, key := range ordered {
		if before.Texts[key] != after.Texts[key] {
			out = append(out, DraftChange{Field: "texts." + key, Before: before.Texts[key], After: after.Texts[key]})
		}
	}
	a, _ := json.Marshal(before.Adhoc)
	b, _ := json.Marshal(after.Adhoc)
	if string(a) != string(b) {
		out = append(out, DraftChange{Field: "adhoc", Before: string(a), After: string(b)})
	}
	return out
}
