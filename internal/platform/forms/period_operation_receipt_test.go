package forms_test

import (
	"context"
	"errors"
	"testing"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/port"
)

func TestPeriodOperations212_ModuleWithoutPoolFailsClosed(t *testing.T) {
	module, err := forms.New(nil, nil, forms.Deps{})
	if err != nil {
		t.Fatal(err)
	}
	capability := module.PeriodOperations()
	if capability == nil {
		t.Fatal("factory returned nil interface")
	}
	got, err := port.ExecutePeriodOperation(context.Background(), capability, port.PeriodOperationRequest{})
	if !errors.Is(err, port.ErrPeriodOperationUnavailable) || got.Outcome != port.PeriodOperationUnknown {
		t.Fatalf("factory missing pool did not fail closed: %+v %v", got, err)
	}
}
