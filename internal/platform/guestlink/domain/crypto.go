// Package domain contains bounded cryptographic and binding policy only.
package domain

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/guestlink/port"
	"github.com/google/uuid"
	"io"
	"regexp"
)

var ownerPattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,30}/[a-z][a-z0-9_]{0,30}$`)
var stepPattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,31}$`)

func ValidBinding(b port.Binding) bool {
	return b.Tenant != uuid.Nil && b.Subject != uuid.Nil && b.Resource != uuid.Nil && ownerPattern.MatchString(b.Owner) && stepPattern.MatchString(b.Step)
}
func BindingKey(b port.Binding) string { raw, _ := json.Marshal(b); return Hash(raw) }
func Hash(b []byte) string             { x := sha256.Sum256(b); return hex.EncodeToString(x[:]) }
func MAC(key []byte, purpose, value string) string {
	h := hmac.New(sha256.New, key)
	h.Write([]byte(purpose))
	h.Write([]byte{0})
	h.Write([]byte(value))
	return hex.EncodeToString(h.Sum(nil))
}
func SameMAC(a, b string) bool { return hmac.Equal([]byte(a), []byte(b)) }

// A token contains only a routing tenant reference and 256 random secret bits;
// no person, contact, resource or step. Its complete bytes are hashed in storage.
func Token(tenant uuid.UUID, rng io.Reader) (port.Secret, error) {
	if tenant == uuid.Nil {
		return port.Secret{}, port.ErrInvalid
	}
	if rng == nil {
		rng = rand.Reader
	}
	raw := make([]byte, 48)
	copy(raw, tenant[:])
	if _, err := io.ReadFull(rng, raw[16:]); err != nil {
		return port.Secret{}, err
	}
	return port.NewSecret(base64.RawURLEncoding.EncodeToString(raw)), nil
}
func ParseToken(token port.Secret) (uuid.UUID, string, error) {
	raw := token.Reveal()
	if len(raw) != 64 {
		return uuid.Nil, "", port.ErrDenied
	}
	b, err := base64.RawURLEncoding.Strict().DecodeString(raw)
	if err != nil || len(b) != 48 {
		return uuid.Nil, "", port.ErrDenied
	}
	tenant, err := uuid.FromBytes(b[:16])
	if err != nil || tenant == uuid.Nil {
		return uuid.Nil, "", port.ErrDenied
	}
	return tenant, Hash(b), nil
}

// Derivation lets a durable idempotent delivery retry use the same code without
// persisting plaintext OTP. 256-bit server key is mandatory; challenge IDs random.
func Code(key []byte, id uuid.UUID) port.Secret {
	h := hmac.New(sha256.New, key)
	h.Write([]byte("guestlink-otp-v1"))
	h.Write(id[:])
	sum := h.Sum(nil)
	return port.NewSecret(fmt.Sprintf("%06d", binary.BigEndian.Uint64(sum[:8])%1000000))
}
func ValidCode(code port.Secret) bool {
	s := code.Reveal()
	if len(s) != 6 {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// DerivedToken is a PRF bearer secret recoverable for an idempotent issue retry.
// The random grant/session UUID and keyed purpose are not part of the public token.
func DerivedToken(key []byte, tenant, id uuid.UUID, purpose string) port.Secret {
	h := hmac.New(sha256.New, key)
	h.Write([]byte("guestlink-token-v1:" + purpose))
	h.Write(tenant[:])
	h.Write(id[:])
	raw := make([]byte, 48)
	copy(raw, tenant[:])
	copy(raw[16:], h.Sum(nil))
	return port.NewSecret(base64.RawURLEncoding.EncodeToString(raw))
}

func ValidOwner(name string) bool { return ownerPattern.MatchString(name) }
