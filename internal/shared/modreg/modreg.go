// Package modreg đọc và kiểm các tệp khai báo module `module.yaml` (ADR-0027,
// kiến trúc v4.1 §7.1) của mọi module internal/{platform,mkg,vault}/*.
//
// Gói chỉ kiểm LƯỢC ĐỒ từng tệp và tính nhất quán giữa các tệp (tên duy nhất,
// depends trỏ tới module có thật, ràng buộc phiên bản thỏa). Kiểm hướng đồ thị
// theo tầng và chu trình là việc của analyzer agmodgraph (W1.2); bật module theo
// đơn vị là W1.4. Hướng dẫn: docs/dev/modules.md.
package modreg

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"regexp"
	"slices"
	"sort"
	"strings"

	"go.yaml.in/yaml/v3"
)

// FileName là tên tệp khai báo, nằm cạnh module.go.
const FileName = "module.yaml"

// Kind là tầng của module (R-MOD-05): lõi ← năng lực ← nghiệp vụ.
type Kind string

// Các tầng hợp lệ. Tầng ứng dụng khai trong app.yaml (W1.3), không phải module.yaml.
const (
	KindKernel     Kind = "kernel"
	KindCapability Kind = "capability"
	KindDomain     Kind = "domain"
)

// Rank là thứ bậc tầng: số nhỏ ở dưới. Dùng cho W1.2 (chỉ phụ thuộc đi xuống).
func (k Kind) Rank() int {
	switch k {
	case KindKernel:
		return 0
	case KindCapability:
		return 1
	case KindDomain:
		return 2
	}
	return -1
}

// Enable là chính sách bật module (R-MOD-08).
type Enable string

// Chính sách bật.
const (
	EnableAlways    Enable = "always"     // luôn bật, không tắt theo đơn vị (lõi)
	EnablePerTenant Enable = "per_tenant" // bật/tắt theo đơn vị tham gia (W1.4)
)

// Zones là các cụm dữ liệu (vùng) mà module có thể chạy: pf (nền tảng), mkg
// (nội bộ), vault (kho định danh), ts (chuỗi thời gian).
var Zones = []string{"pf", "mkg", "vault", "ts"}

// zoneDirs ánh xạ thư mục internal/<dir> sang tiền tố tên module.
var zoneDirs = map[string]string{"platform": "pf", "mkg": "mkg", "vault": "vault"}

// Provides là những gì module cung cấp cho module khác.
type Provides struct {
	Ports          []string `yaml:"ports"`           // interface xuất trong port/ (vault: client/)
	Events         []string `yaml:"events"`          // //ag:event, dạng <tên>/v<N>
	InternalEvents []string `yaml:"internal_events"` // //ag:internal-event, dạng <tên>/v<N>
	Publications   []string `yaml:"publications"`    // //ag:publication (contracts/publication)
	DataProducts   []string `yaml:"data_products"`   // //ag:dataproduct (contracts/dataproduct)
}

// Consumes là những gì module dùng của module khác mà KHÔNG tạo phụ thuộc gọi (§7.1 `consumes`): sự kiện
// nhận qua hàng đợi. Không thuộc đồ thị depends nên không chịu R-MOD-05 (chỉ đi xuống); mỗi sự kiện phải do
// module phát khai trong provides.events / provides.internal_events.
type Consumes struct {
	Events []string `yaml:"events"` // <tên>/v<N>, cùng dạng provides.events
}

// Manifest là nội dung một module.yaml.
type Manifest struct {
	Name        string            `yaml:"name"` // tên repo, vd pf/party
	Aka         []string          `yaml:"aka"`  // tên v4.1 (Phụ lục D), vd k/orgtree
	Kind        Kind              `yaml:"kind"`
	Zones       []string          `yaml:"zones"`
	Version     string            `yaml:"version"`
	Depends     map[string]string `yaml:"depends"` // tên module → ràng buộc phiên bản
	Provides    Provides          `yaml:"provides"`
	Consumes    Consumes          `yaml:"consumes"`
	Permissions []string          `yaml:"permissions"` // vai realm do module khai trong port/
	Shares      []string          `yaml:"shares"`
	OwnsSchema  []string          `yaml:"owns_schema"`
	Enable      Enable            `yaml:"enable"`

	// Path là đường dẫn tệp (gạch chéo xuôi, tương đối gốc repo) — không đọc từ YAML.
	Path string `yaml:"-"`
}

// Dir là thư mục module tương đối gốc repo, vd internal/platform/party.
func (m *Manifest) Dir() string { return path.Dir(m.Path) }

// SemVer trả phiên bản đã phân tích (Manifest đã qua Validate).
func (m *Manifest) SemVer() Version {
	v, _ := ParseVersion(m.Version)
	return v
}

// DependNames trả tên các module phụ thuộc, đã sắp.
func (m *Manifest) DependNames() []string {
	out := make([]string, 0, len(m.Depends))
	for k := range m.Depends {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// NameForDir trả tên module từ thư mục internal/<zone>/<mod> ("" nếu không phải thư mục module).
func NameForDir(dir string) string {
	parts := strings.Split(path.Clean(dir), "/")
	if len(parts) != 3 || parts[0] != "internal" {
		return ""
	}
	prefix, ok := zoneDirs[parts[1]]
	if !ok {
		return ""
	}
	return prefix + "/" + parts[2]
}

// Parse đọc một module.yaml; trường lạ là lỗi (bắt lỗi gõ). Tệp phải có đúng một
// tài liệu YAML. p là đường dẫn để báo lỗi.
func Parse(data []byte, p string) (*Manifest, error) {
	data = bytes.TrimPrefix(data, []byte("\xef\xbb\xbf")) // BOM của trình soạn thảo Windows
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	var m Manifest
	if err := dec.Decode(&m); err != nil {
		if errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("%s: tệp rỗng", p)
		}
		return nil, fmt.Errorf("%s: %w", p, err)
	}
	var extra yaml.Node
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("%s: chỉ được có một tài liệu YAML (thừa sau ---)", p)
	}
	m.Path = p
	return &m, nil
}

var (
	reName    = regexp.MustCompile(`^(pf|mkg|vault)/[a-z][a-z0-9]*$`)
	reAka     = regexp.MustCompile(`^(k|cap|dom|mkg)/[a-z][a-z0-9-]*$`)
	reEvent   = regexp.MustCompile(`^[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)+/v[1-9][0-9]*$`)
	reIdent   = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_.]*$`)
	reSnake   = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)
	reSchema  = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)
	schemaZon = []struct{ prefix, zone string }{{"pf_", "pf"}, {"mkg_", "mkg"}, {"ts_", "ts"}, {"vault", "vault"}}
)

// Validate kiểm lược đồ một tệp (không cần biết module khác).
func (m *Manifest) Validate() error {
	var errs []error
	bad := func(format string, a ...any) {
		errs = append(errs, fmt.Errorf("%s: %s", m.Path, fmt.Sprintf(format, a...)))
	}

	if !reName.MatchString(m.Name) {
		bad("name %q phải có dạng pf|mkg|vault/<module>", m.Name)
	}
	if m.Path != "" {
		if want := NameForDir(m.Dir()); want != m.Name {
			bad("name %q không khớp thư mục (cần %q)", m.Name, want)
		}
	}
	for _, a := range m.Aka {
		if !reAka.MatchString(a) {
			bad("aka %q phải có dạng k|cap|dom|mkg/<tên>", a)
		}
	}
	if m.Kind.Rank() < 0 {
		bad("kind %q phải là kernel | capability | domain", m.Kind)
	}
	if len(m.Zones) == 0 {
		bad("zones bắt buộc")
	}
	for _, z := range m.Zones {
		if !slices.Contains(Zones, z) {
			bad("zone %q phải thuộc %v", z, Zones)
		}
	}
	checkUnique(m.Zones, "zones", bad)
	if _, err := ParseVersion(m.Version); err != nil {
		bad("version: %v", err)
	}
	for dep, c := range m.Depends {
		if dep == m.Name {
			bad("depends: module không phụ thuộc chính nó")
		}
		if !reName.MatchString(dep) {
			bad("depends: tên module %q không hợp lệ", dep)
		}
		if _, err := ParseConstraint(c); err != nil {
			bad("depends %s: %v", dep, err)
		}
	}
	listCheck := func(field string, xs []string, re *regexp.Regexp) {
		for _, x := range xs {
			if !re.MatchString(x) {
				bad("%s: %q không hợp lệ", field, x)
			}
		}
		checkUnique(xs, field, bad)
	}
	listCheck("provides.ports", m.Provides.Ports, reIdent)
	listCheck("provides.events", m.Provides.Events, reEvent)
	listCheck("provides.internal_events", m.Provides.InternalEvents, reEvent)
	listCheck("provides.publications", m.Provides.Publications, reSnake)
	listCheck("provides.data_products", m.Provides.DataProducts, reSnake)
	listCheck("consumes.events", m.Consumes.Events, reEvent)
	for _, e := range m.Consumes.Events {
		if EventProducer(e) == m.Name {
			bad("consumes.events %q: module không nhận sự kiện của chính nó qua consumes", e)
		}
	}
	listCheck("permissions", m.Permissions, reSnake)
	listCheck("shares", m.Shares, reSnake)
	listCheck("owns_schema", m.OwnsSchema, reSchema)
	for _, s := range m.OwnsSchema {
		// Schema lõi k_<x> (v4.1 §13: k_ref, k_ledger, k_jobs) có ở MỌI cụm dữ liệu module chạy — chỉ module
		// kernel được sở hữu (W2.5: k_ref → pf/kref, ADR-0029).
		if strings.HasPrefix(s, "k_") {
			if m.Kind != KindKernel {
				bad("owns_schema %q: schema lõi k_* chỉ thuộc module kernel", s)
			}
			continue
		}
		if z := schemaZone(s); z == "" {
			bad("owns_schema %q: tiền tố phải là pf_, mkg_, ts_ hoặc vault", s)
		} else if !slices.Contains(m.Zones, z) {
			bad("owns_schema %q thuộc cụm %s nhưng zones là %v", s, z, m.Zones)
		}
	}
	switch m.Enable {
	case EnableAlways, EnablePerTenant:
	default:
		bad("enable %q phải là always | per_tenant", m.Enable)
	}
	if m.Kind == KindKernel && m.Enable != EnableAlways {
		bad("module lõi (kernel) phải enable: always")
	}
	return errors.Join(errs...)
}

func schemaZone(s string) string {
	for _, sz := range schemaZon {
		if s == sz.prefix || (strings.HasSuffix(sz.prefix, "_") && strings.HasPrefix(s, sz.prefix)) {
			return sz.zone
		}
	}
	return ""
}

func checkUnique(xs []string, field string, bad func(string, ...any)) {
	seen := map[string]bool{}
	for _, x := range xs {
		if seen[x] {
			bad("%s: %q lặp", field, x)
		}
		seen[x] = true
	}
}

// Registry là tập module đã kiểm, dùng cho agmodgraph (W1.2), bật module (W1.4).
type Registry struct {
	byName map[string]*Manifest
	names  []string
}

// New kiểm từng manifest và tính nhất quán giữa chúng.
func New(ms []*Manifest) (*Registry, error) {
	r := &Registry{byName: map[string]*Manifest{}}
	var errs []error
	for _, m := range ms {
		if err := m.Validate(); err != nil {
			errs = append(errs, err)
		}
		if prev, ok := r.byName[m.Name]; ok {
			errs = append(errs, fmt.Errorf("%s: name %q trùng với %s", m.Path, m.Name, prev.Path))
			continue
		}
		r.byName[m.Name] = m
		r.names = append(r.names, m.Name)
	}
	sort.Strings(r.names)
	schemaOwner := map[string]string{}
	for _, name := range r.names {
		m := r.byName[name]
		for _, dep := range m.DependNames() {
			d, ok := r.byName[dep]
			if !ok {
				errs = append(errs, fmt.Errorf("%s: depends %q: không có module này", m.Path, dep))
				continue
			}
			c, err := ParseConstraint(m.Depends[dep])
			dv, verr := ParseVersion(d.Version)
			if err == nil && verr == nil && !c.Allows(dv) {
				errs = append(errs, fmt.Errorf("%s: depends %s %q không thỏa phiên bản hiện có %s", m.Path, dep, c, dv))
			}
		}
		for _, s := range m.OwnsSchema {
			if o, ok := schemaOwner[s]; ok {
				errs = append(errs, fmt.Errorf("%s: owns_schema %q đã thuộc %s", m.Path, s, o))
			}
			schemaOwner[s] = m.Name
		}
		for _, e := range m.Consumes.Events {
			p, ok := r.byName[EventProducer(e)]
			switch {
			case !ok:
				errs = append(errs, fmt.Errorf("%s: consumes.events %q: không có module phát %q", m.Path, e, EventProducer(e)))
			case slices.Contains(p.Provides.InternalEvents, e):
				// Sự kiện nội bộ MKG (ADR-0018) chỉ lên vhost mkg.internal: module vùng khác không nhận được.
				if !slices.Contains(m.Zones, "mkg") {
					errs = append(errs, fmt.Errorf("%s: consumes.events %q là sự kiện nội bộ MKG, module không chạy ở vùng mkg", m.Path, e))
				}
			case !slices.Contains(p.Provides.Events, e):
				errs = append(errs, fmt.Errorf("%s: consumes.events %q: %s không khai sự kiện này trong provides", m.Path, e, p.Name))
			}
		}
	}
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}
	return r, nil
}

// Glob là mẫu tìm module.yaml tương đối gốc repo.
var Glob = []string{
	"internal/platform/*/" + FileName,
	"internal/mkg/*/" + FileName,
	"internal/vault/*/" + FileName,
}

// LoadFS đọc mọi module.yaml trong fsys (gốc là gốc repo) rồi kiểm.
func LoadFS(fsys fs.FS) (*Registry, error) {
	ms, err := ReadAll(fsys)
	if err != nil {
		return nil, err
	}
	return New(ms)
}

// Load đọc từ thư mục gốc repo trên đĩa (dùng trong tools/ và kiểm thử).
func Load(repoRoot string) (*Registry, error) { return LoadFS(os.DirFS(repoRoot)) }

// ReadAll đọc và phân tích mọi module.yaml, chưa kiểm.
func ReadAll(fsys fs.FS) ([]*Manifest, error) {
	var paths []string
	for _, g := range Glob {
		ps, err := fs.Glob(fsys, g)
		if err != nil {
			return nil, err
		}
		paths = append(paths, ps...)
	}
	sort.Strings(paths)
	var out []*Manifest
	var errs []error
	for _, p := range paths {
		data, err := fs.ReadFile(fsys, p)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		m, err := Parse(data, p)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		out = append(out, m)
	}
	return out, errors.Join(errs...)
}

// Names trả tên mọi module, đã sắp.
func (r *Registry) Names() []string { return slices.Clone(r.names) }

// Modules trả mọi manifest theo thứ tự tên.
func (r *Registry) Modules() []*Manifest {
	out := make([]*Manifest, 0, len(r.names))
	for _, n := range r.names {
		out = append(out, r.byName[n])
	}
	return out
}

// Get trả manifest theo tên.
func (r *Registry) Get(name string) (*Manifest, bool) {
	m, ok := r.byName[name]
	return m, ok
}

// Dependents trả các module khai depends tới name, đã sắp.
func (r *Registry) Dependents(name string) []string {
	var out []string
	for _, n := range r.names {
		if _, ok := r.byName[n].Depends[name]; ok {
			out = append(out, n)
		}
	}
	return out
}

// ByKind trả tên các module theo tầng, đã sắp.
func (r *Registry) ByKind(k Kind) []string {
	var out []string
	for _, n := range r.names {
		if r.byName[n].Kind == k {
			out = append(out, n)
		}
	}
	return out
}

// EventProducer trả tên module phát sự kiện theo quy ước tên R-64 (<vùng>.<module>.<thực thể>.<động từ>),
// vd "mkg.fieldops.roll_count.recorded/v1" → "mkg/fieldops"; "" khi tên không đúng dạng.
func EventProducer(event string) string {
	parts := strings.SplitN(event, ".", 3)
	if len(parts) < 3 || !slices.Contains([]string{"pf", "mkg", "vault"}, parts[0]) {
		return ""
	}
	return parts[0] + "/" + parts[1]
}

// SchemaOwner trả module sở hữu schema (theo owns_schema).
func (r *Registry) SchemaOwner(schema string) (string, bool) {
	for _, n := range r.names {
		if slices.Contains(r.byName[n].OwnsSchema, schema) {
			return n, true
		}
	}
	return "", false
}
