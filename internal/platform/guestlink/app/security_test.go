package app

import (
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/guestlink/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/guestlink/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/guestlink/store"
	"github.com/google/uuid"
	"strings"
	"testing"
	"time"
)

func TestGuest184MetadataProofCannotWidenBinding(t *testing.T) {
	tenant := uuid.New()
	s := &Service{cfg: Config{Tenant: tenant, Key: []byte(strings.Repeat("k", 32))}}
	g := store.Grant{ID: uuid.New(), Binding: port.Binding{Tenant: tenant, Owner: "pf/hrm", Subject: uuid.New(), Resource: uuid.New(), Step: "self"}, OwnerRevision: uuid.New(), ContactRevision: uuid.New(), Issuer: uuid.New(), Until: time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC)}
	g.Key = domain.BindingKey(g.Binding)
	g.LinkHash, _ = s.tokenHash(s.linkToken(&g), "link")
	if !s.validGrant(&g) {
		t.Fatal("original proof invalid")
	}
	ref := uuid.New()
	originalSession := s.sessionToken(&g, ref, g.Until)

	for _, kind := range []string{"tenant", "owner", "subject", "resource", "step", "owner_revision", "contact_revision", "expiry", "issuer", "id"} {
		t.Run(kind, func(t *testing.T) {
			forged := g
			switch kind {
			case "tenant":
				forged.Binding.Tenant = uuid.New()
			case "owner":
				forged.Binding.Owner = "pf/other"
			case "subject":
				forged.Binding.Subject = uuid.New()
			case "resource":
				forged.Binding.Resource = uuid.New()
			case "step":
				forged.Binding.Step = "leader"
			case "owner_revision":
				forged.OwnerRevision = uuid.New()
			case "contact_revision":
				forged.ContactRevision = uuid.New()
			case "expiry":
				forged.Until = forged.Until.Add(time.Hour)
			case "issuer":
				forged.Issuer = uuid.New()
			case "id":
				forged.ID = uuid.New()
			}
			forged.Key = domain.BindingKey(forged.Binding)
			if s.validGrant(&forged) {
				t.Fatal("metadata alteration preserved link proof")
			}
			if s.sessionToken(&forged, ref, g.Until).Reveal() == originalSession.Reveal() {
				t.Fatal("metadata alteration preserved session proof")
			}
		})
	}
	if s.sessionToken(&g, ref, g.Until.Add(time.Hour)).Reveal() == originalSession.Reveal() {
		t.Fatal("session expiry not authenticated")
	}
}
