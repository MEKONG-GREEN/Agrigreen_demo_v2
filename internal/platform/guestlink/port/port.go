// Package port defines owner-neutral guest-link contracts. Owners recheck their
// resource lock in their own write transaction; no identity is impersonated.
package port

import (
	"context"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/google/uuid"
	"log/slog"
	"net/http"
	"time"
)

var (
	ErrDenied      = problem.New(http.StatusForbidden, "guestlink.denied", "Không thể sử dụng liên kết hoặc mã xác thực")
	ErrUnavailable = problem.New(http.StatusServiceUnavailable, "guestlink.unavailable", "Liên kết khách chưa sẵn sàng")
	ErrInvalid     = problem.New(http.StatusUnprocessableEntity, "guestlink.invalid", "Yêu cầu liên kết khách không hợp lệ")
)

// Secret must be deliberately revealed only to a delivery adapter or the owner.
// Ordinary JSON/log/formatting never exposes a bearer token, OTP or contact.
type Secret struct{ value string }

func NewSecret(value string) Secret           { return Secret{value: value} }
func (s Secret) Reveal() string               { return s.value }
func (s Secret) String() string               { return "[redacted]" }
func (s Secret) GoString() string             { return "[redacted]" }
func (s Secret) LogValue() slog.Value         { return slog.StringValue("[redacted]") }
func (s Secret) MarshalJSON() ([]byte, error) { return []byte(`"[redacted]"`), nil }

type Binding struct {
	Tenant   uuid.UUID `json:"tenant"`
	Owner    string    `json:"owner"`
	Subject  uuid.UUID `json:"subject"`
	Resource uuid.UUID `json:"resource"`
	Step     string    `json:"step"`
}

// State contains no client-selected destination. Contact is transient, verified
// by the owner using its vault/profile policy; it must never enter audit/ledger.
type State struct {
	Enabled         bool
	Locked          bool
	Until           time.Time
	OwnerRevision   uuid.UUID // changes on lock/reopen/revoke; old capabilities must never revive
	ContactRevision uuid.UUID
	ContactVerified bool
	Channel         string // first slice supports verified email only; no SMS transport is installed
	Contact         Secret
}

type Owner interface {
	// MayIssue authorizes the current authenticated issuer for this exact binding.
	MayIssue(context.Context, Binding) (State, error)
	// Current is fail-closed and is called on every guest operation outside DB TX.
	Current(context.Context, Binding) (State, error)
}

type Delivery struct {
	Key     uuid.UUID // adapter must deduplicate retries using this exact challenge ID
	Channel string
	To      Secret
	Code    Secret
	Until   time.Time
}

type DeliveryResult string

const (
	Sent     DeliveryResult = "sent"
	Disabled DeliveryResult = "disabled"
	Dropped  DeliveryResult = "dropped"
)

// Sender must provide durable idempotency by Delivery.Key. No real transport is
// installed by this package; HTTP/worker wiring remains explicitly unavailable.
type Sender interface {
	Send(context.Context, Delivery) (DeliveryResult, error)
}

type Grant struct {
	ID      uuid.UUID
	Binding Binding
	Link    Secret
	Until   time.Time
}
type Challenge struct {
	ID       uuid.UUID
	Until    time.Time
	Delivery DeliveryResult
}
type Session struct {
	Token   Secret
	Binding Binding
	Until   time.Time
}

type GuestLinks interface {
	Issue(context.Context, Binding, time.Time, string) (Grant, error)
	Open(context.Context, Secret, string, string) (Challenge, error)
	Verify(context.Context, Secret, uuid.UUID, Secret, string) (Session, error)
	Authorize(context.Context, Secret, string) (Binding, error)
	Revoke(context.Context, Binding) error
}
