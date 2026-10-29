package domain

import (
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/doctpl"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/docx"
)

type Token struct {
	Text     string `json:"text"`
	Variable string `json:"variable,omitempty"`
	Source   string `json:"source,omitempty"`
	Field    string `json:"field,omitempty"`
	Locked   bool   `json:"locked"`
}
type RenderedRow struct {
	Ref            string  `json:"ref"`
	Preamble       string  `json:"preamble,omitempty"`
	PreambleTokens []Token `json:"preamble_tokens,omitempty"`
	Status         string  `json:"status,omitempty"`
	Tokens         []Token `json:"tokens"`
}
type RenderedNode struct {
	ID         string        `json:"id"`
	Parent     string        `json:"parent"`
	Number     string        `json:"number"`
	Title      string        `json:"title"`
	Depth      int           `json:"depth"`
	Processing string        `json:"processing"`
	Protected  bool          `json:"protected"`
	Seeded     bool          `json:"seeded"`
	TextSlot   bool          `json:"text_slot"`
	Rows       []RenderedRow `json:"rows"`
}
type Rendered struct {
	Key         string         `json:"key"`
	Nodes       []RenderedNode `json:"nodes"`
	InputHash   string         `json:"input_hash"`
	Provisional []string       `json:"provisional"`
}

// Render uses actual captured status by default. Explicit scenarios are preview-only;
// callers must not persist a simulated scenario into a draft or an export.
func Render(d Definition, context port.KHTCTemplateContext, scenario string) (Rendered, error) {
	if err := Validate(d, nil); err != nil {
		return Rendered{}, err
	}
	if !slices.Contains([]string{"", "actual", "normal", "no_send", "unclosed", "no_activity", "provisional"}, scenario) {
		return Rendered{}, invalid("scenario", "Tình huống xem trước không hợp lệ")
	}
	count := 0
	for _, rows := range context.Lists {
		count += len(rows)
	}
	if count > MaxRows {
		return Rendered{}, invalid("period_id", "Dữ liệu lặp vượt giới hạn")
	}
	for _, v := range context.Values {
		if utf8.RuneCountInString(v) > doctpl.MaxValueRunes {
			return Rendered{}, invalid("period_id", "Giá trị nguồn vượt giới hạn")
		}
	}
	for _, rows := range context.Lists {
		for _, r := range rows {
			for _, v := range r.Values {
				if utf8.RuneCountInString(v) > doctpl.MaxValueRunes {
					return Rendered{}, invalid("period_id", "Giá trị dòng nguồn vượt giới hạn")
				}
			}
		}
	}
	children := map[string][]Node{}
	for _, n := range d.Nodes {
		children[n.Parent] = append(children[n.Parent], n)
	}
	for parent := range children {
		sort.Slice(children[parent], func(i, j int) bool { return children[parent][i].Order < children[parent][j].Order })
	}
	out := Rendered{Key: d.Key, Nodes: []RenderedNode{}, InputHash: context.InputHash, Provisional: append([]string{}, context.Provisional...)}
	if scenario == "provisional" {
		out.Provisional = append(out.Provisional, "Tình huống xem trước Tạm tính")
	}
	outputRunes := 0
	renderedRows := 0
	var visit func(string, string, int) error
	visit = func(parent, prefix string, depth int) error {
		for i, n := range children[parent] {
			number := strconv.Itoa(i + 1)
			if parent == "" || Protected(d.Key, n.ID) {
				number = n.ID
			} else if prefix != "" {
				number = prefix + "." + number
			}
			item := RenderedNode{ID: n.ID, Parent: n.Parent, Number: number, Title: n.Title, Depth: depth, Processing: Processing(n), Protected: Protected(d.Key, n.ID), Seeded: Seeded(d.Key, n.ID), TextSlot: n.TextSlot, Rows: []RenderedRow{}}
			outputRunes += utf8.RuneCountInString(n.Title)
			rows := selectedRows(n, context)
			if n.Repeat == "none" {
				rows = []port.KHTCTemplateRow{{Ref: "", Values: map[string]string{}}}
			}
			if len(rows) > MaxRows-renderedRows {
				return invalid("period_id", "Tổng số dòng sau khi lặp vượt giới hạn")
			}
			renderedRows += len(rows)
			lastProgram := ""
			for _, row := range rows {
				values, bindings, err := boundValues(n, row, context)
				if err != nil {
					return err
				}
				status := row.Status
				if n.Repeat != "none" && scenario != "" && scenario != "actual" && scenario != "provisional" {
					status = scenario
				}
				body := n.Body
				if message := statusMessage(status, row.Department, context.PeriodType); message != "" {
					item.Rows = append(item.Rows, RenderedRow{Ref: row.Ref, Status: status, Tokens: []Token{{Text: message, Source: "status", Locked: true}}})
					outputRunes += utf8.RuneCountInString(message)
				} else if body != "" {
					tokens, err := sentence(body, values, bindings, n.LockedAdmin)
					if err != nil {
						return err
					}
					rendered := RenderedRow{Ref: row.Ref, Status: status, Tokens: tokens}
					if n.Preamble != "" && (row.ProgramRef != lastProgram || lastProgram == "") {
						preamble, err := sentence(n.Preamble, values, bindings, n.LockedAdmin)
						if err != nil {
							return err
						}
						for _, t := range preamble {
							rendered.Preamble += t.Text
						}
						rendered.PreambleTokens = preamble
						lastProgram = row.ProgramRef
					}
					for _, t := range tokens {
						outputRunes += utf8.RuneCountInString(t.Text)
					}
					outputRunes += utf8.RuneCountInString(rendered.Preamble)
					item.Rows = append(item.Rows, rendered)
				}
				if outputRunes > MaxOutputRunes {
					return invalid("period_id", "Nội dung xem trước vượt giới hạn")
				}
			}
			if n.TextSlot && n.Repeat != "none" && context.Texts[n.ID] != "" {
				text := context.Texts[n.ID]
				if utf8.RuneCountInString(text) > MaxBodyRunes {
					return invalid("texts."+n.ID, "Lời văn vượt giới hạn")
				}
				outputRunes += utf8.RuneCountInString(text)
				if outputRunes > MaxOutputRunes {
					return invalid("period_id", "Nội dung xem trước vượt giới hạn")
				}
				item.Rows = append(item.Rows, RenderedRow{Ref: "text:" + n.ID, Tokens: []Token{{Text: text, Source: "text", Field: "narrative"}}})
			}
			out.Nodes = append(out.Nodes, item)
			if err := visit(n.ID, number, depth+1); err != nil {
				return err
			}
		}
		return nil
	}
	if err := visit("", "", 0); err != nil {
		return Rendered{}, err
	}
	return out, nil
}

func selectedRows(n Node, c port.KHTCTemplateContext) []port.KHTCTemplateRow {
	out := []port.KHTCTemplateRow{}
	for _, row := range c.Lists[n.Repeat] {
		if n.Department != "" && row.Department != n.Department {
			continue
		}
		if n.Tab == "previous" && row.Values["tab"] != "prev_year" || n.Tab == "current" && row.Values["tab"] == "prev_year" {
			continue
		}
		keep := true
		for _, b := range n.Bindings {
			if section, ok := strings.CutPrefix(b.Ref, "section:"); ok && row.Values["section"] != section {
				keep = false
			}
		}
		if keep {
			out = append(out, row)
		}
	}
	return out
}

func boundValues(n Node, row port.KHTCTemplateRow, c port.KHTCTemplateContext) (map[string]string, map[string]Binding, error) {
	values := map[string]string{}
	bindings := map[string]Binding{}
	for _, b := range n.Bindings {
		source := b.Source
		if source == "auto" {
			source = row.Values["data_source"]
			if source != "module" {
				source = "form"
			}
		}
		actual := b
		actual.Source = source
		bindings[b.Variable] = actual
		switch source {
		case "text":
			values[b.Variable] = c.Texts[n.ID]
		case "admin":
			values[b.Variable] = c.Values["admin."+b.Field]
		default:
			selected := row.Values
			if b.Ref != "" && !strings.HasPrefix(b.Ref, "section:") {
				selected = nil
				for _, list := range c.Lists {
					for _, candidate := range list {
						if candidate.Ref == b.Ref {
							selected = candidate.Values
							break
						}
					}
					if selected != nil {
						break
					}
				}
				if selected == nil {
					return nil, nil, invalid("nodes."+n.ID+".bindings", "Không tìm thấy tham chiếu nguồn trong kỳ đã chọn")
				}
			}
			value, ok := selected[source+"."+b.Field]
			if !ok {
				value, ok = c.Values[source+"."+b.Field]
			}
			if !ok {
				return nil, nil, invalid("nodes."+n.ID+".bindings", "Nguồn không cung cấp ô "+source+"."+b.Field)
			}
			values[b.Variable] = value
		}
		if utf8.RuneCountInString(values[b.Variable]) > doctpl.MaxValueRunes {
			return nil, nil, invalid("period_id", "Giá trị nguồn vượt giới hạn")
		}
	}
	return values, bindings, nil
}

// sentence reuses doctpl validation/rendering and returns source-aware tokens for
// the editor. Substituted source values are never interpreted as more template code.
func sentence(body string, values map[string]string, bindings map[string]Binding, locks []string) ([]Token, error) {
	vars := []doctpl.Var{}
	for key := range bindings {
		vars = append(vars, doctpl.Var{Key: key, Label: key})
	}
	sort.Slice(vars, func(i, j int) bool { return vars[i].Key < vars[j].Key })
	t := doctpl.Template{Key: "khtc.sentence", Version: 1, Owner: "pf/extension", Title: "Khuôn câu", Vars: vars, Body: []doctpl.Block{{Kind: doctpl.KindParagraph, Text: body}}}
	doc, err := doctpl.Render(t, doctpl.Data{Vars: values, Lists: map[string][]doctpl.Item{}})
	if err != nil {
		return nil, err
	}
	out := []Token{}
	literal := strings.Builder{}
	flush := func() {
		if literal.Len() > 0 {
			out = append(out, Token{Text: literal.String()})
			literal.Reset()
		}
	}
	for i := 0; i < len(body); {
		if strings.HasPrefix(body[i:], "{{") {
			literal.WriteByte('{')
			i += 2
			continue
		}
		if strings.HasPrefix(body[i:], "}}") {
			literal.WriteByte('}')
			i += 2
			continue
		}
		if body[i] != '{' {
			literal.WriteByte(body[i])
			i++
			continue
		}
		flush()
		end := strings.IndexByte(body[i:], '}')
		if end < 0 {
			return nil, fmt.Errorf("khuôn câu thiếu dấu ngoặc")
		}
		variable := body[i+1 : i+end]
		b, ok := bindings[variable]
		if !ok {
			return nil, invalid("bindings", "Biến chưa gắn nguồn")
		}
		locked := b.Source != "admin" && b.Source != "text" || b.Source == "admin" && slices.Contains(locks, b.Field)
		out = append(out, Token{Text: values[variable], Variable: variable, Source: b.Source, Field: b.Field, Locked: locked})
		i += end + 1
	}
	flush()
	// Keep the token representation identical to the shared renderer, including newlines.
	parts := []string{}
	for _, block := range doc.Blocks {
		if p, ok := block.(docx.Paragraph); ok {
			var text string
			for _, run := range p.Runs {
				text += run.Text
			}
			parts = append(parts, text)
		}
	}
	var joined string
	for _, token := range out {
		joined += token.Text
	}
	normalized := []string{}
	for _, line := range strings.Split(strings.ReplaceAll(joined, "\r\n", "\n"), "\n") {
		if strings.TrimSpace(line) != "" {
			normalized = append(normalized, strings.TrimRight(line, " \t"))
		}
	}
	if strings.Join(parts, "\n") != strings.Join(normalized, "\n") {
		return nil, invalid("body", "Khuôn câu không khớp bộ dựng văn bản")
	}
	return out, nil
}

func statusMessage(status, department, kind string) string {
	names := map[string]string{"trong_trot": "Trồng trọt", "chan_nuoi_thuy_san": "Chăn nuôi – Thủy sản", "hanh_chinh_tong_hop": "Hành chính – Tổng hợp", "thong_tin_truyen_thong": "Thông tin – Tuyên truyền", "ke_hoach_tai_chinh": "Kế hoạch – Tài chính"}
	name := names[department]
	if name == "" {
		name = department
	}
	switch status {
	case "no_send":
		return "Phòng " + name + " chưa gửi số liệu."
	case "unclosed":
		return "Phòng " + name + " chưa chốt số liệu."
	case "no_activity":
		if kind == "week" {
			return "Trong tuần không tổ chức."
		}
		return "Trong tháng không tổ chức."
	}
	return ""
}
