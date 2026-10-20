package domain

import (
	"encoding/json"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
)

// catalogValue decodes an independent value for each consumer. Module-owned JSON
// registrations are compiled by scripts/gen-module-menus.mjs, never loaded from disk at runtime.
func catalogValue[T any](name string) T {
	var tables map[string]json.RawMessage
	if err := json.Unmarshal([]byte(registeredMenuJSON), &tables); err != nil {
		panic(err)
	}
	raw, ok := tables[name]
	if !ok {
		panic("missing registered menu table: " + name)
	}
	var value T
	if err := json.Unmarshal(raw, &value); err != nil {
		panic(err)
	}
	return value
}

// RegisteredNavigation uses the same compiled owner registration for fixtures.
func RegisteredNavigation() (port.NavigationPolicy, error) {
	return port.RegisteredNavigation(catalogValue[map[string]string]("defaultRoutes"), catalogValue[map[string]map[string]string]("contextDefaultRoutes"))
}
