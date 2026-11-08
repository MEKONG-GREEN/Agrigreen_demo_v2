package app

import (
	"encoding/base64"
	"errors"
	"strings"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/envelope"
)

// SupportConfig is deployment authority. No tenant, actor, role or recipient key comes from a public request.
type SupportConfig struct {
	TenantID   uuid.UUID
	SubjectRef uuid.UUID
	PublicKey  []byte
}

// SupportConfigFromEnv: all absent disables public intake; partial/invalid configuration aborts startup.
func SupportConfigFromEnv(get func(string) string) (SupportConfig, error) {
	tid, ref, key := strings.TrimSpace(get("AG_KN_SUPPORT_TENANT_ID")), strings.TrimSpace(get("AG_KN_SUPPORT_SUBJECT_REF")), strings.TrimSpace(get("AG_KN_SUPPORT_PUBLIC_KEY"))
	if tid == "" && ref == "" && key == "" {
		return SupportConfig{}, nil
	}
	bad := errors.New("contact: cấu hình KN support cần tenant UUID, subject UUID và khóa công khai X25519 base64 32 byte")
	t, e1 := uuid.Parse(tid)
	r, e2 := uuid.Parse(ref)
	k, e3 := base64.StdEncoding.DecodeString(key)
	if e1 != nil || e2 != nil || e3 != nil || t == uuid.Nil || r == uuid.Nil || len(k) != envelope.KeySize {
		return SupportConfig{}, bad
	}
	return SupportConfig{TenantID: t, SubjectRef: r, PublicKey: k}, nil
}
