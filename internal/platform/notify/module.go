// Package notify là module thông báo pf/notify (ADR-0015): hộp thư trong app theo
// người nhận, token đẩy gắn với thiết bị pf/sync, thông điệp đẩy CHỈ dữ liệu (FCM,
// RB-7), email tóm tắt do nguồn đăng ký (SMTP trong lãnh thổ), đồng bộ ngay theo yêu
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
	"os"
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
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/residency"
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
	Kinds     []port.KindRegistration
	Digests   []port.DigestRegistration
	Builders  []port.EventBuilderRegistration
	Renderers []port.EventRendererRegistration
	Consumers []port.ConsumerRegistration
	Scope     partyport.ScopeResolver
	Devices   syncport.Devices
	Staff     port.StaffDirectory
	Units     partyport.AdminUnits
	// Farmers (tùy chọn): nông dân có thửa trong địa bàn cảnh báo khẩn (HTX US-NEWS-03).
	Farmers          port.AreaFarmers
	Pusher           channel.Pusher
	Mailer           channel.Mailer
	DigestTo         []string
	DigestRecipients port.DigestRecipients
	Log              *slog.Logger
	// NudgeCooldown, NudgeHourly: giới hạn đồng bộ ngay (mặc định 5 phút/đích, 30 lần/giờ/người).
	NudgeCooldown time.Duration
	NudgeHourly   int
	// SyncEntities: entity chiều xuống của pf/sync để kiểm entities[] của POST /v1/sync/nudge.
	SyncEntities []string
	// Emails (gói NOTIF): tra email người nhận lúc gửi (pf/accounts — Keycloak); nil → không gửi email theo người nhận.
	Emails port.EmailDirectory
	// EmailMailer (gói NOTIF): kênh email theo người nhận (Channels.EmailMailer); nil → Mailer.
	EmailMailer channel.Mailer
	// LinkBase: gốc URL web-ops cho liên kết trong email (AG_WEB_OPS_URL).
	LinkBase string
}

// Channels là kênh gửi ra ngoài đọc từ môi trường (cho cmd/worker). P0-NOTIFY-FAILCLOSED (#127): kênh thiếu cấu
// hình là kênh TẮT (channel.DisabledMailer/DisabledPusher) — bản gửi ghi "dropped" kèm lý do, không bao giờ "sent";
// LogMailer/LogPusher chỉ khi đặt rõ AG_MAILER=log / AG_PUSHER=log trên máy phát triển.
type Channels struct {
	Pusher channel.Pusher // không bao giờ nil (FCM | log | tắt)
	// Mailer: email tóm tắt chỉ đạo khẩn — CHỈ SMTP thật; nil khi kênh email tắt hoặc log (job tóm tắt không chạy,
	// mục giữ nguyên hàng chờ).
	Mailer   channel.Mailer
	DigestTo []string
	// EmailMailer (gói NOTIF): kênh email theo người nhận — SMTP | log | tắt (smtp_not_configured, link_base_missing,
	// disabled_by_config); không bao giờ nil. Bên gọi còn phải nối Deps.Emails thì mới xếp bản gửi.
	EmailMailer channel.Mailer
	// LinkBase: AG_WEB_OPS_URL đã kiểm (residency.PublicBaseURL) — rỗng chỉ ở máy phát triển.
	LinkBase string
	// Email, Push: chế độ + lý do tắt (log khởi động, kiểm thử).
	Email, Push channel.Status
}

// EnvWebOpsURL là URL gốc web-ops cho liên kết trong email thông báo.
const EnvWebOpsURL = "AG_WEB_OPS_URL"

// ChannelsFromEnv đọc kênh từ môi trường tiến trình (ChannelsFromEnvWith(os.Getenv, …)).
func ChannelsFromEnv(log *slog.Logger) (Channels, error) { return ChannelsFromEnvWith(os.Getenv, log) }

// ChannelsFromEnvWith: AG_PUSHER + AG_FCM_* → kênh đẩy (channel.PusherFromEnvWith); AG_MAILER + AG_SMTP_* → kênh email
// (channel.MailerFromEnv); AG_WEB_OPS_URL → gốc liên kết. Ngoài máy phát triển (residency.LocalDev) AG_WEB_OPS_URL
// thiếu/không tuyệt đối/localhost → kênh email theo người nhận TẮT (link_base_missing) thay vì gửi liên kết hỏng.
// Cấu hình sai (AG_MAILER=log ngoài máy phát triển, AG_MAILER=smtp thiếu host, giá trị lạ) → lỗi, worker từ chối dựng.
func ChannelsFromEnvWith(getenv func(string) string, log *slog.Logger) (Channels, error) {
	p, ps, err := channel.PusherFromEnvWith(getenv, log)
	if err != nil {
		return Channels{}, err
	}
	m, ms, err := channel.MailerFromEnv(getenv, log)
	if err != nil {
		return Channels{}, err
	}
	out := Channels{Pusher: p, EmailMailer: m, Email: ms, Push: ps, DigestTo: channel.DigestRecipientsFromEnv()}
	if ms.Mode == channel.ModeSMTP {
		out.Mailer = m
	}
	link, lerr := residency.PublicBaseURL(getenv, EnvWebOpsURL)
	out.LinkBase = link
	if lerr != nil && !ms.Disabled() {
		out.EmailMailer = channel.DisabledMailer{Reason: channel.ReasonLinkBaseMissing}
		out.Email = channel.Status{Mode: channel.ModeDisabled, Reason: channel.ReasonLinkBaseMissing}
	}
	return out, nil
}

// LogStartup ghi chế độ kênh lúc khởi động; kênh TẮT → cảnh báo rõ (bản gửi ghi "dropped" channel_disabled:<lý do>,
// không phải đã gửi — P0-NOTIFY-FAILCLOSED).
func (c Channels) LogStartup(log *slog.Logger) {
	if log == nil {
		return
	}
	log.Info("kênh thông báo", "event", "startup", "push", c.Push.Mode, "email", c.Email.Mode, "digest", c.Mailer != nil, "outcome", "ok")
	for _, x := range []struct {
		name string
		st   channel.Status
	}{{"email", c.Email}, {"push", c.Push}} {
		if x.st.Disabled() {
			log.Warn("kênh "+x.name+" TẮT — thông báo chỉ trong ứng dụng, bản gửi ghi là bị bỏ (không phải đã gửi)", "event", "startup",
				"channel", x.name, "reason", x.st.Reason, "outcome", "disabled")
		}
	}
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
		DigestTo: d.DigestTo, DigestRecipients: d.DigestRecipients, Log: d.Log, NudgeCooldown: d.NudgeCooldown, NudgeHourly: d.NudgeHourly,
		SyncEntities: d.SyncEntities, Emails: d.Emails, EmailMailer: d.EmailMailer, LinkBase: d.LinkBase,
	})}
	m.svc.RegisterKinds(d.Kinds, nativeNotificationKinds())
	m.consumers = newConsumerRegistry(m.nativeConsumers(), d.Consumers)
	var subscribed, native []string
	for _, c := range m.Consumers() {
		subscribed = append(subscribed, c.Events...)
	}
	for _, c := range m.nativeConsumers() {
		native = append(native, c.consumer.Events...)
	}
	m.svc.RegisterEventRenderers(d.Renderers, subscribed, native)
	m.svc.RegisterEventBuilders(d.Builders, subscribed, native)
	m.svc.RegisterDigestPolicies(d.Digests, subscribed, native)
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

// DigestTenants dựng bộ tìm tenant có email tóm tắt chờ gửi từ pool vai
// ag_ops_jobs (worker, nối thẳng).
func DigestTenants(pool *pgxpool.Pool) app.TenantLister {
	return func(ctx context.Context) ([]uuid.UUID, error) { return store.PendingDigestTenants(ctx, pool) }
}

// Jobs returns only explicitly registered digest jobs (leader only).
func (m *Module) Jobs(tenants app.TenantLister, log *slog.Logger) []schedule.Job {
	definition, configured := m.svc.DigestJobDefinition()
	if !configured {
		return nil
	}

	return []schedule.Job{{
		Name: definition.Name, Spec: schedule.Every{Interval: definition.Every}, RunOnStart: definition.RunOnStart, Timeout: definition.Timeout,
		Run: func(ctx context.Context) error {
			n, err := m.svc.RunEmailDigest(ctx, tenants)
			if log != nil {
				outcome := "ok"
				if err != nil {
					outcome = "error"
				}
				log.Info(definition.LogMessage, "event", "job."+definition.Name, "sent", n, "outcome", outcome)
			}
			return err
		},
	}}
}
