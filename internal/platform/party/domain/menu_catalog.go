package domain

import "encoding/json"

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
