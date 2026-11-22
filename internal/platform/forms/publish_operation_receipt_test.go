package forms_test

import (
	"context"
	"errors"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/port"
	"testing"
)

func TestPublishOperation224_ModuleRegistrationAndNoPool(t *testing.T) {
	profile := port.Profile{Name: "fx", Hierarchy: []string{"TINH", "XA"}, Roles: map[string]string{"tinh": "TINH", "xa": "XA"}, PublishBy: "TINH", CloseBy: "TINH", DefineBy: []string{"admin"}}
	module, err := forms.New(nil, nil, forms.Deps{Profiles: []port.Profile{profile}})
	if err != nil {
		t.Fatal(err)
	}
	if err = module.UsePublishOperationPolicy("unknown", nil); !errors.Is(err, port.ErrProfileNotFound) {
		t.Fatal("unknown owner profile accepted")
	}
	if err = module.UsePublishOperationPolicy("fx", nil); err != nil {
		t.Fatal(err)
	}
	got, err := port.ExecutePublishOperation(context.Background(), module.PublishOperations(), port.PublishOperationRequest{})
	if !errors.Is(err, port.ErrPublishOperationUnavailable) || got.Outcome != port.PublishOperationUnknown || len(got.Items) != 0 {
		t.Fatal("module without pool exposed success/fallback")
	}
}
