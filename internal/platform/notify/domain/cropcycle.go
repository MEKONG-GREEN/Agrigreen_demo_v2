package domain

import (
	"fmt"
	"time"

	coopport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cooperative/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/notify/port"
)

// Thông báo liên thông App HTX (pf/cropcycle → pf/notify). Không có dữ liệu cá nhân:
// chỉ mã hoạt động, mã lô, mã vụ; nội dung nhật ký đọc qua API trong phạm vi.

// FarmLogPendingTemplate: nông dân ghi nhật ký → quản lý HTX "nhật ký chờ duyệt" (HTX US-LOG-05).
func FarmLogPendingTemplate(p coopport.FarmLogRecordedV1, occurred time.Time) Template {
	body := fmt.Sprintf("Hoạt động %s, ghi lúc %s — mở để phê duyệt hoặc yêu cầu bổ sung.", p.ActivityCode, viTime(ParseTime(p.EventTime, occurred)))
	if p.LocationStatus == "mismatch" || p.LocationStatus == "missing" {
		body += " Có nhãn cảnh báo vị trí."
	}
	title := "Nhật ký canh tác chờ duyệt"
	if p.SupersedesID != nil {
		title = "Nhật ký canh tác đã bổ sung — chờ duyệt lại"
	}
	return Template{Kind: port.KindFarmLogPending, Severity: port.SeverityNormal, Title: title, Body: body,
		DeepLink: "/cropcycle/farm-logs/" + p.LogID.String(), SubjectRef: ref(p.LogRef), CreatedAt: ParseTime(p.EventTime, occurred)}
}

// FarmLogReviewedTemplate: kết quả duyệt → người ghi nhật ký (HTX US-LOG-05 AC-3:
// yêu cầu bổ sung trả về nông dân chỉnh sửa). Ghi chú của người duyệt KHÔNG đưa vào
// thông báo (có thể chứa thông tin tự do) — người nhận mở nhật ký để đọc.
func FarmLogReviewedTemplate(p coopport.FarmLogReviewedV1, occurred time.Time) Template {
	title, body := "Nhật ký canh tác đã được duyệt", "Nhật ký đã được quản lý HTX phê duyệt và tính vào số liệu của HTX."
	if p.Decision == coopport.LogNeedsSupplement {
		title, body = "Nhật ký canh tác cần bổ sung", "Quản lý HTX yêu cầu bổ sung — mở nhật ký để xem ghi chú và chỉnh sửa."
	}
	return Template{Kind: port.KindFarmLogReviewed, Severity: port.SeverityNormal, Title: title, Body: body,
		DeepLink: "/cropcycle/farm-logs/" + p.LogID.String(), SubjectRef: ref(p.LogRef), CreatedAt: ParseTime(p.EventTime, occurred)}
}

// SeasonOpenedTemplate: nông dân tự mở vụ → quản lý HTX (HTX US-SEASON-01).
func SeasonOpenedTemplate(p coopport.SeasonOpenedV1, occurred time.Time) Template {
	body := fmt.Sprintf("Lô %s, vụ %s %d, giống %s, ngày sạ %s.", p.PlotCode, p.SeasonCode, p.SeasonYear, p.VarietyCode, p.SowingDate)
	return Template{Kind: port.KindSeasonOpened, Severity: port.SeverityNormal, Title: "Nông dân vừa mở vụ mới", Body: body,
		DeepLink: "/cropcycle/seasons/" + p.SeasonRef.String(), SubjectRef: ref(p.SeasonRef), CreatedAt: ParseTime(p.EventTime, occurred)}
}
