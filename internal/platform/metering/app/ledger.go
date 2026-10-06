package app

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	apiclientport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/apiclient/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/metering/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/metering/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/metering/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

var (
	_ port.Ledger = (*Service)(nil)
	_ port.Quotas = (*Service)(nil)
	_ port.Usage  = (*Service)(nil)
)

func validateCall(op uuid.UUID, c port.Call) error {
	switch {
	case op == uuid.Nil:
		return errors.New("metering: thiếu tenant đơn vị vận hành")
	case c.RequestID == uuid.Nil || c.ClientID == uuid.Nil || c.ClientTenantID == uuid.Nil:
		return errors.New("metering: thiếu request_id, client_id hoặc client_tenant_id")
	case c.ReceivedAt.IsZero():
		return errors.New("metering: thiếu thời điểm nhận")
	case c.Status < 100 || c.Status > 599:
		return fmt.Errorf("metering: mã HTTP %d không hợp lệ", c.Status)
	case c.RouteTemplate == "" || c.AuthMethod == "":
		return errors.New("metering: thiếu mẫu route hoặc phương thức xác thực")
	case c.Rows < 0 || c.Bytes < 0 || c.Latency < 0:
		return errors.New("metering: số dòng/byte/độ trễ âm")
	}
	switch c.Kind {
	case port.CallRead, port.CallExportRequest, port.CallJobStatus, port.CallFileDownload, port.CallMeta:
	default:
		return fmt.Errorf("metering: loại lời gọi %q không hợp lệ", c.Kind)
	}
	return nil
}

// recordOut là kết quả nội bộ của một lần ghi sổ.
type recordOut struct {
	rec      port.Recorded
	status   int
	units    int64
	quotaErr error
	replay   bool
	egress   *passed
}

func optStr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func optID(id uuid.UUID) *uuid.UUID {
	if id == uuid.Nil {
		return nil
	}
	return &id
}

// Record thực hiện port.Ledger (ADR-0023 mục 1–3): MỘT giao dịch metering_app — dòng usage_event ở tenant
// đơn vị vận hành, bộ đếm hạn mức (khóa dòng '*' của khách hàng → song song không bao giờ vượt hạn mức),
// và khi phục vụ dữ liệu (DataTenantID ≠ Nil, 2xx) dòng egress_event kênh public_api ở tenant chủ dữ
// liệu (tenant.Within, ADR-0024 — luôn enforce). Vượt hạn mức gói → dòng vẫn ghi với status 429,
// units 0, trả ErrQuotaExceeded; checkpoint từ chối → 429 ErrEgressQuotaExceeded. Lỗi hạ tầng →
// ErrUnavailable (Public API không gửi byte nào). Idempotent theo RequestID.
func (s *Service) Record(ctx context.Context, op uuid.UUID, c port.Call) (port.Recorded, error) {
	if err := validateCall(op, c); err != nil {
		s.metrics().ledgerError(ctx)
		return port.Recorded{}, port.ErrUnavailable.WithCause(err)
	}
	out, err := s.record(ctx, op, c)
	if err != nil {
		s.metrics().ledgerError(ctx)
		if c.DataTenantID != uuid.Nil {
			s.metrics().checkpointError(ctx, string(port.ChannelPublicAPI))
		}
		s.log().ErrorContext(ctx, "ghi sổ sử dụng lỗi — không trả dữ liệu", "event", "metering.record", "request_id", c.RequestID,
			"client_id", c.ClientID, "outcome", "error", "error", err.Error())
		return port.Recorded{}, port.ErrUnavailable.WithCause(err)
	}
	if !out.replay {
		s.metrics().call(ctx, domain.StatusClass(out.status), out.units, errors.Is(out.quotaErr, port.ErrQuotaExceeded))
		if out.egress != nil && !out.egress.replay {
			s.metrics().egress(ctx, string(port.ChannelPublicAPI), out.egress.decision.Allowed, out.egress.decision.Reason, c.Rows, c.Bytes,
				out.egress.firstAnomaly)
		}
	}
	return out.rec, out.quotaErr
}

func (s *Service) record(ctx context.Context, op uuid.UUID, c port.Call) (recordOut, error) {
	plan, err := s.plan(ctx, op, c.ClientID, c.ReceivedAt)
	if err != nil {
		return recordOut{}, fmt.Errorf("metering: gói hạn mức: %w", err)
	}
	serves := c.DataTenantID != uuid.Nil && c.Status >= 200 && c.Status <= 299
	var eparams domain.EgressParams
	if serves {
		if eparams, err = s.EgressParams(ctx, c.DataTenantID, port.ChannelPublicAPI, c.ReceivedAt); err != nil {
			return recordOut{}, err
		}
	}
	var out recordOut
	for attempt := 0; ; attempt++ {
		err = s.tx(ctx, op, func(tx pgx.Tx) error {
			var e2 error
			out, e2 = s.recordTx(ctx, tx, op, c, plan, serves, eparams)
			return e2
		})
		var pg *pgconn.PgError
		if attempt == 0 && errors.As(err, &pg) && pg.Code == "23505" {
			continue // cùng request_id ghi song song: lần sau đọc lại dòng đã ghi
		}
		return out, err
	}
}

func remaining(limit, used int64) int64 {
	if r := limit - used; r > 0 {
		return r
	}
	return 0
}

func (s *Service) recordTx(ctx context.Context, tx pgx.Tx, op uuid.UUID, c port.Call, plan apiclientport.Plan, serves bool, ep domain.EgressParams) (recordOut, error) {
	day, month := domain.Day(c.ReceivedAt), domain.Month(c.ReceivedAt)
	reset := domain.NextDayStart(c.ReceivedAt)
	if prev, err := store.UsageByIdem(ctx, tx, c.RequestID.String()); err != nil {
		return recordOut{}, err
	} else if prev != nil {
		dc, err := store.ReadCounter(ctx, tx, c.ClientID, domain.AllProducts, "day", day)
		if err != nil {
			return recordOut{}, err
		}
		mc, err := store.ReadCounter(ctx, tx, c.ClientID, domain.AllProducts, "month", month)
		if err != nil {
			return recordOut{}, err
		}
		out := recordOut{replay: true, status: prev.Status, units: prev.Units, rec: port.Recorded{EventID: prev.ID, Units: prev.Units,
			RuleVersion: prev.RuleVersion, DailyRemaining: remaining(plan.DailyUnits, dc.Units), MonthlyRemaining: remaining(plan.MonthlyUnits, mc.Units),
			WindowReset: reset}}
		if prev.ProblemCode != nil && *prev.ProblemCode == port.ErrQuotaExceeded.Code {
			out.quotaErr = port.ErrQuotaExceeded
		} else if prev.ProblemCode != nil && *prev.ProblemCode == port.ErrEgressQuotaExceeded.Code {
			out.quotaErr = port.ErrEgressQuotaExceeded
		}
		return out, nil
	}
	// Thứ tự khóa cố định — mọi lời gọi của cùng khách hàng xếp hàng ở dòng '*' ngày.
	dayAll, err := store.LockCounter(ctx, tx, op, c.ClientID, domain.AllProducts, "day", day)
	if err != nil {
		return recordOut{}, err
	}
	monthAll, err := store.LockCounter(ctx, tx, op, c.ClientID, domain.AllProducts, "month", month)
	if err != nil {
		return recordOut{}, err
	}
	if _, err := store.LockCounter(ctx, tx, op, c.ClientID, c.Product, "day", day); err != nil {
		return recordOut{}, err
	}
	if _, err := store.LockCounter(ctx, tx, op, c.ClientID, c.Product, "month", month); err != nil {
		return recordOut{}, err
	}
	units := port.UnitsV1(c)
	status, problemCode := c.Status, c.ProblemCode
	var quotaErr error
	if units > 0 {
		switch {
		case dayAll.Units+units > plan.DailyUnits:
			quotaErr = port.ErrQuotaExceeded
		case monthAll.Units+units > plan.MonthlyUnits:
			quotaErr = port.ErrQuotaExceeded
			reset = domain.NextMonthStart(c.ReceivedAt)
		}
	}
	if quotaErr != nil {
		status, problemCode, units, serves = http.StatusTooManyRequests, port.ErrQuotaExceeded.Code, 0, false
	}
	var eg *passed
	if serves {
		e := port.Egress{SourceID: c.RequestID, Channel: port.ChannelPublicAPI, DataTenantID: c.DataTenantID,
			CounterpartyKind: port.CounterpartyAPIClient, CounterpartyRef: c.ClientID, Product: c.Product, ContractVersion: c.ContractVersion,
			Records: c.Rows, Bytes: c.Bytes, OccurredAt: c.ReceivedAt, ActorRef: c.ClientID, TraceID: c.TraceID}
		var p passed
		// H4: DataTenantID là bên cấp đã kiểm cấp quyền dp_* ở apiclient/public (party/port.GrantLookup) trước khi
		// gọi sổ. GrantLookup (grantlookup) chưa trả bằng chứng — chờ chuyển ra adapter (review M9) — nên đúc ở
		// đây, nơi duy nhất ngoài pf/party có tên trong danh sách đúc của archlint agtenantgate.
		if err := tenant.Within(ctx, tx, tenant.MintGrant(op, c.DataTenantID), func() error {
			var e2 error
			p, e2 = s.passTx(ctx, tx, e, ep, port.ModeEnforce)
			return e2
		}); err != nil {
			return recordOut{}, err
		}
		eg = &p
		if !p.decision.Allowed {
			quotaErr = port.ErrEgressQuotaExceeded
			status, problemCode, units = http.StatusTooManyRequests, port.ErrEgressQuotaExceeded.Code, 0
		}
	}
	dataTenant := optID(c.DataTenantID)
	if eg == nil || !eg.decision.Allowed {
		dataTenant = nil // không phục vụ dữ liệu
	}
	row := store.UsageRow{
		ID: ids.New(), StreamKey: domain.UsageStream(c.ClientID, domain.Period(c.ReceivedAt)), ClientID: c.ClientID, ClientTenantID: c.ClientTenantID,
		KeyPrefix: c.KeyPrefix, AuthMethod: c.AuthMethod, CallKind: string(c.Kind), Product: c.Product, ContractVer: c.ContractVersion,
		Scope: c.Scope, RouteTemplate: c.RouteTemplate, Status: status, ProblemCode: optStr(problemCode), DataTenantID: dataTenant, Units: units,
		RowsOut: c.Rows, BytesOut: c.Bytes, LatencyMS: c.Latency.Milliseconds(), Replayed: c.Replayed, JobID: optID(c.JobID),
		TraceID: traceRef(ctx, c.TraceID), EventTime: c.ReceivedAt.UTC(), IdemKey: c.RequestID.String(), RuleVersion: port.RuleUnitsV1,
	}
	if quotaErr != nil {
		row.RowsOut, row.BytesOut = 0, 0
	}
	if err := store.InsertUsage(ctx, tx, op, &row); err != nil {
		return recordOut{}, ledgerErr(err)
	}
	for _, w := range []struct {
		product, kind string
		start         time.Time
	}{{domain.AllProducts, "day", day}, {domain.AllProducts, "month", month}, {c.Product, "day", day}, {c.Product, "month", month}} {
		if err := store.BumpCounter(ctx, tx, c.ClientID, w.product, w.kind, w.start, units); err != nil {
			return recordOut{}, err
		}
	}
	return recordOut{status: status, units: units, quotaErr: quotaErr, egress: eg, rec: port.Recorded{
		EventID: row.ID, Units: units, RuleVersion: port.RuleUnitsV1,
		DailyRemaining: remaining(plan.DailyUnits, dayAll.Units+units), MonthlyRemaining: remaining(plan.MonthlyUnits, monthAll.Units+units),
		WindowReset: reset,
	}}, nil
}

// Remaining thực hiện port.Quotas: hạn mức còn lại từ bộ đếm ĐÃ COMMIT (không giữ chỗ). Hạn mức gói
// tính trên tổng mọi sản phẩm của khách hàng (dòng '*'); product chỉ để tương thích chữ ký.
func (s *Service) Remaining(ctx context.Context, op, client uuid.UUID, _ string, at time.Time) (port.Remaining, error) {
	plan, err := s.plan(ctx, op, client, at)
	if err != nil {
		return port.Remaining{}, port.ErrUnavailable.WithCause(err)
	}
	var dc, mc store.Counter
	err = s.tx(ctx, op, func(tx pgx.Tx) error {
		var e error
		if dc, e = store.ReadCounter(ctx, tx, client, domain.AllProducts, "day", domain.Day(at)); e != nil {
			return e
		}
		mc, e = store.ReadCounter(ctx, tx, client, domain.AllProducts, "month", domain.Month(at))
		return e
	})
	if err != nil {
		return port.Remaining{}, port.ErrUnavailable.WithCause(err)
	}
	r := port.Remaining{DailyUnits: remaining(plan.DailyUnits, dc.Units), MonthlyUnits: remaining(plan.MonthlyUnits, mc.Units),
		WindowReset: domain.NextDayStart(at)}
	if r.MonthlyUnits == 0 {
		r.WindowReset = domain.NextMonthStart(at)
	}
	return r, nil
}

// UsageOf thực hiện port.Usage: tổng theo ngày × sản phẩm (giờ VN, bao gồm hai đầu) của CHÍNH khách
// hàng client (client_id lấy từ khóa ở api-public), từ bộ đếm.
func (s *Service) UsageOf(ctx context.Context, op, client uuid.UUID, from, to time.Time) ([]port.UsageDay, error) {
	if to.Before(from) {
		return nil, problem.Validation.WithField("to", "phải ≥ from")
	}
	var rows []store.UsageDayRow
	err := s.tx(ctx, op, func(tx pgx.Tx) error {
		var e error
		rows, e = store.UsageDays(ctx, tx, client, domain.Day(from), domain.Day(to))
		return e
	})
	if err != nil {
		return nil, err
	}
	out := make([]port.UsageDay, 0, len(rows))
	for _, r := range rows {
		out = append(out, port.UsageDay{Date: r.Date.Format(time.DateOnly), Product: r.Product, Calls: r.Calls, Units: r.Units})
	}
	return out, nil
}
