package extension

import (
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"slices"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/menu"
)

//go:embed menus/*.json
var menuSources embed.FS

// menuMembers là id tài liệu menu của các module thành viên ứng dụng KN mà gốc lắp ráp phải chuyển vào
// MenuCatalog (mỗi module giữ nguồn của mình: hrm.MenuSource, price.MenuSource, contact.MenuSource).
var menuMembers = []string{"hrm-menus", "price-menus", "contact-menus"}

// ErrMenuMembers: gốc lắp ráp thiếu/thừa nguồn menu của module thành viên KN.
var ErrMenuMembers = errors.New("extension: KN menu member sources mismatch")

// MenuCatalog dựng cây menu KN (KNSPLIT-2-MENU-KN) từ nguồn WH-A của pf/extension và các module thành viên.
// Gốc lắp ráp (#53) nối:
//
//	catalog, err := extension.MenuCatalog(hrm.MenuSource(), price.MenuSource(), contact.MenuSource())
//	menus, err := catalog.Registry()                 // party.Deps.Menus
//	policy := catalog.NavigationPolicy()             // party.Deps.NavigationPolicy (giải alias bộ menu)
//	catalog.UseFormAvailability(rep.FormAvailability())
func MenuCatalog(members ...[]byte) (*menu.Catalog, error) {
	sources, err := ownMenuSources()
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(members))
	for _, raw := range members {
		id, err := menu.SourceID(raw)
		if err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	slices.Sort(ids)
	want := slices.Clone(menuMembers)
	slices.Sort(want)
	if !slices.Equal(ids, want) {
		return nil, fmt.Errorf("%w: got %v, want %v", ErrMenuMembers, ids, want)
	}
	return menu.New(append(sources, members...)...)
}

func ownMenuSources() ([][]byte, error) {
	names, err := fs.Glob(menuSources, "menus/*.json")
	if err != nil {
		return nil, err
	}
	out := make([][]byte, 0, len(names))
	for _, name := range names {
		raw, err := menuSources.ReadFile(name)
		if err != nil {
			return nil, err
		}
		out = append(out, raw)
	}
	return out, nil
}
