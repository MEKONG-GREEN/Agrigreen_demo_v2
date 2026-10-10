// Package port là giao diện công khai DUY NHẤT của module pf/notify mà module
// khác được import (R-02): tên vai, loại thông báo, sự kiện, tên consumer.
package port

import (
	"context"

	"github.com/google/uuid"

	meteringport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/metering/port"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	syncport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/sync/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
)

// EntityNotification là entity chiều xuống của pf/sync: hộp thư của người gọi.
const EntityNotification = "notification"

// Loại thông báo (cột kind, trường "kind" của thông điệp đẩy).
const (
	KindDirective      = "directive"       // chỉ đạo từ TTKN (KN US-TASK-03)
	KindAlert          = "alert"           // cảnh báo khẩn (HTX US-NEWS-03)
	KindDocument       = "document"        // tài liệu/tin mới hoặc cập nhật (US-LIB-06)
	KindTaskEscalated  = "task_escalated"  // nhiệm vụ leo thang lên tỉnh (KN US-TASK-02)
	KindSyncConflict   = "sync_conflict"   // xung đột đồng bộ chờ xử lý (C-8)
	KindOrgDeactivated = "org_deactivated" // tổ chức bị vô hiệu hóa
	// KindHouseholdRegistered / KindHouseholdMembershipChanged: nông hộ mới từ App HTX,
	// nông hộ vào/rời/chuyển HTX → cán bộ khuyến nông của xã (KN US-HTX-05, migration 000028).
	KindHouseholdRegistered        = "household_registered"
	KindHouseholdMembershipChanged = "household_membership_changed"
	// KindTaskAssigned: nhiệm vụ tự sinh từ yêu cầu hỗ trợ App HTX → cán bộ được giao (KN US-TASK-01).
	KindTaskAssigned = "task_assigned"
	// KindFarmLogPending: nông dân ghi nhật ký canh tác → quản lý HTX "nhật ký chờ duyệt"
	// (HTX US-LOG-05; migration 000029).
	KindFarmLogPending = "farm_log_pending"
	// KindFarmLogReviewed: kết quả duyệt nhật ký → người ghi (HTX US-LOG-05 AC-3).
	KindFarmLogReviewed = "farm_log_reviewed"
	// KindSeasonOpened: nông dân tự mở vụ → quản lý HTX (HTX US-SEASON-01).
	KindSeasonOpened = "season_opened"
	// KindIntegrationDeadLettered: một sự kiện đồng bộ tích hợp hết số lần thử và vào hàng chờ
	// lỗi (DLQ) → Admin nền tảng (GIS US-SYNC-02 AC-1; migration 000034).
	KindIntegrationDeadLettered = "integration_dead_lettered"
	// KindAreaFeedStale: dữ liệu diện tích vụ App HTX quá ngưỡng độ tươi → Admin CGH (CGH FN-07 AC-02/AC-03).
	KindAreaFeedStale = "area_feed_stale"
	// KindCollectionSchedule: MKG công bố / đổi lịch gom rơm của HTX → quản lý HTX (G3-2; migration 000041).
	KindCollectionSchedule = "collection_schedule"
	// KindCollectionStatement: MKG gửi bảng kê số cuộn (hoặc bản sửa) → quản lý HTX xác nhận / tranh chấp (G3-2).
	KindCollectionStatement = "collection_statement"
	// KindMRVUrgent: minh chứng sâu bệnh/thiên tai tự gắn nhãn "Cảnh báo khẩn" → người duyệt + quản trị
	// TTKN tỉnh của địa bàn (KN US-MRV-04; migration 000045).
	KindMRVUrgent = "mrv_urgent"
	// KindMRVAnomaly: nhật ký App HTX ghi AWD nhưng vùng chưa đủ hạ tầng → cán bộ khuyến nông xã phụ trách (KN FR-08).
	KindMRVAnomaly = "mrv_anomaly"
	// KindMRVOverdue: hồ sơ MRV chờ duyệt quá hạn — mức 1 người duyệt, mức 2 quản trị TTKN tỉnh (KN US-MRV-06).
	KindMRVOverdue = "mrv_overdue"
	// KindWorkScheduleSubmitted: lịch công tác tuần gửi duyệt / điều chỉnh chờ duyệt lại → người duyệt Cấp Tỉnh
	// (KN v7.7 FN-TI-02.07: "lịch Cán bộ xã gửi hiện ngay ở Chờ duyệt"). Ràng buộc kind trong DB do migration
	// pf_notify của P4 (000073) mở rộng — chưa có thì consumer không ghi được (người tích hợp thêm kind này).
	KindWorkScheduleSubmitted = "work_schedule_submitted"
	// KindWorkScheduleReviewed: kết quả duyệt (Đã duyệt / Yêu cầu chỉnh sửa kèm lý do) → người đăng ký
	// (KN v7.7 FN-XA-03.08 AC; kế hoạch 000073).
	KindWorkScheduleReviewed = "work_schedule_reviewed"
	// KindWorkScheduleDue: nhắc hạn đăng ký lịch tuần (cán bộ chưa gửi) hoặc lịch chờ duyệt quá hạn (người duyệt)
	// (KN v7.7 FN-TI-02.07 luồng phụ; kế hoạch 000073).
	KindWorkScheduleDue = "work_schedule_due"
	// KindEgressAnomaly: khối lượng xuất dữ liệu bất thường lần đầu trong ngày (pf/metering, DLP — ADR-0024,
	// G5) → người nhận DLP của tenant CHỦ DỮ LIỆU (EgressAnomalyRoles; migration pf 000083).
	KindEgressAnomaly = "egress_anomaly"
	// KindPriceReminder: nhắc nhập giá Báo giá nông sản (pf/price W11.74, FN-XA-03.04) → cán bộ xã. DÙNG LẠI loại
	// report_task_due ("tự nhắc trước hạn theo cấu hình", pf 000073): CHECK kind của pf_notify chỉ migration của
	// pf/notify sửa được (agmodgraph) — loại riêng "price_reminder" cần một migration pf_notify (người tích hợp cấp số).
	KindPriceReminder = KindReportTaskDue
	// KindSyncNow là thông điệp dữ liệu "đồng bộ ngay" (US-SYNC-02) — không tạo thông báo.
	KindSyncNow = "sync_now"
)

// Mức độ.
const (
	SeverityUrgent = "urgent"
	SeverityNormal = "normal"
)

// MaxNudgeEntities: số nhóm dữ liệu tối đa trong một yêu cầu đồng bộ ngay (KN US-SYNC-02).
const MaxNudgeEntities = 30

// NudgeRoles là các vai được yêu cầu thiết bị đồng bộ ngay (US-SYNC-02).
var NudgeRoles = []string{
	partyport.RoleKNProvinceAdmin, partyport.RoleKNNationalAdmin, syncport.RoleDeviceAdmin, authz.RolePlatformAdmin,
}

// Sự kiện module phát ra (schema trong contracts/events/).
const (
	EventNotificationCreated = "pf.notify.notification.created"
	EventPushRequested       = "pf.notify.push.requested"
	EventProducerName        = "pf/notify"
)

// Tên consumer trong cmd/worker (-modules), một consumer mỗi nhóm sự kiện.
const (
	ConsumerDirective = "pf.notify.directive"
	ConsumerContent   = "pf.notify.content"
	ConsumerTask      = "pf.notify.task"
	ConsumerSync      = "pf.notify.sync"
	ConsumerParty     = "pf.notify.party"
	ConsumerPush      = "pf.notify.push"
	// ConsumerCropCycle: nhật ký chờ duyệt, kết quả duyệt, nông dân mở vụ (App HTX, G2).
	ConsumerCropCycle = "pf.notify.cropcycle"
	// ConsumerIntegration: cảnh báo Admin về đồng bộ tích hợp — sự kiện vào DLQ (GIS US-SYNC-02),
	// dữ liệu App HTX quá hạn độ tươi (CGH FN-07).
	ConsumerIntegration = "pf.notify.integration"
	// ConsumerMRV: cảnh báo MRV (pf.mrv.alert.raised) — nhãn khẩn, bất thường AWD, leo thang quá hạn (G3).
	ConsumerMRV = "pf.notify.mrv"
	// ConsumerWorkSchedule: lịch công tác tuần (KN v7.7 P5) — gửi duyệt, kết quả duyệt, nhắc hạn.
	ConsumerWorkSchedule = "pf.notify.work_schedule"
	// ConsumerEgressAnomaly: cảnh báo DLP pf.metering.egress.anomaly_detected (G5, ADR-0024) — cùng tên
	// với meteringport.NotifyEgressAnomalyQueue.
	ConsumerEgressAnomaly = "pf.notify.egress_anomaly"
	// ConsumerPrice: nhắc nhập giá Báo giá nông sản (pf.price.reminder.issued, W11.74).
	ConsumerPrice = "pf.notify.price"
)

// EgressAnomalyRoles là người nhận cảnh báo xuất dữ liệu bất thường trong tenant chủ dữ liệu: người
// xem nhật ký xuất (metering_viewer) và quản trị nền tảng của tenant đó (chủ đề toàn tenant, theo vai).
// Tạm, chờ khách Q5-5 ("ai nhận cảnh báo DLP"): đổi danh sách ở đây khi khách chốt.
var EgressAnomalyRoles = []string{meteringport.RoleMeteringViewer, authz.RolePlatformAdmin}

// AreaFarmers tìm tài khoản nông dân nhận cảnh báo khẩn theo địa bàn (HTX
// US-NEWS-03): tài khoản đang liên kết (pf/accounts farmer_link) với nông hộ có thửa
// đang dùng (pf/geo) trong các xã. communes nil = mọi xã của tenant (cảnh báo toàn
// quốc). Gốc lắp ráp nối từ port của pf/geo và pf/accounts; không có dữ liệu cá nhân.
type AreaFarmers interface {
	FarmersInCommunes(ctx context.Context, tenantID uuid.UUID, communes []string) ([]uuid.UUID, error)
}

// JobEmailDigest là tên job gửi email tóm tắt chỉ đạo khẩn trong cmd/worker (-jobs).
const JobEmailDigest = "notify_email_digest"

// NotificationCreatedV1 là payload của pf.notify.notification.created: một thông
// báo đã vào hộp thư. Không có tiêu đề/nội dung (đọc qua API) và không có dữ liệu
// cá nhân. recipient_ref rỗng với thông báo chủ đề.
//
//ag:event pf.notify.notification.created v1
type NotificationCreatedV1 struct {
	NotificationID uuid.UUID  `json:"notification_id"`
	Kind           string     `json:"kind"`
	Severity       string     `json:"severity"`
	RecipientRef   *uuid.UUID `json:"recipient_ref"`
	Topic          bool       `json:"topic"`
	SubjectRef     *uuid.UUID `json:"subject_ref"`
	SourceEventID  uuid.UUID  `json:"source_event_id"`
	CreatedAt      string     `json:"created_at"`
}

// PushRequestedV1 là payload của pf.notify.push.requested: một lô lần gửi đẩy đã
// xếp hàng trong pf_notify.push_delivery. Consumer pf.notify.push gửi từng thiết bị.
//
//ag:event pf.notify.push.requested v1
type PushRequestedV1 struct {
	BatchID    uuid.UUID `json:"batch_id"`
	Kind       string    `json:"kind"`
	Deliveries int       `json:"deliveries"`
}
