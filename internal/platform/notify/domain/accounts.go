package domain

// D24 (CEO 13/10/2026 — W10.05): báo trước khóa tài khoản không hoạt động (pf/accounts job accounts_dormant).

import (
	"encoding/json"
	"fmt"
	"time"

	accport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/notify/port"
)

// AccountDormantLink là đường dẫn trong ứng dụng của thông báo — đăng nhập là đủ để giữ tài khoản.
const AccountDormantLink = "/login"

// DormantWarningNotification dựng thông báo cá nhân (kèm email) cho chính tài khoản sắp bị khóa. Không có dữ liệu cá
// nhân: chỉ số ngày và ngày khóa dự kiến.
func DormantWarningNotification(payload json.RawMessage, occurred time.Time) (Notification, error) {
	var p accport.DormantWarningV1
	if err := json.Unmarshal(payload, &p); err != nil {
		return Notification{}, fmt.Errorf("notify: payload %s: %w", accport.EventUserDormantWarning, err)
	}
	disableAt := ParseTime(p.DisableAt, occurred)
	t := Template{
		Kind:     port.KindAccountDormant,
		Severity: port.SeverityUrgent,
		Title:    "Tài khoản sắp bị khóa do không đăng nhập",
		Body: fmt.Sprintf("Tài khoản của bạn chưa đăng nhập %d ngày. Nếu không đăng nhập lại, tài khoản sẽ tự khóa từ %s "+
			"và chỉ quản trị viên mở được.", p.IdleDays, viTime(disableAt)),
		DeepLink:   AccountDormantLink,
		SubjectRef: ref(p.UserRef),
		CreatedAt:  ParseTime(p.EventTime, occurred),
	}
	n := t.For(p.UserRef)
	n.Email = true
	return n, nil
}
