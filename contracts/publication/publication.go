// Package publication nạp và cưỡng chế hợp đồng van công bố MKG → nền tảng
// (mục 5 tài liệu kiến trúc; R-03). Mặc định là TỪ CHỐI: chỉ trường có trong
// hợp đồng được đi qua van.
//
// Hai lớp cưỡng chế dùng chung mã trong package này:
//   - lúc build: tools/contractcheck (Parse, MetaProblems) so struct Go chú thích
//     //ag:publication <tên> với hợp đồng — trường ngoài hợp đồng, sai kiểu,
//     thiếu người phê duyệt → CI hỏng;
//   - lúc chạy: module mkg gọi Contract.Validate trên JSON sắp xuất ra van —
//     khóa lạ, sai kiểu, thiếu trường bắt buộc → không xuất (Get đọc các hợp
//     đồng nhúng trong nhị phân).
//
// Dạng tệp: xem README.md cùng thư mục.
package publication

import (
	"bytes"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

//go:embed *.json
var files embed.FS

// TypeSet là trường "type" của JSON Schema: một chuỗi hoặc mảng chuỗi.
type TypeSet []string

// UnmarshalJSON nhận "string" hoặc ["string", "null"].
func (t *TypeSet) UnmarshalJSON(b []byte) error {
	var one string
	if err := json.Unmarshal(b, &one); err == nil {
		*t = TypeSet{one}
		return nil
	}
	var many []string
	if err := json.Unmarshal(b, &many); err != nil {
		return errors.New(`"type" phải là chuỗi hoặc mảng chuỗi`)
	}
	*t = many
	return nil
}

// Has cho biết tập kiểu có k. Tập rỗng = không ràng buộc kiểu.
func (t TypeSet) Has(k string) bool {
	for _, x := range t {
		if x == k {
			return true
		}
	}
	return false
}

// Node là tập con của JSON Schema mà van và cổng CI dùng.
type Node struct {
	Type        TypeSet          `json:"type,omitempty"`
	Format      string           `json:"format,omitempty"`
	Pattern     string           `json:"pattern,omitempty"`
	Enum        []any            `json:"enum,omitempty"`
	Required    []string         `json:"required,omitempty"`
	Properties  map[string]*Node `json:"properties,omitempty"`
	Items       *Node            `json:"items,omitempty"`
	MinItems    *int             `json:"minItems,omitempty"`
	Description string           `json:"description,omitempty"`
}

// Contract là một hợp đồng van công bố.
type Contract struct {
	Name    string `json:"name"`
	Version int    `json:"version"`
	// Owner là module sở hữu dữ liệu phía MKG, vd "mkg/hub".
	Owner string `json:"owner"`
	// ProposedBy, ApprovedBy: người đề xuất và người phê duyệt — phải khác nhau (R-56).
	ProposedBy string `json:"proposed_by"`
	ApprovedBy string `json:"approved_by"`
	ApprovedAt string `json:"approved_at"` // YYYY-MM-DD
	// Event là sự kiện mang payload qua hàng đợi: "<event_type> v<N>" (tùy chọn).
	Event string `json:"event,omitempty"`
	// Purpose giải thích vì sao dữ liệu được phép đi qua van.
	Purpose   string   `json:"purpose,omitempty"`
	Consumers []string `json:"consumers,omitempty"`
	// Fields là danh sách trường cấp một, dạng cũ (không kiểu). Có Properties
	// thì Fields (nếu có) phải trùng khóa của Properties.
	Fields []string `json:"fields,omitempty"`
	Node
}

var (
	nameRe  = regexp.MustCompile(`^[a-z][a-z0-9_]{2,63}$`)
	eventRe = regexp.MustCompile(`^((?:pf|mkg|vault|edge)\.[a-z_]+\.[a-z_]+\.[a-z_]+)\s+v(\d+)$`)
)

// Parse đọc một hợp đồng. Lỗi chỉ khi JSON hỏng; vấn đề nội dung xem MetaProblems.
func Parse(raw []byte) (*Contract, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var c Contract
	if err := dec.Decode(&c); err != nil {
		return nil, fmt.Errorf("publication: JSON không hợp lệ: %w", err)
	}
	return &c, nil
}

// EventType tách tên và phiên bản sự kiện từ Event; ok = false nếu không khai.
func (c *Contract) EventType() (name string, version int, ok bool) {
	m := eventRe.FindStringSubmatch(strings.TrimSpace(c.Event))
	if m == nil {
		return "", 0, false
	}
	_, _ = fmt.Sscanf(m[2], "%d", &version)
	return m[1], version, true
}

// AllowedFields là các trường cấp một được đi qua van.
func (c *Contract) AllowedFields() []string {
	if len(c.Properties) == 0 {
		return append([]string(nil), c.Fields...)
	}
	out := make([]string, 0, len(c.Properties))
	for k := range c.Properties {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// Typed cho biết hợp đồng có khai kiểu (properties) hay chỉ danh sách tên.
func (c *Contract) Typed() bool { return len(c.Properties) > 0 }

// MetaProblems kiểm siêu dữ liệu bắt buộc và tính nhất quán của hợp đồng.
func (c *Contract) MetaProblems() []string {
	var p []string
	if !nameRe.MatchString(c.Name) {
		p = append(p, "name phải khớp ^[a-z][a-z0-9_]{2,63}$")
	}
	if c.Version < 1 {
		p = append(p, "version phải là số nguyên ≥ 1")
	}
	for _, f := range []struct{ k, v string }{
		{"owner", c.Owner}, {"proposed_by", c.ProposedBy}, {"approved_by", c.ApprovedBy}, {"approved_at", c.ApprovedAt},
	} {
		if strings.TrimSpace(f.v) == "" {
			p = append(p, "thiếu "+f.k+" (mục 5: hợp đồng phải có chủ sở hữu, người đề xuất, người phê duyệt, ngày phê duyệt)")
		}
	}
	if c.ProposedBy != "" && c.ApprovedBy != "" && strings.EqualFold(strings.TrimSpace(c.ProposedBy), strings.TrimSpace(c.ApprovedBy)) {
		p = append(p, "approved_by phải khác proposed_by (R-56: không tự phê duyệt)")
	}
	if c.ApprovedAt != "" {
		if _, err := time.Parse("2006-01-02", c.ApprovedAt); err != nil {
			p = append(p, "approved_at phải có dạng YYYY-MM-DD")
		}
	}
	if c.Event != "" {
		if _, _, ok := c.EventType(); !ok {
			p = append(p, `event phải có dạng "<vùng>.<module>.<thực thể>.<động từ> v<N>"`)
		}
	}
	if len(c.Properties) == 0 && len(c.Fields) == 0 {
		p = append(p, "hợp đồng rỗng: cần properties (có kiểu) hoặc fields")
	}
	if len(c.Properties) > 0 && len(c.Fields) > 0 {
		keys := c.AllowedFields()
		f := append([]string(nil), c.Fields...)
		sort.Strings(f)
		if strings.Join(keys, ",") != strings.Join(f, ",") {
			p = append(p, "fields phải trùng khóa của properties")
		}
	}
	if len(c.Properties) > 0 {
		p = append(p, nodeProblems("", &c.Node)...)
	}
	return p
}

var knownTypes = map[string]bool{"string": true, "integer": true, "number": true, "boolean": true, "object": true, "array": true, "null": true}

func nodeProblems(path string, n *Node) []string {
	var p []string
	at := path
	if at == "" {
		at = "(gốc)"
	}
	for _, t := range n.Type {
		if !knownTypes[t] {
			p = append(p, fmt.Sprintf("%s: kiểu %q không hỗ trợ", at, t))
		}
	}
	if len(n.Type) == 0 && path != "" {
		p = append(p, at+": thiếu type (hợp đồng van phải khai kiểu cho mọi trường)")
	}
	if n.Pattern != "" {
		if _, err := regexp.Compile(n.Pattern); err != nil {
			p = append(p, at+": pattern không hợp lệ")
		}
	}
	for _, r := range n.Required {
		if _, ok := n.Properties[r]; !ok {
			p = append(p, fmt.Sprintf("%s: required %s không có trong properties", at, r))
		}
	}
	if n.Type.Has("array") && n.Items == nil {
		p = append(p, at+": mảng phải khai items")
	}
	keys := make([]string, 0, len(n.Properties))
	for k := range n.Properties {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		child := n.Properties[k]
		if child == nil {
			p = append(p, join(path, k)+": định nghĩa rỗng")
			continue
		}
		p = append(p, nodeProblems(join(path, k), child)...)
	}
	if n.Items != nil {
		p = append(p, nodeProblems(path+"[]", n.Items)...)
	}
	return p
}

func join(path, k string) string {
	if path == "" {
		return k
	}
	return path + "." + k
}

// ErrDenied bọc mọi lý do van từ chối một payload.
var ErrDenied = errors.New("publication: van từ chối payload")

// Validate kiểm JSON sắp xuất ra van theo hợp đồng: khóa không có trong hợp
// đồng (ở mọi cấp) → từ chối; thiếu trường bắt buộc, sai kiểu, sai enum,
// pattern, format uuid/date/date-time → từ chối.
func (c *Contract) Validate(payload []byte) error {
	dec := json.NewDecoder(bytes.NewReader(payload))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return fmt.Errorf("%w: JSON hỏng: %w", ErrDenied, err)
	}
	if !c.Typed() {
		obj, ok := v.(map[string]any)
		if !ok {
			return fmt.Errorf("%w: payload phải là object", ErrDenied)
		}
		for k := range obj {
			if !contains(c.Fields, k) {
				return fmt.Errorf("%w: trường %s ngoài hợp đồng %s", ErrDenied, k, c.Name)
			}
		}
		return nil
	}
	root := c.Node
	if len(root.Type) == 0 {
		root.Type = TypeSet{"object"}
	}
	if err := validate("", &root, v); err != nil {
		return fmt.Errorf("%w: hợp đồng %s: %w", ErrDenied, c.Name, err)
	}
	return nil
}

func contains(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}

func kindOf(v any) string {
	switch x := v.(type) {
	case nil:
		return "null"
	case string:
		return "string"
	case bool:
		return "boolean"
	case json.Number:
		if _, err := x.Int64(); err == nil && !strings.ContainsAny(x.String(), ".eE") {
			return "integer"
		}
		return "number"
	case map[string]any:
		return "object"
	case []any:
		return "array"
	}
	return "unknown"
}

func typeOK(ts TypeSet, k string) bool {
	if len(ts) == 0 || ts.Has(k) {
		return true
	}
	return k == "integer" && ts.Has("number")
}

var patternCache sync.Map

func validate(path string, n *Node, v any) error {
	at := path
	if at == "" {
		at = "(gốc)"
	}
	k := kindOf(v)
	if !typeOK(n.Type, k) {
		return fmt.Errorf("%s: kiểu %s, hợp đồng cần %v", at, k, []string(n.Type))
	}
	if len(n.Enum) > 0 && k != "null" {
		ok := false
		for _, e := range n.Enum {
			if fmt.Sprint(e) == fmt.Sprint(v) {
				ok = true
				break
			}
		}
		if !ok {
			return fmt.Errorf("%s: giá trị ngoài enum", at)
		}
	}
	switch x := v.(type) {
	case string:
		if n.Pattern != "" {
			re, ok := patternCache.Load(n.Pattern)
			if !ok {
				c, err := regexp.Compile(n.Pattern)
				if err != nil {
					return fmt.Errorf("%s: pattern hợp đồng hỏng", at)
				}
				re, _ = patternCache.LoadOrStore(n.Pattern, c)
			}
			if !re.(*regexp.Regexp).MatchString(x) {
				return fmt.Errorf("%s: không khớp pattern", at)
			}
		}
		switch n.Format {
		case "uuid":
			if _, err := uuid.Parse(x); err != nil {
				return fmt.Errorf("%s: không phải uuid", at)
			}
		case "date-time":
			if _, err := time.Parse(time.RFC3339Nano, x); err != nil {
				return fmt.Errorf("%s: không phải date-time RFC 3339", at)
			}
		case "date":
			if _, err := time.Parse("2006-01-02", x); err != nil {
				return fmt.Errorf("%s: không phải date", at)
			}
		}
	case map[string]any:
		if n.Properties != nil {
			for key := range x {
				if _, ok := n.Properties[key]; !ok {
					return fmt.Errorf("%s: trường ngoài hợp đồng (mặc định từ chối)", join(path, key))
				}
			}
		}
		for _, r := range n.Required {
			if _, ok := x[r]; !ok {
				return fmt.Errorf("%s: thiếu trường bắt buộc", join(path, r))
			}
		}
		keys := make([]string, 0, len(x))
		for key := range x {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			if child := n.Properties[key]; child != nil {
				if err := validate(join(path, key), child, x[key]); err != nil {
					return err
				}
			}
		}
	case []any:
		if n.MinItems != nil && len(x) < *n.MinItems {
			return fmt.Errorf("%s: ít hơn %d phần tử", at, *n.MinItems)
		}
		if n.Items != nil {
			for i, e := range x {
				if err := validate(fmt.Sprintf("%s[%d]", path, i), n.Items, e); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

var (
	loadOnce sync.Once
	loaded   map[string]*Contract
	loadErr  error
)

// Get trả hợp đồng nhúng theo tên. Hợp đồng thiếu siêu dữ liệu hoặc chưa phê
// duyệt thì coi như không có (mặc định từ chối).
func Get(name string) (*Contract, error) {
	loadOnce.Do(func() {
		loaded = map[string]*Contract{}
		entries, err := files.ReadDir(".")
		if err != nil {
			loadErr = err
			return
		}
		for _, e := range entries {
			if !strings.HasSuffix(e.Name(), ".json") {
				continue
			}
			raw, err := files.ReadFile(e.Name())
			if err != nil {
				loadErr = err
				return
			}
			c, err := Parse(raw)
			if err != nil {
				loadErr = fmt.Errorf("%s: %w", e.Name(), err)
				return
			}
			if len(c.MetaProblems()) == 0 {
				loaded[c.Name] = c
			}
		}
	})
	if loadErr != nil {
		return nil, loadErr
	}
	c, ok := loaded[name]
	if !ok {
		return nil, fmt.Errorf("%w: không có hợp đồng %q đã phê duyệt", ErrDenied, name)
	}
	return c, nil
}
