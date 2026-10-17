package domain

import (
	"fmt"
	"time"

	"github.com/google/uuid"

	contentport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/content/port"
	coopport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cooperative/port"
	mechport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/mechanization/port"
	meteringport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/metering/port"
	mrvport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/mrv/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/notify/port"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	syncport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/sync/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/meter"
)

// Template là nội dung chung của một thông báo; người nhận gắn sau (For/ForAudience).
type Template struct {
	Kind, Severity, Title, Body, DeepLink string
	SubjectRef                            *uuid.UUID
	CreatedAt                             time.Time
}

// For dựng thông báo cá nhân.
func (t Template) For(recipient uuid.UUID) Notification {
	r := recipient
	return t.base(&r, Audience{})
}

// ForAudience dựng thông báo chủ đề.
func (t Template) ForAudience(a Audience) Notification { return t.base(nil, a) }

func (t Template) base(r *uuid.UUID, a Audience) Notification {
	return Notification{RecipientRef: r, Audience: a, Kind: t.Kind, Severity: t.Severity, Title: t.Title, Body: t.Body,
		DeepLink: t.DeepLink, SubjectRef: t.SubjectRef, CreatedAt: t.CreatedAt.UTC()}
}

// ParseTime đọc thời điểm RFC 3339 của payload; sai dạng → fallback (occurred_at của phong bì).
func ParseTime(s string, fallback time.Time) time.Time {
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t.UTC()
	}
	return fallback.UTC()
}

// viTime hiển thị giờ Việt Nam dạng 15/09/2026 10:00 (không bị Redact che).
func viTime(t time.Time) string { return t.In(clock.Zone()).Format("02/01/2006 15:04") }

func ref(u uuid.UUID) *uuid.UUID {
	if u == uuid.Nil {
		return nil
	}
	return &u
}

// AlertTemplate: cảnh báo khẩn (HTX US-NEWS-03) — tiêu đề và tóm tắt là nội dung công bố.
func AlertTemplate(p contentport.AlertPublishedV1, occurred time.Time) Template {
	id, _ := uuid.Parse(p.DocumentRef)
	return Template{Kind: port.KindAlert, Severity: port.SeverityUrgent, Title: p.Title, Body: p.Summary,
		DeepLink: "/documents/" + id.String(), SubjectRef: ref(id), CreatedAt: ParseTime(p.PublishedAt, occurred)}
}

var docLabel = map[string]string{
	"procedure": "Quy trình kỹ thuật", "guide": "Hướng dẫn", "news": "Tin tức", "supplement": "Hướng dẫn địa phương",
}

// DocumentTemplate: tài liệu/tin mới hoặc cập nhật (US-LIB-06).
func DocumentTemplate(p contentport.DocumentPublishedV1, occurred time.Time) Template {
	id, _ := uuid.Parse(p.DocumentRef)
	label := docLabel[p.Kind]
	if label == "" {
		label = "Tài liệu"
	}
	title, body := label+" mới: "+p.Title, "Đã xuất bản. Có thể tải về để xem ngoại tuyến."
	if !p.FirstVersion {
		title, body = label+" cập nhật: "+p.Title, fmt.Sprintf("Đã cập nhật lên phiên bản %d.", p.VersionNo)
	}
	return Template{Kind: port.KindDocument, Severity: port.SeverityNormal, Title: title, Body: body,
		DeepLink: "/documents/" + id.String(), SubjectRef: ref(id), CreatedAt: ParseTime(p.PublishedAt, occurred)}
}

// HouseholdRegisteredTemplate: nông hộ mới đăng ký từ App HTX (KN US-HTX-05) — không
// nhập lại tay; hồ sơ mang đúng mã định danh chung household_ref. Không có PII.
func HouseholdRegisteredTemplate(p coopport.HouseholdRegisteredV1, occurred time.Time) Template {
	return Template{Kind: port.KindHouseholdRegistered, Severity: port.SeverityNormal, Title: "Nông hộ mới đăng ký từ App HTX",
		Body:     fmt.Sprintf("Xã %s: hồ sơ nông hộ đã đồng bộ vào App Khuyến nông, không cần nhập lại.", p.CommuneCode),
		DeepLink: "/households/" + p.HouseholdRef.String(), SubjectRef: ref(p.HouseholdRef), CreatedAt: ParseTime(p.EventTime, occurred)}
}

var membershipLabel = map[string]string{
	coopport.MembershipJoined: "vào HTX", coopport.MembershipLeft: "rời HTX", coopport.MembershipTransferred: "chuyển HTX",
}

// MembershipChangedTemplate: nông hộ vào/rời/chuyển HTX (KN US-HTX-05).
func MembershipChangedTemplate(p coopport.HouseholdMembershipChangedV1, occurred time.Time) Template {
	label := membershipLabel[p.Change]
	if label == "" {
		label = "đổi thành viên HTX"
	}
	return Template{Kind: port.KindHouseholdMembershipChanged, Severity: port.SeverityNormal, Title: "Nông hộ trong địa bàn " + label,
		Body:     fmt.Sprintf("Xã %s, hiệu lực từ %s.", p.CommuneCode, p.EffectiveDate),
		DeepLink: "/households/" + p.HouseholdRef.String(), SubjectRef: ref(p.HouseholdRef), CreatedAt: ParseTime(p.EventTime, occurred)}
}

// ConflictTemplate: xung đột đồng bộ chờ xử lý (C-8).
func ConflictTemplate(p syncport.ConflictOpenedV1, occurred time.Time) Template {
	return Template{Kind: port.KindSyncConflict, Severity: port.SeverityNormal, Title: "Xung đột đồng bộ cần xử lý",
		Body:     fmt.Sprintf("Dữ liệu %s gửi từ thiết bị hiện trường xung đột; hạn xử lý %s.", p.Entity, viTime(ParseTime(p.DueAt, occurred))),
		DeepLink: "/sync/conflicts/" + p.ConflictID.String(), SubjectRef: ref(p.ConflictID), CreatedAt: ParseTime(p.OpenedAt, occurred)}
}

// OrgDeactivatedTemplate: tổ chức bị vô hiệu hóa — không đưa mã số thuế hay lý do vào thông báo.
func OrgDeactivatedTemplate(p partyport.OrgDeactivatedV1, occurred time.Time) Template {
	return Template{Kind: port.KindOrgDeactivated, Severity: port.SeverityNormal, Title: "Một tổ chức trong địa bàn đã bị vô hiệu hóa",
		Body:     fmt.Sprintf("Tài khoản thuộc tổ chức đã bị khóa; %d hộ thành viên bị ảnh hưởng.", p.ActiveHouseholds),
		DeepLink: "/orgs/" + p.OrgRef.String(), SubjectRef: ref(p.OrgRef), CreatedAt: ParseTime(p.EventTime, occurred)}
}

// DeadLetterTemplate: sự kiện đồng bộ tích hợp vào hàng chờ lỗi (GIS US-SYNC-02 AC-1) → Admin.
// Chỉ nêu nguồn, loại sự kiện và mã lỗi đã chuẩn hóa — không có dữ liệu nghiệp vụ.
func DeadLetterTemplate(p syncport.IntegrationDeadLetteredV1, occurred time.Time) Template {
	return Template{Kind: port.KindIntegrationDeadLettered, Severity: port.SeverityUrgent,
		Title: "Đồng bộ tích hợp lỗi — sự kiện đã vào hàng chờ lỗi (DLQ)",
		Body: fmt.Sprintf("Nguồn %s không xử lý được sự kiện %s sau %d lần thử (mã lỗi %s). Kiểm tra màn Giám sát đồng bộ và runbook hàng đợi.",
			p.Source, p.EventType, p.DeliveryCount+1, p.ErrorCode),
		DeepLink: "/sync/integrations/" + p.FailureID.String(), SubjectRef: ref(p.FailureID), CreatedAt: ParseTime(p.RecordedAt, occurred)}
}

// AreaFeedStaleTemplate: dữ liệu diện tích vụ App HTX quá ngưỡng độ tươi (CGH FN-07) → Admin CGH.
func AreaFeedStaleTemplate(p mechport.AreaFeedStaleV1, occurred time.Time) Template {
	return Template{Kind: port.KindAreaFeedStale, Severity: port.SeverityNormal,
		Title: "Dữ liệu App HTX chưa cập nhật quá ngưỡng độ tươi",
		Body: fmt.Sprintf("Lần đồng bộ diện tích vụ gần nhất lúc %s, quá %s giờ — Bản đồ CGH đang dùng dữ liệu nhập tay dự phòng.",
			viTime(ParseTime(p.LastSyncAt, occurred)), p.ThresholdHours),
		DeepLink: "/mechanization/area-sync-log", CreatedAt: ParseTime(p.DetectedAt, occurred)}
}

// EgressAnomalyTemplate: khối lượng xuất dữ liệu bất thường lần đầu trong ngày (G5, ADR-0024 — DLP)
// → người nhận DLP của tenant chủ dữ liệu. Chỉ kênh, sản phẩm, loại bên nhận, số bản ghi, ngưỡng và
// mã tham chiếu dòng egress_event — không dữ liệu nghiệp vụ, không dữ liệu cá nhân.
func EgressAnomalyTemplate(p meteringport.EgressAnomalyV1, occurred time.Time) Template {
	basis := "ngưỡng tuyệt đối"
	if p.Basis == "median" {
		basis = "hệ số × trung vị 28 ngày"
	}
	mode := "chế độ quan sát (không chặn)"
	if p.Mode == string(meter.ModeEnforce) {
		mode = "chế độ chặn (enforce)"
	}
	t := Template{Kind: port.KindEgressAnomaly, Severity: port.SeverityUrgent,
		Title: "Cảnh báo xuất dữ liệu bất thường (DLP)",
		Body: fmt.Sprintf("Ngày %s: kênh %s, sản phẩm %s, bên nhận loại %s đã nhận %d bản ghi, vượt ngưỡng %d (%s; %s). Kiểm tra nhật ký xuất dữ liệu.",
			p.BusinessDate, p.Channel, p.Product, p.CounterpartyKind, p.DailyRecords, p.Threshold, basis, mode),
		DeepLink: "/metering/egress/anomalies", CreatedAt: ParseTime(p.DetectedAt, occurred)}
	if id, err := uuid.Parse(p.EgressEventID); err == nil {
		t.SubjectRef = &id
	}
	return t
}

// MRVAlertTemplate: cảnh báo MRV (pf.mrv.alert.raised, G3). Chỉ mã tham chiếu và mã xã — không có
// giá trị minh chứng hay dữ liệu cá nhân.
func MRVAlertTemplate(p mrvport.AlertRaisedV1, occurred time.Time) Template {
	t := Template{Severity: p.Severity, CreatedAt: ParseTime(p.EventTime, occurred)}
	if t.Severity != port.SeverityUrgent {
		t.Severity = port.SeverityNormal
	}
	if id, err := uuid.Parse(p.SubjectRootID); err == nil {
		t.SubjectRef = &id
	}
	switch p.Kind {
	case mrvport.AlertPestUrgent:
		t.Kind, t.Severity = port.KindMRVUrgent, port.SeverityUrgent
		t.Title = "Cảnh báo khẩn: sâu bệnh/thiên tai vượt ngưỡng"
		t.Body = fmt.Sprintf("Xã %s: ghi nhận sâu bệnh/thiên tai vượt ngưỡng nguy hiểm — nguy cơ lan rộng, cần xử lý ngay.", p.AdminUnitCode)
		t.DeepLink = "/mrv/evidence/" + p.SubjectID
	case mrvport.AlertAWDNoInfrastructure:
		t.Kind = port.KindMRVAnomaly
		t.Title = "Nhật ký HTX bất thường: ghi AWD nhưng vùng chưa đủ hạ tầng"
		t.Body = fmt.Sprintf("Xã %s: nhật ký App HTX ghi tưới ướt khô xen kẽ (AWD) cho thửa chưa có xác nhận hạ tầng tưới tiêu — cần kiểm tra thực địa.", p.AdminUnitCode)
		t.DeepLink = "/mrv/alerts/" + p.AlertID
	default:
		t.Kind = port.KindMRVOverdue
		t.Title = "Hồ sơ MRV quá hạn xử lý"
		if p.Level >= mrvport.OverdueLevelProvinceAdmin {
			t.Title = "Hồ sơ MRV quá hạn xử lý đã leo thang lên tỉnh"
		}
		t.Body = fmt.Sprintf("Xã %s: hồ sơ MRV chờ duyệt đã quá thời hạn xử lý.", p.AdminUnitCode)
		t.DeepLink = "/mrv/evidence/" + p.SubjectID
	}
	return t
}
