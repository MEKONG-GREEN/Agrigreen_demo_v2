// Test fixture copied from existing KN77 helpers only, without duplicating legacy Test cases.
// Colocated probes may inspect their own module internals; composition-root tests remain port-only.
package adhocintegration_test

import (
	"context"

	"os"

	"testing"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
)

func TestMain(m *testing.M) { os.Exit(pgtest.Run(m)) }

// These fixtures replace account middleware only. Use the actual owner registry
// for the selected-session contract; synthetic IDs are test-only resolved state.
func bindKNTestSession(t *testing.T, p authz.Principal) authz.Principal {
	t.Helper()
	if p.KN == nil {
		return p
	}
	copyContext := *p.KN
	if copyContext.ID == uuid.Nil {
		copyContext.ID = ids.New()
	}
	p.KN = &copyContext
	registry, err := authz.NewSessionRegistry(extension.SessionProjection())
	if err != nil {
		t.Fatal(err)
	}
	s, err := registry.ResolveSelected(context.Background(), p)
	if err != nil {
		t.Fatal(err)
	}
	p.SelectedSession = &s
	return p
}
