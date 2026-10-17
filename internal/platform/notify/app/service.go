// Package app: use case của pf/notify — hộp thư, token đẩy, đồng bộ ngay, consumer
// sự kiện, gửi đẩy, email tóm tắt. Giao dịch mở ở đây.
package app

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/notify/channel"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/notify/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/notify/port"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	syncport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/sync/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// Mã lỗi của module (docs/errors.md).
var (
	ErrNotificationNotFound = problem.New(http.StatusNotFound, "notify.notification_not_found", "Không tìm thấy thông báo")
	ErrDeviceUnregistered   = problem.New(http.StatusForbidden, "notify.device_unregistered", "Thiết bị chưa đăng ký với tài khoản này")
	ErrDeviceRevoked        = problem.New(http.StatusForbidden, "notify.device_revoked", "Thiết bị đã bị thu hồi")
	ErrOutOfScope           = problem.New(http.StatusForbidden, "notify.out_of_scope", "Đơn vị nằm ngoài phạm vi của bạn")
	ErrNudgeTargetNotFound  = problem.New(http.StatusNotFound, "notify.nudge_target_not_found", "Không có thiết bị nhận được yêu cầu đồng bộ")
	ErrNudgeRateLimited     = problem.New(http.StatusTooManyRequests, "notify.nudge_rate_limited", "Yêu cầu đồng bộ quá dày — thử lại sau")
	ErrNudgeEntityUnknown   = problem.New(http.StatusUnprocessableEntity, "notify.nudge_entity_unknown", "Nhóm dữ liệu không có trong hợp đồng đồng bộ")
	ErrDependency           = problem.New(http.StatusServiceUnavailable, "notify.dependency_unavailable", "Chưa nối phụ thuộc của module thông báo")
)

// Cửa sổ thời gian.
const (
	// TopicWindow: thông báo chủ đề cũ hơn không còn hiện trong hộp thư (người mới
	// vào địa bàn không nhận hàng nghìn thông báo cũ).
	TopicWindow = 90 * 24 * time.Hour
	// SyncWindow: thiết bị ngoại tuyến chỉ nhận thông báo trong 30 ngày gần nhất.
	SyncWindow = 30 * 24 * time.Hour
	// DefaultNudgeCooldown: một đích (thiết bị hoặc đơn vị) nhận tối đa một yêu cầu
	// đồng bộ ngay mỗi 5 phút.
	DefaultNudgeCooldown = 5 * time.Minute
	// DefaultNudgeHourly: mỗi người yêu cầu tối đa 30 lần mỗi giờ.
	DefaultNudgeHourly = 30
)

// Deps là phụ thuộc ngoài module, do gốc lắp ráp truyền vào.
type Deps struct {
	Scope   partyport.ScopeResolver // phạm vi người xem; nil → phạm vi rỗng
	Devices syncport.Devices        // kiểm thiết bị khi gắn token đẩy
	Staff   port.StaffDirectory     // danh bạ cán bộ (người nhận chính xác)
	Units   partyport.AdminUnits    // mở rộng tỉnh → xã cho thông báo chủ đề
	// Farmers: tài khoản nông dân có thửa trong địa bàn (cảnh báo khẩn HTX US-NEWS-03); nil → chỉ chủ đề.
	Farmers port.AreaFarmers
	Pusher  channel.Pusher // chỉ worker cần
	Mailer  channel.Mailer // nil → tắt email tóm tắt
	// DigestTo: hộp thư công vụ nhận email tóm tắt chỉ đạo khẩn (AG_SMTP_DIGEST_TO).
	DigestTo      []string
	NudgeCooldown time.Duration
	NudgeHourly   int
	// SyncEntities: entity của pf/sync (chiều xuống) — kiểm entities[] của nudge; rỗng → chỉ kiểm dạng tên.
	SyncEntities []string
	Log          *slog.Logger
	// Emails (gói NOTIF): tra email người nhận lúc gửi (pf/accounts); nil → không gửi email theo người nhận.
	Emails port.EmailDirectory
	// EmailMailer (gói NOTIF): kênh gửi email theo người nhận (SMTP trong lãnh thổ; Local/Dev: channel.LogMailer). nil →
	// dùng Mailer; cả hai nil → không gửi email theo người nhận.
	EmailMailer channel.Mailer
	// LinkBase: gốc URL web-ops cho đường dẫn trong email (AG_WEB_OPS_URL); rỗng → chỉ đường dẫn tương đối.
	LinkBase string
}

// Service là use case của module.
type Service struct {
	eventRenderers eventRendererRegistry
	eventBuilders  eventBuilderRegistry
	digests        *digestRegistry
	DB             tenant.Beginner // pool vai notify_app
	Clock          clock.Clock
	Deps
}

// NewService điền giá trị mặc định.
func NewService(db tenant.Beginner, clk clock.Clock, d Deps) *Service {
	if d.Log == nil {
		d.Log = slog.New(slog.DiscardHandler)
	}
	if d.NudgeCooldown <= 0 {
		d.NudgeCooldown = DefaultNudgeCooldown
	}
	if d.NudgeHourly <= 0 {
		d.NudgeHourly = DefaultNudgeHourly
	}
	return &Service{DB: db, Clock: clk, Deps: d}
}

func (s *Service) now() time.Time { return s.Clock.Now().UTC() }

func principal(ctx context.Context) (authz.Principal, error) {
	p, ok := authz.FromContext(ctx)
	if !ok {
		return authz.Principal{}, problem.Unauthenticated
	}
	return p, nil
}

func (s *Service) scope(ctx context.Context) (authz.Scope, error) {
	if s.Scope == nil {
		return authz.Scope{}, nil // chưa nối party → mặc định chặn
	}
	return s.Scope.Scope(ctx)
}

// viewer dựng người xem từ Principal và phạm vi hiện tại.
func (s *Service) viewer(ctx context.Context) (authz.Principal, domain.Viewer, error) {
	p, err := principal(ctx)
	if err != nil {
		return p, domain.Viewer{}, err
	}
	sc, err := s.scope(ctx)
	if err != nil {
		return p, domain.Viewer{}, err
	}
	return p, domain.Viewer{ActorRef: p.ActorRef, OrgRef: p.OrgRef, Roles: p.Roles, Scope: sc}, nil
}

func orgPtr(o uuid.UUID) *uuid.UUID {
	if o == uuid.Nil {
		return nil
	}
	return &o
}
