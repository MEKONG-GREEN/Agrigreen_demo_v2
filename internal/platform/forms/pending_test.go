package forms

import (
	"context"
	"errors"
	"testing"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/port"
)

func TestPending293PublicRegistrationFailsClosed(t *testing.T) {
	profile := port.Profile{Name: "owner", Hierarchy: []string{"UPPER", "LEAF"}, Roles: map[string]string{"upper": "UPPER", "leaf": "LEAF"}, PublishBy: "UPPER", CloseBy: "UPPER", DefineBy: []string{"upper"}}
	m, e := New(nil, nil, Deps{Profiles: []port.Profile{profile}})
	if e != nil {
		t.Fatal(e)
	}
	if e = m.UsePendingPolicy("missing", nil); !errors.Is(e, port.ErrProfileNotFound) {
		t.Fatal(e)
	}
	if e = m.UsePendingPolicy("owner", nil); e != nil {
		t.Fatal(e)
	}
	page, e := m.PendingSubmissions().PendingSubmissions(context.Background(), port.PendingQuery{Profile: "owner", UnitKind: port.UnitCommune, Limit: 1})
	if !errors.Is(e, port.ErrPendingUnavailable) || page.CoverageComplete {
		t.Fatal("unwired supplier invented authority/coverage", e)
	}
}
