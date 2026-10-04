package modreg

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"regexp"
	"slices"
	"sort"
	"strings"

	"go.yaml.in/yaml/v3"
)

// Tệp khai báo ỨNG DỤNG app/<APP>/app.yaml (ADR-0027 mục 3, kiến trúc v4.1 §7.2, gói W1.3).
// Ứng dụng là tổ hợp module mà người dùng thấy; đơn vị triển khai là nhị phân cmd/*.
// Một đơn vị có thể phục vụ nhiều ứng dụng. Cổng: tập module ghép trong cmd/<đơn vị>
// ⊆ hợp `modules` của các ứng dụng liệt kê đơn vị đó (CheckAssembly).

// AppFileName là tên tệp khai báo ứng dụng.
const AppFileName = "app.yaml"

// AppGlob là mẫu tìm app.yaml tương đối gốc repo.
const AppGlob = "app/*/" + AppFileName

// RealmFile là tệp realm Keycloak dùng để kiểm vai và client của app.yaml.
const RealmFile = "deploy/compose/keycloak/agrigreen-realm.json"

// AppNames là bảy ứng dụng của v4.1 (ADR-0027 bảng tầng).
var AppNames = []string{"KN", "HTX", "CGH", "SIM", "MKG", "NH", "PF"}

// App là nội dung một app.yaml.
type App struct {
	App         string `yaml:"app"`         // KN | HTX | CGH | SIM | MKG | NH | PF, trùng tên thư mục
	Description string `yaml:"description"` // một câu
	// Modules: tên module repo → ràng buộc phiên bản (R-MOD-06). Không dùng ký tự đại diện.
	Modules map[string]string `yaml:"modules"`
	// Roles: vai realm Keycloak ứng dụng dùng (seed vai, R-MOD-07). Phải có trong RealmFile.
	Roles []string `yaml:"roles"`
	// ContextRoles: vai NGỮ CẢNH PHIÊN không phải vai realm (vd KN P1: kn_tw, kn_tinh, kn_xa, admin).
	ContextRoles []string `yaml:"context_roles"`
	// Landing: vai (thuộc roles hoặc context_roles) → trang đích sau đăng nhập.
	Landing map[string]string `yaml:"landing"`
	// DeploymentUnits: nhị phân cmd/<đơn vị> phục vụ ứng dụng (dịch vụ, worker, job vận hành).
	DeploymentUnits []string `yaml:"deployment_units"`
	// Frontends: thư mục giao diện apps/<x> của ứng dụng (có thể dùng chung giữa ứng dụng).
	Frontends []string `yaml:"frontends"`
	// Clients: client OIDC trong RealmFile mà ứng dụng dùng.
	Clients []string `yaml:"clients"`

	// Path là đường dẫn tệp (gạch chéo xuôi, tương đối gốc repo) — không đọc từ YAML.
	Path string `yaml:"-"`
}

// ModuleNames trả tên module của ứng dụng, đã sắp.
func (a *App) ModuleNames() []string {
	out := make([]string, 0, len(a.Modules))
	for k := range a.Modules {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// ParseApp đọc một app.yaml; trường lạ là lỗi.
func ParseApp(data []byte, p string) (*App, error) {
	data = bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	var a App
	if err := dec.Decode(&a); err != nil {
		return nil, fmt.Errorf("%s: %w", p, err)
	}
	a.Path = p
	return &a, nil
}

var (
	reUnit     = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)
	reFrontend = regexp.MustCompile(`^apps/[a-z][a-z0-9-]*$`)
	reClient   = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)
)

// Validate kiểm lược đồ một app.yaml (không cần biết module, realm, cmd).
func (a *App) Validate() error {
	var errs []error
	bad := func(format string, args ...any) { errs = append(errs, fmt.Errorf(a.Path+": "+format, args...)) }

	if !slices.Contains(AppNames, a.App) {
		bad("app %q phải thuộc %v", a.App, AppNames)
	}
	if a.Path != "" {
		if dir := path.Base(path.Dir(a.Path)); dir != a.App {
			bad("app %q không khớp thư mục app/%s", a.App, dir)
		}
	}
	if strings.TrimSpace(a.Description) == "" {
		bad("description bắt buộc")
	}
	if len(a.Modules) == 0 {
		bad("modules bắt buộc")
	}
	for name, c := range a.Modules {
		if !reName.MatchString(name) {
			bad("modules: tên module %q không hợp lệ", name)
		}
		if _, err := ParseConstraint(c); err != nil {
			bad("modules %s: %v", name, err)
		}
	}
	if len(a.Roles) == 0 {
		bad("roles bắt buộc")
	}
	listCheck := func(field string, xs []string, re *regexp.Regexp) {
		for _, x := range xs {
			if !re.MatchString(x) {
				bad("%s: %q không hợp lệ", field, x)
			}
		}
		checkUnique(xs, field, bad)
	}
	listCheck("roles", a.Roles, reSnake)
	listCheck("context_roles", a.ContextRoles, reSnake)
	for _, r := range a.ContextRoles {
		if slices.Contains(a.Roles, r) {
			bad("context_roles: %q đã có trong roles", r)
		}
	}
	for r, route := range a.Landing {
		if !slices.Contains(a.Roles, r) && !slices.Contains(a.ContextRoles, r) {
			bad("landing: vai %q không có trong roles hoặc context_roles", r)
		}
		if !strings.HasPrefix(route, "/") {
			bad("landing %s: đường dẫn %q phải bắt đầu bằng /", r, route)
		}
	}
	if len(a.DeploymentUnits) == 0 {
		bad("deployment_units bắt buộc")
	}
	listCheck("deployment_units", a.DeploymentUnits, reUnit)
	listCheck("frontends", a.Frontends, reFrontend)
	listCheck("clients", a.Clients, reClient)
	return errors.Join(errs...)
}

// AppEnv là những gì app.yaml tham chiếu ngoài module: realm Keycloak, nhị phân cmd/*,
// thư mục giao diện apps/*.
type AppEnv struct {
	RealmRoles   map[string]bool
	RealmClients map[string]bool
	Units        map[string]bool // cmd/<đơn vị> có main.go
	Frontends    map[string]bool // apps/<x>
}

// Apps là tập app.yaml đã kiểm chéo với registry module và AppEnv.
type Apps struct {
	byName map[string]*App
	names  []string
}

// NewApps kiểm từng app.yaml và tham chiếu của nó: module có thật và ràng buộc phiên bản
// thỏa, vai/client có trong realm, đơn vị triển khai và giao diện có thật.
func NewApps(apps []*App, reg *Registry, env AppEnv) (*Apps, error) {
	s := &Apps{byName: map[string]*App{}}
	var errs []error
	for _, a := range apps {
		if err := a.Validate(); err != nil {
			errs = append(errs, err)
		}
		if prev, ok := s.byName[a.App]; ok {
			errs = append(errs, fmt.Errorf("%s: app %q trùng với %s", a.Path, a.App, prev.Path))
			continue
		}
		s.byName[a.App] = a
		s.names = append(s.names, a.App)
		for _, name := range a.ModuleNames() {
			m, ok := reg.Get(name)
			if !ok {
				errs = append(errs, fmt.Errorf("%s: modules %q: không có module này", a.Path, name))
				continue
			}
			c, err := ParseConstraint(a.Modules[name])
			if err == nil && !c.Allows(m.SemVer()) {
				errs = append(errs, fmt.Errorf("%s: modules %s %q không thỏa phiên bản hiện có %s", a.Path, name, c, m.SemVer()))
			}
		}
		missing := func(field string, xs []string, have map[string]bool, where string) {
			for _, x := range xs {
				if !have[x] {
					errs = append(errs, fmt.Errorf("%s: %s %q không có trong %s", a.Path, field, x, where))
				}
			}
		}
		missing("roles", a.Roles, env.RealmRoles, RealmFile)
		missing("clients", a.Clients, env.RealmClients, RealmFile)
		missing("deployment_units", a.DeploymentUnits, env.Units, "cmd/*/main.go")
		missing("frontends", a.Frontends, env.Frontends, "apps/*")
	}
	sort.Strings(s.names)
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}
	return s, nil
}

// Names trả tên ứng dụng, đã sắp.
func (s *Apps) Names() []string { return slices.Clone(s.names) }

// Get trả app.yaml theo tên.
func (s *Apps) Get(name string) (*App, bool) {
	a, ok := s.byName[name]
	return a, ok
}

// AppsOfUnit trả các ứng dụng liệt kê đơn vị triển khai unit, đã sắp.
func (s *Apps) AppsOfUnit(unit string) []string {
	var out []string
	for _, n := range s.names {
		if slices.Contains(s.byName[n].DeploymentUnits, unit) {
			out = append(out, n)
		}
	}
	return out
}

// ServedModules trả hợp `modules` của các ứng dụng liệt kê unit.
func (s *Apps) ServedModules(unit string) map[string]bool {
	out := map[string]bool{}
	for _, n := range s.AppsOfUnit(unit) {
		for m := range s.byName[n].Modules {
			out[m] = true
		}
	}
	return out
}

// CheckAssembly là cổng ADR-0027 mục 3. wired: đơn vị cmd/* → module được ghép trong tiến
// trình (import gói module, không tính port/ hay client/). Lỗi khi:
//   - đơn vị ghép module ngoài hợp `modules` của các ứng dụng liệt kê nó;
//   - đơn vị có trong wired nhưng không ứng dụng nào liệt kê;
//   - module của registry không thuộc ứng dụng nào;
//   - ứng dụng liệt kê module mà không đơn vị nào của nó ghép.
func (s *Apps) CheckAssembly(reg *Registry, wired map[string][]string) []error {
	var errs []error
	units := make([]string, 0, len(wired))
	for u := range wired {
		units = append(units, u)
	}
	sort.Strings(units)
	for _, u := range units {
		apps := s.AppsOfUnit(u)
		if len(apps) == 0 {
			errs = append(errs, fmt.Errorf("cmd/%s: không ứng dụng nào liệt kê trong deployment_units", u))
			continue
		}
		served := s.ServedModules(u)
		for _, m := range wired[u] {
			if !served[m] {
				errs = append(errs, fmt.Errorf("cmd/%s ghép %s nhưng %s không có trong modules của %v", u, m, m, apps))
			}
		}
	}
	inApp := map[string]bool{}
	for _, n := range s.names {
		a := s.byName[n]
		for _, m := range a.ModuleNames() {
			inApp[m] = true
			ok := false
			for _, u := range a.DeploymentUnits {
				if slices.Contains(wired[u], m) {
					ok = true
					break
				}
			}
			if !ok {
				errs = append(errs, fmt.Errorf("%s: module %s không được đơn vị nào trong %v ghép", a.Path, m, a.DeploymentUnits))
			}
		}
	}
	for _, m := range reg.Names() {
		if !inApp[m] {
			errs = append(errs, fmt.Errorf("module %s không thuộc ứng dụng nào (app/*/app.yaml)", m))
		}
	}
	return errs
}

// ReadApps đọc và phân tích mọi app/*/app.yaml, chưa kiểm.
func ReadApps(fsys fs.FS) ([]*App, error) {
	paths, err := fs.Glob(fsys, AppGlob)
	if err != nil {
		return nil, err
	}
	sort.Strings(paths)
	var out []*App
	var errs []error
	for _, p := range paths {
		data, err := fs.ReadFile(fsys, p)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		a, err := ParseApp(data, p)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		out = append(out, a)
	}
	return out, errors.Join(errs...)
}

// LoadAppEnv đọc realm Keycloak, các nhị phân cmd/*/main.go và thư mục apps/* từ fsys (gốc repo).
func LoadAppEnv(fsys fs.FS) (AppEnv, error) {
	env := AppEnv{RealmRoles: map[string]bool{}, RealmClients: map[string]bool{}, Units: map[string]bool{}, Frontends: map[string]bool{}}
	data, err := fs.ReadFile(fsys, RealmFile)
	if err != nil {
		return env, err
	}
	var realm struct {
		Roles struct {
			Realm []struct {
				Name string `json:"name"`
			} `json:"realm"`
		} `json:"roles"`
		Clients []struct {
			ClientID string `json:"clientId"`
		} `json:"clients"`
	}
	if err := json.Unmarshal(data, &realm); err != nil {
		return env, fmt.Errorf("%s: %w", RealmFile, err)
	}
	for _, r := range realm.Roles.Realm {
		env.RealmRoles[r.Name] = true
	}
	for _, c := range realm.Clients {
		env.RealmClients[c.ClientID] = true
	}
	mains, err := fs.Glob(fsys, "cmd/*/main.go")
	if err != nil {
		return env, err
	}
	for _, m := range mains {
		env.Units[path.Base(path.Dir(m))] = true
	}
	entries, err := fs.ReadDir(fsys, "apps")
	if err != nil {
		return env, err
	}
	for _, e := range entries {
		if e.IsDir() {
			env.Frontends["apps/"+e.Name()] = true
		}
	}
	return env, nil
}

// LoadApps đọc registry module, AppEnv và mọi app.yaml từ fsys rồi kiểm chéo.
func LoadApps(fsys fs.FS, reg *Registry) (*Apps, AppEnv, error) {
	env, err := LoadAppEnv(fsys)
	if err != nil {
		return nil, env, err
	}
	apps, err := ReadApps(fsys)
	if err != nil {
		return nil, env, err
	}
	s, err := NewApps(apps, reg, env)
	return s, env, err
}
