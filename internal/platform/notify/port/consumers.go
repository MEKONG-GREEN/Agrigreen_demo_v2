package port

// ConsumerDefinition registers source-event subscriptions. Identity, inbox transactions and delivery
// stay in notify. Order is a stable position within the assembled subscription catalog.
type ConsumerDefinition struct {
	Name   string
	Order  int
	Events []string
}

type ConsumerRegistration struct {
	Definitions []ConsumerDefinition
}
