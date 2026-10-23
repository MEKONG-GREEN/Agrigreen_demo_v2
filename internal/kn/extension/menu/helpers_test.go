package menu

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
)

// Đường dẫn tới nguồn và đầu ra compatibility (đọc tệp — không import gói của pf/party).
const (
	repoRoot        = "../../../.."
	goldenFixture   = repoRoot + "/apps/web-ops/src/api/mock/fixtures/kn-menu.json"
	generatedTables = repoRoot + "/internal/platform/party/domain/menu_departments.go"
	compatSnapshot  = "testdata/compat-menus.json"
)

// sourceFiles quét đúng các thư mục bộ sinh quét: internal/{platform,kn}/*/menus/**/*.json.
func sourceFiles(t *testing.T) []string {
	t.Helper()
	var files []string
	for _, area := range []string{"platform", "kn"} {
		base := filepath.Join(repoRoot, "internal", area)
		modules, err := os.ReadDir(base)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range modules {
			dir := filepath.Join(base, m.Name(), "menus")
			if _, err := os.Stat(dir); err != nil {
				continue
			}
			err := filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
				if err == nil && !d.IsDir() && strings.HasSuffix(p, ".json") {
					files = append(files, p)
				}
				return err
			})
			if err != nil {
				t.Fatal(err)
			}
		}
	}
	slices.Sort(files)
	return files
}

func sources(t *testing.T) [][]byte {
	t.Helper()
	var out [][]byte
	for _, f := range sourceFiles(t) {
		raw, err := os.ReadFile(f) //nolint:gosec // tệp nguồn cố định của repo
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, raw)
	}
	return out
}

func catalog(t *testing.T) *Catalog {
	t.Helper()
	c, err := New(sources(t)...)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

type forms bool

func (f forms) HasForms(context.Context, string, string, string) (bool, error) { return bool(f), nil }

// viewItem/view là thân menu sau bước shared (party app registeredMenu): lọc module tắt, bỏ mục cha/nhóm rỗng,
// rồi chọn trang đích bằng NavigationPolicy trên mục lá đang bật (không bộ chuẩn hóa compatibility).
type viewItem struct {
	Key         string     `json:"key"`
	Label       string     `json:"label"`
	Route       string     `json:"route"`
	Icon        string     `json:"icon"`
	BadgeSource string     `json:"badge_source"`
	Enabled     bool       `json:"enabled"`
	Status      string     `json:"status"`
	StatusLabel string     `json:"status_label"`
	Children    []viewItem `json:"children"`
}

type viewGroup struct {
	Key   string     `json:"key"`
	Label string     `json:"label"`
	Items []viewItem `json:"items"`
}

type view struct {
	Version      string      `json:"menu_version"`
	DefaultRoute string      `json:"default_route"`
	Groups       []viewGroup `json:"groups"`
	Denied       bool        `json:"denied,omitempty"`
}

var errNoPage = errors.New("không có trang khả dụng trong menu")

// sharedView mô phỏng phần shared sau Menus.Build (internal/platform/party/app/menu_registry.go + navigation.go).
func sharedView(def partyport.MenuDefinition, request partyport.MenuBuildRequest, disabled map[string]bool, policy partyport.NavigationPolicy) (view, error) {
	var convert func([]partyport.MenuItem) []viewItem
	convert = func(items []partyport.MenuItem) []viewItem {
		out := []viewItem{}
		for _, it := range items {
			if len(it.Children) == 0 && disabled[it.Module] {
				continue
			}
			kids := convert(it.Children)
			if len(it.Children) > 0 && len(kids) == 0 {
				continue
			}
			out = append(out, viewItem{Key: it.Key, Label: it.Label, Route: it.Route, Icon: it.Icon, BadgeSource: it.BadgeSource,
				Enabled: it.Enabled, Status: it.Status, StatusLabel: it.StatusLabel, Children: kids})
		}
		return out
	}
	v := view{Version: def.Version, DefaultRoute: def.DefaultRoute, Groups: []viewGroup{}}
	for _, g := range def.Groups {
		if items := convert(g.Items); len(items) > 0 {
			v.Groups = append(v.Groups, viewGroup{Key: g.Key, Label: g.Label, Items: items})
		}
	}
	preferred, err := policy(partyport.NavigationRequest{Audience: request.Audience, UnitRef: request.UnitRef, Department: request.Department})
	if err != nil {
		return view{}, err
	}
	var routes []string
	var visit func([]viewItem)
	visit = func(items []viewItem) {
		for _, it := range items {
			if !it.Enabled {
				continue
			}
			if len(it.Children) > 0 {
				visit(it.Children)
				continue
			}
			if strings.HasPrefix(it.Route, "/") && !strings.HasPrefix(it.Route, "//") {
				routes = append(routes, it.Route)
			}
		}
	}
	for _, g := range v.Groups {
		visit(g.Items)
	}
	for _, want := range preferred {
		if slices.Contains(routes, want) {
			v.DefaultRoute = want
			return v, nil
		}
	}
	if len(routes) == 0 {
		return view{}, errNoPage
	}
	v.DefaultRoute = routes[0]
	return v, nil
}
