package main

import (
	"testing"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/apiclient"
	apiclientport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/apiclient/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
)

// ADR-0022 mục 4: job apiclientport.JobKeyExpiry chỉ có khi đã cấu hình tenant đơn vị vận hành (Phase K);
// ĐTK 1 (không AG_PUBLIC_OPERATOR_TENANT) → không job.
func TestKeyExpiryJobRegistration_ADR0022(t *testing.T) {
	if js := apiclient.New(nil, clock.System{}, apiclient.Deps{}).Jobs(nil); len(js) != 0 {
		t.Fatalf("không tenant vận hành → không job: %v", js)
	}
	js := apiclient.New(nil, clock.System{}, apiclient.Deps{OperatorTenant: ids.New()}).Jobs(nil)
	if len(js) != 1 || js[0].Name != apiclientport.JobKeyExpiry || !js[0].RunOnStart || js[0].Run == nil {
		t.Fatalf("muốn đúng job %s: %+v", apiclientport.JobKeyExpiry, js)
	}
}
