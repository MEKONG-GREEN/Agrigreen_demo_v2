package store

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/costing/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/costing/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ledger"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/mass"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/money"
)

// Settlement là trạng thái ĐẦY ĐỦ của một quyết toán HTX. Mỗi bản ghi sổ settlement_event mang
// toàn bộ trạng thái sau hành động → bảng chiếu = bản ghi cuối (dựng lại tất định).
type Settlement struct {
	ID                 uuid.UUID // settlement_id
	Kind               string    // original | adjustment
	Status             port.SettlementStatus
	CollectionOrderRef uuid.UUID
	HTXRef             uuid.UUID
	HarvestDate        string // YYYY-MM-DD
	ContractRef        *uuid.UUID
	ContractVersion    *int
	Basis              *string
	PriceSource        *string
	UnitPrice          *money.Price
	Rolls              *int
	MassKg             *mass.Kg
	GrossAmount        *money.Amount
	Deductions         money.Amount
	Amount             *money.Amount
	BlockedReason      *string
	DueOn              *string
	PeriodRef          *uuid.UUID
	PreparedBy         uuid.UUID
	ApprovedBy         *uuid.UUID
	ApprovedAt         *time.Time
	SupersedesID       *uuid.UUID
	RuleVersion        string
	LastSeq            int64
}

// SettlementEvent là một hành động trên quyết toán.
type SettlementEvent struct {
	ID        uuid.UUID
	Action    string
	State     Settlement
	Reason    *string
	Pins      domain.Pins
	EventTime time.Time
	ActorRef  uuid.UUID
	IdemKey   string
}

func priceArg(p *money.Price) *string {
	if p == nil {
		return nil
	}
	s := p.String()
	return &s
}

// InsertSettlementEvent ghi sổ quyết toán và áp bảng chiếu (R-21). DB CHECK: approved_by ≠ prepared_by.
func InsertSettlementEvent(ctx context.Context, tx pgx.Tx, tid uuid.UUID, e *SettlementEvent) error {
	s := &e.State
	var corr *string
	if s.SupersedesID != nil {
		r := "quyết toán điều chỉnh của quyết toán đã duyệt / đã trả"
		corr = &r
	}
	var seq int64
	var rec time.Time
	err := tx.QueryRow(ctx, `INSERT INTO mkg_costing.settlement_event
	  (id, tenant_id, stream_key, settlement_id, action, kind, status, collection_order_ref, htx_ref, harvest_date, contract_ref,
	   contract_version, basis, price_source, unit_price, rolls, mass_kg, gross_amount, deductions, amount, blocked_reason, due_on, period_ref,
	   prepared_by, approved_by, reason, param_pins, event_time, supersedes_id, correction_reason, actor_ref, idem_key, rule_version)
	  VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10::date, $11, $12, $13, $14, $15::numeric, $16, $17::numeric, $18::numeric, $19::numeric,
	          $20::numeric, $21, $22::date, $23, $24, $25, $26, $27::jsonb, $28, $29, $30, $31, $32, $33) RETURNING seq, recorded_at`,
		e.ID, tid, "settlement:"+s.ID.String(), s.ID, e.Action, s.Kind, string(s.Status), s.CollectionOrderRef, s.HTXRef, s.HarvestDate,
		s.ContractRef, s.ContractVersion, s.Basis, s.PriceSource, priceArg(s.UnitPrice), s.Rolls, kgArg(s.MassKg), amountArg(s.GrossAmount),
		s.Deductions.String(), amountArg(s.Amount), s.BlockedReason, s.DueOn, s.PeriodRef, s.PreparedBy, s.ApprovedBy, e.Reason,
		pinsJSON(e.Pins), e.EventTime, s.SupersedesID, corr, e.ActorRef, e.IdemKey, domain.RuleSettlement).Scan(&seq, &rec)
	if err != nil {
		return ledger.Classify(err)
	}
	s.LastSeq, s.RuleVersion = seq, domain.RuleSettlement
	if e.Action == "approved" {
		t := e.EventTime
		s.ApprovedAt = &t
	}
	return applySettlement(ctx, tx, tid, *s)
}

func applySettlement(ctx context.Context, tx pgx.Tx, tid uuid.UUID, s Settlement) error {
	_, err := tx.Exec(ctx, `INSERT INTO mkg_costing.settlement_current
	  (tenant_id, settlement_id, kind, status, collection_order_ref, htx_ref, harvest_date, contract_ref, contract_version, basis, price_source,
	   unit_price, rolls, mass_kg, gross_amount, deductions, amount, blocked_reason, due_on, period_ref, prepared_by, approved_by, approved_at,
	   supersedes_id, rule_version, last_seq)
	  VALUES ($1, $2, $3, $4, $5, $6, $7::date, $8, $9, $10, $11, $12::numeric, $13, $14::numeric, $15::numeric, $16::numeric, $17::numeric,
	          $18, $19::date, $20, $21, $22, $23, $24, $25, $26)
	  ON CONFLICT (tenant_id, settlement_id) DO UPDATE SET status = EXCLUDED.status, contract_ref = EXCLUDED.contract_ref,
	    contract_version = EXCLUDED.contract_version, basis = EXCLUDED.basis, price_source = EXCLUDED.price_source,
	    unit_price = EXCLUDED.unit_price, rolls = EXCLUDED.rolls, mass_kg = EXCLUDED.mass_kg, gross_amount = EXCLUDED.gross_amount,
	    deductions = EXCLUDED.deductions, amount = EXCLUDED.amount, blocked_reason = EXCLUDED.blocked_reason, due_on = EXCLUDED.due_on,
	    period_ref = EXCLUDED.period_ref, prepared_by = EXCLUDED.prepared_by, approved_by = EXCLUDED.approved_by,
	    approved_at = COALESCE(EXCLUDED.approved_at, settlement_current.approved_at), rule_version = EXCLUDED.rule_version,
	    last_seq = EXCLUDED.last_seq, updated_at = now()`,
		tid, s.ID, s.Kind, string(s.Status), s.CollectionOrderRef, s.HTXRef, s.HarvestDate, s.ContractRef, s.ContractVersion, s.Basis,
		s.PriceSource, priceArg(s.UnitPrice), s.Rolls, kgArg(s.MassKg), amountArg(s.GrossAmount), s.Deductions.String(), amountArg(s.Amount),
		s.BlockedReason, s.DueOn, s.PeriodRef, s.PreparedBy, s.ApprovedBy, s.ApprovedAt, s.SupersedesID, s.RuleVersion, s.LastSeq)
	return err
}

const settlementCols = `settlement_id, kind, status, collection_order_ref, htx_ref, harvest_date::text, contract_ref, contract_version, basis,
  price_source, unit_price::text, rolls, mass_kg::text, gross_amount::text, deductions::text, amount::text, blocked_reason, due_on::text,
  period_ref, prepared_by, approved_by, approved_at, supersedes_id, rule_version, last_seq`

func scanSettlement(r pgx.Row) (Settlement, error) {
	var s Settlement
	var st, ded string
	var price, kg, gross, amt *string
	err := r.Scan(&s.ID, &s.Kind, &st, &s.CollectionOrderRef, &s.HTXRef, &s.HarvestDate, &s.ContractRef, &s.ContractVersion, &s.Basis,
		&s.PriceSource, &price, &s.Rolls, &kg, &gross, &ded, &amt, &s.BlockedReason, &s.DueOn, &s.PeriodRef, &s.PreparedBy, &s.ApprovedBy,
		&s.ApprovedAt, &s.SupersedesID, &s.RuleVersion, &s.LastSeq)
	if err != nil {
		return s, err
	}
	s.Status = port.SettlementStatus(st)
	if price != nil {
		p, err := money.ParsePrice(*price)
		if err != nil {
			return s, err
		}
		s.UnitPrice = &p
	}
	if s.MassKg, err = kgPtr(kg); err != nil {
		return s, err
	}
	if s.GrossAmount, err = amountPtr(gross); err != nil {
		return s, err
	}
	if s.Deductions, err = money.ParseAmount(ded); err != nil {
		return s, err
	}
	s.Amount, err = amountPtr(amt)
	return s, err
}

// SettlementByID đọc quyết toán hiện hành.
func SettlementByID(ctx context.Context, tx pgx.Tx, id uuid.UUID, forUpdate bool) (*Settlement, error) {
	return one(scanSettlement(tx.QueryRow(ctx, "SELECT "+settlementCols+" FROM mkg_costing.settlement_current WHERE settlement_id = $1"+
		lockClause(forUpdate), id)))
}

// SettlementIDByIdem trả settlement_id của bản ghi sổ theo idem_key.
func SettlementIDByIdem(ctx context.Context, tx pgx.Tx, idem string) (*uuid.UUID, error) {
	var id uuid.UUID
	return one(id, tx.QueryRow(ctx, "SELECT settlement_id FROM mkg_costing.settlement_event WHERE idem_key = $1"+ledger.NewestByIdem(""), idem).Scan(&id))
}

// OpenSettlementsForOrder trả quyết toán gốc CHƯA duyệt của lệnh gom (làm mới sau mỗi lần cân — PM-01).
func OpenSettlementsForOrder(ctx context.Context, tx pgx.Tx, order uuid.UUID) ([]Settlement, error) {
	rows, err := tx.Query(ctx, "SELECT "+settlementCols+` FROM mkg_costing.settlement_current
	  WHERE collection_order_ref = $1 AND kind = 'original' AND status IN ('awaiting_weighing', 'pending_confirmation') FOR UPDATE`, order)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Settlement, error) { return scanSettlement(r) })
}

// ListSettlements liệt kê quyết toán theo bộ lọc (mới trước theo settlement_id).
func ListSettlements(ctx context.Context, tx pgx.Tx, status string, order *uuid.UUID, before *uuid.UUID, limit int) ([]Settlement, error) {
	rows, err := tx.Query(ctx, "SELECT "+settlementCols+` FROM mkg_costing.settlement_current
	  WHERE ($1 = '' OR status = $1) AND ($2::uuid IS NULL OR collection_order_ref = $2) AND ($3::uuid IS NULL OR settlement_id < $3)
	  ORDER BY settlement_id DESC LIMIT $4`, status, order, before, limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Settlement, error) { return scanSettlement(r) })
}

// ApprovedSettlementsForPeriod trả bản ghi 'approved' của kỳ (ảnh chụp đầu vào).
func ApprovedSettlementsForPeriod(ctx context.Context, tx pgx.Tx, period uuid.UUID, asOf time.Time) ([]domain.SnapSettlement, error) {
	rows, err := tx.Query(ctx, `SELECT id, settlement_id, htx_ref, kind, amount::text, CASE WHEN basis = 'per_tonne' THEN mass_kg::text END
	  FROM mkg_costing.settlement_event WHERE action = 'approved' AND period_ref = $1 AND recorded_at <= $2 ORDER BY id`, period, asOf)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (domain.SnapSettlement, error) {
		var s domain.SnapSettlement
		err := r.Scan(&s.ID, &s.SettlementID, &s.HTXRef, &s.Kind, &s.Amount, &s.MassKg)
		s.RuleVersion = domain.RuleSettlement
		return s, err
	})
}

// ---------------------------------------------------------------------------
// Doanh thu carbon
// ---------------------------------------------------------------------------

// Carbon là một khoản doanh thu carbon + phần chia.
type Carbon struct {
	ID         uuid.UUID
	PeriodRef  uuid.UUID
	PeriodCode string
	Revenue    money.Amount
	Split      domain.CarbonSplit
	Pins       domain.Pins
	Reference  *string
	EventTime  time.Time
	ActorRef   uuid.UUID
	IdemKey    string
	RecordedAt time.Time
}

// InsertCarbon ghi doanh thu carbon + phần chia (trigger: kỳ phải mở).
func InsertCarbon(ctx context.Context, tx pgx.Tx, tid uuid.UUID, c *Carbon) error {
	sp := c.Split
	return ledger.Classify(tx.QueryRow(ctx, `INSERT INTO mkg_costing.carbon_revenue_event
	  (id, tenant_id, stream_key, period_ref, period_code, revenue_amount, retained_amount, farmers_amount, authority_amount, verifier_amount,
	   retained_pct, farmers_pct, authority_pct, verifier_pct, param_pins, reference, event_time, actor_ref, idem_key, rule_version)
	  VALUES ($1, $2, $3, $4, $5, $6::numeric, $7::numeric, $8::numeric, $9::numeric, $10::numeric, $11::numeric, $12::numeric, $13::numeric,
	          $14::numeric, $15::jsonb, $16, $17, $18, $19, $20) RETURNING recorded_at`,
		c.ID, tid, "period:"+c.PeriodRef.String(), c.PeriodRef, c.PeriodCode, c.Revenue.String(), sp.Retained.String(), sp.Farmers.String(),
		sp.Authority.String(), sp.Verifier.String(), sp.RetainedPct.StringFixed(2), sp.FarmersPct.StringFixed(2), sp.AuthorityPct.StringFixed(2),
		sp.VerifierPct.StringFixed(2), pinsJSON(c.Pins), c.Reference, c.EventTime, c.ActorRef, c.IdemKey, domain.RuleCarbonSplit).Scan(&c.RecordedAt))
}

const carbonCols = `id, period_ref, period_code, revenue_amount::text, retained_amount::text, farmers_amount::text, authority_amount::text,
  verifier_amount::text, retained_pct::text, farmers_pct::text, authority_pct::text, verifier_pct::text, param_pins, reference, event_time,
  actor_ref, idem_key, recorded_at`

func scanCarbon(r pgx.Row) (Carbon, error) {
	var c Carbon
	var rev, ret, fa, au, ve, rp, fp, ap, vp string
	var pins []byte
	err := r.Scan(&c.ID, &c.PeriodRef, &c.PeriodCode, &rev, &ret, &fa, &au, &ve, &rp, &fp, &ap, &vp, &pins, &c.Reference, &c.EventTime,
		&c.ActorRef, &c.IdemKey, &c.RecordedAt)
	if err != nil {
		return c, err
	}
	amounts := []*money.Amount{&c.Revenue, &c.Split.Retained, &c.Split.Farmers, &c.Split.Authority, &c.Split.Verifier}
	for i, s := range []string{rev, ret, fa, au, ve} {
		if *amounts[i], err = money.ParseAmount(s); err != nil {
			return c, err
		}
	}
	c.Split.RetainedPct, _ = parseDec(rp)
	c.Split.FarmersPct, _ = parseDec(fp)
	c.Split.AuthorityPct, _ = parseDec(ap)
	c.Split.VerifierPct, _ = parseDec(vp)
	return c, json.Unmarshal(pins, &c.Pins)
}

// CarbonByIdem đọc khoản carbon theo idem_key.
func CarbonByIdem(ctx context.Context, tx pgx.Tx, idem string) (*Carbon, error) {
	return one(scanCarbon(tx.QueryRow(ctx, "SELECT "+carbonCols+" FROM mkg_costing.carbon_revenue_event WHERE idem_key = $1"+ledger.NewestByIdem(""), idem)))
}

// CarbonForPeriod trả khoản carbon của kỳ.
func CarbonForPeriod(ctx context.Context, tx pgx.Tx, period uuid.UUID, asOf time.Time) ([]Carbon, error) {
	rows, err := tx.Query(ctx, "SELECT "+carbonCols+` FROM mkg_costing.carbon_revenue_event WHERE period_ref = $1 AND recorded_at <= $2
	  ORDER BY id`, period, asOf)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Carbon, error) { return scanCarbon(r) })
}
