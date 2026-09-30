package app

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/measurement/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/measurement/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/measurement/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/mass"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/outbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/telemetry"
)

func (s *Service) rolls() port.RollTotals {
	if s.Rolls == nil {
		return NoRolls{}
	}
	return s.Rolls
}

func pmKg(lm *store.LotMass, pt port.WeighPoint) (*mass.Kg, *uuid.UUID, time.Time) {
	pm, ok := lm.Points[pt]
	if !ok {
		return nil, nil, time.Time{}
	}
	k, id := pm.Kg, pm.TicketID
	return &k, &id, pm.At
}

// fingerprint là đầu vào tất định của một lần đối soát.
type fingerprint struct {
	Route       port.Route        `json:"route"`
	Basis       port.PricingBasis `json:"basis"`
	Tickets     []*uuid.UUID      `json:"tickets"`
	Tolerance   string            `json:"tolerance"`
	LossNorm    string            `json:"loss_norm"`
	Rolls       *int64            `json:"rolls"`
	KgPerRoll   string            `json:"kg_per_roll"`
	RuleVersion string            `json:"rule_version"`
}

// reconcileTx đối soát một lô trong giao dịch có sẵn. auto = gọi sau khi ghi
// CUSTOMER_IN: lô chưa đủ phiếu thì không làm gì (nil, không lỗi). Đầu vào
// không đổi so với lần trước → trả lần trước (không ghi, replayed).
func (s *Service) reconcileTx(ctx context.Context, tx pgx.Tx, p authz.Principal, lot uuid.UUID, idem string, auto bool) (*store.Reconciliation, bool, error) {
	if prev, err := store.ReconciliationByIdem(ctx, tx, idem); err != nil || prev != nil {
		return prev, prev != nil, err
	}
	lm, err := store.GetLotMass(ctx, tx, lot, true)
	if err != nil {
		return nil, false, err
	}
	if lm == nil || len(lm.Points) == 0 {
		if auto {
			return nil, false, nil
		}
		return nil, false, ErrLotNotFound
	}
	terms, err := store.GetTerms(ctx, tx, lot)
	if err != nil {
		return nil, false, err
	}
	basis := port.PricingPerTonne // mặc định tạm: kiểm chéo chính thức (RB-6) tới khi G4 orders có hợp đồng
	if terms != nil {
		basis = terms.Basis
	}
	hin, hinT, hinAt := pmKg(lm, port.PointHubIn)
	hout, houtT, houtAt := pmKg(lm, port.PointHubOut)
	cin, cinT, cinAt := pmKg(lm, port.PointCustomerIn)

	// Phiên bản tham số theo THỜI ĐIỂM CÂN (mục 16): dung sai theo CUSTOMER_IN, định mức theo HUB_OUT.
	tolAt, normAt := cinAt, houtAt
	if tolAt.IsZero() {
		tolAt = lm.AsOf
	}
	if normAt.IsZero() {
		normAt = tolAt
	}
	tol, err := store.EffectiveTolerance(ctx, tx, tolAt)
	if err != nil {
		return nil, false, err
	}
	norm, err := store.EffectiveLossNorm(ctx, tx, normAt)
	if err != nil {
		return nil, false, err
	}
	// Số cuộn của lệnh gom liên kết (G3-2); ước lượng kg/cuộn học trước phiếu cân đầu tiên.
	weighedAt := hinAt
	if lm.Route == port.RouteDirect || weighedAt.IsZero() {
		weighedAt = cinAt
	}
	rolls, err := s.rollTotal(ctx, tx, p.TenantID, lot, weighedAt)
	if err != nil {
		return nil, false, err
	}
	in := domain.ReconcileInput{Route: lm.Route, Basis: basis, HubIn: hin, HubOut: hout, CustomerIn: cin, HubInMoisture: lm.HubInMoisture,
		Rolls: rolls, Tolerance: tol, LossNorm: norm}
	res, err := domain.Reconcile(in)
	if errors.Is(err, domain.ErrIncomplete) && auto {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	fp := fingerprint{Route: lm.Route, Basis: basis, Tickets: []*uuid.UUID{hinT, houtT, cinT}, Tolerance: tol.Label,
		RuleVersion: domain.RuleReconcile}
	if lm.Route == port.RouteHub {
		fp.LossNorm = norm.Label
	}
	if rolls != nil {
		n := rolls.Rolls
		fp.Rolls, fp.KgPerRoll = &n, rolls.KgPerRollEstimate.String()
	}
	sum, err := store.InputsFingerprint(fp)
	if err != nil {
		return nil, false, err
	}
	latest, err := store.LatestReconciliation(ctx, tx, lot)
	if err != nil {
		return nil, false, err
	}
	if latest != nil && latest.InputsSHA256 == sum {
		return latest, true, nil
	}
	now := s.Clock.Now()
	r := &store.Reconciliation{
		ID: ids.New(), LotRef: lot, Route: lm.Route, Basis: basis, HubIn: hin, HubOut: hout, CustomerIn: cin,
		HubInTicket: hinT, HubOutTicket: houtT, CustomerInTicket: cinT, HubInMoisture: lm.HubInMoisture, Result: res,
		ToleranceID: tol.VersionID, ToleranceLabel: tol.Label, InputsSHA256: sum, EventTime: now, ActorRef: p.ActorRef, IdemKey: idem,
	}
	if lm.Route == port.RouteHub {
		lbl := norm.Label
		r.LossNormID, r.LossNormLabel = norm.VersionID, &lbl
	}
	if latest != nil {
		r.SupersedesID = &latest.ID
	}
	// Vượt dung sai thương mại → hồ sơ tranh chấp (một lô tối đa một hồ sơ đang mở).
	if res.CommercialStatus == domain.CheckFlag || res.CommercialStatus == domain.CheckCritical {
		act, err := store.ActiveDispute(ctx, tx, lot)
		if err != nil {
			return nil, false, err
		}
		if act != nil {
			id := act.ID
			r.DisputeID = &id
		} else {
			did := ids.New()
			r.DisputeID = &did
		}
	}
	if err := insert(store.InsertReconciliation(ctx, tx, p.TenantID, r)); err != nil {
		return nil, false, err
	}
	if r.DisputeID != nil {
		if act, _ := store.GetDispute(ctx, tx, *r.DisputeID, false); act == nil {
			sev, rid := res.CommercialStatus, r.ID
			e := store.DisputeEvent{ID: ids.New(), DisputeID: *r.DisputeID, LotRef: lot, Action: domain.ActOpen, StatusAfter: domain.DisputeOpen,
				ReconciliationID: &rid, Severity: &sev, HubOut: hout, CustomerIn: cin, Diff: res.CommercialDiff, DiffPct: res.CommercialDiffPct,
				EvidenceRefs: evidenceOf(ctx, tx, houtT, cinT), EventTime: now, ActorRef: p.ActorRef, IdemKey: idem + "#dispute"}
			if err := insert(store.InsertDisputeEvent(ctx, tx, p.TenantID, &e)); err != nil {
				return nil, false, err
			}
			if err := store.ApplyDisputeEvent(ctx, tx, p.TenantID, e); err != nil {
				return nil, false, err
			}
		}
	}
	if err := store.UpsertYield(ctx, tx, p.TenantID, *r); err != nil {
		return nil, false, err
	}
	ev := port.ReconciliationCompletedV1{
		ReconciliationID: r.ID, LotRef: lot, Route: r.Route, HubInKg: hin, HubOutKg: hout, CustomerInKg: cin,
		ExpectedLossKg: res.ExpectedLoss, AbnormalLossKg: res.AbnormalLoss, CommercialDiffKg: res.CommercialDiff, Outcome: res.Outcome,
		DisputeID: r.DisputeID, RuleVersion: domain.RuleReconcile, EventTime: now.UTC(),
	}
	tl := tol.Label
	ev.ToleranceVersion = &tl
	ev.LossNormVersion = r.LossNormLabel
	err = outbox.Enqueue(ctx, tx, outbox.Event{
		ID: r.ID, TenantID: p.TenantID, Type: port.EventReconciliationCompleted, SchemaVersion: 1, OccurredAt: now.UTC(),
		TraceID: telemetry.TraceID(ctx), Producer: Producer, Payload: ev, Kind: outbox.KindInternal,
	})
	return r, false, err
}

// evidenceOf gom bằng chứng tay của hai phiếu làm bằng chứng mở hồ sơ.
func evidenceOf(ctx context.Context, tx pgx.Tx, ids ...*uuid.UUID) []uuid.UUID {
	var out []uuid.UUID
	for _, id := range ids {
		if id == nil {
			continue
		}
		if t, err := store.TicketByID(ctx, tx, *id); err == nil && t != nil && t.EvidenceRef != nil {
			out = append(out, *t.EvidenceRef)
		}
	}
	return out
}

// Reconcile chạy đối soát một lô (reconciliation_officer). Idempotent: cùng khóa
// → cùng kết quả; đầu vào không đổi → trả lần trước.
func (s *Service) Reconcile(ctx context.Context, lot uuid.UUID, idem string) (r *store.Reconciliation, replayed bool, err error) {
	p, err := authz.Require(ctx, port.RoleReconciliationOfficer)
	if err != nil {
		return nil, false, err
	}
	if err := requireIdem(idem); err != nil {
		return nil, false, err
	}
	_, err = s.write(ctx, p.TenantID, func(tx pgx.Tx) error {
		var e error
		r, replayed, e = s.reconcileTx(ctx, tx, p, lot, idem, false)
		return e
	}, func(pgx.Tx) (bool, error) { return false, nil })
	return r, replayed, err
}

// Reconciliations trả lịch sử đối soát của lô (mới nhất trước).
func (s *Service) Reconciliations(ctx context.Context, lot uuid.UUID) ([]store.Reconciliation, error) {
	var out []store.Reconciliation
	err := s.read(ctx, func(tx pgx.Tx) error {
		var e error
		out, e = store.Reconciliations(ctx, tx, lot)
		if e == nil && len(out) == 0 {
			return ErrLotNotFound.WithDetail("lô chưa được đối soát")
		}
		return e
	})
	return out, err
}

// Yield trả sản lượng quy đổi của lô (chỉ trong vùng mkg — không bao giờ qua van).
func (s *Service) Yield(ctx context.Context, lot uuid.UUID) (store.Yield, error) {
	var out store.Yield
	err := s.read(ctx, func(tx pgx.Tx) error {
		y, err := store.GetYield(ctx, tx, lot)
		if err != nil {
			return err
		}
		if y == nil {
			return ErrLotNotFound.WithDetail("lô chưa được đối soát")
		}
		out = *y
		return nil
	})
	return out, err
}
