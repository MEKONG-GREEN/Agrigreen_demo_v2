package extension

// W11.53b (#158 — BRD CNTS v3.7 FN-19 AC-02, UR-QG-01): thông báo mẫu biểu dự án KNQG. pf/extension sở hữu sự kiện, loại
// thông báo, consumer và nội dung; người nhận đã tra NGOÀI giao dịch ở pf/extension (cntsknqg/app/notify.go) và đi trong
// payload — renderer chỉ dựng nội dung (thuần, không DB). Gốc lắp ráp (cmd/api-platform/assembly/notify_consumers.go)
// nối KNQGNotificationKinds/Consumers/Renderers sau đăng ký cũ: danh mục consumer cũ (golden) giữ nguyên.

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"

	extport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	notifyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/notify/port"
)

// knqgNotificationLink là màn "Mẫu biểu dự án KNQG" (web-ops); ?doc= mở thẳng biểu.
const knqgNotificationLink = "/kn/cnts/du-an-mau-bieu"

// KNQGNotificationKinds khai loại thông báo của biểu dự án KNQG.
func KNQGNotificationKinds() notifyport.KindRegistration {
	return notifyport.KindRegistration{Kinds: []string{extport.KindCNTSKNQGSent, extport.KindCNTSKNQGReviewed, extport.KindCNTSKNQGSubmitted}}
}

// KNQGNotificationConsumers khai hàng đợi pf.notify.cnts_knqg.
func KNQGNotificationConsumers() notifyport.ConsumerRegistration {
	return notifyport.ConsumerRegistration{Definitions: []notifyport.ConsumerDefinition{{
		Name: extport.NotifyConsumerCNTSKNQG, Order: extport.NotifyConsumerCNTSKNQGOrder, Events: []string{extport.EventCNTSKNQGChanged},
	}}}
}

// KNQGNotificationRenderers dựng nội dung thông báo biểu dự án KNQG.
func KNQGNotificationRenderers() []notifyport.EventRendererRegistration {
	return []notifyport.EventRendererRegistration{{Events: []string{extport.EventCNTSKNQGChanged}, Render: renderKNQGNotifications}}
}

// knqgFormLabel là tên mẫu trên thông báo.
var knqgFormLabel = map[string]string{"01": "Mẫu 01/TĐ-DA", "07": "Mẫu 07/NTKQ-DA"}

func renderKNQGNotifications(eventType string, payload json.RawMessage, occurred time.Time) ([]notifyport.NotificationDraft, error) {
	if eventType != extport.EventCNTSKNQGChanged {
		return nil, nil
	}
	var p extport.CNTSKNQGChangedV1
	if err := json.Unmarshal(payload, &p); err != nil {
		return nil, fmt.Errorf("notify: payload %s: %w", eventType, err)
	}
	form, ok := knqgFormLabel[p.Form]
	if !ok || p.DocRef == uuid.Nil {
		return nil, fmt.Errorf("notify: biểu KNQG thiếu mã hoặc mẫu không thông báo (%q)", p.Form)
	}
	project := sourceNotificationTruncate(p.ProjectCode+" — "+p.ProjectName, 200)
	period := p.Period
	if t, err := time.Parse("2006-01", p.Period); err == nil {
		period = "tháng " + t.Format("01/2006")
	} else if p.Period != "" {
		period = "năm " + p.Period
	}
	tpl := sourceNotificationTemplate{Severity: notifyport.SeverityNormal, DeepLink: knqgNotificationLink + "?doc=" + p.DocRef.String(),
		SubjectRef: sourceNotificationRef(p.DocRef), CreatedAt: sourceNotificationTime(p.EventTime, occurred)}
	switch p.Action {
	case extport.CNTSKNQGSent:
		tpl.Kind, tpl.Title = extport.KindCNTSKNQGSent, form+" chờ Cấp TW xác nhận"
		tpl.Body = fmt.Sprintf("Dự án %s đã gửi biên bản nghiệm thu %s. Mở biểu để xác nhận hoặc trả lại.", project, period)
	case extport.CNTSKNQGConfirmed:
		tpl.Kind, tpl.Title = extport.KindCNTSKNQGReviewed, form+" đã được Cấp TW xác nhận"
		tpl.Body = fmt.Sprintf("Biên bản nghiệm thu %s của dự án %s đã được xác nhận.", period, project)
	case extport.CNTSKNQGReturned:
		tpl.Kind, tpl.Title = extport.KindCNTSKNQGReviewed, form+" bị Cấp TW trả lại"
		tpl.Body = fmt.Sprintf("Biên bản nghiệm thu %s của dự án %s bị trả lại. Mở biểu để xem ý kiến, sửa và gửi lại.", period, project)
	case extport.CNTSKNQGSubmitted:
		tpl.Kind, tpl.Title = extport.KindCNTSKNQGSubmitted, form+" chờ duyệt"
		tpl.Body = fmt.Sprintf("Chủ nhiệm dự án %s đã trình báo cáo tiến độ %s. Mở biểu để duyệt hoặc trả lại.", project, period)
	default:
		return nil, fmt.Errorf("notify: hành động biểu KNQG %q không thông báo", p.Action)
	}
	out := make([]notifyport.NotificationDraft, 0, len(p.RecipientRefs))
	for _, r := range sourceNotificationRecipients(p.RecipientRefs) {
		out = append(out, tpl.For(r))
	}
	return out, nil
}
