// Package notify là module thông báo pf/notify (ADR-0015): hộp thư trong app theo
// người nhận, token đẩy gắn với thiết bị pf/sync, thông điệp đẩy CHỈ dữ liệu (FCM,
// RB-7), email tóm tắt chỉ đạo khẩn (SMTP trong lãnh thổ), đồng bộ ngay theo yêu
// cầu (US-SYNC-02), entity đồng bộ "notification".
//
// Người nhận được tìm không cần dữ liệu cá nhân: danh sách actor_ref trong sự kiện
// hoặc danh bạ cán bộ (extension/port.StaffDirectory); không có danh sách chính xác
// thì thông báo theo CHỦ ĐỀ (đơn vị / tổ chức / vai) và client thấy theo phạm vi.
//
// Module khác chỉ được import notify/port (R-02). Tệp này nối các lớp cho
// cmd/api-platform và cmd/worker.
package notify

import (
	"context"
	"log/slog"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	accport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/port"
	contentport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/content/port"
	coopport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cooperative/port"
	ccport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cropcycle/port"
	mechport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/mechanization/port"
	meteringport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/metering/port"
	mrvport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/mrv/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/notify/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/notify/channel"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/notify/handler"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/notify/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/notify/store"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	syncport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/sync/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/db"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/inbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/schedule"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// DBRole là vai đăng nhập cũ của module (R-05) — kỳ chuyển ADR-0028: thành viên r_notify.
const DBRole = "notify_app"

// DBModule: mọi giao dịch của module chạy SET LOCAL ROLE r_notify (ADR-0028, W2.3). Consumer: pool đưa cho
// inbox phải bọc bằng db.ForModule(pool, DBModule) (cmd/worker) để giao dịch inbox cũng chạy dưới r_notify.
var DBModule = db.Module("pf/notify")

// Deps là port của module khác và kênh gửi. Thiếu Devices → gắn token trả 503;
// thiếu Staff → dùng thông báo chủ đề; thiếu Units → không mở rộng tỉnh ra xã;
// thiếu Pusher → consumer gửi đẩy lỗi (vào DLQ); thiếu Mailer → tắt email tóm tắt.
type Deps struct {
	Renderers []port.EventRendererRegistration
	Consumers []port.ConsumerRegistration
	Scope     partyport.ScopeResolver
	Devices   syncport.Devices
	Staff     port.StaffDirectory
	Units     partyport.AdminUnits
	// Farmers (tùy chọn): nông dân có thửa trong địa bàn cảnh báo khẩn (HTX US-NEWS-03).
	Farmers  port.AreaFarmers
	Pusher   channel.Pusher
	Mailer   channel.Mailer
	DigestTo []string
	Log      *slog.Logger
	// NudgeCooldown, NudgeHourly: giới hạn đồng bộ ngay (mặc định 5 phút/đích, 30 lần/giờ/người).
	NudgeCooldown time.Duration
	NudgeHourly   int
	// SyncEntities: entity chiều xuống của pf/sync để kiểm entities[] của POST /v1/sync/nudge.
	SyncEntities []string
	// PriceDesk (gói NOTIF): người nhận Cấp Tỉnh của Báo giá nông sản (gốc lắp ráp, pf/accounts); nil → chủ đề Cấp Tỉnh.
	PriceDesk port.PriceDesk
	// Emails (gói NOTIF): tra email người nhận lúc gửi (pf/accounts — Keycloak); nil → không gửi email theo người nhận.
	Emails port.EmailDirectory
	// EmailMailer (gói NOTIF): kênh email theo người nhận (Channels.EmailMailer); nil → Mailer.
	EmailMailer channel.Mailer
	// LinkBase: gốc URL web-ops cho liên kết trong email (AG_WEB_OPS_URL).
	LinkBase string
}

// Channels là kênh gửi ra ngoài đọc từ môi trường (cho cmd/worker).
type Channels struct {
	Pusher   channel.Pusher
	Mailer   channel.Mailer // nil khi chưa đặt AG_SMTP_HOST
	DigestTo []string
	// EmailMailer (gói NOTIF): kênh email theo người nhận — SMTP khi đặt AG_SMTP_HOST, không thì LogMailer (chỉ ghi log
	// số người nhận + tiêu đề, Local/Dev). Bên gọi còn phải nối Deps.Emails thì mới gửi.
	EmailMailer channel.Mailer
}

// ChannelsFromEnv: AG_FCM_* → FCM HTTP v1 (không đặt → chỉ ghi log);
// AG_SMTP_* → email tóm tắt qua SMTP trong lãnh thổ (ADR-0015).
func ChannelsFromEnv(log *slog.Logger) (Channels, error) {
	p, err := channel.PusherFromEnv(log)
	if err != nil {
		return Channels{}, err
	}
	out := Channels{Pusher: p, DigestTo: channel.DigestRecipientsFromEnv()}
	m, err := channel.SMTPFromEnv()
	if err != nil {
		return Channels{}, err
	}
	out.EmailMailer = channel.LogMailer{Log: log}
	if m != nil {
		out.Mailer, out.EmailMailer = m, m
	}
	return out, nil
}

// Module là pf/notify đã nối.
type Module struct {
	svc       *app.Service
	consumers *consumerRegistry
}

// New tạo module trên pool b: vai đăng nhập của đơn vị triển khai (app_platform, app_worker) hoặc vai cũ
// notify_app. Mọi giao dịch module tự mở chạy dưới vai r_notify (db.ForModule).
func New(b tenant.Beginner, clk clock.Clock, d Deps) *Module {
	m := &Module{svc: app.NewService(db.ForModule(b, DBModule), clk, app.Deps{
		Scope: d.Scope, Devices: d.Devices, Staff: d.Staff, Units: d.Units, Farmers: d.Farmers, Pusher: d.Pusher, Mailer: d.Mailer,
		DigestTo: d.DigestTo, Log: d.Log, NudgeCooldown: d.NudgeCooldown, NudgeHourly: d.NudgeHourly,
		SyncEntities: d.SyncEntities, PriceDesk: d.PriceDesk, Emails: d.Emails, EmailMailer: d.EmailMailer, LinkBase: d.LinkBase,
	})}
	m.consumers = newConsumerRegistry(m.nativeConsumers(), d.Consumers)
	var subscribed, native []string
	for _, c := range m.Consumers() {
		subscribed = append(subscribed, c.Events...)
	}
	for _, c := range m.nativeConsumers() {
		native = append(native, c.consumer.Events...)
	}
	m.svc.RegisterEventRenderers(d.Renderers, subscribed, native)
	return m
}

// RegisterApp gắn route Application API.
func (m *Module) RegisterApp(rg *gin.RouterGroup) { handler.Handler{Svc: m.svc}.RegisterApp(rg) }

// RegisterAdmin gắn route Admin API.
func (m *Module) RegisterAdmin(rg *gin.RouterGroup) { handler.Handler{Svc: m.svc}.RegisterAdmin(rg) }

// UseAreaFarmers nối cổng tìm nông dân theo địa bàn (pf/geo + pf/accounts) cho cảnh báo khẩn
// (HTX US-NEWS-03). Gốc lắp ráp gọi sau khi dựng pf/accounts.
func (m *Module) UseAreaFarmers(f port.AreaFarmers) { m.svc.Farmers = f }

// UseSyncEntities nối danh sách entity chiều xuống của pf/sync (kiểm entities[] của
// POST /v1/sync/nudge, KN US-SYNC-02). Gốc lắp ráp gọi sau khi dựng pf/sync.
func (m *Module) UseSyncEntities(entities []string) { m.svc.SyncEntities = entities }

// SyncSource cho pf/sync: chiều xuống "notification".
func (m *Module) SyncSource() syncport.Source { return app.SyncSource{Svc: m.svc} }

// Consumer mô tả một consumer của worker.
type Consumer struct {
	Name    string
	Events  []string
	Handler inbox.Handler
}

// Consumers trả consumer của module, một consumer mỗi nhóm sự kiện (mục 6). Mọi
// handler idempotent qua shared.inbox; lỗi → giao lại rồi DLQ (R-62).
func (m *Module) Consumers() []Consumer { return m.consumers.consumers(m.svc.HandleEvent) }

// nativeConsumers declares only capability-owned subscriptions. Stable positions preserve the
// assembled catalog order when an application registers its source-event groups.
func (m *Module) nativeConsumers() []orderedConsumer {
	ev := m.svc.HandleEvent
	return []orderedConsumer{
		{order: 2, consumer: Consumer{Name: port.ConsumerContent, Events: []string{contentport.EventAlertPublished, contentport.EventDocumentPublished}, Handler: ev}},
		{order: 4, consumer: Consumer{Name: port.ConsumerSync, Events: []string{syncport.EventConflictOpened, syncport.EventDeviceRevoked}, Handler: ev}},
		{order: 5, consumer: Consumer{Name: port.ConsumerParty, Events: []string{partyport.EventOrgDeactivated, coopport.EventHouseholdRegistered, coopport.EventHouseholdMembershipChanged}, Handler: ev}},
		{order: 6, consumer: Consumer{Name: port.ConsumerCropCycle, Events: []string{coopport.EventFarmLogRecorded, coopport.EventFarmLogReviewed, coopport.EventSeasonOpened, ccport.EventCollectionScheduleReceived, ccport.EventCollectionStatementReceived}, Handler: ev}},
		{order: 8, consumer: Consumer{Name: port.ConsumerIntegration, Events: []string{syncport.EventIntegrationDeadLettered, mechport.EventAreaFeedStale}, Handler: ev}},
		{order: 9, consumer: Consumer{Name: port.ConsumerMRV, Events: []string{mrvport.EventAlertRaised}, Handler: ev}},
		{order: 11, consumer: Consumer{Name: port.ConsumerEgressAnomaly, Events: []string{meteringport.EventEgressAnomaly}, Handler: ev}},
		{order: 14, consumer: Consumer{Name: port.ConsumerAccounts, Events: []string{accport.EventUserDormantWarning}, Handler: ev}},
		{order: 15, consumer: Consumer{Name: port.ConsumerPush, Events: []string{port.EventPushRequested}, Handler: m.svc.HandlePush}},
		{order: 16, consumer: Consumer{Name: port.ConsumerEmail, Events: []string{port.EventEmailRequested}, Handler: m.svc.HandleEmail}},
	}
}

// EventHandler là handler chung của các sự kiện nguồn (kiểm thử gọi qua inbox.Process).
func (m *Module) EventHandler() inbox.Handler { return m.svc.HandleEvent }

// PushHandler là handler của pf.notify.push.requested.
func (m *Module) PushHandler() inbox.Handler { return m.svc.HandlePush }

// EmailHandler là handler của pf.notify.email.requested (gói NOTIF).
func (m *Module) EmailHandler() inbox.Handler { return m.svc.HandleEmail }

// UsePriceDesk nối người nhận Cấp Tỉnh của Báo giá nông sản (gói NOTIF). Gốc lắp ráp gọi sau khi dựng pf/accounts.
func (m *Module) UsePriceDesk(d port.PriceDesk) { m.svc.PriceDesk = d }

// DigestTenants dựng bộ tìm tenant có email tóm tắt chờ gửi từ pool vai
// ag_ops_jobs (worker, nối thẳng).
func DigestTenants(pool *pgxpool.Pool) app.TenantLister {
	return func(ctx context.Context) ([]uuid.UUID, error) { return store.PendingDigestTenants(ctx, pool) }
}

// Jobs trả job định kỳ (chỉ chạy ở leader): email tóm tắt chỉ đạo khẩn mỗi giờ.
func (m *Module) Jobs(tenants app.TenantLister, log *slog.Logger) []schedule.Job {
	return []schedule.Job{{
		Name: port.JobEmailDigest, Spec: schedule.Every{Interval: time.Hour}, RunOnStart: false, Timeout: 10 * time.Minute,
		Run: func(ctx context.Context) error {
			n, err := m.svc.RunEmailDigest(ctx, tenants)
			if log != nil {
				outcome := "ok"
				if err != nil {
					outcome = "error"
				}
				log.Info("email tóm tắt chỉ đạo khẩn", "event", "job."+port.JobEmailDigest, "sent", n, "outcome", outcome)
			}
			return err
		},
	}}
}
