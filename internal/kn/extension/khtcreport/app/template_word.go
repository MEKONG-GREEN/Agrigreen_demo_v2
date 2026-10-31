package app

import (
	"fmt"
	"strings"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcreport/domain"
	templatedomain "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtctemplate/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/doctpl"
)

const fixedWordKey = "khtc.fixed"
const fixedWordVersion = 1
const fixedWordLines = "fixed_lines"

func fixedWordBodyFields() []string {
	fields := []string{"body"}
	for i := 1; i < (templatedomain.MaxOutputRunes+doctpl.MaxValueRunes-1)/doctpl.MaxValueRunes; i++ {
		fields = append(fields, fmt.Sprintf("body_%d", i))
	}
	return fields
}
func fixedWordBodyPattern() string {
	var pattern strings.Builder
	for _, field := range fixedWordBodyFields() {
		pattern.WriteString("{" + fixedWordLines + "." + field + "}")
	}
	return pattern.String()
}

// FixedWordTemplate is a generic immutable envelope, not a sixth business report.
// Tenant-specific definitions are compiled in KN; pf/export receives only frozen
// text/data and keeps its normal immutable template/run registry.
func FixedWordTemplate() doctpl.Template {
	base := WordTemplate("mau_1")
	out := doctpl.Template{Key: fixedWordKey, Version: fixedWordVersion, Owner: WordOwner, Title: "Báo cáo KHTC theo định nghĩa đã ghim", Vars: append([]doctpl.Var{}, wordVars...)}
	out.Vars = append(out.Vars, doctpl.Var{Key: "mau5_kinh_gui", Label: "Kính gửi của Mẫu 5"}, doctpl.Var{Key: "mau5_can_cu", Label: "Căn cứ của Mẫu 5"}, doctpl.Var{Key: "co_phu_luc", Label: "Có phụ lục"}, doctpl.Var{Key: "mau_phien_ban", Label: "Phiên bản định nghĩa"}, doctpl.Var{Key: "mau_bam", Label: "Băm định nghĩa"}, doctpl.Var{Key: "mau_quy_tac", Label: "Quy tắc biên dịch"})
	out.Lists = []doctpl.List{{Key: listReasons, Label: "Lý do Tạm tính", Fields: []string{"ly_do"}}, {Key: fixedWordLines, Label: "Mục và câu đã biên dịch", Fields: append([]string{"title", "has_body"}, fixedWordBodyFields()...)}, {Key: listAppendix, Label: "Phụ lục tiến độ chương trình", Fields: append([]string{}, appendixField...)}}
	// Reuse the same administrative header and provisional/filter notices.
	for _, b := range base.Body {
		if b.Kind == doctpl.KindSection && b.Key != "tam_tinh" {
			break
		}
		out.Body = append(out.Body, b)
	}
	out.Body = append(out.Body, doctpl.Block{Kind: doctpl.KindParagraph, Text: "Kính gửi: {mau5_kinh_gui}", If: "mau5_kinh_gui", Align: doctpl.AlignCenter}, doctpl.Block{Kind: doctpl.KindParagraph, Text: "{mau5_can_cu}", If: "mau5_can_cu", Indent: true, Align: doctpl.AlignJustify}, doctpl.Block{Kind: doctpl.KindLoop, Each: fixedWordLines, Blocks: []doctpl.Block{{Kind: doctpl.KindHeading, Text: "{fixed_lines.title}", If: "fixed_lines.title"}, {Kind: doctpl.KindParagraph, Text: fixedWordBodyPattern(), If: "fixed_lines.has_body", Indent: true, Align: doctpl.AlignJustify}}})
	signature := false
	for _, b := range base.Body {
		if b.Kind == doctpl.KindParagraph && strings.HasPrefix(b.Text, "Trên đây là báo cáo ") {
			signature = true
		}
		if signature && b.Text != "Dấu dữ liệu: {dau_du_lieu}" {
			out.Body = append(out.Body, b)
		}
	}
	// M3/M4 retain the native appendix even though their editable outline has no
	// appendix node. M5 keeps its same five-column appendix, from the same filter.
	appendix := false
	for _, b := range WordTemplate("mau_5").Body {
		if b.Kind == doctpl.KindPageBreak {
			appendix = true
		}
		if appendix && b.Text != "Dấu dữ liệu: {dau_du_lieu}" {
			b.If = "co_phu_luc"
			out.Body = append(out.Body, b)
		}
	}
	out.Body = append(out.Body, doctpl.Block{Kind: doctpl.KindParagraph, Text: "Dấu dữ liệu: {dau_du_lieu}", If: "dau_du_lieu", Italic: true, Align: doctpl.AlignRight}, doctpl.Block{Kind: doctpl.KindParagraph, Text: "Định nghĩa mẫu {mau_phien_ban} — {mau_bam} — {mau_quy_tac}", Italic: true, Align: doctpl.AlignRight})
	return out
}

// fixedTemplateWord uses only an embedded source. Stored historical nil retains
// Word1 behavior; explicit legacy verifies its pinned renderer fingerprint.
func fixedTemplateWord(pinned *port.KHTCTemplateSnapshot, content domain.Draft, terminal string, filter domain.Filter, version int64) (string, int, doctpl.Data, error) {
	if pinned == nil {
		return WordTemplateKey(content.Data.Template), WordTemplateVersion, WordData(content, filter, version), nil
	}
	if _, err := templatedomain.PinnedDefinition(*pinned, content.Data.Template, content.Data.Period.From); err != nil {
		return "", 0, doctpl.Data{}, ErrInvalid.WithCause(err)
	}
	if pinned.Mode == "legacy" {
		renderer := WordTemplate(content.Data.Template)
		if pinned.Rule != domain.RuleVersion || pinned.WordKey != renderer.Key || pinned.WordVersion != renderer.Version || pinned.WordHash != renderer.Hash() {
			return "", 0, doctpl.Data{}, ErrInvalid.WithDetail("Bộ dựng nguồn nền không khớp phiên bản đã ghim")
		}
		return pinned.WordKey, pinned.WordVersion, WordData(content, filter, version), nil
	}
	renderer := FixedWordTemplate()
	if pinned.WordKey != renderer.Key || pinned.WordVersion != renderer.Version || pinned.WordHash != renderer.Hash() {
		return "", 0, doctpl.Data{}, ErrInvalid.WithDetail("Bộ dựng Word không khớp phiên bản đã ghim")
	}
	document, err := renderFixedTemplate(pinned, content, terminal, filter)
	if err != nil {
		return "", 0, doctpl.Data{}, err
	}
	data := WordData(content, filter, version)
	// Source calculations remain in the existing WordData/capture helpers.
	data.Lists = map[string][]doctpl.Item{listReasons: data.Lists[listReasons], listAppendix: appendixItems(domain.SelectRows(content.Data.Rows, filter)), fixedWordLines: {}}
	if content.Data.Template == "mau_5" {
		data.Vars["mau5_kinh_gui"] = data.Vars["kinh_gui"]
		data.Vars["mau5_can_cu"] = data.Vars["can_cu"]
	}
	if content.Data.Template == "mau_3" || content.Data.Template == "mau_4" || content.Data.Template == "mau_5" {
		data.Vars["co_phu_luc"] = "1"
	} else {
		data.Lists[listAppendix] = nil
	}
	data.Vars["mau_phien_ban"] = fmt.Sprintf("%s/v%d", pinned.Key, pinned.Version)
	data.Vars["mau_bam"] = pinned.Hash
	data.Vars["mau_quy_tac"] = pinned.Rule
	bodyFields := fixedWordBodyFields()
	appendLine := func(title, text string) error {
		if len(data.Lists[fixedWordLines])+len(data.Lists[listReasons])+len(data.Lists[listAppendix]) >= doctpl.MaxItems {
			return ErrInvalid.WithDetail("Nội dung Word vượt trần dòng an toàn")
		}
		runes := []rune(text)
		if len([]rune(title)) > doctpl.MaxValueRunes || len(runes) > templatedomain.MaxOutputRunes {
			return ErrInvalid.WithDetail("Đoạn Word vượt giới hạn ký tự")
		}
		item := doctpl.Item{"title": title}
		if strings.TrimSpace(text) != "" {
			item["has_body"] = "1"
		}
		// Scalar values remain within doctpl's existing cap. Concatenation in the
		// immutable paragraph pattern preserves one full paragraph without truncation.
		for _, field := range bodyFields {
			if len(runes) == 0 {
				break
			}
			take := min(len(runes), doctpl.MaxValueRunes)
			item[field] = string(runes[:take])
			runes = runes[take:]
		}
		if len(runes) > 0 {
			return ErrInvalid.WithDetail("Đoạn Word vượt giới hạn ký tự")
		}
		data.Lists[fixedWordLines] = append(data.Lists[fixedWordLines], item)
		return nil
	}
	for _, node := range document.Nodes {
		if err = appendLine(node.Number+". "+node.Title, ""); err != nil {
			return "", 0, doctpl.Data{}, err
		}
		for _, row := range node.Rows {
			if row.Preamble != "" {
				if err = appendLine("", row.Preamble); err != nil {
					return "", 0, doctpl.Data{}, err
				}
			}
			var text strings.Builder
			for _, token := range row.Tokens {
				text.WriteString(token.Text)
			}
			if err = appendLine("", text.String()); err != nil {
				return "", 0, doctpl.Data{}, err
			}
		}
	}
	return renderer.Key, renderer.Version, data, nil
}
