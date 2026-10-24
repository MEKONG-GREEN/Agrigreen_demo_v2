package port

// DeliveryChannels is the source owner's explicit channel choice. Inbox storage
// for transport deduplication remains internal. Push implies InApp so a data-only
// push can resolve its inbox content; an email-only choice remains hidden.
// A nil NotificationDraft.Channels preserves existing producers unchanged.
type DeliveryChannels struct {
	InApp bool `json:"in_app"`
	Push  bool `json:"push"`
	Email bool `json:"email"`
}
