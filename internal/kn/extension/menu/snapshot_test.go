package menu

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"

	"github.com/google/uuid"

	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
)

type snapDept struct {
	Code        string `json:"code"`
	Name        string `json:"name"`
	Status      string `json:"status"`
	MenuProfile string `json:"menu_profile"`
	Retired     bool   `json:"retired"`
}

type snapCase struct {
	Name         string     `json:"name"`
	Mode         string     `json:"mode"`
	Audience     string     `json:"audience"`
	Department   *snapDept  `json:"department"`
	Departments  []snapDept `json:"departments"`
	FilterTraits bool       `json:"filter_traits"`
	Manager      bool       `json:"manager"`
	Duties       []string   `json:"duties"`
	Forms        bool       `json:"forms"`
	Disabled     []string   `json:"disabled"`
	Result       string     `json:"result"`
	Denied       bool       `json:"denied"`
}

// Ảnh chụp đầu ra compatibility (testdata/compat-menus.json, sinh từ party domain/app ở dev a943fbaf — xem
// docs/kn/gia-dinh/KNSPLIT-2-MENU-KN.md): mọi đối tượng × phòng ban (6 bộ menu + 5 alias generic/rỗng) × chế độ
// MenuDepartment/MenuAdmin/MenuDepartmentModules × traits × form availability × module tắt. Menus.Build của KN +
// bước shared phải cho thân menu giống từng byte (JSON) và từ chối đúng các ca compatibility từ chối.
func TestBuilderMatchesCompatibilitySnapshot(t *testing.T) {
	raw, err := os.ReadFile(compatSnapshot)
	if err != nil {
		t.Fatal(err)
	}
	var snap struct {
		Version string          `json:"version"`
		Unit    uuid.UUID       `json:"unit"`
		Cases   []snapCase      `json:"cases"`
		Results map[string]view `json:"results"`
	}
	if err := json.Unmarshal(raw, &snap); err != nil {
		t.Fatal(err)
	}
	if snap.Version != Version || len(snap.Cases) < 600 {
		t.Fatalf("snapshot version %s, %d cases", snap.Version, len(snap.Cases))
	}
	c := catalog(t)
	reg, err := c.Registry()
	if err != nil {
		t.Fatal(err)
	}
	policy := c.NavigationPolicy()
	toPort := func(d snapDept) partyport.Department {
		return partyport.Department{OrgRef: snap.Unit, Code: d.Code, Name: d.Name, Status: d.Status, MenuProfile: d.MenuProfile, Retired: d.Retired}
	}
	modes, denied := map[string]int{}, 0
	for i, sc := range snap.Cases {
		request := partyport.MenuBuildRequest{NavigationRequest: partyport.NavigationRequest{Audience: sc.Audience, UnitRef: snap.Unit,
			FilterTraits: sc.FilterTraits, Manager: sc.Manager, Duties: sc.Duties}, Mode: partyport.MenuMode(sc.Mode)}
		if sc.Department != nil {
			request.Department = toPort(*sc.Department)
		}
		for _, d := range sc.Departments {
			request.Departments = append(request.Departments, toPort(d))
		}
		if request.Mode == partyport.MenuDepartmentModules {
			request.Departments = []partyport.Department{request.Department}
		}
		if sc.Forms {
			c.UseFormAvailability(forms(true))
		} else {
			c.UseFormAvailability(nil)
		}
		disabled := map[string]bool{}
		for _, m := range sc.Disabled {
			disabled[m] = true
		}
		def, err := reg.Build(context.Background(), request)
		if err != nil {
			t.Fatalf("#%d %s: build: %v", i, sc.Name, err)
		}
		got, err := sharedView(def, request, disabled, policy)
		if sc.Denied {
			denied++
			if !errors.Is(err, errNoPage) {
				t.Fatalf("#%d %s: want compatibility denial (no page), got %v", i, sc.Name, err)
			}
			continue
		}
		if err != nil {
			t.Fatalf("#%d %s: %v", i, sc.Name, err)
		}
		want, ok := snap.Results[sc.Result]
		if !ok {
			t.Fatalf("#%d %s: missing result %s", i, sc.Name, sc.Result)
		}
		gj, _ := json.Marshal(got)
		wj, _ := json.Marshal(want)
		if !bytes.Equal(gj, wj) {
			t.Fatalf("#%d %s (disabled %v, traits %v/%v/%v, forms %v)\n got %s\nwant %s", i, sc.Name, sc.Disabled, sc.FilterTraits, sc.Manager, sc.Duties, sc.Forms, gj, wj)
		}
		modes[sc.Mode]++
	}
	c.UseFormAvailability(nil)
	t.Logf("matched %v, denied %d", modes, denied)
	if modes[string(partyport.MenuDepartment)] == 0 || modes[string(partyport.MenuAdmin)] == 0 || modes[string(partyport.MenuDepartmentModules)] == 0 {
		t.Fatalf("mode coverage %v", modes)
	}
}
