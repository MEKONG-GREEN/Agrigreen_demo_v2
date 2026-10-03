package app

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/metering/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/metering/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/metering/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ledger"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/outbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/telemetry"
)

var _ port.Checkpoint = (*Service)(nil)

// passed là kết quả nội bộ của một lần qua điểm kiểm soát (để tăng metric sau commit).
type passed struct {
	decision     port.Decision
	firstAnomaly bool
	replay       bool
}

func validateEgress(e port.Egress) error {
	switch {
	case !domain.ValidChannel(e.Channel):
		return fmt.Errorf("metering: kênh %q không hợp lệ", e.Channel)
	case !domain.ValidCounterparty(e.CounterpartyKind):
		return fmt.Errorf("metering: loại bên nhận %q không hợp lệ", e.CounterpartyKind)
	case e.SourceID == uuid.Nil, e.DataTenantID == uuid.Nil,
		e.CounterpartyRef == uuid.Nil:
		return errors.New("metering: thiếu source_id, tenant dữ liệu hoặc bên nhận")
	case e.OccurredAt.IsZero():
		return errors.New("metering: thiếu thời điểm xuất")
	case e.Records < 0 || e.Bytes < 0:
		return errors.New("metering: số phần tử/byte âm")
	}
	return nil
}

// Pass thực hiện port.Checkpoint (ADR-0024 mục 1): trong MỘT giao dịch metering_app ở tenant CHỦ DỮ LIỆU —
// ghi egress_event (chuỗi băm), so hạn mức ngày của kênh, phát hiện bất thường (× trung vị 28 ngày /
// ngưỡng tuyệt đối) và lần đầu trong ngày phát pf.metering.egress.anomaly_detected. Ở observe không
// bao giờ từ chối; ở enforce vượt hạn mức → Allowed = false. Lỗi trả về là lỗi hạ tầng (Guard áp ngữ
// nghĩa observe/enforce). Idempotent theo (kênh, nguồn, bên nhận).
func (s *Service) Pass(ctx context.Context, e port.Egress) (port.Decision, error) {
	d, err := s.pass(ctx, e, e.EffectiveMode(s.mode())) // A5: xuất nội bộ luôn observe
	if err != nil {
		s.metrics().checkpointError(ctx, string(e.Channel))
	}
	return d, err
}

func (s *Service) pass(ctx context.Context, e port.Egress, mode port.Mode) (port.Decision, error) {
	if err := validateEgress(e); err != nil {
		return port.Decision{}, err
	}
	params, err := s.EgressParams(ctx, e.DataTenantID, e.Channel, e.OccurredAt)
	if err != nil {
		return port.Decision{}, err
	}
	var res passed
	for attempt := 0; ; attempt++ {
		err = s.tx(ctx, e.DataTenantID, func(tx pgx.Tx) error {
			var e2 error
			res, e2 = s.passTx(ctx, tx, e, params, mode)
			return e2
		})
		var pg *pgconn.PgError
		if attempt == 0 && errors.As(err, &pg) && pg.Code == "23505" {
			continue // hai lần cùng (kênh, nguồn, bên nhận) song song: lần sau đọc lại dòng của lần trước
		}
		break
	}
	if err != nil {
		return port.Decision{}, err
	}
	if !res.replay {
		s.metrics().egress(ctx, string(e.Channel), res.decision.Allowed, res.decision.Reason, e.Records, e.Bytes, res.firstAnomaly)
	}
	return res.decision, nil
}

// passTx là thân điểm kiểm soát trong giao dịch tx đang ở ngữ cảnh tenant chủ dữ liệu (Pass mở giao
// dịch riêng; Ledger.Record gọi trong giao dịch của sổ sử dụng qua tenant.Within).
func (s *Service) passTx(ctx context.Context, tx pgx.Tx, e port.Egress, params domain.EgressParams, mode port.Mode) (passed, error) {
	idem := domain.EgressIdemKey(e)
	if prev, err := store.EgressByIdem(ctx, tx, idem); err != nil {
		return passed{}, err
	} else if prev != nil {
		return passed{replay: true, decision: port.Decision{Allowed: prev.Decision == "allowed", Reason: prev.Reason, Anomaly: prev.Anomaly,
			DailyRecords: prev.DailyRecords, DailyQuota: prev.DailyQuota, Mode: port.Mode(prev.Mode)}}, nil
	}
	day := domain.Day(e.OccurredAt)
	key := store.EgressKey{Channel: string(e.Channel), CounterpartyKind: string(e.CounterpartyKind), CounterpartyRef: e.CounterpartyRef, Day: day}
	before, err := store.LockEgressCounter(ctx, tx, e.DataTenantID, key)
	if err != nil {
		return passed{}, err
	}
	attempted := before + e.Records
	over := params.DailyQuota > 0 && attempted > params.DailyQuota
	d := port.Decision{Allowed: !over || mode != port.ModeEnforce, DailyQuota: params.DailyQuota, Mode: mode}
	if over {
		d.Reason = "quota_exceeded"
	}
	history, err := store.EgressHistory(ctx, tx, key, port.AnomalyBaselineDays)
	if err != nil {
		return passed{}, err
	}
	threshold, basis := domain.Threshold(history, params)
	d.Anomaly = domain.IsAnomaly(attempted, threshold)
	d.DailyRecords = before
	if d.Allowed {
		d.DailyRecords = attempted
	}
	actor := e.ActorRef
	if actor == uuid.Nil {
		actor = e.CounterpartyRef
	}
	row := store.EgressRow{
		ID: ids.New(), StreamKey: domain.EgressStream(e.Channel, day), Channel: string(e.Channel), SourceID: e.SourceID,
		CounterpartyKind: string(e.CounterpartyKind), CounterpartyRef: e.CounterpartyRef, Product: e.Product, ContractVersion: e.ContractVersion,
		Records: e.Records, Bytes: e.Bytes, BusinessDate: day, Decision: "allowed", Reason: d.Reason, Mode: string(mode),
		DailyRecords: d.DailyRecords, DailyQuota: params.DailyQuota, Anomaly: d.Anomaly, TraceID: traceRef(ctx, e.TraceID),
		EventTime: e.OccurredAt.UTC(), ActorRef: actor, IdemKey: idem, RuleVersion: domain.RuleEgressV1,
	}
	if !d.Allowed {
		row.Decision = "denied"
	}
	if err := store.InsertEgress(ctx, tx, e.DataTenantID, &row); err != nil {
		return passed{}, ledgerErr(err)
	}
	if d.Allowed {
		if err := store.BumpEgressCounter(ctx, tx, key, e.Records, e.Bytes); err != nil {
			return passed{}, err
		}
	}
	res := passed{decision: d}
	if d.Anomaly {
		now := s.Clock.Now()
		first, err := store.InsertAnomalyOnce(ctx, tx, e.DataTenantID, store.AnomalyRow{
			ID: ids.New(), BusinessDate: day, Channel: string(e.Channel), CounterpartyKind: string(e.CounterpartyKind),
			CounterpartyRef: e.CounterpartyRef, Product: e.Product, EgressEventID: row.ID, DailyRecords: attempted, Threshold: threshold,
			Basis: basis, Mode: string(mode), DetectedAt: now,
		})
		if err != nil {
			return passed{}, err
		}
		if first {
			res.firstAnomaly = true
			if err := outbox.Enqueue(ctx, tx, outbox.Event{
				ID: ids.New(), TenantID: e.DataTenantID, Type: port.EventEgressAnomaly, SchemaVersion: 1, OccurredAt: now,
				TraceID: e.TraceID, Producer: port.Producer,
				Payload: port.EgressAnomalyV1{
					EgressEventID: row.ID.String(), Channel: string(e.Channel), Product: e.Product, CounterpartyKind: string(e.CounterpartyKind),
					CounterpartyRef: e.CounterpartyRef.String(), BusinessDate: day.Format("2006-01-02"), DailyRecords: attempted,
					Threshold: threshold, Basis: basis, Mode: string(mode), DetectedAt: now.UTC().Format("2006-01-02T15:04:05.000000Z07:00"),
				},
			}); err != nil {
				return passed{}, err
			}
		}
	}
	return res, nil
}

func traceRef(ctx context.Context, t string) *string {
	if t == "" {
		t = telemetry.TraceID(ctx)
	}
	if t == "" {
		return nil
	}
	return &t
}

// ledgerErr quy lỗi ghi sổ; trùng idem_key giữ nguyên PgError 23505 để lớp trên đọc lại.
func ledgerErr(err error) error {
	var pg *pgconn.PgError
	if errors.As(err, &pg) && pg.Code == "23505" {
		return err
	}
	return ledger.Classify(err)
}
