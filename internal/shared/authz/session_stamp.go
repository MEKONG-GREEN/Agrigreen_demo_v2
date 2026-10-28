package authz

import (
	"crypto/sha256"
	"encoding/json"

	"github.com/google/uuid"
)

// AuthorizationStamp binds cached authorization to the complete immutable owner
// projection. Payload bytes are opaque: shared code neither decodes nor logs them.
// The digest is not an authentication token and grants no permission by itself.
func (s Session) AuthorizationStamp() ([]byte, error) {
	if !s.Valid() {
		return nil, ErrInvalidSession
	}
	encoded, err := json.Marshal(struct {
		Profile string
		ID      uuid.UUID
		Scope   SessionScope
		Payload []byte
	}{s.profile, s.id, s.scope, s.payload})
	if err != nil {
		return nil, err
	}
	h := sha256.New()
	_, _ = h.Write([]byte("authz.session.authorization/1\n"))
	_, _ = h.Write(encoded)
	return h.Sum(nil), nil
}
