package port_test

import (
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/notify/port"
	"strings"
	"testing"
)

func TestTextTemplateRendersLiteralVariablesWithoutExecution(t *testing.T) {
	tpl := port.TextTemplate{Title: "Nhắc {{form_name}}", Body: "Kỳ {{period_label}}; hạn {{ due_at }}; {{message}}"}
	title, body, err := tpl.Render(map[string]string{"form_name": "BM-04", "period_label": "Tuần 3", "due_at": "08:00", "message": "{{another}} <b>nội dung</b>"})
	if err != nil || title != "Nhắc BM-04" || body != "Kỳ Tuần 3; hạn 08:00; {{another}} <b>nội dung</b>" {
		t.Fatalf("plain text render: %q %q %v", title, body, err)
	}
}

func TestTextTemplateRejectsInvalidOrMissingVariablesAtomically(t *testing.T) {
	for _, body := range []string{"{{missing}}", "{{form.name}}", "{{call()}}", "{{}}", "{{1name}}", "{{not closed", "stray }}", "}} {{form_name}}", strings.Repeat("x", 2001), "{{long}}", string([]byte{0xff})} {
		t.Run(body[:min(len(body), 30)], func(t *testing.T) {
			title, rendered, err := (port.TextTemplate{Title: "Valid", Body: body}).Render(map[string]string{"form_name": "BM-04", "long": strings.Repeat("é", 2001)})
			if err == nil || title != "" || rendered != "" {
				t.Fatalf("invalid template partially rendered: %q %q %v", title, rendered, err)
			}
		})
	}
	for _, title := range []string{" ", strings.Repeat("é", 301)} {
		if _, _, err := (port.TextTemplate{Title: title}).Render(nil); err == nil {
			t.Fatal("invalid title accepted")
		}
	}
	title, body, err := (port.TextTemplate{Title: strings.Repeat("é", 300), Body: strings.Repeat("ữ", 2000)}).Render(nil)
	if err != nil || len([]rune(title)) != 300 || len([]rune(body)) != 2000 {
		t.Fatal("UTF-8 boundary rejected", err)
	}
}
