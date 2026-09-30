package app

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/costing/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/costing/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/costing/store"
	logport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/logistics/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/outbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/telemetry"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// OpenPeriod mở kỳ tháng tường minh (cost_accountant / period_closer). Đã có → trả kỳ hiện có.
func (s *Service) OpenPeriod(ctx context.Context, code string) (p store.Period, err error) {
	pr, err := authz.Require(ctx, port.RoleCostAccountant, port.RolePeriodCloser)
	if err != nil {
		return p, err
	}
	if _, _, err := domain.PeriodBounds(code); err != nil {
		return p, mapErr(err)
	}
	err = tenant.WithTenantTx(ctx, s.DB, pr.TenantID, func(tx pgx.Tx) error {
		p, err = s.ensurePeriod(ctx, tx, pr.TenantID, code, pr.ActorRef)
		return err
	})
	return p, mapErr(err)
}

// Period đọc kỳ kèm kết quả đóng kỳ (nếu đã đóng).
func (s *Service) Period(ctx context.Context, id uuid.UUID) (p store.Period, sum *store.Summary, err error) {
	err = s.read(ctx, func(tx pgx.Tx, _ authz.Principal) error {
		cur, err := store.PeriodByID(ctx, tx, id, false)
		if err != nil {
			return err
		}
		if cur == nil {
			return ErrPeriodNotFound
		}
		p = *cur
		if p.Closed() && p.CloseRunID != nil {
			sum, err = store.SummaryByRun(ctx, tx, *p.CloseRunID)
		}
		return err
	})
	return p, sum, err
}

// Periods liệt kê kỳ (mới trước).
func (s *Service) Periods(ctx context.Context, before string, limit int) (out []store.Period, err error) {
	if limit <= 0 || limit > 120 {
		limit = 24
	}
	err = s.read(ctx, func(tx pgx.Tx, _ authz.Principal) error {
		out, err = store.ListPeriods(ctx, tx, before, limit)
		return err
	})
	return out, err
}

// Job đọc job đóng kỳ.
func (s *Service) Job(ctx context.Context, id uuid.UUID) (j store.CloseJob, err error) {
	err = s.read(ctx, func(tx pgx.Tx, _ authz.Principal) error {
		cur, err := store.JobByID(ctx, tx, id, false)
		if err != nil {
			return err
		}
		if cur == nil {
			return ErrJobNotFound
		}
		j = *cur
		return nil
	})
	return j, err
}

// RequestClose yêu cầu đóng kỳ (period_closer) theo mẫu mục 12: đọc lại đầu vào qua port (chuyến,
// giao nhận hoàn tất trong kỳ mà consumer chưa ghi), rồi trong MỘT giao dịch: khóa kỳ, chụp đầu
// vào bất biến + SHA-256, ghi job + sổ kỳ (closing) + outbox mkg.costing.period_close.requested.
// Trả job (202). Kỳ không mở → 409 (không mở lại); người đóng là người lập điều chỉnh → 403 (R-56).
func (s *Service) RequestClose(ctx context.Context, periodID uuid.UUID, idem string) (j store.CloseJob, replayed bool, err error) {
	p, err := authz.Require(ctx, port.RolePeriodCloser)
	if err != nil {
		return j, false, err
	}
	if err := requireIdem(idem); err != nil {
		return j, false, err
	}
	var per store.Period
	if err := tenant.WithTenantTx(ctx, s.DB, p.TenantID, func(tx pgx.Tx) error {
		if prev, err := store.JobByIdem(ctx, tx, idem); err != nil || prev != nil {
			if prev != nil {
				j, replayed = *prev, true
			}
			return err
		}
		cur, err := store.PeriodByID(ctx, tx, periodID, false)
		if err != nil {
			return err
		}
		if cur == nil {
			return ErrPeriodNotFound
		}
		per = *cur
		return nil
	}); err != nil || replayed {
		return j, replayed, mapErr(err)
	}
	if per.Status != port.PeriodOpen {
		return j, false, ErrPeriodNotOpen.WithDetail("kỳ " + per.Code + " đang " + string(per.Status) + " — kỳ đã đóng không mở lại")
	}
	if !per.Ended(s.now()) {
		return j, false, ErrPeriodNotEnded
	}
	if err := s.syncFromPorts(ctx, p.TenantID, per); err != nil {
		return j, false, mapErr(err)
	}
	replayed, err = s.write(ctx, p.TenantID, func(tx pgx.Tx) error {
		cur, err := store.PeriodByID(ctx, tx, periodID, true) // FOR UPDATE: bút toán mới xếp hàng sau (trigger FOR SHARE)
		if err != nil {
			return err
		}
		if cur == nil {
			return ErrPeriodNotFound
		}
		if cur.Status != port.PeriodOpen {
			return ErrPeriodNotOpen
		}
		authors, err := store.AdjustingAuthors(ctx, tx, cur.Code)
		if err != nil {
			return err
		}
		for _, a := range authors {
			if a == p.ActorRef {
				return problem.SelfApproval.WithDetail("người đóng kỳ đã lập bút toán điều chỉnh của kỳ " + cur.Code + " (R-56)")
			}
		}
		asOf, err := store.DBNow(ctx, tx)
		if err != nil {
			return err
		}
		snap, err := buildSnapshot(ctx, tx, *cur, asOf)
		if err != nil {
			return err
		}
		sha, err := snap.SHA256()
		if err != nil {
			return err
		}
		j = store.CloseJob{ID: ids.New(), PeriodID: cur.ID, PeriodCode: cur.Code, Status: "queued", RequestedBy: p.ActorRef, AsOf: asOf,
			Snapshot: snap, SnapshotSHA256: sha, AdjustingAuthors: snap.AdjustingAuthors(), IdemKey: idem}
		if err := store.InsertJob(ctx, tx, p.TenantID, &j); err != nil {
			return err
		}
		run := j.ID
		e := store.PeriodEvent{ID: ids.New(), PeriodID: cur.ID, Code: cur.Code, Action: "close_requested", From: cur.From, To: cur.To,
			CloseRunID: &run, EventTime: s.now(), ActorRef: p.ActorRef, IdemKey: "close-req:" + j.ID.String()}
		if err := store.InsertPeriodEvent(ctx, tx, p.TenantID, &e); err != nil {
			return err
		}
		ev := port.PeriodCloseRequestedV1{JobID: j.ID, PeriodID: cur.ID, RequestedBy: p.ActorRef, RequestedAt: j.RequestedAt.UTC()}
		return outbox.Enqueue(ctx, tx, outbox.Event{ID: ids.New(), TenantID: p.TenantID, Type: port.EventPeriodCloseRequested, SchemaVersion: 1,
			OccurredAt: ev.RequestedAt, TraceID: telemetry.TraceID(ctx), Producer: Producer, Payload: ev, Kind: outbox.KindInternal})
	}, func(tx pgx.Tx) (bool, error) {
		prev, err := store.JobByIdem(ctx, tx, idem)
		if prev != nil {
			j = *prev
		}
		return prev != nil, err
	})
	return j, replayed, err
}

// buildSnapshot chụp đầu vào của kỳ tại asOf: bút toán (delta), quyết toán đã duyệt, doanh thu carbon.
func buildSnapshot(ctx context.Context, tx pgx.Tx, per store.Period, asOf time.Time) (domain.Snapshot, error) {
	snap := domain.Snapshot{PeriodID: per.ID, PeriodCode: per.Code, From: per.From, To: per.To, AsOf: asOf, RuleVersion: domain.RulePeriodClose}
	es, err := store.EntriesForPeriod(ctx, tx, per.Code, asOf)
	if err != nil {
		return snap, err
	}
	for _, e := range es {
		se := domain.SnapEntry{ID: e.ID, LotRef: e.LotRef, Category: e.Category, DimensionKind: e.DimensionKind, DimensionRef: e.DimensionRef,
			DeltaAmount: e.DeltaAmount.String(), EntryKind: e.EntryKind, ActorRef: e.ActorRef, RuleVersion: e.RuleVersion}
		if e.DeltaMassKg != nil {
			k := e.DeltaMassKg.String()
			se.DeltaMassKg = &k
		}
		snap.Entries = append(snap.Entries, se)
	}
	if snap.Settlements, err = store.ApprovedSettlementsForPeriod(ctx, tx, per.ID, asOf); err != nil {
		return snap, err
	}
	cs, err := store.CarbonForPeriod(ctx, tx, per.ID, asOf)
	if err != nil {
		return snap, err
	}
	for _, c := range cs {
		shares := c.Split.Farmers.Add(c.Split.Authority).Add(c.Split.Verifier)
		snap.Carbon = append(snap.Carbon, domain.SnapCarbon{ID: c.ID, Retained: c.Split.Retained.String(), Shares: shares.String(),
			RuleVersion: domain.RuleCarbonSplit})
	}
	snap.Normalize()
	return snap, nil
}

// syncFromPorts đọc lại chuyến / giao nhận hoàn tất trong kỳ qua port (asOf = bây giờ) và ghi kết quả
// còn thiếu (consumer trễ / mất sự kiện) — cùng đường ghi idempotent với consumer.
func (s *Service) syncFromPorts(ctx context.Context, tid uuid.UUID, per store.Period) error {
	asOf := s.now()
	if s.Deps.Trips != nil {
		cursor := ""
		for range 1000 {
			page, err := s.Deps.Trips.Completed(ctx, tid, per.From, per.To, asOf, cursor, 200)
			if err != nil {
				return fmt.Errorf("costing: đọc chuyến hoàn tất: %w", err)
			}
			for _, t := range page.Items {
				if err := tenant.WithTenantTx(ctx, s.DB, tid, func(tx pgx.Tx) error { return s.ingestTripView(ctx, tx, tid, t) }); err != nil {
					return err
				}
			}
			if page.Next == "" {
				break
			}
			cursor = page.Next
		}
	}
	if s.Deps.Deliveries != nil {
		cursor := ""
		for range 1000 {
			page, err := s.Deps.Deliveries.Accepted(ctx, tid, per.From, per.To, asOf, cursor, 200)
			if err != nil {
				return fmt.Errorf("costing: đọc giao nhận chấp nhận: %w", err)
			}
			for _, d := range page.Items {
				if d.Amount == nil || d.AcceptedAt == nil || len(d.Lots) == 0 {
					continue
				}
				in := DeliveryInputs{DeliveryID: d.DeliveryID, CustomerRef: d.CustomerRef, Amount: *d.Amount, EventTime: d.AcceptedAt.UTC()}
				for _, l := range d.Lots {
					in.Lots = append(in.Lots, CargoLot{LotRef: l.LotRef, MassKg: l.MassKg})
				}
				if err := tenant.WithTenantTx(ctx, s.DB, tid, func(tx pgx.Tx) error {
					return s.ingestIfMissing(ctx, tx, tid, AllocDelivery, d.DeliveryID, *d.AcceptedAt, in, "delivery:")
				}); err != nil {
					return err
				}
			}
			if page.Next == "" {
				break
			}
			cursor = page.Next
		}
	}
	return nil
}

func (s *Service) ingestTripView(ctx context.Context, tx pgx.Tx, tid uuid.UUID, t logport.TripView) error {
	if t.ActualCost == nil || t.CompletedAt == nil || len(t.Cargo) == 0 {
		return nil
	}
	in := TripInputs{TripID: t.TripID, Mode: string(t.Mode), ActualCost: *t.ActualCost, CostSource: string(t.CostSource),
		EventTime: t.CompletedAt.UTC()}
	for _, c := range t.Cargo {
		in.Cargo = append(in.Cargo, CargoLot{LotRef: c.LotRef, MassKg: c.MassKg})
	}
	return s.ingestIfMissing(ctx, tx, tid, AllocTripCost, t.TripID, *t.CompletedAt, in, "trip:")
}

// ingestIfMissing chỉ ghi khi nguồn CHƯA có kết quả nào (consumer là đường chính; đọc lại chỉ bù chỗ thiếu).
func (s *Service) ingestIfMissing(ctx context.Context, tx pgx.Tx, tid uuid.UUID, kind string, source uuid.UUID, at time.Time, in any, prefix string) error {
	live, err := store.LiveAllocation(ctx, tx, kind, source)
	if err != nil || live != nil {
		return err
	}
	return s.ingestSource(ctx, tx, tid, kind, source, nil, at, in, prefix)
}

// HandleCloseRequested là handler của worker-internal cho mkg.costing.period_close.requested: nạp
// ảnh chụp từ job (không từ sự kiện), kiểm băm, tổng hợp tất định, ghi kết quả + sổ kỳ (closed) +
// job completed + outbox mkg.costing.period.closed trong MỘT giao dịch (giao dịch inbox — R-61).
// Job đã xong → bỏ qua (giao lặp → một kết quả).
func (s *Service) HandleCloseRequested(ctx context.Context, tx pgx.Tx, tid uuid.UUID, ev port.PeriodCloseRequestedV1) error {
	j, err := store.JobByID(ctx, tx, ev.JobID, true)
	if err != nil || j == nil || j.Status != "queued" {
		return err
	}
	sha, err := j.Snapshot.SHA256()
	if err != nil {
		return err
	}
	if sha != j.SnapshotSHA256 {
		return store.FailJob(ctx, tx, j.ID, "snapshot_hash_mismatch")
	}
	per, err := store.PeriodByID(ctx, tx, j.PeriodID, true)
	if err != nil {
		return err
	}
	if per == nil || per.Status != port.PeriodClosing || per.CloseRunID == nil || *per.CloseRunID != j.ID {
		return store.FailJob(ctx, tx, j.ID, "period_state")
	}
	lines := domain.Summarize(j.Snapshot)
	rsha, err := domain.ResultSetSHA256(per.Code, lines)
	if err != nil {
		return err
	}
	now := s.now()
	sum := store.Summary{ID: ids.New(), PeriodID: per.ID, PeriodCode: per.Code, CloseRunID: j.ID, From: per.From, To: per.To, Lines: lines,
		InputSnapshotSHA256: j.SnapshotSHA256, ResultSetSHA256: rsha, ClosedBy: j.RequestedBy, EventTime: now}
	if err := store.InsertSummary(ctx, tx, tid, sum); err != nil {
		return err
	}
	run := j.ID
	pe := store.PeriodEvent{ID: ids.New(), PeriodID: per.ID, Code: per.Code, Action: "closed", From: per.From, To: per.To, CloseRunID: &run,
		EventTime: now, ActorRef: j.RequestedBy, IdemKey: "closed:" + j.ID.String()}
	if err := store.InsertPeriodEvent(ctx, tx, tid, &pe); err != nil {
		return err
	}
	if err := store.CompleteJob(ctx, tx, j.ID, now); err != nil {
		return err
	}
	out := port.PeriodClosedV1{PeriodID: per.ID, PeriodCode: per.Code, From: per.From.UTC(), To: per.To.UTC(), CloseRunID: j.ID,
		InputSnapshotSHA256: j.SnapshotSHA256, ResultSetSHA256: rsha, LineCount: len(lines), ClosedBy: j.RequestedBy,
		RuleVersion: domain.RulePeriodClose, EventTime: now}
	return outbox.Enqueue(ctx, tx, outbox.Event{ID: ids.New(), TenantID: tid, Type: port.EventPeriodClosed, SchemaVersion: 1, OccurredAt: now,
		TraceID: telemetry.TraceID(ctx), Producer: Producer, Payload: out, Kind: outbox.KindInternal})
}

// ClosedPeriod cài đặt port.ClosedPeriods (tenant tường minh, không cần principal — mkg/acctbridge
// cùng tiến trình). Tất định: cùng CloseRunID → cùng Lines (đã sắp) và cùng ResultSetSHA256.
func (s *Service) ClosedPeriod(ctx context.Context, tid, periodID uuid.UUID) (port.PeriodSummary, error) {
	var out port.PeriodSummary
	err := tenant.WithTenantTx(ctx, s.DB, tid, func(tx pgx.Tx) error {
		per, err := store.PeriodByID(ctx, tx, periodID, false)
		if err != nil {
			return err
		}
		if per == nil {
			return port.ErrPeriodNotFound
		}
		if !per.Closed() || per.CloseRunID == nil {
			return port.ErrPeriodNotClosed
		}
		sum, err := store.SummaryByRun(ctx, tx, *per.CloseRunID)
		if err != nil {
			return err
		}
		if sum == nil {
			return port.ErrPeriodNotClosed
		}
		lines, err := domain.ToPort(sum.Lines)
		if err != nil {
			return err
		}
		out = port.PeriodSummary{PeriodID: per.ID, PeriodCode: per.Code, From: per.From.UTC(), To: per.To.UTC(), Status: per.Status,
			CloseRunID: sum.CloseRunID, ClosedAt: per.ClosedAt.UTC(), ClosedBy: sum.ClosedBy, RuleVersion: sum.RuleVersion,
			InputSnapshotSHA256: sum.InputSnapshotSHA256, ResultSetSHA256: sum.ResultSetSHA256, Lines: lines}
		return nil
	})
	return out, err
}

var _ port.ClosedPeriods = (*Service)(nil)

// SnapshotJSON trả dạng tất định của ảnh chụp của job (API đọc / kiểm toán).
func SnapshotJSON(j store.CloseJob) (json.RawMessage, error) { return j.Snapshot.Canonical() }
