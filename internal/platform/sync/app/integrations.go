package app

import (
	"context"
	"errors"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/sync/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/sync/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/outbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/telemetry"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// integrationRoles: vai xem màn giám sát đồng bộ tích hợp (Admin API, GIS US-SYNC-01).
var integrationRoles = []string{authz.RolePlatformAdmin, port.RoleDeviceAdmin}

// IntegrationDeps là phụ thuộc tùy chọn của màn giám sát (gốc lắp ráp nối).
type IntegrationDeps struct {
	// Consumers: mọi consumer đã khai (assembly.Queues) — hiện cả luồng chưa có sự kiện nào.
	Consumers []string
	// DLQDepth (tùy chọn): số thông điệp trong <consumer>.dlq qua RabbitMQ management API.
	DLQDepth func(ctx context.Context) (map[string]int64, error)
}

// sourceLabels: nhãn nguồn tích hợp theo consumer (tiếng Việt, màn giám sát).
var sourceLabels = map[string]string{
	"pf.mechanization.area_sync": "App HTX → Bản đồ CGH (diện tích vụ)",
	"pf.geo.hub_valve":           "ERP/MKG → GIS (van công bố kho/Hub)",
	"pf.cropcycle.gps_log":       "App HTX → GIS (log GPS mùa vụ)",
	"pf.extension.gps_log":       "App HTX → GIS (log GPS yêu cầu hỗ trợ)",
	"pf.simulation.snapshot":     "Mô phỏng (ảnh chụp đầu vào)",
	"pf.simulation.optimizer":    "Bộ tối ưu mô phỏng",
	"pf.accounts.org_status":     "Tổ chức → tài khoản",
	"pf.waterway.build":          "Dựng mạng đường thủy",
	"pf.mrv.export":              "MRV (gói xuất)",
	"pf.reports.export":          "Báo cáo (dựng tệp)",
	"pf.reports.delivery":        "Báo cáo (gửi email)",
	"pf.audit.consent":           "Nhật ký đồng ý",
}

// SourceLabel trả nhãn nguồn của consumer.
func SourceLabel(consumer string) string {
	if l, ok := sourceLabels[consumer]; ok {
		return l
	}
	if strings.HasPrefix(consumer, "pf.notify.") {
		return "Thông báo (" + strings.TrimPrefix(consumer, "pf.notify.") + ")"
	}
	return consumer
}

var errCodeRe = regexp.MustCompile(`[^a-z0-9_.:]`)

// ErrorCode chuẩn hóa lỗi của handler thành mã không chứa dữ liệu nghiệp vụ (R-14): mã
// problem của module, "db:<SQLSTATE>", "timeout", "canceled" hoặc "internal".
func ErrorCode(err error) string {
	var p *problem.Problem
	var pg *pgconn.PgError
	switch {
	case err == nil:
		return "internal"
	case errors.As(err, &p):
		return sanitizeCode(p.Code)
	case errors.As(err, &pg):
		return sanitizeCode("db:" + strings.ToLower(pg.Code))
	case errors.Is(err, context.DeadlineExceeded):
		return "timeout"
	case errors.Is(err, context.Canceled):
		return "canceled"
	}
	return "internal"
}

func sanitizeCode(s string) string {
	s = errCodeRe.ReplaceAllString(strings.ToLower(s), "_")
	if s == "" {
		return "internal"
	}
	if len(s) > 120 {
		s = s[:120]
	}
	return s
}

// RecordFailure ghi một lần xử lý thất bại của consumer (tác nhân hệ thống — worker, không
// có người gọi) trong giao dịch riêng của tenant; lần giao cuối (vào DLQ) phát thêm
// pf.sync.integration.dead_lettered đúng một lần cho mỗi (sự kiện, consumer) để pf/notify
// báo Admin (GIS US-SYNC-02 AC-1).
func (s *Service) RecordFailure(ctx context.Context, f port.ConsumerFailure) error {
	if f.TenantID == uuid.Nil || f.EventID == uuid.Nil || f.Consumer == "" {
		return errors.New("sync: lần thất bại thiếu tenant, sự kiện hoặc consumer")
	}
	row := store.IntegrationFailure{
		ID: ids.New(), Consumer: f.Consumer, EventID: f.EventID, EventType: f.EventType, ErrorCode: sanitizeCode(f.ErrorCode),
		DeliveryCount: f.DeliveryCount, DeadLettered: f.DeadLettered, EventTime: f.EventTime,
	}
	if row.EventTime.IsZero() {
		row.EventTime = s.Clock.Now()
	}
	return tenant.WithTenantTx(ctx, s.DB, f.TenantID, func(tx pgx.Tx) error {
		inserted, err := store.InsertIntegrationFailure(ctx, tx, f.TenantID, &row)
		if err != nil || !inserted || !row.DeadLettered {
			return err
		}
		return outbox.Enqueue(ctx, tx, outbox.Event{
			ID: ids.New(), TenantID: f.TenantID, Type: port.EventIntegrationDeadLettered, SchemaVersion: 1,
			OccurredAt: row.RecordedAt, TraceID: telemetry.TraceID(ctx), Producer: "pf/sync",
			Payload: port.IntegrationDeadLetteredV1{
				FailureID: row.ID, Consumer: row.Consumer, Source: SourceLabel(row.Consumer), EventID: row.EventID,
				EventType: row.EventType, ErrorCode: row.ErrorCode, DeliveryCount: row.DeliveryCount,
				RecordedAt: row.RecordedAt.UTC().Format(time.RFC3339),
			},
		})
	})
}

// IntegrationStatus là tình trạng một luồng đồng bộ (một consumer).
type IntegrationStatus struct {
	Consumer      string
	Source        string
	Status        string
	Processed     int64
	LastProcessed *time.Time
	LastEventType *string
	Failures      int64
	DeadLettered  int64
	LastFailure   *time.Time
	LastErrorCode *string
	DLQDepth      *int64 // nil = không đo được (chưa cấu hình management API)
}

// IntegrationSummary là màn giám sát đồng bộ.
type IntegrationSummary struct {
	Since        time.Time
	Items        []IntegrationStatus
	DLQAvailable bool
}

// MaxWindow là cửa sổ lớn nhất của màn giám sát.
const MaxWindow = 30 * 24 * time.Hour

// Integrations trả tình trạng mọi luồng đồng bộ theo consumer trong cửa sổ window (mặc định
// 24 giờ): số sự kiện xử lý, lần thành công gần nhất, số lần lỗi, số sự kiện vào DLQ, mã lỗi
// gần nhất, độ sâu DLQ hiện tại (nếu đo được). source lọc theo consumer; status lọc theo trạng thái.
func (s *Service) Integrations(ctx context.Context, window time.Duration, consumer, status string) (IntegrationSummary, error) {
	p, err := authz.Require(ctx, integrationRoles...)
	if err != nil {
		return IntegrationSummary{}, err
	}
	switch status {
	case "", port.IntegrationOK, port.IntegrationError, port.IntegrationIdle:
	default:
		return IntegrationSummary{}, problem.Validation.WithField("status", "phải là ok, error hoặc idle")
	}
	if window <= 0 {
		window = 24 * time.Hour
	}
	if window > MaxWindow {
		return IntegrationSummary{}, problem.Validation.WithField("window_hours", "tối đa 720 giờ")
	}
	since := s.Clock.Now().Add(-window)
	var oks []store.ConsumerOK
	var errs []store.ConsumerErr
	err = tenant.WithTenantTx(ctx, s.DB, p.TenantID, func(tx pgx.Tx) error {
		var e error
		if oks, e = store.InboxSummary(ctx, tx, since); e != nil {
			return e
		}
		errs, e = store.FailureSummary(ctx, tx, since)
		return e
	})
	if err != nil {
		return IntegrationSummary{}, err
	}
	by := map[string]*IntegrationStatus{}
	get := func(c string) *IntegrationStatus {
		if st, ok := by[c]; ok {
			return st
		}
		st := &IntegrationStatus{Consumer: c, Source: SourceLabel(c)}
		by[c] = st
		return st
	}
	for _, c := range s.Integration.Consumers {
		get(c)
	}
	for _, o := range oks {
		st := get(o.Consumer)
		st.Processed, st.LastProcessed, st.LastEventType = o.Processed, o.LastProcessed, o.LastEventType
	}
	for _, e := range errs {
		st := get(e.Consumer)
		st.Failures, st.DeadLettered, st.LastFailure, st.LastErrorCode = e.Failures, e.DeadLettered, e.LastFailure, e.LastErrorCode
	}
	out := IntegrationSummary{Since: since}
	if s.Integration.DLQDepth != nil {
		if depth, err := s.Integration.DLQDepth(ctx); err == nil {
			out.DLQAvailable = true
			for _, st := range by {
				n := depth[st.Consumer+".dlq"]
				st.DLQDepth = &n
			}
		} else {
			s.Log.Warn("không đọc được độ sâu DLQ", "event", "sync.integrations", "outcome", "error")
		}
	}
	for _, st := range by {
		st.Status = integrationState(*st, since)
		if (consumer == "" || st.Consumer == consumer) && (status == "" || st.Status == status) {
			out.Items = append(out.Items, *st)
		}
	}
	sort.Slice(out.Items, func(i, j int) bool { return out.Items[i].Consumer < out.Items[j].Consumer })
	return out, nil
}

// integrationState: lỗi nếu lần thất bại gần nhất sau lần thành công gần nhất, hoặc có
// sự kiện vào DLQ trong cửa sổ, hoặc DLQ đang có thông điệp; chưa có gì trong cửa sổ → idle.
func integrationState(st IntegrationStatus, since time.Time) string {
	switch {
	case st.DeadLettered > 0 || (st.DLQDepth != nil && *st.DLQDepth > 0):
		return port.IntegrationError
	case st.LastFailure != nil && (st.LastProcessed == nil || st.LastFailure.After(*st.LastProcessed)):
		return port.IntegrationError
	case st.Processed == 0 && st.Failures == 0 && (st.LastProcessed == nil || st.LastProcessed.Before(since)):
		return port.IntegrationIdle
	}
	return port.IntegrationOK
}

// LogQuery là bộ lọc nhật ký đồng bộ.
type LogQuery struct {
	Consumer   string
	Status     string // ok | error | ""
	From, To   time.Time
	AfterAt    *time.Time
	AfterEvent uuid.UUID
	Limit      int
}

// LogPage là một trang nhật ký.
type LogPage struct {
	Items   []store.IntegrationLogRow
	NextAt  *time.Time
	NextID  uuid.UUID
	HasNext bool
}

// IntegrationLog trả nhật ký đồng bộ (mỗi sự kiện một dòng: thời điểm, nguồn, loại, thành
// công/lỗi, mã lỗi), mới nhất trước, lọc theo nguồn/thời gian/trạng thái (GIS US-SYNC-01 AC-1).
func (s *Service) IntegrationLog(ctx context.Context, q LogQuery) (LogPage, error) {
	p, err := authz.Require(ctx, integrationRoles...)
	if err != nil {
		return LogPage{}, err
	}
	if q.Status != "" && q.Status != port.IntegrationOK && q.Status != port.IntegrationError {
		return LogPage{}, problem.Validation.WithField("status", "phải là ok hoặc error")
	}
	now := s.Clock.Now()
	if q.To.IsZero() {
		q.To = now.Add(time.Second)
	}
	if q.From.IsZero() {
		q.From = q.To.Add(-7 * 24 * time.Hour)
	}
	if !q.From.Before(q.To) {
		return LogPage{}, problem.Validation.WithField("from", "phải trước to")
	}
	if q.Limit <= 0 || q.Limit > 200 {
		q.Limit = 50
	}
	var rows []store.IntegrationLogRow
	err = tenant.WithTenantTx(ctx, s.DB, p.TenantID, func(tx pgx.Tx) error {
		var e error
		rows, e = store.IntegrationLog(ctx, tx, store.LogFilter{Consumer: q.Consumer, Status: q.Status, From: q.From, To: q.To,
			AfterAt: q.AfterAt, AfterEvent: q.AfterEvent, Limit: q.Limit + 1})
		return e
	})
	if err != nil {
		return LogPage{}, err
	}
	pg := LogPage{Items: rows}
	if len(rows) > q.Limit {
		pg.Items = rows[:q.Limit]
		last := pg.Items[q.Limit-1]
		at := last.At
		pg.NextAt, pg.NextID, pg.HasNext = &at, last.EventID, true
	}
	return pg, nil
}
