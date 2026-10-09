package store

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/costing/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ledger"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/mass"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/money"
)

// ---------------------------------------------------------------------------
// Đầu vào (phiếu cân, đếm cuộn)
// ---------------------------------------------------------------------------

// Input là một bản ghi sổ input_event.
type Input struct {
	ID                 uuid.UUID
	LotRef             uuid.UUID
	Kind               string // weigh | rolls
	SourceRef          uuid.UUID
	CollectionOrderRef uuid.UUID
	HTXRef             *uuid.UUID
	Point              *string
	Route              *string
	MassKg             *mass.Kg
	MoisturePct        *string
	Rolls              *int
	PlotRef            *uuid.UUID
	Stage              *string
	EventTime          time.Time
	SupersedesID       *uuid.UUID
	CorrectionReason   *string
	ActorRef           uuid.UUID
	IdemKey            string
	RecordedAt         time.Time
}

const inputCols = `id, lot_ref, kind, source_ref, collection_order_ref, htx_ref, point, route, mass_kg::text, moisture_pct::text, rolls,
  plot_ref, stage, event_time, supersedes_id, correction_reason, actor_ref, idem_key, recorded_at`

func scanInput(r pgx.Row) (Input, error) {
	var in Input
	var kg *string
	err := r.Scan(&in.ID, &in.LotRef, &in.Kind, &in.SourceRef, &in.CollectionOrderRef, &in.HTXRef, &in.Point, &in.Route, &kg, &in.MoisturePct,
		&in.Rolls, &in.PlotRef, &in.Stage, &in.EventTime, &in.SupersedesID, &in.CorrectionReason, &in.ActorRef, &in.IdemKey, &in.RecordedAt)
	if err == nil {
		in.MassKg, err = kgPtr(kg)
	}
	return in, err
}

// InsertInput ghi đầu vào.
func InsertInput(ctx context.Context, tx pgx.Tx, tid uuid.UUID, in *Input) error {
	return ledger.Classify(tx.QueryRow(ctx, `INSERT INTO mkg_costing.input_event
	  (id, tenant_id, stream_key, lot_ref, kind, source_ref, collection_order_ref, htx_ref, point, route, mass_kg, moisture_pct, rolls,
	   plot_ref, stage, event_time, supersedes_id, correction_reason, actor_ref, idem_key, rule_version)
	  VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11::numeric, $12::numeric, $13, $14, $15, $16, $17, $18, $19, $20, $21)
	  RETURNING recorded_at`,
		in.ID, tid, "lot:"+in.LotRef.String(), in.LotRef, in.Kind, in.SourceRef, in.CollectionOrderRef, in.HTXRef, in.Point, in.Route,
		kgArg(in.MassKg), in.MoisturePct, in.Rolls, in.PlotRef, in.Stage, in.EventTime, in.SupersedesID, in.CorrectionReason, in.ActorRef,
		in.IdemKey, domain.RuleInput).Scan(&in.RecordedAt))
}

// InputByIdem đọc đầu vào theo idem_key.
func InputByIdem(ctx context.Context, tx pgx.Tx, idem string) (*Input, error) {
	return one(scanInput(tx.QueryRow(ctx, "SELECT "+inputCols+" FROM mkg_costing.input_event WHERE idem_key = $1"+ledger.NewestByIdem(""), idem)))
}

// InputBySource đọc đầu vào mới nhất của một nguồn (phiếu / đếm cuộn).
func InputBySource(ctx context.Context, tx pgx.Tx, source uuid.UUID) (*Input, error) {
	return one(scanInput(tx.QueryRow(ctx, "SELECT "+inputCols+` FROM mkg_costing.input_event WHERE source_ref = $1
	  ORDER BY recorded_at DESC, seq DESC LIMIT 1`, source)))
}

// EffectiveInputsForOrder trả đầu vào HIỆU LỰC (chưa bị bản bù thay) của một lệnh gom.
func EffectiveInputsForOrder(ctx context.Context, tx pgx.Tx, order uuid.UUID) ([]Input, error) {
	rows, err := tx.Query(ctx, "SELECT "+inputCols+` FROM mkg_costing.input_event i
	  WHERE collection_order_ref = $1
	    AND NOT EXISTS (SELECT 1 FROM mkg_costing.input_event s WHERE s.tenant_id = i.tenant_id AND s.supersedes_id = i.id)
	  ORDER BY event_time, id`, order)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Input, error) { return scanInput(r) })
}

// Collection là bảng chiếu collection_current.
type Collection struct {
	OrderRef      uuid.UUID
	HTXRef        *uuid.UUID
	Rolls         *int
	WeighedKg     *mass.Kg
	WeighPoint    *string
	WeighedLots   int
	LastEventTime time.Time
}

// AggregateCollection tính bảng chiếu của lệnh gom từ đầu vào hiệu lực: số cuộn = Σ theo thửa của
// công đoạn cuối có dữ liệu; khối lượng = Σ HUB_IN của lô cân (lô DIRECT: CUSTOMER_IN).
func AggregateCollection(order uuid.UUID, ins []Input) *Collection {
	if len(ins) == 0 {
		return nil
	}
	c := &Collection{OrderRef: order}
	rank := map[string]int{"bale": 1, "gather": 2, "boat_load": 3}
	type plotStage struct {
		plot  uuid.UUID
		stage string
	}
	byStage := map[plotStage]int{}
	best := map[uuid.UUID]int{}
	type lotW struct {
		hubIn, custIn *mass.Kg
	}
	lots := map[uuid.UUID]*lotW{}
	for _, in := range ins {
		if in.EventTime.After(c.LastEventTime) {
			c.LastEventTime = in.EventTime
		}
		if in.HTXRef != nil {
			h := *in.HTXRef
			c.HTXRef = &h
		}
		switch in.Kind {
		case "rolls":
			var plot uuid.UUID
			if in.PlotRef != nil {
				plot = *in.PlotRef
			}
			st := ""
			if in.Stage != nil {
				st = *in.Stage
			}
			byStage[plotStage{plot, st}] += *in.Rolls
			if rank[st] > best[plot] {
				best[plot] = rank[st]
			}
		case "weigh":
			w := lots[in.LotRef]
			if w == nil {
				w = &lotW{}
				lots[in.LotRef] = w
			}
			k := *in.MassKg
			switch *in.Point {
			case "HUB_IN":
				w.hubIn = &k
			case "CUSTOMER_IN":
				if in.Route != nil && *in.Route == "DIRECT" {
					w.custIn = &k
				}
			}
		}
	}
	if len(best) > 0 {
		total := 0
		for ps, n := range byStage {
			if rank[ps.stage] == best[ps.plot] {
				total += n
			}
		}
		c.Rolls = &total
	}
	var sum mass.Kg
	point := ""
	for _, w := range lots {
		switch {
		case w.hubIn != nil:
			sum, point = sum.Add(*w.hubIn), "HUB_IN"
			c.WeighedLots++
		case w.custIn != nil:
			sum = sum.Add(*w.custIn)
			if point == "" {
				point = "CUSTOMER_IN"
			}
			c.WeighedLots++
		}
	}
	if c.WeighedLots > 0 {
		c.WeighedKg, c.WeighPoint = &sum, &point
	}
	return c
}

// UpsertCollection ghi bảng chiếu của lệnh gom.
func UpsertCollection(ctx context.Context, tx pgx.Tx, tid uuid.UUID, c Collection) error {
	_, err := tx.Exec(ctx, `INSERT INTO mkg_costing.collection_current
	  (tenant_id, collection_order_ref, htx_ref, rolls, weighed_kg, weigh_point, weighed_lots, last_event_time)
	  VALUES ($1, $2, $3, $4, $5::numeric, $6, $7, $8)
	  ON CONFLICT (tenant_id, collection_order_ref) DO UPDATE SET htx_ref = EXCLUDED.htx_ref, rolls = EXCLUDED.rolls,
	    weighed_kg = EXCLUDED.weighed_kg, weigh_point = EXCLUDED.weigh_point, weighed_lots = EXCLUDED.weighed_lots,
	    last_event_time = EXCLUDED.last_event_time, updated_at = now()`,
		tid, c.OrderRef, c.HTXRef, c.Rolls, kgArg(c.WeighedKg), c.WeighPoint, c.WeighedLots, c.LastEventTime)
	return err
}

// CollectionByOrder đọc bảng chiếu của lệnh gom.
func CollectionByOrder(ctx context.Context, tx pgx.Tx, order uuid.UUID) (*Collection, error) {
	var c Collection
	var kg *string
	err := tx.QueryRow(ctx, `SELECT collection_order_ref, htx_ref, rolls, weighed_kg::text, weigh_point, weighed_lots, last_event_time
	  FROM mkg_costing.collection_current WHERE collection_order_ref = $1`, order).
		Scan(&c.OrderRef, &c.HTXRef, &c.Rolls, &kg, &c.WeighPoint, &c.WeighedLots, &c.LastEventTime)
	p, err := one(c, err)
	if err != nil || p == nil {
		return p, err
	}
	p.WeighedKg, err = kgPtr(kg)
	return p, err
}

// ---------------------------------------------------------------------------
// Bút toán chi phí / doanh thu
// ---------------------------------------------------------------------------

// Entry là một bản ghi sổ cost_entry.
type Entry struct {
	ID               uuid.UUID
	LotRef           uuid.UUID
	PeriodCode       string
	LateFrom         *string
	Category         string
	Amount           money.Amount
	DeltaAmount      money.Amount
	MassKg           *mass.Kg
	DeltaMassKg      *mass.Kg
	DimensionKind    string
	DimensionRef     *uuid.UUID
	SourceKind       string
	SourceRef        *uuid.UUID
	AllocationRef    *uuid.UUID
	EntryKind        string
	Note             *string
	EventTime        time.Time
	SupersedesID     *uuid.UUID
	CorrectionReason *string
	ActorRef         uuid.UUID
	IdemKey          string
	RuleVersion      string
	Seq              int64
	RecordedAt       time.Time
}

const entryCols = `id, lot_ref, period_code, late_from, category, amount::text, delta_amount::text, mass_kg::text, delta_mass_kg::text,
  dimension_kind, dimension_ref, source_kind, source_ref, allocation_ref, entry_kind, note, event_time, supersedes_id, correction_reason,
  actor_ref, idem_key, rule_version, seq, recorded_at`

func scanEntry(r pgx.Row) (Entry, error) {
	var e Entry
	var amt, delta string
	var kg, dkg *string
	err := r.Scan(&e.ID, &e.LotRef, &e.PeriodCode, &e.LateFrom, &e.Category, &amt, &delta, &kg, &dkg, &e.DimensionKind, &e.DimensionRef,
		&e.SourceKind, &e.SourceRef, &e.AllocationRef, &e.EntryKind, &e.Note, &e.EventTime, &e.SupersedesID, &e.CorrectionReason,
		&e.ActorRef, &e.IdemKey, &e.RuleVersion, &e.Seq, &e.RecordedAt)
	if err != nil {
		return e, err
	}
	if e.Amount, err = money.ParseAmount(amt); err != nil {
		return e, err
	}
	if e.DeltaAmount, err = money.ParseAmount(delta); err != nil {
		return e, err
	}
	if e.MassKg, err = kgPtr(kg); err != nil {
		return e, err
	}
	e.DeltaMassKg, err = kgPtr(dkg)
	return e, err
}

// InsertEntry ghi bút toán và cộng delta vào bảng chiếu lot_cost_current cùng giao dịch (R-21).
// Trigger DB từ chối nếu kỳ period_code không mở.
func InsertEntry(ctx context.Context, tx pgx.Tx, tid uuid.UUID, e *Entry) error {
	err := tx.QueryRow(ctx, `INSERT INTO mkg_costing.cost_entry
	  (id, tenant_id, stream_key, lot_ref, period_code, late_from, category, amount, delta_amount, mass_kg, delta_mass_kg, dimension_kind,
	   dimension_ref, source_kind, source_ref, allocation_ref, entry_kind, note, event_time, supersedes_id, correction_reason, actor_ref,
	   idem_key, rule_version)
	  VALUES ($1, $2, $3, $4, $5, $6, $7, $8::numeric, $9::numeric, $10::numeric, $11::numeric, $12, $13, $14, $15, $16, $17, $18, $19,
	          $20, $21, $22, $23, $24) RETURNING seq, recorded_at`,
		e.ID, tid, "lot:"+e.LotRef.String(), e.LotRef, e.PeriodCode, e.LateFrom, e.Category, e.Amount.String(), e.DeltaAmount.String(),
		kgArg(e.MassKg), kgArg(e.DeltaMassKg), e.DimensionKind, e.DimensionRef, e.SourceKind, e.SourceRef, e.AllocationRef, e.EntryKind,
		e.Note, e.EventTime, e.SupersedesID, e.CorrectionReason, e.ActorRef, e.IdemKey, e.RuleVersion).Scan(&e.Seq, &e.RecordedAt)
	if err != nil {
		return ledger.Classify(err)
	}
	return applyEntry(ctx, tx, tid, *e)
}

// applyEntry là hàm chiếu của lot_cost_current (Σ delta theo lô × nhóm).
func applyEntry(ctx context.Context, tx pgx.Tx, tid uuid.UUID, e Entry) error {
	_, err := tx.Exec(ctx, `INSERT INTO mkg_costing.lot_cost_current (tenant_id, lot_ref, category, amount, mass_kg, entries)
	  VALUES ($1, $2, $3, $4::numeric, $5::numeric, 1)
	  ON CONFLICT (tenant_id, lot_ref, category) DO UPDATE SET amount = lot_cost_current.amount + EXCLUDED.amount,
	    mass_kg = CASE WHEN EXCLUDED.mass_kg IS NULL THEN lot_cost_current.mass_kg
	                   ELSE COALESCE(lot_cost_current.mass_kg, 0) + EXCLUDED.mass_kg END,
	    entries = lot_cost_current.entries + 1, updated_at = now()`,
		tid, e.LotRef, e.Category, e.DeltaAmount.String(), kgArg(e.DeltaMassKg))
	return err
}

// EntryByID đọc bút toán.
func EntryByID(ctx context.Context, tx pgx.Tx, id uuid.UUID) (*Entry, error) {
	return one(scanEntry(tx.QueryRow(ctx, "SELECT "+entryCols+" FROM mkg_costing.cost_entry WHERE id = $1", id)))
}

// EntryByIdem đọc bút toán theo idem_key.
func EntryByIdem(ctx context.Context, tx pgx.Tx, idem string) (*Entry, error) {
	return one(scanEntry(tx.QueryRow(ctx, "SELECT "+entryCols+" FROM mkg_costing.cost_entry WHERE idem_key = $1"+ledger.NewestByIdem(""), idem)))
}

// EntrySuperseded cho biết bút toán đã bị dòng khác thay.
func EntrySuperseded(ctx context.Context, tx pgx.Tx, id uuid.UUID) (bool, error) {
	var ok bool
	err := tx.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM mkg_costing.cost_entry WHERE supersedes_id = $1)", id).Scan(&ok)
	return ok, err
}

// LiveEntriesOfAllocation trả bút toán hiệu lực (chưa bị thay) sinh từ một kết quả phân bổ.
func LiveEntriesOfAllocation(ctx context.Context, tx pgx.Tx, alloc uuid.UUID) ([]Entry, error) {
	rows, err := tx.Query(ctx, "SELECT "+entryCols+` FROM mkg_costing.cost_entry e WHERE allocation_ref = $1
	  AND NOT EXISTS (SELECT 1 FROM mkg_costing.cost_entry s WHERE s.tenant_id = e.tenant_id AND s.supersedes_id = e.id)
	  ORDER BY lot_ref`, alloc)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Entry, error) { return scanEntry(r) })
}

// EntriesForLot liệt kê bút toán của lô (theo thứ tự chuỗi).
func EntriesForLot(ctx context.Context, tx pgx.Tx, lot uuid.UUID, afterSeq int64, limit int) ([]Entry, error) {
	rows, err := tx.Query(ctx, "SELECT "+entryCols+` FROM mkg_costing.cost_entry WHERE stream_key = $1 AND seq > $2 ORDER BY seq LIMIT $3`,
		"lot:"+lot.String(), afterSeq, limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Entry, error) { return scanEntry(r) })
}

// EntriesForPeriod trả mọi bút toán của kỳ (ảnh chụp đầu vào), sắp theo id.
func EntriesForPeriod(ctx context.Context, tx pgx.Tx, code string, asOf time.Time) ([]Entry, error) {
	rows, err := tx.Query(ctx, "SELECT "+entryCols+` FROM mkg_costing.cost_entry WHERE period_code = $1 AND recorded_at <= $2 ORDER BY id`,
		code, asOf)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Entry, error) { return scanEntry(r) })
}

// LotCost là một dòng lot_cost_current.
type LotCost struct {
	Category string
	Amount   money.Amount
	MassKg   *mass.Kg
	Entries  int
}

// LotCosts đọc giá thành lô theo nhóm.
func LotCosts(ctx context.Context, tx pgx.Tx, lot uuid.UUID) ([]LotCost, error) {
	rows, err := tx.Query(ctx, `SELECT category, amount::text, mass_kg::text, entries FROM mkg_costing.lot_cost_current
	  WHERE lot_ref = $1 ORDER BY category`, lot)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (LotCost, error) {
		var c LotCost
		var amt string
		var kg *string
		if err := r.Scan(&c.Category, &amt, &kg, &c.Entries); err != nil {
			return c, err
		}
		var err error
		if c.Amount, err = money.ParseAmount(amt); err != nil {
			return c, err
		}
		c.MassKg, err = kgPtr(kg)
		return c, err
	})
}

// AdjustingAuthors trả người lập bút toán điều chỉnh của kỳ (R-56).
func AdjustingAuthors(ctx context.Context, tx pgx.Tx, code string) ([]uuid.UUID, error) {
	rows, err := tx.Query(ctx, `SELECT DISTINCT actor_ref FROM mkg_costing.cost_entry WHERE period_code = $1 AND entry_kind = 'adjusting'
	  ORDER BY actor_ref`, code)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
}

// ---------------------------------------------------------------------------
// Kết quả phân bổ
// ---------------------------------------------------------------------------

// Allocation là một bản ghi sổ allocation_result.
type Allocation struct {
	ID               uuid.UUID
	PeriodRef        uuid.UUID
	Kind             string
	SourceRef        uuid.UUID
	SourceEventTime  time.Time
	Basis            string
	TotalAmount      *money.Amount
	TotalKg          *mass.Kg
	Lines            json.RawMessage
	Inputs           json.RawMessage
	InputsSHA256     string
	ResultSHA256     string
	Pins             domain.Pins
	ParamsAsOf       *time.Time // nil = now() của giao dịch
	EventTime        time.Time
	SupersedesID     *uuid.UUID
	CorrectionReason *string
	ActorRef         uuid.UUID
	IdemKey          string
	RuleVersion      string
	Seq              int64
	RecordedAt       time.Time
	SupersededBy     *uuid.UUID // từ bảng chiếu
}

const allocCols = `a.id, a.period_ref, a.kind, a.source_ref, a.source_event_time, a.basis, a.total_amount::text, a.total_kg::text, a.lines,
  a.inputs, a.inputs_sha256, a.result_sha256, a.param_pins, a.params_as_of, a.event_time, a.supersedes_id, a.correction_reason,
  a.actor_ref, a.idem_key, a.rule_version, a.seq, a.recorded_at, c.superseded_by`

const allocFrom = ` FROM mkg_costing.allocation_result a
  LEFT JOIN mkg_costing.allocation_current c ON c.tenant_id = a.tenant_id AND c.allocation_id = a.id`

func scanAlloc(r pgx.Row) (Allocation, error) {
	var a Allocation
	var amt, kg *string
	var pins []byte
	var asOf time.Time
	err := r.Scan(&a.ID, &a.PeriodRef, &a.Kind, &a.SourceRef, &a.SourceEventTime, &a.Basis, &amt, &kg, &a.Lines, &a.Inputs, &a.InputsSHA256,
		&a.ResultSHA256, &pins, &asOf, &a.EventTime, &a.SupersedesID, &a.CorrectionReason, &a.ActorRef, &a.IdemKey, &a.RuleVersion, &a.Seq,
		&a.RecordedAt, &a.SupersededBy)
	if err != nil {
		return a, err
	}
	a.ParamsAsOf = &asOf
	if a.TotalAmount, err = amountPtr(amt); err != nil {
		return a, err
	}
	if a.TotalKg, err = kgPtr(kg); err != nil {
		return a, err
	}
	err = json.Unmarshal(pins, &a.Pins)
	return a, err
}

// InsertAllocation ghi kết quả phân bổ + bảng chiếu (đánh dấu kết quả cũ bị thay) cùng giao dịch.
func InsertAllocation(ctx context.Context, tx pgx.Tx, tid uuid.UUID, a *Allocation) error {
	var asOf time.Time
	err := tx.QueryRow(ctx, `INSERT INTO mkg_costing.allocation_result
	  (id, tenant_id, stream_key, period_ref, kind, source_ref, source_event_time, basis, total_amount, total_kg, lines, inputs, inputs_sha256,
	   result_sha256, param_pins, params_as_of, event_time, supersedes_id, correction_reason, actor_ref, idem_key, rule_version)
	  VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9::numeric, $10::numeric, $11::jsonb, $12::jsonb, $13, $14, $15::jsonb, COALESCE($16, now()), $17,
	          $18, $19, $20, $21, $22) RETURNING seq, recorded_at, params_as_of`,
		a.ID, tid, "period:"+a.PeriodRef.String(), a.PeriodRef, a.Kind, a.SourceRef, a.SourceEventTime, a.Basis, amountArg(a.TotalAmount),
		kgArg(a.TotalKg), string(a.Lines), string(a.Inputs), a.InputsSHA256, a.ResultSHA256, pinsJSON(a.Pins), a.ParamsAsOf, a.EventTime,
		a.SupersedesID, a.CorrectionReason, a.ActorRef, a.IdemKey, a.RuleVersion).Scan(&a.Seq, &a.RecordedAt, &asOf)
	if err != nil {
		return ledger.Classify(err)
	}
	a.ParamsAsOf = &asOf
	return applyAllocation(ctx, tx, tid, *a)
}

// applyAllocation là hàm chiếu của allocation_current.
func applyAllocation(ctx context.Context, tx pgx.Tx, tid uuid.UUID, a Allocation) error {
	if a.SupersedesID != nil {
		if _, err := tx.Exec(ctx, `UPDATE mkg_costing.allocation_current SET superseded_by = $2, updated_at = now()
		  WHERE allocation_id = $1`, *a.SupersedesID, a.ID); err != nil {
			return err
		}
	}
	_, err := tx.Exec(ctx, `INSERT INTO mkg_costing.allocation_current
	  (tenant_id, allocation_id, kind, source_ref, period_ref, total_amount, total_kg, result_sha256, rule_version)
	  VALUES ($1, $2, $3, $4, $5, $6::numeric, $7::numeric, $8, $9)`,
		tid, a.ID, a.Kind, a.SourceRef, a.PeriodRef, amountArg(a.TotalAmount), kgArg(a.TotalKg), a.ResultSHA256, a.RuleVersion)
	return err
}

// AllocationByID đọc kết quả phân bổ.
func AllocationByID(ctx context.Context, tx pgx.Tx, id uuid.UUID) (*Allocation, error) {
	return one(scanAlloc(tx.QueryRow(ctx, "SELECT "+allocCols+allocFrom+" WHERE a.id = $1", id)))
}

// AllocationByIdem đọc kết quả phân bổ theo idem_key.
func AllocationByIdem(ctx context.Context, tx pgx.Tx, idem string) (*Allocation, error) {
	return one(scanAlloc(tx.QueryRow(ctx, "SELECT "+allocCols+allocFrom+" WHERE a.idem_key = $1"+ledger.NewestByIdem("a"), idem)))
}

// LiveAllocation đọc kết quả hiệu lực của một nguồn.
func LiveAllocation(ctx context.Context, tx pgx.Tx, kind string, source uuid.UUID) (*Allocation, error) {
	return one(scanAlloc(tx.QueryRow(ctx, "SELECT "+allocCols+allocFrom+` WHERE c.kind = $1 AND c.source_ref = $2 AND c.superseded_by IS NULL`,
		kind, source)))
}

// AllocationsForSource liệt kê mọi kết quả (kể cả đã bị thay) của một nguồn.
func AllocationsForSource(ctx context.Context, tx pgx.Tx, source uuid.UUID) ([]Allocation, error) {
	rows, err := tx.Query(ctx, "SELECT "+allocCols+allocFrom+" WHERE a.source_ref = $1 ORDER BY a.recorded_at, a.seq", source)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Allocation, error) { return scanAlloc(r) })
}

// ---------------------------------------------------------------------------
// Quy đổi cuộn → tấn
// ---------------------------------------------------------------------------

// Conversion là một bản ghi sổ conversion_result.
type Conversion struct {
	ID             uuid.UUID
	OrderRef       uuid.UUID
	Rolls          int
	WeighedKg      mass.Kg
	WeighPoint     string
	YieldKgPerRoll mass.Kg
	MoistureRule   string
	ConvertedKg    mass.Kg
	InputsSHA256   string
	Pins           domain.Pins
	EventTime      time.Time
	SupersedesID   *uuid.UUID
	IdemKey        string
	RuleVersion    string
	RecordedAt     time.Time
}

// InsertConversion ghi quy đổi + bảng chiếu.
func InsertConversion(ctx context.Context, tx pgx.Tx, tid uuid.UUID, c *Conversion) error {
	var reason *string
	if c.SupersedesID != nil {
		r := "tính lại khi đầu vào thay đổi (số cuộn / phiếu cân)"
		reason = &r
	}
	err := tx.QueryRow(ctx, `INSERT INTO mkg_costing.conversion_result
	  (id, tenant_id, stream_key, collection_order_ref, rolls, weighed_kg, weigh_point, yield_kg_per_roll, moisture_rule, converted_kg,
	   inputs_sha256, param_pins, event_time, supersedes_id, correction_reason, actor_ref, idem_key, rule_version)
	  VALUES ($1, $2, $3, $4, $5, $6::numeric, $7, $8::numeric, $9, $10::numeric, $11, $12::jsonb, $13, $14, $15, $16, $17, $18) RETURNING recorded_at`,
		c.ID, tid, "order:"+c.OrderRef.String(), c.OrderRef, c.Rolls, c.WeighedKg.String(), c.WeighPoint, c.YieldKgPerRoll.String(),
		c.MoistureRule, c.ConvertedKg.String(), c.InputsSHA256, pinsJSON(c.Pins), c.EventTime, c.SupersedesID, reason, domain.SystemActorRef,
		c.IdemKey, c.RuleVersion).Scan(&c.RecordedAt)
	if err != nil {
		return ledger.Classify(err)
	}
	return applyConversion(ctx, tx, tid, *c)
}

func applyConversion(ctx context.Context, tx pgx.Tx, tid uuid.UUID, c Conversion) error {
	_, err := tx.Exec(ctx, `INSERT INTO mkg_costing.conversion_current
	  (tenant_id, collection_order_ref, result_id, rolls, weighed_kg, yield_kg_per_roll, converted_kg, inputs_sha256, rule_version)
	  VALUES ($1, $2, $3, $4, $5::numeric, $6::numeric, $7::numeric, $8, $9)
	  ON CONFLICT (tenant_id, collection_order_ref) DO UPDATE SET result_id = EXCLUDED.result_id, rolls = EXCLUDED.rolls,
	    weighed_kg = EXCLUDED.weighed_kg, yield_kg_per_roll = EXCLUDED.yield_kg_per_roll, converted_kg = EXCLUDED.converted_kg,
	    inputs_sha256 = EXCLUDED.inputs_sha256, rule_version = EXCLUDED.rule_version, updated_at = now()`,
		tid, c.OrderRef, c.ID, c.Rolls, c.WeighedKg.String(), c.YieldKgPerRoll.String(), c.ConvertedKg.String(), c.InputsSHA256, c.RuleVersion)
	return err
}

// CurrentConversion đọc quy đổi hiệu lực của lệnh gom.
func CurrentConversion(ctx context.Context, tx pgx.Tx, order uuid.UUID) (*Conversion, error) {
	var c Conversion
	var kg, y, conv string
	var pins []byte
	err := tx.QueryRow(ctx, `SELECT r.id, r.collection_order_ref, r.rolls, r.weighed_kg::text, r.weigh_point, r.yield_kg_per_roll::text,
	  r.moisture_rule, r.converted_kg::text, r.inputs_sha256, r.param_pins, r.event_time, r.supersedes_id, r.idem_key, r.rule_version, r.recorded_at
	  FROM mkg_costing.conversion_current cc JOIN mkg_costing.conversion_result r ON r.tenant_id = cc.tenant_id AND r.id = cc.result_id
	  WHERE cc.collection_order_ref = $1`, order).
		Scan(&c.ID, &c.OrderRef, &c.Rolls, &kg, &c.WeighPoint, &y, &c.MoistureRule, &conv, &c.InputsSHA256, &pins, &c.EventTime,
			&c.SupersedesID, &c.IdemKey, &c.RuleVersion, &c.RecordedAt)
	p, err := one(c, err)
	if err != nil || p == nil {
		return p, err
	}
	if p.WeighedKg, err = mass.ParseKg(kg); err != nil {
		return nil, err
	}
	if p.YieldKgPerRoll, err = mass.ParseKg(y); err != nil {
		return nil, err
	}
	if p.ConvertedKg, err = mass.ParseKg(conv); err != nil {
		return nil, err
	}
	return p, json.Unmarshal(pins, &p.Pins)
}
