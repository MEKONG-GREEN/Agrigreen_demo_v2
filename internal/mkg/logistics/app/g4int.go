package app

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/metric/noop"

	hubport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/hub/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/logistics/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/logistics/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/logistics/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// ErrNotAssignedDriver: tài xế ghi mốc cho chuyến không giao cho mình (G4 tích hợp mục 9).
var ErrNotAssignedDriver = problem.New(403, "logistics.not_assigned_driver", "Chuyến không giao cho tài xế này — chỉ tài xế được giao hoặc điều phối ghi mốc")

// DriverOnly cho biết người gọi chỉ có vai tài xế (không điều phối): danh sách tự lọc "chuyến của tôi", không
// thấy cước (G4 tích hợp mục 8, 10). driver_ref của chuyến = sub của token tài khoản tài xế tenant MKG
// (g4-conventions §3.2 — như member_refs của tổ gom fieldops); định danh (tên, điện thoại) ở vault.
func DriverOnly(p authz.Principal) bool { return p.Has(RoleDriver) && !p.Has(RoleDispatcher) }

// assignedTo cho biết chuyến giao cho người gọi.
func assignedTo(t *store.Trip, p authz.Principal) bool {
	return t.DriverRef != nil && *t.DriverRef == p.ActorRef
}

// TripQuery là truy vấn danh sách chuyến.
type TripQuery struct {
	Status       string
	AssignedToMe bool       // chỉ chuyến có driver_ref = sub của người gọi (tự bật với người chỉ có vai driver)
	UpdatedSince *time.Time // delta theo recorded_at (sắp tăng dần)
	AfterAt      *time.Time
	AfterID      uuid.UUID
	Limit        int
}

// QueryTrips liệt kê chuyến theo truy vấn (điều phối, tài xế). Trả thêm DriverOnly của người gọi để lớp HTTP
// ẩn cước.
func (s *Service) QueryTrips(ctx context.Context, q TripQuery) ([]store.Trip, bool, error) {
	if q.Limit <= 0 || q.Limit > 200 {
		q.Limit = 50
	}
	p, err := authz.Require(ctx, RoleDispatcher, RoleDriver)
	if err != nil {
		return nil, false, err
	}
	f := store.TripFilter{Status: q.Status, UpdatedSince: q.UpdatedSince, AfterAt: q.AfterAt, AfterID: q.AfterID, Limit: q.Limit}
	if q.AssignedToMe || DriverOnly(p) {
		me := p.ActorRef
		f.DriverRef = &me
	}
	var out []store.Trip
	err = s.read(ctx, func(tx pgx.Tx) error {
		var e error
		out, e = store.ListTripsFiltered(ctx, tx, f)
		return e
	})
	return out, DriverOnly(p), err
}

// ---------------------------------------------------------------------------
// L-08: job cảnh báo định kỳ (ghe chưa cân > N giờ, đăng kiểm hết / sắp hết hạn)
// ---------------------------------------------------------------------------

// Loại thông báo.
const (
	NoticeTripUnweighed        = "trip_unweighed"
	NoticeRegistrationExpired  = "registration_expired"
	NoticeRegistrationExpiring = "registration_expiring"
	RuleAlertNotice            = "logistics.alert_notice.v1"
	MetricAlertsOpen           = "ag.logistics.alerts.open"
	MetricAlertNotices         = "ag.logistics.alert.notices"
	MetricAlertScans           = "ag.logistics.alerts.scans"
	alertKindAttr              = "kind"
)

// Người nhận theo loại (vai trong Keycloak; proto nhắc ghe chưa cân: vận hành, điều phối, kho).
var (
	AudienceUnweighed    = []string{RoleDispatcher, hubport.RoleHubOperator}
	AudienceRegistration = []string{RoleDispatcher}
)

// TenantLister liệt kê tenant cần quét (pool vai ag_outbox_publisher, chỉ đọc cột tenant_id, status).
type TenantLister func(ctx context.Context) ([]uuid.UUID, error)

// AlertScan là kết quả một lượt quét.
type AlertScan struct {
	Tenants    int
	Open       map[string]int // số điều kiện cảnh báo đang mở theo loại (mọi tenant)
	NewNotices map[string]int // thông báo MỚI ghi ở lượt này
}

// Metrics là bộ đo của job cảnh báo (OTLP → Prometheus: ag_logistics_alerts_open{kind},
// ag_logistics_alert_notices_total{kind}, ag_logistics_alerts_scans_total{outcome}). Luật:
// deploy/observability/prometheus/agrigreen-mkg-logistics.rules.yml.
type Metrics struct {
	open    metric.Int64Gauge
	notices metric.Int64Counter
	scans   metric.Int64Counter

	mu     sync.Mutex
	counts map[string]int64
}

// NewMetrics tạo bộ đo trên meter (nil → noop).
func NewMetrics(m metric.Meter) *Metrics {
	if m == nil {
		m = noop.NewMeterProvider().Meter("logistics")
	}
	x := &Metrics{counts: map[string]int64{}}
	x.open, _ = m.Int64Gauge(MetricAlertsOpen, metric.WithDescription("Điều kiện cảnh báo logistics đang mở (L-08, L-10) theo loại"))
	x.notices, _ = m.Int64Counter(MetricAlertNotices, metric.WithDescription("Thông báo cảnh báo logistics mới đã ghi (chống lặp theo đối tượng)"))
	x.scans, _ = m.Int64Counter(MetricAlertScans, metric.WithDescription("Lượt quét cảnh báo logistics theo kết quả"))
	return x
}

// Count trả số đếm trong tiến trình (kiểm thử): "open:<kind>", "notices:<kind>", "scans:<outcome>".
func (m *Metrics) Count(key string) int64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.counts[key]
}

func (m *Metrics) record(ctx context.Context, r AlertScan, outcome string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, k := range []string{NoticeTripUnweighed, NoticeRegistrationExpired, NoticeRegistrationExpiring} {
		m.open.Record(ctx, int64(r.Open[k]), metric.WithAttributes(attribute.String(alertKindAttr, k)))
		m.counts["open:"+k] = int64(r.Open[k])
		if n := r.NewNotices[k]; n > 0 {
			m.notices.Add(ctx, int64(n), metric.WithAttributes(attribute.String(alertKindAttr, k)))
			m.counts["notices:"+k] += int64(n)
		}
	}
	m.scans.Add(ctx, 1, metric.WithAttributes(attribute.String("outcome", outcome)))
	m.counts["scans:"+outcome]++
}

func (s *Service) metrics() *Metrics {
	if s.Metrics == nil {
		s.Metrics = NewMetrics(nil)
	}
	return s.Metrics
}

// ScanAlerts quét mọi tenant (job logistics_alerts của api-internal, chỉ leader): ghe chưa cân quá
// logistics.trip.unweighed_alert_hours (proto nhắc ghe chưa cân, L-10) và phương tiện đang dùng hết / sắp hết hạn
// đăng kiểm (GH-02, L-08), theo tham số tại thời điểm quét. Mỗi điều kiện ghi MỘT thông báo (chống lặp theo
// chuyến / theo (phương tiện, hạn đăng kiểm)) vào alert_notice và cập nhật metric để Alertmanager gửi — chỉ cảnh
// báo, không chặn nghiệp vụ nào. Lỗi của một tenant không dừng tenant khác (trả lỗi gộp).
func (s *Service) ScanAlerts(ctx context.Context, tenants TenantLister) (AlertScan, error) {
	out := AlertScan{Open: map[string]int{}, NewNotices: map[string]int{}}
	tids, err := tenants(ctx)
	if err != nil {
		s.metrics().record(ctx, out, "error")
		return out, err
	}
	var errs []error
	for _, tid := range tids {
		if err := tenant.WithTenantTx(ctx, s.DB, tid, func(tx pgx.Tx) error { return s.scanTenant(ctx, tx, tid, &out) }); err != nil {
			errs = append(errs, err)
			continue
		}
		out.Tenants++
	}
	outcome := "ok"
	if len(errs) > 0 {
		outcome = "error"
	}
	s.metrics().record(ctx, out, outcome)
	return out, errors.Join(errs...)
}

func (s *Service) scanTenant(ctx context.Context, tx pgx.Tx, tid uuid.UUID, out *AlertScan) error {
	now := s.Clock.Now()
	h, err := param(ctx, tx, port.ParamUnweighedAlertHours, now)
	if err != nil {
		return err
	}
	hours := int(h.Value.IntPart())
	trips, err := store.UnweighedTrips(ctx, tx, now.Add(-time.Duration(hours)*time.Hour))
	if err != nil {
		return err
	}
	for _, t := range trips {
		out.Open[NoticeTripUnweighed]++
		n := store.Notice{ID: ids.New(), Kind: NoticeTripUnweighed, SubjectRef: t.TripID, SubjectCode: t.Code,
			DedupeKey: NoticeTripUnweighed + ":" + t.TripID.String(), Audience: AudienceUnweighed, ParamVersion: h.Version,
			RuleVersion: RuleAlertNotice, NoticedAt: now,
			Detail: map[string]string{"planned_at": t.PlannedAt.UTC().Format(time.RFC3339), "status": string(t.Status),
				"threshold_hours": h.Value.String(), "hours_waiting": itoa(int(now.Sub(t.PlannedAt) / time.Hour))}}
		if t.VehicleRef != nil {
			n.Detail["vehicle_ref"] = t.VehicleRef.String()
		}
		if err := s.notice(ctx, tx, tid, &n, out); err != nil {
			return err
		}
	}
	vs, err := store.ListVessels(ctx, tx, "active")
	if err != nil {
		return err
	}
	for _, v := range vs {
		d, err := decorate(ctx, tx, v, now)
		if err != nil {
			return err
		}
		kind := ""
		switch d.Expiry {
		case domain.ExpiryExpired:
			kind = NoticeRegistrationExpired
		case domain.ExpirySoon:
			kind = NoticeRegistrationExpiring
		default:
			continue
		}
		out.Open[kind]++
		exp := v.RegistrationExpiry.UTC().Format("2006-01-02")
		n := store.Notice{ID: ids.New(), Kind: kind, SubjectRef: v.VesselID, SubjectCode: v.Code, DedupeKey: kind + ":" + v.VesselID.String() + ":" + exp,
			Audience: AudienceRegistration, ParamVersion: d.WarnVersion, RuleVersion: RuleAlertNotice, NoticedAt: now,
			Detail: map[string]string{"registration_expiry": exp, "warn_days": itoa(d.WarnDays)}}
		if d.DaysLeft != nil {
			n.Detail["days_left"] = itoa(*d.DaysLeft)
		}
		if err := s.notice(ctx, tx, tid, &n, out); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) notice(ctx context.Context, tx pgx.Tx, tid uuid.UUID, n *store.Notice, out *AlertScan) error {
	fresh, err := store.InsertNotice(ctx, tx, tid, n)
	if err != nil {
		return err
	}
	if fresh {
		out.NewNotices[n.Kind]++
	}
	return nil
}

// Notices liệt kê thông báo cảnh báo đã gửi (điều phối), mới nhất trước.
func (s *Service) Notices(ctx context.Context, kind string, afterAt *time.Time, afterID uuid.UUID, limit int) ([]store.Notice, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	var out []store.Notice
	err := s.read(ctx, func(tx pgx.Tx) error {
		var e error
		out, e = store.Notices(ctx, tx, kind, afterAt, afterID, limit)
		return e
	}, RoleDispatcher)
	return out, err
}
