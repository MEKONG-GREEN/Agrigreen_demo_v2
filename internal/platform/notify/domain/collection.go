package domain

import (
	"fmt"
	"time"

	ccport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cropcycle/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/notify/port"
)

// Thông báo thu gom rơm MKG cho quản lý HTX (G3-2). Không có dữ liệu cá nhân: chỉ mã xã,
// khung ngày, số cuộn; chi tiết đọc qua API trong phạm vi.

// viDay đổi YYYY-MM-DD sang DD/MM/YYYY (giữ nguyên nếu sai dạng).
func viDay(s string) string {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return s
	}
	return t.Format("02/01/2006")
}

// collectionScheduleStatus là nhãn trạng thái lịch gom MKG.
var collectionScheduleStatus = map[string]string{
	"planned": "dự kiến", "confirmed": "đã chốt", "in_progress": "đang gom", "completed": "đã gom xong", "cancelled": "đã hủy",
}

// CollectionScheduleTemplate: MKG công bố / đổi lịch gom rơm của HTX → quản lý HTX.
func CollectionScheduleTemplate(p ccport.CollectionScheduleReceivedV1, occurred time.Time) Template {
	title := "Lịch gom rơm mới của Mekong Green"
	if !p.First {
		title = "Lịch gom rơm đã thay đổi"
	}
	body := fmt.Sprintf("Xã %s, từ %s đến %s — %s.", p.CommuneCode, viDay(p.WindowFrom), viDay(p.WindowTo), collectionScheduleStatus[p.Status])
	return Template{Kind: port.KindCollectionSchedule, Severity: port.SeverityNormal, Title: title, Body: body,
		DeepLink: "/htx/thu-gom", SubjectRef: ref(p.ScheduleRef), CreatedAt: occurred}
}

// CollectionStatementTemplate: MKG gửi bảng kê số cuộn (hoặc bản sửa) → quản lý HTX xác nhận / tranh chấp.
func CollectionStatementTemplate(p ccport.CollectionStatementReceivedV1, occurred time.Time) Template {
	title := "Bảng kê số cuộn cần xác nhận"
	if !p.First {
		title = "Bảng kê số cuộn đã được MKG sửa — cần xác nhận lại"
	}
	body := fmt.Sprintf("MKG ghi %d cuộn đã gom ngày %s — mở để xác nhận hoặc tranh chấp.", p.Rolls, viDay(p.CollectedOn))
	return Template{Kind: port.KindCollectionStatement, Severity: port.SeverityNormal, Title: title, Body: body,
		DeepLink: "/htx/thu-gom", SubjectRef: ref(p.StatementRef), CreatedAt: occurred}
}
