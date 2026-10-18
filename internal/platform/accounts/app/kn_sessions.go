package app

import (
	"context"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/port"
)

// UserSessions là dữ liệu tab "Phiên đang hoạt động" và "Lịch sử đăng nhập" của màn Detail tài khoản
// (FN-AD-04.04, UR-AD-04). Available = false khi bản cài Directory không đọc được phiên (vd bộ giả lập cũ).
type UserSessions struct {
	Available bool
	Active    []port.Session
	History   []port.LoginEvent
}

// KNUserSessions đọc phiên và lịch sử đăng nhập của tài khoản từ Keycloak (Admin REST), cùng kiểm phạm vi như
// khung Log (người gọi quản trị KN, tài khoản trong phạm vi). Không ghi sổ, không lưu IP ở pf (R-14): dữ liệu chỉ đi
// qua bộ nhớ tới quản trị viên. Lịch sử cần realm bật lưu sự kiện (eventsEnabled, docs/brd/kn-p1-realm-patch.md).
func (s *Service) KNUserSessions(ctx context.Context, user uuid.UUID, max int) (UserSessions, error) {
	a, err := s.knCaller(ctx)
	if err != nil {
		return UserSessions{}, err
	}
	if _, err := s.target(ctx, a, user); err != nil {
		return UserSessions{}, err
	}
	sr, ok := s.Dir.(port.SessionReader)
	if !ok {
		return UserSessions{Active: []port.Session{}, History: []port.LoginEvent{}}, nil
	}
	act, err := sr.UserSessions(ctx, user)
	if err != nil {
		return UserSessions{}, dirErr(err)
	}
	hist, err := sr.LoginEvents(ctx, user, max)
	if err != nil {
		return UserSessions{}, dirErr(err)
	}
	if act == nil {
		act = []port.Session{}
	}
	if hist == nil {
		hist = []port.LoginEvent{}
	}
	return UserSessions{Available: true, Active: act, History: hist}, nil
}
