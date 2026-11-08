package forms_test

import (
	"context"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/port"
	"testing"
)

func TestEntryOperations288_AdditiveModuleFailsClosed(t *testing.T) {
	m, err := forms.New(nil, nil, forms.Deps{})
	if err != nil {
		t.Fatal(err)
	}
	legacy := m.Entry()
	if legacy == nil || m.EntryOperations() == nil {
		t.Fatal("missing additive/legacy capability")
	}
	got, err := m.EntryOperations().ExecuteReviewOperation(context.Background(), port.ReviewOperationRequest{})
	if err == nil || got.Outcome != port.ReviewOperationUnknown {
		t.Fatalf("unavailable supplier did not fail closed: %+v %v", got, err)
	}
	if err = m.UseReviewOperationPolicy("unknown", nil); err == nil {
		t.Fatal("unregistered profile accepted policy")
	}
}
