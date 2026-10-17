package assembly

import (
	"context"
	"errors"
	accport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/port"
	notifyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/notify/port"
	"github.com/google/uuid"
)

// NotifyEmails bọc accounts/port.MailDirectory thành notify/port.EmailDirectory (đổi lỗi "không có email").
type NotifyEmails struct{ Dir accport.MailDirectory }

// EmailOf thực hiện notify/port.EmailDirectory.
func (e NotifyEmails) EmailOf(ctx context.Context, tid, actor uuid.UUID) (string, error) {
	addr, err := e.Dir.EmailOf(ctx, tid, actor)
	if errors.Is(err, accport.ErrNoEmail) {
		return "", notifyport.ErrNoEmail
	}
	return addr, err
}
