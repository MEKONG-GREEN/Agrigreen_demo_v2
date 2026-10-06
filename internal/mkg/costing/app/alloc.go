package app

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/costing/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/costing/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/costing/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/mass"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/money"
)

// Loại kết quả phân bổ.
const (
	AllocHubProcessing = "hub_processing"
	AllocTripCost      = "trip_cost"
	AllocDelivery      = "delivery_revenue"
	AllocLoss          = "loss_attribution"
)

// CargoLot là một lô với khối lượng (đầu vào phân bổ, lưu jsonb — số là chuỗi).
type CargoLot struct {
	LotRef uuid.UUID `json:"lot_ref"`
	MassKg mass.Kg   `json:"mass_kg"`
}

// HubInputs là đầu vào chi phí sơ chế hub (phiếu HUB_IN).
type HubInputs struct {
	TicketID  uuid.UUID `json:"ticket_id"`
	LotRef    uuid.UUID `json:"lot_ref"`
	MassKg    mass.Kg   `json:"mass_kg"`
	EventTime time.Time `json:"event_time"`
}

// TripInputs là đầu vào phân bổ cước chuyến.
type TripInputs struct {
	TripID     uuid.UUID    `json:"trip_id"`
	Mode       string       `json:"mode"`
	ActualCost money.Amount `json:"actual_cost"`
	CostSource string       `json:"cost_source"`
	Cargo      []CargoLot   `json:"cargo"`
	EventTime  time.Time    `json:"event_time"`
}

// DeliveryInputs là đầu vào phân bổ doanh thu giao nhận.
type DeliveryInputs struct {
	DeliveryID  uuid.UUID    `json:"delivery_id"`
	CustomerRef uuid.UUID    `json:"customer_ref"`
	Amount      money.Amount `json:"amount"`
	Lots        []CargoLot   `json:"lots"`
	EventTime   time.Time    `json:"event_time"`
}

// LossSource là một nguồn lô trong đầu vào quy kết.
type LossSource struct {
	Kind  string    `json:"kind"`
	Ref   uuid.UUID `json:"ref"`
	Ratio string    `json:"ratio"`
}

// LossInputs là đầu vào quy kết hao hụt (đối soát measurement + nguồn lô hub).
type LossInputs struct {
	ReconciliationID uuid.UUID    `json:"reconciliation_id"`
	LotRef           uuid.UUID    `json:"lot_ref"`
	HubInKg          *mass.Kg     `json:"hub_in_kg"`
	AbnormalLossKg   mass.Kg      `json:"abnormal_loss_kg"`
	ExpectedLossKg   mass.Kg      `json:"expected_loss_kg"`
	Sources          []LossSource `json:"sources"`
	OriginFound      bool         `json:"origin_found"`
	EventTime        time.Time    `json:"event_time"`
}

// computed là kết quả tính một phân bổ (chưa ghi).
type computed struct {
	Basis    string
	Total    *money.Amount
	TotalKg  *mass.Kg
	Lines    any // []domain.AllocLine | []domain.LossLine
	Alloc    []domain.AllocLine
	Pins     domain.Pins
	Rule     string
	Category port.CostCategory
	DimKind  string
	DimRef   *uuid.UUID
	Source   string // source_kind của bút toán
}

// compute chạy quy tắc của loại kết quả với tham số tại thời điểm sự việc (asOf nil = hiện hành).
func compute(ctx context.Context, tx pgx.Tx, kind string, raw json.RawMessage, eventTime time.Time, asOf *time.Time) (computed, error) {
	switch kind {
	case AllocHubProcessing:
		var in HubInputs
		if err := json.Unmarshal(raw, &in); err != nil {
			return computed{}, err
		}
		rate, err := paramAt(ctx, tx, port.ParamHubProcessingRate, eventTime, asOf)
		if err != nil {
			return computed{}, err
		}
		amt, err := domain.HubProcessingCost(in.MassKg, rate.Decimal())
		if err != nil {
			return computed{}, err
		}
		kg := in.MassKg
		lines := []domain.AllocLine{{LotRef: in.LotRef, MassKg: kg.String(), Ratio: "1.000000", Amount: amt.String()}}
		return computed{Basis: "self", Total: &amt, TotalKg: &kg, Lines: lines, Alloc: lines, Pins: domain.Pins{}.Add(rate),
			Rule: domain.RuleHubProcessing, Category: port.CatHubProcessing, DimKind: "none", Source: "hub_processing"}, nil
	case AllocTripCost:
		var in TripInputs
		if err := json.Unmarshal(raw, &in); err != nil {
			return computed{}, err
		}
		key, err := paramAt(ctx, tx, port.ParamAllocationKey, eventTime, asOf)
		if err != nil {
			return computed{}, err
		}
		ws := make([]domain.Weight, 0, len(in.Cargo))
		var total mass.Kg
		for _, c := range in.Cargo {
			ws = append(ws, domain.Weight{LotRef: c.LotRef, MassKg: c.MassKg})
			total = total.Add(c.MassKg)
		}
		lines, used, err := domain.SplitAmount(in.ActualCost, ws, key.Value)
		if err != nil {
			return computed{}, err
		}
		cat := port.CatTransportRoad
		if in.Mode == "waterway" {
			cat = port.CatTransportWaterway
		}
		amt := in.ActualCost
		return computed{Basis: used, Total: &amt, TotalKg: &total, Lines: lines, Alloc: lines, Pins: domain.Pins{}.Add(key),
			Rule: domain.RuleTripCost, Category: cat, DimKind: "none", Source: "trip"}, nil
	case AllocDelivery:
		var in DeliveryInputs
		if err := json.Unmarshal(raw, &in); err != nil {
			return computed{}, err
		}
		ws := make([]domain.Weight, 0, len(in.Lots))
		var total mass.Kg
		for _, c := range in.Lots {
			ws = append(ws, domain.Weight{LotRef: c.LotRef, MassKg: c.MassKg})
			total = total.Add(c.MassKg)
		}
		lines, used, err := domain.SplitAmount(in.Amount, ws, domain.AllocByMass)
		if err != nil {
			return computed{}, err
		}
		amt := in.Amount
		cust := in.CustomerRef
		return computed{Basis: used, Total: &amt, TotalKg: &total, Lines: lines, Alloc: lines, Pins: domain.Pins{},
			Rule: domain.RuleDelivery, Category: port.CatSalesRevenue, DimKind: "customer", DimRef: &cust, Source: "delivery"}, nil
	case AllocLoss:
		var in LossInputs
		if err := json.Unmarshal(raw, &in); err != nil {
			return computed{}, err
		}
		p19, err := paramAt(ctx, tx, port.ParamLossRatioPct, eventTime, asOf)
		if err != nil {
			return computed{}, err
		}
		p56, err := paramAt(ctx, tx, port.ParamHubProcessingLossPct, eventTime, asOf)
		if err != nil {
			return computed{}, err
		}
		var norm mass.Kg
		if in.HubInKg != nil {
			if norm, err = domain.ExpectedLossKg(*in.HubInKg, p19.Decimal(), p56.Decimal()); err != nil {
				return computed{}, err
			}
		}
		srcs := make([]domain.Source, 0, len(in.Sources))
		for _, s := range in.Sources {
			r, err := decimal.NewFromString(s.Ratio)
			if err != nil {
				return computed{}, err
			}
			srcs = append(srcs, domain.Source{Kind: s.Kind, Ref: s.Ref, Ratio: r})
		}
		lines, err := domain.AttributeLoss(in.AbnormalLossKg, in.ExpectedLossKg, norm, srcs)
		if err != nil {
			return computed{}, err
		}
		basis := "origins"
		if !in.OriginFound {
			basis = "self"
		}
		kg := in.AbnormalLossKg
		return computed{Basis: basis, TotalKg: &kg, Lines: lines, Pins: domain.Pins{}.Add(p19).Add(p56), Rule: domain.RuleLoss}, nil
	}
	return computed{}, fmt.Errorf("costing: loại phân bổ lạ %q", kind)
}

// resultSHA băm kết quả (không gồm id): cùng đầu vào + cùng tham số → cùng băm.
func resultSHA(kind string, source uuid.UUID, c computed) (string, error) {
	return domain.InputsSHA256(struct {
		Kind    string        `json:"kind"`
		Source  uuid.UUID     `json:"source"`
		Basis   string        `json:"basis"`
		Total   *money.Amount `json:"total"`
		TotalKg *mass.Kg      `json:"total_kg"`
		Lines   any           `json:"lines"`
		Pins    domain.Pins   `json:"pins"`
		Rule    string        `json:"rule"`
	}{kind, source, c.Basis, c.Total, c.TotalKg, c.Lines, c.Pins, c.Rule})
}

// allocReq là một yêu cầu ghi kết quả phân bổ.
type allocReq struct {
	Kind      string
	SourceRef uuid.UUID
	EventTime time.Time
	Inputs    any
	Old       *store.Allocation // kết quả hiệu lực bị thay (nil = lần đầu)
	AsOf      *time.Time        // mốc tham số khi tính lại (R-13); nil = hiện hành
	Actor     uuid.UUID
	IdemKey   string
	Reason    string // lý do khi thay kết quả cũ
}

// persistAllocation tính + ghi kết quả phân bổ (sổ allocation_result, supersedes kết quả cũ) và
// bút toán theo lô (thay bút toán cũ của cùng lô bằng dòng mới mang delta). Kết quả trùng băm với
// kết quả hiệu lực → không ghi gì (trả kết quả cũ, changed = false).
func (s *Service) persistAllocation(ctx context.Context, tx pgx.Tx, tid uuid.UUID, r allocReq) (store.Allocation, bool, error) {
	raw, err := json.Marshal(r.Inputs)
	if err != nil {
		return store.Allocation{}, false, err
	}
	c, err := compute(ctx, tx, r.Kind, raw, r.EventTime, r.AsOf)
	if err != nil {
		return store.Allocation{}, false, err
	}
	rsha, err := resultSHA(r.Kind, r.SourceRef, c)
	if err != nil {
		return store.Allocation{}, false, err
	}
	if r.Old != nil && r.Old.ResultSHA256 == rsha {
		return *r.Old, false, nil
	}
	period, late, err := s.targetPeriod(ctx, tx, tid, r.EventTime)
	if err != nil {
		return store.Allocation{}, false, err
	}
	lines, err := json.Marshal(c.Lines)
	if err != nil {
		return store.Allocation{}, false, err
	}
	isha, err := domain.InputsSHA256(r.Inputs)
	if err != nil {
		return store.Allocation{}, false, err
	}
	a := store.Allocation{ID: ids.New(), PeriodRef: period.ID, Kind: r.Kind, SourceRef: r.SourceRef, SourceEventTime: r.EventTime.UTC(),
		Basis: c.Basis, TotalAmount: c.Total, TotalKg: c.TotalKg, Lines: lines, Inputs: raw, InputsSHA256: isha, ResultSHA256: rsha,
		Pins: c.Pins, ParamsAsOf: r.AsOf, EventTime: s.now(), ActorRef: r.Actor, IdemKey: r.IdemKey, RuleVersion: c.Rule}
	var oldEntries []store.Entry
	if r.Old != nil {
		a.SupersedesID = &r.Old.ID
		reason := r.Reason
		if reason == "" {
			reason = "tính lại kết quả phân bổ " + r.Old.ID.String()
		}
		a.CorrectionReason = &reason
		if oldEntries, err = store.LiveEntriesOfAllocation(ctx, tx, r.Old.ID); err != nil {
			return store.Allocation{}, false, err
		}
	}
	if err := store.InsertAllocation(ctx, tx, tid, &a); err != nil {
		return store.Allocation{}, false, err
	}
	if r.Kind == AllocLoss {
		return a, true, nil // quy kết hao hụt: kết quả khối lượng theo nguồn, không phải bút toán tiền
	}
	old := map[uuid.UUID]store.Entry{}
	for _, e := range oldEntries {
		old[e.LotRef] = e
	}
	reason := "tính lại theo kết quả phân bổ " + a.ID.String()
	// reverse thay bút toán cũ bằng dòng 0 cùng nhóm / chiều (delta = −cũ).
	reverse := func(o store.Entry) error {
		e := store.Entry{ID: ids.New(), LotRef: o.LotRef, PeriodCode: period.Code, LateFrom: late, Category: o.Category,
			Amount: money.MustAmount("0"), DeltaAmount: money.MustAmount("0").Sub(o.Amount), DeltaMassKg: deltaKg(nil, o.MassKg),
			DimensionKind: o.DimensionKind, DimensionRef: o.DimensionRef, SourceKind: o.SourceKind, SourceRef: &r.SourceRef,
			AllocationRef: &a.ID, EntryKind: "normal", EventTime: r.EventTime.UTC(), SupersedesID: &o.ID, CorrectionReason: &reason,
			ActorRef: r.Actor, IdemKey: "alloc:" + a.ID.String() + ":" + o.LotRef.String() + ":rev", RuleVersion: c.Rule}
		return store.InsertEntry(ctx, tx, tid, &e)
	}
	write := func(lot uuid.UUID, amt money.Amount, kg *mass.Kg) error {
		e := store.Entry{ID: ids.New(), LotRef: lot, PeriodCode: period.Code, LateFrom: late, Category: string(c.Category), Amount: amt,
			DeltaAmount: amt, MassKg: kg, DeltaMassKg: kg, DimensionKind: c.DimKind, DimensionRef: c.DimRef, SourceKind: c.Source,
			SourceRef: &r.SourceRef, AllocationRef: &a.ID, EntryKind: "normal", EventTime: r.EventTime.UTC(), ActorRef: r.Actor,
			IdemKey: "alloc:" + a.ID.String() + ":" + lot.String(), RuleVersion: c.Rule}
		if o, ok := old[lot]; ok {
			delete(old, lot)
			if o.Category != e.Category || o.DimensionKind != e.DimensionKind || !sameRef(o.DimensionRef, e.DimensionRef) {
				if err := reverse(o); err != nil { // đổi nhóm/chiều: đảo dòng cũ, ghi dòng mới
					return err
				}
			} else {
				e.SupersedesID, e.CorrectionReason = &o.ID, &reason
				e.DeltaAmount = amt.Sub(o.Amount)
				e.DeltaMassKg = deltaKg(kg, o.MassKg)
			}
		}
		return store.InsertEntry(ctx, tx, tid, &e)
	}
	for _, l := range c.Alloc {
		amt, err := money.ParseAmount(l.Amount)
		if err != nil {
			return a, false, err
		}
		kg, err := mass.ParseKg(l.MassKg)
		if err != nil {
			return a, false, err
		}
		if err := write(l.LotRef, amt, &kg); err != nil {
			return a, false, err
		}
	}
	for _, o := range oldEntries { // lô không còn trong kết quả mới → dòng 0 thay dòng cũ (delta = −cũ)
		if _, still := old[o.LotRef]; still {
			if err := reverse(o); err != nil {
				return a, false, err
			}
		}
	}
	return a, true, nil
}

func sameRef(a, b *uuid.UUID) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

func deltaKg(n, o *mass.Kg) *mass.Kg {
	switch {
	case n != nil && o != nil:
		d := n.Sub(*o)
		return &d
	case n != nil:
		d := *n
		return &d
	case o != nil:
		var z mass.Kg
		d := z.Sub(*o)
		return &d
	}
	return nil
}
