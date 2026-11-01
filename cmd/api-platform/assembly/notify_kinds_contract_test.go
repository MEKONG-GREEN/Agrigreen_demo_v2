package assembly

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"go.yaml.in/yaml/v3"
)

// W11.53b (#158, Codex [P2]): mọi loại thông báo đăng ký với pf/notify phải nằm trong enum Notification.kind của hợp
// đồng nguồn (contracts/openapi/modules/common/application.yaml) — API thông báo trả kind ngoài enum là sai hợp đồng.
//
// knownContractGap là các loại ĐÃ đăng ký trước gói này mà hợp đồng chưa liệt kê (không thuộc phạm vi W11.53b; ghi rõ
// để không lan thêm). Gói nào thêm loại mới phải thêm vào enum, không thêm vào danh sách này.
var knownContractGap = []string{
	"cnts_alert", "cnts_alert_assigned", "cnts_alert_overdue", "cnts_duty_changed", "cnts_form_reminder",
	"report_adjustment_requested", "report_period_reopened", "report_reminder", "report_task_assigned", "report_task_due",
	"work_schedule_due", "work_schedule_reviewed", "work_schedule_submitted",
}

func notificationKindEnum(t *testing.T) []string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "contracts", "openapi", "modules", "common", "application.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Components struct {
			Schemas map[string]struct {
				Properties map[string]struct {
					Enum []string `yaml:"enum"`
				} `yaml:"properties"`
			} `yaml:"schemas"`
		} `yaml:"components"`
	}
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	enum := doc.Components.Schemas["Notification"].Properties["kind"].Enum
	if len(enum) == 0 {
		t.Fatal("hợp đồng thiếu enum Notification.kind")
	}
	return enum
}

func TestNotificationKindsInContract(t *testing.T) {
	enum := notificationKindEnum(t)
	for _, reg := range NotificationKinds() {
		for _, k := range reg.Kinds {
			if slices.Contains(enum, k) {
				if slices.Contains(knownContractGap, k) {
					t.Errorf("loại %q đã có trong hợp đồng — bỏ khỏi knownContractGap", k)
				}
				continue
			}
			if !slices.Contains(knownContractGap, k) {
				t.Errorf("loại thông báo %q đăng ký với pf/notify nhưng không có trong enum Notification.kind của hợp đồng", k)
			}
		}
	}
	for _, k := range []string{"cnts_knqg_sent", "cnts_knqg_reviewed", "cnts_knqg_submitted"} {
		if !slices.Contains(enum, k) {
			t.Errorf("enum Notification.kind thiếu %q (W11.53b)", k)
		}
	}
}
