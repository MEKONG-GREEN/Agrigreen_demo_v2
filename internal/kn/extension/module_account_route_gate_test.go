package extension

import (
	"errors"
	"net/http"
	"testing"
)

// KN-STANDALONE-1-ADMIN-TOOLS (#264): cổng route của phiên KN đóng API công cụ nền tảng, mở hạ tầng KN còn dùng.
func TestAccountRouteGate(t *testing.T) {
	if AccountSessionPolicies(nil).RouteGate == nil {
		t.Fatal("chủ KN phải cấp RouteGate cho pf/accounts")
	}
	for _, c := range [][2]string{
		{http.MethodGet, "/v1/masterdata/params"}, {http.MethodPost, "/v1/masterdata/params/versions/:id/approve"},
		{http.MethodPost, "/v1/orgs"}, {http.MethodGet, "/v1/orgs/:ref/events"}, {http.MethodGet, "/v1/reports/schedules/:id/runs"},
		{http.MethodGet, "/v1/sync/conflicts/:id"}, {http.MethodGet, "/v1/geo/conflicts"}, {http.MethodPost, "/v1/sync/nudge"},
		{http.MethodGet, "/v1/devices"}, {http.MethodPost, "/v1/devices/:id/revoke"}, {http.MethodGet, "/v1/api-clients/:id/keys"},
		{http.MethodGet, "/v1/metering/egress/anomalies"},
	} {
		if err := accountRouteGate(c[0], c[1]); !errors.Is(err, ErrSurfaceRemoved) {
			t.Fatalf("%s %s phải bị đóng: %v", c[0], c[1], err)
		}
	}
	for _, c := range [][2]string{
		{http.MethodPost, "/v1/devices"}, {http.MethodPost, "/v1/devices/:id/push-token"}, {http.MethodPost, "/v1/sync/push"},
		{http.MethodGet, "/v1/sync/pull"}, {http.MethodGet, "/v1/masterdata/params/value"}, {http.MethodGet, "/v1/orgs/tree"},
		{http.MethodPost, "/v1/orgs/:ref/move"}, {http.MethodGet, "/v1/reports/forms"}, {http.MethodGet, "/v1/menu"},
		{http.MethodGet, "/v1/metering"}, {http.MethodGet, "/v1/orgsx"}, {http.MethodGet, ""},
	} {
		if err := accountRouteGate(c[0], c[1]); err != nil {
			t.Fatalf("%s %s là hạ tầng/bề mặt KN, không được đóng: %v", c[0], c[1], err)
		}
	}
}
