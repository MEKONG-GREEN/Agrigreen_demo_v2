package store

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/measurement/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/measurement/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/mass"
)

// Reconciliation là một dòng sổ đối soát.
type Reconciliation struct {
	ID               uuid.UUID
	Seq              int64
	LotRef           uuid.UUID
	Route            port.Route
	Basis            port.PricingBasis
	HubIn, HubOut    *mass.Kg
	CustomerIn       *mass.Kg
	HubInTicket      *uuid.UUID
	HubOutTicket     *uuid.UUID
	CustomerInTicket *uuid.UUID
	HubInMoisture    *decimal.Decimal
	Result           domain.ReconcileResult
	ToleranceID      *uuid.UUID
	ToleranceLabel   string
	LossNormID       *uuid.UUID
	LossNormLabel    *string
	DisputeID        *uuid.UUID
	InputsSHA256     string
	EventTime        time.Time
	RecordedAt       time.Time
	SupersedesID     *uuid.UUID
	ActorRef         uuid.UUID
	IdemKey          string
}

func i64Ptr(p *int64) *int64 { return p }

// InsertReconciliation ghi sổ đối soát.
func InsertReconciliation(ctx context.Context, tx pgx.Tx, tid uuid.UUID, r *Reconciliation) error {
	var reason *string
	if r.SupersedesID != nil {
		s := "Đối soát lại: đầu vào đã đổi (phiếu bù / số cuộn / phiên bản tham số)"
		reason = &s
	}
	x := r.Result
	return tx.QueryRow(ctx, `INSERT INTO mkg_measurement.reconciliation
	    (id, tenant_id, stream_key, lot_ref, route, pricing_basis, hub_in_kg, hub_out_kg, customer_in_kg, hub_in_ticket, hub_out_ticket,
	     customer_in_ticket, hub_in_moisture_pct, internal_loss_kg, loss_norm_pct, expected_loss_kg, abnormal_loss_kg, internal_status,
	     commercial_diff_kg, commercial_diff_pct, commercial_status, rolls, kg_per_roll_estimate, roll_estimate_kg, roll_variance_pct,
	     roll_check, yield_kg_per_roll, yield_aggregated, outcome, tolerance_version_id, tolerance_label, loss_norm_version_id,
	     loss_norm_label, dispute_id, inputs_sha256, event_time, supersedes_id, correction_reason, actor_ref, idem_key, rule_version)
	  VALUES ($1, $2, $3, $4, $5, $6, $7::numeric, $8::numeric, $9::numeric, $10, $11, $12, $13::numeric, $14::numeric, $15::numeric,
	          $16::numeric, $17::numeric, $18, $19::numeric, $20::numeric, $21, $22, $23::numeric, $24::numeric, $25::numeric, $26,
	          $27::numeric, $28, $29, $30, $31, $32, $33, $34, $35, $36, $37, $38, $39, $40, $41)
	  RETURNING seq, recorded_at`,
		r.ID, tid, LotStream(r.LotRef), r.LotRef, string(r.Route), string(r.Basis), kgArg(r.HubIn), kgArg(r.HubOut), kgArg(r.CustomerIn),
		r.HubInTicket, r.HubOutTicket, r.CustomerInTicket, decArg(r.HubInMoisture), kgArg(x.InternalLoss), decArg(x.LossNormPct),
		kgArg(x.ExpectedLoss), kgArg(x.AbnormalLoss), x.InternalStatus, kgArg(x.CommercialDiff), decArg(x.CommercialDiffPct),
		x.CommercialStatus, i64Ptr(x.Rolls), kgArg(x.KgPerRollEstimate), kgArg(x.RollEstimate), decArg(x.RollVariancePct), x.RollCheck,
		kgArg(x.Yield), x.YieldAggregated, x.Outcome, r.ToleranceID, r.ToleranceLabel, r.LossNormID, r.LossNormLabel, r.DisputeID,
		r.InputsSHA256, r.EventTime.UTC(), r.SupersedesID, reason, r.ActorRef, r.IdemKey, domain.RuleReconcile).Scan(&r.Seq, &r.RecordedAt)
}

const reconCols = `id, seq, lot_ref, route, pricing_basis, hub_in_kg::text, hub_out_kg::text, customer_in_kg::text, hub_in_ticket,
  hub_out_ticket, customer_in_ticket, hub_in_moisture_pct::text, internal_loss_kg::text, loss_norm_pct::text, expected_loss_kg::text,
  abnormal_loss_kg::text, internal_status, commercial_diff_kg::text, commercial_diff_pct::text, commercial_status, rolls,
  kg_per_roll_estimate::text, roll_estimate_kg::text, roll_variance_pct::text, roll_check, yield_kg_per_roll::text, yield_aggregated,
  outcome, tolerance_version_id, tolerance_label, loss_norm_version_id, loss_norm_label, dispute_id, inputs_sha256, event_time,
  recorded_at, supersedes_id, actor_ref, idem_key`

func scanRecon(row pgx.Row) (Reconciliation, error) {
	var r Reconciliation
	var route, basis string
	var hin, hout, cin, moist, iloss, npct, exp, abn, cdiff, cpct, kpr, rest, rvar, yld *string
	x := &r.Result
	err := row.Scan(&r.ID, &r.Seq, &r.LotRef, &route, &basis, &hin, &hout, &cin, &r.HubInTicket, &r.HubOutTicket, &r.CustomerInTicket,
		&moist, &iloss, &npct, &exp, &abn, &x.InternalStatus, &cdiff, &cpct, &x.CommercialStatus, &x.Rolls, &kpr, &rest, &rvar,
		&x.RollCheck, &yld, &x.YieldAggregated, &x.Outcome, &r.ToleranceID, &r.ToleranceLabel, &r.LossNormID, &r.LossNormLabel,
		&r.DisputeID, &r.InputsSHA256, &r.EventTime, &r.RecordedAt, &r.SupersedesID, &r.ActorRef, &r.IdemKey)
	if err != nil {
		return r, err
	}
	r.Route, r.Basis = port.Route(route), port.PricingBasis(basis)
	kgs := []struct {
		dst **mass.Kg
		src *string
	}{{&r.HubIn, hin}, {&r.HubOut, hout}, {&r.CustomerIn, cin}, {&x.InternalLoss, iloss}, {&x.ExpectedLoss, exp},
		{&x.AbnormalLoss, abn}, {&x.CommercialDiff, cdiff}, {&x.KgPerRollEstimate, kpr}, {&x.RollEstimate, rest}, {&x.Yield, yld}}
	for _, k := range kgs {
		if *k.dst, err = kgScan(k.src); err != nil {
			return r, err
		}
	}
	decs := []struct {
		dst **decimal.Decimal
		src *string
	}{{&r.HubInMoisture, moist}, {&x.LossNormPct, npct}, {&x.CommercialDiffPct, cpct}, {&x.RollVariancePct, rvar}}
	for _, d := range decs {
		if *d.dst, err = decScan(d.src); err != nil {
			return r, err
		}
	}
	return r, nil
}

// LatestReconciliation trả lần đối soát mới nhất của lô (nil khi chưa có).
func LatestReconciliation(ctx context.Context, tx pgx.Tx, lot uuid.UUID) (*Reconciliation, error) {
	r, err := scanRecon(tx.QueryRow(ctx, "SELECT "+reconCols+" FROM mkg_measurement.reconciliation WHERE stream_key = $1 ORDER BY seq DESC LIMIT 1", LotStream(lot)))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &r, nil
}

// ReconciliationByIdem trả lần đối soát đã ghi với idem_key.
func ReconciliationByIdem(ctx context.Context, tx pgx.Tx, key string) (*Reconciliation, error) {
	r, err := scanRecon(tx.QueryRow(ctx, "SELECT "+reconCols+" FROM mkg_measurement.reconciliation WHERE idem_key = $1", key))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &r, nil
}

// Reconciliations liệt kê lịch sử đối soát của lô (mới nhất trước).
func Reconciliations(ctx context.Context, tx pgx.Tx, lot uuid.UUID) ([]Reconciliation, error) {
	rows, err := tx.Query(ctx, "SELECT "+reconCols+" FROM mkg_measurement.reconciliation WHERE stream_key = $1 ORDER BY seq DESC", LotStream(lot))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Reconciliation
	for rows.Next() {
		r, err := scanRecon(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// InputsFingerprint băm đầu vào đối soát (tất định) để chạy lại không đổi thì không ghi.
func InputsFingerprint(v any) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	s := sha256.Sum256(b)
	const hexd = "0123456789abcdef"
	out := make([]byte, 64)
	for i, c := range s {
		out[i*2], out[i*2+1] = hexd[c>>4], hexd[c&15]
	}
	return string(out), nil
}

// UpsertYield cập nhật bảng chiếu sản lượng quy đổi.
func UpsertYield(ctx context.Context, tx pgx.Tx, tid uuid.UUID, r Reconciliation) error {
	x := r.Result
	_, err := tx.Exec(ctx, `INSERT INTO mkg_measurement.lot_yield_current
	    (tenant_id, lot_ref, route, basis_point, basis_kg, rolls, yield_kg_per_roll, aggregated, reconciliation_id)
	  VALUES ($1, $2, $3, $4, $5::numeric, $6, $7::numeric, $8, $9)
	  ON CONFLICT (tenant_id, lot_ref) DO UPDATE SET route = EXCLUDED.route, basis_point = EXCLUDED.basis_point, basis_kg = EXCLUDED.basis_kg,
	    rolls = EXCLUDED.rolls, yield_kg_per_roll = EXCLUDED.yield_kg_per_roll, aggregated = EXCLUDED.aggregated,
	    reconciliation_id = EXCLUDED.reconciliation_id, updated_at = now()`,
		tid, r.LotRef, string(r.Route), string(x.YieldBasis), x.YieldBasisKg.String(), x.Rolls, kgArg(x.Yield), x.YieldAggregated, r.ID)
	return err
}

// Yield là một dòng lot_yield_current.
type Yield struct {
	LotRef           uuid.UUID
	Route            string
	BasisPoint       string
	BasisKg          mass.Kg
	Rolls            *int64
	YieldKgPerRoll   *mass.Kg
	Aggregated       bool
	ReconciliationID uuid.UUID
}

// GetYield đọc sản lượng quy đổi của lô.
func GetYield(ctx context.Context, tx pgx.Tx, lot uuid.UUID) (*Yield, error) {
	var y Yield
	var basis string
	var yld *string
	err := tx.QueryRow(ctx, `SELECT lot_ref, route, basis_point, basis_kg::text, rolls, yield_kg_per_roll::text, aggregated, reconciliation_id
	  FROM mkg_measurement.lot_yield_current WHERE lot_ref = $1`, lot).Scan(&y.LotRef, &y.Route, &y.BasisPoint, &basis, &y.Rolls, &yld,
		&y.Aggregated, &y.ReconciliationID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if y.BasisKg, err = mass.ParseKg(basis); err != nil {
		return nil, err
	}
	y.YieldKgPerRoll, err = kgScan(yld)
	return &y, err
}

// RebuildYield dựng lại lot_yield_current từ lần đối soát mới nhất của mỗi lô.
func RebuildYield(ctx context.Context, tx pgx.Tx, tid uuid.UUID) (int64, error) {
	if _, err := tx.Exec(ctx, "DELETE FROM mkg_measurement.lot_yield_current"); err != nil {
		return 0, err
	}
	rows, err := tx.Query(ctx, "SELECT DISTINCT ON (stream_key) "+reconCols+" FROM mkg_measurement.reconciliation ORDER BY stream_key, seq DESC")
	if err != nil {
		return 0, err
	}
	var rs []Reconciliation
	for rows.Next() {
		r, err := scanRecon(rows)
		if err != nil {
			rows.Close()
			return 0, err
		}
		rs = append(rs, r)
	}
	rows.Close()
	for _, r := range rs {
		r.Result.YieldBasis, r.Result.YieldBasisKg = port.PointHubIn, mass.Kg{}
		if r.Route == port.RouteDirect {
			r.Result.YieldBasis = port.PointCustomerIn
			if r.CustomerIn != nil {
				r.Result.YieldBasisKg = *r.CustomerIn
			}
		} else if r.HubIn != nil {
			r.Result.YieldBasisKg = *r.HubIn
		}
		if err := UpsertYield(ctx, tx, tid, r); err != nil {
			return 0, err
		}
	}
	return int64(len(rs)), rows.Err()
}
