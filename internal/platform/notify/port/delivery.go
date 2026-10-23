package port

// DeliveryChannels is the source owner's explicit channel choice. Inbox storage
// for transport deduplication remains internal; InApp alone controls visibility.
// A nil NotificationDraft.Channels preserves existing producers unchanged.
type DeliveryChannels struct {
	InApp bool `json:"in_app"`
	Push  bool `json:"push"`
	Email bool `json:"email"`
}
