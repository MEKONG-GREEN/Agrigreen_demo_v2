package extension

import (
	"embed"
	"encoding/json"

	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
)

//go:embed menus/base.json menus/navigation.json
var navigationRegistration embed.FS

// NavigationPolicy registers KN landing preferences from the same owner data
// compiled into menus and mock-server fixtures. It never consumes client routes.
func NavigationPolicy() (partyport.NavigationPolicy, error) {
	var base struct {
		Tables struct {
			Defaults map[string]string `json:"defaultRoutes"`
		}
	}
	var contextual struct {
		Tables struct {
			Profiles map[string]map[string]string `json:"contextDefaultRoutes"`
		}
	}
	for _, item := range []struct {
		path  string
		value any
	}{{"menus/base.json", &base}, {"menus/navigation.json", &contextual}} {
		raw, err := navigationRegistration.ReadFile(item.path)
		if err != nil {
			return nil, err
		}
		if err = json.Unmarshal(raw, item.value); err != nil {
			return nil, err
		}
	}
	return partyport.RegisteredNavigation(base.Tables.Defaults, contextual.Tables.Profiles)
}
