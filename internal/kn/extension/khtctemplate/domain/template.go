// Package domain implements the five fixed KHTC outlines (BRD v2.3 FN-AD-04.03).
// Authoring and rendering share this definition without database or clock I/O.
package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/doctpl"
)

const (
	RuleVersion        = "khtc-template/v2.3-1"
	MaxNodes           = 256
	MaxDepth           = 6
	MaxBodyRunes       = 12000
	MaxBindings        = 32
	MaxDefinitionBytes = 1 << 20
	MaxRows            = 20000
	MaxOutputRunes     = 1 << 20
)

var Keys = []string{"mau_1", "mau_2", "mau_3", "mau_4", "mau_5"}
var Repeats = []string{"none", "plan_item", "program", "metric", "commodity", "form_bullet"}
var AdminFields = []string{"document_no", "place", "report_date", "recipient", "basis", "recipients", "authority", "signer_title", "signer"}
var idPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,79}$`)
var varPattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,39}$`)

// Binding references an approved reporting projection, never SQL or an expression.
type Binding struct {
	Variable string `json:"variable"`
	Source   string `json:"source"`
	Field    string `json:"field"`
	Ref      string `json:"ref,omitempty"`
}

type Node struct {
	ID          string    `json:"id"`
	Parent      string    `json:"parent"`
	Order       int       `json:"order"`
	Title       string    `json:"title"`
	Repeat      string    `json:"repeat"`
	Tab         string    `json:"tab,omitempty"` // previous | current | all
	Department  string    `json:"department,omitempty"`
	Preamble    string    `json:"preamble,omitempty"`
	Body        string    `json:"body"`
	Bindings    []Binding `json:"bindings"`
	TextSlot    bool      `json:"text_slot"`
	LockedAdmin []string  `json:"locked_admin"`
}

type Definition struct {
	Key   string `json:"key"`
	Nodes []Node `json:"nodes"`
}

type InputError struct{ Field, Message string }

func (e InputError) Error() string        { return e.Field + ": " + e.Message }
func invalid(field, message string) error { return InputError{field, message} }
func Fixed(key string) bool               { return slices.Contains(Keys, key) }

// Protected is computed from the official seed, never accepted as client metadata.
func Protected(key, id string) bool {
	for _, n := range Seed(key).Nodes {
		if n.ID == id {
			return n.Parent == "" || strings.Count(n.ID, ".") < 2
		}
	}
	return false
}
func Seeded(key, id string) bool {
	for _, n := range Seed(key).Nodes {
		if n.ID == id {
			return true
		}
	}
	return false
}

// Canonical does not mutate a caller's historical snapshot.
func Canonical(d Definition) Definition {
	raw, _ := json.Marshal(d)
	var out Definition
	_ = json.Unmarshal(raw, &out)
	sort.Slice(out.Nodes, func(i, j int) bool { return out.Nodes[i].ID < out.Nodes[j].ID })
	for i := range out.Nodes {
		sort.Slice(out.Nodes[i].Bindings, func(a, b int) bool { return out.Nodes[i].Bindings[a].Variable < out.Nodes[i].Bindings[b].Variable })
		sort.Strings(out.Nodes[i].LockedAdmin)
		if out.Nodes[i].Bindings == nil {
			out.Nodes[i].Bindings = []Binding{}
		}
		if out.Nodes[i].LockedAdmin == nil {
			out.Nodes[i].LockedAdmin = []string{}
		}
	}
	return out
}

func (d Definition) BytesHash() ([]byte, string, error) {
	raw, err := json.Marshal(Canonical(d))
	if err != nil {
		return nil, "", err
	}
	if len(raw) > MaxDefinitionBytes {
		return nil, "", invalid("definition", "Mẫu vượt giới hạn 1 MiB")
	}
	h := sha256.Sum256(raw)
	return raw, hex.EncodeToString(h[:]), nil
}

// Validate guards official levels and same-parent reordering against the previous version.
func Validate(d Definition, previous *Definition) error {
	if !Fixed(d.Key) {
		return invalid("key", "Chỉ cấu hình Mẫu 1–5")
	}
	if len(d.Nodes) == 0 || len(d.Nodes) > MaxNodes {
		return invalid("nodes", "Mẫu cần 1–256 mục")
	}
	nodes := map[string]Node{}
	children := map[string]int{}
	orders := map[string]bool{}
	for _, n := range d.Nodes {
		field := "nodes." + n.ID
		if !idPattern.MatchString(n.ID) {
			return invalid("nodes", "Mã mục không hợp lệ")
		}
		if _, found := nodes[n.ID]; found {
			return invalid(field, "Mã mục trùng")
		}
		if strings.TrimSpace(n.Title) == "" || utf8.RuneCountInString(n.Title) > 500 || strings.ContainsRune(n.Title, 0) {
			return invalid(field+".title", "Tên mục bắt buộc, tối đa 500 ký tự")
		}
		if n.Order < 1 || n.Order > 10000 {
			return invalid(field+".order", "STT cần là số nguyên từ 1 đến 10000")
		}
		orderKey := fmt.Sprintf("%s:%d", n.Parent, n.Order)
		if orders[orderKey] {
			return invalid(field+".order", "Thứ tự trong cùng mục cha bị trùng")
		}
		orders[orderKey] = true
		if !slices.Contains(Repeats, n.Repeat) {
			return invalid(field+".repeat", "Phạm vi lặp không hợp lệ")
		}
		if !slices.Contains([]string{"", "all", "previous", "current"}, n.Tab) {
			return invalid(field+".tab", "Nhóm kế hoạch không hợp lệ")
		}
		if !slices.Contains([]string{"", "trong_trot", "chan_nuoi_thuy_san", "hanh_chinh_tong_hop", "thong_tin_truyen_thong", "ke_hoach_tai_chinh"}, n.Department) {
			return invalid(field+".department", "Phòng không hợp lệ")
		}
		if utf8.RuneCountInString(n.Body) > MaxBodyRunes || utf8.RuneCountInString(n.Preamble) > 1000 || strings.ContainsRune(n.Body+n.Preamble, 0) {
			return invalid(field+".body", "Khuôn câu vượt giới hạn")
		}
		if n.Repeat == "none" && n.Preamble != "" {
			return invalid(field+".preamble", "Câu mở đầu chỉ dành cho mục lặp")
		}
		if err := validateBindings(n); err != nil {
			return err
		}
		nodes[n.ID] = n
		children[n.Parent]++
	}
	for _, n := range d.Nodes {
		if n.Parent == "" && !Seeded(d.Key, n.ID) {
			return invalid("nodes."+n.ID+".parent", "Chỉ thêm mục con trong khung mẫu cố định")
		}
		if children[n.ID] == 0 && strings.TrimSpace(n.Body) == "" {
			return invalid("nodes."+n.ID+".body", "Khuôn câu bắt buộc")
		}
		path := map[string]bool{n.ID: true}
		at := n
		for depth := 1; at.Parent != ""; depth++ {
			p, ok := nodes[at.Parent]
			if !ok || path[at.Parent] {
				return invalid("nodes."+n.ID+".parent", "Mục cha thiếu hoặc tạo vòng lặp")
			}
			if depth >= MaxDepth {
				return invalid("nodes."+n.ID+".parent", "Mục lục tối đa 6 cấp")
			}
			path[p.ID] = true
			at = p
		}
	}
	for _, original := range Seed(d.Key).Nodes {
		n, ok := nodes[original.ID]
		if !ok {
			return invalid("nodes", "Chỉ xóa mục con do Admin thêm")
		}
		if n.Parent != original.Parent {
			return invalid("nodes."+n.ID+".parent", "Không chuyển mục sang cha khác")
		}
		if Protected(d.Key, n.ID) && (n.Title != original.Title || n.Order != original.Order) {
			return invalid("nodes."+n.ID+".title", "Giữ tiêu đề và thứ tự cấp I/II của mẫu chính thức")
		}
	}
	if previous != nil {
		if previous.Key != d.Key {
			return invalid("key", "Không chuyển mục sang mẫu khác")
		}
		for _, old := range previous.Nodes {
			if n, ok := nodes[old.ID]; ok && n.Parent != old.Parent {
				return invalid("nodes."+n.ID+".parent", "Chỉ sắp xếp lại trong cùng cha")
			}
		}
	}
	_, _, err := d.BytesHash()
	return err
}

func validateBindings(n Node) error {
	field := "nodes." + n.ID + ".body"
	if len(n.Bindings) > MaxBindings {
		return invalid(field, "Tối đa 32 biến trong một mục")
	}
	vars := []doctpl.Var{}
	seen := map[string]bool{}
	for _, b := range n.Bindings {
		if !varPattern.MatchString(b.Variable) || seen[b.Variable] || !AllowedField(b.Source, b.Field) || len(b.Ref) > 200 || strings.ContainsRune(b.Ref, 0) {
			return invalid(field, "Biến thiếu hoặc nguồn/ô dữ liệu không hợp lệ")
		}
		if b.Source == "text" && !n.TextSlot {
			return invalid(field, "Biến lời văn cần bật vùng lời văn tự do")
		}
		seen[b.Variable] = true
		vars = append(vars, doctpl.Var{Key: b.Variable, Label: b.Variable})
	}
	locks := map[string]bool{}
	for _, key := range n.LockedAdmin {
		if !slices.Contains(AdminFields, key) || locks[key] {
			return invalid("nodes."+n.ID+".locked_admin", "Chỉ khóa thêm ô hành chính hợp lệ")
		}
		locks[key] = true
	}
	t := doctpl.Template{Key: "khtc.authoring", Version: 1, Owner: "pf/extension", Title: "Khuôn câu", Vars: vars, Body: []doctpl.Block{{Kind: doctpl.KindParagraph, Text: n.Body}, {Kind: doctpl.KindParagraph, Text: n.Preamble}}}
	if err := doctpl.Validate(t); err != nil {
		return invalid(field, "Khuôn câu có biến chưa gắn nguồn hoặc dấu ngoặc không hợp lệ")
	}
	return nil
}

func AllowedField(source, field string) bool {
	switch source {
	case "plan":
		return slices.Contains([]string{"item_name", "program_name", "roman", "unit", "planned", "department"}, field)
	case "auto", "form", "module":
		return slices.Contains([]string{"value", "content", "unit", "label", "department", "status", "people", "budget", "from_price", "to_price"}, field)
	case "calculated":
		return slices.Contains([]string{"year_to_date", "percent"}, field)
	case "admin":
		return slices.Contains(AdminFields, field)
	case "text":
		return field == "narrative"
	}
	return false
}

func Processing(n Node) string {
	set := map[string]bool{}
	for _, b := range n.Bindings {
		set[b.Source] = true
	}
	if len(set) == 0 {
		return "fixed"
	}
	if len(set) > 1 {
		return "mixed"
	}
	for k := range set {
		return k
	}
	return "fixed"
}

func RemovedTextSlots(previous, next Definition) []string {
	kept := map[string]bool{}
	for _, n := range next.Nodes {
		if n.TextSlot {
			kept[n.ID] = true
		}
	}
	out := []string{}
	for _, n := range previous.Nodes {
		if n.TextSlot && !kept[n.ID] {
			out = append(out, n.ID)
		}
	}
	return out
}
